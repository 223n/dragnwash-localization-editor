package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// sampleDir は見本の翻訳リポジトリ（samples/harbor）。テストはパッケージの
// ディレクトリ（cmd/dwloc）をカレントにして走るので、そこからの相対で引く。
var sampleDir = filepath.Join("..", "..", "samples", "harbor")

// copySample は見本を一時ディレクトリへ写し、そのパスを返す。
//
// 写すのは2つの理由による。1つは、publish が見本そのものを書き換えないように
// するため。もう1つは、validate を git の外で走らせるためである。見本の
// Translations/_discovered/ja.working.csv はこのリポジトリにコミットしてあるので、
// その場で validate を掛けると「コミットしてはいけない作業コピー」として
// 問題（終了コード1）になる。翻訳リポジトリとしての見本の検査は、写しで行う
// （samples/README.md の手順も写しを開く）。
func copySample(t *testing.T) string {
	t.Helper()

	dst := filepath.Join(t.TempDir(), "harbor")
	if err := os.CopyFS(dst, os.DirFS(sampleDir)); err != nil {
		t.Fatalf("見本を写せない: %v", err)
	}
	return dst
}

// TestSampleHarborPassesReleaseChecks は、見本がリリースの確認を通る形を
// 保っていることを確かめる。
//
// release-publish.yml は、タグを打つ前に配る書庫を展開し、見本の写しで
// validate と publish --no-game --check を走らせる。どちらも終了コード0で
// 終わらなければ、タグも Release も作らずに止まる。その確認は release/* を
// main へマージしたあとで初めて走るので、見本が崩れたまま develop へ入ると、
// 公開の段になって止まる。ここで先に落とし、見本を変えた Pull Request のうちに
// 気付けるようにする。
//
// ワークフローも試験も、publish の出力の文言ではなく終了コードで見る
// （--check。改善の調査の cli-15）。以前は --dry-run の出力に「件中 0 件が
// 変わります」があり「[変更あり」が無いことを文字列で見ていたので、文言を
// 直すたびに、ワークフローとこの試験の両方を直す必要があった。
func TestSampleHarborPassesReleaseChecks(t *testing.T) {
	root := copySample(t)

	code, stdout, stderr := runCLI("validate", "--root", root)
	if code != exitOK {
		t.Fatalf("validate の終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
	}
	if stdout != "translations OK\n" {
		t.Errorf("validate の標準出力 = %q, 期待 %q", stdout, "translations OK\n")
	}

	code, stdout, stderr = runCLI("publish", "--root", root, "--no-game", "--check")
	if code != exitOK {
		t.Fatalf("publish --check の終了コード = %d, 期待 %d。見本の公開ファイルが publish の出力とずれていれば、"+
			"写しで dwloc publish --no-game を回した出力で置き換える\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
	}

	// --check は書かない約束。ワークフローも写しが見本と同じままかを見る。
	if err := fs.WalkDir(os.DirFS(sampleDir), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		want, err := os.ReadFile(filepath.Join(sampleDir, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		if got := readFile(t, root, path); got != string(want) {
			t.Errorf("publish --check が %s を書き換えた", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
