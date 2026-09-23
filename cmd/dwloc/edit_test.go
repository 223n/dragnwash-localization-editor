package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// editTree は edit を試すための最小のリポジトリを作る。
func editTree(t *testing.T) string {
	t.Helper()
	return makeTree(t, map[string]string{
		"data/script_order.csv": diffOrderCSV,
		"Translations/ja/strings.csv": publish.HeaderLine + "\n" +
			diffHelloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
	})
}

// runEditArgs は runEdit を呼んで、終了コードと出力を返す。
func runEditArgs(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	// 第3引数は共通オプションで受けた --game の既定値。空なら見に行かない。
	code := runEdit(args, ".", "", &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRunEditHelp(t *testing.T) {
	code, stdout, stderr := runEditArgs(t, "--help")
	if code != exitOK {
		t.Errorf("終了コードが %d", code)
	}
	if !strings.HasPrefix(stdout, "使い方: dwloc edit") {
		t.Errorf("使い方が標準出力に出ていない:\n%s", stdout)
	}
	if stderr != "" {
		t.Errorf("--help で標準エラーに出ている:\n%s", stderr)
	}
	// 保存の性質を、使い方の時点で伝える。どこへ書くのか、publish を回すのか、
	// 手前でファイルが変わっていたらどうなるのか。起動する前に読めるようにする。
	for _, want := range []string{"自動で保存", "publish は回しません", "1バイトも"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("使い方に %q が書かれていない", want)
		}
	}
	// 待ち受ける先が 127.0.0.1 に固定であることも、使う前に読めるようにする。
	if !strings.Contains(stdout, "127.0.0.1") {
		t.Error("使い方に待ち受け先が書かれていない")
	}
}

func TestRunEditRejectsBadFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"ポートが範囲外", []string{"--port", "70000"}, "--port"},
		{"ポートが負", []string{"--port", "-1"}, "--port"},
		{"待ち時間が負", []string{"--idle-timeout", "-1m"}, "--idle-timeout"},
		{"余分な引数", []string{"ja"}, "余分な引数"},
	}
	for _, tc := range cases {
		code, _, stderr := runEditArgs(t, tc.args...)
		if code != exitError {
			t.Errorf("%s: 終了コードが %d、%d を期待", tc.name, code, exitError)
		}
		if !strings.Contains(stderr, tc.want) {
			t.Errorf("%s: 標準エラーに %q が無い:\n%s", tc.name, tc.want, stderr)
		}
	}
}

func TestRunEditRejectsUnknownLocale(t *testing.T) {
	root := editTree(t)
	code, _, stderr := runEditArgs(t, "--root", root, "--locale", "nope", "--no-browser")
	if code != exitError {
		t.Fatalf("終了コードが %d", code)
	}
	// 文面は publish / diff の --locale とそろえる。1か所で保つため
	// selectLocales に通している。
	if !strings.Contains(stderr, "対象にできるのは") {
		t.Errorf("標準エラーが %q", stderr)
	}
}

// TestRunEditLocaleWithoutTranslations は、--locale を照合する段で Translations を
// 読めなければ、待ち受けを始めずに終了コード2で止まることを見る。
//
// 待ち受けを立ててから止めると、URL が出たあとに落ちる。開いた画面が何も
// 出せないまま残る。
func TestRunEditLocaleWithoutTranslations(t *testing.T) {
	root := makeTree(t, map[string]string{"data/script_order.csv": diffOrderCSV})
	code, stdout, stderr := runEditArgs(t, "--root", root, "--locale", "ja", "--no-browser")
	if code != exitError {
		t.Fatalf("終了コードが %d、%d を期待\n%s", code, exitError, stderr)
	}
	if !strings.Contains(stderr, "Translations を読めません") {
		t.Errorf("理由が出ていない:\n%s", stderr)
	}
	if strings.Contains(stdout, "http://127.0.0.1:") {
		t.Errorf("待ち受けを始めている:\n%s", stdout)
	}
}

func TestRunEditAcceptsLocaleCaseInsensitively(t *testing.T) {
	// pt-BR や zh-Hant のように大文字を含む名前があり、Windows では大小が
	// 保たれないまま打たれることがある。publish / diff と同じ緩め方にする。
	root := editTree(t)
	code, _, stderr := runEditArgs(t, "--root", root, "--locale", "JA",
		"--no-browser", "--idle-timeout", "1ns")
	if code != exitOK {
		t.Fatalf("終了コードが %d:\n%s", code, stderr)
	}
}

func TestRunEditStopsWhenIdle(t *testing.T) {
	// --idle-timeout を極端に短くして、自分で終わることを確かめる。
	// 翻訳者の PC に待ち受けを残さないための仕掛けなので、実際に終わることを見る。
	root := editTree(t)
	code, stdout, stderr := runEditArgs(t, "--root", root, "--no-browser", "--idle-timeout", "1ns")
	if code != exitOK {
		t.Fatalf("終了コードが %d:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "http://127.0.0.1:") {
		t.Errorf("URL が標準出力に出ていない:\n%s", stdout)
	}
	if !strings.Contains(stdout, "?t=") {
		t.Errorf("トークンが URL に載っていない:\n%s", stdout)
	}
}

func TestRunEditPrintsURLEvenWithoutBrowser(t *testing.T) {
	// WSL や Steam Deck ではブラウザーが開かない。URL は必ず出す。
	root := editTree(t)
	_, stdout, _ := runEditArgs(t, "--root", root, "--no-browser", "--idle-timeout", "1ns")
	if !strings.Contains(stdout, "ブラウザーは開きません") {
		t.Errorf("--no-browser の断りが出ていない:\n%s", stdout)
	}
}

func TestRunEditReportsMissingTranslations(t *testing.T) {
	root := makeTree(t, map[string]string{"data/script_order.csv": diffOrderCSV})
	code, _, stderr := runEditArgs(t, "--root", root, "--no-browser")
	if code != exitError {
		t.Fatalf("終了コードが %d", code)
	}
	if stderr == "" {
		t.Error("理由が出ていない")
	}
}

func TestMainRoutesEdit(t *testing.T) {
	// main.go の switch に1ケース足しただけであることを、入口から確かめる。
	var stdout, stderr bytes.Buffer
	code := run([]string{"edit", "--help"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("終了コードが %d", code)
	}
	if !strings.HasPrefix(stdout.String(), "使い方: dwloc edit") {
		t.Errorf("edit の使い方が出ていない:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"help"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("help の終了コードが %d", code)
	}
	if !strings.Contains(stdout.String(), "edit ") {
		t.Errorf("一覧に edit が出ていない:\n%s", stdout.String())
	}
}
