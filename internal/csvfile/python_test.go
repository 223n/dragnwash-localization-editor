package csvfile

import (
	"slices"
	"testing"
)

func TestSplitPythonLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Line
	}{
		{name: "空ファイル", in: "", want: nil},
		{
			name: "LF。終端は行に残る",
			in:   "a\nb\n",
			want: []Line{{1, "a\n"}, {2, "b\n"}},
		},
		{
			name: "末尾に改行が無い",
			in:   "a\nb",
			want: []Line{{1, "a\n"}, {2, "b"}},
		},
		{
			name: "CRLFはCRLFのまま残る",
			in:   "a\r\nb\r\n",
			want: []Line{{1, "a\r\n"}, {2, "b\r\n"}},
		},
		{
			name: "CR単独も終端",
			in:   "a\rb\r",
			want: []Line{{1, "a\r"}, {2, "b\r"}},
		},
		{
			name: "空行も1行として数える",
			in:   "a\n\nb\n",
			want: []Line{{1, "a\n"}, {2, "\n"}, {3, "b\n"}},
		},
		{
			name: "BOMは剥がす",
			in:   bom + "key,translation\n",
			want: []Line{{1, "key,translation\n"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitPythonLines([]byte(tt.in))
			if !slices.Equal(got, tt.want) {
				t.Errorf("SplitPythonLines(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestKeepContentLines(t *testing.T) {
	text := "key,translation\n" + // 1
		"\n" + // 2
		"# 見出し\n" + // 3
		"   \n" + // 4
		" # 先頭に空白があるのでコメントではない\n" + // 5
		"\r\n" + // 6
		"a,b\n" + // 7
		"\x1c\n" + // 8 Python の str.isspace() は U+001C を空白とみなす
		"c,d" // 9

	kept := KeepContentLines(SplitPythonLines([]byte(text)))
	wantNumbers := []int{1, 5, 7, 9}
	var gotNumbers []int
	for _, line := range kept {
		gotNumbers = append(gotNumbers, line.Number)
	}
	if !slices.Equal(gotNumbers, wantNumbers) {
		t.Errorf("残った行番号 = %v, want %v", gotNumbers, wantNumbers)
	}
	if len(kept) > 0 && kept[0].Text != "key,translation\n" {
		t.Errorf("行の本文 = %q。終端の改行を落としてはいけない", kept[0].Text)
	}
}

func TestParsePythonRecords(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  [][]string
	}{
		{
			name:  "通常の行",
			lines: []string{"key,translation\n", "a,b\n"},
			want:  [][]string{{"key", "translation"}, {"a", "b"}},
		},
		{
			name:  "末尾に改行が無い",
			lines: []string{"key,translation\n", "a,b"},
			want:  [][]string{{"key", "translation"}, {"a", "b"}},
		},
		{
			name:  "CRLF",
			lines: []string{"key,translation\r\n", "a,b\r\n"},
			want:  [][]string{{"key", "translation"}, {"a", "b"}},
		},
		{
			// 引用符で囲まないフィールド中の裸の二重引用符。元実装は黙って受理する。
			// Go の encoding/csv は LazyQuotes を立てないとここでパースを打ち切り、
			// 以降の行が一切検査されなくなる。
			name:  "裸の二重引用符",
			lines: []string{"0123456789abcdef,he said \"hi\"\n"},
			want:  [][]string{{"0123456789abcdef", `he said "hi"`}},
		},
		{
			// 閉じ引用符の直後に文字が続く形。Python は xy" になる
			// （LazyQuotes=true の encoding/csv は x"y になり一致しない）。
			name:  "閉じ引用符の後ろの文字",
			lines: []string{"a,\"x\"y\"\n"},
			want:  [][]string{{"a", `xy"`}},
		},
		{
			name:  "引用の中のカンマ",
			lines: []string{"a,\"b,c\",d\n"},
			want:  [][]string{{"a", "b,c", "d"}},
		},
		{
			name:  "引用の中の二重引用符",
			lines: []string{"a,\"b\"\"c\"\n"},
			want:  [][]string{{"a", `b"c`}},
		},
		{
			name:  "引用の中のシャープ",
			lines: []string{"a,\"#b\"\n"},
			want:  [][]string{{"a", "#b"}},
		},
		{
			name:  "引用フィールドが複数行にまたがる",
			lines: []string{"abc,\"multi\n", "line\"\n", "def,x\n"},
			want:  [][]string{{"abc", "multi\nline"}, {"def", "x"}},
		},
		{
			// 元実装は newline="" なので CRLF が値に残る。
			// Go の encoding/csv はここを "\n" に潰すため一致しない。
			name:  "引用フィールド内のCRLFは残る",
			lines: []string{"abc,\"multi\r\n", "line\"\r\n"},
			want:  [][]string{{"abc", "multi\r\nline"}},
		},
		{
			name:  "空行は0フィールドのレコード",
			lines: []string{"a,b\n", "\n", "c,d\n"},
			want:  [][]string{{"a", "b"}, {}, {"c", "d"}},
		},
		{
			name:  "列数はレコードごとに変わってよい",
			lines: []string{"a,b,c\n", "1\n", "1,2,3,4\n"},
			want:  [][]string{{"a", "b", "c"}, {"1"}, {"1", "2", "3", "4"}},
		},
		{
			name:  "末尾がカンマ",
			lines: []string{"a,\n"},
			want:  [][]string{{"a", ""}},
		},
		{
			name:  "カンマだけの行",
			lines: []string{",\n"},
			want:  [][]string{{"", ""}},
		},
		{
			name:  "空の引用フィールドだけの行は1フィールドのレコード",
			lines: []string{"\"\"\n"},
			want:  [][]string{{""}},
		},
		{
			name:  "閉じない引用符のままファイルが終わる",
			lines: []string{"a,\"x\n"},
			want:  [][]string{{"a", "x\n"}},
		},
		{
			name:  "入力が0行ならレコードも0件",
			lines: nil,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := ParsePythonRecords(numberedLines(tt.lines))
			got := make([][]string, 0, len(records))
			for _, r := range records {
				got = append(got, r.Fields)
			}
			if !equalRecords(got, tt.want) {
				t.Errorf("ParsePythonRecords(%q)\n = %q\nwant %q", tt.lines, got, tt.want)
			}
		})
	}
}

// TestParsePythonRecordsLineNumbers は、報告用の行番号の対応付け（移植仕様 R15）を
// 確かめる。期待値は仕様に載っている元実装での実測値と同じ組み合わせ。
func TestParsePythonRecordsLineNumbers(t *testing.T) {
	// コメント行と空行を落としたあとの kept を模した入力。
	kept := []Line{
		{Number: 1, Text: "key,translation\n"},
		{Number: 4, Text: "abc,\"multi\n"},
		{Number: 5, Text: "line\"\n"},
		{Number: 6, Text: "def,x\n"},
	}

	records := ParsePythonRecords(kept)
	if len(records) != 3 {
		t.Fatalf("レコード数 = %d, want 3", len(records))
	}

	want := []struct {
		fields   []string
		number   int
		index    int
		consumed int
	}{
		{[]string{"key", "translation"}, 1, 0, 1},
		{[]string{"abc", "multi\nline"}, 4, 1, 3},
		{[]string{"def", "x"}, 6, 3, 4},
	}
	for i, w := range want {
		got := records[i]
		if !slices.Equal(got.Fields, w.fields) {
			t.Errorf("records[%d].Fields = %q, want %q", i, got.Fields, w.fields)
		}
		if got.Number != w.number {
			t.Errorf("records[%d].Number = %d, want %d（複数行にまたがる行は先頭行を指す）", i, got.Number, w.number)
		}
		if got.Index != w.index {
			t.Errorf("records[%d].Index = %d, want %d", i, got.Index, w.index)
		}
		if got.Consumed != w.consumed {
			t.Errorf("records[%d].Consumed = %d, want %d", i, got.Consumed, w.consumed)
		}
	}
}

// TestReadPythonLines は、読み込みから行の取捨までを通しで確かめる。
func TestReadPythonLines(t *testing.T) {
	text := bom + "key,translation\n# 見出し\n\n0123456789abcdef,訳\n"
	kept := ReadPythonLines([]byte(text))
	if len(kept) != 2 {
		t.Fatalf("残った行数 = %d, want 2", len(kept))
	}
	if kept[1].Number != 4 {
		t.Errorf("2行目の物理行番号 = %d, want 4", kept[1].Number)
	}

	records := ParsePythonRecords(kept)
	if len(records) != 2 {
		t.Fatalf("レコード数 = %d, want 2", len(records))
	}
	if got, want := records[0].Fields, []string{"key", "translation"}; !slices.Equal(got, want) {
		t.Errorf("ヘッダー = %q, want %q。BOM が剥がれていない可能性がある", got, want)
	}
	if records[1].Number != 4 {
		t.Errorf("データ行の報告用行番号 = %d, want 4", records[1].Number)
	}
}

// numberedLines はテキストの並びを1始まりの行番号つきの [Line] にする。
func numberedLines(texts []string) []Line {
	lines := make([]Line, 0, len(texts))
	for i, text := range texts {
		lines = append(lines, Line{Number: i + 1, Text: text})
	}
	return lines
}
