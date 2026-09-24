package publish

import (
	"os"
	"syscall"
)

// linkCount は、ファイルの名前の数（ハードリンクの数）を返す。数えられなければ 1 を返す。
//
// Windows の os.Stat はこの数を持たないので、ファイルを開いて尋ねる。開けない
// とき（ほかのプロセスが共有を許さずに開いているなど）は 1 に倒し、今までどおり
// rename で書く。その場合は rename も同じ理由で失敗して報せる。
func linkCount(path string, _ os.FileInfo) uint64 {
	f, err := os.Open(path)
	if err != nil {
		return 1
	}
	defer f.Close()
	var d syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &d); err != nil {
		return 1
	}
	return uint64(d.NumberOfLinks)
}
