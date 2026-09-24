package publish

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ここは [WriteBytes] がリンクを壊さないことの試験。
//
// 作業コピーや公開ファイルを、ゲームのフォルダーへのリンクで置く人がいる。
// 上流の tools/hash-strings.ps1 はリポジトリ側を読むので、そうしたくなる。
// 一時ファイルを出力先のパスへ rename するだけだと、リンクが普通のファイルに
// 置き換わる。訳はリンク先に届かず、ゲームはホットリロードしないのに、画面は
// 「保存済み」と出す。上流の [IO.File]::WriteAllText はリンクをたどって書く。

// symlinkOrSkip はシンボリックリンクを作る。作れなければ試験を飛ばす。
//
// Windows でファイルへのシンボリックリンクを作るには、開発者モードか管理者の
// 権限が要る。CI の Linux では必ず作れるので、そちらで確かめる。
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()

	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("シンボリックリンクを作れない（Windows では開発者モードか管理者の権限が要る）: %v", err)
		}
		t.Fatalf("シンボリックリンクを作れない: %v", err)
	}
}

// linkOrSkip はハードリンクを作る。作れなければ試験を飛ばす。
//
// NTFS と Linux のふつうのファイルシステムでは権限なしで作れる。作れないのは
// ハードリンクを持たないファイルシステム（FAT など）に一時ディレクトリがある
// ときだけである。
func linkOrSkip(t *testing.T, target, link string) {
	t.Helper()

	if err := os.Link(target, link); err != nil {
		t.Skipf("ハードリンクを作れない（一時ディレクトリのファイルシステムが持たない）: %v", err)
	}
}

// mode は path の権限を返す。Windows では権限の値を持たないので呼ばない。
func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s を調べられない: %v", path, err)
	}
	return info.Mode().Perm()
}

// TestWriteBytesFollowsSymlink は、シンボリックリンクを通して書くと、リンクを
// 残したままリンク先へ書くことを見る。
func TestWriteBytesFollowsSymlink(t *testing.T) {
	realDir := t.TempDir()
	real := filepath.Join(realDir, "ja.working.csv")
	writeFile(t, real, "古い内容\n")
	if err := os.Chmod(real, 0o640); err != nil {
		t.Fatal(err)
	}
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "ja.working.csv")
	// 相対のリンクにする。リンクのあるフォルダーから解くことも確かめる。
	rel, err := filepath.Rel(linkDir, real)
	if err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, rel, link)

	if err := WriteBytes(link, []byte("新しい内容\n")); err != nil {
		t.Fatalf("WriteBytes が失敗した: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("リンクが普通のファイルに置き換わった（%v）", info.Mode())
	}
	if got := readString(t, real); got != "新しい内容\n" {
		t.Errorf("リンク先に届いていない: %q", got)
	}
	if runtime.GOOS != "windows" {
		if got := mode(t, real); got != 0o640 {
			t.Errorf("リンク先の権限が %v に変わった。0640 を期待", got)
		}
	}
	// 一時ファイルはリンク先のフォルダーに作って、残さない。
	assertOnlyEntries(t, realDir, "ja.working.csv")
	assertOnlyEntries(t, linkDir, "ja.working.csv")
}

// TestWriteBytesRefusesDanglingSymlink は、リンク先の無いシンボリックリンクには
// 書かずに誤りを返すことを見る。
//
// 書けば、リンクが普通のファイルに置き換わるか、どこか別の場所にファイルが
// できる。どちらも、置いた人の意図とは違う。
func TestWriteBytesRefusesDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "ja.working.csv")
	symlinkOrSkip(t, filepath.Join(dir, "missing", "ja.working.csv"), link)

	if err := WriteBytes(link, []byte("新しい内容\n")); err == nil {
		t.Fatal("リンク先が無いのに誤りを返していない")
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("リンクが普通のファイルに置き換わった（%v）", info.Mode())
	}
	assertOnlyEntries(t, dir, "ja.working.csv")
}

// TestWriteBytesKeepsHardLink は、ハードリンクのあるファイルへ書くと、どの名前から
// 読んでも新しい中身になることを見る。
//
// rename で置き換えると、書いた名前だけが新しいファイルになり、もう一方の名前は
// 古い中身のまま残る。Windows では、ファイルへのシンボリックリンクより
// ハードリンクのほうが権限なしで作れるので、こちらで置く人がいる。
func TestWriteBytesKeepsHardLink(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "ja.working.csv")
	writeFile(t, name, "古い内容\n")
	if err := os.Chmod(name, 0o640); err != nil {
		t.Fatal(err)
	}
	otherDir := t.TempDir()
	other := filepath.Join(otherDir, "ja.working.csv")
	linkOrSkip(t, name, other)

	if err := WriteBytes(name, []byte("新しい内容\n")); err != nil {
		t.Fatalf("WriteBytes が失敗した: %v", err)
	}

	for _, path := range []string{name, other} {
		if got := readString(t, path); got != "新しい内容\n" {
			t.Errorf("%s の中身が %q。どちらの名前からも新しい中身を読めることを期待", path, got)
		}
	}
	a, errA := os.Stat(name)
	b, errB := os.Stat(other)
	if errA != nil || errB != nil {
		t.Fatalf("調べられない: %v %v", errA, errB)
	}
	if !os.SameFile(a, b) {
		t.Error("2つの名前が別のファイルになった（ハードリンクが切れた）")
	}
	if runtime.GOOS != "windows" {
		if got := mode(t, name); got != 0o640 {
			t.Errorf("権限が %v に変わった。0640 を期待", got)
		}
	}
	// 書く前に同じフォルダーへ用意した一時ファイルは、書き終えたら消す。
	assertOnlyEntries(t, dir, "ja.working.csv")
}

// TestWriteBytesLeavesAnUnwritableHardLinkAlone は、ハードリンクのあるファイルを
// 書き込みで開けないときは、中身を変えずに誤りを返し、一時ファイルも残さない
// ことを見る。
func TestWriteBytesLeavesAnUnwritableHardLinkAlone(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "ja.working.csv")
	writeFile(t, name, "古い内容\n")
	linkOrSkip(t, name, filepath.Join(t.TempDir(), "ja.working.csv"))
	if err := os.Chmod(name, 0o444); err != nil {
		t.Fatal(err)
	}
	// 後始末で消せるように戻す。Windows では読み取り専用のファイルを消せない。
	t.Cleanup(func() { _ = os.Chmod(name, 0o644) })
	if f, err := os.OpenFile(name, os.O_WRONLY, 0); err == nil {
		_ = f.Close()
		t.Skip("読み取り専用にしても書き込みで開ける（root で走っている）")
	}

	if err := WriteBytes(name, []byte("新しい内容\n")); err == nil {
		t.Fatal("書き込みで開けないのに誤りを返していない")
	}
	if got := readString(t, name); got != "古い内容\n" {
		t.Errorf("書けなかったのに中身が変わった: %q", got)
	}
	assertOnlyEntries(t, dir, "ja.working.csv")
}

// TestWriteBytesKeepsThePermission は、既にあるファイルの権限を引き継ぎ、新しい
// ファイルは 0644 で作ることを見る。
//
// 0600 や 0640 に絞ったファイルを書き直すたびに 0644 へ広げると、絞った意図が
// 黙って消える。Windows は権限の値を持たないので飛ばす。
func TestWriteBytesKeepsThePermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows のファイルは Unix の権限の値を持たない")
	}
	dir := t.TempDir()
	narrow := filepath.Join(dir, "narrow.csv")
	writeFile(t, narrow, "古い内容\n")
	if err := os.Chmod(narrow, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteBytes(narrow, []byte("新しい内容\n")); err != nil {
		t.Fatalf("WriteBytes が失敗した: %v", err)
	}
	if got := mode(t, narrow); got != 0o600 {
		t.Errorf("権限が %v に変わった。0600 を期待", got)
	}

	fresh := filepath.Join(dir, "fresh.csv")
	if err := WriteBytes(fresh, []byte("新しい内容\n")); err != nil {
		t.Fatalf("WriteBytes が失敗した: %v", err)
	}
	if got := mode(t, fresh); got != 0o644 {
		t.Errorf("新しいファイルの権限が %v。0644 を期待", got)
	}
}
