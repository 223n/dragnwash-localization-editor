package csvfile

import (
	"errors"
	"slices"
	"testing"
)

// TestParsePowerShellRecord は pwsh 7.6.6 の ConvertFrom-Csv で実測した値を固定する。
// 期待値はすべて実測であって、移植仕様の文面からの推測ではない。
func TestParsePowerShellRecord(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		want   []string
		record bool
	}{
		{name: "通常の行", line: "1,2", want: []string{"1", "2"}, record: true},
		{name: "フィールド1つ", line: "x", want: []string{"x"}, record: true},
		{name: "末尾がカンマ", line: "1,", want: []string{"1"}, record: true},
		{name: "先頭がカンマ", line: ",2", want: []string{"", "2"}, record: true},

		// レコードにならない行。
		{name: "空行", line: "", record: false},
		{name: "空白だけの行", line: "   ", record: false},
		{name: "タブだけの行", line: "\t", record: false},
		{name: "カンマだけの行", line: ",", record: false},
		{name: "空の引用フィールドだけの行", line: `""`, record: false},
		{name: "空の引用フィールドと空フィールド", line: `"",`, record: false},
		{name: "空白とカンマだけの行", line: " , ", record: false},

		// 一方こちらはレコードになる。行末のフィールドが「空かつ引用符なし」の
		// ときだけ捨てられる、という規則で説明できる。
		{name: "カンマ2つ", line: ",,", want: []string{"", ""}, record: true},
		{name: "末尾が空の引用フィールド", line: `,""`, want: []string{"", ""}, record: true},
		{name: "空の引用フィールド2つ", line: `"",""`, want: []string{"", ""}, record: true},
		// 全角空白は空白として扱われない（対象は半角スペースとタブだけ）。
		{name: "全角空白だけの行", line: "　", want: []string{"　"}, record: true},

		// 引用符の扱い。
		{name: "引用の中のカンマ", line: `1,"a,b"`, want: []string{"1", "a,b"}, record: true},
		{name: "引用の中の二重引用符", line: `1,"a""b"`, want: []string{"1", `a"b`}, record: true},
		{name: "閉じない引用符", line: `1,"x`, want: []string{"1", "x"}, record: true},
		{name: "閉じ引用符の後ろの文字", line: `1,"x"y"`, want: []string{"1", `xy"`}, record: true},
		{name: "閉じ引用符の後ろの文字とカンマ", line: `"a"x,b`, want: []string{"ax", "b"}, record: true},
		{name: "引用符なしの裸の二重引用符", line: `1,he said "hi"`, want: []string{"1", `he said "hi"`}, record: true},
		{name: "引用の中の前後空白は保たれる", line: `1,"  q  "`, want: []string{"1", "  q  "}, record: true},
		{name: "引用符の前の空白は捨てる", line: `1, "q"`, want: []string{"1", "q"}, record: true},

		// 引用符なしフィールドの空白。移植仕様の「末尾空白を1個残して除去」は
		// 単純な形にしか当てはまらない。
		{name: "先頭空白は全部捨てる", line: "1,  lead", want: []string{"1", "lead"}, record: true},
		{name: "末尾空白は1文字だけ残る", line: "1,x   ", want: []string{"1", "x "}, record: true},
		{name: "残るのは最初の1文字なのでタブ", line: "1,x\t\t", want: []string{"1", "x\t"}, record: true},
		{name: "途中に空白があると削らない", line: "1,x y  ", want: []string{"1", "x y  "}, record: true},
		{name: "途中に空白があると削らない2", line: "1,x  y  ", want: []string{"1", "x  y  "}, record: true},
		{name: "途中に二重引用符があると削らない", line: `1,x"  `, want: []string{"1", `x"  `}, record: true},
		{name: "引用符でなければ削る", line: "1,x'y  ", want: []string{"1", "x'y "}, record: true},
		{name: "前後どちらにも空白", line: "1,  x  ", want: []string{"1", "x "}, record: true},
		{name: "複数フィールドそれぞれで削る", line: "x  ,y  ", want: []string{"x ", "y "}, record: true},
		{name: "引用フィールドの後ろの空白は全部捨てる", line: `1,"q"  `, want: []string{"1", "q"}, record: true},
		{name: "引用フィールドの後ろに文字があれば空白も残る", line: `1,"q"  x  `, want: []string{"1", "q  x  "}, record: true},

		// 追加の実測値。上の規則だけで説明がつくかを確かめるために選んだ形。
		{name: "引用の後ろにさらに引用符", line: `a,"b"c"d`, want: []string{"a", `bc"d`}, record: true},
		{name: "引用の中の二重引用符と後ろの文字", line: `"a""b"c`, want: []string{`a"bc`}, record: true},
		{name: "引用の前後に空白があってカンマが続く", line: ` "a" ,b`, want: []string{"a", "b"}, record: true},
		{name: "引用の中が空白だけ", line: `"  "`, want: []string{"  "}, record: true},
		{name: "末尾フィールドが空白だけなら捨てる", line: "a,  ", want: []string{"a"}, record: true},
		{name: "先頭フィールドが空白だけ", line: "  ,a", want: []string{"", "a"}, record: true},
		{name: "引用の後ろに空白と引用符", line: `"a" "b"`, want: []string{`a "b"`}, record: true},
		{name: "引用符を含む引用符なしフィールドの末尾空白", line: `a"b"c  `, want: []string{`a"b"c  `}, record: true},
		{name: "タブのあとに引用符", line: "\t\"q\"", want: []string{"q"}, record: true},
		{name: "引用の後ろのタブと文字", line: "\"q\"\t\tx", want: []string{"q\t\tx"}, record: true},
		{name: "途中のフィールドの末尾空白", line: "a,b ,c", want: []string{"a", "b ", "c"}, record: true},
		{name: "引用の中のカンマと後ろの空白", line: `"a,b" ,c`, want: []string{"a,b", "c"}, record: true},
		{name: "二重引用符4つはリテラルの引用符1つ", line: `""""`, want: []string{`"`}, record: true},
		{name: "末尾空白のあとカンマで終わる", line: "x ,", want: []string{"x "}, record: true},
		{name: "カンマの後ろの空白は捨てる", line: "a, b", want: []string{"a", "b"}, record: true},
		{name: "先頭が空の引用フィールド", line: `"",x`, want: []string{"", "x"}, record: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParsePowerShellRecord(tt.line)
			if ok != tt.record {
				t.Fatalf("ParsePowerShellRecord(%q) のレコード判定 = %v, want %v", tt.line, ok, tt.record)
			}
			if !ok {
				return
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("ParsePowerShellRecord(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestReadPowerShellRows(t *testing.T) {
	t.Run("BOMを剥がす", func(t *testing.T) {
		rows := mustReadPowerShell(t, bom+"flow_asset,level\nLevelFlow,0\n")
		if len(rows) != 1 {
			t.Fatalf("行数 = %d, want 1", len(rows))
		}
		if got, ok := rows[0].Lookup("flow_asset"); !ok || got != "LevelFlow" {
			t.Errorf("Lookup(flow_asset) = (%q, %v), want (\"LevelFlow\", true)。BOM が剥がれていない", got, ok)
		}
	})

	t.Run("行頭のシャープを落とす", func(t *testing.T) {
		rows := mustReadPowerShell(t, "# 見出し\nkey,translation\n# --- node ---\na,b\n")
		if len(rows) != 1 {
			t.Fatalf("行数 = %d, want 1", len(rows))
		}
		if got := rows[0].Get("translation"); got != "b" {
			t.Errorf("Get(translation) = %q, want %q", got, "b")
		}
	})

	t.Run("先頭に空白があるシャープはデータ行", func(t *testing.T) {
		rows := mustReadPowerShell(t, "key,translation\n #x,b\n")
		if len(rows) != 1 {
			t.Fatalf("行数 = %d, want 1", len(rows))
		}
		if got := rows[0].Get("key"); got != "#x" {
			t.Errorf("Get(key) = %q, want %q（先頭空白は落ちるが行は残る）", got, "#x")
		}
	})

	t.Run("シャープを除いた残りが2行未満なら0行", func(t *testing.T) {
		for _, text := range []string{"", "key,translation\n", "# a\nkey,translation\n", "# a\n# b\n"} {
			rows, err := ReadPowerShellRows([]byte(text))
			if err != nil {
				t.Fatalf("ReadPowerShellRows(%q) が失敗した: %v", text, err)
			}
			if len(rows) != 0 {
				t.Errorf("ReadPowerShellRows(%q) の行数 = %d, want 0", text, len(rows))
			}
		}
	})

	// '#' を除いた残りは2行以上あるが、すべて空行でヘッダーが見つからない形。
	// 元実装の Read-Csv も0行を返して止まらない（pwsh 7.6.6 で実測）。
	t.Run("空行しか残らなければ0行", func(t *testing.T) {
		for _, text := range []string{"\n\n", "\r\n\r\n\r\n", "# a\n\n\n"} {
			rows, err := ReadPowerShellRows([]byte(text))
			if err != nil {
				t.Fatalf("ReadPowerShellRows(%q) が失敗した: %v", text, err)
			}
			if len(rows) != 0 {
				t.Errorf("ReadPowerShellRows(%q) の行数 = %d, want 0", text, len(rows))
			}
		}
	})

	t.Run("空行と空白だけの行は落ちる", func(t *testing.T) {
		rows := mustReadPowerShell(t, "key,translation\n\na,b\n   \nc,d\n")
		if len(rows) != 2 {
			t.Fatalf("行数 = %d, want 2", len(rows))
		}
	})

	t.Run("空行が先にあると次の行がヘッダーになる", func(t *testing.T) {
		rows := mustReadPowerShell(t, "\nkey,translation\na,b\n")
		if len(rows) != 1 {
			t.Fatalf("行数 = %d, want 1", len(rows))
		}
		if got := rows[0].Get("key"); got != "a" {
			t.Errorf("Get(key) = %q, want %q", got, "a")
		}
	})

	t.Run("列数の過不足はエラーにしない", func(t *testing.T) {
		rows := mustReadPowerShell(t, "a,b,c\n1\n1,2,3,4\n")
		if len(rows) != 2 {
			t.Fatalf("行数 = %d, want 2", len(rows))
		}
		if got, ok := rows[0].Lookup("c"); !ok || got != "" {
			t.Errorf("Lookup(c) = (%q, %v), want (\"\", true)", got, ok)
		}
		if got := rows[1].Get("c"); got != "3" {
			t.Errorf("Get(c) = %q, want %q（余った列は捨てる）", got, "3")
		}
	})

	t.Run("ヘッダー名の重複はエラー", func(t *testing.T) {
		_, err := ReadPowerShellRows([]byte("a,A\n1,2\n"))
		var dup *DuplicateColumnError
		if !errors.As(err, &dup) {
			t.Fatalf("err = %v, want *DuplicateColumnError", err)
		}
		if dup.Name != "A" {
			t.Errorf("重複した列名 = %q, want %q", dup.Name, "A")
		}
	})

	t.Run("列名の照合は大文字小文字を区別しない", func(t *testing.T) {
		rows := mustReadPowerShell(t, "Key,Source_EN,Translation\nk,s,t\n")
		if len(rows) != 1 {
			t.Fatalf("行数 = %d, want 1", len(rows))
		}
		for _, name := range []string{"key", "source_en", "translation"} {
			if _, ok := rows[0].Lookup(name); !ok {
				t.Errorf("Lookup(%q) が見つからない。厳密一致で実装すると出力が全損する", name)
			}
		}
	})

	t.Run("CRLFでも読める", func(t *testing.T) {
		rows := mustReadPowerShell(t, "key,translation\r\na,b\r\n")
		if len(rows) != 1 {
			t.Fatalf("行数 = %d, want 1", len(rows))
		}
		if got := rows[0].Get("translation"); got != "b" {
			t.Errorf("Get(translation) = %q, want %q", got, "b")
		}
	})

	// 引用フィールド内の改行はサポートされない。物理行ごとに別レコードへ割れる。
	// 元実装と同じ壊れ方を保つ（移植仕様「公開CSV生成 / 敵対検証」[medium] R4）。
	t.Run("引用フィールド内の改行は物理行で割れる", func(t *testing.T) {
		rows := mustReadPowerShell(t, "a,b\n1,\"x\ny\"\n2,3\n")
		if len(rows) != 3 {
			t.Fatalf("行数 = %d, want 3（複数行フィールドは解釈しない）", len(rows))
		}
		want := []struct{ a, b string }{{"1", "x"}, {`y"`, ""}, {"2", "3"}}
		for i, w := range want {
			if got := rows[i].Get("a"); got != w.a {
				t.Errorf("rows[%d].Get(a) = %q, want %q", i, got, w.a)
			}
			if got := rows[i].Get("b"); got != w.b {
				t.Errorf("rows[%d].Get(b) = %q, want %q", i, got, w.b)
			}
		}
	})
}

// TestReadPowerShellRowsNumbered は、各行に添える物理行番号を確かめる。
//
// この番号は internal/publish が「何行目の訳が失われるか」を人に示すのに使う。
// '#' の行・空行・空白だけの行を数え落とすと、指した行に別の訳が書いてあり、
// 利用者は失われる訳を見誤る。終端の3種（CRLF / CR / LF）と、末尾に改行が
// 無い最終行も1行として数えることをここで見る。
func TestReadPowerShellRowsNumbered(t *testing.T) {
	text := bom +
		"# 見出し\r\n" + // 1
		"\r\n" + // 2 ヘッダー探索で飛ばす空行
		"key,translation\r\n" + // 3 ヘッダー
		"a,1\r\n" + // 4
		"# --- node ---\n" + // 5
		"\n" + // 6 空行
		"   \n" + // 7 空白だけの行（レコードにならない）
		"b,2\r" + // 8 CR 単独の終端
		"c,3" // 9 末尾に改行が無い

	numbered, err := ReadPowerShellRowsNumbered([]byte(text))
	if err != nil {
		t.Fatalf("ReadPowerShellRowsNumbered が失敗した: %v", err)
	}
	want := []struct {
		key, translation string
		line             int
	}{
		{"a", "1", 4},
		{"b", "2", 8},
		{"c", "3", 9},
	}
	if len(numbered) != len(want) {
		t.Fatalf("行数 = %d, want %d", len(numbered), len(want))
	}
	for i, w := range want {
		got := numbered[i]
		if got.Get("key") != w.key || got.Get("translation") != w.translation {
			t.Errorf("[%d] = (%q, %q), want (%q, %q)",
				i, got.Get("key"), got.Get("translation"), w.key, w.translation)
		}
		if got.Line != w.line {
			t.Errorf("[%d].Line = %d, want %d", i, got.Line, w.line)
		}
	}

	// 番号なしの読み方は同じ行を同じ順に返す。規則が片方だけで直る形を防ぐ。
	rows := mustReadPowerShell(t, text)
	if len(rows) != len(numbered) {
		t.Fatalf("ReadPowerShellRows の行数 = %d, want %d", len(rows), len(numbered))
	}
	for i := range rows {
		if !slices.Equal(rows[i].Columns(), numbered[i].Columns()) ||
			rows[i].Get("key") != numbered[i].Get("key") ||
			rows[i].Get("translation") != numbered[i].Get("translation") {
			t.Errorf("[%d] の中身が番号つきと違う", i)
		}
	}
}

// TestDuplicateColumnErrorMessage は、重複した列名が文言に引用符つきで出ることを見る。
// 利用者は文言だけを頼りにヘッダーを直す。空白だけの列名も見分けられるよう、
// 名前は %q で囲む。
func TestDuplicateColumnErrorMessage(t *testing.T) {
	tests := []struct {
		label  string
		column string
		want   string
	}{
		{"ふつうの列名", "KEY", `ヘッダーの列名 "KEY" が重複している`},
		{"空白だけの列名", " ", `ヘッダーの列名 " " が重複している`},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			err := error(&DuplicateColumnError{Name: tt.column})
			if got := err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func mustReadPowerShell(t *testing.T, text string) []Row {
	t.Helper()
	rows, err := ReadPowerShellRows([]byte(text))
	if err != nil {
		t.Fatalf("ReadPowerShellRows が失敗した: %v", err)
	}
	return rows
}
