package publish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustMkdir は中身の無いディレクトリを作る。
func mustMkdir(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("%s を作れない: %v", dir, err)
	}
}

// gameTree はゲーム側のプラグインフォルダーらしい形を作り、そのパスを返す。
// files のキーはプラグインフォルダーからの相対で、区切りはスラッシュ。
func gameTree(t *testing.T, files map[string]string) string {
	t.Helper()

	game := filepath.Join(t.TempDir(), "plugins", "DragNWashLocalization")
	for rel, content := range files {
		writeFile(t, filepath.Join(game, filepath.FromSlash(rel)), content)
	}
	return game
}

func TestDiscoverEditTargets(t *testing.T) {
	t.Run("リポジトリに無ければゲーム側の作業コピーを入力にする", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, TranslationsDir, "ja", StringsFile), "key\n")
		game := gameTree(t, map[string]string{
			"Translations/_discovered/ja.working.csv": "source_en\n",
		})

		targets, err := DiscoverEditTargets(root, game)
		if err != nil {
			t.Fatalf("DiscoverEditTargets がエラーを返した: %v", err)
		}
		if len(targets) != 1 {
			t.Fatalf("対象が1件でない: %+v", targets)
		}
		want := filepath.Join(game, TranslationsDir, DiscoveredDir, "ja"+WorkingSuffix)
		if targets[0].Input != want {
			t.Errorf("入力が違う: got %q, want %q", targets[0].Input, want)
		}
		// ゲーム側から採ったことは印で分かるようにする。internal/web は
		// これを見て、保存を2つのファイルへ分けるかどうかを決める。
		if !targets[0].FromGame {
			t.Error("FromGame が立っていない")
		}
		// 書き出す先はリポジトリのまま。ゲーム側へは publish しない。
		wantOut := filepath.Join(root, TranslationsDir, "ja", StringsFile)
		if targets[0].Output != wantOut {
			t.Errorf("出力が違う: got %q, want %q", targets[0].Output, wantOut)
		}
	})

	t.Run("リポジトリの作業コピーが先に当たる", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, TranslationsDir, "ja", StringsFile), "key\n")
		writeFile(t, filepath.Join(root, TranslationsDir, DiscoveredDir, "ja"+WorkingSuffix), "in repo\n")
		game := gameTree(t, map[string]string{
			"Translations/_discovered/ja.working.csv": "in game\n",
		})

		targets, err := DiscoverEditTargets(root, game)
		if err != nil {
			t.Fatalf("DiscoverEditTargets がエラーを返した: %v", err)
		}
		want := filepath.Join(root, TranslationsDir, DiscoveredDir, "ja"+WorkingSuffix)
		if targets[0].Input != want {
			t.Errorf("リポジトリ側が先でない: got %q, want %q", targets[0].Input, want)
		}
		// リポジトリ側の作業コピーは publish が入力として拾うので、
		// 2つ書きにしない。印も立てない。
		if targets[0].FromGame {
			t.Error("リポジトリ側なのに FromGame が立っている")
		}
	})

	t.Run("ロケールごとに別々に決まる", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, TranslationsDir, "ja", StringsFile), "key\n")
		writeFile(t, filepath.Join(root, TranslationsDir, "ko", StringsFile), "key\n")
		writeFile(t, filepath.Join(root, TranslationsDir, DiscoveredDir, "ja"+WorkingSuffix), "in repo\n")
		game := gameTree(t, map[string]string{
			"Translations/_discovered/ko.working.csv": "in game\n",
		})

		targets, err := DiscoverEditTargets(root, game)
		if err != nil {
			t.Fatalf("DiscoverEditTargets がエラーを返した: %v", err)
		}
		got := make(map[string]string, len(targets))
		for _, target := range targets {
			got[target.Locale] = target.Input
		}
		if want := filepath.Join(root, TranslationsDir, DiscoveredDir, "ja"+WorkingSuffix); got["ja"] != want {
			t.Errorf("ja の入力が違う: got %q, want %q", got["ja"], want)
		}
		if want := filepath.Join(game, TranslationsDir, DiscoveredDir, "ko"+WorkingSuffix); got["ko"] != want {
			t.Errorf("ko の入力が違う: got %q, want %q", got["ko"], want)
		}
	})

	t.Run("ゲーム側にしか無いロケールは対象にしない", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, TranslationsDir, "ja", StringsFile), "key\n")
		game := gameTree(t, map[string]string{
			"Translations/_discovered/ja.working.csv": "source_en\n",
			// リポジトリに de のディレクトリが無い。出力先を勝手に作らない。
			"Translations/_discovered/de.working.csv": "source_en\n",
		})

		targets, err := DiscoverEditTargets(root, game)
		if err != nil {
			t.Fatalf("DiscoverEditTargets がエラーを返した: %v", err)
		}
		var names []string
		for _, target := range targets {
			names = append(names, target.Locale)
		}
		if strings.Join(names, ",") != "ja" {
			t.Errorf("対象が違う: %v", names)
		}
	})

	t.Run("ゲーム側にも公開ファイルにも無ければ対象にしない", func(t *testing.T) {
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, TranslationsDir, "ja"))
		game := gameTree(t, map[string]string{
			"Translations/_discovered/ko.working.csv": "source_en\n",
		})

		targets, err := DiscoverEditTargets(root, game)
		if err != nil {
			t.Fatalf("DiscoverEditTargets がエラーを返した: %v", err)
		}
		if len(targets) != 0 {
			t.Errorf("対象にしてはいけない: %+v", targets)
		}
	})

	t.Run("ゲームの指定が空なら DiscoverTargets と同じ", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, TranslationsDir, "ja", StringsFile), "key\n")
		gameTree(t, map[string]string{
			"Translations/_discovered/ja.working.csv": "source_en\n",
		})

		with, err := DiscoverEditTargets(root, "")
		if err != nil {
			t.Fatalf("DiscoverEditTargets がエラーを返した: %v", err)
		}
		plain, err := DiscoverTargets(root)
		if err != nil {
			t.Fatalf("DiscoverTargets がエラーを返した: %v", err)
		}
		if len(with) != len(plain) {
			t.Fatalf("件数が違う: %d と %d", len(with), len(plain))
		}
		for i := range with {
			if with[i] != plain[i] {
				t.Errorf("%d 件目が違う\ngot  %+v\nwant %+v", i, with[i], plain[i])
			}
		}
	})

	t.Run("ゲームのフォルダーが無くても落ちない", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, TranslationsDir, "ja", StringsFile), "key\n")

		targets, err := DiscoverEditTargets(root, filepath.Join(t.TempDir(), "nope"))
		if err != nil {
			t.Fatalf("DiscoverEditTargets がエラーを返した: %v", err)
		}
		want := filepath.Join(root, TranslationsDir, "ja", StringsFile)
		if len(targets) != 1 || targets[0].Input != want {
			t.Errorf("公開ファイル自身に落ちていない: %+v", targets)
		}
	})
}

func TestWorkingPath(t *testing.T) {
	root := filepath.FromSlash("/repo")
	game := filepath.FromSlash("/game/plugin")

	t.Run("ゲームの指定が無ければリポジトリ側", func(t *testing.T) {
		want := filepath.Join(root, TranslationsDir, DiscoveredDir, "ja"+WorkingSuffix)
		if got := WorkingPath(root, "", "ja"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("ゲームの指定があればゲーム側", func(t *testing.T) {
		// 「作業コピーがありません。ここへ書き出してください」と伝える先は、
		// Modが実際に書き出す場所でなければ案内にならない。
		want := filepath.Join(game, TranslationsDir, DiscoveredDir, "ja"+WorkingSuffix)
		if got := WorkingPath(root, game, "ja"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestDiscoverTargetsNeverLooksAtTheGame(t *testing.T) {
	// publish が使うのはこちら。ゲームのフォルダーを渡す道が無いことを、
	// 引数の形（第2引数が無い）だけでなく振る舞いでも書き留めておく。
	//
	// 目の前にゲームのフォルダーがあっても、入力は公開ファイル自身のままになる。
	root := t.TempDir()
	writeFile(t, filepath.Join(root, TranslationsDir, "ja", StringsFile), "key\n")
	gameTree(t, map[string]string{
		"Translations/_discovered/ja.working.csv": "source_en\n",
	})

	targets, err := DiscoverTargets(root)
	if err != nil {
		t.Fatalf("DiscoverTargets がエラーを返した: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("対象が1件でない: %+v", targets)
	}
	if targets[0].Input != targets[0].Output {
		t.Errorf("入力が公開ファイル自身でない: %+v", targets[0])
	}
	if targets[0].FromGame {
		t.Error("FromGame が立っている")
	}
}
