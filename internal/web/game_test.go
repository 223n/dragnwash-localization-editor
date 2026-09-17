package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// keyOnlyInWorking は作業コピーにあって公開ファイルに無いキー。
//
// 公開ファイルは publish が作り、訳が空の行を書かない（移植仕様 R20）ので、
// まだ訳していない行はすべてこの形になる。画面で訳を入れても公開ファイルは
// 変わらず、入るのは publish を回したときである。
const keyOnlyInWorking = "1234567890abcdef"

// newTestGame はゲーム側のプラグインフォルダーを作り、そのパスを返す。
//
// 中身は ja の作業コピー1つだけ。原文（source_en）が入っているのが、
// リポジトリ側にあるファイルとの違いである。
//
// 行番号: 1 ヘッダー / 2 訳あり / 3 訳が空 / 4 公開ファイルに無い行
func newTestGame(t *testing.T) string {
	t.Helper()

	game := filepath.Join(t.TempDir(), "BepInEx", "plugins", "DragNWashLocalization")
	dir := filepath.Join(game, "Translations", "_discovered")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// ヘッダーは WorkingCopy.cs が書く7列（internal/edit の workingHeader）。
	body := strings.Join([]string{
		"key,section,node,order,speaker,source_en,translation",
		keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,もしもし？",
		keyKept2 + ",L01 Ryan,Ryan_1_intro,2,Kobold,Hi there!,",
		keyOnlyInWorking + ",L01 Ryan,Ryan_1_intro,3,Ryan,Brand new line.,",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "ja.working.csv"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return game
}

// gamePair は翻訳リポジトリとゲームのフォルダーを両方作り、待ち受けを組む。
func gamePair(t *testing.T) (s *server, root, game string) {
	t.Helper()

	root = newTestRoot(t)
	game = newTestGame(t)
	return newTestServer(t, Options{Root: root, Game: game, UILang: "ja"}), root, game
}

// workingCopyPath はゲーム側の作業コピーのパスを返す。
func workingCopyPath(game string) string {
	return filepath.Join(game, "Translations", "_discovered", "ja.working.csv")
}

// publishedPath はコミットする側の公開ファイルのパスを返す。
func publishedPath(root string) string {
	return filepath.Join(root, "Translations", "ja", "strings.csv")
}

// currentVersion は画面が並べているファイルのいまの版を返す。
func currentVersion(t *testing.T, s *server) string {
	t.Helper()

	rec := do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d", rec.Code)
	}
	return decode[linesResponse](t, rec.Body.Bytes()).Version
}

func TestBootstrapShowsGameFolder(t *testing.T) {
	game := newTestGame(t)
	s := newTestServer(t, Options{Game: game, Locale: "ja", UILang: "ja"})

	rec := do(t, s, http.MethodGet, "/api/bootstrap", true, nil)
	got := decode[bootstrapResponse](t, rec.Body.Bytes())
	if got.Game != filepath.ToSlash(game) {
		t.Errorf("Game が %q、期待 %q", got.Game, filepath.ToSlash(game))
	}
	// 画面が出す文言も目録にある。無いと、鍵がそのまま画面に出る。
	if got.UI.Messages["ui.game_folder"] == "" {
		t.Error("ui.game_folder が目録に無い")
	}
	// 文面は「探し先」であって「使っています」ではない。そのロケールの作業コピーが
	// そこに無ければ1バイトも読まないので、読む前から言い切らない。
	if strings.Contains(got.UI.Messages["ui.game_folder"], "使") {
		t.Errorf("読む前から使うと言い切っている: %q", got.UI.Messages["ui.game_folder"])
	}
}

func TestBootstrapHasNoGameFolderByDefault(t *testing.T) {
	// --game を指定していないときは空。空なら画面はその行を隠す。
	s := newTestServer(t, Options{Locale: "ja", UILang: "ja"})

	rec := do(t, s, http.MethodGet, "/api/bootstrap", true, nil)
	got := decode[bootstrapResponse](t, rec.Body.Bytes())
	if got.Game != "" {
		t.Errorf("Game が %q。指定していないのに入っている", got.Game)
	}
}

func TestLinesUseGameWorkingCopy(t *testing.T) {
	s, root, game := gamePair(t)

	rec := do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d", rec.Code)
	}
	got := decode[linesResponse](t, rec.Body.Bytes())

	// 原文の欄が埋まる。埋まるのは作業コピーを読めたときだけである。
	var source string
	for _, line := range got.Lines {
		if line.Key == keyKept {
			source = line.Source
		}
	}
	if source != "Hello?" {
		t.Errorf("原文が %q。ゲーム側の作業コピーを読めていない", source)
	}
	// 画面が並べるのはゲーム側の作業コピー。行番号も版もこちらのもの。
	if filepath.ToSlash(got.Path) != filepath.ToSlash(workingCopyPath(game)) {
		t.Errorf("並べているファイルが %q", got.Path)
	}
	// 並べているのが作業コピーで、コミットする側へは publish で入ることを断る。
	// 出さないと、翻訳者は画面で直した訳がそのままコミットされると読む。
	//
	// 文面は目録から組み立てて突き合わせる。試験に日本語を書き写すと、
	// 目録を直したときにこちらだけが古いまま通る。
	if want := viaPublishNote(s, root); !hasNote(got.Notes, want) {
		t.Errorf("publish の断りが無い\n--- 期待 ---\n%s\n--- 断り書き ---\n%q", want, got.Notes)
	}
}

func TestLinesHaveNoPublishNoteWithoutWorkingCopy(t *testing.T) {
	// 作業コピーが無ければ、並べているのは公開ファイル自身。コミットする側は
	// いま直しているファイルそのものなので、publish の断りは出さない。
	root := newTestRoot(t)
	s := newTestServer(t, Options{Root: root, UILang: "ja"})

	rec := do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	got := decode[linesResponse](t, rec.Body.Bytes())
	if want := viaPublishNote(s, root); hasNote(got.Notes, want) {
		t.Errorf("作業コピーが無いのに publish の断りが出ている: %q", got.Notes)
	}
}

// viaPublishNote は「コミットする側へは publish で入る」という断り書きを、
// 目録から組み立てて返す。
func viaPublishNote(s *server, root string) string {
	return s.cat.T(s.cat.lookup("ja"), "note.via_publish",
		"path", s.displayPath(publishedPath(root)))
}

func TestSaveWritesOnlyTheWorkingCopy(t *testing.T) {
	// 保存先は1つ。画面が並べているゲーム側の作業コピーだけが変わり、
	// コミットする側の公開ファイルは1バイトも変わらない。
	s, root, game := gamePair(t)
	version := currentVersion(t, s)
	before := readFile(t, publishedPath(root))

	rec := save(t, s, "ja", version, rowEdit{Line: 3, Key: keyKept2, Translation: "やあ！"})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
	got := decode[rowsResponse](t, rec.Body.Bytes())
	if !got.Results[0].Saved {
		t.Fatalf("保存できていない: %+v", got.Results[0])
	}
	if got.Results[0].Warning != "" {
		t.Errorf("断りが出ている: %q", got.Results[0].Warning)
	}

	working := readFile(t, workingCopyPath(game))
	if !strings.Contains(working, "やあ！") {
		t.Errorf("作業コピーに入っていない:\n%s", working)
	}
	// 公開ファイルへ書くのは publish の仕事。ここでは触らない。
	if after := readFile(t, publishedPath(root)); after != before {
		t.Errorf("公開ファイルが変わっている\n--- 前 ---\n%s\n--- 後 ---\n%s", before, after)
	}
}

func TestSaveUntranslatedRowGoesToTheWorkingCopy(t *testing.T) {
	// 未訳の行。公開ファイルにその行は無い（publish が訳の空の行を書かないため）。
	//
	// ここが2つ書きを取り下げた理由そのものである。キーで公開ファイルへ書き戻そうと
	// しても書き戻す先の行が無く、翻訳者がいちばんやりたいことが1行も入らない。
	// 作業コピーへ入れておいて publish で作り直すのが本来の道である。
	s, root, game := gamePair(t)
	version := currentVersion(t, s)
	before := readFile(t, publishedPath(root))
	if strings.Contains(before, keyOnlyInWorking) {
		t.Fatalf("前提が崩れている。公開ファイルに未訳の行がある:\n%s", before)
	}

	rec := save(t, s, "ja", version,
		rowEdit{Line: 4, Key: keyOnlyInWorking, Translation: "あたらしい訳"})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
	got := decode[rowsResponse](t, rec.Body.Bytes())
	if !got.Results[0].Saved {
		t.Fatalf("保存できていない: %+v", got.Results[0])
	}
	// 行ごとの断りは出ない。行き先が1つしか無いので、断ることが無い。
	if got.Results[0].Warning != "" {
		t.Errorf("断りが出ている: %q", got.Results[0].Warning)
	}

	if working := readFile(t, workingCopyPath(game)); !strings.Contains(working, "あたらしい訳") {
		t.Errorf("作業コピーに入っていない:\n%s", working)
	}
	if after := readFile(t, publishedPath(root)); after != before {
		t.Errorf("公開ファイルが変わっている\n--- 前 ---\n%s\n--- 後 ---\n%s", before, after)
	}
}

func TestSaveFailsWhenTheWorkingCopyCannotBeWritten(t *testing.T) {
	// 書けなければ 503 で、saved も倒す。画面は未保存の控えを捨てずに送り直す。
	// ゲームは C:/Program Files (x86)/ の下に入ることが多く、拒まれうる。
	s, root, game := gamePair(t)
	version := currentVersion(t, s)
	before := readFile(t, publishedPath(root))

	makeReadOnly(t, workingCopyPath(game))

	rec := save(t, s, "ja", version, rowEdit{Line: 3, Key: keyKept2, Translation: "やあ！"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
	got := decode[errorResponse](t, rec.Body.Bytes())
	if len(got.Results) == 0 || got.Results[0].Saved {
		t.Fatalf("保存できたことになっている: %+v", got.Results)
	}
	if got.Results[0].Translation != "" {
		t.Errorf("保存した値を返している: %q", got.Results[0].Translation)
	}
	// 公開ファイルにも入らない。どこにも中途半端に入った状態を作らない。
	if after := readFile(t, publishedPath(root)); after != before {
		t.Errorf("公開ファイルが変わっている:\n%s", after)
	}
}

func TestSaveWithoutGameWritesOnlyOneFile(t *testing.T) {
	// --game が無いときも保存先は1つ。publish が入力に選ぶファイル
	// （リポジトリの作業コピー）だけが変わる。
	root := newEditRoot(t)
	s := newTestServer(t, Options{Root: root, UILang: "ja"})
	version := currentVersion(t, s)
	before := readFile(t, publishedPath(root))

	rec := save(t, s, "ja", version, rowEdit{Line: 6, Key: keyOf(t, s, 6), Translation: "さようなら。"})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
	// 公開ファイルは触らない。ここへ書くのは publish の仕事である。
	if after := readFile(t, publishedPath(root)); after != before {
		t.Errorf("公開ファイルが変わっている\n--- 前 ---\n%s\n--- 後 ---\n%s", before, after)
	}
	if working := readFile(t, inputPath(t, s, "ja")); !strings.Contains(working, "さようなら。") {
		t.Errorf("作業コピーに入っていない:\n%s", working)
	}
}

func TestGameSaveLogHasNoRowContent(t *testing.T) {
	// --verbose の記録に原文と訳を出さない。ゲーム側の作業コピーには原文
	// （英語の台本）が入っているので、そちらを開いているときも確かめる。
	root := newTestRoot(t)
	game := newTestGame(t)
	var log strings.Builder
	s := newTestServer(t, Options{Root: root, Game: game, UILang: "ja", Verbose: true, Stderr: &log})
	version := currentVersion(t, s)

	if rec := save(t, s, "ja", version,
		rowEdit{Line: 3, Key: keyKept2, Translation: jaTyped}); rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
	// 書けなかったときの記録にも中身を出さない。
	makeReadOnly(t, workingCopyPath(game))
	version = currentVersion(t, s)
	save(t, s, "ja", version, rowEdit{Line: 3, Key: keyKept2, Translation: jaTyped})

	for _, secret := range []string{jaTyped, "Hello?", "Hi there!", "もしもし？"} {
		if strings.Contains(log.String(), secret) {
			t.Errorf("記録に行の中身が出ている（%q）:\n%s", secret, log.String())
		}
	}
}

// hasNote は断り書きの並びに text を含む行があるかを返す。
func hasNote(notes []string, text string) bool {
	for _, note := range notes {
		if strings.Contains(note, text) {
			return true
		}
	}
	return false
}

// keyOf は行番号からその行のキーを引く。
func keyOf(t *testing.T, s *server, line int) string {
	t.Helper()

	rec := do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	for _, l := range decode[linesResponse](t, rec.Body.Bytes()).Lines {
		if l.Number == line {
			return l.Key
		}
	}
	t.Fatalf("%d 行目が無い", line)
	return ""
}

// makeReadOnly はファイルを読み取り専用にする。
//
// Windows では読み取り専用の属性が付き、[os.Rename] での置き換えが拒まれる。
// 保存が失敗する経路を、権限をいじらずに作れる。
func makeReadOnly(t *testing.T, path string) {
	t.Helper()

	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	// 後始末で消せるように戻す。t.TempDir の削除は読み取り専用で失敗しうる。
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
}
