package diff

import (
	"strconv"
	"unicode/utf8"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/linekey"
	"github.com/223n/dragnwash-localization-editor/internal/order"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// このファイルは「引き継ぎ元の候補」（[CatCarryFrom]）を作る。
//
// # 引き継ぎ候補（[CatCarryover]）との違い
//
// どちらも「英文が書き換わってキーが変わった行の訳を、失わないようにする」ための
// 判定だが、根拠と向きが逆である。
//
//	引き継ぎ候補   1つ前の版の再生順（git の履歴）を根拠にする。
//	               報告は公開ファイルの旧行に付き、引き継ぎ先の新キーを指す。
//	引き継ぎ元候補 コミットされている再生順の norm / fp 列を根拠にする。
//	               報告は作業コピーの新しい行に付き、引き継ぎ元の旧キーを指す。
//
// 分けたのは、効く場面が違うからである。引き継ぎ候補が見ているのは「再生順が
// 更新された」という記録で、ゲームが更新されてから data/script_order.csv が
// コミットされるまでのあいだは、まだ何も記録されていない。その窓のあいだ、
// 新しいキーは再生順のどこにも無く、作業コピーにだけ現れる。翻訳者の画面では
// ただの「未翻訳」に見え、同じ台詞の訳が公開ファイルに残っていることは
// どこにも出ない。訳がいちばん失われるのはこの窓である。
//
// # 突き合わせの層
//
// 元実装は翻訳リポジトリの tools/rekey.py（LineResolver.Resolve の写し）で、
// 4層ある。台詞ID → ハッシュ → 正規化 → 指紋の順で、当たった層がそのまま
// 確かさの順になる。
//
// ここが使うのは下2層（正規化と指紋）である。台詞IDの層を使えないのは、
// 作業コピーが line_id 列を持たないためで、ハッシュの層は「キーが変わって
// いない行」を見つける層なので、ここでは当たらない側の判定に使う。
// 上2層が要る作業（ゲームの台本そのものを入力にする rekey.py replay / packs）は
// 上流の道具に残る。
//
// # 誰に決めさせるか
//
// 元実装は行（レコード）を1つに絞れたときだけ答えを返す。ここが要るのは行では
// なく訳の出どころ、つまりキーである。同じ英文が複数のノードに現れる行は、
// 行としては複数でもキーは1つなので、キーで数え直せば候補にできる。
// 実データの 1523 種の norm のうち、行が1つだけのものは 1394 種だが、
// キーが1種に決まるものは 1472 種ある。
//
// 逆に、キーが2種以上になるものは 51 種ある。これは「正規化すると同じになる
// 別々の英文」で、どちらの訳を持ってくるべきか決められないので黙る。

// carrySource は引き継ぎ元の旧キーと、その再生順での位置。
type carrySource struct {
	key       string
	section   string
	node      string
	orderText string
	// distance は指紋で突き合わせたときのハミング距離。
	//
	// 正規化した文字が一致して突き合わせたときは [carryExact] を入れる。
	// 0 と区別が要るのは、距離 0 の指紋一致（別の文字列でも起こりうる）を
	// 「文字が一致した」と書いてしまわないためである。
	distance int
}

// carryExact は [carrySource.distance] に入れる「指紋では突き合わせていない」印。
const carryExact = -1

// cause は Finding.Note と Finding.NoteReason に入れる理由を組み立てる。
//
// 位置まで書くのは [carryTarget.cause] と同じ理由で、キーだけでは16桁hexの
// 見比べになり、翻訳者が正しさを自分で判断できないためである。
//
// 文字で当たったか指紋で当たったかを書き分けるのは、確かさが違うから。
// 正規化して一致した行は「同じ台詞の書式と記号だけが変わった」と言い切れるが、
// 指紋で当たった行は「似ている」しか言えない。後者に距離を添えるのは、
// 翻訳者が候補を見る順番を自分で決められるようにするためである。
func (s carrySource) cause() reason.Reason {
	pos := joinNonEmpty(" / ", s.section, s.node, s.orderText)
	source := s.key
	if pos != "" {
		source += " / " + pos
	}
	head := "引き継ぎ元 " + source
	distance := strconv.Itoa(s.distance)
	args := []string{"source", source, "key", s.key, "pos", pos, "distance", distance}
	if s.distance == carryExact {
		return reason.New(reason.NoteCarryFromSame,
			head+"（原文は書式と記号だけが違います）", args...)
	}
	return reason.New(reason.NoteCarryFromSimilar,
		head+"（原文が書き換わっています。指紋の距離 "+distance+"。中身を確かめてください）", args...)
}

// carryFromFindings は、作業コピーの新しい行に対する引き継ぎ元の候補を集める。
//
// 対象にするのは、次の全部に当てはまる作業コピーの行だけである。
//
//   - publish が採るハッシュ行（台詞ID行は外す。空の台詞ID行は正常な状態で、
//     埋めるよう促すと1ロケールあたり最大1839件の誤検出になる）
//   - 原文が入っている（ゲームがまだ読み込んでいない行は突き合わせられない）
//   - 訳が空（訳が入っている行に引き継ぎ元を出しても、することが無い）
//   - そのキーがコミットされている再生順に無い（あるならキーは変わっていない）
//   - 引き継ぎ元のキーに、このロケールの公開ファイルが訳を持っている
//     （持っていなければ、持ってくる訳が無い）
//
// 同じキーの行が2つあれば、先に報告した行だけを出す。画面はバッジをキー単位で
// 付けるので、2回数えると件数とバッジの数が食い違う。
func carryFromFindings(idx *orderIndex, loc Locale) []Finding {
	if !loc.HasWorking {
		return nil
	}
	published := publishedTranslations(loc)
	if len(published) == 0 {
		return nil
	}
	var out []Finding
	seen := make(map[string]struct{})
	for _, row := range loc.Working {
		k, adopted := PublishKey(row)
		if !adopted || !key.LooksLike(k) {
			continue
		}
		if row.SourceEn == "" || row.Translation != "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		if _, inOrder := idx.first[k]; inOrder {
			continue
		}
		src, ok := idx.resolveCarryFrom(row.SourceEn, row.Node, row.Speaker)
		if !ok || src.key == k {
			continue
		}
		if published[src.key] == "" {
			continue
		}
		seen[k] = struct{}{}
		f := workingFinding(idx, row)
		f.CarryFrom = src.key
		cause := src.cause()
		f.Note, f.NoteReason = cause.Text, cause
		out = append(out, f)
	}
	return out
}

// publishedTranslations は公開ファイルのハッシュ行から、キーと訳の対応を作る。
// 同じキーが2行あれば先に出たほうを採る（publish が出す形では起こらない）。
func publishedTranslations(loc Locale) map[string]string {
	out := make(map[string]string, len(loc.Published))
	for _, row := range loc.Published {
		if row.Kind != KindHash || row.Key == "" {
			continue
		}
		if _, seen := out[row.Key]; seen {
			continue
		}
		out[row.Key] = row.Translation
	}
	return out
}

// resolveCarryFrom は、いまの原文に対する引き継ぎ元の候補を再生順から探す。
//
// 元実装 tools/rekey.py の resolve の下2層にあたる。返す2つ目の値が false の
// ときは「決められなかった」であって、「引き継ぎ元が無い」ではない。
func (idx *orderIndex) resolveCarryFrom(source, node, speaker string) (carrySource, bool) {
	n := linekey.Normalize(source)
	if n == "" {
		return carrySource{}, false
	}

	// 正規化した文字が一致する層。書式タグ・記号・大文字小文字・空白だけが
	// 変わった行がここで当たる。
	if e, ok := singleKey(idx.norms[key.For(n)]); ok {
		return idx.carrySourceFor(e.Key, carryExact), true
	}

	// 指紋の層。短い台詞は当てない。"Yes" や "Wonderful!" は3文字窓の指紋が
	// 近くても別の台詞であることが多い。
	if utf8.RuneCountInString(n) < linekey.MinFuzzyLength {
		return carrySource{}, false
	}
	pool := idx.fuzzy
	if node != "" {
		// ノードが分かるなら同じノードの中だけを見る。元実装も同じで、
		// ノードそのものが再生順に無ければ突き合わせない（新しい会話は
		// 引き継ぎ元を持たない）。
		byNode, ok := idx.fuzzyByNode[node]
		if !ok {
			return carrySource{}, false
		}
		pool = byNode
	}
	fp := linekey.Fingerprint(n)
	best := linekey.MaxFuzzyDistance + 1
	var tied []order.Entry
	for _, e := range pool {
		other, ok := linekey.ParseFingerprint(e.FP)
		if !ok {
			continue
		}
		d := linekey.Distance(fp, other)
		switch {
		case d < best:
			best, tied = d, []order.Entry{e}
		case d == best:
			tied = append(tied, e)
		}
	}
	e, ok := singleKey(tied)
	if !ok && speaker != "" {
		// キーが2種以上に割れたときだけ話者で絞る。元実装は「同じ距離の行が
		// 2つ以上あれば絞る」だが、こちらはキーで数えているので、行が複数でも
		// キーが1つなら絞る必要が無い。絞った結果が空になれば、決められなかった
		// ものとして黙る（元実装も空なら None を返す）。
		e, ok = singleKey(sameSpeaker(tied, speaker))
	}
	if !ok {
		return carrySource{}, false
	}
	return idx.carrySourceFor(e.Key, best), true
}

// singleKey は、候補のキーが1種に決まるならその最初の行を返す。
//
// 行の数ではなくキーの種類で数えるのは、探しているのが訳の出どころだから。
// 同じ英文が複数のノードに現れる行は、行としては複数でもキーは1つなので、
// どこから訳を持ってくるかは決まる。
func singleKey(entries []order.Entry) (order.Entry, bool) {
	if len(entries) == 0 {
		return order.Entry{}, false
	}
	for _, e := range entries[1:] {
		if e.Key != entries[0].Key {
			return order.Entry{}, false
		}
	}
	return entries[0], true
}

// sameSpeaker は話者が一致する行だけを返す。話者名は正規化しない
// （[order.Entry.Speaker] の決まり）。
func sameSpeaker(entries []order.Entry, speaker string) []order.Entry {
	out := make([]order.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Speaker == speaker {
			out = append(out, e)
		}
	}
	return out
}

// carrySourceFor は旧キーに位置を添えて [carrySource] にする。
func (idx *orderIndex) carrySourceFor(k string, distance int) carrySource {
	src := carrySource{key: k, distance: distance}
	if section, node, orderText, _, ok := idx.position(k); ok {
		src.section, src.node, src.orderText = section, node, orderText
	}
	return src
}
