package edit

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// publishedCSV は公開ファイルを模した中身。見出しと空行が入っているのは、
// キーで引くときに行番号が作業コピーとそろわないことを示すためでもある。
const publishedCSV = "key,section,node,order,speaker,translation\n" +
	"# 言語名などの引き継ぎコメント\n" +
	"\n" +
	"# ===== L01 Ryan =====\n" +
	"# --- intro: Ryan_1_intro ---\n" +
	"0da72197e898ebe1,L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？\n" +
	"334d016f755cd6dc,L01 Ryan,Ryan_1_intro,2,Kobold,こんにちは！\n"

func TestWriteByKey(t *testing.T) {
	t.Run("キーで引いてその行だけを差し替える", func(t *testing.T) {
		path := writeTemp(t, publishedCSV)

		got, err := WriteByKey(path, []KeyEdit{
			{Key: "334d016f755cd6dc", Translation: "やあ！"},
		})
		if err != nil {
			t.Fatalf("WriteByKey: %v", err)
		}
		if got[0].Rows != 1 || !got[0].Why.Empty() {
			t.Fatalf("結果が違う: %+v", got[0])
		}

		after := readFile(t, path)
		want := strings.Replace(publishedCSV, "こんにちは！", "やあ！", 1)
		if after != want {
			t.Errorf("中身が違う\n--- got ---\n%s\n--- want ---\n%s", after, want)
		}
	})

	t.Run("そのキーの行が無ければ書かずに断る", func(t *testing.T) {
		path := writeTemp(t, publishedCSV)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}

		got, err := WriteByKey(path, []KeyEdit{
			{Key: "ffffffffffffffff", Translation: "あたらしい行"},
		})
		if err != nil {
			t.Fatalf("WriteByKey: %v", err)
		}
		if got[0].Rows != 0 {
			t.Errorf("書いてしまっている: %+v", got[0])
		}
		if got[0].Why.ID != reason.SaveKeyMissing {
			t.Errorf("理由が違う: %+v", got[0].Why)
		}
		if after := readFile(t, path); after != publishedCSV {
			t.Errorf("中身が変わっている:\n%s", after)
		}
		// 1行も当たらなければ書かない。更新時刻も動かさない。ファイルを
		// 見張っているゲームを、意味の無い変更で起こさないため。
		later, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !later.ModTime().Equal(info.ModTime()) {
			t.Error("更新時刻が動いている")
		}
	})

	t.Run("同じキーの行が複数あればどれも同じ訳にする", func(t *testing.T) {
		// キーは原文のハッシュなので、同じキーの行は同じ原文を指す。片方だけ
		// 直すと、同じ原文に古い訳と新しい訳が並ぶ。publish がどちらを採るかを
		// 気にしなくてよいように、どれも同じ訳にする。
		path := writeTemp(t, "key,translation\nk1,ひとつめ\nk2,べつの行\nk1,ふたつめ\n")

		got, err := WriteByKey(path, []KeyEdit{{Key: "k1", Translation: "そろえた"}})
		if err != nil {
			t.Fatalf("WriteByKey: %v", err)
		}
		if got[0].Rows != 2 {
			t.Errorf("差し替えた行数が違う: %+v", got[0])
		}
		if got[0].Why.ID != reason.SaveKeyDuplicated {
			t.Errorf("断りが違う: %+v", got[0].Why)
		}
		want := "key,translation\nk1,そろえた\nk2,べつの行\nk1,そろえた\n"
		if after := readFile(t, path); after != want {
			t.Errorf("中身が違う\n--- got ---\n%s\n--- want ---\n%s", after, want)
		}
	})

	t.Run("キーが空なら引かずに断る", func(t *testing.T) {
		path := writeTemp(t, publishedCSV)

		got, err := WriteByKey(path, []KeyEdit{{Key: "", Translation: "どこへ書く"}})
		if err != nil {
			t.Fatalf("WriteByKey: %v", err)
		}
		if got[0].Rows != 0 || got[0].Why.ID != reason.SaveNoKey {
			t.Errorf("結果が違う: %+v", got[0])
		}
		if after := readFile(t, path); after != publishedCSV {
			t.Errorf("中身が変わっている:\n%s", after)
		}
	})

	t.Run("書けない値は1行も書かずに断る", func(t *testing.T) {
		path := writeTemp(t, publishedCSV)

		got, err := WriteByKey(path, []KeyEdit{
			{Key: "0da72197e898ebe1", Translation: "1行目\n2行目"},
		})
		if err != nil {
			t.Fatalf("WriteByKey: %v", err)
		}
		if got[0].Rows != 0 || got[0].Why.ID != reason.EditNoNewline {
			t.Errorf("結果が違う: %+v", got[0])
		}
		if after := readFile(t, path); after != publishedCSV {
			t.Errorf("中身が変わっている:\n%s", after)
		}
	})

	t.Run("何件かが当たれば、当たったぶんだけ書く", func(t *testing.T) {
		path := writeTemp(t, publishedCSV)

		got, err := WriteByKey(path, []KeyEdit{
			{Key: "0da72197e898ebe1", Translation: "もしもーし"},
			{Key: "ffffffffffffffff", Translation: "行き先が無い"},
		})
		if err != nil {
			t.Fatalf("WriteByKey: %v", err)
		}
		if got[0].Rows != 1 || got[1].Rows != 0 {
			t.Fatalf("結果が違う: %+v", got)
		}
		after := readFile(t, path)
		if !strings.Contains(after, "もしもーし") {
			t.Errorf("当たった行が書かれていない:\n%s", after)
		}
		if strings.Contains(after, "行き先が無い") {
			t.Errorf("当たらない行を足している:\n%s", after)
		}
	})

	t.Run("ヘッダーが受理できないファイルは書かない", func(t *testing.T) {
		path := writeTemp(t, "a,b,c\n1,2,3\n")

		_, err := WriteByKey(path, []KeyEdit{{Key: "1", Translation: "x"}})
		if !errors.Is(err, ErrReadOnly) {
			t.Fatalf("ErrReadOnly を期待した: %v", err)
		}
	})

	t.Run("ファイルが無ければ誤りを返す", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nope.csv")

		_, err := WriteByKey(path, []KeyEdit{{Key: "k", Translation: "x"}})
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("存在しないという誤りを期待した: %v", err)
		}
	})

	t.Run("触っていない行は1バイトも変わらない", func(t *testing.T) {
		// このパッケージの約束そのもの。差し替えるのは最終フィールドだけで、
		// 見出しも空行もコメントも、行の前半も動かさない。
		path := writeTemp(t, publishedCSV)

		if _, err := WriteByKey(path, []KeyEdit{
			{Key: "0da72197e898ebe1", Translation: "もしもし？"}, // 同じ値
		}); err != nil {
			t.Fatalf("WriteByKey: %v", err)
		}
		if after := readFile(t, path); after != publishedCSV {
			t.Errorf("中身が変わっている:\n%s", after)
		}
	})
}
