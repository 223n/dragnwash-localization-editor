package validate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
)

// CreditsFile は、訳の状態と確かめた人を書くファイルの名前。
// Translations/<ロケール>/ の直下に置く。無くてもよい。
const CreditsFile = "credits.txt"

// creditStatuses は credits.txt の最初の行に書ける状態語。
// 並びは上流の CREDIT_STATUSES のままで、報告の文面にもこの順で出る。
// native（ネイティブが訳したパック）は上流の caa470b で supervised の次に入った。
var creditStatuses = []string{"supervised", "native", "proofread", "converted", "provisional", "fun"}

// checkCredits は credits.txt の中身を検査する。上流の check_credits
// （912f519、状態語の native は caa470b）に当たる。
//
// 最初の行が状態語で、そのあとの行に確かめた人の名前を1人1行で書く。見るのは
// 最初の行の状態語だけで、名前の行は見ない。空行と '#' で始まる行（前後の空白を
// 落としてから見る）は飛ばす。問題は多くても1件。
//
// name は報告に出す表示用のパス、data はファイルの生のバイト列。
//
// 行の分け方は Python のテキストモード（universal newlines）に合わせて
// "\r\n" / "\n" / "\r" の3つにする。[csvfile.SplitPythonLines] と同じ規則で、
// 報告の行番号もそこから来る。
func checkCredits(name string, data []byte) []Problem {
	for _, line := range csvfile.SplitPythonLines(data) {
		status := csvfile.TrimPythonSpace(line.Text)
		if status == "" || strings.HasPrefix(status, "#") {
			continue
		}
		if slices.Contains(creditStatuses, asciiLower(status)) {
			return nil
		}
		// 書かれた値をそのまま出す。strings.csv と違い、ここに入るのは台本ではなく
		// 状態語なので、公開のログに出してよい。上流も repr ではなく生の値を
		// 二重引用符で囲んで出す。
		return []Problem{{
			Path: name, Line: line.Number,
			Message: fmt.Sprintf(`"%s" is not a status; use one of %s`, status, strings.Join(creditStatuses, ", ")),
		}}
	}
	return []Problem{{
		Path:    name,
		Message: "empty; the first line is the status (" + strings.Join(creditStatuses, ", ") + ")",
	}}
}

// asciiLower は ASCII の大文字だけを小文字にする。
//
// 上流は Python の str.lower() で比べる。strings.ToLower を使わないのは、
// 小文字の対応表が違うため。Python は 'İ'（U+0130）を2文字の "i̇" にするので
// "provİsional" は状態語にならないが、Go の strings.ToLower は 'i' 1文字にして
// 通してしまう。Python の lower() で ASCII になる非 ASCII の文字は
// ケルビン記号（U+212A → 'k'）だけで、状態語にも ".png" にも 'k' は無い。
// したがって ASCII だけを小文字にすれば、比べた結果は Python と一致する。
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
