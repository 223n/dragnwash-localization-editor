//go:build unix

package publish

import (
	"os"
	"syscall"
)

// linkCount は、ファイルの名前の数（ハードリンクの数）を返す。数えられなければ 1 を返す。
//
// 1 に倒すのは、数えられないときに今までどおり rename で書くためである。
func linkCount(_ string, info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink)
	}
	return 1
}
