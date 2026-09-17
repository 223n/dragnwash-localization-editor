package publish

import (
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// lossHeadRunes は報告に出す訳の文字数。
//
// 訳を丸ごと並べない理由は2つある。1つは量で、実測（この開発機）では、実機の
// ja.working.csv の先頭400行だけを入力にすると1,367件が失われる。全文を並べると
// 端末が訳で埋まり、どのロケールで何が起きたのかが流れて消える。もう1つは、
// この報告が不具合の報せとして貼られる先が手元とはかぎらないことで、
// そこへ訳が丸ごと出ていくのは、--verbose が原文と訳を記録しないと決めたことと
// 食い違う。行を見分けられるだけの長さがあればよい。
const lossHeadRunes = 12

// lossHeadEllipsis は、先頭だけを出したことの印。
const lossHeadEllipsis = "…"

// Loss は、書き出すと失われる訳1件。
//
// 「失われる」は次のどちらかである。どちらも、いまの公開ファイルに訳が入って
// いることが前提で、訳の中身が変わっただけのものは入らない。
//
//   - その行のキーが新しい出力に無い
//   - その行のキーはあるが、新しい出力では訳が空になっている
type Loss struct {
	// Locale はロケール名。--path で走らせたときは空になる（ロケールを決められない）。
	Locale string
	// Key はいまの公開ファイルの行から決めたキー。キーを決められなかった行では、
	// key 列の値をトリムしたものがそのまま入る。
	Key string
	// Line はいまの公開ファイルでの1始まりの物理行番号。
	Line int
	// Head はいまの訳の先頭だけ（[lossHeadRunes] 文字）。丸ごとは持たない。
	Head string
	// Why は失われる理由。
	Why reason.Reason
}

// CheckLoss は、いまの公開ファイル current にある訳が、書き出そうとしている
// next に残るかを1行ずつ確かめ、残らないものを返す。
//
// current は [BuildTarget] の出力先にいまあるバイト列、next は組み立てた結果。
// 返りが空なら、訳は1つも失われない。
//
// 読み方は publish が入力を読むときと同じ（[csvfile.ReadPowerShellRowsNumbered]）で、
// キーの決め方も同じ（[rowKey]）である。守りが見ているものと publish が書くものが
// 別の読み方で決まっていると、守りを通ったのに消える、という形になる。
//
// 誤りを返すのは current か next のヘッダー列名が重複しているときだけである。
// そのときは「失われないこと」を確かめられていないので、呼び出し側は書かないこと。
func CheckLoss(locale string, current, next []byte) ([]Loss, error) {
	rows, err := csvfile.ReadPowerShellRowsNumbered(current)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		// まだ何も入っていないファイル。失うものが無い。
		return nil, nil
	}
	kept, err := survivors(next)
	if err != nil {
		return nil, err
	}

	var losses []Loss
	// 同じキーの行が2行あっても報告は1件にする。publish は2行目以降を捨てる
	// （先勝ち、R18）ので、2行が別々に失われるわけではない。
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		tr := row.Get(colTranslation)
		if tr == "" {
			continue // 訳が入っていない行。失うものが無い
		}

		k, how := rowKey(row.Row)
		if how == keyDropped {
			// キーを決められない行。publish はこの行を捨てるので、訳は消える。
			// 報告に出す名前だけは、その行の key 列から採る。
			k = strings.TrimSpace(row.Get(colKey))
		}
		folded := csvfile.FoldASCII(k)
		if k != "" {
			if _, dup := seen[folded]; dup {
				continue
			}
			seen[folded] = struct{}{}
		}

		survivor, found := kept[folded]
		switch {
		case how == keyDropped || !found:
			losses = append(losses, Loss{Locale: locale, Key: k, Line: row.Line,
				Head: head(tr), Why: reason.New(reason.PublishRowGone,
					"この行が新しい出力に無い")})
		case survivor == "":
			// いまの [Build] は訳が空の行を書かない（R20）ので、この枝は実際には
			// 立たない。それでも見ているのは、守りの条件を「キーがあるかどうか」
			// だけに狭めないためである。出力の作り方が変わって空の訳を書くように
			// なったとき、狭い守りは黙って通す。
			losses = append(losses, Loss{Locale: locale, Key: k, Line: row.Line,
				Head: head(tr), Why: reason.New(reason.PublishTranslationCleared,
					"新しい出力ではこの行の訳が空になる")})
		}
	}
	return losses, nil
}

// CheckTargetLoss は [BuildTarget] が組み立てた out を t.Output へ書いたときに
// 失われる訳を返す。
//
// 出力先のファイルがまだ無ければ、失うものが無いので何も返さない。
func CheckTargetLoss(t Target, out []byte) ([]Loss, error) {
	current, err := readIfExists(t.Output)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, nil
	}
	return CheckLoss(t.Locale, current, out)
}

// survivors は書き出そうとしている中身から「キー→訳」を作る。
//
// 引き当ては [csvfile.FoldASCII] で畳んで行う。ハッシュキーは常に小文字なので
// 畳んでも変わらないが、台詞IDは lineTable が大文字小文字を区別せずに引くので、
// ここだけ区別すると、綴りの大小が違う台詞ID行を「失われた」と誤って報せる。
func survivors(next []byte) (map[string]string, error) {
	rows, err := csvfile.ReadPowerShellRows(next)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		k := strings.TrimSpace(row.Get(colKey))
		if k == "" {
			continue
		}
		folded := csvfile.FoldASCII(k)
		if _, dup := out[folded]; dup {
			continue // 先勝ち（R18）
		}
		out[folded] = row.Get(colTranslation)
	}
	return out, nil
}

// head は訳の先頭だけを返す。長さは [lossHeadRunes] 文字。
//
// 文字数はルーンで数える。バイトで切ると日本語の訳が途中で割れて、壊れた
// UTF-8 が端末へ出る。
func head(s string) string {
	runes := []rune(s)
	if len(runes) <= lossHeadRunes {
		return s
	}
	return string(runes[:lossHeadRunes]) + lossHeadEllipsis
}
