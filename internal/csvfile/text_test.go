package csvfile

import (
	"slices"
	"testing"
)

// bom はテストで使う UTF-8 BOM。Go のソースに BOM をそのまま書くと
// コンパイルエラーになるのでバイトで書く。
const bom = "\xef\xbb\xbf"

func TestTrimBOM(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"BOMなし", "flow_asset,level", "flow_asset,level"},
		{"BOMあり", bom + "flow_asset,level", "flow_asset,level"},
		{"剥がすのは先頭の1個だけ", bom + bom + "a", bom + "a"},
		{"行途中のBOMは残す", "a," + bom + "b", "a," + bom + "b"},
		{"空文字", "", ""},
		{"BOMだけ", bom, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TrimBOMString(tt.in); got != tt.want {
				t.Errorf("TrimBOMString(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if got := string(TrimBOM([]byte(tt.in))); got != tt.want {
				t.Errorf("TrimBOM(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSplitNetLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"空文字は0行", "", nil},
		{"改行なし", "a", []string{"a"}},
		{"LF", "a\nb\n", []string{"a", "b"}},
		{"末尾に改行が無い", "a\nb", []string{"a", "b"}},
		{"CRLF", "a\r\nb\r\n", []string{"a", "b"}},
		{"CR単独も終端", "a\rb\r", []string{"a", "b"}},
		{"混在", "a\r\nb\nc\rd", []string{"a", "b", "c", "d"}},
		{"空行は空文字の行として残る", "a\n\nb\n", []string{"a", "", "b"}},
		{"改行だけ", "\n", []string{""}},
		{"末尾の改行で余分な空行は作らない", "a\n", []string{"a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitNetLines(tt.in)
			if !slices.Equal(got, tt.want) {
				t.Errorf("SplitNetLines(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFoldASCII(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"小文字", "key", "KEY"},
		{"大文字混じり", "Source_EN", "SOURCE_EN"},
		{"変換不要", "KEY_1", "KEY_1"},
		{"空文字", "", ""},
		{"非ASCIIはそのまま", "訳文", "訳文"},
		// .NET の OrdinalIgnoreCase は U+212A（KELVIN SIGN）を 'K' と別物とみなす。
		// ASCII 限定の大文字化にしているので、ここでも別物になる。
		{"ケルビン記号は大文字化しない", "K", "K"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FoldASCII(tt.in); got != tt.want {
				t.Errorf("FoldASCII(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
