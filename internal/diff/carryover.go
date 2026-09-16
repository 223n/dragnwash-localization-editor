package diff

import (
	"fmt"

	"github.com/223n/dragnwash-localization-editor/internal/order"
)

// CarryKind は引き継ぎ元の旧行をどう扱えばよいかの区別。
//
// 分けて持つのは、旧行の始末が正反対になるから。取り違えると、生きている行の訳を
// 消すことになる。
type CarryKind int

const (
	// CarryNone は引き継ぎ候補ではないこと。[CatCarryover] 以外の Finding はこれ。
	CarryNone CarryKind = iota
	// CarryMoved は移動。旧キーはいまの再生順のどこにも無いので、旧行は要らなくなる。
	CarryMoved
	// CarryCopied は複製。旧キーはいまの再生順の別の行で生きているので、
	// 訳を新しいキーへ写したうえで、旧行の訳もそのまま残す。
	CarryCopied
)

// String は画面に出す日本語名を返す。
func (k CarryKind) String() string {
	switch k {
	case CarryMoved:
		return "移動"
	case CarryCopied:
		return "複製"
	default:
		return ""
	}
}

// staleOldOrderReason は、旧再生順として読んだものが既に更新後の内容に
// 見えるときの理由。judgeBlockReason からそのまま画面へ出る。
const staleOldOrderReason = "読めた1つ前の再生順が、いまの版と同じ内容に見えます"

// 旧再生順としては読めたが、更新前の版ではなさそうだと疑う条件。
//
// 台本から消えた行が1つでもあるなら、どこかで英文が書き換わってキーが変わった
// はず。それなのに旧版と新版のあいだで key の変化が1行も見つからないなら、
// 旧版として読んだものが既に更新後の内容になっている。
//
// 見るのは「対応表が完全に一致するか」で、「候補が0件か」でも「変化が0行か」でも
// ない。候補は取り違えを避けるためにわざと取り下げることがあるし、行が丸ごと
// 削除された更新では変化0行でも旧版は正しい。どちらを材料にしても、正しく
// 黙っているときまで「旧版が変だ」と言い出す（実際にテストで両方とも誤爆した）。
//
// 表が1行も違わないなら、旧版は消えた行について何も説明できない。そのときだけ疑う。
func looksStaleOldOrder(sameTable bool, vanished int) bool {
	return sameTable && vanished > 0
}

// carryTarget は引き継ぎ先の新しいキーと、その再生順での位置。
type carryTarget struct {
	key       string
	kind      CarryKind
	section   string
	node      string
	orderText string
	// livePos は [CarryCopied] のとき、旧キーがいまの再生順のどこで生きているか。
	//
	// 持たないと報告が嘘になる。一覧に出る位置は公開ファイルの値、つまり
	// 「旧キーがもういない古い位置」で、引き継ぎ先の位置と一字違いの同じ場所に
	// 見えることさえある。翻訳者はそこを見に行き、旧キーの行が見つからないので
	// 「もう無い」と判断して消す。実データの d12499a3f17512de がまさにこの形で、
	// 生きている場所（Unused / RyanMuddy_Intro）は報告のどこにも出なかった。
	livePos string
}

// note は Finding.Note に入れる文面を組み立てる。
//
// 位置まで書くのは、翻訳者が候補を確かめる手立てを1つでも増やすため。
// キーだけでは16桁hexの見比べになり、正しさを自分で判断できない。
// 移動か複製かも書くのは、旧行の訳を消してよいかがそこで決まるから。
func (t carryTarget) note() string {
	pos := joinNonEmpty(" / ", t.section, t.node, t.orderText)
	head := fmt.Sprintf("引き継ぎ先 %s", t.key)
	if pos != "" {
		head += " / " + pos
	}
	switch t.kind {
	case CarryCopied:
		where := t.livePos
		if where == "" {
			where = "別の行"
		}
		return head + "（複製。旧キーは " + where + " で生きているので、この行の訳は残してください）"
	default:
		return head + "（移動。旧キーはもう再生順にありません）"
	}
}

// carryoverCandidates は1ロケール分の引き継ぎ候補を、旧キーから引ける形で返す。
//
// 新旧の再生順を台詞ID (line_id) で突き合わせる。line_id は Yarn が台詞に振る
// 識別子で、英文が直されても変わらない。同じ line_id の行で key だけが変わって
// いれば、それは「その台詞の原文が書き換わった」という記録になる。
//
// 規則:
//
//   - 旧新の両方にある line_id で key が変わった → 旧キーの訳の引き継ぎ先は新キー
//   - 旧キーがいまの再生順のどこにも無い         → [CarryMoved]（旧行はもう要らない）
//   - 旧キーがいまの再生順の別の行で生きている   → [CarryCopied]（旧行の訳は残す）
//   - line_id が旧だけ → 行が消えた。候補にしない
//   - line_id が新だけ → 本当に新しい行。引き継ぎ元は無い
//
// 公開ファイルと今の再生順だけで済ませる方式（ノードごとにキー列を最長共通部分列で
// 揃える）を捨てたのは、原理的に潰せない誤検出があったため。Translations/ignore.txt
// で記録の対象外になっているキーは「どのロケールにも訳が無い」まま再生順に居座る。
// 「本当に新しく現れたキー」も公開CSVからは同じ姿に見えるので、区別がつかない。
// 実データでは、その32件が引き継ぎ先の候補に混ざった。旧版の再生順を見れば、
// 前からあったのか今回現れたのかが直接分かるので、この見分けが要らなくなる。
//
// 候補を落とす場合:
//
//   - 同じ旧キーに2つ以上の引き継ぎ先が付いた（同じ英文の2つの行が、別々の英文に
//     書き換えられた）。どちらへ移すか決められないので両方取り下げる。
//   - 1つの引き継ぎ先に2つ以上の引き継ぎ元が付いた（別々だった2つの英文が、
//     同じ英文に統一された）。どちらの訳を残すか決められないので取り下げる。
//   - 引き継ぎ先のキーに、このロケールが既に訳を公開している。移すと上書きになる。
//
// どれも「間違った候補を出すくらいなら黙る」側に倒している。翻訳者は候補を信じて
// 訳を別の行へ移すので、取り違えは訳を失うより悪い結果になる。
// 第2戻り値は、旧版として読んだものの「台詞ID→キー」の表が、いまの版のそれと
// 完全に一致するかどうか。旧再生順が本当に更新前のものかを疑う材料に使う。
func carryoverCandidates(idx *orderIndex, old *order.Data, mine map[string]struct{}) (map[string]carryTarget, bool) {
	if old == nil || len(idx.first) == 0 {
		return nil, false
	}
	oldKeys := keysByLineID(old.Entries)
	newKeys := keysByLineID(idx.data.Entries)

	// 旧キー → 引き継ぎ先の新キーの対応を、両方向とも「集合」として先に作りきる。
	//
	// 片方向だけを1つずつ確定させながら進めてはいけない。ある旧キーに2つの
	// 引き継ぎ先が付いて取り下げになる場合、先に入れた1つがそのまま残っていると、
	// 「1つの引き継ぎ先に2つの引き継ぎ元」の判定がマップの反復順に左右される。
	// 実測では、同じ入力で候補が出たり出なかったりする（2000回で12.5%）。
	// 旧版として読んだものが、いまの版と見分けがつかないかどうか。
	same := sameLineIDKeys(oldKeys, newKeys)

	// 旧版に載っていたキー。引き継ぎ先が「今回はじめて現れたキー」であることを
	// 確かめるのに使う。
	wasThere := make(map[string]struct{}, len(old.Entries))
	for _, e := range old.Entries {
		if e.Key != "" {
			wasThere[e.Key] = struct{}{}
		}
	}

	targets := make(map[string]map[string]struct{})
	sources := make(map[string]map[string]struct{})
	for id, from := range oldKeys {
		to, ok := newKeys[id]
		if !ok || from == to {
			continue
		}

		if targets[from] == nil {
			targets[from] = make(map[string]struct{})
		}
		targets[from][to] = struct{}{}
		if sources[to] == nil {
			sources[to] = make(map[string]struct{})
		}
		sources[to][from] = struct{}{}
	}

	dropped := make(map[string]struct{})
	for from, tos := range targets {
		// 同じ英文の2つの行が、別々の英文に書き換えられた。どちらへ移すか
		// 決められない。
		if len(tos) > 1 {
			dropped[from] = struct{}{}
		}
	}
	for _, froms := range sources {
		// 別々だった2つの英文が、同じ英文に統一された。どちらの訳を残すか
		// 決められない。取り下げるのは集まった全部で、そこに取り下げ済みの
		// 旧キーが混じっていても判定は変わらない。
		if len(froms) < 2 {
			continue
		}
		for from := range froms {
			dropped[from] = struct{}{}
		}
	}

	out := make(map[string]carryTarget, len(targets))
	for from, tos := range targets {
		if _, drop := dropped[from]; drop {
			continue
		}
		var to string
		for k := range tos {
			to = k // 取り下げていないので要素はちょうど1つ。
		}
		section, node, orderText, _, live := idx.position(to)
		if !live {
			// 新しい再生順から取ったキーなので普通はここへ来ない。来るとすれば
			// 呼び出し側が新旧を取り違えたときで、そのときは候補にしない。
			continue
		}
		if _, have := mine[to]; have {
			// このロケールは引き継ぎ先のキーを既に公開している。移すと
			// いまある訳を上書きすることになるので、候補にしない。
			continue
		}
		if _, old := wasThere[to]; old {
			// 引き継ぎ先のキーは旧版にも載っていた。つまりその英文は前から
			// ゲームにあり、今回現れたものではない。この行の英文が「前からある
			// 別の英文」に変わったということなので、旧訳をそこへ持って行くと
			// 別の台詞に別の訳を貼ることになる。公開が遅れているロケールでは
			// 他の言語が既に訳している行でもある。
			continue
		}
		kind := CarryMoved
		livePos := ""
		if e, alive := idx.first[from]; alive {
			// 旧キーがいまの再生順の別の行で生きている。旧行の訳を消すと、
			// 生きている行が英語に戻る。どこで生きているかまで伝える。
			kind = CarryCopied
			livePos = joinNonEmpty(" / ", e.Section, e.Node, e.OrderText)
		}
		out[from] = carryTarget{
			key: to, kind: kind,
			section: section, node: node, orderText: orderText,
			livePos: livePos,
		}
	}
	if len(out) == 0 {
		return nil, same
	}
	return out, same
}

// sameLineIDKeys は2つの「台詞ID→キー」の表が完全に同じかを返す。
func sameLineIDKeys(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for id, key := range a {
		if b[id] != key {
			return false
		}
	}
	return true
}

// keysByLineID は台詞IDからキーを引く表を作る。
//
// 同じ台詞IDに違うキーが並んでいたら、その台詞IDは使わない。実データでは
// 1839行とも台詞IDがユニークだが、手編集やマージでは壊れうる。壊れた行を使うと、
// 無関係な2つの台詞を結びつけた候補が出る。
func keysByLineID(entries []order.Entry) map[string]string {
	out := make(map[string]string, len(entries))
	broken := make(map[string]struct{})
	for _, e := range entries {
		if e.LineID == "" || e.Key == "" {
			continue
		}
		if prev, seen := out[e.LineID]; seen {
			if prev != e.Key {
				broken[e.LineID] = struct{}{}
			}
			continue
		}
		out[e.LineID] = e.Key
	}
	for id := range broken {
		delete(out, id)
	}
	return out
}
