package main

import (
	"os"
	"path/filepath"
	"strings"
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
// validate と publish --no-game --dry-run を走らせる。前者が終了コード0、
// 後者が「変更なし」で終わらなければ、タグも Release も作らずに止まる。
// その確認は release/* を main へマージしたあとで初めて走るので、見本が
// 崩れたまま develop へ入ると、公開の段になって止まる。ここで先に落とし、
// 見本を変えた Pull Request のうちに気付けるようにする。
//
// 出力の文言も確かめるのは、ワークフローが publish の出力を文字列で見ている
// ためである（「件中 0 件が変わります」があり、「[変更あり」が無いこと）。
// 文言を変えるときは、release-publish.yml の「作った書庫を展開して動かす」も直す。
func TestSampleHarborPassesReleaseChecks(t *testing.T) {
	root := copySample(t)

	code, stdout, stderr := runCLI("validate", "--root", root)
	if code != exitOK {
		t.Fatalf("validate の終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
	}
	if stdout != "translations OK\n" {
		t.Errorf("validate の標準出力 = %q, 期待 %q", stdout, "translations OK\n")
	}

	code, stdout, stderr = runCLI("publish", "--root", root, "--no-game", "--dry-run")
	if code != exitOK {
		t.Fatalf("publish --dry-run の終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
	}
	checkContains(t, "publish --dry-run の標準出力", stdout, []string{"[変更なし]", "件中 0 件が変わります"})
	if strings.Contains(stdout, "[変更あり") {
		t.Errorf("見本の公開ファイルが publish の出力とずれている。写しで dwloc publish --no-game を回した出力で置き換える\n%s", stdout)
	}
}
