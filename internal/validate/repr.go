package validate

import (
	"strconv"
	"strings"
	"unicode"
)

// pythonRepr は文字列スライスを Python の repr(list[str]) と同じ形に組み立てる。
//
// ヘッダー不正のメッセージは元実装が `{header!r}` を埋め込むので、
// 文言をバイト単位で合わせるにはこれが要る（移植仕様「形式検証 R13 / R24」）。
// 例: ["key", "source_en", "translation"] → `['key', 'source_en', 'translation']`。
// 空のスライスは `[]`。
//
// 文言を合わせにいくのは、CIのログの見た目を元実装から変えないため。
// 移行の途中で両方が走っても差分に見えないようにしておきたい。
func pythonRepr(list []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range list {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(pythonReprString(s))
	}
	b.WriteByte(']')
	return b.String()
}

// pythonReprString は文字列1つを Python の repr(str) と同じ形にする。
//
// Python の規則:
//   - 囲みは通常シングルクォート。値に ' を含み " を含まないときだけダブルクォート。
//   - バックスラッシュと囲み文字はバックスラッシュでエスケープする。
//   - \n \r \t だけ名前つきのエスケープ。\a や \v は \x07 \x0b になる。
//   - str.isprintable() が偽の文字は \xNN（<0x100）/ \uXXXX（<0x10000）/
//     \UXXXXXXXX に置き換える。16進は小文字。
//   - それ以外の非ASCIIはそのまま出す（Python 3 の repr は ASCII 化しない）。
//
// str.isprintable() は「Unicode の Other（C*）と Separator（Z*）に属する文字は
// 印字可能でない。ただし ASCII のスペース U+0020 は印字可能」と定義される。
// Go の unicode.IsPrint はちょうどその補集合（L, M, N, P, S とASCIIスペース）を
// 真とするので、そのまま使える。
//
// 残る差は2つ。どちらも実用上はヘッダー列名に現れない。
//   - Unicode の版が Python と Go で違えば、未割り当て符号位置の扱いがずれうる。
//   - 不正なUTF-8バイトは U+FFFD として1文字ぶん出す。元実装はそもそもファイルを
//     読む時点で落ちるので比較対象が無い。
func pythonReprString(s string) string {
	quote := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}

	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case unicode.IsPrint(r):
			b.WriteRune(r)
		default:
			b.WriteString(pythonHexEscape(r))
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// pythonHexEscape は印字できない1文字を Python のエスケープ表記にする。
// 桁数は符号位置の大きさで決まり、16進は小文字。
func pythonHexEscape(r rune) string {
	switch {
	case r < 0x100:
		return `\x` + pad(r, 2)
	case r < 0x10000:
		return `\u` + pad(r, 4)
	default:
		return `\U` + pad(r, 8)
	}
}

// pad は符号位置を小文字16進の固定桁で返す。
func pad(r rune, width int) string {
	h := strconv.FormatInt(int64(r), 16)
	if len(h) >= width {
		return h
	}
	return strings.Repeat("0", width-len(h)) + h
}
