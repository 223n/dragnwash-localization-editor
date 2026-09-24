package csvfile

import (
	"errors"
	"slices"
	"strings"
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

// TestPythonCommentLines は、上流 c8fda90 の comment_lines と同じ行をコメントと
// 見なすことを確かめる。
func TestPythonCommentLines(t *testing.T) {
	tests := []struct {
		name string
		text string
		// want はコメントと見なす行の番号（1始まり）。
		want []int
	}{
		{
			name: "行頭の'#'だけがコメント",
			text: "key,translation\n# 見出し\n #先頭に空白\na,b\n",
			want: []int{2},
		},
		{
			name: "引用値の途中の'#'行はコメントではない",
			text: "key,translation\na,\"ひとつ\n#ふたつ\"\n# 見出し\n",
			want: []int{4},
		},
		{
			// 偶奇は裸の '"' も数える。後ろの '#' 行はコメントに見えなくなる。
			name: "裸の引用符のあとは'#'行もデータ",
			text: "key,translation\na,5\" 画面\n\n# 見出し\nb,c\n",
			want: nil,
		},
		{
			// コメント行の引用符は数えない。数えると後ろの見出しを見失う。
			name: "コメント行の引用符は偶奇に入らない",
			text: "# メモ,\"開いたまま\na,b\n# 見出し\n",
			want: []int{1, 3},
		},
		{
			name: "空行と空白だけの行はコメントではない",
			text: "\n   \n#\n",
			want: []int{3},
		},
		{
			name: "CRLFとCRでも行ごとに決まる",
			text: "# ひとつ\r\na,\"ふた\r\n#つ\"\r# みっつ\r",
			want: []int{1, 4},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := SplitPythonLines([]byte(tt.text))
			var got []int
			for i, isComment := range pythonCommentLines(lines) {
				if isComment {
					got = append(got, lines[i].Number)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("コメントの行 = %v, want %v", got, tt.want)
			}
		})
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
			// [SplitPythonLines] は終端の無い空文字の行を作らないが、呼び出し側が
			// 組んだ行に混ざっても、前後の行とつながらずに0フィールドのレコードになる。
			// Python の csv.reader(["a\n", "", "b\n"]) も [['a'], [], ['b']] を返す。
			name:  "終端の無い空文字の行も0フィールドのレコード",
			lines: []string{"a\n", "", "b\n"},
			want:  [][]string{{"a"}, {}, {"b"}},
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
			records, err := ParsePythonRecords(numberedLines(tt.lines))
			if err != nil {
				t.Fatalf("ParsePythonRecords が失敗した: %v", err)
			}
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
	// 途中の行を落とした並びを模した入力。行番号は飛んでいてよい。
	kept := []Line{
		{Number: 1, Text: "key,translation\n"},
		{Number: 4, Text: "abc,\"multi\n"},
		{Number: 5, Text: "line\"\n"},
		{Number: 6, Text: "def,x\n"},
	}

	records, err := ParsePythonRecords(kept)
	if err != nil {
		t.Fatalf("ParsePythonRecords が失敗した: %v", err)
	}
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

// TestReadPythonRecords は、公開ファイルの読み込みから、コメントと空行を捨てる
// ところまでを通しで確かめる。
func TestReadPythonRecords(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []struct {
			number int
			fields []string
		}
	}{
		{
			name: "BOMとコメントと空行",
			text: bom + "key,translation\n# 見出し\n\n0123456789abcdef,訳\n",
			want: []struct {
				number int
				fields []string
			}{
				{1, []string{"key", "translation"}},
				{4, []string{"0123456789abcdef", "訳"}},
			},
		},
		{
			// 上流 f816618 から、空白だけの行は1フィールドのレコードとして残る。
			name: "空白だけの行は残る",
			text: "key,translation\n   \n\t\n",
			want: []struct {
				number int
				fields []string
			}{
				{1, []string{"key", "translation"}},
				{2, []string{"   "}},
				{3, []string{"\t"}},
			},
		},
		{
			// 上流 c8fda90 から、引用値の途中の '#' 行と空行は値の一部になる。
			name: "引用値の途中の'#'行と空行は値に残る",
			text: "key,translation\na,\"ひと\n\n#つ\"\nb,c\n",
			want: []struct {
				number int
				fields []string
			}{
				{1, []string{"key", "translation"}},
				{2, []string{"a", "ひと\n\n#つ"}},
				{5, []string{"b", "c"}},
			},
		},
		{
			// 上流はコメント行も csv.reader に通すので、開いたままの引用符が後ろの
			// 行を飲み込む。ゲームと同じくコメント行はパーサーに渡さない。
			name: "コメント行の引用符はレコードを開かない",
			text: "key,translation\n# メモ,\"開いたまま\na,\nb,c\n",
			want: []struct {
				number int
				fields []string
			}{
				{1, []string{"key", "translation"}},
				{3, []string{"a", ""}},
				{4, []string{"b", "c"}},
			},
		},
		{
			// 偶奇の上ではコメントでも、パーサーが前のレコードの引用値を読んでいる
			// 最中なら値の一部として渡す。上流も同じ行を値に入れる。
			name: "引用値の途中に来た偶奇上のコメント行は値に入る",
			text: "key,a,b\nk,x\"y,\"c\n#z\"\nd,e,f\n",
			want: []struct {
				number int
				fields []string
			}{
				{1, []string{"key", "a", "b"}},
				{2, []string{"k", `x"y`, "c\n#z"}},
				{4, []string{"d", "e", "f"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records, err := ReadPythonRecords([]byte(tt.text))
			if err != nil {
				t.Fatalf("ReadPythonRecords が失敗した: %v", err)
			}
			if len(records) != len(tt.want) {
				t.Fatalf("レコード数 = %d, want %d（%+v）", len(records), len(tt.want), records)
			}
			for i, w := range tt.want {
				if records[i].Number != w.number {
					t.Errorf("records[%d].Number = %d, want %d", i, records[i].Number, w.number)
				}
				if !slices.Equal(records[i].Fields, w.fields) {
					t.Errorf("records[%d].Fields = %q, want %q", i, records[i].Fields, w.fields)
				}
			}
		})
	}
}

// TestPythonFieldLimit は、フィールドの長さの上限を Python と同じところに置くことを
// 確かめる。上限は文字数で、131072文字は通り、131073文字で csv.Error になる。
func TestPythonFieldLimit(t *testing.T) {
	long := func(s string, n int) string { return strings.Repeat(s, n) }

	tests := []struct {
		name string
		text string
		// wantLine は 0 なら成功。それ以外はエラーの行番号。
		wantLine int
	}{
		{name: "ちょうど上限", text: "a," + long("x", PythonFieldLimit) + "\n"},
		{name: "上限を1文字超える", text: "k\na," + long("x", PythonFieldLimit+1) + "\n", wantLine: 2},
		{
			// バイト数では上限の3倍でも、文字数で数えるので通る。
			name: "多バイト文字でちょうど上限",
			text: "a," + long("あ", PythonFieldLimit) + "\n",
		},
		{name: "多バイト文字で1文字超える", text: "a," + long("あ", PythonFieldLimit+1) + "\n", wantLine: 1},
		{
			// 引用値の中の改行も1文字に数える。エラーの行はレコードの先頭行ではなく、
			// 超えた文字のある行（Python の reader.line_num）。
			name:     "複数行の引用値は超えた行を指す",
			text:     "k\na,\"b\n" + long("x", PythonFieldLimit-1) + "\nc\"\n",
			wantLine: 3,
		},
		{
			name:     "引用値の中の二重引用符も1文字",
			text:     "a,\"" + long(`""`, PythonFieldLimit+1) + "\"\n",
			wantLine: 1,
		},
		{
			// 上流はコメント行も csv.reader に通すので、長すぎるコメント行でもエラーになる。
			name:     "長すぎるコメント行",
			text:     "k\n# " + long("x", PythonFieldLimit) + "\na\n",
			wantLine: 2,
		},
		{
			name: "カンマで区切られたコメント行は各フィールドが短ければ通る",
			text: "k\n# " + long("x", PythonFieldLimit-2) + "," + long("y", PythonFieldLimit) + "\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadPythonRecords([]byte(tt.text))
			if tt.wantLine == 0 {
				if err != nil {
					t.Fatalf("エラーになった: %v", err)
				}
				return
			}
			var perr *PythonParseError
			if !errors.As(err, &perr) {
				t.Fatalf("PythonParseError にならなかった: %v", err)
			}
			if perr.Line != tt.wantLine {
				t.Errorf("行 = %d, want %d", perr.Line, tt.wantLine)
			}
			if want := "field larger than field limit (131072)"; perr.Message != want {
				t.Errorf("文面 = %q, want %q", perr.Message, want)
			}
		})
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
