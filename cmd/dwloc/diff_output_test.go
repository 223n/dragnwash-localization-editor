package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// utf8BOM は UTF-8 の BOM。diff --output が csv の頭に付ける。
const utf8BOM = "\xef\xbb\xbf"

// TestRunDiffOutputWritesTheBytes は、diff --output がバイト列をそのままファイルへ書く
// ことを見る（改善の決定 8、改善の調査の cli-3）。
//
// Windows PowerShell 5.1 で diff の出力を > でファイルへ送ると、コンソールの
// コードページで読み直して UTF-16 で書くので、日本語が化け、化けた字が改行を
// 飲み込んで CSV の行がつながる。--output は、標準出力を通さずに書く道である。
// csv には BOM を付ける（表計算ソフトが UTF-8 と見分けて開けるように）。text には
// 付けない。本文は標準出力には出さない。
func TestRunDiffOutputWritesTheBytes(t *testing.T) {
	root := recordDiffTree(t)
	for _, tc := range []struct {
		format string
		bom    string
	}{
		{diffFormatCSV, utf8BOM},
		{diffFormatText, ""},
	} {
		t.Run(tc.format, func(t *testing.T) {
			wantCode, want, _ := runCLI("diff", "--root", root, "--no-game", "--format", tc.format)
			out := filepath.Join(t.TempDir(), "report."+tc.format)
			code, stdout, stderr := runCLI("diff", "--root", root, "--no-game", "--format", tc.format, "--output", out)
			if code != wantCode {
				t.Fatalf("終了コード = %d, --output なしと同じ %d を期待\nstderr:\n%s", code, wantCode, stderr)
			}
			if stdout != "" {
				t.Errorf("--output なのに標準出力へ本文を書いている:\n%s", stdout)
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("--output のファイルを読めない: %v", err)
			}
			if string(got) != tc.bom+want {
				t.Errorf("--output のファイルが、標準出力の本文と違う\n got %q\nwant %q", got, tc.bom+want)
			}
			// 原文と訳は、日本語のまま1バイトも変わらずに入る。
			checkContains(t, "--output のファイル", string(got), []string{recordSource, recordVanished})
			checkContains(t, "標準エラー", stderr, []string{"dwloc: 結果を " + filepath.ToSlash(out) + " に書きました。"})
		})
	}
}

// TestRunDiffOutputRefusesWhatDiffReads は、diff --output が、翻訳リポジトリの
// Translations と data の中と、ゲームの Translations の中には書かないことを見る。
//
// そこは diff・publish・edit が読む場所で、公開ファイルや作業コピーを報告で
// 上書きすると訳を失う。まだ無い名前でも、書くと次の実行から公開ファイルや
// 作業コピーとして読まれる（Translations/xx/strings.csv なら新しいロケールになる）。
// 書き出し先のフォルダーが無いときも、作らずに止める（打ち間違いで別の場所へ
// 書かないように）。
func TestRunDiffOutputRefusesWhatDiffReads(t *testing.T) {
	root := recordDiffTree(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": "key,source_en,translation\n",
	})
	published := readFile(t, root, "Translations/ja/strings.csv")
	tests := []struct {
		name string
		out  string
		args []string
		want string
	}{
		{"公開ファイル", filepath.Join(root, "Translations", "ja", "strings.csv"), []string{"--no-game"},
			"--output には、diff が読むフォルダーの中を指定できません"},
		{"新しいロケールになる場所", filepath.Join(root, "Translations", "xx", "strings.csv"), []string{"--no-game"},
			"--output には、diff が読むフォルダーの中を指定できません"},
		{"作業コピーのフォルダー", filepath.Join(root, "Translations", "_discovered", "report.csv"), []string{"--no-game"},
			"--output には、diff が読むフォルダーの中を指定できません"},
		{"再生順のフォルダー", filepath.Join(root, "data", "report.csv"), []string{"--no-game"},
			"--output には、diff が読むフォルダーの中を指定できません"},
		{"ゲームの Translations", filepath.Join(game, "Translations", "_discovered", "ja.working.csv"), []string{"--game", game},
			"--output には、diff が読むフォルダーの中を指定できません"},
		{"無いフォルダー", filepath.Join(root, "nope", "report.csv"), []string{"--no-game"},
			"--output の書き出し先のフォルダーがありません"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"diff", "--root", root, "--format", "csv", "--output", tt.out}, tt.args...)
			code, stdout, stderr := runCLI(args...)
			if code != exitError {
				t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
			}
			checkContains(t, "標準エラー", stderr, []string{tt.want})
			if stdout != "" {
				t.Errorf("止めたのに標準出力へ書いている:\n%s", stdout)
			}
		})
	}
	if got := readFile(t, root, "Translations/ja/strings.csv"); got != published {
		t.Errorf("公開ファイルが書き換わった:\n%s", got)
	}
	for _, p := range []string{
		filepath.Join(root, "Translations", "xx"),
		filepath.Join(root, "Translations", "_discovered", "report.csv"),
		filepath.Join(root, "data", "report.csv"),
		filepath.Join(root, "nope"),
	} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s ができている", p)
		}
	}
	if got := readFile(t, game, "Translations/_discovered/ja.working.csv"); got != "key,source_en,translation\n" {
		t.Errorf("ゲームの作業コピーが書き換わった:\n%s", got)
	}
}

// TestRunDiffOutputReportsWriteFailure は、--output に書けなかったとき（書き出し先が
// フォルダーだった、など）に終了コード2で止まり、どこに書けなかったかを出すことを見る。
func TestRunDiffOutputReportsWriteFailure(t *testing.T) {
	root := recordDiffTree(t)
	dir := filepath.Join(t.TempDir(), "report.csv")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLI("diff", "--root", root, "--no-game", "--format", "csv", "--output", dir)
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{"dwloc: 結果を " + filepath.ToSlash(dir) + " に書き出せません: "})
	if strings.Contains(stderr, "に書きました") {
		t.Errorf("書けなかったのに書いたと出している:\n%s", stderr)
	}
}

// TestRunDiffOutputIsNotRecorded は、--output で書いた本文を、画面に出したときと
// 同じく記録（logs/dwloc_<日付>.log）へ写さないことを見る。記録には、text の見出しと
// 件数と、本文を省いたことの1行を残す。
func TestRunDiffOutputIsNotRecorded(t *testing.T) {
	resetRecord(t)
	root := recordDiffTree(t)
	dir := t.TempDir()
	t.Chdir(dir)
	setArgs(t, "diff", "--root", root, "--no-game", "--output", "report.txt")

	read := captureStd(t)
	code := mainWithRecord()
	stdout, stderr := read()
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	got := readFile(t, dir, "report.txt")
	_, log := readLogs(t, dir)
	for _, secret := range []string{recordSource, recordVanished} {
		if !strings.Contains(got, secret) {
			t.Errorf("--output のファイルに %q が無い（試験の前提が崩れている）", secret)
		}
		if strings.Contains(log, secret) {
			t.Errorf("記録に %q が入っている\n--- 記録 ---\n%s", secret, log)
		}
	}
	checkContains(t, "記録", log, []string{
		"要確認が 1 行あります。",
		"（--output のファイルに書きました）",
		"dwloc: 結果を report.txt に書きました。",
	})
}

// TestRunDiffOutputUsage は、使い方が --output と BOM の扱いを書いていることを見る。
func TestRunDiffOutputUsage(t *testing.T) {
	_, stdout, _ := runCLI("diff", "--help")
	checkContains(t, "diff --help", stdout, []string{
		"[--output <ファイル>]",
		"  --output <ファイル>\n",
		"BOM",
		"PowerShell 5.1",
	})
}
