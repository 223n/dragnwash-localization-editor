package publish

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestMain は、錠のファイルを試験用の一時フォルダーに置かせる（[LockDirEnv]）。
// 利用者のキャッシュのフォルダーへ、試験が作った一時パスの錠のファイルを残さない。
// 子のプロセス（[TestLockFileAcrossProcesses]）は親の値を受け継ぐので、上書きしない。
func TestMain(m *testing.M) {
	os.Exit(withLockDir(m))
}

// withLockDir は、LockDirEnv が無ければ一時フォルダーを作って渡し、試験を走らせる。
func withLockDir(m *testing.M) int {
	if os.Getenv(LockDirEnv) != "" {
		return m.Run()
	}
	dir, err := os.MkdirTemp("", "dwloc-locks-test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	os.Setenv(LockDirEnv, dir)
	defer os.Unsetenv(LockDirEnv)
	return m.Run()
}

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
// 取れることと、錠のファイルを書き出し先のフォルダーに置かないことを見る。
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
	name, err := lockName(path)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(name) != os.Getenv(LockDirEnv) {
		t.Errorf("錠のファイルが %s にある。%s を期待", name, os.Getenv(LockDirEnv))
	}
	if _, err := os.Stat(name); err != nil {
		t.Errorf("錠のファイルが無い: %v", err)
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
		t.Errorf("書き出し先のフォルダーにファイルが増えた: %v", names)
	}
}

// TestLockFileUnderContention は、いくつもの取り手が短い間隔で錠を取っては放しても、
// 2つが同時に錠を持たず、誤りも出ないことを見る。
//
// 放すときに錠のファイルを消していたころは、Windows でこの試験が落ちた。消した
// ファイルをほかの取り手が開けず（削除の保留中で拒まれる）、2つが同時に錠を持つ
// こともあった。
func TestLockFileUnderContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strings.csv")
	if err := os.WriteFile(path, []byte("key,translation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var inside, doubles, errs int32
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				unlock, err := LockFile(path)
				if err != nil {
					atomic.AddInt32(&errs, 1)
					continue
				}
				if atomic.AddInt32(&inside, 1) > 1 {
					atomic.AddInt32(&doubles, 1)
				}
				time.Sleep(100 * time.Microsecond)
				atomic.AddInt32(&inside, -1)
				unlock()
			}
		}()
	}
	wg.Wait()
	if doubles != 0 || errs != 0 {
		t.Errorf("2つが同時に錠を持った回数 %d、誤り %d", doubles, errs)
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

// TestLockNameIsTheSameForTheSameFile は、同じファイルを指す綴りの違い（相対パス、
// 途中の「.」、Windows と macOS の大文字小文字）が同じ錠のファイルになることと、
// 別のファイルは別の錠のファイルになることを見る。
func TestLockNameIsTheSameForTheSameFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "strings.csv")
	if err := os.WriteFile(path, []byte("key,translation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want, err := lockName(path)
	if err != nil {
		t.Fatal(err)
	}
	spellings := []string{filepath.Join(dir, ".", "strings.csv")}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		spellings = append(spellings, filepath.Join(dir, "STRINGS.CSV"))
	}
	t.Chdir(dir)
	spellings = append(spellings, "strings.csv")
	for _, s := range spellings {
		if got, err := lockName(s); err != nil || got != want {
			t.Errorf("%s の錠のファイル = %s（%v）、%s を期待", s, got, err, want)
		}
	}
	other, err := lockName(filepath.Join(dir, "other.csv"))
	if err != nil || other == want {
		t.Errorf("別のファイルの錠のファイル = %s（%v）", other, err)
	}
}

// TestLockFileFollowsSymlink は、シンボリックリンクの錠を、たどった先の実体で取る
// ことを見る。書き出し（[WriteBytes]）も実体へ書くので、リンクと実体のどちらの
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
	if _, err := LockFile(real); err == nil {
		t.Error("実体の名前から、リンクで持っている錠を取れた")
	}
}

// TestLockFileErrors は、リンク先をたどれないときと、錠のファイルを置けないときに、
// 誤りを返すことを見る。
func TestLockFileErrors(t *testing.T) {
	dir := t.TempDir()
	// 錠のファイルを置くフォルダーの代わりに、ふつうのファイルを置く。
	notDir := filepath.Join(dir, "file")
	if err := os.WriteFile(notDir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(LockDirEnv, filepath.Join(notDir, "locks"))
	if _, err := LockFile(filepath.Join(dir, "strings.csv")); err == nil {
		t.Error("錠のファイルを置けないのに錠を取れたことになっている")
	}

	link := filepath.Join(dir, "broken.csv")
	if err := os.Symlink(filepath.Join(dir, "無い.csv"), link); err != nil {
		t.Skipf("シンボリックリンクを作れない環境なので飛ばす: %v", err)
	}
	if _, err := LockFile(link); err == nil {
		t.Error("たどれないリンクで錠を取れたことになっている")
	}
}

// TestLockDir は、錠のファイルを置くフォルダーの選び方を見る。環境変数があれば
// それを使い、無ければ利用者のキャッシュのフォルダーの dwloc/locks を使う。
func TestLockDir(t *testing.T) {
	t.Setenv(LockDirEnv, "")
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("利用者のキャッシュのフォルダーが無い環境なので飛ばす: %v", err)
	}
	if got, want := lockDir(), filepath.Join(cache, "dwloc", "locks"); got != want {
		t.Errorf("lockDir() = %s、%s を期待", got, want)
	}
	t.Setenv(LockDirEnv, "somewhere")
	if got := lockDir(); got != "somewhere" {
		t.Errorf("lockDir() = %s、環境変数の値を期待", got)
	}
}

// TestSameFile は、開いているファイルと、いまその名前が指すファイルが同じかを
// 見分けることを見る。錠を取るあいだに錠のファイルが消されて、別のファイルが
// 作られた場合に、取り直すための見分けである。
func TestSameFile(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "x.lock")
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
