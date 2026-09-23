package publish

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
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

// TestCheckBaseReportsInFileOrder は、報告の見本が公開ファイルの順に並び、
// 何度呼んでも同じであることを見る。
//
// Go の map の反復順はわざと毎回違う。[CheckBase] が map を直になぞっていた
// ころは、同じ入力で6回走らせると見本の並びが4通り出た（実機で確認）。
// 件数と「書かない」判断は変わらないので、害は報告の読みにくさだけである。
// それでも直すのは、この報告が不具合の報せとして貼られる先が手元とはかぎらず、
// 貼られた報告から元の状態を読み取れなくなるからである。
//
// 見本の上限（[baseDriftListMax]）を超える数の食い違いを置く。上限以下だと
// 「どの5件が入るか」が揺れる余地が無く、並びの揺れしか見られない。
func TestCheckBaseReportsInFileOrder(t *testing.T) {
	const drifts = baseDriftListMax + 4

	var repo, game, working strings.Builder
	repo.WriteString(baseHeader)
	game.WriteString(baseHeader)
	working.WriteString(baseWorkingHeader)
	var want []string
	for i := 0; i < drifts; i++ {
		// キーは16桁。i を末尾に埋めて、ファイルの順と辞書順を別にしておく。
		// 同じにすると、たまたま辞書順で並んだだけでも通ってしまう。
		key := fmt.Sprintf("%016x", (drifts-i)*0x1000+i)
		want = append(want, key)
		fmt.Fprintf(&repo, "%s,UI,,,UI,新しい訳%02d\n", key, i)
		fmt.Fprintf(&game, "%s,UI,,,UI,古い訳%02d\n", key, i)
		fmt.Fprintf(&working, "%s,UI,,,UI,,古い訳%02d\n", key, i)
	}

	target, current := newBaseTree(t, repo.String(), game.String(), working.String())

	var first []string
	for run := 0; run < 8; run++ {
		res, err := CheckBase(target, current)
		if err != nil {
			t.Fatalf("CheckBase: %v", err)
		}
		if res.Count != drifts {
			t.Fatalf("%d 回目: 食い違いが %d 件、%d 件を期待", run+1, res.Count, drifts)
		}
		var got []string
		for _, d := range res.Sample {
			got = append(got, d.Key)
		}
		if len(got) != baseDriftListMax {
			t.Fatalf("%d 回目: 見本が %d 件、%d 件を期待", run+1, len(got), baseDriftListMax)
		}
		if run == 0 {
			first = got
			// ファイルに現れた順の先頭から採ること。辞書順でも map 順でもない。
			if !slices.Equal(got, want[:baseDriftListMax]) {
				t.Fatalf("見本がファイルの順で並んでいない\n  出た順:   %v\n  期待した順: %v",
					got, want[:baseDriftListMax])
			}
			continue
		}
		if !slices.Equal(got, first) {
			t.Fatalf("%d 回目で並びが変わった\n  1回目: %v\n  今回:   %v", run+1, first, got)
		}
	}
}

// TestCheckBaseWithoutGameLooksAtNothing は、入力がゲーム側の作業コピーでない
// ロケールでは何も読まずに黙ることを見る。
//
// リポジトリの作業コピーや公開ファイル自身を入力にしたロケールには、見比べる
// 土台が無い。ここでコミット済みの公開ファイルを読みにいくと、見比べる相手が
// 無いのに「読めない」で publish 全体を止めうる。
func TestCheckBaseWithoutGameLooksAtNothing(t *testing.T) {
	// 読めば誤りになる中身を渡す。誤りが返らないことで、読んでいないと分かる。
	unreadable := []byte("key,Key,node,order,speaker,translation\naaaaaaaaaaaaaaaa,UI,,,UI,訳\n")

	res, err := CheckBase(Target{Locale: "ja"}, unreadable)
	if err != nil {
		t.Fatalf("土台が無いのに読みにいった: %v", err)
	}
	if res.Locale != "ja" {
		t.Errorf("ロケールが %q", res.Locale)
	}
	if res.Count != 0 || res.Sample != nil {
		t.Errorf("土台が無いのに食い違いを報せた: %+v", res)
	}
}

// TestCheckBaseFailsWhenItCannotRead は、読めなくて確かめられないときに
// 「そろっている」ではなく誤りを返すことを見る。
//
// 呼び出し側（dwloc publish と画面の書き出し）は、誤りなら1バイトも書かずに
// 終わる。ここで件数0を返すと、確かめられなかったものが素通りし、古い版への
// 巻き戻りがそのまま書き出される。
func TestCheckBaseFailsWhenItCannotRead(t *testing.T) {
	good := baseHeader + "aaaaaaaaaaaaaaaa,UI,,,UI,訳\n"
	// 列名が重複していると、どの列が訳なのかを決められない。
	duplicated := "key,Key,node,order,speaker,translation\naaaaaaaaaaaaaaaa,UI,,,UI,訳\n"
	working := baseWorkingHeader + "aaaaaaaaaaaaaaaa,UI,,,UI,,訳\n"

	t.Run("コミット済みの公開ファイルの列名が重複している", func(t *testing.T) {
		target, _ := newBaseTree(t, good, good, working)
		_, err := CheckBase(target, []byte(duplicated))
		var dup *csvfile.DuplicateColumnError
		if !errors.As(err, &dup) {
			t.Fatalf("err = %v, want *csvfile.DuplicateColumnError", err)
		}
	})

	t.Run("ゲーム側の公開ファイルの列名が重複している", func(t *testing.T) {
		target, current := newBaseTree(t, good, duplicated, working)
		_, err := CheckBase(target, current)
		var dup *csvfile.DuplicateColumnError
		if !errors.As(err, &dup) {
			t.Fatalf("err = %v, want *csvfile.DuplicateColumnError", err)
		}
	})

	t.Run("ゲーム側の公開ファイルの場所がディレクトリ", func(t *testing.T) {
		// 「無い」とは違う。無いなら何も言えないので黙るが、あるのに読めないなら
		// 確かめられていない。
		target := Target{Locale: "ja", GameBase: t.TempDir()}
		if _, err := CheckBase(target, []byte(good)); err == nil {
			t.Fatal("読めないのに誤りを返さなかった")
		}
	})
}

// TestCheckBaseComparesOnlyTranslatedRows は、見比べる相手を訳のある行に
// 絞るときの読み方を固定する。
//
// 読み方は publish が入力を読むときにそろえてある。訳の空の行は公開しない
// （R20）、重複したキーは先勝ち（R18）、台詞IDは大文字小文字を区別しない。
// ここがずれると、同じ公開ファイルを publish と別の中身として見比べることになり、
// 巻き戻りでないものを止めるか、巻き戻りを見逃す。
func TestCheckBaseComparesOnlyTranslatedRows(t *testing.T) {
	const keyA = "aaaaaaaaaaaaaaaa"

	tests := []struct {
		name string
		// repo / game はヘッダーを除いたデータ行。
		repo, game string
		// wantKeys は見本に出るキー。件数もこれで決まる。
		wantKeys []string
	}{
		{
			name: "コミット済みで訳が空の行は数えない",
			repo: keyA + ",UI,,,UI,\n",
			game: keyA + ",UI,,,UI,ゲームの訳\n",
		},
		{
			name: "ゲーム側で訳が空の行は数えない",
			repo: keyA + ",UI,,,UI,コミット済みの訳\n",
			game: keyA + ",UI,,,UI,\n",
		},
		{
			// キーの無い行は publish が捨てる。同じ行として見比べる相手がいない。
			name: "キーが空の行は数えない",
			repo: ",UI,,,UI,こちらの訳\n",
			game: ",UI,,,UI,あちらの訳\n",
		},
		{
			name: "コミット済みで重なったキーは先の行で見る",
			repo: keyA + ",UI,,,UI,新しい訳\n" + keyA + ",UI,,,UI,古い訳\n",
			game: keyA + ",UI,,,UI,新しい訳\n",
		},
		{
			name:     "ゲーム側で重なったキーも先の行で見る",
			repo:     keyA + ",UI,,,UI,新しい訳\n",
			game:     keyA + ",UI,,,UI,古い訳\n" + keyA + ",UI,,,UI,新しい訳\n",
			wantKeys: []string{keyA},
		},
		{
			// 見本のキーはコミット済みのファイルにあった綴りのまま返す。人はこの値を
			// ファイルから探すので、畳んだ形では引き当てられない。
			name:     "台詞IDは大文字小文字を区別せずに見比べる",
			repo:     "line:Intro_A,UI,,,UI,新しい訳\n",
			game:     "line:intro_a,UI,,,UI,古い訳\n",
			wantKeys: []string{"line:Intro_A"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			working := baseWorkingHeader + keyA + ",UI,,,UI,,訳\n"
			target, current := newBaseTree(t, baseHeader+tt.repo, baseHeader+tt.game, working)

			res, err := CheckBase(target, current)
			if err != nil {
				t.Fatalf("CheckBase: %v", err)
			}
			if res.Count != len(tt.wantKeys) {
				t.Fatalf("食い違いが %d 件、%d 件を期待: %+v", res.Count, len(tt.wantKeys), res)
			}
			var got []string
			for _, d := range res.Sample {
				got = append(got, d.Key)
				if d.Locale != "ja" {
					t.Errorf("見本のロケールが %q", d.Locale)
				}
			}
			if !slices.Equal(got, tt.wantKeys) {
				t.Errorf("見本のキーが %q、期待 %q", got, tt.wantKeys)
			}
		})
	}
}

// TestBaseReason は、土台がずれていることの理由が識別子と置換の組を
// そろえて持つことを見る。
//
// 画面は ID と Args から目録の文面を組み、CLI とテストは Text をそのまま出す。
// Args の名前が目録の {locale} と {count} からずれると、画面にだけ置き換わらない
// 波括弧が残る。
func TestBaseReason(t *testing.T) {
	got := BaseReason("pt-BR", 12)

	if got.ID != reason.PublishBaseDrift {
		t.Errorf("ID が %q、期待 %q", got.ID, reason.PublishBaseDrift)
	}
	if want := []string{"locale", "pt-BR", "count", "12"}; !slices.Equal(got.Args, want) {
		t.Errorf("Args が %q、期待 %q", got.Args, want)
	}
	if want := "ゲームに入っている訳が pt-BR で 12 件ちがう"; got.Text != want {
		t.Errorf("Text が %q、期待 %q", got.Text, want)
	}
}

// TestDriftHeadsShowsTheDifferenceAtEveryPosition は、食い違う位置がどこに
// あっても切り出しに違いが入ることを見る。
//
// 窓をずらすかどうかは、食い違う位置が [lossHeadRunes] 文字目より前か後ろかで
// 分かれる。境目の前後1文字で窓が違いを取りこぼすと、同じ文字列が2行並ぶだけの
// 報告に戻る。位置を1文字ずつずらして全部確かめる。
func TestDriftHeadsShowsTheDifferenceAtEveryPosition(t *testing.T) {
	base := distinctRunes(lossHeadRunes*2 + 3)

	for at := range base {
		changed := slices.Clone(base)
		changed[at] = 'X'
		a, b := driftHeads(string(base), string(changed))
		if a == b {
			t.Errorf("%d 文字目の違いが切り出しに入らない: %q", at, a)
		}
		if !strings.ContainsRune(b, 'X') {
			t.Errorf("%d 文字目を変えたのに、変えた文字が見えない: %q", at, b)
		}
	}

	// 片方がもう片方の先頭部分そのもの（閉じタグの付け外しなど）。短いほうの
	// 終わりが食い違う位置になる。
	for n := range base {
		a, b := driftHeads(string(base[:n]), string(base))
		if a == b {
			t.Errorf("先頭 %d 文字だけの訳と見分けられない: %q", n, a)
		}
	}
}

// TestWindow は切り出しの境目と省略の印を固定する。
//
// 印は「そこで削った」ことを示す。削っていない側に付けると、訳がそこで
// 途切れているのか、報告が切ったのかを読み手が見分けられない。
func TestWindow(t *testing.T) {
	full := distinctRunes(lossHeadRunes)
	long := distinctRunes(lossHeadRunes * 2)

	tests := []struct {
		name  string
		runes []rune
		start int
		want  string
	}{
		{"短ければそのまま", full[:3], 0, string(full[:3])},
		{"ちょうどの長さなら印を付けない", full, 0, string(full)},
		{"1文字でも長ければ後ろに印", long[:lossHeadRunes+1], 0, string(long[:lossHeadRunes]) + lossHeadEllipsis},
		{"前を削ったら頭に印", full, 2, lossHeadEllipsis + string(full[2:])},
		{"前後とも削れば両方に印", long, 4,
			lossHeadEllipsis + string(long[4:4+lossHeadRunes]) + lossHeadEllipsis},
		{"長さを越えた位置でも落ちない", full[:3], 10, lossHeadEllipsis},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := window(tt.runes, tt.start); got != tt.want {
				t.Errorf("window = %q、期待 %q", got, tt.want)
			}
		})
	}
}

// distinctRunes は互いに違う文字を n 個並べる。切り出した位置が1文字ずれた
// だけでも結果が変わるよう、同じ文字を繰り返さない。
func distinctRunes(n int) []rune {
	out := make([]rune, n)
	for i := range out {
		out[i] = 'あ' + rune(i)
	}
	return out
}
