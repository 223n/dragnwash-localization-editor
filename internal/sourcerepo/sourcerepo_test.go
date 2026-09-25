package sourcerepo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTB は Fatalf と Skipf の呼ばれ方を記録する [TB]。本物と違い、呼ばれても戻る。
type fakeTB struct {
	fatal, skip string
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Fatalf(format string, args ...any) { f.fatal = fmt.Sprintf(format, args...) }

func (f *fakeTB) Skipf(format string, args ...any) { f.skip = fmt.Sprintf(format, args...) }

// setCandidates は Candidates を差し替える。試験が終わると元へ戻す。
func setCandidates(t *testing.T, c ...string) {
	t.Helper()
	was := Candidates
	t.Cleanup(func() { Candidates = was })
	Candidates = c
}

// makeRepo は data/script_order.csv だけを持つ翻訳リポジトリの形を作り、ルートを返す。
func makeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "script_order.csv"), []byte("key\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestFind は、環境変数を指定したときと、しないときの探し方を見る（改善の決定 30）。
func TestFind(t *testing.T) {
	marker := []string{"data", "script_order.csv"}

	t.Run("指定した場所にあればそこを返す", func(t *testing.T) {
		root := makeRepo(t)
		t.Setenv(Env, root)
		setCandidates(t)
		var f fakeTB
		if got := Find(&f, marker...); got != root || f.fatal != "" || f.skip != "" {
			t.Errorf("Find = %q（fatal %q、skip %q）、%q を期待", got, f.fatal, f.skip, root)
		}
	})

	t.Run("指定したのに無ければ落とす", func(t *testing.T) {
		// 打ち間違えたパス。飛ばすと、実データでの確かめを一度もせずに ok になる。
		missing := filepath.Join(t.TempDir(), "dragnwash-localizaton")
		t.Setenv(Env, missing)
		setCandidates(t, makeRepo(t))
		var f fakeTB
		if got := Find(&f, marker...); got != "" {
			t.Errorf("Find = %q、空を期待", got)
		}
		if f.skip != "" || !strings.Contains(f.fatal, Env) || !strings.Contains(f.fatal, missing) {
			t.Errorf("fatal = %q、skip = %q。環境変数と指定した場所を名指して落とすことを期待", f.fatal, f.skip)
		}
	})

	t.Run("指定しなければ候補を探す", func(t *testing.T) {
		root := makeRepo(t)
		t.Setenv(Env, "")
		setCandidates(t, filepath.Join(t.TempDir(), "nope"), root)
		var f fakeTB
		if got := Find(&f, marker...); got != root || f.fatal != "" || f.skip != "" {
			t.Errorf("Find = %q（fatal %q、skip %q）、%q を期待", got, f.fatal, f.skip, root)
		}
	})

	t.Run("指定せず候補にも無ければ飛ばす", func(t *testing.T) {
		t.Setenv(Env, "")
		setCandidates(t, filepath.Join(t.TempDir(), "nope"))
		var f fakeTB
		if got := Find(&f, marker...); got != "" {
			t.Errorf("Find = %q、空を期待", got)
		}
		if f.fatal != "" || !strings.Contains(f.skip, Env) {
			t.Errorf("fatal = %q、skip = %q。飛ばすことを期待", f.fatal, f.skip)
		}
	})
}
