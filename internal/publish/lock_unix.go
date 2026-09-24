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

// readOnlyFS は、err が読み取り専用で繋いだファイルシステムへの書き込みの誤りかを返す。
func readOnlyFS(err error) bool { return errors.Is(err, syscall.EROFS) }

// unlockFile は f の錠を放す。
func unlockFile(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

// release は錠を放し、横のファイルを消す。
//
// 錠を持ったまま名前を消してから放す。順が逆だと、放してから消すまでのあいだに、
// ほかの dwloc がこのファイルの錠を取り、名前がまだこのファイルを指しているので
// [LockFile] の確かめも通る。そのあと名前が消えると、次の dwloc は新しいファイルを
// 作って錠を取り、2つが同時に錠を持つ。先に消せば、放したあとで錠を取った dwloc は、
// 確かめで名前が別のファイルを指している（か無い）ことに気づいて取り直す。
func release(f *os.File, name string) {
	_ = os.Remove(name)
	unlockFile(f)
	_ = f.Close()
}
