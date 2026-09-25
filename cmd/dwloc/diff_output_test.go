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
//
// --no-game のときと、ゲームが見つからないときは、ゲームの Translations を守りの
// フォルダーに数えられない。それでもゲームの作業コピーを報告で上書きしないよう、
// 場所に関わらず、dwloc が読むファイルの名前では書かない。
func TestRunDiffOutputRefusesWhatDiffReads(t *testing.T) {
	root := recordDiffTree(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": "key,source_en,translation\n",
	})
	elsewhere := t.TempDir()
	published := readFile(t, root, "Translations/ja/strings.csv")
	const (
		inFolder = "--output には、diff が読むフォルダーの中を指定できません"
		byName   = "--output には、dwloc が読むファイルの名前を使えません"
	)
	gameWorking := filepath.Join(game, "Translations", "_discovered", "ja.working.csv")
	tests := []struct {
		name string
		out  string
		args []string
		want string
	}{
		{"公開ファイル", filepath.Join(root, "Translations", "ja", "strings.csv"), []string{"--no-game"}, inFolder},
		{"新しいロケールになる場所", filepath.Join(root, "Translations", "xx", "strings.csv"), []string{"--no-game"}, inFolder},
		{"作業コピーのフォルダー", filepath.Join(root, "Translations", "_discovered", "report.csv"), []string{"--no-game"}, inFolder},
		{"再生順のフォルダー", filepath.Join(root, "data", "report.csv"), []string{"--no-game"}, inFolder},
		{"ゲームの Translations", gameWorking, []string{"--game", game}, inFolder},
		{"--no-game でゲームの作業コピー", gameWorking, []string{"--no-game"}, byName},
		{"ゲームが見つからないときのゲームの作業コピー", gameWorking, nil, byName},
		{"--no-game でゲームの公開ファイルになる場所", filepath.Join(game, "Translations", "ja", "strings.csv"), []string{"--no-game"}, byName},
		{"ほかの場所の公開ファイルの名前", filepath.Join(elsewhere, "strings.csv"), []string{"--no-game"}, byName},
		{"ほかの場所の作業コピーの名前（大文字）", filepath.Join(elsewhere, "JA.Working.CSV"), []string{"--no-game"}, byName},
		{"はみ出しの記録の名前", filepath.Join(elsewhere, "layout_risks.csv"), []string{"--no-game"}, byName},
		{"再生順の名前", filepath.Join(elsewhere, "script_order.csv"), []string{"--no-game"}, byName},
		{"見出しの表の名前", filepath.Join(elsewhere, "level_flow.csv"), []string{"--no-game"}, byName},
		{"末尾の点と空白（Windows は落として開く）", filepath.Join(elsewhere, "strings.csv. "), []string{"--no-game"}, byName},
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
	if names, err := os.ReadDir(elsewhere); err != nil || len(names) > 0 {
		t.Errorf("ほかの場所に書いている: %v %v", names, err)
	}
}

// TestRunDiffOutputAcceptsOtherNames は、dwloc が読む名前に似ているだけの名前には、
// 止めずに書くことを見る。名前の守りは、読む名前そのものだけに当てる。
func TestRunDiffOutputAcceptsOtherNames(t *testing.T) {
	root := recordDiffTree(t)
	dir := t.TempDir()
	for _, name := range []string{"strings.csv.bak", "my-strings.csv", "working.csv.txt", "ja.working.txt"} {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(dir, name)
			code, _, stderr := runCLI("diff", "--root", root, "--no-game", "--format", "csv", "--output", out)
			if code != exitProblems {
				t.Fatalf("終了コード = %d, 期待 %d\nstderr:\n%s", code, exitProblems, stderr)
			}
			if _, err := os.Stat(out); err != nil {
				t.Errorf("%s に書いていない: %v", name, err)
			}
		})
	}
}

// TestRunDiffOutputRefusesALinkIntoTranslations は、Translations の外に置いたリンクが
// 公開ファイルを指しているとき、リンク先へ書かずに止めることを見る。書き出しは
// リンクを残したままリンク先へ書く（publish.WriteBytes）ので、リンクの置き場だけを
// 見ると公開ファイルを上書きする。リンクを作れない環境（権限の無い Windows）では飛ばす。
func TestRunDiffOutputRefusesALinkIntoTranslations(t *testing.T) {
	root := recordDiffTree(t)
	published := filepath.Join(root, "Translations", "ja", "strings.csv")
	before := readFile(t, root, "Translations/ja/strings.csv")
	link := filepath.Join(t.TempDir(), "report.csv")
	if err := os.Symlink(published, link); err != nil {
		t.Skipf("シンボリックリンクを作れない: %v", err)
	}
	code, stdout, stderr := runCLI("diff", "--root", root, "--no-game", "--format", "csv", "--output", link)
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{"--output には、diff が読むフォルダーの中を指定できません"})
	if got := readFile(t, root, "Translations/ja/strings.csv"); got != before {
		t.Errorf("リンク先の公開ファイルが書き換わった:\n%s", got)
	}
}

// TestRunDiffOutputReportsWriteFailure は、--output に書けなかったとき（書き出し先が
// フォルダーだった、など）に終了コード2で止まり、どこに書けなかったかを出すことを見る。
//
// 記録（logs/dwloc_<日付>.log）にも、書けなかったと残す。本文を省いたことの1行が
// 「--output のファイルに書きました」のままだと、記録を添えた報告を読む人は、
// ファイルができたと読む。
func TestRunDiffOutputReportsWriteFailure(t *testing.T) {
	resetRecord(t)
	root := recordDiffTree(t)
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir(filepath.Join(dir, "report.csv"), 0o755); err != nil {
		t.Fatal(err)
	}
	setArgs(t, "diff", "--root", root, "--no-game", "--format", "csv", "--output", "report.csv")

	read := captureStd(t)
	code := mainWithRecord()
	stdout, stderr := read()
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{"dwloc: 結果を report.csv に書き出せません: "})
	_, log := readLogs(t, dir)
	checkContains(t, "記録", log, []string{
		"dwloc: 結果を report.csv に書き出せません: ",
		"（--output のファイルには書けませんでした）",
	})
	for label, got := range map[string]string{"標準エラー": stderr, "記録": log} {
		if strings.Contains(got, "に書きました") {
			t.Errorf("書けなかったのに、%sで書いたと言っている:\n%s", label, got)
		}
	}
}

// TestRunDiffOutputIsNotRecorded は、--output で書いた本文を、画面に出したときと
// 同じく記録（logs/dwloc_<日付>.log）へ写さないことを見る（改善の決定 1）。
// 記録には、text の見出しと件数と、本文を省いたことの1行を残す。csv は1行も写さない。
//
// csv の行は8桁の字下げで始まらないので、text の見出しを選ぶ関数（diffHeadingLine）を
// csv にも当てると、全行が見出しとして記録に入る。形式ごとに見るのはそのためである。
func TestRunDiffOutputIsNotRecorded(t *testing.T) {
	tests := []struct {
		name string
		args []string
		out  string
		// wantLog は記録に残っていてほしい文字列。
		wantLog []string
	}{
		{"text", nil, "report.txt", []string{"要確認が 1 行あります。"}},
		{"csv", []string{"--format", "csv"}, "report.csv", nil},
		{"csv --raw-csv", []string{"--format", "csv", "--raw-csv"}, "report.csv", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetRecord(t)
			root := recordDiffTree(t)
			dir := t.TempDir()
			t.Chdir(dir)
			setArgs(t, append([]string{"diff", "--root", root, "--no-game", "--output", tt.out}, tt.args...)...)

			read := captureStd(t)
			code := mainWithRecord()
			stdout, stderr := read()
			if code != exitProblems {
				t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
			}
			got := readFile(t, dir, tt.out)
			_, log := readLogs(t, dir)
			for _, secret := range []string{recordSource, recordVanished} {
				if !strings.Contains(got, secret) {
					t.Errorf("--output のファイルに %q が無い（試験の前提が崩れている）", secret)
				}
				if strings.Contains(log, secret) {
					t.Errorf("記録に %q が入っている\n--- 記録 ---\n%s", secret, log)
				}
			}
			// csv のヘッダー（source_en と translation の列名を持つ行）も写さない。
			// 見本の原文と訳だけを探すと、行の写り方が変わったときに見落とす。
			if strings.Contains(log, "source_en") {
				t.Errorf("記録に csv のヘッダーか行が入っている\n--- 記録 ---\n%s", log)
			}
			checkContains(t, "記録", log, append([]string{
				"（--output のファイルに書きました）",
				"dwloc: 結果を " + tt.out + " に書きました。",
			}, tt.wantLog...))
		})
	}
}

// TestRunDiffOutputUsage は、使い方が --output と BOM の扱いを書いていることを見る。
func TestRunDiffOutputUsage(t *testing.T) {
	_, stdout, _ := runCLI("diff", "--help")
	checkContains(t, "diff --help", stdout, []string{
		"[--output <ファイル>]",
		"  --output <ファイル>\n",
		"BOM",
		"PowerShell 5.1",
		"dwloc が読むファイルの名前",
	})
	// 使い方に並べた名前は、守りが断る名前（dwlocReadNames）と同じにする。
	// 片方だけを足し引きすると、使い方を読んだ人が守りの範囲を取り違える。
	flat := strings.Join(strings.Fields(stdout), "")
	want := "（" + strings.Join(dwlocReadNames(), "、") + "）"
	if !strings.Contains(flat, want) {
		t.Errorf("diff --help に守りの名前の並び %q が無い:\n%s", want, stdout)
	}
}
