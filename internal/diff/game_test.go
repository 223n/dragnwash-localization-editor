package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// newGame はゲーム側のプラグインフォルダーを作り、そのパスを返す。
// files のキーはプラグインフォルダーからのスラッシュ区切りの相対パス。
func newGame(t *testing.T, files map[string]string) string {
	t.Helper()

	game := filepath.Join(t.TempDir(), "BepInEx", "plugins", "DragNWashLocalization")
	for name, content := range files {
		path := filepath.Join(game, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("ディレクトリを作れない: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("ファイルを書けない: %v", err)
		}
	}
	return game
}

// newRepoAndGame は翻訳リポジトリとゲームのフォルダーを別々に作って読み込む。
func newRepoAndGame(t *testing.T, repoFiles, gameFiles map[string]string) *Repo {
	t.Helper()

	root := t.TempDir()
	for name, content := range repoFiles {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("ディレクトリを作れない: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("ファイルを書けない: %v", err)
		}
	}
	repo, err := LoadWith(root, Options{Working: true, Game: newGame(t, gameFiles)})
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	return repo
}

// localeOf は名前でロケールを引く。
func localeOf(t *testing.T, repo *Repo, name string) Locale {
	t.Helper()

	for _, loc := range repo.Locales {
		if loc.Name == name {
			return loc
		}
	}
	t.Fatalf("ロケール %s が無い", name)
	return Locale{}
}

func TestLoadWithGameReadsWorkingCopy(t *testing.T) {
	// ゲーム側の作業コピーを読めると、未翻訳を判定できる。リポジトリ側だけを
	// 見ていたときは、この判定が「判定できません」で止まっていた。
	repo := newRepoAndGame(t,
		map[string]string{
			"data/script_order.csv": orderTwo,
			"Translations/ja/strings.csv": publishedHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
		},
		map[string]string{
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n" +
				keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold," + srcBye + ",\n",
		})

	loc := localeOf(t, repo, "ja")
	if !loc.WorkingExists {
		t.Fatal("作業コピーがあると分かっていない")
	}
	if !loc.HasWorking {
		t.Fatal("作業コピーを読んでいない")
	}
	if len(loc.Working) != 2 {
		t.Fatalf("読んだ行数が違う: %d", len(loc.Working))
	}

	report := Compare(repo, nil)
	if got := counts(t, report, "ja")[CatUntranslated]; got != 1 {
		t.Errorf("未翻訳が %d 件。1件を期待", got)
	}
}

func TestLoadWithGameKeepsOutputInRepo(t *testing.T) {
	repo := newRepoAndGame(t,
		map[string]string{
			"data/script_order.csv": orderCSV1,
			"Translations/ja/strings.csv": publishedHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
		},
		map[string]string{
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n",
		})

	loc := localeOf(t, repo, "ja")
	// 公開ファイルはリポジトリ側のまま。ゲーム側の同名ファイルへ移らない。
	if !strings.HasPrefix(loc.PublishedPath, repo.Root) {
		t.Errorf("公開ファイルが %q。リポジトリの外を指している", loc.PublishedPath)
	}
	// 作業コピーはゲーム側。publish が入力に選ぶファイルと同じでなければ、
	// 「publish を回すとどうなるか」という主張が崩れる。
	if strings.HasPrefix(loc.WorkingPath, repo.Root) {
		t.Errorf("作業コピーが %q。ゲーム側を指していない", loc.WorkingPath)
	}
}

func TestLoadWithGameUsesRepositoryOrder(t *testing.T) {
	// ゲーム側にも再生順があるが、読むのはリポジトリ側だけ。引き継ぎ候補は
	// git の履歴にある1つ前の再生順が根拠なので、履歴の無いゲーム側へ
	// 替えると、その判定そのものが消える。
	repo := newRepoAndGame(t,
		map[string]string{
			"data/script_order.csv": orderCSV1,
			"Translations/ja/strings.csv": publishedHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
		},
		map[string]string{
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n",
			"data/script_order.csv":                     orderTwo,
			"Translations/_discovered/script_order.csv": orderTwo,
		})

	if !strings.HasPrefix(repo.OrderPath, repo.Root) {
		t.Errorf("再生順が %q。ゲーム側を読んでいる", repo.OrderPath)
	}
	if len(repo.Order.Entries) != 1 {
		t.Errorf("再生順の行数が %d。リポジトリ側の1行を期待", len(repo.Order.Entries))
	}
}

func TestLoadWithGameWorkingPathPointsAtTheGame(t *testing.T) {
	// 作業コピーがまだ無いとき、「ここへ書き出してください」と示す先は
	// ゲーム側にする。Modが書き出すのはそこで、リポジトリ側を示しても
	// 翻訳者にはそこへ置く手立てが無い。
	repo := newRepoAndGame(t,
		map[string]string{
			"data/script_order.csv": orderCSV1,
			"Translations/ja/strings.csv": publishedHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
		},
		nil)

	loc := localeOf(t, repo, "ja")
	if loc.WorkingExists {
		t.Fatal("作業コピーは無いはず")
	}
	if !strings.HasSuffix(filepath.ToSlash(loc.WorkingPath),
		"/"+publish.TranslationsDir+"/"+publish.DiscoveredDir+"/ja"+publish.WorkingSuffix) {
		t.Errorf("案内先が %q", loc.WorkingPath)
	}
	if strings.HasPrefix(loc.WorkingPath, repo.Root) {
		t.Errorf("案内先が %q。リポジトリ側を指している", loc.WorkingPath)
	}
}

func TestLoadWithoutGameIsUnchanged(t *testing.T) {
	// --game を指定しないときの読み込みは、いままでと同じ。ゲームのフォルダーが
	// 目の前にあっても見に行かない。
	files := map[string]string{
		"data/script_order.csv": orderTwo,
		"Translations/ja/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
	}
	repo := newRepo(t, files, true)

	loc := localeOf(t, repo, "ja")
	if loc.WorkingExists || loc.HasWorking {
		t.Fatal("作業コピーを見つけてしまっている")
	}
	want := filepath.Join(repo.Root, publish.TranslationsDir, publish.DiscoveredDir,
		"ja"+publish.WorkingSuffix)
	if loc.WorkingPath != want {
		t.Errorf("案内先が %q、期待 %q", loc.WorkingPath, want)
	}
}
