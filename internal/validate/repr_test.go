package validate

import "testing"

// TestPythonReprString は Python の repr(str) と同じ文字列になることを見る。
// 期待値は元実装と同じ Python で実際に repr を取って確かめたもの。
func TestPythonReprString(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"ふつうの列名", "key", `'key'`},
		{"空文字", "", `''`},
		{"前後の空白は残る", " pad ", `' pad '`},
		{"シングルクォートを含む", "it's", `"it's"`},
		{"ダブルクォートを含む", `say "hi"`, `'say "hi"'`},
		{"両方を含むとシングルを逃がす", `both ' and "`, `'both \' and "'`},
		{"バックスラッシュ", `a\b`, `'a\\b'`},
		{"改行", "a\nb", `'a\nb'`},
		{"復帰", "a\rb", `'a\rb'`},
		{"タブ", "a\tb", `'a\tb'`},
		// \a や \v には名前つきのエスケープが無く、16進になる。
		{"ベル", "\a", `'\x07'`},
		{"垂直タブ", "\v", `'\x0b'`},
		{"DEL", "\x7f", `'\x7f'`},
		{"BOMが列名に残った場合", "\U0000FEFFkey", `'\ufeffkey'`},
		{"ゼロ幅スペース", "\U0000200B", `'\u200b'`},
		{"全角スペース", "\U00003000", `'\u3000'`},
		{"ノーブレークスペース", "\U000000A0", `'\xa0'`},
		// Python 3 の repr は印字可能な非ASCIIをそのまま出す。
		{"絵文字", "\U0001F600", `'😀'`},
		{"日本語", "日本語", `'日本語'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pythonReprString(tt.value); got != tt.want {
				t.Errorf("pythonReprString(%+q) = %s, want %s", tt.value, got, tt.want)
			}
		})
	}
}

// TestPythonRepr はリスト全体の体裁を見る。
func TestPythonRepr(t *testing.T) {
	tests := []struct {
		name string
		list []string
		want string
	}{
		{"空のリスト", nil, `[]`},
		{"1つ", []string{"key"}, `['key']`},
		{"2つ", []string{"key", "translation"}, `['key', 'translation']`},
		{"原文の列が混ざった形", []string{"key", "source_en", "translation"}, `['key', 'source_en', 'translation']`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pythonRepr(tt.list); got != tt.want {
				t.Errorf("pythonRepr(%q) = %s, want %s", tt.list, got, tt.want)
			}
		})
	}
}
