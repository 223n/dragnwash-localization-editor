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
// 公開ファイルは publish が作るので、まだ回していない新しい行がこの形になる。
// 2つ書きでは、この行はゲーム側にだけ入り、行ごとに断りが付く。
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
	// もう1つの行き先（コミットする側）も画面から読める。
	if got.CommitPath != s.displayPath(publishedPath(root)) {
		t.Errorf("コミットする側が %q、期待 %q", got.CommitPath, s.displayPath(publishedPath(root)))
	}
	// 2つ書きであることは、行ごとの断りが出ないふだんの保存でも分かるように、
	// 画面の断り書きに入れる。
	if !hasNote(got.Notes, got.CommitPath) {
		t.Errorf("2つ書きの断りが無い: %q", got.Notes)
	}
}

func TestLinesHaveNoCommitPathWithoutGame(t *testing.T) {
	// --game が無ければ保存先は1つ。もう1つの行き先は出さない。
	s := newTestServer(t, Options{UILang: "ja"})

	rec := do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	got := decode[linesResponse](t, rec.Body.Bytes())
	if got.CommitPath != "" {
		t.Errorf("コミットする側が %q。2つ書きではない", got.CommitPath)
	}
}

func TestSaveWritesBothFiles(t *testing.T) {
	// 2つ書きの本体。ゲーム側の作業コピーと、コミットする側の公開ファイルの
	// 両方の「その行」が差し替わる。
	s, root, game := gamePair(t)
	version := currentVersion(t, s)

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
		t.Errorf("ゲーム側に入っていない:\n%s", working)
	}
	published := readFile(t, publishedPath(root))
	if !strings.Contains(published, "やあ！") {
		t.Errorf("コミットする側に入っていない:\n%s", published)
	}
	// 差し替えたのはその行だけ。公開ファイルの行数も見出しも変わらない。
	before := newTestRoot(t)
	if got, want := countLines(published), countLines(readFile(t, publishedPath(before))); got != want {
		t.Errorf("公開ファイルの行数が %d。%d のまま変わらないはず", got, want)
	}
	// 触っていない行は1バイトも変わらない。
	if !strings.Contains(published, jaVanished) {
		t.Errorf("触っていない行が消えている:\n%s", published)
	}
}

func TestSaveRowMissingFromPublishedGoesToTheGameOnly(t *testing.T) {
	// 公開ファイルにまだ無い行。ゲーム側にだけ入れて、行ごとに断る。
	// 黙って saved=true だけを返すと、翻訳者は「コミットすれば入る」と読む。
	s, root, game := gamePair(t)
	version := currentVersion(t, s)
	before := readFile(t, publishedPath(root))

	rec := save(t, s, "ja", version,
		rowEdit{Line: 4, Key: keyOnlyInWorking, Translation: "あたらしい訳"})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
	got := decode[rowsResponse](t, rec.Body.Bytes())
	if !got.Results[0].Saved {
		t.Fatalf("保存できていない: %+v", got.Results[0])
	}
	if got.Results[0].Warning == "" {
		t.Error("断りが無い")
	}

	if working := readFile(t, workingCopyPath(game)); !strings.Contains(working, "あたらしい訳") {
		t.Errorf("ゲーム側に入っていない:\n%s", working)
	}
	// 公開ファイルは1バイトも変わらない。行を足したりしない。
	if after := readFile(t, publishedPath(root)); after != before {
		t.Errorf("公開ファイルが変わっている\n--- 前 ---\n%s\n--- 後 ---\n%s", before, after)
	}
}

func TestSaveStopsWhenTheCommitSideCannotBeWritten(t *testing.T) {
	// 1 が書けなければ 2 は書かない。画面は未保存のまま抱えて送り直す。
	s, root, game := gamePair(t)
	version := currentVersion(t, s)
	beforeWorking := readFile(t, workingCopyPath(game))

	makeReadOnly(t, publishedPath(root))

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
	// ゲーム側へは書いていない。片方にだけ入った状態を作らない。
	if after := readFile(t, workingCopyPath(game)); after != beforeWorking {
		t.Errorf("ゲーム側が変わっている:\n%s", after)
	}
}

func TestSaveKeepsTheTranslationWhenTheGameCopyCannotBeWritten(t *testing.T) {
	// 1 が書けて 2 が書けなかったとき。訳はコミットする側に入っているので
	// 失われていない。saved=true で返し、ゲームへ届いていないことを断る。
	s, root, game := gamePair(t)
	version := currentVersion(t, s)

	makeReadOnly(t, workingCopyPath(game))

	rec := save(t, s, "ja", version, rowEdit{Line: 3, Key: keyKept2, Translation: "やあ！"})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
	got := decode[rowsResponse](t, rec.Body.Bytes())
	if !got.Results[0].Saved {
		t.Fatalf("saved=false で返している: %+v", got.Results[0])
	}
	if got.Results[0].Warning == "" {
		t.Error("ゲームへ届いていないことを断っていない")
	}
	if published := readFile(t, publishedPath(root)); !strings.Contains(published, "やあ！") {
		t.Errorf("コミットする側に入っていない:\n%s", published)
	}
	// 版は動いていない。画面はそのまま送り直せる。
	if got.Version != version {
		t.Errorf("版が動いている: got %q, want %q", got.Version, version)
	}
}

func TestSaveWithoutGameWritesOnlyOneFile(t *testing.T) {
	// --game が無いときは、この変更の前と同じ。保存先は1つで、publish が
	// 入力に選ぶファイル（リポジトリの作業コピー）だけが変わる。
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
	// --verbose の記録に原文と訳を出さない。2つ書きで記録を1行増やしたので、
	// そちらにも中身が混じっていないことを見る。
	root := newTestRoot(t)
	game := newTestGame(t)
	var log strings.Builder
	s := newTestServer(t, Options{Root: root, Game: game, UILang: "ja", Verbose: true, Stderr: &log})
	version := currentVersion(t, s)

	if rec := save(t, s, "ja", version,
		rowEdit{Line: 3, Key: keyKept2, Translation: jaTyped}); rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
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

// countLines は行数を数える。
func countLines(body string) int {
	return strings.Count(body, "\n")
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
