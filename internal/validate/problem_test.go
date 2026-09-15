package validate

import "testing"

// TestProblemString は1件ぶんの体裁を見る（移植仕様「形式検証 R24」）。
func TestProblemString(t *testing.T) {
	tests := []struct {
		name    string
		problem Problem
		want    string
	}{
		{
			name:    "行番号あり",
			problem: Problem{Path: "Translations/ja/strings.csv", Line: 42, Message: "empty translation"},
			want:    "Translations/ja/strings.csv:42: empty translation",
		},
		{
			name:    "行番号なし",
			problem: Problem{Path: "Translations/ja/strings.csv", Message: "empty file"},
			want:    "Translations/ja/strings.csv: empty file",
		},
		{
			name:    "ディレクトリに対する報告",
			problem: Problem{Path: "Translations/ja", Message: "no strings.csv"},
			want:    "Translations/ja: no strings.csv",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.problem.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestReport は標準出力へ書く内容の体裁を見る（移植仕様「形式検証 R22 / R23」）。
// 1件でも "(s)" が付く。問題の前に空行が1行入る。
func TestReport(t *testing.T) {
	tests := []struct {
		name     string
		problems []Problem
		want     string
	}{
		{
			name: "問題なし",
			want: "translations OK\n",
		},
		{
			name:     "1件",
			problems: []Problem{{Path: "a.csv", Line: 2, Message: "empty translation"}},
			want:     "a.csv:2: empty translation\n\n1 problem(s).\n",
		},
		{
			name: "2件",
			problems: []Problem{
				{Path: "a.csv", Message: "empty file"},
				{Path: "b.csv", Line: 9, Message: "duplicate key (see line 3)"},
			},
			want: "a.csv: empty file\nb.csv:9: duplicate key (see line 3)\n\n2 problem(s).\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Report(tt.problems); got != tt.want {
				t.Errorf("Report() = %q, want %q", got, tt.want)
			}
		})
	}
}
