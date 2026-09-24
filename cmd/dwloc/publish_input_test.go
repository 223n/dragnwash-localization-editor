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
	checkContains(t, "stderr", stderr, []string{publishInputChangedText, "dwloc:   Translations/ja/strings.csv\n"})
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

// TestPublishLocksTheSameInputOnce は、--path に同じファイルを2度渡しても、錠を
// 1度だけ掛けて書けることを見る。同じ錠を同じプロセスの中で2度取ろうとすると、
// 自分を待って上限まで止まり、書けない。
func TestPublishLocksTheSameInputOnce(t *testing.T) {
	root := publishTree(t, "ja")
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
