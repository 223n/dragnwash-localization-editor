package publish

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
)

// TestSourceLineEndHints は、原文の CRLF を LF にするとキーが一致する行の見つけ方を
// 固定する（決まったことのそのほか 7）。
//
// 表計算ソフトなどで作業コピーを保存し直すと、原文の中の改行が LF から CRLF に
// 変わる。キーは LF の原文から計算したものなので合わなくなり、publish はその行を
// 捨てる（上流と同じ）。止めはせず、その行を知らせる。
func TestSourceLineEndHints(t *testing.T) {
	const multi = "para1\n\npara2"
	crlf := "para1\r\n\r\npara2"
	keyMulti := key.For(multi)
	tests := []struct {
		name    string
		working string
		want    []SourceLineEndHint
	}{
		{
			// ml-source-crlf-key-mismatch と同じ形。
			name: "原文の LF が CRLF になった",
			working: "key,section,node,order,speaker,source_en,translation\r\n" +
				keyMulti + ",UI,,,UI,\"" + crlf + "\",段落の訳\r\n",
			want: []SourceLineEndHint{{Line: 2, EndLine: 4, Key: keyMulti}},
		},
		{
			// key の綴りの大小と前後の空白は publish と同じく見ない。報告には元の綴りで出す。
			name:    "key が大文字で空白がある",
			working: "key,source_en,translation\n " + "F2EA4A1F0E4E8626" + " ,\"" + crlf + "\",訳\n",
			want:    []SourceLineEndHint{{Line: 2, EndLine: 4, Key: "F2EA4A1F0E4E8626"}},
		},
		{
			// 訳が空なら、公開されないのはもともとで、知らせることが無い。
			name:    "訳が空",
			working: "key,source_en,translation\n" + keyMulti + ",\"" + crlf + "\",\n",
		},
		{
			// 原文のハッシュがキーと合えば捨てられない。
			name:    "LF のまま",
			working: "key,source_en,translation\n" + keyMulti + ",\"" + multi + "\",訳\n",
		},
		{
			// key が空なら原文のハッシュをキーにするので捨てられない。
			name:    "key が空",
			working: "key,source_en,translation\n,\"" + crlf + "\",訳\n",
		},
		{
			// CRLF を LF にしてもキーと合わないなら、別の原因で捨てられている。
			name:    "LF にしても合わない",
			working: "key,source_en,translation\n" + key.For("other") + ",\"" + crlf + "\",訳\n",
		},
		{
			// 原文に CRLF が無い食い違いは、この知らせの話ではない。
			name:    "原文に CRLF が無い",
			working: "key,source_en,translation\n" + key.For("other") + ",hello,訳\n",
		},
		{
			name:    "台詞ID行",
			working: "key,source_en,translation\nline:aaaaaaaa,\"" + crlf + "\",訳\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "in.csv")
			writeFile(t, path, tt.working)
			got, err := SourceLineEndHints(Target{Input: path})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}

	t.Run("読めなければ誤りを返す", func(t *testing.T) {
		if _, err := SourceLineEndHints(Target{Input: t.TempDir()}); err == nil {
			t.Error("ディレクトリを読めたことにしている")
		}
		path := filepath.Join(t.TempDir(), "in.csv")
		writeFile(t, path, "key,source_en,translation\n"+keyMulti+",\"x\n")
		var unclosed *csvfile.UnclosedQuoteError
		if _, err := SourceLineEndHints(Target{Input: path}); !errors.As(err, &unclosed) {
			t.Errorf("閉じない引用符を誤りにしない: %v", err)
		}
	})
}
