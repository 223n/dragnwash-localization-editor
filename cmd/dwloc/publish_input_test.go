package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// setBeforePublishWrite は、試験のあいだだけ beforePublishWrite を差し替える。
func setBeforePublishWrite(t *testing.T, fn func()) {
	t.Helper()
	old := beforePublishWrite
	beforePublishWrite = fn
	t.Cleanup(func() { beforePublishWrite = old })
}

// TestPublishStopsWhenTheInputChangesAfterBuilding は、組み立てたあとに入力が書き
// 換わったら、1バイトも書かずに止まることを見る（改善の決定 3）。
//
// 作業コピーの無いロケールでは、入力と書き出し先が同じファイルになる。組み立てた
// あとに画面（dwloc edit）が訳を保存してから publish が書くと、その訳が消える。
// 書く直前に入力を読み直し、組み立てたときのバイトと違えば止める。
func TestPublishStopsWhenTheInputChangesAfterBuilding(t *testing.T) {
	root := publishTree(t, "ja")
	path := filepath.Join(root, "Translations", "ja", "strings.csv")
	edited := workingCSV + ",Bye,さよなら\n"
	setBeforePublishWrite(t, func() {
		if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
			t.Error(err)
		}
	})

	code, stdout, stderr := runCLI("publish", "--root", root)
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	checkContains(t, "stderr", stderr, []string{publishFilesChangedText, "dwloc:   Translations/ja/strings.csv\n"})
	if strings.Contains(stdout, "書き出しました") {
		t.Errorf("書き出したと言っている:\n%s", stdout)
	}
	if got := readFile(t, root, "Translations/ja/strings.csv"); got != edited {
		t.Errorf("画面が保存した訳を消した:\n%s", got)
	}

	// もう一度回せば、書き換わったあとの入力から書く。
	setBeforePublishWrite(t, func() {})
	if code, stdout, stderr := runCLI("publish", "--root", root); code != exitOK {
		t.Fatalf("2回目の終了コード = %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	checkContains(t, "出力", readFile(t, root, "Translations/ja/strings.csv"), []string{"こんにちは", "さよなら"})
}

// TestPublishStopsWhenTheInputCannotBeReadAgain は、書く直前に入力を読み直せなければ、
// 1バイトも書かずに止まることを見る（終了コード 2）。
func TestPublishStopsWhenTheInputCannotBeReadAgain(t *testing.T) {
	root := publishTree(t, "ja")
	path := filepath.Join(root, "Translations", "ja", "strings.csv")
	setBeforePublishWrite(t, func() {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
	})
	code, _, stderr := runCLI("publish", "--root", root)
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstderr:\n%s", code, exitError, stderr)
	}
	checkContains(t, "stderr", stderr, []string{"Translations/ja/strings.csv を読み直せないので、1バイトも書きませんでした"})
	if _, err := os.Stat(path); err == nil {
		t.Error("読み直せなかったのに書いた")
	}
}

// TestPublishWaitsForTheInputLock は、画面の保存（dwloc edit）が入力の錠を持っている
// あいだ、publish が入力を読み直さずに待ち、放されたら書くことを見る。
//
// 錠を持つのは publish.LockFile を呼んだこの試験で、画面の保存の代わりである。
// 読み直しが錠の中にあることは TestPublishRereadsTheInputInsideTheLock が見る。
func TestPublishWaitsForTheInputLock(t *testing.T) {
	root := publishTree(t, "ja")
	path := filepath.Join(root, "Translations", "ja", "strings.csv")
	unlock, err := publish.LockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		code           int
		stdout, stderr string
	}
	done := make(chan result, 1)
	go func() {
		code, stdout, stderr := runCLI("publish", "--root", root)
		done <- result{code, stdout, stderr}
	}()

	select {
	case r := <-done:
		unlock()
		t.Fatalf("錠を持っているあいだに publish が終わった: %d\n%s", r.code, r.stderr)
	case <-time.After(150 * time.Millisecond):
	}
	if got := readFile(t, root, "Translations/ja/strings.csv"); got != workingCSV {
		t.Fatalf("錠を持っているあいだに書いた:\n%s", got)
	}
	unlock()
	r := <-done
	if r.code != exitOK {
		t.Fatalf("放したあとの終了コード = %d\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
	}
	checkContains(t, "stdout", r.stdout, []string{"1 件を書き出しました。"})
}

// TestPublishRereadsTheInputInsideTheLock は、書く直前の入力の読み直しを錠の中で
// 行うことを見る。
//
// 試験が入力の錠を持っているあいだに入力を書き換えてから放す。錠を持つ画面の保存
// （dwloc edit）が訳を書いた場面である。読み直しが錠を取る前にあると、書き換える前の
// 中身と比べて通り、錠を待ったあとで、画面が保存した訳を消して書く。錠の中で読み
// 直せば、書き換えたあとの中身と比べるので、1バイトも書かずに止まる。
func TestPublishRereadsTheInputInsideTheLock(t *testing.T) {
	root := publishTree(t, "ja")
	path := filepath.Join(root, "Translations", "ja", "strings.csv")
	unlock, err := publish.LockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		code           int
		stdout, stderr string
	}
	done := make(chan result, 1)
	go func() {
		code, stdout, stderr := runCLI("publish", "--root", root)
		done <- result{code, stdout, stderr}
	}()
	select {
	case r := <-done:
		unlock()
		t.Fatalf("錠を持っているあいだに publish が終わった: %d\n%s", r.code, r.stderr)
	case <-time.After(150 * time.Millisecond):
	}

	// 錠を持つ側（画面の保存）が、訳を1行足す。
	edited := workingCSV + ",Bye,さよなら\n"
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()

	r := <-done
	if r.code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", r.code, exitProblems, r.stdout, r.stderr)
	}
	checkContains(t, "stderr", r.stderr, []string{publishFilesChangedText})
	if got := readFile(t, root, "Translations/ja/strings.csv"); got != edited {
		t.Errorf("画面が保存した訳を消した:\n%s", got)
	}
}

// gameInputTree は、ゲーム側の作業コピーを入力にし、リポジトリの公開ファイルへ書く
// publish の見本を作る。返すのはリポジトリのルートとゲームのフォルダー。
//
// 画面を --no-game で開くと、画面はリポジトリの公開ファイルを開いて保存する。publish の
// 入力（ゲーム側の作業コピー）と、画面が書くファイル（publish の書き出し先）が別になる
// 組み合わせである。
//
// ゲーム側の公開ファイル（作業コピーの土台）は、リポジトリの公開ファイルと同じにする
// （ゲームに最新の翻訳が入っている）。
func gameInputTree(t *testing.T) (root, game string) {
	t.Helper()
	root = lossRepo(t)
	return root, makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": workingBoth,
		"Translations/ja/strings.csv":             readFile(t, root, jaPublishedPath),
	})
}

// screenSaved は、画面（dwloc edit --no-game）が公開ファイルに訳を保存したあとの中身。
// lossRepo の公開ファイルの訳を書き換えたもの。
const screenSaved = "key,section,node,order,speaker,translation\n" +
	keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？（画面で直した）\n"

// TestPublishLocksTheOutputToo は、publish が入力だけでなく書き出し先にも錠を掛け、
// 錠の中で書き出し先を読み直すことを見る。
//
// ゲーム側の作業コピーを入力にした publish と、公開ファイルを開いた画面（--no-game）が
// 同時に動く場面である。画面の保存は公開ファイルに錠を掛けて書く。publish が入力に
// しか錠を掛けないと、画面の保存を待たずに、保存する前の公開ファイルから組み立てた
// 中身で上書きし、画面が「保存済み」と出した訳が消える（検証で、実物の写しを使って
// Windows で60回中2回、Linux で100回中7回再現した）。
func TestPublishLocksTheOutputToo(t *testing.T) {
	root, game := gameInputTree(t)
	output := filepath.Join(root, filepath.FromSlash(jaPublishedPath))
	unlock, err := publish.LockFile(output)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		code           int
		stdout, stderr string
	}
	done := make(chan result, 1)
	go func() {
		code, stdout, stderr := runCLI("publish", "--root", root, "--game", game)
		done <- result{code, stdout, stderr}
	}()
	select {
	case r := <-done:
		unlock()
		t.Fatalf("書き出し先の錠を持っているあいだに publish が終わった: %d\n%s", r.code, r.stderr)
	case <-time.After(150 * time.Millisecond):
	}

	// 錠を持つ側（画面の保存）が、公開ファイルの訳を書き換える。
	if err := os.WriteFile(output, []byte(screenSaved), 0o644); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()

	r := <-done
	if r.code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", r.code, exitProblems, r.stdout, r.stderr)
	}
	checkContains(t, "stderr", r.stderr, []string{publishFilesChangedText, "dwloc:   Translations/ja/strings.csv\n"})
	if got := readFile(t, root, jaPublishedPath); got != screenSaved {
		t.Errorf("画面が保存した訳を消した:\n%s", got)
	}
}

// TestPublishStopsWhenTheOutputChangesAfterBuilding は、組み立てたあとに書き出し先が
// 書き換わったら、入力が変わっていなくても、1バイトも書かずに止まることを見る。
// 組み立て（前の公開ファイルからの引き継ぎ）と失われる訳の確かめは、書き換える前の
// 中身で行っている。
func TestPublishStopsWhenTheOutputChangesAfterBuilding(t *testing.T) {
	root, game := gameInputTree(t)
	output := filepath.Join(root, filepath.FromSlash(jaPublishedPath))
	setBeforePublishWrite(t, func() {
		if err := os.WriteFile(output, []byte(screenSaved), 0o644); err != nil {
			t.Error(err)
		}
	})

	code, stdout, stderr := runCLI("publish", "--root", root, "--game", game)
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	checkContains(t, "stderr", stderr, []string{publishFilesChangedText, "dwloc:   Translations/ja/strings.csv\n"})
	if strings.Contains(stderr, "ja.working.csv") {
		t.Errorf("変わっていない入力を変わったと言っている:\n%s", stderr)
	}
	if got := readFile(t, root, jaPublishedPath); got != screenSaved {
		t.Errorf("画面が保存した訳を消した:\n%s", got)
	}

	// もう一度回せば、書き換わったあとの公開ファイルで確かめる。コミットする側の訳が
	// ゲーム側の公開ファイルと食い違うので、今度は「ゲームに入っている翻訳が古い」で
	// 止まり、画面の訳は残る（順に動かしたときと同じ）。
	setBeforePublishWrite(t, func() {})
	if code, _, stderr := runCLI("publish", "--root", root, "--game", game); code != exitProblems ||
		!strings.Contains(stderr, "ゲームに入っている翻訳が古いので") {
		t.Fatalf("2回目の終了コード = %d、ゲームに入っている翻訳が古いので止まることを期待\n%s", code, stderr)
	}
	if got := readFile(t, root, jaPublishedPath); got != screenSaved {
		t.Errorf("2回目で画面が保存した訳を消した:\n%s", got)
	}
}

// TestPublishStopsWhenTheGameBaseChangesAfterBuilding は、組み立てたあとにゲーム側の
// 公開ファイルが書き換わったら、1バイトも書かずに止まることを見る。「ゲームに入っている
// 翻訳が古い」の確かめは、書き換える前の中身で行っている。dwloc はこのファイルを
// 書かないので錠は掛けず、読み直して比べるだけにする。
func TestPublishStopsWhenTheGameBaseChangesAfterBuilding(t *testing.T) {
	root, game := gameInputTree(t)
	base := filepath.Join(game, "Translations", "ja", "strings.csv")
	setBeforePublishWrite(t, func() {
		if err := os.WriteFile(base, []byte(screenSaved), 0o644); err != nil {
			t.Error(err)
		}
	})
	before := readFile(t, root, jaPublishedPath)

	code, stdout, stderr := runCLI("publish", "--root", root, "--game", game)
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	// ゲームのフォルダーは、製品がリンクを解いた形で出す（8.3 形式の TMP など）。
	shown := filepath.Join(realPath(t, game), "Translations", "ja", "strings.csv")
	checkContains(t, "stderr", stderr, []string{publishFilesChangedText, "dwloc:   " + filepath.ToSlash(shown) + "\n"})
	if got := readFile(t, root, jaPublishedPath); got != before {
		t.Errorf("止めたのに公開ファイルが変わった:\n%s", got)
	}
}

// TestPublishStopsWhenTheOutputCannotBeReadAgain は、書く直前に書き出し先を読み直せ
// なければ（ファイルがフォルダーに置き換わった、など）、1バイトも書かずに止まることを
// 見る（終了コード 2）。
func TestPublishStopsWhenTheOutputCannotBeReadAgain(t *testing.T) {
	root, game := gameInputTree(t)
	output := filepath.Join(root, filepath.FromSlash(jaPublishedPath))
	setBeforePublishWrite(t, func() {
		if err := os.Remove(output); err != nil {
			t.Error(err)
		}
		if err := os.Mkdir(output, 0o755); err != nil {
			t.Error(err)
		}
	})
	code, _, stderr := runCLI("publish", "--root", root, "--game", game)
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstderr:\n%s", code, exitError, stderr)
	}
	checkContains(t, "stderr", stderr, []string{"Translations/ja/strings.csv を読み直せないので、1バイトも書きませんでした"})
}

// TestUsageSaysPublishStopsWhenFilesChange は、使い方の説明（dwloc publish -h と
// dwloc help）が、組み立てたあとに入力か書き出し先が変わって止まる場合（終了コード 1）と、
// 書く直前の錠と読み直しを書いていることを見る。README の終了コードの7つと合わせる。
// 説明に無いと、端末で説明を読んだ人は、終了コード1と「組み立てたあとに入力か書き
// 出し先が変わったので…」の出力を結び付けられない。
func TestUsageSaysPublishStopsWhenFilesChange(t *testing.T) {
	code, stdout, stderr := runCLI("publish", "-h")
	if code != exitOK {
		t.Fatalf("publish -h の終了コード = %d\n%s", code, stderr)
	}
	checkContains(t, "publish -h", stdout, []string{
		"書き込みの錠を\n掛けてから",
		"入力・書き出し先・ゲーム側の公開ファイルを\n読み直します",
		"または組み立てたあとに入力か書き出し先が変わったので止めた",
	})
	code, stdout, stderr = runCLI("help")
	if code != exitOK {
		t.Fatalf("help の終了コード = %d\n%s", code, stderr)
	}
	checkContains(t, "help", stdout, []string{
		"diff が閉じない引用符のファイルを読めず判定して",
		"「読み違える\n      形のファイルがある」",
		"「組み立てたあとに\n      入力か書き出し先が変わった」",
	})
}

// TestPublishLocksTheSameInputOnce は、--path に同じファイルを2度渡しても、錠を
// 1度だけ掛けて書けることを見る。同じ錠を同じプロセスの中で2度取ろうとすると、
// 自分を待って上限まで止まり、書けない。
func TestPublishLocksTheSameInputOnce(t *testing.T) {
	// --path に渡すのは公開ファイルの形（source_en 列の無いもの）。作業コピーの形は
	// 書かずに止まる（TestPublishPathRefusesAWorkingCopy）。
	root := makeTree(t, map[string]string{
		"data/script_order.csv":       scriptOrderCSV,
		"Translations/ja/strings.csv": publishedHello,
	})
	path := filepath.Join(root, "Translations", "ja", "strings.csv")
	start := time.Now()
	code, stdout, stderr := runCLI("publish", "--root", root, "--path", path, "--path", filepath.Join(filepath.Dir(path), ".", "strings.csv"))
	if code != exitOK {
		t.Fatalf("終了コード = %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("錠を待った: %v", took)
	}
	checkContains(t, "stdout", stdout, []string{"2 件を書き出しました。"})
}
