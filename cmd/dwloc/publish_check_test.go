package main

import (
	"strings"
	"testing"
)

// TestPublishCheck は、publish --check が書き換えの要否を終了コードで返し、1バイトも
// 書かないことを見る（改善の調査の cli-15）。
//
// --dry-run は変更があっても無くても終了コード0を返すので、「publish し忘れて
// いないか」を確かめるには出力の文言を読むしかなかった。リリースの確かめ
// （release-publish.yml）も、「件中 0 件が変わります」と「[変更あり」の文字列で
// 判定し、同じ文言を Go の試験で支えていた。文言を直すたびに、ワークフローと
// 試験の両方を直す必要があった。
func TestPublishCheck(t *testing.T) {
	t.Run("書き換えが要れば 1 で、書かない", func(t *testing.T) {
		root := publishTree(t, "ja")
		before := readFile(t, root, "Translations/ja/strings.csv")
		code, stdout, stderr := runCLI("publish", "--root", root, "--no-game", "--check")
		if code != exitProblems {
			t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
		}
		checkContains(t, "stdout", stdout, []string{
			"[check] Translations/ja/strings.csv <- Translations/ja/strings.csv",
			"[変更あり: ",
			"[check] 1 件中 1 件が変わります。ファイルは書いていません。dwloc publish で書き換えてください。",
		})
		if after := readFile(t, root, "Translations/ja/strings.csv"); after != before {
			t.Errorf("--check なのに書き換えた:\n%s", after)
		}
	})

	t.Run("書き換えが要らなければ 0", func(t *testing.T) {
		root := publishTree(t, "ja", "de")
		if code, _, stderr := runCLI("publish", "--root", root, "--no-game"); code != exitOK {
			t.Fatalf("publish の終了コード = %d\n%s", code, stderr)
		}
		code, stdout, stderr := runCLI("publish", "--root", root, "--no-game", "--check")
		if code != exitOK {
			t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
		}
		checkContains(t, "stdout", stdout, []string{"[変更なし]", "[check] 2 件中 0 件が変わります。書き換えは要りません。"})
		if strings.Contains(stdout, "[変更あり") {
			t.Errorf("変わらないのに変更ありと出している:\n%s", stdout)
		}
	})

	t.Run("--dry-run と一緒でも --check の終了コード", func(t *testing.T) {
		root := publishTree(t, "ja")
		code, stdout, _ := runCLI("publish", "--root", root, "--no-game", "--check", "--dry-run")
		if code != exitProblems {
			t.Fatalf("終了コード = %d, 期待 %d\n%s", code, exitProblems, stdout)
		}
	})

	t.Run("守りで止まれば、その理由で 1", func(t *testing.T) {
		root, game := recordLossTree(t)
		code, stdout, stderr := runCLI("publish", "--root", root, "--game", game, "--check")
		if code != exitProblems {
			t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
		}
		checkContains(t, "stderr", stderr, []string{"訳が失われるので、1バイトも書きませんでした。"})
		if strings.Contains(stdout, "[check]") {
			t.Errorf("止めたのに --check の報告を出している:\n%s", stdout)
		}
	})

	t.Run("使い方に書いてある", func(t *testing.T) {
		_, stdout, _ := runCLI("publish", "--help")
		checkContains(t, "publish --help", stdout, []string{
			"[--check]",
			"  --check\n",
			"--check では、書き換えが要るロケールがあるときも 1 です",
		})
	})
}
