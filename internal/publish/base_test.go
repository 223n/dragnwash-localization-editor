package publish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// baseHeader は公開ファイルの列。
const baseHeader = "key,section,node,order,speaker,translation\n"

// baseWorkingHeader は作業コピーの列。source_en を持つ。
const baseWorkingHeader = "key,section,node,order,speaker,source_en,translation\n"

// newBaseTree は、リポジトリとゲームのフォルダーを1組作る。
//
// repo / game のどちらにも ja の公開ファイルを置き、ゲーム側には作業コピーも置く。
// 返すのは [DiscoverTargetsWithGame] が選んだ ja の Target である。
func newBaseTree(t *testing.T, repoJa, gameJa, working string) (Target, []byte) {
	t.Helper()

	root, game := t.TempDir(), t.TempDir()
	write := func(dir, rel, body string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(root, "Translations/ja/strings.csv", repoJa)
	if gameJa != "" {
		write(game, "Translations/ja/strings.csv", gameJa)
	}
	if working != "" {
		write(game, "Translations/_discovered/ja.working.csv", working)
	}

	targets, err := DiscoverTargetsWithGame(root, game)
	if err != nil {
		t.Fatalf("DiscoverTargetsWithGame: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("ja の Target が1つでない: %+v", targets)
	}
	current, err := os.ReadFile(targets[0].Output)
	if err != nil {
		t.Fatal(err)
	}
	return targets[0], current
}

// TestCheckBaseCatchesAStaleGame は、ゲーム側の訳が古いときに捕まえることを見る。
//
// これが [CheckLoss] では捕まらない形である。行は消えず、訳も空にならず、
// 値だけが古い版へ戻る。実機（開発機のSteam）で実際に3件起きていた。
func TestCheckBaseCatchesAStaleGame(t *testing.T) {
	// 先頭を長くそろえてある。実機で出た3件は3件ともこの形で、先頭から一定の
	// 文字数を切り出すだけの見本では、コミット済みとゲーム側が同じ文字列になって
	// 並んでいた。違いを見せるための報告なのに、違いが切り落とされていた。
	repo := baseHeader +
		"aaaaaaaaaaaaaaaa,UI,,,UI,<i>ずいぶんと長い訳の文章がここにあります。</i>\n" +
		"bbbbbbbbbbbbbbbb,UI,,,UI,そのまま\n"
	game := baseHeader +
		"aaaaaaaaaaaaaaaa,UI,,,UI,<i>ずいぶんと長い訳の文章がここにあります。\n" + // 閉じタグが無い古い版
		"bbbbbbbbbbbbbbbb,UI,,,UI,そのまま\n"
	working := baseWorkingHeader +
		"aaaaaaaaaaaaaaaa,UI,,,UI,,<i>ずいぶんと長い訳の文章がここにあります。\n"

	target, current := newBaseTree(t, repo, game, working)
	if target.GameBase == "" {
		t.Fatal("入力がゲーム側の作業コピーになっていない")
	}
	res, err := CheckBase(target, current)
	if err != nil {
		t.Fatalf("CheckBase: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("食い違いが %d 件、1件を期待: %+v", res.Count, res)
	}
	got := res.Sample[0]
	if got.Key != "aaaaaaaaaaaaaaaa" {
		t.Errorf("キーが畳んだ形で返っている: %q", got.Key)
	}
	// 違いが見えること。先頭を切るだけだと、両方とも同じ文字列になる。
	if got.Repo == got.Game {
		t.Errorf("見本が同じ文字列で、違いが見えない: %q", got.Repo)
	}
}

// TestCheckBaseStaysQuietWhenTheGameIsCurrent は、ゲーム側がそろっていれば
// 何も言わないことを見る。
//
// ここが鳴ると publish が使えない。作業コピーの訳がコミット済みと違っていても、
// 土台がそろっているかぎり、それは翻訳者が入れた編集である。
func TestCheckBaseStaysQuietWhenTheGameIsCurrent(t *testing.T) {
	same := baseHeader +
		"aaaaaaaaaaaaaaaa,UI,,,UI,おなじ訳\n" +
		"bbbbbbbbbbbbbbbb,UI,,,UI,これもおなじ\n"
	// 作業コピーでは1件を書き換えてある。これは翻訳者の編集で、止める理由ではない。
	working := baseWorkingHeader +
		"aaaaaaaaaaaaaaaa,UI,,,UI,,ゲームで直した訳\n" +
		"bbbbbbbbbbbbbbbb,UI,,,UI,,これもおなじ\n"

	target, current := newBaseTree(t, same, same, working)
	res, err := CheckBase(target, current)
	if err != nil {
		t.Fatalf("CheckBase: %v", err)
	}
	if res.Count != 0 {
		t.Errorf("そろっているのに %d 件を報せた: %+v", res.Count, res)
	}
}

// TestCheckBaseIgnoresRowsOnlyOneSideHas は、片側にしか無い行を数えないことを見る。
//
// ゲームの版が古ければ、行そのものの増減は当たり前に起きる。それを数えると、
// 版が1つ違うだけで publish が止まり、この確認が実質「--game を使うな」になる。
// 行が減って実際に訳が消えるなら [CheckLoss] が捕まえる。
func TestCheckBaseIgnoresRowsOnlyOneSideHas(t *testing.T) {
	repo := baseHeader +
		"aaaaaaaaaaaaaaaa,UI,,,UI,おなじ訳\n" +
		"cccccccccccccccc,UI,,,UI,リポジトリにしかない\n"
	game := baseHeader +
		"aaaaaaaaaaaaaaaa,UI,,,UI,おなじ訳\n" +
		"dddddddddddddddd,UI,,,UI,ゲームにしかない\n"
	working := baseWorkingHeader + "aaaaaaaaaaaaaaaa,UI,,,UI,,おなじ訳\n"

	target, current := newBaseTree(t, repo, game, working)
	res, err := CheckBase(target, current)
	if err != nil {
		t.Fatalf("CheckBase: %v", err)
	}
	if res.Count != 0 {
		t.Errorf("片側にしか無い行を数えている: %+v", res)
	}
}

// TestCheckBaseSkipsWhenThereIsNoBase は、見比べる土台が無いときに黙ることを見る。
//
// 入力がリポジトリの作業コピーや公開ファイル自身のときは、ゲーム側の版は
// 関わりが無い。ゲーム側に公開ファイルが無いときも、そろっているともいないとも
// 言えない。どちらも「確かめられない」であって「ずれている」ではない。
func TestCheckBaseSkipsWhenThereIsNoBase(t *testing.T) {
	repo := baseHeader + "aaaaaaaaaaaaaaaa,UI,,,UI,訳\n"
	working := baseWorkingHeader + "aaaaaaaaaaaaaaaa,UI,,,UI,,訳\n"

	t.Run("ゲーム側に公開ファイルが無い", func(t *testing.T) {
		target, current := newBaseTree(t, repo, "", working)
		if target.GameBase == "" {
			t.Fatal("入力がゲーム側の作業コピーになっていない")
		}
		res, err := CheckBase(target, current)
		if err != nil {
			t.Fatalf("CheckBase: %v", err)
		}
		if res.Count != 0 {
			t.Errorf("土台が無いのに %d 件を報せた", res.Count)
		}
	})

	t.Run("そもそもゲームを見ていない", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, filepath.FromSlash("Translations/ja/strings.csv"))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(repo), 0o644); err != nil {
			t.Fatal(err)
		}
		targets, err := DiscoverTargets(root)
		if err != nil {
			t.Fatal(err)
		}
		if targets[0].GameBase != "" {
			t.Fatalf("--game を渡していないのに土台が埋まっている: %q", targets[0].GameBase)
		}
	})
}

// TestDriftHeadsShowsTheDifference は、切り出しに違いが入ることを見る。
//
// 先頭から一定の文字数を出すだけでは足りない。実機で出た3件は、3件とも先頭が
// 同じで、並べても同じ文字列が2行出るだけだった。
func TestDriftHeadsShowsTheDifference(t *testing.T) {
	tests := []struct {
		name       string
		repo, game string
	}{
		{
			name: "頭がそろっていて終わりだけ違う",
			repo: "ずいぶんと長い訳の文章がここにあります。おわり",
			game: "ずいぶんと長い訳の文章がここにあります。",
		},
		{
			name: "真ん中だけ違う",
			repo: "Drag'n Wash を 16 言語で遊べるようにします。",
			game: "Drag'n Wash を 13 言語で遊べるようにします。",
		},
		{
			name: "頭から違う",
			repo: "まったくちがう訳",
			game: "ぜんぜん別の訳",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := driftHeads(tt.repo, tt.game)
			if a == b {
				t.Errorf("同じ文字列になり、違いが見えない: %q", a)
			}
			// 切り出しが元の訳より長くなることは無い（印のぶんを除く）。
			trim := func(s string) string {
				return strings.Trim(s, lossHeadEllipsis)
			}
			if !strings.Contains(tt.repo, trim(a)) {
				t.Errorf("コミット済みの切り出しが元の訳に含まれない: %q", a)
			}
			if !strings.Contains(tt.game, trim(b)) {
				t.Errorf("ゲーム側の切り出しが元の訳に含まれない: %q", b)
			}
		})
	}
}
