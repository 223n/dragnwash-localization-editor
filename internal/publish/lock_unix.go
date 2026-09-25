//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package publish

import (
	"errors"
	"os"
	"syscall"
)

// tryLock は f に排他の錠を掛けてみる。ほかが持っていれば待たずに false を返す。
//
// flock の錠は開いたファイルごと（open ごと）に分かれるので、同じプロセスの中で
// 別に開いたものどうしも競り合う。
func tryLock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return false, err
}

// unlockFile は f の錠を放す。
func unlockFile(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

// readOnlyFS は、err が読み取り専用のファイルシステムに書こうとした誤り（EROFS）かを
// 返す（[cannotWrite]）。利用者のホームを読み取り専用で渡したコンテナーで、錠の
// フォルダーが既にあるときに当たる。
func readOnlyFS(err error) bool { return errors.Is(err, syscall.EROFS) }

// ownDir は、info のフォルダーが自分のもので、ほかの利用者（グループを含む）が
// 書けないかを返す（[makeOwnDir]）。
func ownDir(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid() && info.Mode().Perm()&0o022 == 0
}
