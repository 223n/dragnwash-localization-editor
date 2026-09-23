package diff

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/reason"
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

	t.Run("再生順が2つ目以降のコミットで入ったきり", func(t *testing.T) {
		// README などを先にコミットし、再生順をあとから足したリポジトリ。
		// 足したコミットに親はあるが、親には再生順が無い。比べる相手が
		// いないのは最初のコミットで入った場合と同じなので、同じ理由を返す。
		// 「git から取り出せません」と書くと、git の故障を疑わせることになる。
		root := gitRepo(t)
		if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("よみもの\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, root, "add", "README.md")
		runGit(t, root, "commit", "-m", "first")
		path := writeOrder(t, root, orderFile(introRow("1", "line:0001", keyB)))
		runGit(t, root, "add", ".")
		runGit(t, root, "commit", "-m", "add order")

		got, err := GitOldOrder(root, path)
		if !errors.Is(err, ErrOnlyOneVersion) {
			t.Errorf("理由が違う: got %v, want %v", err, ErrOnlyOneVersion)
		}
		if got != nil {
			t.Errorf("取り出せないのに中身を返している:\n%s", got)
		}
	})

	t.Run("再生順がリポジトリの外にある", func(t *testing.T) {
		root := gitRepo(t)
		if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("よみもの\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, root, "add", "README.md")
		runGit(t, root, "commit", "-m", "first")
		// 別の一時ディレクトリ。git から見ると「リポジトリの外」になる。
		path := writeOrder(t, t.TempDir(), orderFile(introRow("1", "line:0001", keyB)))

		_, err := GitOldOrder(root, path)
		if !errors.Is(err, ErrNotTracked) {
			t.Errorf("理由が違う: got %v, want %v", err, ErrNotTracked)
		}
	})

	t.Run("root が無い", func(t *testing.T) {
		// git そのものを起動できない。git が PATH に無くても同じ理由になるので、
		// git の有無に関わらず走らせられる。
		root := filepath.Join(t.TempDir(), "missing")
		_, err := GitOldOrder(root, filepath.Join(root, "data", "script_order.csv"))
		if !errors.Is(err, ErrNoGit) {
			t.Errorf("理由が違う: got %v, want %v", err, ErrNoGit)
		}
	})

	t.Run("git が PATH に無い", func(t *testing.T) {
		root := t.TempDir()
		path := writeOrder(t, root, orderFile(introRow("1", "line:0001", keyB)))
		t.Setenv("PATH", "")

		_, err := GitOldOrder(root, path)
		if !errors.Is(err, ErrNoGit) {
			t.Errorf("理由が違う: got %v, want %v", err, ErrNoGit)
		}
	})

	t.Run("再生順をルートからの相対にできない", func(t *testing.T) {
		// 片方だけが絶対パスだと filepath.Rel は失敗する。git を呼ぶ前に止まり、
		// 中身は返さない（失敗は全部「候補を出さない」に倒す）。
		got, err := GitOldOrder("relative-root", filepath.Join(t.TempDir(), "script_order.csv"))
		if err == nil {
			t.Fatalf("エラーにならない: %q", got)
		}
		if got != nil {
			t.Errorf("中身を返している: %q", got)
		}
		for _, sentinel := range []error{ErrNoGit, ErrNoRepository, ErrNotTracked, ErrOnlyOneVersion, ErrGitFailed} {
			if errors.Is(err, sentinel) {
				t.Errorf("git の理由として扱っている: %v", err)
			}
		}
	})
}

// TestGitShowFailure は、git は動いたが中身を取り出せないときの理由を確かめる。
// ここで空の中身を返すと、呼び出し側は「変わった行は無い」と読んでしまう。
func TestGitShowFailure(t *testing.T) {
	root := gitRepo(t)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("よみもの\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "first")

	got, err := gitShow(root, "HEAD:./data/script_order.csv")
	if !errors.Is(err, ErrGitFailed) {
		t.Errorf("理由が違う: got %v, want %v", err, ErrGitFailed)
	}
	if got != nil {
		t.Errorf("中身を返している: %q", got)
	}
}

// TestLoadWithoutGitStillWorks は、git が無い環境でも読み込みが止まらず、
// 引き継ぎ候補だけを理由つきで保留することを確かめる。
//
// 旧再生順を要るのは引き継ぎ候補だけで、残りのカテゴリは git が無くても成り立つ。
// git が無いせいで道具ごと動かなくなるほうが困る（LoadWith のコメント）。
func TestLoadWithoutGitStillWorks(t *testing.T) {
	t.Setenv("PATH", "")
	repo := newRepo(t, map[string]string{
		"data/script_order.csv": orderCSV1,
		"Translations/ja/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
			keyBye + ",L01 Ryan,Ryan_1_intro,2,Ryan,さようなら\n",
	}, false)

	if repo.OldOrder != nil {
		t.Fatal("git が無いのに旧再生順を読めたことになっている")
	}
	if repo.OldOrderReasonID != reason.OldOrderNoGit || repo.OldOrderReason != ErrNoGit.Error() {
		t.Errorf("理由が違う: %q (%q)", repo.OldOrderReasonID, repo.OldOrderReason)
	}
	sum := Compare(repo, nil).Locales[0]
	if sum.CanJudge(CatCarryover) {
		t.Error("旧再生順が無いのに引き継ぎ候補を判定している")
	}
	if got := sum.Counts[CatVanished]; got != 1 {
		t.Errorf("旧再生順に頼らない判定が止まっている: 台本から消えた行 = %d 件, want 1", got)
	}
}

// TestLoadOldOrderReason は、旧再生順を取り出せなかった理由に識別子を当てることを
// 確かめる。
//
// 画面（internal/web）は識別子を鍵にして目録から訳された文面を引く。当てるのは
// このパッケージの番兵だけで、包まれていても見分ける。名前の無い誤りには当てず、
// 文面をそのまま残す。近い識別子を当てずっぽうで付けると、関係のない英文が出る。
func TestLoadOldOrderReason(t *testing.T) {
	files := map[string]string{
		"data/script_order.csv":       orderFile(introRow("1", "line:0001", keyB2)),
		"Translations/ja/strings.csv": publishedHeader + keyB + ",L01 Ryan,Ryan_1_intro,1,Ryan,い\n",
	}

	tests := []struct {
		name   string
		err    error
		wantID string
	}{
		{name: "git が無い", err: ErrNoGit, wantID: reason.OldOrderNoGit},
		{name: "リポジトリでない", err: ErrNoRepository, wantID: reason.OldOrderNoRepository},
		{name: "管理下に無い", err: ErrNotTracked, wantID: reason.OldOrderNotTracked},
		{name: "履歴が1版しかない", err: ErrOnlyOneVersion, wantID: reason.OldOrderOnlyOneVersion},
		{name: "取り出しに失敗した", err: ErrGitFailed, wantID: reason.OldOrderGitFailed},
		{name: "包まれた番兵も見分ける", err: fmt.Errorf("手元の事情: %w", ErrNotTracked), wantID: reason.OldOrderNotTracked},
		{name: "名前の無い誤りには当てない", err: errors.New("自前の理由"), wantID: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRepoWith(t, files, Options{OldOrder: func(root, orderPath string) ([]byte, error) {
				return nil, tt.err
			}})
			if repo.OldOrder != nil {
				t.Fatal("取り出せないのに旧再生順がある")
			}
			if repo.OldOrderReasonID != tt.wantID {
				t.Errorf("識別子が違う: got %q, want %q", repo.OldOrderReasonID, tt.wantID)
			}
			if repo.OldOrderReason != tt.err.Error() {
				t.Errorf("文面が違う: got %q, want %q", repo.OldOrderReason, tt.err.Error())
			}
			// 判定しない理由として、同じ識別子と文面がそのまま画面へ運ばれること。
			why := Compare(repo, nil).Locales[0].JudgeBlockReason(CatCarryover)
			if why.ID != tt.wantID || why.Text != tt.err.Error() {
				t.Errorf("判定しない理由が違う: %q (%q)", why.ID, why.Text)
			}
		})
	}

	t.Run("取り出せても再生順として読めない", func(t *testing.T) {
		repo := newRepoWith(t, files, Options{OldOrder: fixedOldOrder("key,key\n" + keyB + "," + keyB + "\n")})
		if repo.OldOrder != nil {
			t.Fatal("読めない旧再生順を使っている")
		}
		if repo.OldOrderReasonID != reason.OldOrderUnreadable {
			t.Errorf("識別子が違う: %q (%q)", repo.OldOrderReasonID, repo.OldOrderReason)
		}
	})

	t.Run("取り出し口にはルートといまの再生順の場所を渡す", func(t *testing.T) {
		var gotRoot, gotPath string
		repo := newRepoWith(t, files, Options{OldOrder: func(root, orderPath string) ([]byte, error) {
			gotRoot, gotPath = root, orderPath
			return nil, ErrNoGit
		}})
		if gotRoot != repo.Root {
			t.Errorf("root が違う: got %q, want %q", gotRoot, repo.Root)
		}
		if gotPath != repo.OrderPath {
			t.Errorf("orderPath が違う: got %q, want %q", gotPath, repo.OrderPath)
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
