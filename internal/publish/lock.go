package publish

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// lockSuffix は、錠を掛けるために書き出し先の横に置くファイルの名前の後ろ。
const lockSuffix = ".dwloc-lock"

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
// 錠は OS のもの（Windows は LockFileEx、Linux や macOS は flock）を、書き出し先の横に
// 置いたファイル（<名前>.dwloc-lock）に掛ける。書き出し先そのものに掛けないのは、
// 書き出しが一時ファイルからの rename でファイルを置き換える（錠を掛けたファイルが
// 消える）ためと、Windows では錠を掛けたファイルをゲームが読めなくなるためである。
// OS の錠は、持っているプロセスが終われば OS が放すので、落ちた dwloc が錠を残す
// ことは無い。
//
// 横のファイルは、放すときに消す。ほかの dwloc が錠を待っているときは、消えずに
// 残ることがある（Windows はほかのプロセスが開いているファイルを消せない）。残っても
// 次の錠に使われるだけで、害は無い。消した名前と錠の相手が分かれないよう、錠を取った
// あとで、名前がいま開いているファイルを指しているかを確かめ、違えば取り直す。
//
// path がシンボリックリンクなら、たどった先の実体の横に置く（[WriteBytes] と同じ
// 実体に書くので、錠も同じ実体で取る）。ハードリンクの別の名前から書く道は直列に
// ならない。
//
// 錠は dwloc 同士の約束で、ゲーム（Mod の書き出し）や表計算ソフトは従わない。
// そちらとの競り合いは、版の照合（画面）と書く直前の読み直し（publish）で見つける。
//
// 書き出し先のフォルダーに書けない（錠のファイルを作れない）ときは、錠を掛けずに
// 何もしない関数を返す。書き出しも同じ理由で失敗するので、競り合う書き込みは起きない。
func LockFile(path string) (unlock func(), err error) {
	target, err := resolveLink(path)
	if err != nil {
		return nil, err
	}
	name := target + lockSuffix
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) || readOnlyFS(err) {
				// 書き出し先のフォルダーに書けない（権限が無い、読み取り専用で繋いだ
				// 外付け、など）。書き出しも同じ理由で失敗する（[WriteBytes] は同じ
				// フォルダーに一時ファイルを作る）ので、競り合う dwloc の書き込みは起きない。
				// 錠を掛けずに通し、書き出しの誤りで知らせる。ここで止めると、publish の
				// 「書き出せません」とその案内が、錠のファイルの誤りに置き換わる。
				return func() {}, nil
			}
			return nil, err
		}
		locked, err := tryLock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if locked {
			if sameFile(f, name) {
				return func() { release(f, name) }, nil
			}
			// 錠を取るあいだに、前の持ち主がこの名前を消した。消えたファイルの錠は
			// 誰とも競り合わないので、取り直す。
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
