package diff

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo は git リポジトリを1つ作り、そのルートを返す。
// git が無い環境や git が失敗する環境では、呼んだテストごと飛ばす。
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無いので飛ばす")
	}
	root := t.TempDir()
	runGit(t, root, "init")
	return root
}

// gitTestOpts は、テストで動かす git に与える設定を返す。
//
// 名前とメールは、手元の git 設定に依らないために与える。
//
// gc.auto と maintenance.auto は、commit や merge が裏で起こす
// 「git maintenance run --auto」を止めるために与える。この子プロセスは
// gc.autoDetach の既定（true）で本体から分離するため、テストの本体が
// 終わったあとも .git へ書き続けることがある。t.TempDir の後片付け
// （RemoveAll）と競ると、中身を消したあとのディレクトリへ書き戻され、
// 「directory not empty」で落ちる。CI で実際に起きた。
//
// 呼ぶたびに新しいスライスを返す。共有のスライスに append すると、
// 呼び出しごとに同じ配列を書き換えてしまう。
func gitTestOpts() []string {
	return []string{
		"-c", "user.name=dwloc test",
		"-c", "user.email=dwloc@example.invalid",
		"-c", "gc.auto=0",
		"-c", "maintenance.auto=false",
	}
}

// runGit は root で git を走らせる。失敗したらテストを飛ばす。
// 設定は gitTestOpts が与える。
func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	full := append(gitTestOpts(), args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git %v が失敗したので飛ばす: %v (%s)", args, err, out)
	}
}

// writeOrder は root/data/script_order.csv を書く。
func writeOrder(t *testing.T, root, content string) string {
	t.Helper()
	path := filepath.Join(root, "data", "script_order.csv")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestGitOldOrderUsesHeadWhenWorktreeDiffers は、いちばん多い流れを確かめる。
//
// ゲームが更新され、ゲーム内で再生順を書き出し直し、まだコミットしていない状態。
// このとき「前回の公開に使った版」は HEAD にある。
func TestGitOldOrderUsesHeadWhenWorktreeDiffers(t *testing.T) {
	root := gitRepo(t)
	old := orderFile(introRow("1", "line:0001", keyB))
	path := writeOrder(t, root, old)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "before")

	// 更新後。コミットはしない。
	writeOrder(t, root, orderFile(introRow("1", "line:0001", keyB2)))

	got, err := GitOldOrder(root, path)
	if err != nil {
		t.Fatalf("旧再生順を取り出せない: %v", err)
	}
	if !strings.Contains(string(got), keyB) {
		t.Errorf("HEAD の版になっていない:\n%s", got)
	}
	if strings.Contains(string(got), keyB2) {
		t.Errorf("作業ツリーの版を返している:\n%s", got)
	}
}

// TestGitOldOrderUsesPreviousCommitWhenClean は、更新をもうコミットしてしまった
// あとの流れを確かめる。作業ツリーと HEAD が同じなので、1つ前のコミットへ遡る。
func TestGitOldOrderUsesPreviousCommitWhenClean(t *testing.T) {
	root := gitRepo(t)
	path := writeOrder(t, root, orderFile(introRow("1", "line:0001", keyB)))
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "before")

	// 再生順に関係ないコミットを1つ挟む。rev-list はこれを飛ばすはず。
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("よみもの\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "unrelated")

	writeOrder(t, root, orderFile(introRow("1", "line:0001", keyB2)))
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "after")

	got, err := GitOldOrder(root, path)
	if err != nil {
		t.Fatalf("旧再生順を取り出せない: %v", err)
	}
	if !strings.Contains(string(got), keyB) {
		t.Errorf("1つ前の版になっていない:\n%s", got)
	}
	if strings.Contains(string(got), keyB2) {
		t.Errorf("いまの版を返している:\n%s", got)
	}
}

// TestGitOldOrderBlocked は、旧版を取り出せない場面で理由を返すことを確かめる。
//
// ここで黙って空の中身を返すと、呼び出し側は「変わった行は無い」と読んで
// 引き継ぎ候補を 0 件と書く。翻訳者はそれを信じて孤児の訳を捨てる。
func TestGitOldOrderBlocked(t *testing.T) {
	t.Run("git リポジトリでない", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git が無いので飛ばす")
		}
		root := t.TempDir()
		path := writeOrder(t, root, orderFile(introRow("1", "line:0001", keyB)))

		_, err := GitOldOrder(root, path)
		if !errors.Is(err, ErrNoRepository) {
			t.Errorf("理由が違う: got %v, want %v", err, ErrNoRepository)
		}
	})

	t.Run("履歴が1版しかない", func(t *testing.T) {
		root := gitRepo(t)
		path := writeOrder(t, root, orderFile(introRow("1", "line:0001", keyB)))
		runGit(t, root, "add", ".")
		runGit(t, root, "commit", "-m", "first")

		_, err := GitOldOrder(root, path)
		if !errors.Is(err, ErrOnlyOneVersion) {
			t.Errorf("理由が違う: got %v, want %v", err, ErrOnlyOneVersion)
		}
	})

	t.Run("再生順が管理下にない", func(t *testing.T) {
		root := gitRepo(t)
		// 再生順は add せずに、別のファイルだけをコミットする。
		if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("よみもの\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, root, "add", "README.md")
		runGit(t, root, "commit", "-m", "first")
		path := writeOrder(t, root, orderFile(introRow("1", "line:0001", keyB)))

		_, err := GitOldOrder(root, path)
		if !errors.Is(err, ErrNotTracked) {
			t.Errorf("理由が違う: got %v, want %v", err, ErrNotTracked)
		}
	})
}

// TestLoadUsesGitByDefault は、[Options.OldOrder] に nil を渡すと git を使うことを
// 確かめる。既定を差し替えたときに、入口が黙って候補を出さなくなるのを防ぐ。
func TestLoadUsesGitByDefault(t *testing.T) {
	root := gitRepo(t)
	writeOrder(t, root, orderFile(introRow("1", "line:0001", keyB)))
	published := filepath.Join(root, "Translations", "ja", "strings.csv")
	if err := os.MkdirAll(filepath.Dir(published), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(published,
		[]byte(publishedHeader+keyB+",L01 Ryan,Ryan_1_intro,1,Ryan,い\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "before")

	// ゲームが更新され、再生順だけが新しくなった。
	writeOrder(t, root, orderFile(introRow("1", "line:0001", keyB2)))

	repo, err := Load(root, false)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	if repo.OldOrder == nil {
		t.Fatalf("git から旧再生順を読めていない: %s", repo.OldOrderReason)
	}

	rep := Compare(repo, nil)
	if got := carryPairs(rep, "ja"); got[keyB] != keyB2 {
		t.Errorf("引き継ぎ候補が出ていない: %v", got)
	}
}
