package edit

import (
	"slices"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/validate"
)

// workingHeader は作業コピー（Translations/_discovered/<ロケール>.working.csv）の
// ヘッダー。src/DragNWashLocalization/WorkingCopy.cs が書く7列で、公開ファイルの
// 6列に source_en を1つ挟んだ形。最終列はやはり translation。
//
// 形式検証（internal/validate）はこのヘッダーを受理しない。作業コピーは
// コミットしないファイルであり、公開ファイルに source_en が混ざっていたら
// それは事故だからである。編集モデルは作業コピーこそ主な編集対象なので、
// ここでだけ受理する。
var workingHeader = []string{"key", "section", "node", "order", "speaker", "source_en", "translation"}

// AcceptedHeaders は編集モデルが受理するヘッダーを複製して返す。
//
// validate.AcceptedHeaders() の3種（6列 / 3列 / 2列）に、作業コピーの7列を
// 加えたもの。並びは公開ファイルの現行形が先頭、作業コピーが最後。
// どの形でも最終列は translation で、これが「最終フィールドだけを差し替える」
// 保存が成り立つ根拠になる。
//
// どれとも一致しないヘッダーのファイルは、ファイル全体を読み取り専用として
// 開く（[File.ReadOnly]）。
func AcceptedHeaders() [][]string {
	accepted := validate.AcceptedHeaders()
	return append(accepted, slices.Clone(workingHeader))
}

// matchHeader はヘッダー行の生テキスト（改行を除いたもの）を受理される4種と
// 照合し、一致したものを返す。一致しなければ nil。
//
// 照合はCSVとして解釈したあとの値ではなく、行の生テキストの完全一致で行う。
// 引用符もエスケープも認めない。理由は2つある。
//
//   - internal/validate（Python の csv.reader）と、ここで使う
//     csvfile.ParsePowerShellRecord（ConvertFrom-Csv）とで、ヘッダー行の解釈が
//     食い違うため。後者は引用符なしフィールドの先頭空白を落とし、末尾の空
//     フィールドを捨てるので、" key,translation" や
//     "key,section,node,order,speaker,translation," を受理してしまう。
//     validate はどちらも落とす。緩い側にそろえると「編集では通るのに
//     コミット前の検証で落ちるファイル」が生まれる。
//   - 生テキストの一致にしておけば、ヘッダー行の書き方は1通りに定まる。
//     `"key",translation` のように validate だけが通す書き方は、こちらでは
//     読み取り専用になる。厳しい方向にだけずれる。
//
// 実データの公開ファイルも WorkingCopy.cs が書く作業コピーも、ヘッダーは
// 引用符なしの素の並びなので、この照合で落ちるものは無い。
func matchHeader(body string) []string {
	for _, header := range AcceptedHeaders() {
		if body == strings.Join(header, ",") {
			return header
		}
	}
	return nil
}
