package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// absoluteForms は、path を誤りの文に出たときに見分けるための綴りを返す。
// 渡されたままの形と、スラッシュ区切りの形と、リンクと 8.3 形式を解いた形
// （解けるところまで）である。
func absoluteForms(t *testing.T, path string) []string {
	t.Helper()
	forms := []string{path, filepath.ToSlash(path)}
	// 無いパスは解けないので、在る親まで遡って解き、残りを継ぎ足す。
	dir, rest := path, ""
	for {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			full := filepath.Join(resolved, rest)
			forms = append(forms, full, filepath.ToSlash(full))
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
	return forms
}

// checkNoAbsolutePath は、out に path の絶対パスの綴りが1つも出ていないことを見る。
func checkNoAbsolutePath(t *testing.T, label, out, path string) {
	t.Helper()
	for _, form := range absoluteForms(t, path) {
		if strings.Contains(out, form) {
			t.Errorf("%s に絶対パス %q が出ている:\n%s", label, form, out)
			return
		}
	}
}

// TestMissingRootIsReportedInJapanese は、無い --root を絶対パスで渡したとき、
// validate・diff・publish・edit --locale のどれも、絶対パスと OS の英語の文を
// 出さずに、相対パスと日本語の理由で止まることを見る（改善の調査の cli-2 と
// cli-missed-2）。
//
// 以前は4つとも「open C:\Users\<利用者>\...\Translations: The system cannot find
// the path specified.」を出していた。絶対パスには利用者名が入り、記録を添えた
// 不具合の報告からそのまま漏れる。前置きもサブコマンドごとに割れていた。
// publish は、その前に「再生順の行がありません」という的外れな警告も出していた。
//
// edit は2つの道を見る。--locale を付けると CLI の側が、付けないと待ち受けの側
// （internal/web）が同じ誤りを返す。
func TestMissingRootIsReportedInJapanese(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nope")
	tests := []struct {
		name string
		args []string
	}{
		{"validate", []string{"validate", "--root", root}},
		{"diff", []string{"diff", "--no-game", "--root", root}},
		{"publish", []string{"publish", "--no-game", "--root", root}},
		{"edit --locale", []string{"edit", "--no-game", "--no-browser", "--locale", "ja", "--root", root}},
		{"edit", []string{"edit", "--no-game", "--no-browser", "--root", root}},
		{"edit --ui-lang ja", []string{"edit", "--no-game", "--no-browser", "--ui-lang", "ja-JP", "--root", root}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(tt.args...)
			if code != exitError {
				t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
			}
			checkNoAbsolutePath(t, "標準エラー", stderr, root)
			checkContains(t, "標準エラー", stderr, []string{"Translations", "見つかりません"})
			for _, english := range []string{"cannot find", "no such file"} {
				if strings.Contains(stderr, english) {
					t.Errorf("OS の英語の文が出ている:\n%s", stderr)
				}
			}
			if strings.Contains(stderr, "再生順の行がありません") {
				t.Errorf("Translations を読めないのに、再生順の警告を出している:\n%s", stderr)
			}
		})
	}
}

// TestEditInEnglishKeepsTheOSReason は、edit に --ui-lang en を付けたとき、パスは
// 相対にしても、OS の理由は日本語に言い換えないことを見る。edit が黒い窓に出す文は
// --ui-lang の言語に従うので、理由だけを日本語にすると、出力が2言語に割れる。
func TestEditInEnglishKeepsTheOSReason(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nope")
	code, _, stderr := runCLI("edit", "--no-game", "--no-browser", "--ui-lang", "en", "--root", root)
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstderr:\n%s", code, exitError, stderr)
	}
	checkNoAbsolutePath(t, "標準エラー", stderr, root)
	if strings.Contains(stderr, "見つかりません") {
		t.Errorf("英語を選んだのに理由を日本語にしている:\n%s", stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{"dwloc: Translations: "})
}

// TestPublishUnreadableFileIsRelative は、publish が読めないファイルを、ルートからの
// 相対パスと日本語の理由で伝えることを見る。
//
// ファイルの代わりにディレクトリを --path に渡す。どの OS でも読むと失敗し、権限を
// 変えられない環境（Windows、root で走る Docker）でも同じに再現できる。
func TestPublishUnreadableFileIsRelative(t *testing.T) {
	root := publishTree(t, "ja")
	dir := filepath.Join(root, "Translations", "ja")
	code, stdout, stderr := runCLI("publish", "--root", root, "--path", dir)
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
	}
	checkNoAbsolutePath(t, "標準エラー", stderr, root)
	checkContains(t, "標準エラー", stderr, []string{
		"dwloc: Translations/ja を読めないので、訳が失われないことを確かめられません: フォルダーです（ファイルを指定してください）\n",
	})
}

// TestErrorText は、誤りの文の言い換え（[errorText]）を1つずつ見る。
func TestErrorText(t *testing.T) {
	root := t.TempDir()
	inRoot := filepath.Join(root, "Translations", "ja", "strings.csv")
	outside := filepath.Join(t.TempDir(), "game", "strings.csv")
	notExist := &fs.PathError{Op: "open", Path: inRoot, Err: fs.ErrNotExist}
	// Windows では、Go は ENOTDIR も「無い」の一種として扱う（ERROR_PATH_NOT_FOUND が
	// 両方に当たる）。見つからないほうを先に見るので、Windows では「見つかりません」になる。
	notDir := "フォルダーではありません"
	if runtime.GOOS == "windows" {
		notDir = "見つかりません"
	}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "ルートの中のファイルは相対パスと日本語の理由になる",
			err:  notExist,
			want: "Translations/ja/strings.csv: 見つかりません",
		},
		{
			name: "包んだ誤りの前置きは残す",
			err:  fmt.Errorf("再生順のデータを読めません: %w", notExist),
			want: "再生順のデータを読めません: Translations/ja/strings.csv: 見つかりません",
		},
		{
			name: "前置きが同じパスを名指していれば、パスを重ねない",
			err:  fmt.Errorf("Translations/ja/strings.csv が読めない: %w", notExist),
			want: "Translations/ja/strings.csv が読めない: 見つかりません",
		},
		{
			name: "権限",
			err:  &fs.PathError{Op: "open", Path: inRoot, Err: fs.ErrPermission},
			want: "Translations/ja/strings.csv: 権限がありません",
		},
		{
			name: "フォルダーではない",
			err:  &fs.PathError{Op: "open", Path: inRoot, Err: syscall.ENOTDIR},
			want: "Translations/ja/strings.csv: " + notDir,
		},
		{
			name: "フォルダー",
			err:  &fs.PathError{Op: "read", Path: inRoot, Err: syscall.EISDIR},
			want: "Translations/ja/strings.csv: フォルダーです（ファイルを指定してください）",
		},
		{
			name: "知らない理由は OS の文のまま",
			err:  &fs.PathError{Op: "write", Path: inRoot, Err: errors.New("disk is on fire")},
			want: "Translations/ja/strings.csv: disk is on fire",
		},
		{
			name: "ルートの外は渡されたパスのまま",
			err:  &fs.PathError{Op: "open", Path: outside, Err: fs.ErrNotExist},
			want: filepath.ToSlash(outside) + ": 見つかりません",
		},
		{
			name: "rename の誤りは書き出し先のパスで言う",
			err:  &os.LinkError{Op: "rename", Old: inRoot + ".tmp123", New: inRoot, Err: fs.ErrPermission},
			want: "Translations/ja/strings.csv: 権限がありません",
		},
		{
			name: "diff.FileError のパスを相対にする",
			err:  &diff.FileError{Path: inRoot, Err: errors.New(`ヘッダーの列名 "key" が重複している`)},
			want: `Translations/ja/strings.csv: ヘッダーの列名 "key" が重複している`,
		},
		{
			name: "publish.ShapeError の中の PathError も言い換える",
			err:  &publish.ShapeError{Path: inRoot, Err: notExist},
			want: "Translations/ja/strings.csv: 見つかりません",
		},
		{
			name: "パスを持たない誤りはそのまま",
			err:  errors.New("ロケール名が空です"),
			want: "ロケール名が空です",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errorText(root, tt.err); got != tt.want {
				t.Errorf("errorText = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestErrorTextFindsADirectoryReadAsAFile は、ディレクトリをファイルとして読んだ
// ときの OS の誤りを「フォルダーです」に言い換えることを、実際の誤りで見る。
// Windows は EISDIR ではなく「Incorrect function.」を返すので、パスを見て決める。
func TestErrorTextFindsADirectoryReadAsAFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "strings.csv")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := os.ReadFile(dir)
	if err == nil {
		t.Skipf("%s ではディレクトリを読めてしまう", runtime.GOOS)
	}
	if got, want := errorText(root, err), "strings.csv: フォルダーです（ファイルを指定してください）"; got != want {
		t.Errorf("errorText = %q, want %q（元の誤り: %v）", got, want, err)
	}
}
