package csvfile

import (
	"bytes"
	"strings"
)

// utf8BOM は UTF-8 のバイト順マーク（U+FEFF）。
// Go のソースに BOM をそのまま書くとコンパイルエラーになるのでバイトで書く。
const utf8BOM = "\xef\xbb\xbf"

// TrimBOM は先頭の UTF-8 BOM を取り除く。
//
// 元実装はどの経路でも BOM を自動で剥がす。PowerShell 側は
// File.ReadAllLines(path, Encoding.UTF8)（detectEncodingFromByteOrderMarks が
// 有効）、C# 側は new StreamReader(fs, Encoding.UTF8, true)、Python 側は
// encoding="utf-8-sig"。つまり「入力にBOMがあってもなくても同じ結果」が仕様
// （移植仕様「実データの形式 R2b」）。
//
// data/level_flow.csv は実際にBOM付きなので、剥がさないと1列目のヘッダー名の
// 先頭に BOM が残り、その列だけ引けなくなる。さらに先頭行が '#' コメントや
// 空行のときは、BOM が残るとコメント判定・空行判定の「バッファが空」条件まで
// 壊れる（移植仕様「抽出が取りこぼしていた規則」）。
//
// 剥がすのは先頭の1個だけ。行途中や2個目の BOM はただの文字として残す。
// UTF-16/UTF-32 の BOM 検出（.NET の StreamReader は行う）は再現しない。
// 実運用のファイルはすべて UTF-8 で、UTF-16 の入力は想定しない。
func TrimBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte(utf8BOM))
}

// TrimBOMString は [TrimBOM] の文字列版。
func TrimBOMString(s string) string {
	return strings.TrimPrefix(s, utf8BOM)
}

// SplitNetLines は .NET の File.ReadAllLines と同じ規則で物理行に分ける。
//
// 終端は "\r\n" / "\n" / "\r" の3種で、返す行に終端文字は含まない。
// 末尾が終端で終わるファイルでも、余分な空行は作らない。空文字は0行。
//
// tools/hash-strings.ps1 の Read-Csv（53行目）と、出力ファイルからヘッダー直下の
// コメントを読み写す処理（140行目）が、どちらもこの分け方を前提にしている。
func SplitNetLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\n' && c != '\r' {
			continue
		}
		lines = append(lines, s[start:i])
		if c == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			i++
		}
		start = i + 1
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// FoldASCII は大文字小文字を無視する照合のための正規化を行う。
// ASCII の小文字だけを大文字に変え、それ以外の文字はそのまま返す。
//
// 元実装の照合はどれも .NET 由来で、C# の CsvReader は
// Dictionary(StringComparer.OrdinalIgnoreCase)、PowerShell の
// $r.PSObject.Properties['key'] や [ordered]@{} は OrdinalIgnoreCase。
// これらは「序数＋インバリアント大文字化」であり、Go の strings.EqualFold
// （Unicode simple folding）とは一部の文字で結果が食い違う。たとえば
// U+212A（KELVIN SIGN）を .NET は 'K' と等しくないと判定するが、EqualFold は
// 等しいと判定する（移植仕様「敵対検証」[low] R13）。
//
// 実ファイルのヘッダー名はすべて ASCII なので、ASCII 限定の大文字化で
// 正規化する。非 ASCII の列名は「大文字小文字を区別する」挙動になるが、
// .NET のインバリアント大文字化に寄せるより誤りが少ない。
func FoldASCII(s string) string {
	// 変換が要る文字があるかを先に見る。多くの列名はすでに小文字のみなので、
	// ここで返せれば割り当てが起きない。
	needs := false
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'a' && c <= 'z' {
			needs = true
			break
		}
	}
	if !needs {
		return s
	}
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		if c := b[i]; c >= 'a' && c <= 'z' {
			b[i] = c - ('a' - 'A')
		}
	}
	return string(b)
}
