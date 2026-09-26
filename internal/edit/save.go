package edit

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// saveRetryDelays は保存が失敗したときに待つ間隔。1回目の失敗で先頭から使う。
//
// Windows ではゲーム（ゲーム内Modのホットリロード）がファイルを開いている最中に
// os.Rename が共有違反で失敗しうる。読み終わるのを待てば通ることが多いので、
// 短い間隔で数回だけ試す。合計でも 0.2 秒に満たないので、自動保存の呼び出しを
// 目に見えて待たせることはない。
//
// 原因を選り分けずに再試行する。共有違反かどうかを判別するには
// syscall.ERROR_SHARING_VIOLATION を見ることになり、Windows 以外で
// ビルドが割れる。権限不足のような直らない誤りでも数百ミリ秒無駄になるだけで、
// 最後は同じ誤りが返る。錠を取れなかったとき（[publish.LockFile] の誤り）も同じに
// 扱う。
var saveRetryDelays = []time.Duration{10 * time.Millisecond, 30 * time.Millisecond, 100 * time.Millisecond}

// Save は現在の中身を [File.Path] へ書く。
//
// 書く前に保存先を読み直して版を照合し、読み込んだときと違えば1バイトも書かずに
// [ConflictError] を返す（errors.Is(err, [ErrConflict]) で判定できる）。自動保存が
// 前提のモデルなので、黙って上書きすると別の窓や publish の再生成が書いた内容を消す。
//
// 中身が保存先と完全に一致しているときは書かない。ファイルの更新時刻も変えない。
//
// 書き換えたレコードがあれば、書く前に、書こうとしているバイト列をファイル全体として
// 読み直して確かめる（書く前の事後確認の後半。[File.verify]）。外れたら1バイトも
// 書かずに [RecheckError] を返す（errors.Is(err, [ErrRecheck]) で判定できる）。
//
// 版の照合から rename までは、OS の錠（[publish.LockFile]）で囲む（改善の決定 3）。
// 錠が無いと、同じファイルを開いたもう1つの dwloc edit（や publish）の書き込みが、
// こちらの照合のあとで rename の前に入り、こちらがそれを黙って上書きする。どちらの
// 画面にも「保存済み」と出たまま、片方の訳が消える（改善の調査 security-2 で再現した）。
// 2つ目の dwloc edit を起動させない形にはせず、書き込みだけを直列にする。
//
// 保存に成功すると版が更新され、[File.Dirty] は false に戻る。続けてもう一度
// 呼んでよい。
func (f *File) Save() error {
	if f.path == "" {
		return ErrNoPath
	}
	if f.readOnly {
		return fmt.Errorf("%w: %s", ErrReadOnly, f.readOnlyReason)
	}

	out := f.Bytes()
	if len(f.touched) > 0 {
		if err := f.verify(out); err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		// 版の照合は試行のたびに行う。1回目の前だけで済ませてはいけない。
		// 再試行が起きるのは「ほかのプロセスがこのファイルを掴んでいる」ときで、
		// それは別の窓や publish の再生成が書き込む場面そのものである。
		// 照合を冒頭の1回にすると、待っているあいだ（合計140ms）に入った
		// 書き込みを黙って消し、しかも Save は nil を返してしまう。
		err := f.writeLocked(out)
		var conflict *ConflictError
		if err == nil || errors.As(err, &conflict) {
			return err
		}
		if attempt >= len(saveRetryDelays) {
			return err
		}
		time.Sleep(saveRetryDelays[attempt])
	}
}

// writeLocked は、錠を取ってから版を照合し、out を書く。書いたら（または中身が同じで
// 書かなくてよかったら）モデルを保存したものとして覚える。
//
// 錠を取れない・書けない誤りはそのまま返し、[File.Save] が少し待って試し直す。
func (f *File) writeLocked(out []byte) error {
	unlock, err := publish.LockFile(f.path)
	if err != nil {
		return err
	}
	defer unlock()

	current, err := os.ReadFile(f.path)
	if err != nil {
		// 読めないなら照合できない。消えた・名前が変わった・権限が変わった、
		// いずれにせよ書いてよい根拠が無いので競合として扱う。
		return &ConflictError{Path: f.path, Want: f.version, Err: err}
	}
	if got := hashBytes(current); got != f.version {
		return &ConflictError{Path: f.path, Want: f.version, Got: got}
	}
	if !bytes.Equal(current, out) {
		// 中身が同じなら書かない。ファイルを見張っているゲームを
		// 無駄に起こさないため。
		if err := publish.WriteBytes(f.path, out); err != nil {
			return err
		}
		f.version = hashBytes(out)
	}
	f.dirty = false
	f.saved()
	return nil
}

// saved は、いまの中身を保存したものとして覚える。書き換えた印を下ろし、どの行も
// いまのバイト列を「読み込んだときのバイト列」にする（[File.verify] が比べる相手）。
// 物理行の数も、いまのものを覚える（訳の改行で変わる）。
func (f *File) saved() {
	for i := range f.touched {
		line := &f.lines[i]
		line.orig, line.origSpan = line.Text, line.EndNumber-line.Number
	}
	f.touched = nil
}
