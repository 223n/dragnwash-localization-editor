package publish

import (
	"os"
	"syscall"
	"unsafe"
)

// LockFileEx と UnlockFileEx は標準の syscall に無いので、kernel32.dll から引く。
// kernel32.dll は KnownDLLs に入っているので、置き換えられた DLL を読む恐れは無い。
var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	// lockfileFailImmediately は、ほかが持っていれば待たずに失敗させる印。
	lockfileFailImmediately = 0x00000001
	// lockfileExclusiveLock は排他の錠の印。
	lockfileExclusiveLock = 0x00000002
	// errorLockViolation は、ほかが錠を持っているときの誤り（ERROR_LOCK_VIOLATION）。
	errorLockViolation syscall.Errno = 33
)

// tryLock は f の先頭1バイトに排他の錠を掛けてみる。ほかが持っていれば待たずに
// false を返す。
//
// LockFileEx の錠はハンドルごとに分かれるので、同じプロセスの中で別に開いたハンドル
// どうしも競り合う。
func tryLock(f *os.File) (bool, error) {
	var ol syscall.Overlapped
	r, _, err := procLockFileEx.Call(f.Fd(), lockfileExclusiveLock|lockfileFailImmediately,
		0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r != 0 {
		return true, nil
	}
	if err == errorLockViolation {
		return false, nil
	}
	return false, err
}

// readOnlyFS は、Windows では使わない（読み取り専用の媒体への書き込みは権限の誤りに
// なる）。
func readOnlyFS(error) bool { return false }

// unlockFile は f の錠を放す。ハンドルを閉じても OS は錠を放すが、放すまでの時間は
// 決まっていないので、先に明示して放す。
func unlockFile(f *os.File) {
	var ol syscall.Overlapped
	_, _, _ = procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
}

// release は錠を放し、横のファイルを消す。
//
// Windows は、ほかのプロセスが開いているファイルを消せない（Go の os.OpenFile は共有に
// 削除を含めない）。そのため、放して閉じてから消す。錠を待っている dwloc がそのファイルを
// 開いていれば消えずに残り、その dwloc が同じファイルで錠を取る。誰も開いていなければ
// 消え、次の dwloc は新しく作る。どちらでも、2つが同時に錠を持つことは無い。
func release(f *os.File, name string) {
	unlockFile(f)
	_ = f.Close()
	_ = os.Remove(name)
}
