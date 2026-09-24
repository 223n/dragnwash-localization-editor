package main

import (
	"io"
	"slices"
	"strings"
	"testing"
)

// TestNoGameIsACommonOption は、--no-game がサブコマンドの前でも効くことを見る。
//
// 使い方は --no-game を「共通のオプション」に並べ、README はダブルクリックで
// 使う人に「コマンドから --no-game を付けてください」と案内している。そのとおりに
// dwloc --no-game と打つと、英語の1行と使い方の全文で止まっていた。
func TestNoGameIsACommonOption(t *testing.T) {
	root := gameRepo(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})
	// 探せば見つかる状態にしておく。--no-game が効いていなければ、探し先の1行が出る。
	stubFindGame(t, game)

	for _, args := range [][]string{
		{"--no-game", "--root", root, "diff"},
		{"--no-game", "--root", root, "publish", "--dry-run"},
		{"--root", root, "--no-game", "diff", "--format", "csv"},
	} {
		code, _, stderr := runCLI(args...)
		if code == exitError {
			t.Errorf("%v: 終了コード = %d\n%s", args, code, stderr)
		}
		if strings.Contains(stderr, "作業コピーの探し先にします") {
			t.Errorf("%v: --no-game なのにゲームのフォルダーを探している:\n%s", args, stderr)
		}
	}
}

// TestNoGameBeforeTheSubcommandStillRejectsGame は、共通の入口で受けた --no-game と
// --game を一緒に打つと、サブコマンドの後ろに書いたときと同じく止まることを見る。
// どちらを打ち間違えたのかは、こちらからは決められない。
func TestNoGameBeforeTheSubcommandStillRejectsGame(t *testing.T) {
	root := gameRepo(t)
	game := makeGame(t, nil)

	code, _, stderr := runCLI("--no-game", "--game", game, "--root", root, "publish", "--dry-run")
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\n%s", code, exitError, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{"--game と --no-game は一緒に指定できません"})
}

// TestNoGameReachesTheDefaultEditor は、サブコマンドを省いたときも --no-game が
// edit へ届くことを見る。ダブルクリックで使う人が、同じ画面をゲームを見ずに
// 開くための形である。
func TestNoGameReachesTheDefaultEditor(t *testing.T) {
	root := gameRepo(t)

	var gotArgs []string
	orig := startEdit
	startEdit = func(args []string, _, _ string, _, _ io.Writer) int {
		gotArgs = args
		return exitOK
	}
	t.Cleanup(func() { startEdit = orig })

	if code, _, stderr := runCLI("--no-game", "--root", root); code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}
	if !slices.Contains(gotArgs, "--no-game") {
		t.Errorf("edit に --no-game が届いていない: %q", gotArgs)
	}
}

// TestValidateAndVersionAcceptButIgnoreNoGame は、validate と version が --no-game を
// 受けて使わないことを見る。
//
// --game と同じ扱いにする。validate、diff、publish に同じ指定を付けて回す
// スクリプトで、validate だけが落ちないようにするためである。
func TestValidateAndVersionAcceptButIgnoreNoGame(t *testing.T) {
	root := gameRepo(t)

	for _, args := range [][]string{
		{"validate", "--root", root, "--no-game"},
		{"--no-game", "validate", "--root", root},
		{"version", "--no-game"},
		{"--no-game", "version"},
	} {
		code, _, stderr := runCLI(args...)
		if code == exitError {
			t.Errorf("%v: --no-game で止まっている:\n%s", args, stderr)
		}
	}
}
