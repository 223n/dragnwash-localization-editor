package diff

import (
	"os/exec"
	"strings"
	"testing"
)

// TestGitOldOrderSkipsSideBranch は、マージのある履歴でも「最後の変更の直前」を
// 選ぶことを確かめる。
//
// このプロジェクトは git-flow でマージコミットを必ず残す。`git rev-list -2 HEAD -- <path>`
// の2つ目を旧版として使うと、側枝のコミットが2つ目に来たときに「既に新しい内容」を
// 旧再生順として読んでしまう。そうなると差がほとんど出ず、引き継ぎ候補が
// 保留ではなく「判定済みの0件」として消える。
func TestGitOldOrderSkipsSideBranch(t *testing.T) {
	const oldCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
		"L01 Ryan,intro,N1,1,line:aaaa1111,1111111111111111,Ryan,\n" +
		"L01 Ryan,intro,N1,2,line:bbbb2222,2222222222222222,Ryan,\n"
	// 側枝は同じファイルを別の理由で触る（話者だけ直す）。キーは旧のまま。
	const sideCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
		"L01 Ryan,intro,N1,1,line:aaaa1111,1111111111111111,Kobold,\n" +
		"L01 Ryan,intro,N1,2,line:bbbb2222,2222222222222222,Ryan,\n"
	// ゲーム更新。line:aaaa1111 のキーが変わる。
	const newCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
		"L01 Ryan,intro,N1,1,line:aaaa1111,3333333333333333,Ryan,\n" +
		"L01 Ryan,intro,N1,2,line:bbbb2222,2222222222222222,Ryan,\n"

	root := gitRepo(t)
	orderPath := writeOrder(t, root, oldCSV)
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-m", "v1")
	runGit(t, root, "branch", "side")
	runGit(t, root, "branch", "update")

	// 側枝を先にマージする。
	runGit(t, root, "checkout", "side")
	writeOrder(t, root, sideCSV)
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-m", "side")
	runGit(t, root, "checkout", "-")
	runGit(t, root, "merge", "--no-ff", "-m", "Merge side", "side")

	// ゲーム更新の枝をマージする。衝突は新版で解決する。
	runGit(t, root, "checkout", "update")
	writeOrder(t, root, newCSV)
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-m", "update")
	runGit(t, root, "checkout", "-")
	mergeOrResolve(t, root, "update", newCSV)

	got, err := GitOldOrder(root, orderPath)
	if err != nil {
		t.Fatalf("GitOldOrder が失敗した: %v", err)
	}
	// 旧版には line:aaaa1111 の旧キーが残っていること。側枝の版（キーは同じ）でも
	// 通ってしまうので、キーではなく「更新前であること」を鍵で見る。
	if !strings.Contains(string(got), "1111111111111111") {
		t.Errorf("旧再生順が更新前の版になっていない:\n%s", got)
	}
	if strings.Contains(string(got), "3333333333333333") {
		t.Errorf("旧再生順として、既に更新後の版を読んでいる:\n%s", got)
	}
}

// mergeOrResolve は branch をマージする。衝突したら want で解決してコミットする。
func mergeOrResolve(t *testing.T, root, branch, want string) {
	t.Helper()
	// merge も commit と同じく裏で git maintenance run --auto を起こす。
	// 詳しくは oldorder_test.go の gitTestOpts にある。
	cmd := exec.Command("git",
		append(gitTestOpts(), "merge", "--no-ff", "-m", "Merge "+branch, branch)...)
	cmd.Dir = root
	if err := cmd.Run(); err == nil {
		return
	}
	writeOrder(t, root, want)
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-m", "Merge "+branch)
}

// TestCarryoverIsDeterministic は、同じ入力なら必ず同じ候補を返すことを確かめる。
//
// 取り下げの判定をマップの反復順に任せると、同じ入力で候補が出たり出なかったり
// する。出荷後に再現できない不具合になるので、繰り返し呼んで確かめる。
//
// 仕掛けは「1つの引き継ぎ先に2つの引き継ぎ元が集まる」形。a は2つの引き継ぎ先を
// 持つので取り下げ、b は引き継ぎ先 x を a と取り合うので取り下げ。どちらも
// 候補にならないのが正しい。
func TestCarryoverIsDeterministic(t *testing.T) {
	oldEntries := orderFile(
		orderRow{"L01 Ryan", "intro", "N1", "1", "line:0001", "aaaaaaaaaaaaaaaa", "Ryan", ""},
		orderRow{"L01 Ryan", "intro", "N1", "2", "line:0002", "aaaaaaaaaaaaaaaa", "Ryan", ""},
		orderRow{"L01 Ryan", "intro", "N1", "3", "line:0003", "bbbbbbbbbbbbbbbb", "Ryan", ""},
	)
	newEntries := orderFile(
		orderRow{"L01 Ryan", "intro", "N1", "1", "line:0001", "1111111111111111", "Ryan", ""},
		orderRow{"L01 Ryan", "intro", "N1", "2", "line:0002", "2222222222222222", "Ryan", ""},
		orderRow{"L01 Ryan", "intro", "N1", "3", "line:0003", "1111111111111111", "Ryan", ""},
	)
	files := map[string]string{
		"data/script_order.csv": newEntries,
		"Translations/ja/strings.csv": publishedHeader +
			"aaaaaaaaaaaaaaaa,L01 Ryan,N1,1,Ryan,あ\n" +
			"bbbbbbbbbbbbbbbb,L01 Ryan,N1,3,Ryan,い\n",
	}

	for i := 0; i < 200; i++ {
		repo := newRepoWith(t, files, Options{OldOrder: fixedOldOrder(oldEntries)})
		rep := Compare(repo, nil)
		if got := rep.Locales[0].Counts[CatCarryover]; got != 0 {
			t.Fatalf("%d 回目で引き継ぎ候補 = %d 件, want 0（どちらも決められない）", i+1, got)
		}
	}
}

// TestCarryoverCopiedShowsLivePosition は、「複製」のときに旧キーがいまどこで
// 生きているかを伝えることを確かめる。
//
// 伝えないと報告が嘘になる。一覧に出る位置は公開ファイルの値、つまり旧キーが
// もういない古い位置で、引き継ぎ先の位置とほとんど同じに見えることさえある。
// 翻訳者はそこを見に行き、見つからないので「もう無い」と判断して消す。
func TestCarryoverCopiedShowsLivePosition(t *testing.T) {
	oldEntries := orderFile(
		orderRow{"L01 Ryan", "intro", "N1", "1", "line:0001", "aaaaaaaaaaaaaaaa", "Ryan", ""},
		orderRow{"Unused", "", "Start", "7", "line:0002", "aaaaaaaaaaaaaaaa", "Kobold", ""},
	)
	newEntries := orderFile(
		// N1 の英文だけが書き換わり、Unused/Start にはそのまま残る。
		orderRow{"L01 Ryan", "intro", "N1", "1", "line:0001", "1111111111111111", "Ryan", ""},
		orderRow{"Unused", "", "Start", "7", "line:0002", "aaaaaaaaaaaaaaaa", "Kobold", ""},
	)
	repo := newRepoWith(t, map[string]string{
		"data/script_order.csv": newEntries,
		"Translations/ja/strings.csv": publishedHeader +
			"aaaaaaaaaaaaaaaa,L01 Ryan,N1,1,Ryan,あ\n",
	}, Options{OldOrder: fixedOldOrder(oldEntries)})

	rep := Compare(repo, nil)
	var f Finding
	for _, x := range rep.Findings {
		if x.Category == CatCarryover {
			f = x
		}
	}
	if f.CarryKind != CarryCopied {
		t.Fatalf("CarryKind = %v, want %v", f.CarryKind, CarryCopied)
	}
	if !strings.Contains(f.Note, "Unused / Start / 7") {
		t.Errorf("旧キーが生きている場所を伝えていない: %q", f.Note)
	}

	var b strings.Builder
	if err := rep.WriteText(&b, TextOptions{All: true}); err != nil {
		t.Fatalf("WriteText が失敗した: %v", err)
	}
	if !strings.Contains(b.String(), "Unused / Start / 7") {
		t.Errorf("一覧に旧キーの生きている場所が出ていない:\n%s", b.String())
	}
}

// TestStaleOldOrderIsHeld は、旧再生順として読んだものがいまの版と見分けが
// つかないときに、判定せず保留することを確かめる。
//
// ゲーム更新をコミットしたあとに再生順を1行でも触ると、作業ツリーと HEAD が
// 違うので HEAD が旧版として選ばれる。しかし HEAD には既に新しい内容が入って
// いるので、キーの変化が1つも見えない。ここで「引き継ぎ候補 0 件」と書くと、
// 消えた行の訳を捨ててよいと読まれる。
func TestStaleOldOrderIsHeld(t *testing.T) {
	newEntries := orderFile(
		orderRow{"L01 Ryan", "intro", "N1", "1", "line:0001", "1111111111111111", "Ryan", ""},
	)
	files := map[string]string{
		"data/script_order.csv": newEntries,
		// 公開ファイルには、いまの再生順に無いキーの訳がある（＝台本から消えた行）。
		"Translations/ja/strings.csv": publishedHeader +
			"aaaaaaaaaaaaaaaa,L01 Ryan,N1,1,Ryan,あ\n",
	}

	t.Run("旧版がいまの版と同じなら保留する", func(t *testing.T) {
		repo := newRepoWith(t, files, Options{OldOrder: fixedOldOrder(newEntries)})
		rep := Compare(repo, nil)
		sum := rep.Locales[0]

		if sum.Counts[CatVanished] == 0 {
			t.Fatal("前提が崩れている: 台本から消えた行が無い")
		}
		if !sum.OldOrderStale {
			t.Error("旧版を疑っていない")
		}
		if sum.canJudge(CatCarryover) {
			t.Error("判定してしまっている")
		}
		var b strings.Builder
		if err := rep.WriteText(&b, TextOptions{}); err != nil {
			t.Fatalf("WriteText が失敗した: %v", err)
		}
		if !strings.Contains(b.String(), staleOldOrderReason) {
			t.Errorf("疑った理由を伝えていない:\n%s", b.String())
		}
	})

	t.Run("行が消えただけなら疑わない", func(t *testing.T) {
		// 旧版にだけある台詞ID。キーの変化は1件も無いが、旧版は正しい。
		oldEntries := newEntries + "L01 Ryan,intro,N1,2,line:0002,aaaaaaaaaaaaaaaa,Ryan,\n"
		repo := newRepoWith(t, files, Options{OldOrder: fixedOldOrder(oldEntries)})
		rep := Compare(repo, nil)
		if rep.Locales[0].OldOrderStale {
			t.Error("行が消えただけなのに旧版を疑っている")
		}
	})
}

// TestStaleOldOrderNotFlaggedWhenNothingVanished は、何も消えていないときに
// 疑わないことを確かめる。更新の無い普通の日がこれで、毎回疑うと警告が
// 読み飛ばされるようになる。
func TestStaleOldOrderNotFlaggedWhenNothingVanished(t *testing.T) {
	entries := orderFile(
		orderRow{"L01 Ryan", "intro", "N1", "1", "line:0001", "1111111111111111", "Ryan", ""},
	)
	repo := newRepoWith(t, map[string]string{
		"data/script_order.csv": entries,
		"Translations/ja/strings.csv": publishedHeader +
			"1111111111111111,L01 Ryan,N1,1,Ryan,あ\n",
	}, Options{OldOrder: fixedOldOrder(entries)})

	rep := Compare(repo, nil)
	sum := rep.Locales[0]
	if sum.OldOrderStale {
		t.Error("何も消えていないのに旧版を疑っている")
	}
	if !sum.canJudge(CatCarryover) {
		t.Error("判定できるはずなのに保留している")
	}
}
