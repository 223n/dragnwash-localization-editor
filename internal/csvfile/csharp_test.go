package csvfile

import (
	"slices"
	"testing"
)

func TestParseCSharpRecords(t *testing.T) {
	tests := []struct {
		name string
		text string
		want [][]string
	}{
		{
			name: "空ファイルはレコード0件",
			text: "",
			want: nil,
		},
		{
			name: "末尾が改行",
			text: "key,translation\na,b\n",
			want: [][]string{{"key", "translation"}, {"a", "b"}},
		},
		{
			name: "末尾が改行なし",
			text: "key,translation\na,b",
			want: [][]string{{"key", "translation"}, {"a", "b"}},
		},
		{
			name: "CRLF",
			text: "key,translation\r\na,b\r\n",
			want: [][]string{{"key", "translation"}, {"a", "b"}},
		},
		{
			name: "空行は読み飛ばす",
			text: "key,translation\n\n\na,b\n",
			want: [][]string{{"key", "translation"}, {"a", "b"}},
		},
		{
			// R5: レコード先頭の '#' はコメント。次の LF まで捨てる。
			name: "コメント行",
			text: "# 見出し\nkey,translation\n# --- node ---\na,b\n",
			want: [][]string{{"key", "translation"}, {"a", "b"}},
		},
		{
			name: "末尾がコメント行で改行が無いとレコードは増えない",
			text: "key,translation\na,b\n# 末尾",
			want: [][]string{{"key", "translation"}, {"a", "b"}},
		},
		{
			// R5-a / R5-b: 2列目以降の '#' と、空白のあとの '#' はただの文字。
			name: "コメントにならないシャープ",
			text: "a,#x\n #y,b\n",
			want: [][]string{{"a", "#x"}, {" #y", "b"}},
		},
		{
			// R5-d: 空の引用フィールドの直後の '#' はコメントになる（条件式の副作用）。
			name: "空の引用フィールド直後のシャープは行ごと消える",
			text: "key,translation\n\"\"#x,b\na,b\n",
			want: [][]string{{"key", "translation"}, {"a", "b"}},
		},
		{
			// R4: 引用の中の '#' はただの文字。
			name: "引用の中のシャープ",
			text: "\"#x\",b\n",
			want: [][]string{{"#x", "b"}},
		},
		{
			name: "引用の中のカンマ",
			text: "a,\"b,c\",d\n",
			want: [][]string{{"a", "b,c", "d"}},
		},
		{
			name: "引用の中の二重引用符",
			text: "a,\"b\"\"c\"\n",
			want: [][]string{{"a", `b"c`}},
		},
		{
			// R4: 引用内の CRLF は "\r\n" の2文字として残る。Go の encoding/csv は
			// ここを "\n" に潰すので一致しない。
			name: "引用の中の改行はCRLFのまま残る",
			text: "a,\"b\r\nc\"\nd,e\n",
			want: [][]string{{"a", "b\r\nc"}, {"d", "e"}},
		},
		{
			// R6: 引用はフィールド途中で開始・終了できる。引用符自体は値に入らない。
			name: "フィールド途中の引用符",
			text: "ab\"cd\"ef,\"abc\"def\n",
			want: [][]string{{"abcdef", "abcdef"}},
		},
		{
			// R8: 引用外の CR は位置を問わず捨てる。
			name: "フィールド途中のCRは消える",
			text: "a\rb,c\n",
			want: [][]string{{"ab", "c"}},
		},
		{
			name: "CRだけの行は空行扱い",
			text: "a,b\n\r\n\rc,d\n",
			want: [][]string{{"a", "b"}, {"c", "d"}},
		},
		{
			// R8 goNote: CR のみを改行に使うファイルは全体が1レコードに潰れる。
			name: "CR改行のファイルは1レコードに潰れる",
			text: "a,b\rc,d\r",
			want: [][]string{{"a", "bc", "d"}},
		},
		{
			// R9-a: ',' だけの行は2列の空レコードになる。
			name: "カンマだけの行",
			text: ",\n",
			want: [][]string{{"", ""}},
		},
		{
			// R9-c: `""` だけの行は空行として捨てられる。
			name: "空の引用フィールドだけの行",
			text: "a,b\n\"\"\nc,d\n",
			want: [][]string{{"a", "b"}, {"c", "d"}},
		},
		{
			// R11-c: 末尾が ',' で終わると空フィールドが1つ増える。
			name: "末尾がカンマ",
			text: "a,\n",
			want: [][]string{{"a", ""}},
		},
		{
			// R11-d: 引用が閉じないままEOFでも例外にしない。
			name: "閉じない引用符",
			text: "a,\"bc",
			want: [][]string{{"a", "bc"}},
		},
		{
			// R6 goNote: 引用符で囲まないフィールド中の裸の二重引用符も
			// エラーにしない。encoding/csv は LazyQuotes の設定を問わず一致しない。
			name: "裸の二重引用符",
			text: "a,he said \"hi\"\n",
			want: [][]string{{"a", "he said hi"}},
		},
		{
			name: "列数はレコードごとに変わってよい",
			text: "a,b,c\n1\n1,2,3,4\n",
			want: [][]string{{"a", "b", "c"}, {"1"}, {"1", "2", "3", "4"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCSharpRecords(tt.text)
			if !equalRecords(got, tt.want) {
				t.Errorf("ParseCSharpRecords(%q)\n = %q\nwant %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestReadCSharpRows(t *testing.T) {
	t.Run("BOMを剥がす", func(t *testing.T) {
		rows := ReadCSharpRows([]byte(bom + "key,translation\na,b\n"))
		if len(rows) != 1 {
			t.Fatalf("行数 = %d, want 1", len(rows))
		}
		if got, ok := rows[0].Lookup("key"); !ok || got != "a" {
			t.Errorf("Lookup(key) = (%q, %v), want (\"a\", true)。BOM が剥がれていない", got, ok)
		}
	})

	t.Run("空ファイルは0行", func(t *testing.T) {
		if rows := ReadCSharpRows(nil); len(rows) != 0 {
			t.Errorf("行数 = %d, want 0", len(rows))
		}
	})

	t.Run("ヘッダーだけのファイルは0行", func(t *testing.T) {
		if rows := ReadCSharpRows([]byte("key,translation\n")); len(rows) != 0 {
			t.Errorf("行数 = %d, want 0", len(rows))
		}
	})

	t.Run("コメントと空行しかないファイルは0行", func(t *testing.T) {
		if rows := ReadCSharpRows([]byte("# a\n\n# b\n")); len(rows) != 0 {
			t.Errorf("行数 = %d, want 0", len(rows))
		}
	})

	t.Run("冒頭のコメントの次の行がヘッダーになる", func(t *testing.T) {
		rows := ReadCSharpRows([]byte("# 見出し\n\nkey,translation\na,b\n"))
		if len(rows) != 1 {
			t.Fatalf("行数 = %d, want 1", len(rows))
		}
		if got := rows[0].Get("translation"); got != "b" {
			t.Errorf("Get(translation) = %q, want %q", got, "b")
		}
	})

	t.Run("ヘッダー名はトリムしない", func(t *testing.T) {
		rows := ReadCSharpRows([]byte(" key ,translation\na,b\n"))
		if len(rows) != 1 {
			t.Fatalf("行数 = %d, want 1", len(rows))
		}
		if rows[0].Has("key") {
			t.Error("Has(key) = true。元実装はヘッダー名をトリムしないので \" key \" のまま")
		}
		if got := rows[0].Get(" key "); got != "a" {
			t.Errorf("Get(\" key \") = %q, want %q", got, "a")
		}
	})

	t.Run("列名の照合は大文字小文字を区別しない", func(t *testing.T) {
		rows := ReadCSharpRows([]byte("Key,Source_EN,Translation\nk,s,t\n"))
		if len(rows) != 1 {
			t.Fatalf("行数 = %d, want 1", len(rows))
		}
		for _, name := range []string{"key", "source_en", "translation"} {
			if _, ok := rows[0].Lookup(name); !ok {
				t.Errorf("Lookup(%q) が見つからない。厳密一致で実装すると出力が全損する", name)
			}
		}
	})
}

// equalRecords は [][]string を比べる。
func equalRecords(got, want [][]string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !slices.Equal(got[i], want[i]) {
			return false
		}
	}
	return true
}
