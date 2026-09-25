//go:build !windows && !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package publish

import "os"

// ここは、dwloc を配らない OS（配るのは Windows・Linux・macOS）でもビルドが通るように
// するための受け皿である。錠を持たないので、書き込みは直列にならない。版の照合（画面）と
// 書く直前の読み直し（publish）だけが守りになる。

// tryLock は錠を持たない OS で、いつも取れたことにする。
func tryLock(*os.File) (bool, error) { return true, nil }

// unlockFile は何もしない。
func unlockFile(*os.File) {}

// ownDir は、錠を持たない OS では確かめない。
func ownDir(os.FileInfo) bool { return true }

// readOnlyFS は、錠を持たない OS では権限の誤り（[cannotWrite]）のほかを見ない。
func readOnlyFS(error) bool { return false }
