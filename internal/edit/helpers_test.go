package edit

import (
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
)

// splitTerminator は物理行を本体と行末の改行に分ける。試験で、書き出したバイト列を
// 編集モデルの内部を使わずに物理行へ分け直して調べるときに使う。
// csvfile.SplitPythonLines が返す行は "\r\n" / "\n" / "\r" のいずれかで終わるか、
// 末尾行なら終端を持たない。
func splitTerminator(text string) (body, term string) {
	switch {
	case strings.HasSuffix(text, "\r\n"):
		return text[:len(text)-2], "\r\n"
	case strings.HasSuffix(text, "\n"), strings.HasSuffix(text, "\r"):
		return text[:len(text)-1], text[len(text)-1:]
	}
	return text, ""
}

// isRecord は、1物理行（改行を除いたもの）がレコードとして読まれるかを返す。
// 空行相当の判定は csvfile.ParsePowerShellRecord の第2戻り値をそのまま使う。
func isRecord(body string) bool {
	_, ok := csvfile.ParsePowerShellRecord(body)
	return ok
}
