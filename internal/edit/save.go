package edit

import (
	"bytes"
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
// 最後は同じ誤りが返る。
var saveRetryDelays = []time.Duration{10 * time.Millisecond, 30 * time.Millisecond, 100 * time.Millisecond}

// Save は現在の中身を [File.Path] へ書く。
//
// 書く前に保存先を読み直して版を照合し、読み込んだときと違えば1バイトも書かずに
// [ConflictError] を返す（errors.Is(err, [ErrConflict]) で判定できる）。自動保存が
// 前提のモデルなので、黙って上書きすると別の窓や publish の再生成が書いた内容を消す。
//
// 中身が保存先と完全に一致しているときは書かない。ファイルの更新時刻も変えない。
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
	for attempt := 0; ; attempt++ {
		// 版の照合は試行のたびに行う。1回目の前だけで済ませてはいけない。
		// 再試行が起きるのは「ほかのプロセスがこのファイルを掴んでいる」ときで、
		// それは別の窓や publish の再生成が書き込む場面そのものである。
		// 照合を冒頭の1回にすると、待っているあいだ（合計140ms）に入った
		// 書き込みを黙って消し、しかも Save は nil を返してしまう。
		current, err := os.ReadFile(f.path)
		if err != nil {
			// 読めないなら照合できない。消えた・名前が変わった・権限が変わった、
			// いずれにせよ書いてよい根拠が無いので競合として扱う。
			return &ConflictError{Path: f.path, Want: f.version, Err: err}
		}
		if got := hashBytes(current); got != f.version {
			return &ConflictError{Path: f.path, Want: f.version, Got: got}
		}
		if bytes.Equal(current, out) {
			// 中身が同じなら書かない。ファイルを見張っているゲームを
			// 無駄に起こさないため。
			f.dirty = false
			return nil
		}

		writeErr := publish.WriteBytes(f.path, out)
		if writeErr == nil {
			break
		}
		if attempt >= len(saveRetryDelays) {
			return writeErr
		}
		time.Sleep(saveRetryDelays[attempt])
	}

	f.version = hashBytes(out)
	f.dirty = false
	return nil
}
