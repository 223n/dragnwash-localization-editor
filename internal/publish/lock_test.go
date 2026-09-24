package publish

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// lockHelperEnv は、[TestLockFileAcrossProcesses] が子のプロセスとして自分を
// 起動するときに、錠を掛けるファイルを渡す環境変数。
const lockHelperEnv = "DWLOC_LOCK_HELPER"

// shortLockWait は、錠を待つ上限を試験の間だけ短くする。
func shortLockWait(t *testing.T, d time.Duration) {
	t.Helper()
	old := lockWait
	lockWait = d
	t.Cleanup(func() { lockWait = old })
}

// TestLockFileIsExclusive は、同じファイルの錠を2つ同時に取れないことと、放したら
// 取れること、放したあとに横のファイルを残さないことを見る。
//
// 同じプロセスの中でも、別に開いたものどうしは競り合う（flock は開いたファイルごと、
// LockFileEx はハンドルごと）。画面の待ち受けを2つ動かしても、publish と画面を
// 同時に動かしても、書き込みは直列になる。
func TestLockFileIsExclusive(t *testing.T) {
	shortLockWait(t, 200*time.Millisecond)
	dir := t.TempDir()
	path := filepath.Join(dir, "strings.csv")
	if err := os.WriteFile(path, []byte("key,translation\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	unlock, err := LockFile(path)
	if err != nil {
		t.Fatalf("錠を取れない: %v", err)
	}
	if _, err := os.Stat(path + lockSuffix); err != nil {
		t.Errorf("錠を持っているあいだ、横のファイルが無い: %v", err)
	}

	start := time.Now()
	_, err = LockFile(path)
	var timeout *LockTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("2つ目の錠 = %v、*LockTimeoutError を期待", err)
	}
	if waited := time.Since(start); waited < 200*time.Millisecond {
		t.Errorf("待たずに諦めた: %v", waited)
	}
	if timeout.Path != path || !strings.Contains(timeout.Error(), path) || !strings.Contains(timeout.Error(), "ほかの dwloc") {
		t.Errorf("誤り = %+v（%q）", timeout, timeout.Error())
	}

	unlock()
	again, err := LockFile(path)
	if err != nil {
		t.Fatalf("放したのに取れない: %v", err)
	}
	again()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("横のファイルが残った: %v", names)
	}
}

// TestLockFileAcrossProcesses は、別のプロセスが持っている錠を取れず、そのプロセスが
// 放したら取れることを見る。OS の錠がプロセスをまたいで効くことの確かめである。
//
// 子のプロセスは、この試験のバイナリを lockHelperEnv を付けて起動し直したもので、
// 錠を取ったら "locked" と書き、標準入力が閉じるまで持ち続ける。
func TestLockFileAcrossProcesses(t *testing.T) {
	if path := os.Getenv(lockHelperEnv); path != "" {
		unlock, err := LockFile(path)
		if err != nil {
			os.Stdout.WriteString("error: " + err.Error() + "\n")
			os.Exit(1)
		}
		os.Stdout.WriteString("locked\n")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		unlock()
		os.Exit(0)
	}

	shortLockWait(t, 300*time.Millisecond)
	path := filepath.Join(t.TempDir(), "strings.csv")
	if err := os.WriteFile(path, []byte("key,translation\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestLockFileAcrossProcesses$")
	cmd.Env = append(os.Environ(), lockHelperEnv+"="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("子のプロセスが錠を取れない: %q %v", line, err)
	}

	var timeout *LockTimeoutError
	if _, err := LockFile(path); !errors.As(err, &timeout) {
		t.Fatalf("別のプロセスが持っている錠を取れた、または別の誤り: %v", err)
	}

	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("子のプロセスが失敗した: %v", err)
	}
	unlock, err := LockFile(path)
	if err != nil {
		t.Fatalf("子のプロセスが放したのに取れない: %v", err)
	}
	unlock()
}

// TestLockFileFollowsSymlink は、シンボリックリンクの錠を、たどった先の実体の横で
// 取ることを見る。書き出し（[WriteBytes]）も実体へ書くので、リンクと実体のどちらの
// 名前から書いても同じ錠で直列になる。
func TestLockFileFollowsSymlink(t *testing.T) {
	shortLockWait(t, 100*time.Millisecond)
	dir := t.TempDir()
	real := filepath.Join(dir, "real.csv")
	if err := os.WriteFile(real, []byte("key,translation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.csv")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("シンボリックリンクを作れない環境なので飛ばす: %v", err)
	}

	unlock, err := LockFile(link)
	if err != nil {
		t.Fatalf("リンクの錠を取れない: %v", err)
	}
	defer unlock()
	if _, err := os.Stat(real + lockSuffix); err != nil {
		t.Errorf("実体の横に錠のファイルが無い: %v", err)
	}
	if _, err := os.Lstat(link + lockSuffix); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("リンクの横に錠のファイルがある: %v", err)
	}
	if _, err := LockFile(real); err == nil {
		t.Error("実体の名前から、リンクで持っている錠を取れた")
	}
}

// TestLockFileErrors は、錠のファイルを置けないときと、リンク先をたどれないときに、
// 誤りを返すことを見る。
func TestLockFileErrors(t *testing.T) {
	if _, err := LockFile(filepath.Join(t.TempDir(), "無いフォルダー", "strings.csv")); err == nil {
		t.Error("無いフォルダーに錠を置けたことになっている")
	}

	dir := t.TempDir()
	link := filepath.Join(dir, "broken.csv")
	if err := os.Symlink(filepath.Join(dir, "無い.csv"), link); err != nil {
		t.Skipf("シンボリックリンクを作れない環境なので飛ばす: %v", err)
	}
	if _, err := LockFile(link); err == nil {
		t.Error("たどれないリンクで錠を取れたことになっている")
	}
}

// TestLockFileInAFolderThatCannotBeWritten は、書き出し先のフォルダーに書けない
// （錠のファイルを作れない）とき、錠を掛けずに通すことを見る。書き出しも同じ理由で
// 失敗するので、呼び出し側は書き出しの誤りで知らせる（publish の「書き出せません」）。
//
// POSIX でフォルダーの書き込み権を外して確かめる。Windows はフォルダーの読み取り
// 専用の属性ではファイルの作成を止められないので飛ばす。root で走っていても飛ばす。
func TestLockFileInAFolderThatCannotBeWritten(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows はフォルダーの属性でファイルの作成を止められないので飛ばす")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "strings.csv")
	if err := os.WriteFile(path, []byte("key,translation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if probe, err := os.CreateTemp(dir, "probe*"); err == nil {
		probe.Close()
		_ = os.Remove(probe.Name())
		t.Skip("書き込み権を外しても書けたので飛ばす。root で走っていると効かない")
	}

	unlock, err := LockFile(path)
	if err != nil {
		t.Fatalf("書けないフォルダーで誤りを返した: %v", err)
	}
	unlock()
	if err := WriteBytes(path, []byte("x")); err == nil {
		t.Error("前提が崩れた: 書けないフォルダーへ書けた")
	}
}

// TestSameFile は、開いているファイルと、いまその名前が指すファイルが同じかを
// 見分けることを見る。錠を取るあいだに前の持ち主が名前を消して、別のファイルが
// 作られた場合に、取り直すための見分けである。
func TestSameFile(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "x"+lockSuffix)
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !sameFile(f, name) {
		t.Error("同じファイルを違うと言う")
	}
	if sameFile(f, filepath.Join(dir, "無い")) {
		t.Error("無い名前を同じと言う")
	}
	other := filepath.Join(dir, "y")
	if err := os.WriteFile(other, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if sameFile(f, other) {
		t.Error("別のファイルを同じと言う")
	}
	closed, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	if sameFile(closed, name) {
		t.Error("閉じたファイルを同じと言う")
	}
}
