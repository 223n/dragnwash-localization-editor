package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/gamedir"
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

// realPath は path のリンクを解いた形を返す。
//
// dwloc は --game で渡された場所を、リンクを解いてから端末に出す（gamedir の
// canonical が EvalSymlinks をかける）。t.TempDir() は TMP の綴りのままなので、
// TMP が 8.3 形式の短い名前（C:\Users\RUNNER~1\... など）だったり、リンクを含んで
// いたり（macOS の /var は /private/var へのリンク）すると、出力と字面が合わない。
// 期待する側も、製品と同じ形にそろえてから比べる。
func realPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("%s のリンクを解けない: %v", path, err)
	}
	return resolved
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
		[]string{"作業コピーの探し先にします", filepath.ToSlash(realPath(t, game))})
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
			[]string{"Translations/_discovered がありません", "Export working copy"})
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

// editNoGameArgs は --game を書かずに edit を1回回す。待ち受けはすぐ終わらせる。
//
// --idle-timeout 1ns にするのは、実際に待ち受けを立ててから終わらせるためである。
// resolveGameForEdit の戻り値だけを見ると、決めたフォルダーが実際に入力として
// 使われたかが分からない。作業コピーを開いたことは、待ち受けが標準出力へ出す
// 1行（server.publish_hint）に出る。
func editNoGameArgs(t *testing.T, root string) (int, string, string) {
	t.Helper()

	return runCLI("edit", "--root", root, "--no-browser", "--idle-timeout", "1ns")
}

// TestEditWithoutGameFindsTheGameFolder は、--game を省いた edit がゲームの
// フォルダーを探し、見つけた作業コピーを入力にすることを見る。
//
// 実際に起きた: v0.5.0 を --game なしで起動すると、画面の「ファイル」は
// Translations/ja/strings.csv（公開ファイル）で、原文の欄は空、「未翻訳」は
// 「作業コピーがありません」だった。リポジトリの Translations/_discovered は
// .gitignore で外してあり、ディレクトリ自体が無い。作業コピーはゲームの
// フォルダーにしか無いので、探さなければ永久に当たらない。
func TestEditWithoutGameFindsTheGameFolder(t *testing.T) {
	root := gameRepo(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})
	stubFindGame(t, game)

	code, stdout, stderr := editNoGameArgs(t, root)
	if code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}
	// 打っていない指定で読む先が増えたのだから、決めたことは必ず書く。
	checkContains(t, "標準エラー", stderr,
		[]string{"作業コピーの探し先にします", filepath.ToSlash(game)})
	// 決めただけでなく、入力になっていること。作業コピーを開いたロケールが
	// 1つでもあると、待ち受けはこの1行を出す。
	checkContains(t, "標準出力", stdout, []string{"作業コピーを開いています"})
}

// TestEditWithoutGameKeepsGoingWhenNothingIsFound は、見つからなくても edit が
// 止まらないことを見る。
//
// ここで止めると、ゲームを入れていない PC では公開ファイルの手直しすらできなく
// なる。--game auto と違って、打っていない指定で画面が開かないのは通らない。
func TestEditWithoutGameKeepsGoingWhenNothingIsFound(t *testing.T) {
	root := gameRepo(t)
	stubFindGame(t)

	code, stdout, stderr := editNoGameArgs(t, root)
	if code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}
	// 画面は開いている。公開ファイルを並べて続ける。
	checkContains(t, "標準出力", stdout, []string{"http://127.0.0.1:", "?t="})
	// 探して無かったことは伝える。原文の欄が空な理由がここにしか出ない。
	checkContains(t, "標準エラー", stderr,
		[]string{"探しましたが、見つかりませんでした", "--game"})
	// 作業コピーは開いていない。開いていないのに開いたと言わない。
	if strings.Contains(stdout, "作業コピーを開いています") {
		t.Errorf("作業コピーを開いたことになっている:\n%s", stdout)
	}
	// --game auto のときの長い案内は出さない。打っていない人への字なので、
	// 次にやること（ゲーム内の Export working copy）まで並べない。
	if strings.Contains(stderr, "Export working copy") {
		t.Errorf("--game auto の案内が出ている:\n%s", stderr)
	}
}

// TestEditWithoutGameDoesNotPickAmongCandidates は、候補が複数あっても
// どれかを選ばないことを見る。
//
// 選んだ根拠は翻訳者から見えないので、黙って別のゲームのファイルを直させる
// ことになる。かわりに並べて見せ、作業コピー無しで始める。
func TestEditWithoutGameDoesNotPickAmongCandidates(t *testing.T) {
	root := gameRepo(t)
	first := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})
	second := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})
	stubFindGame(t, first, second)

	code, stdout, stderr := editNoGameArgs(t, root)
	if code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{
		"2個見つかりました", "作業コピー無しで始めます", "--game",
		filepath.ToSlash(first), filepath.ToSlash(second),
	})
	// どちらも読んでいない。片方を選んでいたら、この1行が出る。
	if strings.Contains(stdout, "作業コピーを開いています") {
		t.Errorf("候補のどちらかを選んでいる:\n%s", stdout)
	}
	checkContains(t, "標準出力", stdout, []string{"http://127.0.0.1:"})
}

// TestEditWithGameIgnoresTheAutoSearch は、--game を書いたときに自動検出へ
// 落ちないことを見る。
//
// 指定を書いた人は、その場所を読ませたいのである。書いたのに別の場所が混ざると、
// どちらを直しているのか画面から追えなくなる。
func TestEditWithGameIgnoresTheAutoSearch(t *testing.T) {
	root := gameRepo(t)
	asked := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})
	other := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})
	stubFindGame(t, other)

	code, _, stderr := runCLI("edit", "--root", root, "--game", asked,
		"--no-browser", "--idle-timeout", "1ns")
	if code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{filepath.ToSlash(realPath(t, asked))})
	// 混ざったときにどちらの綴りで出るかは道筋しだいなので、両方の形で探す。
	// 解いた形だけで探すと、TMP が 8.3 形式のときに素の形で混ざっても見逃す。
	for _, p := range []string{other, realPath(t, other)} {
		if strings.Contains(stderr, filepath.ToSlash(p)) {
			t.Errorf("自動検出の結果が混ざっている:\n%s", stderr)
		}
	}
}

// TestEveryCommandSearchesForTheGame は、publish と diff も自動検出を呼ぶことを見る。
//
// はじめは edit だけが探す形にしていた。publish の入力が実行する PC で変わるのを
// 避けたかったためである。それは誤りだった。既定の edit はゲーム側の作業コピーへ
// 保存するので、既定の publish がそれを読まないと訳が1行も届かない。しかも
// publish は終了コード0で「書き出しました」と言うので、届かなかったことが
// どこにも出ない。片方だけ探すのは、静かに壊れる道だった。
//
// 「呼ぶ」を数で見るのは、出力の突き合わせだけでは足りないからである。探しに
// 行ったうえで結果を捨てていても、作業コピーの無いリポジトリでは出力が同じに見える。
func TestEveryCommandSearchesForTheGame(t *testing.T) {
	root := gameRepo(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})

	calls := 0
	was := findGame
	t.Cleanup(func() { findGame = was })
	findGame = func() []gamedir.Plugin {
		calls++
		return []gamedir.Plugin{{Path: game}}
	}

	for _, args := range [][]string{
		{"publish", "--root", root, "--dry-run"},
		{"diff", "--root", root},
		{"diff", "--root", root, "--format", "csv"},
	} {
		calls = 0
		_, _, stderr := runCLI(args...)
		if calls != 1 {
			t.Errorf("%v: 自動検出を %d 回呼んでいる、1回を期待", args, calls)
		}
		// 読む先が増えたことは必ず伝える。黙って別のフォルダーを読み始めない。
		if !strings.Contains(stderr, "作業コピーの探し先にします") {
			t.Errorf("%v: 探し先を伝えていない\n%s", args, stderr)
		}
	}

	// validate は --game を受け取るが使わない。探しにも行かない。
	calls = 0
	if _, _, stderr := runCLI("validate", "--root", root); calls != 0 {
		t.Errorf("validate が自動検出を %d 回呼んでいる\n%s", calls, stderr)
	}
}

// TestNoGameKeepsPublishAndDiffOutput は、--no-game を書いた publish と diff の
// 出力が、目の前にゲームのフォルダーがあっても1バイト変わらないことを見る。
//
// 3つとも探すようにした代わりに、同じ答えが要るときの逃げ道がこれになった。
// 機械と突き合わせる出力、コミットする中身を固定したいとき、そして develop との
// 出力の突き合わせがここに乗っている。
func TestNoGameKeepsPublishAndDiffOutput(t *testing.T) {
	root := gameRepo(t)

	type run struct {
		stdout, stderr string
	}
	args := [][]string{
		{"publish", "--root", root, "--dry-run", "--no-game"},
		{"diff", "--root", root, "--no-game"},
		{"diff", "--root", root, "--format", "csv", "--no-game"},
		{"diff", "--root", root, "--all", "--no-game"},
		{"validate", "--root", root},
	}

	// ゲームのフォルダーが無い状態で1回ずつ。
	before := make([]run, len(args))
	for i, a := range args {
		_, stdout, stderr := runCLI(a...)
		before[i] = run{stdout, stderr}
	}

	// 目の前にゲームのフォルダーを置き、自動検出もそこへ当たるようにする。
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})
	stubFindGame(t, game)

	for i, a := range args {
		_, stdout, stderr := runCLI(a...)
		if stdout != before[i].stdout {
			t.Errorf("%v: 標準出力が変わった\n--- 前 ---\n%s\n--- 後 ---\n%s",
				a, before[i].stdout, stdout)
		}
		if stderr != before[i].stderr {
			t.Errorf("%v: 標準エラーが変わった\n--- 前 ---\n%s\n--- 後 ---\n%s",
				a, before[i].stderr, stderr)
		}
	}
}

// TestPublishAndDiffFindTheGameWorkingCopy は、--game を省いた publish と diff が
// ゲーム側の作業コピーを実際に入力にすることを見る。
//
// これが通らないと、既定の edit で入れた訳が既定の publish に拾われない。
func TestPublishAndDiffFindTheGameWorkingCopy(t *testing.T) {
	root := gameRepo(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})
	stubFindGame(t, game)

	// publish は "<出力> <- <入力>" の行に、実際に読んだファイルを出す。
	_, stdout, stderr := runCLI("publish", "--root", root, "--dry-run")
	if !strings.Contains(stdout, "ja.working.csv") {
		t.Errorf("publish がゲーム側の作業コピーを入力にしていない\n--- 標準出力 ---\n%s\n--- 標準エラー ---\n%s",
			stdout, stderr)
	}
	// diff は「作業コピー」の行に出す。
	_, stdout, stderr = runCLI("diff", "--root", root)
	if !strings.Contains(stdout, "ja.working.csv") {
		t.Errorf("diff がゲーム側の作業コピーを読んでいない\n--- 標準出力 ---\n%s\n--- 標準エラー ---\n%s",
			stdout, stderr)
	}
}

// TestEditNoGameStaysInsideTheRoot は、--no-game を書いた edit が探しも読みも
// しないことを見る。
//
// 要るのは、探すのを既定にしたことで --root が edit の書き込み先を囲わなく
// なったからである。保存先は見つかったゲームのフォルダーの作業コピーになるので、
// リポジトリを写して試す人は自分の写しではなくゲームのフォルダーを直すことに
// なる（実際に踏んだ: 写しを --root に渡した edit が、実機のゲームのフォルダーの
// ja.working.csv を並べ、そこへ保存した）。ゲームのフォルダーへ書けない PC では、
// 画面は開くのに保存が落ち続ける。どちらも、打ち消しが無いと逃げ道が無い。
func TestEditNoGameStaysInsideTheRoot(t *testing.T) {
	root := gameRepo(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})
	stubFindGame(t, game)

	code, stdout, stderr := runCLI("edit", "--root", root, "--no-game",
		"--no-browser", "--idle-timeout", "1ns")
	if code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}
	// 画面は開く。公開ファイルの手直しはできる。
	checkContains(t, "標準出力", stdout, []string{"http://127.0.0.1:", "?t="})
	// 探し先の1行を出さない。打った人が減らした指定なので、断る字が要らない。
	if strings.Contains(stderr, "作業コピーの探し先にします") ||
		strings.Contains(stderr, filepath.ToSlash(game)) {
		t.Errorf("--no-game なのにゲームのフォルダーを探している:\n%s", stderr)
	}
	// 読んでもいない。探していれば、この root には作業コピーが無いので
	// ゲーム側が当たり、待ち受けがこの1行を出す
	// （[TestEditWithoutGameFindsTheGameFolder] がその道を押さえている）。
	if strings.Contains(stdout, "作業コピーを開いています") {
		t.Errorf("ゲーム側の作業コピーを開いている:\n%s", stdout)
	}
}

// TestEditRefusesGameAndNoGameTogether は、指定と打ち消しを両方書かれたときに
// 止まることを見る。
//
// どちらかを勝たせない。「ここを読め」と「ゲームは見るな」のどちらを打ち
// 間違えたのかは、こちらからは決められない。勝ち負けの決まりを作ると、
// 打ち間違えた人は別の場所を直すことになる。
func TestEditRefusesGameAndNoGameTogether(t *testing.T) {
	root := gameRepo(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": gameWorkingCSV,
	})

	code, stdout, stderr := runCLI("edit", "--root", root, "--game", game,
		"--no-game", "--no-browser", "--idle-timeout", "1ns")
	if code != exitError {
		t.Fatalf("終了コード = %d、%d を期待\n%s", code, exitError, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{"--game", "--no-game"})
	// 待ち受けを立てない。立ててから止めると、URL が出たあとに落ちる。
	if strings.Contains(stdout, "http://127.0.0.1:") {
		t.Errorf("待ち受けを始めている:\n%s", stdout)
	}
}

// TestWriteGameErrorNotFound は、--game auto が空振りしたときの案内を見る。
//
// 入口から試さないのは、--game auto が gamedir.Find を直に呼び、実機の Steam を
// 見に行くからである（[TestMain] の注意書き）。案内の選び分けだけをここで見る。
//
// macOS では「ゲームを起動して Export working copy を押す」が実行できない案内に
// なる（Mod が macOS で動かない）。文面を分けたことが崩れると、押せないボタンを
// 押させる行き止まりに戻る。
func TestWriteGameErrorNotFound(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"番兵そのもの", gamedir.ErrNotFound},
		// 包まれていても同じ案内にする。gamedir が文脈を足しても、次にやることは変わらない。
		{"包まれた番兵", fmt.Errorf("探した結果: %w", gamedir.ErrNotFound)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			writeGameError(tc.err, &buf)
			got := buf.String()

			checkContains(t, "案内", got, []string{"ゲームのフォルダーが見つかりません", "--game <フォルダー>"})
			if runtime.GOOS == "darwin" {
				checkContains(t, "案内", got, []string{"macOS", "dwloc 自身は動きます"})
				return
			}
			// macOS 以外では、次にやることはゲーム内での書き出しである。
			checkContains(t, "案内", got, []string{"F1 → Translation → Export working copy"})
			if strings.Contains(got, "macOS") {
				t.Errorf("macOS 以外で macOS 向けの案内が出ている:\n%s", got)
			}
		})
	}
}

// TestWriteGameErrorUnknown は、名前の付いていない理由でも黙って終わらないことを見る。
//
// internal/gamedir が新しい誤りを増やしたとき、ここが何も書かないと、終了コード2
// だけが残って理由が画面のどこにも出ない。
func TestWriteGameErrorUnknown(t *testing.T) {
	var buf bytes.Buffer
	writeGameError(errors.New("読み取りの権限がありません"), &buf)
	checkContains(t, "案内", buf.String(),
		[]string{"ゲームのフォルダーを決められません", "読み取りの権限がありません"})
}

// TestResolveGameEmptyValue は、値が空なら探しも書きもしないことを見る。
//
// resolveGame は --game を打ったときだけの部品で、空のときの自動検出は
// resolveGameAuto の役目である。ここが空で探しに行くと、自動検出が2度走り、
// 探し先の1行も2度出る。
func TestResolveGameEmptyValue(t *testing.T) {
	calls := 0
	was := findGame
	t.Cleanup(func() { findGame = was })
	findGame = func() []gamedir.Plugin {
		calls++
		return nil
	}

	var buf bytes.Buffer
	path, ok := resolveGame("", &buf)
	if !ok || path != "" {
		t.Errorf("resolveGame(\"\") = (%q, %v), 期待 (\"\", true)", path, ok)
	}
	if buf.Len() != 0 {
		t.Errorf("何か書いている:\n%s", buf.String())
	}
	if calls != 0 {
		t.Errorf("自動検出を %d 回呼んでいる", calls)
	}
}

// TestPublishStopsBeforeReadingWhenTheGameIsWrong は、ゲームのフォルダーを決め
// られなかった publish が、何も書かずに終了コード2で止まることを見る。
//
// 止まらずに進むと、打った場所とは別の入力（リポジトリの中だけ）で書き出す。
// 翻訳者はゲームで入れた訳が入ったつもりで、入っていない公開ファイルを
// コミットすることになる。
func TestPublishStopsBeforeReadingWhenTheGameIsWrong(t *testing.T) {
	for _, tc := range []struct {
		name string
		// args は publish と --root のあとに置く引数。{game} はゲームのフォルダーに置き換える。
		args []string
		want []string
	}{
		{
			name: "--game と --no-game を一緒に打った",
			args: []string{"--game", "{game}", "--no-game"},
			want: []string{"--game と --no-game は一緒に指定できません"},
		},
		{
			name: "--game が目印の無い場所を指している",
			args: []string{"--game", "{empty}"},
			want: []string{"Translations/_discovered がありません"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := gameRepo(t)
			game := makeGame(t, map[string]string{
				"Translations/_discovered/ja.working.csv": gameWorkingCSV,
			})
			empty := t.TempDir()
			before := readFile(t, root, "Translations/ja/strings.csv")

			args := []string{"publish", "--root", root}
			for _, a := range tc.args {
				a = strings.ReplaceAll(a, "{game}", game)
				a = strings.ReplaceAll(a, "{empty}", empty)
				args = append(args, a)
			}
			code, stdout, stderr := runCLI(args...)
			if code != exitError {
				t.Fatalf("終了コード = %d、%d を期待\n%s", code, exitError, stderr)
			}
			checkContains(t, "標準エラー", stderr, tc.want)
			if stdout != "" {
				t.Errorf("失敗したのに標準出力へ書いている:\n%s", stdout)
			}
			if after := readFile(t, root, "Translations/ja/strings.csv"); after != before {
				t.Errorf("止めたのに公開ファイルが変わっている:\n%s", after)
			}
		})
	}
}

// TestNoGameAppearsInEveryUsage は、打ち消しが使い方に出ていることを見る。
//
// 逃げ道は、あることが読めないと逃げ道にならない。publish / diff / edit の3つとも
// --game を省くと探すので、3つとも打ち消しを受け、3つとも説明に出す。
// 一度は edit だけに置いていたが、探すのを edit だけにしていたころの形である。
func TestNoGameAppearsInEveryUsage(t *testing.T) {
	for _, args := range [][]string{
		{"help"},
		{"edit", "--help"},
		{"publish", "--help"},
		{"diff", "--help"},
	} {
		_, stdout, _ := runCLI(args...)
		checkContains(t, strings.Join(args, " ")+" の説明", stdout, []string{"--no-game"})
	}

	// validate は --game と --no-game を受け取るが使わない。受けることと使わない
	// ことの両方を説明に出す。以前は打ち消す相手が無いとして置かなかったが、
	// 指定を付けて回るスクリプトで validate だけが落ちていた。
	_, stdout, _ := runCLI("validate", "--help")
	checkContains(t, "validate --help の説明", stdout, []string{"--no-game", "validate では使いません"})
}
