package web

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
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
	//
	// 並べているのがゲーム側でも、案内は「dwloc publish」でよい。publish も
	// --game を省いたときにゲームのフォルダーを探すからである。片方だけ探して
	// いたころは、この案内どおり打った publish が公開ファイル自身を入力にして
	// 「変更なし」で終わり、訳が届かなかったことに気づく手がかりが出なかった。
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

// TestPublishNoteIsTheSameOnBothSides は、ゲーム側でもリポジトリ側でも、
// publish の案内が同じ1つであることを見る。
//
// 一度は「ゲーム側のときだけ publish --game と言う」形にした。--game を省いた
// edit がゲームのフォルダーを探すのに、publish は探さなかったためである。
// その非対称そのものが誤りだったので、publish も探すようにして案内を1つへ戻した。
// 案内が2つに割れていると、どちらを読ませるかの判定が画面側に増え、
// 判定が外れたときに黙って訳が届かない道ができる。
func TestPublishNoteIsTheSameOnBothSides(t *testing.T) {
	t.Run("ゲーム側", func(t *testing.T) {
		root := newEditRoot(t)
		game := newEditGame(t)
		s := newTestServer(t, Options{Root: root, Game: game, UILang: "ja"})

		notes := getLines(t, s, "ja").Notes
		if want := viaPublishNote(s, root); !hasNote(notes, want) {
			t.Errorf("publish の断りが無い\n--- 期待 ---\n%s\n--- 断り書き ---\n%q",
				want, notes)
		}
		checkAnnounce(t, s, "server.publish_hint")
	})

	t.Run("リポジトリ側", func(t *testing.T) {
		root := newEditRoot(t)
		s := newTestServer(t, Options{Root: root, UILang: "ja"})

		notes := getLines(t, s, "ja").Notes
		if want := viaPublishNote(s, root); !hasNote(notes, want) {
			t.Errorf("publish の断りが無い\n--- 期待 ---\n%s\n--- 断り書き ---\n%q",
				want, notes)
		}
		checkAnnounce(t, s, "server.publish_hint")
	})
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
	//
	// 標準エラーを控えるのは、理由がそこにしか出ないからである（応答には
	// 誤りの中身を載せない）。edit は --game を省いてもゲームのフォルダーを
	// 探すので、書けない PC では、指定を打っていない人の画面でも保存だけが
	// 落ち続ける。
	var log strings.Builder
	root := newTestRoot(t)
	game := newTestGame(t)
	s := newTestServer(t, Options{Root: root, Game: game, UILang: "ja", Stderr: &log})
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
	// 端末には、どのロケールで失敗したかと理由を出す。理由が出ないと、権限で
	// 拒まれたのか待てば直るのかが、画面からも端末からも分からない。
	if got := log.String(); !strings.Contains(got, "save failed locale=ja") {
		t.Errorf("記録に失敗が出ていない: %q", got)
	}
	if got := log.String(); !strings.Contains(got, "ja.working.csv") {
		t.Errorf("記録に書けなかったファイルが出ていない: %q", got)
	}
	// 理由を出しても、行の中身は出さない（[TestGameSaveLogHasNoRowContent] と
	// 同じ約束。あちらは --verbose の記録を見ている）。
	if got := log.String(); strings.Contains(got, "やあ！") {
		t.Errorf("記録に訳が出ている: %q", got)
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

// viaPublishNote は「コミットする側へは publish で入る」という断り書きを、
// 目録から組み立てて返す。
//
// 鍵は1つしかない。ゲーム側とリポジトリ側で文面を分けていたことがあるが、
// publish も --game を省いたときにゲームのフォルダーを探すようにしたので、
// どちらでも「dwloc publish」で正しくなった（[TestPublishNoteIsTheSameOnBothSides]）。
func viaPublishNote(s *server, root string) string {
	return s.cat.T(s.cat.lookup("ja"), "note.via_publish",
		"path", s.displayPath(publishedPath(root)))
}

// checkAnnounce は、起動時に標準出力へ出す案内に、目録の key の文面が
// そのまま出ていることを確かめる。
//
// 画面の断り書きまで読まずに打ち始める人が読むのはこちらなので、画面と
// 同じ向きにそろっていることを別に見張る。試験に日本語を書き写さず目録から
// 引くのは、目録を直したときにこちらだけが古いまま通るのを避けるためである。
func checkAnnounce(t *testing.T, s *server, key string) {
	t.Helper()

	var out strings.Builder
	s.stdout = &out
	s.announce("http://127.0.0.1:54321/?t=x")
	want := s.t(key)
	if !strings.Contains(out.String(), want) {
		t.Errorf("起動時の1行に %s が出ていない\n--- 期待 ---\n%s\n--- 標準出力 ---\n%s",
			key, want, out.String())
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

// makeReadOnly は path への保存が失敗するようにする。
//
// 閉じる相手が OS で違う。[publish.WriteBytes] は同じディレクトリに一時ファイルを
// 作ってから [os.Rename] で置き換えるので、止まる場所が違うためである。
//
//   - Windows … ファイルに読み取り専用の属性が付くと、その置き換えが拒まれる
//   - POSIX … rename の可否はディレクトリの書き込み権で決まる。ファイルの
//     モードを落としても、同じディレクトリに作った一時ファイルからの置き換えは
//     通ってしまうので、ディレクトリのほうを閉じる
//
// ファイルだけを 0444 にしていたころは、Windows で通り Linux の CI で落ちていた。
// 落ちたのは保存が失敗しなかったからで、製品側は正しく保存できていた。
// 「書けない状態」を作れていないのに、書けなかったときの振る舞いを見ていた。
//
// 実際に書けなくなったことを確かめてから返す。root で走るとどちらの手も効かず、
// 同じことがまた起きるためである。効かないときは試験を飛ばす。
func makeReadOnly(t *testing.T, path string) {
	t.Helper()

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	target := path
	var closed, open os.FileMode = 0o444, 0o644
	if runtime.GOOS != "windows" {
		target, closed, open = filepath.Dir(path), 0o555, 0o755
	}
	if err := os.Chmod(target, closed); err != nil {
		t.Fatal(err)
	}
	// 後始末で消せるように戻す。t.TempDir の削除は読み取り専用で失敗しうる。
	// t.Cleanup は後入れ先出しなので、ここは TempDir の削除より先に走る。
	t.Cleanup(func() { _ = os.Chmod(target, open) })

	// 確かめ方は、製品が保存に使う経路そのものである。判定を書き写すと、
	// 書き方が変わったときにここだけ古いままになる。
	// 書くのはいま入っている中身なので、通ってしまってもファイルは変わらない。
	if err := publish.WriteBytes(path, before); err == nil {
		t.Skipf("%s を書けない状態にできない。root で走っていると効かない", target)
	}
}

// newEditGame は [newEditRoot] と同じ形の作業コピーをゲーム側に作る。
//
// リポジトリ側（newEditRoot が置く Translations/_discovered/ja.working.csv）と
// 行の並びまでそろえてある。そろえてあるので、6行目へ保存したあとにどちらの
// ファイルが変わったかで、どちらを読んだかが分かる。
func newEditGame(t *testing.T) string {
	t.Helper()

	game := filepath.Join(t.TempDir(), "BepInEx", "plugins", "DragNWashLocalization")
	dir := filepath.Join(game, "Translations", "_discovered")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"key,section,node,order,speaker,source_en,translation",
		"",
		"# ===== Level 1: Ryan (Sunny) =====",
		"# --- intro: Ryan_1_intro ---",
		key.For(srcHello) + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + "," + jaHello,
		key.For(srcBye) + ",L01 Ryan,Ryan_1_intro,2,Ryan," + srcBye + ",",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "ja.working.csv"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return game
}

// TestGameWorkingCopyComesFirst は、両方に作業コピーがあるときゲーム側を
// 読むことを見る。
//
// 以前はリポジトリ側が先だった。順を覆したのは、Modが書き出した生のファイルを
// 常に入力にするためである（[publish.DiscoverTargetsWithGame] の呼び先の
// doc コメント）。代わりに、自分で <ルート>/Translations/_discovered へ置いた
// ファイルは、ゲーム側に同じロケールの作業コピーがあるかぎり読まれない。
// 黙ったままにはならず、読んだファイルは画面の「ファイル」の欄に出る。
func TestGameWorkingCopyComesFirst(t *testing.T) {
	root := newEditRoot(t)
	game := newEditGame(t)
	s := newTestServer(t, Options{Root: root, Game: game, UILang: "ja"})

	if got, want := inputPath(t, s, "ja"), workingCopyPath(game); got != want {
		t.Errorf("読む先が %q、%q を期待", got, want)
	}
	// 画面にも出る。どのファイルを読んだかが分からないまま直させない。
	if path := getLines(t, s, "ja").Path; !strings.Contains(path, "_discovered") ||
		!strings.Contains(filepath.ToSlash(path), filepath.ToSlash(game)) {
		t.Errorf("画面の「ファイル」が %q。ゲーム側の作業コピーを指していない", path)
	}
}

// TestEditAndPublishPickTheSameInput は、edit が書いた訳が publish に拾われる
// ことを見る。
//
// この2つが同じファイルを選ぶことは、偶然ではなく約束である。どちらも
// [publish.DiscoverTargetsWithGame] を通っていて、作業コピーを探す順は
// 1か所（internal/publish の workingCopy）にしかない。片方だけ順を変えると、
// edit で入れた訳が publish に拾われず、コミットする側へ1行も届かない。
// 訳の入っていない行は公開ファイルに存在しない（publish が訳の空の行を
// 書かない）ので、あとから拾い直すすべもない。
func TestEditAndPublishPickTheSameInput(t *testing.T) {
	root := newEditRoot(t)
	game := newEditGame(t)
	s := newTestServer(t, Options{Root: root, Game: game, UILang: "ja"})

	inRepo := filepath.Join(root, "Translations", "_discovered", "ja.working.csv")
	repoBefore := readFile(t, inRepo)

	// 画面から1行保存する。6行目は訳が空の行（newEditRoot の注記）。
	lines := getLines(t, s, "ja")
	rec := save(t, s, "ja", lines.Version, rowEdit{Line: 6, Translation: jaTyped})
	if rec.Code != http.StatusOK {
		t.Fatalf("保存の状態コードが %d\n%s", rec.Code, rec.Body.String())
	}

	// 書いたのはゲーム側。リポジトリ側は1バイトも触っていない。
	if got := readFile(t, workingCopyPath(game)); !strings.Contains(got, jaTyped) {
		t.Errorf("ゲーム側の作業コピーに訳が入っていない:\n%s", got)
	}
	if got := readFile(t, inRepo); got != repoBefore {
		t.Errorf("リポジトリ側の作業コピーが変わっている\n--- 前 ---\n%s\n--- 後 ---\n%s",
			repoBefore, got)
	}

	// publish が同じファイルを入力に選び、その訳を公開ファイルへ載せる。
	data, err := publish.LoadOrder(root)
	if err != nil {
		t.Fatalf("再生順を読めない: %v", err)
	}
	targets, err := publish.DiscoverTargetsWithGame(root, game)
	if err != nil {
		t.Fatalf("対象を列挙できない: %v", err)
	}
	var ja *publish.Target
	for i := range targets {
		if targets[i].Locale == "ja" {
			ja = &targets[i]
		}
	}
	if ja == nil {
		t.Fatal("ja が publish の対象に無い")
	}
	// 選んだファイルが、画面が書いた先と同じであること。ここがずれると、
	// 下の「公開ファイルに載る」も別のファイルの話になる。
	if ja.Input != inputPath(t, s, "ja") {
		t.Fatalf("publish の入力が %q、画面の書き先は %q", ja.Input, inputPath(t, s, "ja"))
	}
	out, _, err := publish.BuildTarget(data, *ja)
	if err != nil {
		t.Fatalf("公開CSVを組み立てられない: %v", err)
	}
	if !strings.Contains(string(out), jaTyped) {
		t.Errorf("画面で入れた訳が公開ファイルに載らない:\n%s", out)
	}
}
