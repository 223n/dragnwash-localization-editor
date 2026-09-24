package csvfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// segView は試験で期待する1セグメント。位置は本体の先頭からの相対で書く。
type segView struct {
	kind      SegmentKind
	line, end int
	body      string
	term      Terminator
	fields    []string
	offsets   []int
	open      int
}

func (v segView) String() string {
	return fmt.Sprintf("{%s L%d-%d %q %q fields=%q offsets=%v open=%d}",
		v.kind, v.line, v.end, v.body, string(v.term), v.fields, v.offsets, v.open)
}

func viewSegments(segs Segments) []segView {
	var out []segView
	for _, seg := range segs.List {
		v := segView{kind: seg.Kind, line: seg.Line, end: seg.EndLine, body: segs.Body(seg),
			term: seg.Term, fields: seg.Fields, open: seg.OpenLine}
		for _, o := range seg.Offsets {
			v.offsets = append(v.offsets, o-seg.Start)
		}
		out = append(out, v)
	}
	return out
}

func equalSegViews(a, b []segView) bool {
	return slices.EqualFunc(a, b, func(x, y segView) bool {
		return x.kind == y.kind && x.line == y.line && x.end == y.end && x.body == y.body &&
			x.term == y.term && slices.Equal(x.fields, y.fields) && slices.Equal(x.offsets, y.offsets) &&
			x.open == y.open
	})
}

// TestSplitSegments は、区切りの関数が分けるセグメントを固定する。
//
// 読み方の規則はこの関数だけが持つ。主の読み手・守り専用の読み手・飲み込みの
// 検出・保存（PR3 から）が、みなこの結果の上に載るので、ここがずれると全部が
// 同じ向きにずれる。値は ConvertFrom-Csv の全文の読み方（pwsh 7.6.6 の実測、
// [TestReadPowerShellWhole]）に合わせてある。
func TestSplitSegments(t *testing.T) {
	const header7 = "key,section,node,order,speaker,source_en,translation"
	tests := []struct {
		name  string
		text  string
		want  []segView
		lines int
	}{
		{
			// ゲーム側の作業コピーの実物と同じ形。レコードの区切りは CRLF、値の中は LF で、
			// 原文の段落のあいだに空行がある。訳は空。
			name: "実物と同じ形の作業コピー",
			text: bom + "# Language: Testish\r\n\r\n" + header7 + "\r\n" +
				"f2ea4a1f0e4e8626,UI,,,UI,\"para1\n\npara2\",\r\n" +
				"# --- node ---\r\n" +
				"3fc4ccfe745870e2,UI,,,UI,two,に\r\n",
			want: []segView{
				{kind: SegmentComment, line: 1, end: 1, body: "# Language: Testish", term: TermCRLF},
				{kind: SegmentBlank, line: 2, end: 2, body: "", term: TermCRLF},
				{kind: SegmentHeader, line: 3, end: 3, body: header7, term: TermCRLF,
					fields:  []string{"key", "section", "node", "order", "speaker", "source_en", "translation"},
					offsets: []int{0, 4, 12, 17, 23, 31, 41}},
				{kind: SegmentRecord, line: 4, end: 6, body: "f2ea4a1f0e4e8626,UI,,,UI,\"para1\n\npara2\",", term: TermCRLF,
					fields:  []string{"f2ea4a1f0e4e8626", "UI", "", "", "UI", "para1\n\npara2"},
					offsets: []int{0, 17, 20, 21, 22, 25, 40}},
				{kind: SegmentComment, line: 7, end: 7, body: "# --- node ---", term: TermCRLF},
				{kind: SegmentRecord, line: 8, end: 8, body: "3fc4ccfe745870e2,UI,,,UI,two,に", term: TermCRLF,
					fields:  []string{"3fc4ccfe745870e2", "UI", "", "", "UI", "two", "に"},
					offsets: []int{0, 17, 20, 21, 22, 25, 29}},
			},
			lines: 8,
		},
		{
			name: "終端の3種と改行の無い最終行",
			text: "a,b\r\n1,2\n3,4\r5,6",
			want: []segView{
				{kind: SegmentHeader, line: 1, end: 1, body: "a,b", term: TermCRLF, fields: []string{"a", "b"}, offsets: []int{0, 2}},
				{kind: SegmentRecord, line: 2, end: 2, body: "1,2", term: TermLF, fields: []string{"1", "2"}, offsets: []int{0, 2}},
				{kind: SegmentRecord, line: 3, end: 3, body: "3,4", term: TermCR, fields: []string{"3", "4"}, offsets: []int{0, 2}},
				{kind: SegmentRecord, line: 4, end: 4, body: "5,6", term: TermNone, fields: []string{"5", "6"}, offsets: []int{0, 2}},
			},
			lines: 4,
		},
		{
			// "," と `""` の行はデータとしては空のレコード。",x" は key の空いた2列の
			// レコードで、訳を消すと "," になって空のレコードへ変わる（保存でどう扱うかは
			// internal/edit の PR3 で決める。decisions のそのほか 8）。
			name: "カンマと空の引用だけの行は空のレコード",
			text: "key,translation\n,\n\"\"\n,x\n,,\n",
			want: []segView{
				{kind: SegmentHeader, line: 1, end: 1, body: "key,translation", term: TermLF, fields: []string{"key", "translation"}, offsets: []int{0, 4}},
				{kind: SegmentEmpty, line: 2, end: 2, body: ",", term: TermLF, fields: []string{""}, offsets: []int{0, 1}},
				{kind: SegmentEmpty, line: 3, end: 3, body: `""`, term: TermLF, fields: []string{""}, offsets: []int{0}},
				{kind: SegmentRecord, line: 4, end: 4, body: ",x", term: TermLF, fields: []string{"", "x"}, offsets: []int{0, 1}},
				{kind: SegmentRecord, line: 5, end: 5, body: ",,", term: TermLF, fields: []string{"", ""}, offsets: []int{0, 1, 2}},
			},
			lines: 5,
		},
		{
			// 上流でも "," の行は Trim で空にならないので、ヘッダーの候補から外れない。
			name: "ヘッダーの位置のカンマの行はヘッダー",
			text: ",\nkey,translation\n",
			want: []segView{
				{kind: SegmentHeader, line: 1, end: 1, body: ",", term: TermLF, fields: []string{""}, offsets: []int{0, 1}},
				{kind: SegmentRecord, line: 2, end: 2, body: "key,translation", term: TermLF, fields: []string{"key", "translation"}, offsets: []int{0, 4}},
			},
			lines: 2,
		},
		{
			// 上流の Remove-NonRecords の Trim（.NET の Char.IsWhiteSpace）と同じく落とす。
			// 行単位の読み手は、半角空白とタブしか空白と見ないのでレコードにする。
			name: "全角空白や NO-BREAK SPACE だけの行は空行",
			text: "a,b\n　\n  \t\n1,2\n",
			want: []segView{
				{kind: SegmentHeader, line: 1, end: 1, body: "a,b", term: TermLF, fields: []string{"a", "b"}, offsets: []int{0, 2}},
				{kind: SegmentBlank, line: 2, end: 2, body: "　", term: TermLF},
				{kind: SegmentBlank, line: 3, end: 3, body: "  \t", term: TermLF},
				{kind: SegmentRecord, line: 4, end: 4, body: "1,2", term: TermLF, fields: []string{"1", "2"}, offsets: []int{0, 2}},
			},
			lines: 4,
		},
		{
			name: "引用の中の '#' の行と空行は値に入る",
			text: "a,b\n1,\"x\n# y\n\nz\"\n# c\n",
			want: []segView{
				{kind: SegmentHeader, line: 1, end: 1, body: "a,b", term: TermLF, fields: []string{"a", "b"}, offsets: []int{0, 2}},
				{kind: SegmentRecord, line: 2, end: 5, body: "1,\"x\n# y\n\nz\"", term: TermLF, fields: []string{"1", "x\n# y\n\nz"}, offsets: []int{0, 2}},
				{kind: SegmentComment, line: 6, end: 6, body: "# c", term: TermLF},
			},
			lines: 6,
		},
		{
			// コメントの中の引用符はレコードを開かない（ゲームの CsvReader も同じ）。
			// 先頭に空白のある '#' はコメントにならない（行頭の1文字だけを見る）。
			name: "コメントの中の引用符と、空白の後ろの '#'",
			text: "a,b\n# note,\"see\n #x,1\n",
			want: []segView{
				{kind: SegmentHeader, line: 1, end: 1, body: "a,b", term: TermLF, fields: []string{"a", "b"}, offsets: []int{0, 2}},
				{kind: SegmentComment, line: 2, end: 2, body: "# note,\"see", term: TermLF},
				{kind: SegmentRecord, line: 3, end: 3, body: " #x,1", term: TermLF, fields: []string{"#x", "1"}, offsets: []int{0, 4}},
			},
			lines: 3,
		},
		{
			name: "閉じない引用符はファイルの終わりまでを1つのレコードにする",
			text: "a,b\n1,\"x\n2,3\n",
			want: []segView{
				{kind: SegmentHeader, line: 1, end: 1, body: "a,b", term: TermLF, fields: []string{"a", "b"}, offsets: []int{0, 2}},
				{kind: SegmentRecord, line: 2, end: 3, body: "1,\"x\n2,3\n", term: TermNone, fields: []string{"1", "x\n2,3\n"}, offsets: []int{0, 2}, open: 2},
			},
			lines: 3,
		},
		{
			// 引用符が開いたのはレコードの2行目。OpenLine はレコードの先頭ではなく、
			// 引用符のある行を指す。
			name: "行をまたいだ値の後ろで開いた引用符",
			text: "a,b,c\n1,\"x\ny\",\"z\n",
			want: []segView{
				{kind: SegmentHeader, line: 1, end: 1, body: "a,b,c", term: TermLF, fields: []string{"a", "b", "c"}, offsets: []int{0, 2, 4}},
				{kind: SegmentRecord, line: 2, end: 3, body: "1,\"x\ny\",\"z\n", term: TermNone, fields: []string{"1", "x\ny", "z\n"}, offsets: []int{0, 2, 8}, open: 3},
			},
			lines: 3,
		},
		{
			// 閉じない引用符のあとが空なので値は落ちる。読み手はデータに入れないが、
			// 閉じない引用符の印は残す（ReadPowerShellWhole も同じ）。
			name: "引用符1つだけの最終行は空のレコードで閉じない",
			text: "a,b\n\"",
			want: []segView{
				{kind: SegmentHeader, line: 1, end: 1, body: "a,b", term: TermLF, fields: []string{"a", "b"}, offsets: []int{0, 2}},
				{kind: SegmentEmpty, line: 2, end: 2, body: "\"", term: TermNone, offsets: []int{0}, open: 2},
			},
			lines: 2,
		},
		{
			// 行末の単独の CR もレコードの区切りになる。上流の Remove-NonRecords はここで
			// CR の手前を捨てる（上流の不具合。写さない）。
			name: "行末の単独の CR",
			text: "a,b\n1,x\r2,y\n",
			want: []segView{
				{kind: SegmentHeader, line: 1, end: 1, body: "a,b", term: TermLF, fields: []string{"a", "b"}, offsets: []int{0, 2}},
				{kind: SegmentRecord, line: 2, end: 2, body: "1,x", term: TermCR, fields: []string{"1", "x"}, offsets: []int{0, 2}},
				{kind: SegmentRecord, line: 3, end: 3, body: "2,y", term: TermLF, fields: []string{"2", "y"}, offsets: []int{0, 2}},
			},
			lines: 3,
		},
		{
			name:  "空のファイル",
			text:  "",
			lines: 0,
		},
		{
			name:  "BOM だけのファイル",
			text:  bom,
			lines: 0,
		},
		{
			name: "空行と空白だけのファイル",
			text: "\n \t\r\n",
			want: []segView{
				{kind: SegmentBlank, line: 1, end: 1, body: "", term: TermLF},
				{kind: SegmentBlank, line: 2, end: 2, body: " \t", term: TermCRLF},
			},
			lines: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			segs := SplitSegments([]byte(tt.text))
			if got := viewSegments(segs); !equalSegViews(got, tt.want) {
				t.Errorf("セグメント\n got %v\nwant %v", got, tt.want)
			}
			if segs.Lines != tt.lines {
				t.Errorf("物理行の数 = %d, want %d", segs.Lines, tt.lines)
			}
			wantUnclosed := 0
			for _, v := range tt.want {
				wantUnclosed = max(wantUnclosed, v.open)
			}
			if segs.UnclosedLine != wantUnclosed {
				t.Errorf("閉じない引用符の行 = %d, want %d", segs.UnclosedLine, wantUnclosed)
			}
			checkSegmentInvariants(t, tt.text, segs)
		})
	}
}

// TestSplitSegmentsInvariantsOnFixture は、上流との突き合わせの入力の表の全件で、
// 区切りの関数が約束する形を確かめる。
//
// 入力の表は、複数行の値・閉じない引用符・飲み込み・単独の CR・BOM など、読み方が
// 割れやすい形を集めたものなので、約束の確かめにそのまま使える。
func TestSplitSegmentsInvariantsOnFixture(t *testing.T) {
	for _, c := range loadFixtureCases(t).Cases {
		t.Run(c.Name, func(t *testing.T) {
			checkSegmentInvariants(t, c.Text, SplitSegments([]byte(c.Text)))
		})
	}
}

// checkSegmentInvariants は、区切りの関数が約束する形を確かめる。
//
//   - セグメントは BOM の直後からファイルの終わりまで、隙間も重なりも無く並ぶ。
//     保存がセグメントを元のバイト列のままつなぎ直せるための前提である
//   - ID は1からの通し番号で、物理行の範囲も隙間なく続き、最後は物理行の数で終わる
//   - フィールドの開始位置は、本体に FieldOffsets を掛けた位置と同じ。最終フィールドを
//     差し替える保存が、行単位のときと同じ関数で位置を出せることの裏付けである
//   - 閉じない引用符は最後のセグメントにしか無い
//   - 物理行を順につなぐと BOM の後ろのバイト列に戻る
//
// 実物の作業コピー（[TestRealWorkingCopy]）にも使うので、落ちたときに出すのは
// 位置と数だけにする。ゲームの台本を試験の出力に書き写さない。
func checkSegmentInvariants(t *testing.T, text string, segs Segments) {
	t.Helper()
	if segs.Text != text {
		t.Fatalf("Text が元のバイト列と違う")
	}
	pos, line := segs.BOM, 0
	for i, seg := range segs.List {
		if seg.ID != i+1 {
			t.Errorf("[%d] ID = %d, want %d", i, seg.ID, i+1)
		}
		if seg.Start != pos {
			t.Errorf("[%d] 開始 %d、直前のセグメントの終わりは %d", i, seg.Start, pos)
		}
		if seg.Line != line+1 || seg.EndLine < seg.Line {
			t.Errorf("[%d] 行の範囲 %d-%d、直前の終わりは %d", i, seg.Line, seg.EndLine, line)
		}
		pos, line = seg.End+len(seg.Term), seg.EndLine
		switch seg.Term {
		case TermLF, TermCRLF, TermCR:
		case TermNone:
			if i != len(segs.List)-1 {
				t.Errorf("[%d] 最後でないセグメントに終端が無い", i)
			}
		default:
			t.Errorf("[%d] 終端が改行でない: %q", i, seg.Term)
		}
		if seg.Unclosed() && i != len(segs.List)-1 {
			t.Errorf("[%d] 閉じない引用符が最後のセグメントでない", i)
		}
		if !seg.HasFields() {
			if seg.Fields != nil || seg.Offsets != nil {
				t.Errorf("[%d] %s にフィールドがある", i, seg.Kind)
			}
			continue
		}
		body := segs.Body(seg)
		want := FieldOffsets(body)
		for j := range want {
			want[j] += seg.Start
		}
		if !slices.Equal(seg.Offsets, want) {
			t.Errorf("[%d] フィールドの開始位置 %v、本体に FieldOffsets を掛けると %v", i, seg.Offsets, want)
		}
		if d := len(seg.Offsets) - len(seg.Fields); d < 0 || d > 1 {
			t.Errorf("[%d] 開始位置 %d 個に値 %d 個", i, len(seg.Offsets), len(seg.Fields))
		}
	}
	if pos != len(text) {
		t.Errorf("最後のセグメントの終わり %d がファイルの長さ %d と違う", pos, len(text))
	}
	if line != segs.Lines {
		t.Errorf("最後の行 %d が物理行の数 %d と違う", line, segs.Lines)
	}
	var joined strings.Builder
	for n := 1; n <= segs.Lines; n++ {
		body, term := segs.PhysicalLine(n)
		joined.WriteString(body + string(term))
	}
	if got, want := joined.String(), text[segs.BOM:]; got != want {
		at := 0
		for at < min(len(got), len(want)) && got[at] == want[at] {
			at++
		}
		t.Errorf("物理行をつないでも元に戻らない（%d バイト目から違う。長さ %d、元は %d）", at, len(got), len(want))
	}
}

// TestSegmentsPhysicalLine は、物理行を範囲の外で引いたときに空を返すことを見る。
func TestSegmentsPhysicalLine(t *testing.T) {
	segs := SplitSegments([]byte(bom + "a,b\r\nc"))
	for _, n := range []int{-1, 0, 3} {
		if body, term := segs.PhysicalLine(n); body != "" || term != TermNone {
			t.Errorf("PhysicalLine(%d) = (%q, %q), want 空", n, body, term)
		}
	}
	if body, term := segs.PhysicalLine(1); body != "a,b" || term != TermCRLF {
		t.Errorf("PhysicalLine(1) = (%q, %q)", body, term)
	}
	if h, ok := segs.Header(); !ok || h.Start != len(bom) || segs.BOM != len(bom) {
		t.Errorf("ヘッダー = %+v（%v）、BOM = %d", h, ok, segs.BOM)
	}
	if _, ok := SplitSegments([]byte("# a\n\n")).Header(); ok {
		t.Error("コメントと空行だけのファイルにヘッダーがあると言っている")
	}
}

// TestSegmentKindString は、種類の名前を固定する。呼び出し側が理由の文に使う。
func TestSegmentKindString(t *testing.T) {
	want := map[SegmentKind]string{
		SegmentBlank: "blank", SegmentComment: "comment", SegmentHeader: "header",
		SegmentRecord: "record", SegmentEmpty: "empty", SegmentKind(0): "unknown",
	}
	for kind, name := range want {
		if got := kind.String(); got != name {
			t.Errorf("%d.String() = %q, want %q", int(kind), got, name)
		}
	}
}

// loadFixtureCases は上流との突き合わせの入力の表（testdata/upstream/cases.json）を読む。
// 正解（expected.json）は見ないので、正解と照らさない試験から使う。
func loadFixtureCases(t *testing.T) fixtureCases {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(upstreamDir, "cases.json"))
	if err != nil {
		t.Fatalf("入力の表が読めない: %v", err)
	}
	var cases fixtureCases
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("入力の表を解釈できない: %v", err)
	}
	return cases
}
