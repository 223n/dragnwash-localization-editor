package publish

import (
	"strconv"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// baseDriftListMax は [CheckBase] が1ロケールにつき何件まで持ち帰るか。
//
// 数え上げは全件行うが、持ち帰るのは見本だけにする。土台がずれているときは
// たいてい版まるごとがずれていて、件数が数百に達する。呼び出し側が全件を
// 受け取っても、端末へ並べれば「なぜ止めたか」が流れて消える。
const baseDriftListMax = 5

// driftLeadRunes は、食い違う位置の手前に何文字残すか（[driftHeads]）。
const driftLeadRunes = 4

// BaseDrift は、ゲーム側の公開ファイルとコミット済みの公開ファイルで
// 訳が食い違っているキー1件。
type BaseDrift struct {
	// Locale はロケール名。
	Locale string
	// Key は食い違っているキー。コミット済みのファイルにあった綴りのまま。
	//
	// 畳んだ形（[csvfile.FoldASCII]）では返さない。人はこの値をファイルから
	// 探すので、綴りが変わっていると引き当てられない。
	Key string
	// Repo はコミット済みの訳の先頭だけ（[lossHeadRunes] 文字）。
	Repo string
	// Game はゲームに入っている訳の先頭だけ。
	Game string
}

// BaseResult は [CheckBase] の結果。
type BaseResult struct {
	// Locale はロケール名。
	Locale string
	// Count は食い違っているキーの総数。
	Count int
	// Sample は見本。多くても [baseDriftListMax] 件。
	Sample []BaseDrift
}

// CheckBase は、ゲーム側の作業コピーが建っている土台がコミット済みと
// そろっているかを確かめる。
//
// 確かめるのは t.GameBase が埋まっているとき、つまり入力がゲーム側の作業コピーに
// なったときだけである。リポジトリの作業コピーや公開ファイル自身を入力にした
// ロケールには、見比べる土台が無い。
//
// なぜ要るか。Modは「いま読み込んでいる訳」を作業コピーへ書き出す。ゲームに
// 入っている公開ファイルが古ければ、作業コピーの訳も古い。それを入力にすると、
// 新しいコミットが古い版へ静かに巻き戻る。[CheckLoss] はこれを捕まえられない。
// 訳は消えておらず、書き換わっただけだからである。
//
// では「書き換わったら止める」にすればよいかというと、それはできない。訳を
// 書き換えることこそ翻訳者の仕事で、止めれば publish が使えなくなる。
// 編集と巻き戻りを見分けるには3点目が要る。それがこの土台である。
//
//   - コミット済み == ゲーム側 … 作業コピーの違いは翻訳者が入れたもの
//   - コミット済み != ゲーム側 … 作業コピーは別の土台の上に建っている
//
// 見るのは訳だけである。section や order の食い違いは見ない。ゲーム側の
// 再生順はリポジトリのものを使うので、そこがずれていても書き出す中身は変わらない。
//
// ゲーム側に公開ファイルが無いときは、そろっているともいないとも言えないので
// 何も返さない。そのロケールで訳が実際に消えるなら [CheckLoss] が捕まえる。
func CheckBase(t Target, repoCurrent []byte) (BaseResult, error) {
	res := BaseResult{Locale: t.Locale}
	if t.GameBase == "" {
		return res, nil
	}
	gameBytes, err := readIfExists(t.GameBase)
	if err != nil {
		return res, err
	}
	if gameBytes == nil {
		return res, nil
	}

	repo, err := translationsByKey(repoCurrent)
	if err != nil {
		return res, err
	}
	game, err := translationsByKey(gameBytes)
	if err != nil {
		return res, err
	}

	// 片方にしか無いキーは数えない。ゲームの版が古ければ、行そのものの
	// 増減は当たり前に起きる。ここで見たいのは「同じ行の訳が違う」ことだけで、
	// 行の増減で実際に訳が消えるなら [CheckLoss] が捕まえる。
	for folded, mine := range repo {
		theirs, both := game[folded]
		if !both || mine.text == theirs.text {
			continue
		}
		res.Count++
		if len(res.Sample) < baseDriftListMax {
			repoHead, gameHead := driftHeads(mine.text, theirs.text)
			res.Sample = append(res.Sample, BaseDrift{
				Locale: t.Locale, Key: mine.key,
				Repo: repoHead, Game: gameHead,
			})
		}
	}
	return res, nil
}

// driftHeads は、2つの訳の「違っているところが見える」切り出しを返す。
//
// 先頭から [lossHeadRunes] 文字を出すだけでは足りない。実測（実機のゲーム
// フォルダー）で出た3件は、3件とも先頭12文字が同じで、並べても同じ文字列が
// 2行出るだけだった。違いを見せるための報告なのに、違いが切り落とされていた。
//
// そこで、最初に食い違う位置が窓に入るように窓をずらす。前を削ったときは
// 頭にも省略の印を付ける。どちらかが他方の先頭部分そのものというときは
// （閉じタグの付け外しなど）、短いほうの終わりが食い違う位置になる。
func driftHeads(repo, game string) (string, string) {
	a, b := []rune(repo), []rune(game)
	at := 0
	for at < len(a) && at < len(b) && a[at] == b[at] {
		at++
	}
	// 違いの手前を少しだけ残す。どこが変わったのかは、前後があるほうが読める。
	start := 0
	if at >= lossHeadRunes {
		start = at - driftLeadRunes
	}
	return window(a, start), window(b, start)
}

// window は runes の start から [lossHeadRunes] 文字を切り出し、
// 前後を削ったぶんだけ省略の印を付ける。
func window(runes []rune, start int) string {
	if start > len(runes) {
		start = len(runes)
	}
	end := start + lossHeadRunes
	cut := end < len(runes)
	if cut {
		runes = runes[:end]
	}
	out := string(runes[start:])
	if start > 0 {
		out = lossHeadEllipsis + out
	}
	if cut {
		out += lossHeadEllipsis
	}
	return out
}

// BaseReason は土台がずれていることの理由。置換は locale と count。
func BaseReason(locale string, count int) reason.Reason {
	n := strconv.Itoa(count)
	return reason.New(reason.PublishBaseDrift,
		"ゲームに入っている訳が "+locale+" で "+n+" 件ちがう",
		"locale", locale, "count", n)
}

// translationsByKey は公開ファイルから「畳んだキー→（元の綴り, 訳）」を作る。
//
// 引き当ては [survivors] と同じく [csvfile.FoldASCII] で畳む。訳の入っていない
// 行は入れない。土台がそろっているかは、訳のある行だけで決まる。
func translationsByKey(data []byte) (map[string]keyedText, error) {
	rows, err := csvfile.ReadPowerShellRows(data)
	if err != nil {
		return nil, err
	}
	out := make(map[string]keyedText, len(rows))
	for _, row := range rows {
		k := strings.TrimSpace(row.Get(colKey))
		if k == "" {
			continue
		}
		tr := row.Get(colTranslation)
		if tr == "" {
			continue
		}
		folded := csvfile.FoldASCII(k)
		if _, dup := out[folded]; dup {
			continue // 先勝ち（R18）
		}
		out[folded] = keyedText{key: k, text: tr}
	}
	return out, nil
}

// keyedText は畳む前のキーの綴りと、その行の訳。
type keyedText struct {
	key  string
	text string
}
