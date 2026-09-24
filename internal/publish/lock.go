package publish

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// LockDirEnv は、錠のファイルを置くフォルダーを変える環境変数。試験が、利用者の
// キャッシュのフォルダーへ錠のファイルを残さないために使う。ふだんは使わない。
const LockDirEnv = "DWLOC_LOCK_DIR"

// lockWait は、ほかの dwloc が錠を持っているときに待つ上限。lockPoll は試す間隔。
//
// 錠を持つのは、版を照合してから rename するまで（画面の保存）か、入力を読み直して
// から書き出し終えるまで（publish）のあいだだけで、ふつうは数ミリ秒から1秒に満たない。
// 待ちきれなかったときは誤りを返し、呼び出し側が送り直す（画面の保存は
// [edit.File.Save] が数回試し、画面はそれでも書けなければ少し置いて送り直す）。
var (
	lockWait = 3 * time.Second
	lockPoll = 10 * time.Millisecond
)

// LockTimeoutError は、待つ上限まで待っても錠を取れなかったことを表す。
type LockTimeoutError struct {
	// Path は錠を掛けようとしたファイル（書き出し先の実体）。
	Path string
	// Wait は待った長さ。
	Wait time.Duration
}

func (e *LockTimeoutError) Error() string {
	return fmt.Sprintf("%s の錠を %v 待っても取れません（ほかの dwloc が同じファイルを書いている最中です）", e.Path, e.Wait)
}

// LockFile は、path への書き込みを、ほかの dwloc（画面の保存と publish）の書き込みと
// 直列にする錠を取る。返す関数で錠を放す（改善の決定 3）。
//
// 錠は OS のもの（Windows は LockFileEx、Linux や macOS は flock）を、錠のためだけの
// ファイルに掛ける。書き出し先そのものに掛けないのは、書き出しが一時ファイルからの
// rename でファイルを置き換える（錠を掛けたファイルが消える）ためと、Windows では
// 錠を掛けたファイルをゲームが読めなくなるためである。OS の錠は、持っているプロセスが
// 終われば OS が放すので、落ちた dwloc が錠を残すことは無い。
//
// 錠のファイルは、利用者のキャッシュのフォルダー（Windows は %LocalAppData%、Linux は
// ~/.cache、macOS は ~/Library/Caches）の dwloc/locks に置き、名前は書き出し先の実体の
// 絶対パスから作る（[lockName]）。書き出し先の横には置かない。横に置くと、作業コピーの
// 無いロケールでは翻訳リポジトリの Translations/<ロケール>/ に錠のファイルが残り、
// git status に出る。放すときに消す形も試したが、Windows では、消したファイルを
// ほかの dwloc が開けない（削除の保留中で拒まれる）ことや、2つの dwloc が同時に錠を
// 持つことがあった（試験で確かめた）。そのため錠のファイルは消さずに残す。中身は空で、
// 書き出し先1つにつき1つである。
//
// path がシンボリックリンクなら、たどった先の実体で名前を作る（[WriteBytes] と同じ
// 実体に書くので、錠も同じ実体で取る）。途中のフォルダーのリンク、大文字小文字
// （Windows と macOS）、Windows の 8.3 形式の短い名前の違いもそろえる。ハードリンクの
// 別の名前から書く道は直列にならない。
//
// 錠は dwloc 同士の約束で、ゲーム（Mod の書き出し）や表計算ソフトは従わない。
// そちらとの競り合いは、版の照合（画面）と書く直前の読み直し（publish）で見つける。
func LockFile(path string) (unlock func(), err error) {
	target, err := resolveLink(path)
	if err != nil {
		return nil, err
	}
	name, err := lockName(target)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return nil, err
		}
		locked, err := tryLock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if locked {
			if sameFile(f, name) {
				return func() {
					unlockFile(f)
					_ = f.Close()
				}, nil
			}
			// 錠を取るあいだに、錠のファイルが消された（人が消した、など）。消えた
			// ファイルの錠は誰とも競り合わないので、取り直す。
			unlockFile(f)
			f.Close()
			continue
		}
		f.Close()
		if !time.Now().Before(deadline) {
			return nil, &LockTimeoutError{Path: target, Wait: lockWait}
		}
		time.Sleep(lockPoll)
	}
}

// lockDir は錠のファイルを置くフォルダーを返す。
func lockDir() string {
	if d := os.Getenv(LockDirEnv); d != "" {
		return d
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "dwloc", "locks")
	}
	return filepath.Join(os.TempDir(), "dwloc-locks")
}

// lockName は、書き出し先の実体 target の錠のファイルの名前を返す。
//
// 名前は、実体の絶対パスの SHA-256 の先頭から作る。同じファイルを指す綴りの違い
// （相対パス、途中のフォルダーのリンク、Windows の 8.3 形式の短い名前、大文字小文字）を
// そろえてから数える。そろえられない（実体もフォルダーも無い）ときは、絶対パスの
// ままで数える。
func lockName(target string) (string, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	} else if dir, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		abs = filepath.Join(dir, filepath.Base(abs))
	}
	key := abs
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		// どちらも既定のファイルシステムが大文字小文字を区別しない。
		key = strings.ToLower(key)
	}
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(lockDir(), hex.EncodeToString(sum[:12])+".lock"), nil
}

// sameFile は、開いているファイル f と、いま name が指すファイルが同じかを返す。
func sameFile(f *os.File, name string) bool {
	held, err := f.Stat()
	if err != nil {
		return false
	}
	now, err := os.Stat(name)
	if err != nil {
		return false
	}
	return os.SameFile(held, now)
}
