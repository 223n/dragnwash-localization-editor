package sourcerepo

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

// TestLocales は、ロケールの名前を Translations 直下のディレクトリから数えることと、
// 読めないときや少なすぎるときに落とすことを見る。
func TestLocales(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Translations")
	var want []string
	for i := range MinLocales {
		name := "l" + strconv.Itoa(10+i)
		want = append(want, name)
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// '_' で始まるディレクトリとファイルは数えない。
	if err := os.MkdirAll(filepath.Join(dir, "_discovered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var f fakeTB
	if got := Locales(&f, root); !slices.Equal(got, want) || f.fatal != "" {
		t.Errorf("Locales = %q（fatal %q）、%q を期待", got, f.fatal, want)
	}

	t.Run("少なすぎれば落とす", func(t *testing.T) {
		if err := os.RemoveAll(filepath.Join(dir, want[0])); err != nil {
			t.Fatal(err)
		}
		var f fakeTB
		if got := Locales(&f, root); got != nil || f.fatal == "" {
			t.Errorf("Locales = %q（fatal %q）、落とすことを期待", got, f.fatal)
		}
	})

	t.Run("読めなければ落とす", func(t *testing.T) {
		var f fakeTB
		if got := Locales(&f, filepath.Join(root, "nope")); got != nil || f.fatal == "" {
			t.Errorf("Locales = %q（fatal %q）、落とすことを期待", got, f.fatal)
		}
	})
}

// TestContentLines は、BOM を除き、空行と '#' で始まる行を飛ばして、物理行の番号を
// 添えて返すことを見る。
func TestContentLines(t *testing.T) {
	data := "\xef\xbb\xbfkey,translation\r\n# ===== 見出し =====\r\n\r\na,b\n\n#c\nd,e"
	got := ContentLines([]byte(data))
	want := []Line{{1, "key,translation"}, {4, "a,b"}, {7, "d,e"}}
	if !slices.Equal(got, want) {
		t.Errorf("ContentLines = %+v, want %+v", got, want)
	}
}

// TestPlainFields は、引用符の無い行だけをカンマで分けることを見る。
func TestPlainFields(t *testing.T) {
	if got, ok := PlainFields("a,,b"); !ok || !slices.Equal(got, []string{"a", "", "b"}) {
		t.Errorf("PlainFields = %q, %v", got, ok)
	}
	if got, ok := PlainFields(`a,"b,c"`); ok || got != nil {
		t.Errorf("引用符のある行を分けた: %q, %v", got, ok)
	}
}
