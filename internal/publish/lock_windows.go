package publish

import (
	"errors"
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
	// errorWriteProtect は、書き込みを禁じた媒体に書こうとした誤り（ERROR_WRITE_PROTECT）。
	errorWriteProtect syscall.Errno = 19
)

// readOnlyFS は、err が書き込みを禁じた媒体に書こうとした誤りかを返す（[cannotWrite]）。
// フォルダーやファイルの権限で断られたとき（ERROR_ACCESS_DENIED）は fs.ErrPermission に
// 当たるので、ここでは見ない。
func readOnlyFS(err error) bool { return errors.Is(err, errorWriteProtect) }

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

// ownDir は、Windows では確かめない。一時フォルダーはもともと利用者ごとにあり
// （%LocalAppData%\Temp）、ほかの利用者は入れない。
func ownDir(os.FileInfo) bool { return true }

// unlockFile は f の錠を放す。ハンドルを閉じても OS は錠を放すが、放すまでの時間は
// 決まっていないので、先に明示して放す。
func unlockFile(f *os.File) {
	var ol syscall.Overlapped
	_, _, _ = procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
}
