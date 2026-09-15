package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
			name:       "引数なしは使い方を標準エラーへ出してエラーにする",
			args:       nil,
			wantCode:   exitError,
			wantStderr: []string{"使い方:", "validate", "publish", "version"},
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
