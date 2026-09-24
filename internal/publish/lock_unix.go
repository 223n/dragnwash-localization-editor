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
