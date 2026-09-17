package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/gamedir"
)

// TestMain は、この package の試験が実機の Steam を見に行かないようにする。
//
// edit は --game を省いてもゲームのフォルダーを探す（[resolveGameForEdit]）。
// 素のままにしておくと、ゲームを入れている PC では試験がそのフォルダーを読み、
// 入れていない PC では読まない。同じ試験が PC ごとに別のことを確かめる形になる。
// 探す穴（[findGame]）をここで塞いでおき、探させたい試験だけが
// [stubFindGame] で入れ替える。
//
// 塞げるのは --game を省いた道だけである。--game auto は [resolveGame] から
// gamedir.Resolve → gamedir.Find へ直に入るので、ここを差し替えても止まらない。
// いま auto を渡す試験は [TestValidateAcceptsButIgnoresGame] だけで、validate は
// ゲームを解決しないため実機を読まない。publish / diff / edit に auto の試験を
// 足すときは、この穴を先に塞ぐこと。塞がないと、ゲームを入れている PC でだけ
// 通る試験になる。
func TestMain(m *testing.M) {
	findGame = func() []gamedir.Plugin { return nil }
	os.Exit(m.Run())
}

// stubFindGame は自動検出の結果を paths に差し替える。試験が終わると元へ戻す。
//
// 実機の Steam を当てにしないのは [TestMain] と同じ理由である。見つかった
// ときの道は、一時ディレクトリに作ったフォルダーで確かめる。
func stubFindGame(t *testing.T, paths ...string) {
	t.Helper()

	was := findGame
	t.Cleanup(func() { findGame = was })
	findGame = func() []gamedir.Plugin {
		found := make([]gamedir.Plugin, 0, len(paths))
		for _, p := range paths {
			found = append(found, gamedir.Plugin{Path: p})
		}
		return found
	}
}

// runCLI は run を呼んで終了コードと出力を返す。
// テストはすべてこの入口を通す。main は os.Exit を呼ぶだけなので、
// 引数の解釈と終了コードの確認はここで足りる。
func runCLI(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// makeTree は一時ディレクトリに files を書き、そのルートを返す。
// キーはルートからの相対パスで、区切りは常にスラッシュで書く。
func makeTree(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("%s の親ディレクトリを作れない: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("%s を書けない: %v", rel, err)
		}
	}
	return root
}

// readFile はルート相対のファイルを読む。無ければテストを落とす。
func readFile(t *testing.T, root, rel string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("%s を読めない: %v", rel, err)
	}
	return string(data)
}

// checkContains は出力に want のすべてが含まれることを確かめる。
func checkContains(t *testing.T, label, got string, want []string) {
	t.Helper()

	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s に %q が含まれない\n--- %s ---\n%s", label, w, label, got)
		}
	}
}

func TestRunArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		// wantCode は期待する終了コード。
		wantCode int
		// wantStdout / wantStderr は含まれていてほしい文字列。
		wantStdout []string
		wantStderr []string
	}{
		{
			// 引数なしは画面を始める。翻訳リポジトリでない場所では、どこへ
			// 置けばよいかを案内して終わる。使い方は help で出す。
			name:       "引数なしで翻訳リポジトリでなければ置き場所を案内する",
			args:       nil,
			wantCode:   exitError,
			wantStderr: []string{"翻訳リポジトリではないようです", "Translations", "dwloc help"},
		},
		{
			name:       "help は使い方を標準出力へ出して成功する",
			args:       []string{"help"},
			wantCode:   exitOK,
			wantStdout: []string{"サブコマンド:", "--root"},
		},
		{
			name:       "--help も使い方を標準出力へ出す",
			args:       []string{"--help"},
			wantCode:   exitOK,
			wantStdout: []string{"サブコマンド:"},
		},
		{
			name:       "-h も使い方を標準出力へ出す",
			args:       []string{"-h"},
			wantCode:   exitOK,
			wantStdout: []string{"サブコマンド:"},
		},
		{
			name:       "知らないサブコマンドはエラーにする",
			args:       []string{"frobnicate"},
			wantCode:   exitError,
			wantStderr: []string{"知らないサブコマンドです", "frobnicate"},
		},
		{
			name:       "知らないフラグはエラーにする",
			args:       []string{"--nope"},
			wantCode:   exitError,
			wantStderr: []string{"使い方:"},
		},
		{
			name:       "version は版を1行で出す",
			args:       []string{"version"},
			wantCode:   exitOK,
			wantStdout: []string{"dwloc dev"},
		},
		{
			name:       "version は共通オプションを打たれても止まらない",
			args:       []string{"version", "--root", "."},
			wantCode:   exitOK,
			wantStdout: []string{"dwloc dev"},
		},
		{
			name:       "version の余分な引数はエラーにする",
			args:       []string{"version", "1.0"},
			wantCode:   exitError,
			wantStderr: []string{"余分な引数です"},
		},
		{
			name:       "publish の余分な引数はエラーにする",
			args:       []string{"publish", "ja"},
			wantCode:   exitError,
			wantStderr: []string{"余分な引数です", "ja"},
		},
		{
			name:       "validate の余分な引数はエラーにする",
			args:       []string{"validate", "ja"},
			wantCode:   exitError,
			wantStderr: []string{"余分な引数です"},
		},
		{
			name:       "validate --help はそのサブコマンドの説明を出す",
			args:       []string{"validate", "--help"},
			wantCode:   exitOK,
			wantStdout: []string{"使い方: dwloc validate", "--root"},
		},
		{
			name:       "publish --help はそのサブコマンドの説明を出す",
			args:       []string{"publish", "--help"},
			wantCode:   exitOK,
			wantStdout: []string{"使い方: dwloc publish", "--locale", "--dry-run"},
		},
		{
			name:       "--locale に空を渡すとエラーにする",
			args:       []string{"publish", "--locale", ""},
			wantCode:   exitError,
			wantStderr: []string{"ロケール名が空です"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(tt.args...)
			if code != tt.wantCode {
				t.Errorf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, tt.wantCode, stdout, stderr)
			}
			checkContains(t, "stdout", stdout, tt.wantStdout)
			checkContains(t, "stderr", stderr, tt.wantStderr)

			// 説明とエラーの出し分け。求められて出す説明は標準出力、
			// 失敗の報告は標準エラーへ出す約束になっている。
			if tt.wantCode == exitError && stdout != "" {
				t.Errorf("失敗したのに標準出力へ書いている:\n%s", stdout)
			}
		})
	}
}

func TestRunVersionUsesLinkerValue(t *testing.T) {
	// -ldflags "-X main.version=..." で差し替えられることの確認。
	// テストからは同じ変数を直接書き換えて代用する。
	saved := version
	t.Cleanup(func() { version = saved })

	version = "1.2.3"
	code, stdout, stderr := runCLI("version")
	if code != exitOK {
		t.Fatalf("終了コード = %d, 期待 %d (stderr: %s)", code, exitOK, stderr)
	}
	if stdout != "dwloc 1.2.3\n" {
		t.Errorf("stdout = %q, 期待 %q", stdout, "dwloc 1.2.3\n")
	}
}

func TestDisplayPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "ルート配下はスラッシュ区切りの相対になる",
			path: filepath.Join(root, "Translations", "ja", "strings.csv"),
			want: "Translations/ja/strings.csv",
		},
		{
			name: "ルート自身は . になる",
			path: root,
			want: ".",
		},
		{
			name: "ルートの外は渡されたパスのまま返す",
			path: filepath.Join(outside, "strings.csv"),
			want: filepath.ToSlash(filepath.Join(outside, "strings.csv")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := displayPath(root, tt.path); got != tt.want {
				t.Errorf("displayPath = %q, 期待 %q", got, tt.want)
			}
		})
	}
}

// TestDefaultStartsTheEditor は、サブコマンドを省くと画面が始まることを見る。
//
// 翻訳者はコマンドプロンプトに慣れていないことが多い。翻訳リポジトリへ dwloc を
// 置いてダブルクリックするだけで開ける、というのがこの経路の狙いである。
// ここが使い方の表示に戻ると、最初の1回で脱落する。
func TestDefaultStartsTheEditor(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "Translations", "ja"), 0o755); err != nil {
		t.Fatal(err)
	}

	var gotRoot string
	called := 0
	orig := startEdit
	startEdit = func(args []string, root, game string, stdout, stderr io.Writer) int {
		called++
		gotRoot = root
		if args != nil {
			t.Errorf("edit に引数を渡している: %q", args)
		}
		if game != "" {
			// --game を打っていないのでゲームのフォルダーは空で来る。埋まって
			// いたら、指定していない人が黙ってゲーム側を読む経路ができている。
			t.Errorf("--game を指定していないのに渡っている: %q", game)
		}
		return exitOK
	}
	t.Cleanup(func() { startEdit = orig })

	var out, errOut bytes.Buffer
	if code := run([]string{"--root", repo}, &out, &errOut); code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, errOut.String())
	}
	if called != 1 {
		t.Fatalf("edit を %d 回呼んだ、1回を期待", called)
	}
	if gotRoot != repo {
		t.Errorf("root = %q, 期待 %q", gotRoot, repo)
	}
	// 使い方を出さなくなったぶん、ほかのこともできると伝える手掛かりを残す。
	if !strings.Contains(out.String(), "dwloc help") {
		t.Errorf("ほかの使い方への案内が出ていない:\n%s", out.String())
	}
}

// TestDefaultWaitsForEnterOnlyWhenBare は、引数を1つも受け取っていないときだけ
// Enter を待つことを見る。
//
// ダブルクリックで開いた窓は、終わると同時に閉じる。案内を読む間も無く消えるので
// 待つ。一方、--root を付けて端末から呼んだ人を待たせる理由は無い。
func TestDefaultWaitsForEnterOnlyWhenBare(t *testing.T) {
	notRepo := t.TempDir()

	origIn := stdin
	t.Cleanup(func() { stdin = origIn })

	t.Run("素で呼ばれたら待つ", func(t *testing.T) {
		stdin = strings.NewReader("\n")
		var out, errOut bytes.Buffer
		// カレントディレクトリを翻訳リポジトリでない場所にして、素の呼び出しを作る。
		t.Chdir(notRepo)
		if code := run(nil, &out, &errOut); code != exitError {
			t.Fatalf("終了コード = %d", code)
		}
		// 「Enter」の3文字で見ると、t.TempDir が作る道（テスト名を含む）に当たる。
		// 実際に出す文そのもので見る。
		if !strings.Contains(errOut.String(), enterPrompt) {
			t.Errorf("Enter を待っていない:\n%s", errOut.String())
		}
	})

	t.Run("引数があれば待たない", func(t *testing.T) {
		stdin = strings.NewReader("")
		var out, errOut bytes.Buffer
		if code := run([]string{"--root", notRepo}, &out, &errOut); code != exitError {
			t.Fatalf("終了コード = %d", code)
		}
		if strings.Contains(errOut.String(), enterPrompt) {
			t.Errorf("引数があるのに Enter を待っている:\n%s", errOut.String())
		}
	})
}

// TestLooksLikeRepo は、翻訳リポジトリらしさの見方を確かめる。
//
// 見るのは Translations ディレクトリの有無だけである。中身まで確かめないのは、
// ロケールの数え方を publish.DiscoverTargets と2か所に持たないためである。
func TestLooksLikeRepo(t *testing.T) {
	empty := t.TempDir()
	if looksLikeRepo(empty) {
		t.Error("Translations が無いのに翻訳リポジトリだと言っている")
	}

	withDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(withDir, "Translations"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !looksLikeRepo(withDir) {
		t.Error("Translations があるのに翻訳リポジトリでないと言っている")
	}

	// 同じ名前のファイルはディレクトリではない。
	withFile := t.TempDir()
	if err := os.WriteFile(filepath.Join(withFile, "Translations"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if looksLikeRepo(withFile) {
		t.Error("Translations がファイルなのに翻訳リポジトリだと言っている")
	}
}
