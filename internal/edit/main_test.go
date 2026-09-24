package edit

import (
	"os"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// TestMain は、保存の錠のファイルを試験用の一時フォルダーに置かせる
// （publish.LockDirEnv）。利用者のキャッシュのフォルダーへ、試験が作った一時パスの
// 錠のファイルを残さない。
func TestMain(m *testing.M) {
	os.Exit(runWithLockDir(m))
}

func runWithLockDir(m *testing.M) int {
	if os.Getenv(publish.LockDirEnv) != "" {
		return m.Run()
	}
	dir, err := os.MkdirTemp("", "dwloc-locks-test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	os.Setenv(publish.LockDirEnv, dir)
	defer os.Unsetenv(publish.LockDirEnv)
	return m.Run()
}
