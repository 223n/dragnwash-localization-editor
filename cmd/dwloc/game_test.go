package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeGame はゲーム側のプラグインフォルダーを作り、そのパスを返す。
// files のキーはプラグインフォルダーからのスラッシュ区切りの相対パス。
func makeGame(t *testing.T, files map[string]string) string {
	t.Helper()

	game := filepath.Join(t.TempDir(), "common", "Drag'n Wash", "BepInEx", "plugins",
		"DragNWashLocalization")
	// 目印（Translations/_discovered）は中身が無くても作る。ここが在ることが
	// 「プラグインのフォルダーである」の判定そのものだからである。
	mark := filepath.Join(game, "Translations", "_discovered")
	if err := os.MkdirAll(mark, 0o755); err != nil {
		t.Fatalf("%s を作れない: %v", mark, err)
	}
	for rel, content := range files {
		path := filepath.Join(game, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("%s の親を作れない: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("%s を書けない: %v", rel, err)
		}
	}
	return game
}

// gameWorkingCSV は原文つきの作業コピー1件分。訳は1行目だけ入っている。
const gameWorkingCSV = "key,section,node,order,speaker,source_en,translation\n" +
	"0da72197e898ebe1,L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,もしもし？\n" +
	"334d016f755cd6dc,L01 Ryan,Ryan_1_intro,2,Kobold,Hi there!,\n"

// gameRepo は publish / diff を通せる最小の翻訳リポジトリを作る。
func gameRepo(t *testing.T) string {
	t.Helper()

	return makeTree(t, map[string]string{
		"data/script_order.csv": "section,phase,node,order,line_id,key,speaker,condition\n" +
			"L01 Ryan,intro,Ryan_1_intro,1,line:aaaaaaaa,0da72197e898ebe1,Ryan,\n" +
			"L01 Ryan,intro,Ryan_1_intro,2,line:bbbbbbbb,334d016f755cd6dc,Kobold,\n",
		"Translations/ja/strings.csv": "key,section,node,order,speaker,translation\n" +
			"0da72197e898ebe1,L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？\n",
	})
}

func TestRunDiffWithGameFindsUntranslated(t *testing.T) {
	root := gameRepo(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})

	code, stdout, stderr := runCLI("diff", "--root", root, "--game", game)
	if code != exitOK && code != exitProblems {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}
	// 探し先は必ず伝える。黙って別の場所を読み始めない。
	// パスはスラッシュ区切りで出す（cmd/dwloc の displayPath と同じ扱い）。
	checkContains(t, "標準エラー", stderr,
		[]string{"作業コピーの探し先にします", filepath.ToSlash(game)})
	// 「使います」とは書かない。そのロケールの作業コピーがそこに無いこともある。
	if strings.Contains(stderr, "ゲームのフォルダーを使います") {
		t.Errorf("読む前から使うと言い切っている:\n%s", stderr)
	}
	// 作業コピーを読めたので、未翻訳を「判定できません」で止めない。
	checkContains(t, "標準出力", stdout, []string{"未翻訳"})
}

func TestRunDiffCSVKeepsStdoutClean(t *testing.T) {
	// --format csv の標準出力は表計算へそのまま貼る前提。探し先の案内が
	// 混じると列が崩れるので、標準エラーへ出す。
	root := gameRepo(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})

	_, stdout, stderr := runCLI("diff", "--root", root, "--game", game, "--format", "csv")
	if strings.Contains(stdout, "ゲームのフォルダー") {
		t.Errorf("案内が標準出力に混じっている:\n%s", stdout)
	}
	checkContains(t, "標準エラー", stderr, []string{"作業コピーの探し先にします"})

	for i, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if i == 0 {
			if !strings.HasPrefix(line, "locale,category,") {
				t.Fatalf("1行目が見出しでない: %q", line)
			}
			continue
		}
		if line != "" && strings.Count(line, ",") < 10 {
			t.Errorf("%d 行目の列数が足りない: %q", i+1, line)
		}
	}
}

// TestPublishRefusesGame は、publish が --game を受け取って断ることを見る。
//
// 黙って無視すると「ゲーム側の作業コピーから作り直した」と読まれる。使って
// しまえばもっと悪く、作業コピーに無い行がコミット済みの公開ファイルから消える。
// どちらも起こさないために、ここで止めて何も書かない。
func TestPublishRefusesGame(t *testing.T) {
	root := gameRepo(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})
	before := readFile(t, root, "Translations/ja/strings.csv")

	for _, args := range [][]string{
		{"publish", "--root", root, "--game", game},
		// 共通オプションとしてサブコマンドの前に置かれた場合も同じ。
		{"--root", root, "--game", game, "publish"},
		// 自動検出も走らせない。断るのは値を見る前である。
		{"publish", "--root", root, "--game", "auto"},
		// 書かないことは --dry-run でも変わらない。
		{"publish", "--root", root, "--game", game, "--dry-run"},
	} {
		code, stdout, stderr := runCLI(args...)
		if code != exitError {
			t.Fatalf("%v: 終了コード = %d\n%s", args, code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{
			"publish は --game を受け付けません",
			"dwloc edit --game",
		})
		if strings.Contains(stderr, "作業コピーの探し先にします") {
			t.Errorf("%v: 断ったのに探しにいっている:\n%s", args, stderr)
		}
		if stdout != "" {
			t.Errorf("%v: 標準出力に何か出ている:\n%s", args, stdout)
		}
		if after := readFile(t, root, "Translations/ja/strings.csv"); after != before {
			t.Fatalf("%v: 断ったのに公開ファイルが変わっている", args)
		}
	}
}

// TestPublishWithoutGameKeepsUsingTheRepository は、--game を付けない publish が
// ゲーム側を読まないことを見る。
//
// ゲームのフォルダーが目の前にあっても、入力は公開ファイル自身のままである。
// 作業コピーの2行目（訳が空）に引きずられて公開ファイルが削れない、という
// この設計の要になる。
func TestPublishWithoutGameKeepsUsingTheRepository(t *testing.T) {
	root := gameRepo(t)
	makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})

	// 1回目で見出しを整える。見本の公開ファイルは見出しを持たないので、
	// ここを飛ばすと「publish が入力を変えた」と「見出しを足した」が混ざる。
	if code, _, stderr := runCLI("publish", "--root", root); code != exitOK {
		t.Fatalf("1回目の終了コード = %d\n%s", code, stderr)
	}
	before := readFile(t, root, "Translations/ja/strings.csv")

	code, _, stderr := runCLI("publish", "--root", root)
	if code != exitOK {
		t.Fatalf("2回目の終了コード = %d\n%s", code, stderr)
	}
	// 入力＝出力なので冪等。1バイトも変わらない。
	after := readFile(t, root, "Translations/ja/strings.csv")
	if after != before {
		t.Errorf("公開ファイルが変わっている\n--- 前 ---\n%s\n--- 後 ---\n%s", before, after)
	}
	// 作業コピーの2行目（訳が空）に引きずられていない。ゲーム側を入力に
	// していたら、この行は消えるか、speaker が UI に変わる。
	if !strings.Contains(after, "もしもし？") {
		t.Errorf("訳が消えている:\n%s", after)
	}
}

func TestRunWithoutGameDoesNotLookAtTheGame(t *testing.T) {
	// --game を指定しないときは、ゲームのフォルダーを見に行かない。
	// 出力も、案内が1行も足されないままになる。
	root := gameRepo(t)
	makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})

	for _, args := range [][]string{
		{"diff", "--root", root},
		{"publish", "--root", root, "--dry-run"},
		{"validate", "--root", root},
	} {
		_, stdout, stderr := runCLI(args...)
		if strings.Contains(stderr, "ゲームのフォルダー") {
			t.Errorf("%v: 案内が出ている:\n%s", args, stderr)
		}
		if strings.Contains(stdout, "ゲームのフォルダー") {
			t.Errorf("%v: 案内が標準出力に出ている:\n%s", args, stdout)
		}
	}
}

func TestRunWithGameErrors(t *testing.T) {
	root := gameRepo(t)

	t.Run("目印の無い場所を指すと断る", func(t *testing.T) {
		code, _, stderr := runCLI("diff", "--root", root, "--game", t.TempDir())
		if code != exitError {
			t.Fatalf("終了コード = %d\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr,
			[]string{"Translations/_discovered がありません", "Export game flow"})
	})

	t.Run("候補が複数なら選ばずに一覧を出す", func(t *testing.T) {
		gameDir := filepath.Join(t.TempDir(), "Drag'n Wash")
		for _, name := range []string{"a", "b"} {
			dir := filepath.Join(gameDir, "BepInEx", "plugins", name, "Translations", "_discovered")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}

		code, _, stderr := runCLI("diff", "--root", root, "--game", gameDir)
		if code != exitError {
			t.Fatalf("終了コード = %d\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr,
			[]string{"2個見つかりました", "--game", "plugins/a", "plugins/b"})
	})
}

func TestGameIsACommonOption(t *testing.T) {
	// --root と同じく、サブコマンドの前に置いても後ろに置いても効く。
	root := gameRepo(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})

	for _, args := range [][]string{
		{"--root", root, "--game", game, "diff"},
		{"diff", "--root", root, "--game", game},
	} {
		_, _, stderr := runCLI(args...)
		checkContains(t, "標準エラー", stderr, []string{"作業コピーの探し先にします"})
	}
}

func TestValidateAcceptsButIgnoresGame(t *testing.T) {
	// 共通オプションのつもりで打たれても止まらない。使わないことは説明に書く。
	//
	// publish と違って断らないのは、使っても壊れないからである。validate は
	// コミットする側の形を見る処理で、ゲームのフォルダーは判断に関わらない。
	// CI で --game を共通に付けている人を、ここで止める理由が無い。
	root := gameRepo(t)

	code, _, stderr := runCLI("validate", "--root", root, "--game", "auto")
	if code == exitError {
		t.Fatalf("--game で止まっている:\n%s", stderr)
	}
	if strings.Contains(stderr, "作業コピーの探し先にします") {
		t.Errorf("validate が自動検出を走らせている:\n%s", stderr)
	}
}

func TestGameAppearsInUsage(t *testing.T) {
	for _, args := range [][]string{
		{"help"},
		{"diff", "--help"},
		{"publish", "--help"},
		{"edit", "--help"},
	} {
		_, stdout, _ := runCLI(args...)
		checkContains(t, strings.Join(args, " ")+" の説明", stdout, []string{"--game"})
	}
}

// TestPublishUsageSaysItRefusesGame は、publish の説明が「受け付けない」と
// 書いてあることを見る。
//
// 一覧に --game が出ているだけだと、使える指定に見える。
func TestPublishUsageSaysItRefusesGame(t *testing.T) {
	_, stdout, _ := runCLI("publish", "--help")
	checkContains(t, "publish の説明", stdout, []string{"受け付けません"})
}
