package diff

import (
	"strings"
	"testing"
)

// TestDefuseFormula は、表計算が式と読む値にだけ ' を付けることを見る。
func TestDefuseFormula(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"=1+1", "'=1+1"},
		{"+81", "'+81"},
		{"-では", "'-では"},
		{"@名前", "'@名前"},
		{"\t=1+1", "'\t=1+1"},
		{"\r=1+1", "'\r=1+1"},
		{"", ""},
		{"1+1=2", "1+1=2"},
		{"こんにちは", "こんにちは"},
		// 全角の記号は表計算が式と読まないので変えない。
		{"＝１", "＝１"},
		{"'=既に付いている", "'=既に付いている"},
	}
	for _, tt := range tests {
		if got := defuseFormula(tt.in); got != tt.want {
			t.Errorf("defuseFormula(%q) = %q, 期待 %q", tt.in, got, tt.want)
		}
	}
}

// TestWriteCSVDefusesFormulasAndRawKeepsThem は、WriteCSV が ' を付け、
// WriteCSVRaw が値をそのまま書くことを見る。' は引用の内側に入る。
func TestWriteCSVDefusesFormulasAndRawKeepsThem(t *testing.T) {
	rep := &Report{Findings: []Finding{{
		Locale:      "ja",
		Category:    CatVanished,
		Key:         "0123456789abcdef",
		Translation: `=HYPERLINK("https://example.invalid/","open")`,
		Note:        "-",
	}}}

	var guarded, raw strings.Builder
	if err := rep.WriteCSV(&guarded); err != nil {
		t.Fatal(err)
	}
	if err := rep.WriteCSVRaw(&raw); err != nil {
		t.Fatal(err)
	}

	if want := `,"'=HYPERLINK(""https://example.invalid/"",""open"")",'-` + "\n"; !strings.HasSuffix(guarded.String(), want) {
		t.Errorf("WriteCSV = %q、末尾が %q であることを期待", guarded.String(), want)
	}
	if want := `,"=HYPERLINK(""https://example.invalid/"",""open"")",-` + "\n"; !strings.HasSuffix(raw.String(), want) {
		t.Errorf("WriteCSVRaw = %q、末尾が %q であることを期待", raw.String(), want)
	}
	// 1行目の見出しはどちらも同じ。
	for _, out := range []string{guarded.String(), raw.String()} {
		if !strings.HasPrefix(out, CSVHeader+"\n") {
			t.Errorf("1行目が見出しでない: %q", out)
		}
	}
}
