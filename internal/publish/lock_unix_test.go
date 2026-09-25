//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package publish

import (
	"io/fs"
	"syscall"
	"testing"
)

// TestCannotWriteOnUnix は、錠のファイルを開けない誤りのうち、一時フォルダーへ移る
// もの（権限と読み取り専用のファイルシステム）とそうでないものを見分けることを見る。
// 読み取り専用のファイルシステムは、試験の中では作れないので、誤りの値で見る。
func TestCannotWriteOnUnix(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"権限", &fs.PathError{Op: "open", Path: "x.lock", Err: syscall.EACCES}, true},
		{"読み取り専用のファイルシステム", &fs.PathError{Op: "open", Path: "x.lock", Err: syscall.EROFS}, true},
		{"フォルダー", &fs.PathError{Op: "open", Path: "x.lock", Err: syscall.EISDIR}, false},
		{"開けるファイルの数の上限", &fs.PathError{Op: "open", Path: "x.lock", Err: syscall.EMFILE}, false},
	}
	for _, tt := range tests {
		if got := cannotWrite(tt.err); got != tt.want {
			t.Errorf("%s: cannotWrite = %v、%v を期待", tt.name, got, tt.want)
		}
	}
}
