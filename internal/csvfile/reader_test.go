package csvfile

import (
	"errors"
	"slices"
	"testing"
)

// mainRow は試験で期待する1レコード。列 a と b の値と、ID と物理行の範囲を持つ。
type mainRow struct {
	a, b          string
	id            int
	line, endLine int
}

func mainRows(f PowerShellFile) []mainRow {
	var out []mainRow
	for _, r := range f.Records {
		out = append(out, mainRow{a: r.Get("a"), b: r.Get("b"), id: r.ID, line: r.Line, endLine: r.EndLine})
	}
	return out
}

// TestReadPowerShell は、主の読み手が読むレコードと返す誤りを固定する。
//
// 読み方そのもの（引用の中の改行、コメント、空行）は [TestSplitSegments] と
// [TestReadPowerShellWhole] が見ている。ここで見るのは、その上に載せたもの
// （ID と行の範囲、空のレコードを落とすこと、型付きの誤り）である。
func TestReadPowerShell(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []mainRow
	}{
		{
			// ID はコメント行と空行も数えた通し番号。行番号とは別に進む。
			name: "ID は通し番号で、行をまたぐレコードでも1つ",
			text: "# c\na,b\n1,\"x\ny\"\n\n2,3\n",
			want: []mainRow{{a: "1", b: "x\ny", id: 3, line: 3, endLine: 4}, {a: "2", b: "3", id: 5, line: 6, endLine: 6}},
		},
		{
			// 上流は空の値のレコードとして返し、publish の集計で malformed dropped に数える。
			// dwloc は黙って落とす。出力は同じで、違うのは集計の数だけ（decisions のそのほか 2）。
			name: "カンマだけの行と空の引用だけの行は落とす",
			text: "a,b\n1,2\n,\n\"\"\n3,4\n",
			want: []mainRow{{a: "1", b: "2", id: 2, line: 2, endLine: 2}, {a: "3", b: "4", id: 5, line: 5, endLine: 5}},
		},
		{
			// 行単位の読み手は半角空白とタブしか空白と見ないのでレコードにするが、
			// 上流 main の Remove-NonRecords は .NET の Trim で落とす。そちらにそろう。
			name: "全角空白だけの行は落とす",
			text: "a,b\n1,2\n　\n3,4\n",
			want: []mainRow{{a: "1", b: "2", id: 2, line: 2, endLine: 2}, {a: "3", b: "4", id: 4, line: 4, endLine: 4}},
		},
		{
			// 空の列名は重複に数えない（上流は H1, H2 ... の既定名を振る）。
			name: "空の列名が並んでも誤りにしない",
			text: "a,,b,\n1,x,2,y\n",
			want: []mainRow{{a: "1", b: "2", id: 2, line: 2, endLine: 2}},
		},
		{
			name: "ヘッダーだけのファイル",
			text: "a,b\n",
		},
		{
			name: "空のファイル",
			text: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := ReadPowerShell([]byte(tt.text))
			if err != nil {
				t.Fatalf("ReadPowerShell が失敗した: %v", err)
			}
			if got := mainRows(f); !slices.Equal(got, tt.want) {
				t.Errorf("レコード\n got %+v\nwant %+v", got, tt.want)
			}
			for _, r := range f.Records {
				seg := f.Segments.List[r.ID-1]
				if seg.Kind != SegmentRecord || seg.Line != r.Line || seg.EndLine != r.EndLine {
					t.Errorf("ID %d がセグメント %+v を指していない", r.ID, seg)
				}
				if r.MultiLine() != seg.MultiLine() || r.Unclosed {
					t.Errorf("ID %d の MultiLine / Unclosed が食い違う: %+v", r.ID, r)
				}
			}
		})
	}
}

// TestReadPowerShellErrors は、主の読み手が値を返さずに型付きの誤りを返すことを見る。
//
// 閉じない引用符で値を返すと、後ろの行（英語の原文やキー）を飲み込んだ値が
// 呼び出し側へ渡る。上流の報告 #11 の漏れは、確かめ忘れた呼び出し側から起きる
// （decisions の 3）。
func TestReadPowerShellErrors(t *testing.T) {
	tests := []struct {
		name string
		text string
		// unclosed は閉じない引用符の行。0 なら列名の重複を期待する。
		unclosed int
		dup      string
	}{
		{name: "閉じない引用符", text: "a,b\n1,\"x\n2,3\n", unclosed: 2},
		{name: "最終行で開いた引用符", text: "a,b\n1,2\n3,\"x\n", unclosed: 3},
		{name: "ヘッダーの引用符が閉じない", text: "a,\"b\n1,2\n", unclosed: 1},
		{name: "行をまたいだ値の後ろで開いた引用符", text: "a,b,c\n1,\"x\ny\",\"z\n", unclosed: 3},
		{name: "列名の重複（データあり）", text: "key,translation,KEY\nk,t,x\n", dup: "KEY"},
		{name: "列名の重複（データが0件）", text: "key,translation,Key\n", dup: "Key"},
		{name: "列名の重複（後ろに空行だけ）", text: "key,translation,Key\n\n", dup: "Key"},
		{name: "列名の重複（大文字小文字違い、空の列をはさむ）", text: "key,,KEY\n", dup: "KEY"},
		{
			// 閉じない引用符はそこから後ろ（ヘッダーのこともある）を崩すので、先に直す。
			name: "閉じない引用符と列名の重複の両方", text: "key,KEY,\"t\nk,x,y\n", unclosed: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := ReadPowerShell([]byte(tt.text))
			if err == nil {
				t.Fatalf("誤りを返さなかった: %+v", f)
			}
			if f.Records != nil || f.Header.ID != 0 || f.Segments.List != nil {
				t.Errorf("誤りと一緒に値を返している: %+v", f)
			}
			var unclosed *UnclosedQuoteError
			var dup *DuplicateColumnError
			switch {
			case tt.unclosed > 0:
				if !errors.As(err, &unclosed) || unclosed.Line != tt.unclosed {
					t.Errorf("err = %v, want %d行目の *UnclosedQuoteError", err, tt.unclosed)
				}
			default:
				if !errors.As(err, &dup) || dup.Name != tt.dup {
					t.Errorf("err = %v, want 列名 %q の *DuplicateColumnError", err, tt.dup)
				}
			}
		})
	}
}

// TestReadPowerShellMarked は、表示用の入口が誤りにせず印を付けて返すことを見る。
//
// 画面は壊れたファイルでも並べる必要がある。閉じない引用符のあるファイルは全体を
// 読み取り専用にし、引用符が開いたレコードから後ろを物理行のまま並べる
// （decisions の 3）。そのためにセグメントと印が要る。
func TestReadPowerShellMarked(t *testing.T) {
	t.Run("閉じない引用符と列名の重複に印を付ける", func(t *testing.T) {
		f := ReadPowerShellMarked([]byte("key,Key,t\nk,x,1\nk2,y,\"open\nk3,z,3\n"))
		if f.Unclosed == nil || f.Unclosed.Line != 3 {
			t.Errorf("閉じない引用符の印 = %+v, want 3行目", f.Unclosed)
		}
		if f.Duplicate == nil || f.Duplicate.Name != "Key" {
			t.Errorf("列名の重複の印 = %+v", f.Duplicate)
		}
		var unclosed *UnclosedQuoteError
		if err := f.Err(); !errors.As(err, &unclosed) {
			t.Errorf("Err() = %v, want 閉じない引用符が先", err)
		}
		if len(f.Records) != 2 || f.Records[1].Line != 3 || f.Records[1].EndLine != 4 || !f.Records[1].Unclosed {
			t.Fatalf("レコード = %+v", f.Records)
		}
		// 後の列が勝つ（NewRow）。ゲームの CsvReader のインデクサ代入と同じ。
		if got := f.Records[0].Get("key"); got != "x" {
			t.Errorf("重複した列の値 = %q, want 後の列の \"x\"", got)
		}
		// 開いたレコードより後ろは、物理行のまま引ける。
		if body, term := f.Segments.PhysicalLine(4); body != "k3,z,3" || term != TermLF {
			t.Errorf("4行目 = (%q, %q)", body, term)
		}
	})

	t.Run("列名の重複だけ", func(t *testing.T) {
		f := ReadPowerShellMarked([]byte("a,A\n"))
		var dup *DuplicateColumnError
		if err := f.Err(); !errors.As(err, &dup) || f.Unclosed != nil {
			t.Errorf("Err() = %v、閉じない引用符 = %+v", err, f.Unclosed)
		}
	})

	t.Run("壊れていなければ印は無い", func(t *testing.T) {
		f := ReadPowerShellMarked([]byte("a,b\n1,2\n"))
		// 型付きの nil を error に入れると nil でなくなる。その取り違えを見る。
		if err := f.Err(); err != nil {
			t.Errorf("Err() = %v, want nil", err)
		}
		if f.Unclosed != nil || f.Duplicate != nil {
			t.Errorf("印がある: %+v %+v", f.Unclosed, f.Duplicate)
		}
	})
}

// TestPowerShellHeaderCommentLike は、'#' で始まるヘッダーを見分けることを固定する。
//
// dwloc は飛ばさずにヘッダーとして返し、publish の形の確かめ (a) がこれを見て止める
// （decisions のそのほか 2 の (iii)）。判定は列名の前後の空白を除いて見るので、
// 上流が飛ばさない `" #key"` や NO-BREAK SPACE の後ろの '#' も当たる
// （[TestCommentLikeOnFixture]）。
func TestPowerShellHeaderCommentLike(t *testing.T) {
	tests := []struct {
		text  string
		first string
		want  bool
	}{
		{"\"#key\",source_en,translation\nk,s,t\n", "#key", true},
		{" #key,source_en,translation\nk,s,t\n", "#key", true},
		{"\" #key\",translation\n", " #key", true},
		{"\"\t#key\",translation\n", "\t#key", true},
		{" #key,translation\n", " #key", true},
		{"\"　#key\",translation\n", "　#key", true},
		{"key,source_en,translation\nk,s,t\n", "key", false},
		{"key#,translation\n", "key#", false},
		// 行頭の '#' はコメントとして落ちるので、次の行がヘッダーになる（上流と同じ）。
		{"#key,source_en,translation\nk,s,t\n", "k", false},
		{"\"\"\n", "", false},
		{"# only\n", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			f, err := ReadPowerShell([]byte(tt.text))
			if err != nil {
				t.Fatal(err)
			}
			first := ""
			if len(f.Header.Fields) > 0 {
				first = f.Header.Fields[0]
			}
			if first != tt.first || f.Header.CommentLike() != tt.want {
				t.Errorf("最初の列名 %q、CommentLike %v, want %q と %v", first, f.Header.CommentLike(), tt.first, tt.want)
			}
		})
	}
}

// TestCommentLikeOnFixture は、上流との突き合わせの入力の表で、'#' で始まると
// 見なすヘッダーと、そのうち上流が飛ばすものを固定する。
//
// 上流が飛ばすのは、読んだ最初の値そのものが '#' で始まる2件だけである。
// hash-header-quoted-space と hash-header-nbsp は、上流ではその名前の列（key 列の
// 無いヘッダー）になり、source_en から訳を書く。dwloc は前後の空白を除いて見るので
// この2件も '#' で始まると見なし、PR2 から (a) で止める（移植仕様「上流と意図して
// 違える点」）。判定を上流と同じ「最初の値そのもの」に変えると、この試験が落ちる。
func TestCommentLikeOnFixture(t *testing.T) {
	commentLike := map[string]bool{
		"hash-header-quoted": true, "hash-header-leading-space": true,
		"hash-header-quoted-space": true, "hash-header-nbsp": true,
	}
	upstreamSkips := map[string]bool{"hash-header-quoted": true, "hash-header-leading-space": true}

	cases, exp := loadUpstreamFixture(t)
	for i, c := range cases.Cases {
		header := ReadPowerShellMarked([]byte(c.Text)).Header
		if got := header.CommentLike(); got != commentLike[c.Name] {
			t.Errorf("%s: CommentLike() = %v, want %v", c.Name, got, commentLike[c.Name])
		}
		if !commentLike[c.Name] {
			continue
		}
		// 上流が飛ばしたなら、上流の列名は dwloc のヘッダーではなく次のレコードになる。
		skipped := !sameColumns(exp.Cases[i].Read.Columns, header.Fields)
		if skipped != upstreamSkips[c.Name] {
			t.Errorf("%s: 上流がヘッダーを飛ばしたか = %v, want %v（上流の列名 %q）",
				c.Name, skipped, upstreamSkips[c.Name], exp.Cases[i].Read.Columns)
		}
	}
	for name := range commentLike {
		if !slices.ContainsFunc(cases.Cases, func(c fixtureCase) bool { return c.Name == name }) {
			t.Errorf("表の %s は入力の表に無い", name)
		}
	}
}

// TestReadPowerShellAgreesWithWhole は、主の読み手と守り専用の読み手が、誤りの
// 無いファイルでは同じレコードを返すことを、上流との突き合わせの入力の表の全件で見る。
//
// 2つは同じ区切りの関数に載っていて、違うのは誤りの扱いだけのはずである。
// ここが割れると、PR2 で publish の守りを主の読み手へ移したときに、守りの結果まで
// 変わってしまう。
func TestReadPowerShellAgreesWithWhole(t *testing.T) {
	for _, c := range loadFixtureCases(t).Cases {
		t.Run(c.Name, func(t *testing.T) {
			whole := ReadPowerShellWhole([]byte(c.Text))
			f := ReadPowerShellMarked([]byte(c.Text))
			if (f.Unclosed != nil) != (whole.UnclosedLine > 0) || (f.Unclosed != nil && f.Unclosed.Line != whole.UnclosedLine) {
				t.Errorf("閉じない引用符: 主 %+v、全文 %d", f.Unclosed, whole.UnclosedLine)
			}
			if !slices.Equal(f.Header.Fields, whole.Header.Fields) || f.Header.Line != whole.Header.Line ||
				f.Header.EndLine != whole.Header.EndLine || f.Header.Unclosed != whole.Header.Unclosed {
				t.Errorf("ヘッダー: 主 %+v、全文 %+v", f.Header, whole.Header)
			}
			if len(f.Records) != len(whole.Rows) {
				t.Fatalf("レコード数: 主 %d、全文 %d", len(f.Records), len(whole.Rows))
			}
			for i, r := range f.Records {
				w := whole.Rows[i]
				if r.Line != w.Line || r.EndLine != w.EndLine || r.Unclosed != w.Unclosed ||
					!slices.Equal(rowRecord(r.Row, 0, 0).values, rowRecord(w.Row, 0, 0).values) {
					t.Errorf("[%d] 主 %+v、全文 %+v", i, r, w)
				}
			}
		})
	}
}

// TestUnclosedQuoteErrorMessage は、文言に引用符が開いた行が出ることを見る。
// 利用者は文言だけを頼りに、どこの引用符を閉じるかを探す。
func TestUnclosedQuoteErrorMessage(t *testing.T) {
	err := error(&UnclosedQuoteError{Line: 12})
	if got, want := err.Error(), "12行目で開いた引用符がファイルの終わりまで閉じない"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
