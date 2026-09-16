package edit

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// writeTemp はテスト用のファイルを作ってパスを返す。
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "strings.csv")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("下ごしらえに失敗した: %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	return string(data)
}

// TestSaveWritesOnlyTheEditedLine は保存後のファイルが「触った行だけ違う」
// ことを見る。
func TestSaveWritesOnlyTheEditedLine(t *testing.T) {
	path := writeTemp(t, sampleWorking)
	f, err := Open(path)
	if err != nil {
		t.Fatalf("開けない: %v", err)
	}
	if f.Path() != path {
		t.Errorf("Path が %q", f.Path())
	}

	if err := f.SetTranslation(6, "こんにちは"); err != nil {
		t.Fatalf("書き換えに失敗した: %v", err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("保存に失敗した: %v", err)
	}
	if f.Dirty() {
		t.Error("保存後に Dirty が残っている")
	}

	got := readFile(t, path)
	want := "key,section,node,order,speaker,source_en,translation\n" +
		"\n" +
		"# ===== Level 1: Ryan (Sunny) =====\n" +
		"# --- intro: Ryan_1_intro ---\n" +
		"0da72197e898ebe1,L01 Ryan,Ryan_1_intro,1,Ryan,Ah,ああ\n" +
		"334d016f755cd6dc,L01 Ryan,Ryan_1_intro,2,Kobold,\"Hi, there\",こんにちは\n" +
		"\n" +
		"# ===== UI and other text (not part of the dialogue script) =====\n" +
		"9a1b2c3d4e5f6071,UI,,,UI,Start,はじめる\n"
	if got != want {
		t.Errorf("保存後の中身が違う:\n got %q\nwant %q", got, want)
	}
	if f.Version() != hashBytes([]byte(want)) {
		t.Error("保存後の版が書いたバイト列と合わない")
	}

	// 続けてもう一度保存できる（自動保存の2回目）。
	if err := f.SetTranslation(6, "やあ"); err != nil {
		t.Fatalf("2回目の書き換えに失敗した: %v", err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("2回目の保存に失敗した: %v", err)
	}
	if readFile(t, path) == want {
		t.Error("2回目の保存が効いていない")
	}
}

// TestSaveWithoutChangesDoesNotWrite は、変えていないなら書かないことを見る。
// 更新時刻が動かないので、ファイルを見張っているゲーム側を無駄に起こさない。
func TestSaveWithoutChangesDoesNotWrite(t *testing.T) {
	path := writeTemp(t, sampleWorking)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	f, err := Open(path)
	if err != nil {
		t.Fatalf("開けない: %v", err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("保存に失敗した: %v", err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("何も変えていないのに書き込んだ")
	}
	if readFile(t, path) != sampleWorking {
		t.Error("中身が変わった")
	}
}

// TestSaveDetectsConflict は、読んだあとにファイルが変わっていたら
// 1バイトも書かないことを見る。
func TestSaveDetectsConflict(t *testing.T) {
	path := writeTemp(t, sampleWorking)
	f, err := Open(path)
	if err != nil {
		t.Fatalf("開けない: %v", err)
	}
	if err := f.SetTranslation(6, "こんにちは"); err != nil {
		t.Fatalf("書き換えに失敗した: %v", err)
	}

	// 別のツール（publish の再生成など）が書いたことにする。
	const outside = "key,translation\n0da72197e898ebe1,別の中身\n"
	if err := os.WriteFile(path, []byte(outside), 0o644); err != nil {
		t.Fatal(err)
	}

	err = f.Save()
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Save = %v, want ErrConflict", err)
	}
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Save = %v, want *ConflictError", err)
	}
	if conflict.Path != path {
		t.Errorf("Path が %q", conflict.Path)
	}
	if conflict.Want != hashBytes([]byte(sampleWorking)) {
		t.Error("読んだときの版が合わない")
	}
	if conflict.Got != hashBytes([]byte(outside)) {
		t.Error("いまの版が合わない")
	}
	if got := readFile(t, path); got != outside {
		t.Errorf("1バイトも書かないはずが書いている: %q", got)
	}
	// 編集内容は捨てない。呼び出し側が読み直して判断できるようにする。
	if !f.Dirty() {
		t.Error("競合で Dirty が落ちた")
	}
}

// TestSaveDetectsMissingFile は、保存先が消えていたら書かないことを見る。
// 消えた場所へ書き戻すと、削除したつもりのファイルが復活する。
func TestSaveDetectsMissingFile(t *testing.T) {
	path := writeTemp(t, sampleWorking)
	f, err := Open(path)
	if err != nil {
		t.Fatalf("開けない: %v", err)
	}
	if err := f.SetTranslation(6, "こんにちは"); err != nil {
		t.Fatalf("書き換えに失敗した: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	err = f.Save()
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Save = %v, want ErrConflict", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Save = %v, want fs.ErrNotExist も一致すること", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Error("消えたファイルを復活させた")
	}
}

// TestSaveRejects は保存できない場合を固定する。
func TestSaveRejects(t *testing.T) {
	t.Run("パスが無い", func(t *testing.T) {
		f := Parse([]byte(sampleWorking))
		if err := f.Save(); !errors.Is(err, ErrNoPath) {
			t.Errorf("Save = %v, want ErrNoPath", err)
		}
	})

	t.Run("読み取り専用", func(t *testing.T) {
		const content = "foo,bar\n1,2\n"
		path := writeTemp(t, content)
		f, err := Open(path)
		if err != nil {
			t.Fatalf("開けない: %v", err)
		}
		if err := f.Save(); !errors.Is(err, ErrReadOnly) {
			t.Errorf("Save = %v, want ErrReadOnly", err)
		}
		if readFile(t, path) != content {
			t.Error("読み取り専用なのに書いた")
		}
	})

	t.Run("開けないファイル", func(t *testing.T) {
		if _, err := Open(filepath.Join(t.TempDir(), "ない.csv")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Open = %v, want fs.ErrNotExist", err)
		}
	})
}

// TestSaveKeepsBOMAndCRLF は保存の往復でも BOM と改行の種類が残ることを見る。
func TestSaveKeepsBOMAndCRLF(t *testing.T) {
	const content = "\xef\xbb\xbfkey,translation\r\n" +
		"0da72197e898ebe1,古い\r\n" +
		"334d016f755cd6dc,\r\n"
	path := writeTemp(t, content)
	f, err := Open(path)
	if err != nil {
		t.Fatalf("開けない: %v", err)
	}
	if err := f.SetTranslation(3, "新しい"); err != nil {
		t.Fatalf("書き換えに失敗した: %v", err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("保存に失敗した: %v", err)
	}

	want := "\xef\xbb\xbfkey,translation\r\n" +
		"0da72197e898ebe1,古い\r\n" +
		"334d016f755cd6dc,新しい\r\n"
	if got := readFile(t, path); got != want {
		t.Errorf("保存後の中身が違う:\n got %q\nwant %q", got, want)
	}
}

// TestSaveCreatesNoLeftovers は保存後に一時ファイルが残らないことを見る。
// publish.WriteBytes は保存先と同じディレクトリに一時ファイルを作るので、
// 残ると Translations の走査に混ざる。
func TestSaveCreatesNoLeftovers(t *testing.T) {
	path := writeTemp(t, sampleWorking)
	f, err := Open(path)
	if err != nil {
		t.Fatalf("開けない: %v", err)
	}
	if err := f.SetTranslation(6, "こんにちは"); err != nil {
		t.Fatalf("書き換えに失敗した: %v", err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("保存に失敗した: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("余計なファイルが残った: %v", names)
	}
}
