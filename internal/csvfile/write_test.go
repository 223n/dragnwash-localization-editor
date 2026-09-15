package csvfile

import (
	"strings"
	"testing"
)

func TestEscapeField(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"素通し", "abc", "abc"},
		{"空文字は空のまま", "", ""},
		{"カンマを含む", "a,b", `"a,b"`},
		{"二重引用符を含む", `a"b`, `"a""b"`},
		{"二重引用符だけ", `"`, `""""`},
		{"LFを含む", "a\nb", "\"a\nb\""},
		{"CRを含む", "a\rb", "\"a\rb\""},
		{"CRLFを含む", "a\r\nb", "\"a\r\nb\""},
		// 以下は「引用しない」ことの確認。Go の csv.Writer は先頭が空白の
		// フィールドや `\.` を引用するため、ここで出力が食い違う。
		{"先頭の空白は引用しない", " a", " a"},
		{"末尾の空白は引用しない", "a ", "a "},
		{"タブは引用しない", "a\tb", "a\tb"},
		{"セミコロンは引用しない", "a;b", "a;b"},
		{"シャープは引用しない", "#a", "#a"},
		{"バックスラッシュとドットは引用しない", `\.`, `\.`},
		{
			// 実データ ja/strings.csv:69 の訳文。タグ属性が二重引用符を含むので
			// 引用対象になり、内部の " が "" に倍化される。
			name: "実データの書式タグ",
			in:   `<gradient="gold">金</gradient>`,
			want: `"<gradient=""gold"">金</gradient>"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EscapeField(tt.in); got != tt.want {
				t.Errorf("EscapeField(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestJoinFieldsAndAppendLine(t *testing.T) {
	tests := []struct {
		name   string
		fields []string
		want   string
	}{
		{"通常の行", []string{"key", "section", "訳"}, "key,section,訳"},
		{"空フィールド", []string{"a", "", "b"}, "a,,b"},
		{"引用が要る行", []string{"a", `b"c`, "d,e"}, `a,"b""c","d,e"`},
		{"フィールド1つ", []string{"a"}, "a"},
		{"フィールド0個", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JoinFields(tt.fields...); got != tt.want {
				t.Errorf("JoinFields(%q) = %q, want %q", tt.fields, got, tt.want)
			}
			// AppendLine は同じ内容に LF を足したものになる。CRLF にはしない。
			got := string(AppendLine(nil, tt.fields...))
			if want := tt.want + "\n"; got != want {
				t.Errorf("AppendLine(nil, %q) = %q, want %q", tt.fields, got, want)
			}
		})
	}
}

func TestAppendLineAccumulates(t *testing.T) {
	var buf []byte
	buf = AppendLine(buf, "key", "translation")
	buf = AppendLine(buf, "0da72197e898ebe1", "もしもし？")
	want := "key,translation\n0da72197e898ebe1,もしもし？\n"
	if string(buf) != want {
		t.Errorf("AppendLine の積み上げ = %q, want %q", buf, want)
	}
	if strings.Contains(string(buf), "\r") {
		t.Error("出力に CR が混ざっている。改行は LF 固定")
	}
}

func TestWriteLine(t *testing.T) {
	var sb strings.Builder
	if err := WriteLine(&sb, "a", "b,c"); err != nil {
		t.Fatalf("WriteLine が失敗した: %v", err)
	}
	if got, want := sb.String(), "a,\"b,c\"\n"; got != want {
		t.Errorf("WriteLine = %q, want %q", got, want)
	}
}

// TestRoundTripIsBrokenForLeadingHash は、'#' で始まる値を先頭列に書くと
// 読み戻しで消えるという既知の非対称（移植仕様 R14）を、仕様として固定する。
// 直すと既存の公開ファイルとバイト非互換になるため、直さないことを記録しておく。
func TestRoundTripIsBrokenForLeadingHash(t *testing.T) {
	line := string(AppendLine(nil, "#key", "value"))
	if line != "#key,value\n" {
		t.Fatalf("書き出しが %q。'#' は引用されない", line)
	}

	text := "key,value\n" + line
	if rows := ReadCSharpRows([]byte(text)); len(rows) != 0 {
		t.Errorf("C# 方式で読み戻すと %d 行。コメント行として消えるのが元実装の挙動", len(rows))
	}
	rows, err := ReadPowerShellRows([]byte(text))
	if err != nil {
		t.Fatalf("ReadPowerShellRows が失敗した: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("PowerShell 方式で読み戻すと %d 行。コメント行として消えるのが元実装の挙動", len(rows))
	}
}
