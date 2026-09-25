package publish

import (
	"io/fs"
	"syscall"
	"testing"
)

// TestCannotWriteOnWindows は、錠のファイルを開けない誤りのうち、一時フォルダーへ移る
// もの（権限と書き込みを禁じた媒体）とそうでないものを見分けることを見る。どちらの形も
// 試験の中では作れないので、誤りの値で見る。
func TestCannotWriteOnWindows(t *testing.T) {
	const errorSharingViolation syscall.Errno = 32
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"権限", &fs.PathError{Op: "open", Path: "x.lock", Err: syscall.ERROR_ACCESS_DENIED}, true},
		{"書き込みを禁じた媒体", &fs.PathError{Op: "open", Path: "x.lock", Err: errorWriteProtect}, true},
		{"ほかが開いている", &fs.PathError{Op: "open", Path: "x.lock", Err: errorSharingViolation}, false},
	}
	for _, tt := range tests {
		if got := cannotWrite(tt.err); got != tt.want {
			t.Errorf("%s: cannotWrite = %v、%v を期待", tt.name, got, tt.want)
		}
	}
}
