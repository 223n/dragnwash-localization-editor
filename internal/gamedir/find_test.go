package gamedir

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// tempDir は一時ディレクトリを、実体と同じ綴りで返す。
//
// [testing.T.TempDir] は TMP / TEMP をそのまま使う。環境変数の綴りが実体と
// 違うこと（大文字小文字が違う、8.3の短い名前、ジャンクション）があり、
// [Resolve] は [canonical] を通して実体の綴りを返すので、素の t.TempDir() と
// 突き合わせると落ちる。実測で、TMP と TEMP を
// C:\USERS\223N\AppData\Local\TEMP にして走らせると TestResolve の2件が
// 「パスが違う」で落ちた。
//
// 落ちているのは試験の組み立てであって道具ではない。綴りをそろえる場所を
// ここ1つにして、パスを突き合わせる試験が t.TempDir() を直に使わないようにする。
func tempDir(t *testing.T) string {
	t.Helper()

	return canonical(t.TempDir())
}

// makePlugin は dir をプラグインのフォルダーらしくする（目印を作る）。
func makePlugin(t *testing.T, dir string) string {
	t.Helper()

	mark := filepath.Join(dir, publish.TranslationsDir, publish.DiscoveredDir)
	if err := os.MkdirAll(mark, 0o755); err != nil {
		t.Fatalf("%s を作れない: %v", mark, err)
	}
	return dir
}

// makeLibrary は Steam のライブラリらしい形を作り、そのライブラリのパスを返す。
// games はライブラリに入れるゲームのフォルダー名。
func makeLibrary(t *testing.T, root string, games ...string) string {
	t.Helper()

	for _, game := range games {
		dir := filepath.Join(root, "steamapps", "common", game, "BepInEx", "plugins")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("%s を作れない: %v", dir, err)
		}
	}
	return root
}

// pluginIn はライブラリの中のプラグインフォルダーのパスを組み立てる。
func pluginIn(library, game, plugin string) string {
	return filepath.Join(library, "steamapps", "common", game, "BepInEx", "plugins", plugin)
}

func TestInspect(t *testing.T) {
	t.Run("フォルダーそのものが目印を持てば当たる", func(t *testing.T) {
		dir := makePlugin(t, t.TempDir())

		got := Inspect(dir)
		if len(got) != 1 || got[0].Path != dir {
			t.Fatalf("見つからない: %+v", got)
		}
	})

	t.Run("ゲームのフォルダーを渡すと中のプラグインを見つける", func(t *testing.T) {
		game := t.TempDir()
		want := makePlugin(t, filepath.Join(game, "BepInEx", "plugins", "DragNWashLocalization"))

		got := Inspect(game)
		if len(got) != 1 || got[0].Path != want {
			t.Fatalf("見つからない: %+v", got)
		}
	})

	t.Run("プラグインのフォルダー名は決め打ちにしない", func(t *testing.T) {
		game := t.TempDir()
		// 利用者が名前を変えている場合。名前で当てにいっていないことを見る。
		want := makePlugin(t, filepath.Join(game, "BepInEx", "plugins", "わたしの翻訳"))

		got := Inspect(game)
		if len(got) != 1 || got[0].Path != want {
			t.Fatalf("見つからない: %+v", got)
		}
	})

	t.Run("plugins の直下に置かれていても見つける", func(t *testing.T) {
		game := t.TempDir()
		want := makePlugin(t, filepath.Join(game, "BepInEx", "plugins"))

		got := Inspect(game)
		if len(got) != 1 || got[0].Path != want {
			t.Fatalf("見つからない: %+v", got)
		}
	})

	t.Run("目印が無ければ何も返さない", func(t *testing.T) {
		game := t.TempDir()
		if err := os.MkdirAll(filepath.Join(game, "BepInEx", "plugins", "other"), 0o755); err != nil {
			t.Fatal(err)
		}
		// Translations はあるが _discovered が無い、という半端な状態も外す。
		if err := os.MkdirAll(filepath.Join(game, publish.TranslationsDir), 0o755); err != nil {
			t.Fatal(err)
		}

		if got := Inspect(game); len(got) != 0 {
			t.Fatalf("当たってはいけない: %+v", got)
		}
	})

	t.Run("2段より深いところは見ない", func(t *testing.T) {
		game := t.TempDir()
		makePlugin(t, filepath.Join(game, "BepInEx", "plugins", "a", "b"))

		if got := Inspect(game); len(got) != 0 {
			t.Fatalf("深すぎる候補を拾っている: %+v", got)
		}
	})

	t.Run("空の場所を渡されても作業ディレクトリを目印と取り違えない", func(t *testing.T) {
		// dwloc を走らせる場所は翻訳リポジトリで、そこにも Translations がある。
		// 作業コピー（_discovered）を置いたままにしていると、空の場所を相対パスの
		// 起点として読んだときに、リポジトリがプラグインのフォルダーに見える。
		repo := makePlugin(t, t.TempDir())
		t.Chdir(repo)

		if got := Inspect(""); len(got) != 0 {
			t.Fatalf("作業ディレクトリを候補にしている: %+v", got)
		}
	})
}

func TestFindIn(t *testing.T) {
	t.Run("ライブラリを辿って見つける", func(t *testing.T) {
		lib := makeLibrary(t, t.TempDir(), "Drag'n Wash", "Other Game")
		want := makePlugin(t, pluginIn(lib, "Drag'n Wash", "DragNWashLocalization"))

		got := findIn([]string{lib})
		if len(got) != 1 {
			t.Fatalf("候補の数が違う: %+v", got)
		}
		if got[0].Path != want {
			t.Errorf("パスが違う: got %q, want %q", got[0].Path, want)
		}
		// 辿ってきたライブラリは別に持たない。候補のパスがライブラリから
		// 始まっているので、同じことを2か所で言わない（Plugin のコメント）。
		if !strings.HasPrefix(got[0].Path, lib) {
			t.Errorf("候補がライブラリの下でない: got %q, lib %q", got[0].Path, lib)
		}
	})

	t.Run("ゲームのフォルダー名も決め打ちにしない", func(t *testing.T) {
		lib := makeLibrary(t, t.TempDir())
		want := makePlugin(t, pluginIn(lib, "dw-backup-2026", "loc"))

		got := findIn([]string{lib})
		if len(got) != 1 || got[0].Path != want {
			t.Fatalf("見つからない: %+v", got)
		}
	})

	t.Run("ライブラリが複数あれば全部辿る", func(t *testing.T) {
		libA := makeLibrary(t, filepath.Join(t.TempDir(), "A"))
		libB := makeLibrary(t, filepath.Join(t.TempDir(), "B"))
		makePlugin(t, pluginIn(libA, "Game", "loc"))
		makePlugin(t, pluginIn(libB, "Game", "loc"))

		if got := findIn([]string{libA, libB}); len(got) != 2 {
			t.Fatalf("候補の数が違う: %+v", got)
		}
	})

	t.Run("同じライブラリを2回渡しても候補は増えない", func(t *testing.T) {
		lib := makeLibrary(t, t.TempDir())
		makePlugin(t, pluginIn(lib, "Game", "loc"))

		if got := findIn([]string{lib, lib}); len(got) != 1 {
			t.Fatalf("重複を畳めていない: %+v", got)
		}
	})

	t.Run("読めない場所を渡しても落ちない", func(t *testing.T) {
		if got := findIn([]string{filepath.Join(t.TempDir(), "nope")}); len(got) != 0 {
			t.Fatalf("当たってはいけない: %+v", got)
		}
	})
}

func TestResolve(t *testing.T) {
	t.Run("プラグインのフォルダーを指定できる", func(t *testing.T) {
		dir := makePlugin(t, tempDir(t))

		got, err := Resolve(dir)
		if err != nil {
			t.Fatalf("Resolve がエラーを返した: %v", err)
		}
		if got.Path != dir {
			t.Errorf("パスが違う: got %q, want %q", got.Path, dir)
		}
	})

	t.Run("ゲームのフォルダーを指定してもよい", func(t *testing.T) {
		game := tempDir(t)
		want := makePlugin(t, filepath.Join(game, "BepInEx", "plugins", "loc"))

		got, err := Resolve(game)
		if err != nil {
			t.Fatalf("Resolve がエラーを返した: %v", err)
		}
		if got.Path != want {
			t.Errorf("パスが違う: got %q, want %q", got.Path, want)
		}
	})

	t.Run("目印が無ければ NotPluginError", func(t *testing.T) {
		dir := t.TempDir()

		_, err := Resolve(dir)
		var notPlugin *NotPluginError
		if !errors.As(err, &notPlugin) {
			t.Fatalf("NotPluginError を期待した: %v", err)
		}
		// 文面にパスが入る。翻訳者が場所を取り違えたときの手掛かりになる。
		if !strings.Contains(notPlugin.Error(), publish.DiscoveredDir) {
			t.Errorf("文面に目印の名前が無い: %s", notPlugin.Error())
		}
	})

	t.Run("候補が複数なら選ばずに AmbiguousError", func(t *testing.T) {
		game := tempDir(t)
		makePlugin(t, filepath.Join(game, "BepInEx", "plugins", "a"))
		makePlugin(t, filepath.Join(game, "BepInEx", "plugins", "b"))

		_, err := Resolve(game)
		var ambiguous *AmbiguousError
		if !errors.As(err, &ambiguous) {
			t.Fatalf("AmbiguousError を期待した: %v", err)
		}
		if len(ambiguous.Candidates) != 2 {
			t.Fatalf("候補の数が違う: %+v", ambiguous.Candidates)
		}
		// 並びは実行のたびに変わらない。人に選ばせる一覧なので、
		// 同じ状態から同じ順で出ないと「さっきと違う」と読まれる。
		if ambiguous.Candidates[0].Path >= ambiguous.Candidates[1].Path {
			t.Errorf("並びがパス順でない: %+v", ambiguous.Candidates)
		}
	})
}

func TestAmbiguousErrorListsCandidates(t *testing.T) {
	// 文面に数と候補の全部を出す。どれを --game に書くかは、この並びから
	// 選んでもらうしかない。区切りは / にそろえ、OS で見た目を変えない。
	err := &AmbiguousError{Candidates: []Plugin{
		{Path: filepath.FromSlash("/lib/a/loc")},
		{Path: filepath.FromSlash("/lib/b/loc")},
	}}

	want := "ゲームのプラグインフォルダーが2個見つかった: /lib/a/loc, /lib/b/loc"
	if got := err.Error(); got != want {
		t.Errorf("文面が違う\n got %q\nwant %q", got, want)
	}
}

func TestPickReturnsNotFound(t *testing.T) {
	// auto の空振りは ErrNotFound。呼び出し側はこれで「まだ書き出していない」
	// 案内へ分ける。
	_, err := pick(nil, ErrNotFound)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ErrNotFound を期待した: %v", err)
	}
}

func TestAutoValueIsNotTreatedAsAPath(t *testing.T) {
	// "auto" という名前のフォルダーを作っても、自動検出として扱う。
	// この語を予約していることを、試験でも書き留めておく。
	dir := makePlugin(t, filepath.Join(tempDir(t), AutoValue))
	t.Chdir(filepath.Dir(dir))

	got, err := Resolve(AutoValue)
	if err == nil && got.Path == dir {
		t.Fatal("auto を相対パスとして解釈している")
	}
}

func TestCanonicalKeepsMissingPaths(t *testing.T) {
	// まだ無い場所を指されても落ちない。EvalSymlinks が失敗する経路。
	missing := filepath.Join(tempDir(t), "nope")

	if got := canonical(missing); got != filepath.Clean(missing) {
		t.Errorf("canonical が変えた: got %q, want %q", got, filepath.Clean(missing))
	}
}

func TestCanonicalKeepsValueWithoutWorkingDir(t *testing.T) {
	// 作業ディレクトリが消えていると Abs が失敗する。そのときは指定された値を
	// そのまま使う（canonical のコメント）。落ちたり空にしたりすると、
	// NotPluginError の文面に翻訳者が打った場所が出なくなる。
	if runtime.GOOS != "linux" {
		t.Skip("使っている作業ディレクトリを消せるのは Linux だけで確かめている")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Remove(dir); err != nil {
		t.Fatalf("作業ディレクトリを消せない: %v", err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Skip("作業ディレクトリを消しても Getwd が失敗しない")
	}

	// Linux だけで走るので区切りは / で書く。filepath.Join を通すと先に
	// 畳まれてしまい、canonical が Clean をかけるかどうかを見られない。
	if got, want := canonical("games/../DragNWash"), "DragNWash"; got != want {
		t.Errorf("canonical = %q, want %q", got, want)
	}
}

func TestDedupeStrings(t *testing.T) {
	t.Run("並びを変えない", func(t *testing.T) {
		// Steam を入れた場所が先頭で、そこから libraryfolders.vdf を辿る。
		// dedupe と違い、パス順に並べ替えてはいけない。
		base := tempDir(t)
		b := filepath.Join(base, "b")
		a := filepath.Join(base, "a")

		got := dedupeStrings([]string{b, a, b})
		if strings.Join(got, "|") != strings.Join([]string{b, a}, "|") {
			t.Errorf("並びが違う\ngot  %q\nwant %q", got, []string{b, a})
		}
	})

	t.Run("空は落とす", func(t *testing.T) {
		// 空を canonical に通すと作業ディレクトリになり、翻訳リポジトリを
		// Steam のライブラリとして辿ってしまう。
		dir := tempDir(t)

		got := dedupeStrings([]string{"", dir, ""})
		if len(got) != 1 || got[0] != dir {
			t.Errorf("空が残っている: %q", got)
		}
	})

	t.Run("相対パスは絶対パスにする", func(t *testing.T) {
		base := tempDir(t)
		t.Chdir(base)

		got := dedupeStrings([]string{"Steam"})
		if want := filepath.Join(base, "Steam"); len(got) != 1 || got[0] != want {
			t.Errorf("絶対パスになっていない: got %q, want %q", got, want)
		}
	})

	t.Run("大文字小文字だけ違う綴りは実体の綴りで1つに畳む", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("大文字小文字を区別しないファイルシステムで、実体の綴りを返すのは Windows だけ")
		}
		// レジストリの値は小文字、libraryfolders.vdf は大文字混じりで来る。
		// 畳まないと同じライブラリを2回走査し、候補が2つに見える。
		dir := filepath.Join(tempDir(t), "Steam")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}

		got := dedupeStrings([]string{strings.ToLower(dir), dir})
		if len(got) != 1 || got[0] != dir {
			t.Errorf("畳めていないか、綴りが実体と違う: got %q, want %q", got, dir)
		}
	})
}
