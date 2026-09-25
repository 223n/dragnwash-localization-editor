package publish

import (
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
)

// TestKeyless は、publish がキーを決められずに捨てるレコード（移植仕様 R17）の判定を
// 確かめる。画面の保存（internal/edit）はこれに当たる行を編集させないので、同じ行を
// collect が捨てて集計の malformed dropped に数えることも、同じ入力で見る。原文があって
// key 列と合わない行（R15）も捨てられるが、Keyless は当てない。
func TestKeyless(t *testing.T) {
	tests := []struct {
		name, header, record string
		want                 bool
	}{
		{"key 列も原文も空", "key,source_en,translation", ",,訳", true},
		{"key 列が空白だけ", "key,translation", `"  ",訳`, true},
		{"key 列が16桁のキーでない", "key,translation", "hello,訳", true},
		{"key 列が15桁", "key,translation", "0123456789abcde,訳", true},
		{"台詞ID の接頭辞が大文字", "key,translation", "LINE:intro_1,訳", true},
		{"台詞ID の接頭辞だけ", "key,translation", "line:,訳", true},
		{"key 列の無い形で原文も空", "source_en,translation", ",訳", true},
		{"16桁のキー", "key,translation", "0123456789abcdef,訳", false},
		{"前後に空白のある16桁のキー", "key,translation", `"  0123456789abcdef  ",訳`, false},
		{"大文字の16桁のキー", "key,translation", "0123456789ABCDEF,訳", false},
		{"台詞ID", "key,translation", "line:intro_1,訳", false},
		{"原文があって key 列が空", "key,source_en,translation", ",Hello,訳", false},
		{"原文が空白だけ", "key,source_en,translation", `," ",訳`, false},
		{"原文があって key 列と合わない", "key,source_en,translation", "hello,Hello,訳", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.header + "\n" + tt.record + "\n")
			f, err := csvfile.ReadPowerShell(input)
			if err != nil {
				t.Fatal(err)
			}
			rows := f.Rows()
			if len(rows) != 1 {
				t.Fatalf("行が %d 件（1件のはず）", len(rows))
			}
			if got := Keyless(rows[0]); got != tt.want {
				t.Errorf("Keyless = %v、%v を期待", got, tt.want)
			}
			c, err := collect(input)
			if err != nil {
				t.Fatal(err)
			}
			// 原文が空なら、捨てることと Keyless が一致する。原文があれば Keyless は
			// 当たらない（捨てるのは R15 のときだけ）。
			if src := rows[0].Get(colSourceEn); src == "" && (c.stats.Dropped == 1) != tt.want {
				t.Errorf("原文が空の行で、捨てた行 %d と Keyless %v が食い違う", c.stats.Dropped, tt.want)
			}
		})
	}
}
