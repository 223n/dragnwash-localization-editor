package csvfile

import (
	"slices"
	"testing"
)

// wholeRow は試験で期待する1レコード。列 a と b の値と、物理行の範囲を持つ。
type wholeRow struct {
	a, b          string
	line, endLine int
	unclosed      bool
}

// TestReadPowerShellWhole は、全文を1つの文字列として読んだ結果を固定する。
//
// 値はどれも pwsh 7.6.6 で、ファイル全体の文字列を ConvertFrom-Csv に渡して
// 実測したもの（上流の Read-Csv が f816618 からそうしている）。違えている
// ところは、各行の説明に書いた。
func TestReadPowerShellWhole(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		want         []wholeRow
		wantUnclosed int
	}{
		{
			name: "引用の中の LF は値に入る",
			text: "a,b\n1,\"x\ny\"\n2,3\n",
			want: []wholeRow{{a: "1", b: "x\ny", line: 2, endLine: 3}, {a: "2", b: "3", line: 4, endLine: 4}},
		},
		{
			// ゲーム側の作業コピーの実物と同じ形。レコードの区切りは CRLF、値の中は LF。
			name: "区切りが CRLF で値の中が LF",
			text: "a,b\r\n1,\"x\n\ny\",z\r\n2,3\r\n",
			want: []wholeRow{{a: "1", b: "x\n\ny", line: 2, endLine: 4}, {a: "2", b: "3", line: 5, endLine: 5}},
		},
		{
			name: "引用の中の CRLF はそのまま値に入る",
			text: "a,b\r\n1,\"x\r\ny\"\r\n",
			want: []wholeRow{{a: "1", b: "x\r\ny", line: 2, endLine: 3}},
		},
		{
			// 行の区切りは行単位の読み手と同じく単独の CR も数えるので、2行にまたがる。
			name: "引用の中の単独の CR も値に入る",
			text: "a,b\n1,\"x\ry\"\n2,3\n",
			want: []wholeRow{{a: "1", b: "x\ry", line: 2, endLine: 3}, {a: "2", b: "3", line: 4, endLine: 4}},
		},
		{
			// 上流の Remove-NonRecords はここで行を捨てるが、ConvertFrom-Csv は2行を読む。
			name: "単独の CR で区切ったレコードも読める",
			text: "a,b\r1,2\r3,4\r",
			want: []wholeRow{{a: "1", b: "2", line: 2, endLine: 2}, {a: "3", b: "4", line: 3, endLine: 3}},
		},
		{
			name: "先頭の空白の後ろの引用符は引用を開く",
			text: "a,b\n1,  \"x\ny\"\n2,3\n",
			want: []wholeRow{{a: "1", b: "x\ny", line: 2, endLine: 3}, {a: "2", b: "3", line: 4, endLine: 4}},
		},
		{
			// 引用符なしのフィールドの途中の '"' はただの文字。偶奇を数えるだけだと
			// ここから引用が開いたように見えるが、ConvertFrom-Csv は行末で閉じる。
			name: "フィールドの途中の引用符は引用を開かない",
			text: "a,b\n1,5\" x\n2,3\n",
			want: []wholeRow{{a: "1", b: "5\" x", line: 2, endLine: 2}, {a: "2", b: "3", line: 3, endLine: 3}},
		},
		{
			name: "閉じ引用符の後ろの引用符も引用を開かない",
			text: "a,b\n1,\"p\"q\"r\ns\"\n2,3\n",
			want: []wholeRow{
				{a: "1", b: "pq\"r", line: 2, endLine: 2},
				{a: "s\"", b: "", line: 3, endLine: 3},
				{a: "2", b: "3", line: 4, endLine: 4},
			},
		},
		{
			name: "閉じ引用符の後ろの空白は捨てる",
			text: "a,b\n1,\"x\"  \n2,3\n",
			want: []wholeRow{{a: "1", b: "x", line: 2, endLine: 2}, {a: "2", b: "3", line: 3, endLine: 3}},
		},
		{
			name:         "閉じない引用符はファイルの終わりまでを値にする",
			text:         "a,b\n1,\"x\n2,3\n",
			want:         []wholeRow{{a: "1", b: "x\n2,3\n", line: 2, endLine: 3, unclosed: true}},
			wantUnclosed: 2,
		},
		{
			// 最終行で開いた引用符は1行に収まるが、閉じてはいない。
			name:         "最終行で開いた引用符も閉じないと数える",
			text:         "a,b\n1,2\n3,\"x\n",
			want:         []wholeRow{{a: "1", b: "2", line: 2, endLine: 2}, {a: "3", b: "x\n", line: 3, endLine: 3, unclosed: true}},
			wantUnclosed: 3,
		},
		{
			// 上流の c8fda90 の Remove-NonRecords と同じく、引用の外の空行・空白だけの
			// 行・'#' の行はレコードにしない。引用の中にある同じ形の行は値に入る。
			name: "引用の外のコメントと空行は落とし、引用の中は残す",
			text: "a,b\n# c\n1,\"p\n# q\n\n  \nr\"\n\n# d\n  \n2,3\n",
			want: []wholeRow{{a: "1", b: "p\n# q\n\n  \nr", line: 3, endLine: 7}, {a: "2", b: "3", line: 11, endLine: 11}},
		},
		{
			// コメントの中の引用符はレコードを開かない（ゲームの CsvReader も同じ）。
			name: "コメントの中の引用符は数えない",
			text: "a,b\n# note,\"see\n1,2\n",
			want: []wholeRow{{a: "1", b: "2", line: 3, endLine: 3}},
		},
		{
			name: "空の引用フィールドだけの行はレコードにならない",
			text: "a,b\n\"\"\n1,2\n",
			want: []wholeRow{{a: "1", b: "2", line: 3, endLine: 3}},
		},
		{
			// 全文の ConvertFrom-Csv はこれを空の値2つのレコードとして返す（実測）。
			// ここでは行単位の読み方と同じく落とす。key も訳も持たないので、
			// 訳の突き合わせには効かない。
			name: "カンマだけの行はレコードにしない",
			text: "a,b\n,\n1,2\n",
			want: []wholeRow{{a: "1", b: "2", line: 3, endLine: 3}},
		},
		{
			name: "行末の空の引用符なしフィールドは捨てる",
			text: "a,b,c\n1,\"x\ny\",\n2,3,4\n",
			want: []wholeRow{{a: "1", b: "x\ny", line: 2, endLine: 3}, {a: "2", b: "3", line: 4, endLine: 4}},
		},
		{
			name: "BOM と末尾に改行の無い最終行",
			text: bom + "a,b\n1,2",
			want: []wholeRow{{a: "1", b: "2", line: 2, endLine: 2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := ReadPowerShellWhole([]byte(tt.text))
			var got []wholeRow
			for _, r := range w.Rows {
				got = append(got, wholeRow{a: r.Get("a"), b: r.Get("b"),
					line: r.Line, endLine: r.EndLine, unclosed: r.Unclosed})
				if r.MultiLine() != (r.EndLine > r.Line) {
					t.Errorf("MultiLine が行の範囲と食い違う: %+v", r)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("レコード\n got %+v\nwant %+v", got, tt.want)
			}
			if w.UnclosedLine != tt.wantUnclosed {
				t.Errorf("閉じない引用符の行 = %d, want %d", w.UnclosedLine, tt.wantUnclosed)
			}
		})
	}
}

// TestReadPowerShellWholeHeader は、ヘッダーのレコードの読み方を見る。
func TestReadPowerShellWholeHeader(t *testing.T) {
	t.Run("ヘッダーが行をまたぐ", func(t *testing.T) {
		w := ReadPowerShellWhole([]byte("a,\"b\nc\"\n1,2\n"))
		if !slices.Equal(w.Header.Fields, []string{"a", "b\nc"}) {
			t.Errorf("列名 = %q", w.Header.Fields)
		}
		if w.Header.Line != 1 || w.Header.EndLine != 2 || !w.Header.MultiLine() {
			t.Errorf("ヘッダーの範囲 = %+v", w.Header)
		}
		if len(w.Rows) != 1 || w.Rows[0].Get("b\nc") != "2" {
			t.Errorf("行 = %+v", w.Rows)
		}
	})

	t.Run("ヘッダーの前の空行とコメントと空白だけの行は飛ばす", func(t *testing.T) {
		w := ReadPowerShellWhole([]byte("\r\n# x\r\n \t\r\nkey,translation\r\nk,t\r\n"))
		if !slices.Equal(w.Header.Fields, []string{"key", "translation"}) || w.Header.Line != 4 {
			t.Errorf("ヘッダー = %+v", w.Header)
		}
		if w.Header.MultiLine() {
			t.Error("1行のヘッダーを行をまたぐと言っている")
		}
	})

	t.Run("ヘッダーの引用符が閉じない", func(t *testing.T) {
		w := ReadPowerShellWhole([]byte("key,\"source_en,translation\nk,s,t\n"))
		if !w.Header.Unclosed || w.UnclosedLine != 1 || w.Header.EndLine != 2 {
			t.Errorf("ヘッダー = %+v、閉じない引用符の行 = %d", w.Header, w.UnclosedLine)
		}
		if len(w.Rows) != 0 {
			t.Errorf("ヘッダーに飲み込まれた行が残っている: %+v", w.Rows)
		}
	})

	t.Run("レコードが無い", func(t *testing.T) {
		for _, text := range []string{"", "\n\n", "# a\n", " \n# b"} {
			w := ReadPowerShellWhole([]byte(text))
			if w.Header.Line != 0 || w.Header.Fields != nil || len(w.Rows) != 0 {
				t.Errorf("%q: %+v", text, w)
			}
		}
	})
}

// TestReadPowerShellWholeAgreesWithRowsOnSingleLines は、どのレコードも1行に
// 収まるファイルなら、全文の読み方と行単位の読み方が同じ行を返すことを見る。
//
// ここが割れると、publish の守りが食い違いを見つけても、それが行をまたぐ値の
// せいなのか、読み方そのものの差なのかを区別できない。
func TestReadPowerShellWholeAgreesWithRowsOnSingleLines(t *testing.T) {
	text := bom +
		"# 見出し\r\n" +
		"\r\n" +
		"key,section,node,order,speaker,translation\r\n" +
		"aaaaaaaaaaaaaaaa,UI,,,UI,\"a, \"\"quoted\"\" value\"\r\n" +
		"# --- node ---\n" +
		"\n" +
		"   \n" +
		"bbbbbbbbbbbbbbbb,L01 Ryan,N1,1,Ryan,  spaced  \r" +
		"line:cccccccc,L01 Ryan,N1,2,Ryan,5\" screen\n" +
		"\"\"\n" +
		"dddddddddddddddd,UI,,,UI,\"x\"tail,extra\n" +
		"eeeeeeeeeeeeeeee,UI,,,UI,"

	rows := mustReadPowerShell(t, text)
	w := ReadPowerShellWhole([]byte(text))
	if len(rows) != len(w.Rows) {
		t.Fatalf("行数: 行単位 %d、全文 %d", len(rows), len(w.Rows))
	}
	numbered, err := ReadPowerShellRowsNumbered([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		for _, col := range []string{"key", "section", "node", "order", "speaker", "translation"} {
			if rows[i].Get(col) != w.Rows[i].Get(col) {
				t.Errorf("[%d] %s: 行単位 %q、全文 %q", i, col, rows[i].Get(col), w.Rows[i].Get(col))
			}
		}
		if numbered[i].Line != w.Rows[i].Line || w.Rows[i].MultiLine() {
			t.Errorf("[%d] 行番号: 行単位 %d、全文 %d〜%d", i, numbered[i].Line, w.Rows[i].Line, w.Rows[i].EndLine)
		}
	}
	if w.UnclosedLine != 0 {
		t.Errorf("閉じない引用符があると言っている: %d", w.UnclosedLine)
	}
}
