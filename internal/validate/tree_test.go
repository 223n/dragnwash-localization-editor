package validate

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// writeFile はテスト用のファイルを作る。親ディレクトリも作る。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("%s が作れない: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("%s が書けない: %v", path, err)
	}
}

// trackedSet は「このパスだけ git が追跡している」という [Tracked] を作る。
// git を呼ばずに R3 / R5 の分岐を試せるようにするためのもの。
func trackedSet(paths ...string) Tracked {
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[filepath.Clean(p)] = true
	}
	return func(path string) bool { return set[filepath.Clean(path)] }
}

// TestCheckTree は Translations 配下を歩く順と、各分岐の報告を見る。
func TestCheckTree(t *testing.T) {
	root := t.TempDir()
	tr := filepath.Join(root, TranslationsDir)

	// _discovered の直下。サブディレクトリには降りない。
	discoveredTracked := filepath.Join(tr, DiscoveredDir, "a.csv")
	writeFile(t, discoveredTracked, "key,translation\n")
	writeFile(t, filepath.Join(tr, DiscoveredDir, "b.csv"), "key,translation\n")
	buried := filepath.Join(tr, DiscoveredDir, "sub", "c.csv")
	writeFile(t, buried, "key,translation\n")

	// Translations 直下の通常ファイル。ロケールではないので飛ばす。
	writeFile(t, filepath.Join(tr, "ignore.txt"), "メモ\n")
	// '_' で始まるディレクトリも飛ばす。
	writeFile(t, filepath.Join(tr, "_hidden", PublishedFile), "でたらめ\n")

	// 問題のないロケール。
	writeFile(t, filepath.Join(tr, "de", PublishedFile), "key,translation\n"+keyA+",Hallo\n")
	// 作業コピーがコミットされているロケール。公開ファイルにも問題がある。
	jaLocal := filepath.Join(tr, "ja", LocalFile)
	writeFile(t, jaLocal, "key,source_en,translation\n")
	writeFile(t, filepath.Join(tr, "ja", PublishedFile), "key,translation\n"+keyA+",\n")
	// 作業コピーはあるが追跡されていないロケール。問題にしない。
	writeFile(t, filepath.Join(tr, "ko", LocalFile), "key,source_en,translation\n")
	writeFile(t, filepath.Join(tr, "ko", PublishedFile), "key,translation\n"+keyA+",안녕\n")
	// 公開ファイルが無いロケール。
	if err := os.MkdirAll(filepath.Join(tr, "zz"), 0o755); err != nil {
		t.Fatalf("zz が作れない: %v", err)
	}

	got, err := CheckTree(root, trackedSet(discoveredTracked, buried, jaLocal))
	if err != nil {
		t.Fatalf("CheckTree が失敗した: %v", err)
	}
	want := []string{
		"Translations/_discovered/a.csv: " + mustNotBeCommitted,
		"Translations/ja/strings.local.csv: " + mustNotBeCommitted,
		"Translations/ja/strings.csv:2: empty translation",
		"Translations/zz: no strings.csv",
	}
	if diff := problemStrings(got); !slices.Equal(diff, want) {
		t.Errorf("報告が違う\n got = %#v\nwant = %#v", diff, want)
	}
}

// TestCheckTreeEmpty は問題が1件も無い木を見る。
func TestCheckTreeEmpty(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, TranslationsDir, "ja", PublishedFile),
		validHeader+keyA+",UI,,,UI,やあ\n")

	got, err := CheckTree(root, trackedSet())
	if err != nil {
		t.Fatalf("CheckTree が失敗した: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("問題 = %#v, want 0件", problemStrings(got))
	}
	if r := Report(got); r != "translations OK\n" {
		t.Errorf("Report = %q, want %q", r, "translations OK\n")
	}
}

// TestCheckTreeNoDiscovered は _discovered が無くてもエラーにならないことを見る。
// 元実装も存在しなければブロックごと飛ばす（現行リポジトリに _discovered は無い）。
func TestCheckTreeNoDiscovered(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, TranslationsDir, "ja", PublishedFile),
		"key,translation\n"+keyA+",やあ\n")

	if _, err := CheckTree(root, trackedSet()); err != nil {
		t.Fatalf("CheckTree が失敗した: %v", err)
	}
}

// TestCheckTreeMissingTranslations は Translations が無いときにエラーを返すことを見る。
// 元実装は未捕捉の FileNotFoundError で異常終了するが、ここではエラーとして返す。
func TestCheckTreeMissingTranslations(t *testing.T) {
	if _, err := CheckTree(t.TempDir(), trackedSet()); err == nil {
		t.Error("Translations が無いのにエラーにならなかった")
	}
}

// TestCheckTreeUsesGitByDefault は tracked に nil を渡すと git を使うことを見る。
// 追跡されていない一時ディレクトリなので、_discovered のファイルは問題にならない。
func TestCheckTreeUsesGitByDefault(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無いので飛ばす")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, TranslationsDir, DiscoveredDir, "a.csv"), "原文\n")
	writeFile(t, filepath.Join(root, TranslationsDir, "ja", PublishedFile),
		"key,translation\n"+keyA+",やあ\n")

	got, err := CheckTree(root, nil)
	if err != nil {
		t.Fatalf("CheckTree が失敗した: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("問題 = %#v, want 0件", problemStrings(got))
	}
}

// TestGitTracked は git を実際に呼んで判定を確かめる。
func TestGitTracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無いので飛ばす")
	}
	root := t.TempDir()
	tracked := filepath.Join(root, "tracked.txt")
	untracked := filepath.Join(root, "untracked.txt")
	writeFile(t, tracked, "ひとつ\n")
	writeFile(t, untracked, "ふたつ\n")

	for _, args := range [][]string{
		{"init"},
		{"add", "tracked.txt"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v が失敗したので飛ばす: %v (%s)", args, err, out)
		}
	}

	isTracked := GitTracked(root)
	if !isTracked(tracked) {
		t.Error("add したファイルが追跡されていないと判定された")
	}
	if isTracked(untracked) {
		t.Error("add していないファイルが追跡されていると判定された")
	}
	if isTracked(filepath.Join(root, "そんなファイルは無い.txt")) {
		t.Error("存在しないファイルが追跡されていると判定された")
	}
}

// TestGitTrackedOutsideRepository は git リポジトリの外では false になることを見る。
// 元実装も非ゼロ終了を「追跡されていない」として扱う（移植仕様「形式検証 R7」）。
func TestGitTrackedOutsideRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無いので飛ばす")
	}
	root := t.TempDir()
	path := filepath.Join(root, "f.txt")
	writeFile(t, path, "ひとつ\n")

	if GitTracked(root)(path) {
		t.Error("リポジトリの外なのに追跡されていると判定された")
	}
}

// TestDisplayPath は報告に出すパスの整形を見る。
func TestDisplayPath(t *testing.T) {
	root := t.TempDir()
	show := newDisplay(root)

	inside := filepath.Join(root, TranslationsDir, "ja", PublishedFile)
	if got, want := show.of(inside), "Translations/ja/strings.csv"; got != want {
		t.Errorf("of(配下) = %q, want %q", got, want)
	}

	// ルートの外はそのまま返す（元実装の ValueError 経路）。
	outside := filepath.Join(filepath.Dir(root), "よそ", "f.csv")
	if got := show.of(outside); got != outside {
		t.Errorf("of(外) = %q, want %q", got, outside)
	}
}

// TestDisplayPathOtherVolume は、ルートと別のドライブにあるパスをそのまま返すことを見る。
// filepath.Rel はドライブが違うと失敗する。Translations の下にほかのドライブへの
// リンクがあると当たる。元実装の relative_to も ValueError になり、そのまま出す。
func TestDisplayPathOtherVolume(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("ドライブ名があるのは Windows だけ")
	}
	root := t.TempDir()
	other := "Z:"
	if strings.EqualFold(filepath.VolumeName(root), other) {
		other = "Y:"
	}
	path := other + `\よそ\f.csv`
	if got := newDisplay(root).of(path); got != path {
		t.Errorf("of(別ドライブ) = %q, want %q", got, path)
	}
}

// TestGitTrackedFallsBackToExistence は git を起動できないときの判定を見る。
//
// 規則（移植仕様「形式検証 R7」）: 起動できなければ「存在すれば追跡されている」に
// 倒す。手元の作業コピーを誤検出してでも、原文の混入を見逃さない側を選んでいる。
// root が無いと git の有無にかかわらず起動に失敗するので、git の無い環境でも
// 同じ結果になる。
func TestGitTrackedFallsBackToExistence(t *testing.T) {
	root := filepath.Join(t.TempDir(), "無いディレクトリ")
	dir := t.TempDir()
	existing := filepath.Join(dir, LocalFile)
	writeFile(t, existing, "key,source_en,translation\n")

	isTracked := GitTracked(root)
	if !isTracked(existing) {
		t.Error("git を起動できないのに、あるファイルを追跡されていないと判定した")
	}
	if isTracked(filepath.Join(dir, "そんなファイルは無い.csv")) {
		t.Error("git を起動できないのに、無いファイルを追跡されていると判定した")
	}
}

// TestCheckTreeUnreadablePublished は、strings.csv があるのに読めないとき、
// 問題の一覧ではなくエラーを返すことを見る。
//
// 読めなかったファイルを問題0件や "no strings.csv" に落とすと、検査していない
// ファイルがCIを通る。ディレクトリになった strings.csv は「Stat では在るが
// 中身は読めない」という形で、どの OS でも作れる。
func TestCheckTreeUnreadablePublished(t *testing.T) {
	root := t.TempDir()
	published := filepath.Join(root, TranslationsDir, "ja", PublishedFile)
	if err := os.MkdirAll(published, 0o755); err != nil {
		t.Fatalf("%s が作れない: %v", published, err)
	}

	got, err := CheckTree(root, trackedSet())
	if err == nil {
		t.Fatalf("エラーにならなかった（問題 = %#v）", problemStrings(got))
	}
	if got != nil {
		t.Errorf("エラーと一緒に問題を返した: %#v", problemStrings(got))
	}
	// どれが読めなかったかを、報告と同じリポジトリ相対のパスで言う。
	if want := "Translations/ja/strings.csv"; !strings.Contains(err.Error(), want) {
		t.Errorf("エラー = %q, want %q を含む", err, want)
	}
}

// TestCheckTreeUnreadableDiscovered は、_discovered があるのに中を読めないとき、
// エラーを返すことを見る。
//
// ここを黙って飛ばすと、原文つきの作業コピーがコミットされていても検査を
// すり抜ける。原文を公開リポジトリに漏らさないというこの検査の目的に反する。
// 読めないディレクトリは chmod で作るので、Windows と root では作れず飛ばす。
func TestCheckTreeUnreadableDiscovered(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows では chmod でディレクトリを読めなくできない")
	}
	root := t.TempDir()
	discovered := filepath.Join(root, TranslationsDir, DiscoveredDir)
	working := filepath.Join(discovered, "ja.working.csv")
	writeFile(t, working, "key,source_en,translation\n")
	writeFile(t, filepath.Join(root, TranslationsDir, "ja", PublishedFile),
		"key,translation\n"+keyA+",やあ\n")

	if err := os.Chmod(discovered, 0o000); err != nil {
		t.Fatalf("%s の権限を変えられない: %v", discovered, err)
	}
	// 後始末で消せるように戻す。t.Cleanup は後入れ先出しなので、
	// t.TempDir の削除より先に走る。
	t.Cleanup(func() { _ = os.Chmod(discovered, 0o755) })
	if _, err := os.ReadDir(discovered); err == nil {
		t.Skip("読めない状態にできなかったので飛ばす。root で走っていると効かない")
	}

	got, err := CheckTree(root, trackedSet(working))
	if err == nil {
		t.Fatalf("エラーにならなかった（問題 = %#v）", problemStrings(got))
	}
	if got != nil {
		t.Errorf("エラーと一緒に問題を返した: %#v", problemStrings(got))
	}
	if want := "Translations/_discovered"; !strings.Contains(err.Error(), want) {
		t.Errorf("エラー = %q, want %q を含む", err, want)
	}
}
