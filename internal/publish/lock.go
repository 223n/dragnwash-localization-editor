package publish

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// LockDirEnv は、錠のファイルを置くフォルダーを変える環境変数。試験が、利用者の
// キャッシュのフォルダーへ錠のファイルを残さないために使う。ふだんは使わない。
// 値があれば、そのフォルダーだけを使う（作れなければ誤りにし、ほかへ落とさない）。
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
// $XDG_CACHE_HOME か ~/.cache、macOS は ~/Library/Caches）の dwloc/locks に置き、名前は
// 書き出し先の実体の絶対パスから作る（[lockName]）。キャッシュのフォルダーを作れない
// 環境（HOME の無い利用者で動かした docker など）では、一時フォルダーに落とす
// （[lockDir]）。書き出し先の横には置かない。横に置くと、作業コピーの
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
	l, err := lockFor(path)
	if err != nil {
		return nil, err
	}
	return l.acquire()
}

// LockFiles は、paths のどれにも [LockFile] と同じ錠を掛ける。返す関数で、掛けた順と
// 逆に放す。1つでも取れなければ、それまでに取った錠を放して誤りを返す。
//
// publish が、入力と書き出し先の両方に錠を掛けるためにある。画面の保存
// （dwloc edit）は、開いたファイル（作業コピーのあるロケールでは作業コピー、無ければ
// 公開ファイル）に錠を掛けて書くので、publish の入力だけに掛けると、画面が公開
// ファイルを開いているときの保存と、作業コピーから公開ファイルを書く publish が
// 直列にならない。
//
// 同じ錠のファイルになるパス（同じファイルを綴りを変えて2度渡した、入力と書き出し先が
// 同じファイル、など）は1度だけ掛ける。同じ錠を同じプロセスの中で2度取ろうとすると、
// 自分を待って上限まで止まる。
//
// 掛ける順は、パスの綴りではなく錠のファイルの名前の順である。2つの publish が同じ
// ファイルを綴りを変えて渡しても、同じ順に掛けるので、互いに待ち合って止まることは
// 無い。画面の保存は錠を1つしか取らないので、publish と待ち合うことも無い。
func LockFiles(paths []string) (unlock func(), err error) {
	var locks []fileLock
	seen := make(map[string]bool)
	for _, p := range paths {
		l, err := lockFor(p)
		if err != nil {
			return nil, err
		}
		if seen[l.name] {
			continue
		}
		seen[l.name] = true
		locks = append(locks, l)
	}
	slices.SortFunc(locks, func(a, b fileLock) int { return strings.Compare(a.name, b.name) })

	var unlocks []func()
	release := func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}
	for _, l := range locks {
		u, err := l.acquire()
		if err != nil {
			release()
			return nil, err
		}
		unlocks = append(unlocks, u)
	}
	return release, nil
}

// fileLock は、書き出し先1つの錠。
type fileLock struct {
	// target は書き出し先の実体。
	target string
	// name は錠のファイルのパス（[lockName]）。
	name string
}

// lockFor は path の錠を求める。錠のファイルを置くフォルダーが無ければ作る。
func lockFor(path string) (fileLock, error) {
	target, err := resolveLink(path)
	if err != nil {
		return fileLock{}, err
	}
	name, err := lockName(target)
	if err != nil {
		return fileLock{}, err
	}
	return fileLock{target: target, name: name}, nil
}

// acquire は錠を取る。ほかが持っていれば、上限（lockWait）まで待つ。
func (l fileLock) acquire() (unlock func(), err error) {
	name, target := l.name, l.target
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

// lockDir は錠のファイルを置くフォルダーを返す。無ければ作る。
//
// 使うのは、利用者のキャッシュのフォルダーの dwloc/locks で、作れなければ一時フォルダーの
// dwloc-locks-<利用者の番号>（Windows は dwloc-locks）である。キャッシュのフォルダーを
// 作れない環境（HOME の無い利用者で動かした docker、passwd に無い利用者など）でも、
// 画面の保存と publish が毎回止まらないようにする（PR3 の前は、錠が無いので書けた）。
// 同じ環境の dwloc は同じ選び方をするので、同じ書き出し先には同じ錠のファイルを使う。
// 環境の違う dwloc（HOME の違う端末など）どうしは、キャッシュのフォルダーが違うのと
// 同じく直列にならず、版の照合（画面）と書く直前の読み直し（publish）だけが守りになる。
//
// 一時フォルダーはほかの利用者と分け合うことがある（Linux の /tmp）。ほかの利用者が
// 先に作ったフォルダーや、ほかの利用者が書けるフォルダーに錠のファイルを置くと、錠の
// ファイルを消されて直列が崩れうるので、自分のもので、ほかの利用者が書けないときだけ
// 使う（[ownDir]）。
//
// どちらも作れなければ、直し方を添えた誤り（[LockDirError]）を返す。
func lockDir() (string, error) {
	if d := os.Getenv(LockDirEnv); d != "" {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return "", err
		}
		return d, nil
	}
	var tried []error
	if cache, err := os.UserCacheDir(); err != nil {
		tried = append(tried, err)
	} else {
		d := filepath.Join(cache, "dwloc", "locks")
		if err := os.MkdirAll(d, 0o700); err == nil {
			return d, nil
		} else {
			tried = append(tried, err)
		}
	}
	d := tempLockDir()
	if err := makeOwnDir(d); err != nil {
		return "", &LockDirError{Tried: append(tried, err)}
	}
	return d, nil
}

// tempLockDir は、キャッシュのフォルダーを作れないときに錠のファイルを置く一時
// フォルダーを返す。利用者の番号がある OS（Windows のほか）では、名前に番号を入れて
// 利用者ごとに分ける。Windows の一時フォルダーは、もともと利用者ごとにある。
func tempLockDir() string {
	name := "dwloc-locks"
	if uid := os.Getuid(); uid >= 0 {
		name += "-" + strconv.Itoa(uid)
	}
	return filepath.Join(os.TempDir(), name)
}

// makeOwnDir は d を作り、自分のもので、ほかの利用者が書けないフォルダーであることを
// 確かめる。シンボリックリンクは、たどった先がどこでも使わない。
func makeOwnDir(d string) error {
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(d)
	if err != nil {
		return err
	}
	if !info.IsDir() || !ownDir(info) {
		return fmt.Errorf("%s は自分だけが書けるフォルダーではないので、錠のファイルを置きません", d)
	}
	return nil
}

// LockDirError は、錠のファイルを置くフォルダーをどこにも作れなかったこと。
type LockDirError struct {
	// Tried は、試した置き場ごとの誤り（キャッシュのフォルダー、一時フォルダーの順）。
	Tried []error
}

func (e *LockDirError) Error() string {
	msgs := make([]string, len(e.Tried))
	for i, err := range e.Tried {
		msgs[i] = err.Error()
	}
	return "書き込みの錠のファイルを置くフォルダーを作れません（" + strings.Join(msgs, "、") + "）。" +
		"利用者のキャッシュのフォルダー（Linux は XDG_CACHE_HOME か HOME、macOS は HOME、Windows は LocalAppData）か、" +
		"一時フォルダー（TMPDIR、Windows は TMP）を、書けるフォルダーに向けてください"
}

// lockName は、書き出し先の実体 target の錠のファイルの名前を返す。
//
// 名前は、実体の絶対パスの SHA-256 の先頭から作る。同じファイルを指す綴りの違い
// （相対パス、途中のフォルダーのリンク、Windows の 8.3 形式の短い名前、大文字小文字）を
// そろえてから数える。そろえられない（実体もフォルダーも無い）ときは、絶対パスの
// ままで数える。置くフォルダー（[lockDir]）が無ければ作る。
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
	dir, err := lockDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, hex.EncodeToString(sum[:12])+".lock"), nil
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
