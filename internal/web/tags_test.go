package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
)

// この束は、「タグの開閉がそろわない行」が保存のたびにその行だけ判定し直される
// ことを見る。
//
// ほかのカテゴリと違って、この判定は保存した行の訳だけで決まる（[diff.CheckTags]。
// 原文とも他のロケールとも比べない）。だから起動時の判定を置いたままにする理由が
// 無く、置いたままにすると、閉じ忘れを直した行にバッジが残り、新しく閉じ忘れた
// 行にバッジが付かない。翻訳者が欲しいのは「いま閉じていない行」であって、
// 「起動したときに閉じていなかった行」ではない。

// tagBadge はその行のバッジから「タグの開閉がそろわない行」を探す。
func tagBadge(badges []badgeView) (badgeView, bool) {
	for _, b := range badges {
		if b.Category == "tag_unbalanced" {
			return b, true
		}
	}
	return badgeView{}, false
}

// newCountsGameWithTranslation は [newCountsGame] の作業コピーの、訳が入っている
// 行（srcDone）の訳を差し替えたものを作る。
func newCountsGameWithTranslation(t *testing.T, translation string) string {
	t.Helper()

	game := newCountsGame(t)
	path := filepath.Join(game, "Translations", "_discovered", "ja.working.csv")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	from := "," + srcDone + ",もしもし？\n"
	if !strings.Contains(string(body), from) {
		t.Fatalf("作業コピーに差し替える行が無い")
	}
	replaced := strings.Replace(string(body), from, ","+srcDone+","+translation+"\n", 1)
	if err := os.WriteFile(path, []byte(replaced), 0o644); err != nil {
		t.Fatal(err)
	}
	return game
}

// idOf はそのキーのデータ行の ID を返す。
func idOf(t *testing.T, lines []lineView, k string) int {
	t.Helper()
	for _, l := range lines {
		if l.Kind == lineKindData && l.Key == k {
			return l.ID
		}
	}
	t.Fatalf("キー %s の行が無い", k)
	return 0
}

func TestSaveRejudgesTagBalance(t *testing.T) {
	root := newCountsRoot(t)
	s := newTestServer(t, Options{Root: root, Game: newCountsGame(t), UILang: "ja"})

	before := getLines(t, s, "ja")
	was := mustCount(t, before.Counts, "tag_unbalanced")
	if !was.Judged || was.Count != 0 {
		t.Fatalf("起動時は1件も無いはず: %+v", was)
	}
	line := idOf(t, before.Lines, key.For(srcDone))

	// 閉じ忘れて保存する。その行にバッジが付き、件数も行数も1増える。
	rec := save(t, s, "ja", before.Version,
		rowEdit{ID: line, Key: key.For(srcDone), Translation: "<i>もしもし？"})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
	broken := decode[rowsResponse](t, rec.Body.Bytes())
	if len(broken.Results) != 1 || !broken.Results[0].Saved {
		t.Fatalf("保存できていない: %+v", broken.Results)
	}
	badge, ok := tagBadge(broken.Results[0].Badges)
	if !ok {
		t.Fatalf("閉じ忘れた行にバッジが付いていない: %+v", broken.Results[0].Badges)
	}
	if badge.Status != "info" || badge.Label != "タグの開閉がそろわない行" {
		t.Errorf("バッジが違う: %+v", badge)
	}
	if badge.Note != "閉じていないタグ: <i>" {
		t.Errorf("注記が違う: %q", badge.Note)
	}
	now := mustCount(t, broken.Counts, "tag_unbalanced")
	if now.Count != 1 || now.Rows != 1 || now.RowsDiffer {
		t.Errorf("件数 %d ／ 行数 %d。1件1行のはず", now.Count, now.Rows)
	}
	// 局所更新したことを断る。
	if !strings.Contains(strings.Join(broken.Notes, "\n"), "タグの開閉") {
		t.Errorf("局所更新の断りが無い: %v", broken.Notes)
	}

	// 読み直しても同じ判定が付いている。起動時のスナップショットへ戻らない。
	again := getLines(t, s, "ja")
	for _, l := range again.Lines {
		if l.ID != line {
			continue
		}
		if _, ok := tagBadge(l.Badges); !ok {
			t.Errorf("読み直すとバッジが消えた: %+v", l.Badges)
		}
	}
	if got := mustCount(t, again.Counts, "tag_unbalanced"); got.Count != 1 || got.Rows != 1 {
		t.Errorf("読み直したあとの件数 %d ／ 行数 %d", got.Count, got.Rows)
	}

	// 閉じて保存し直す。バッジが外れ、件数も行数も戻る。
	rec = save(t, s, "ja", broken.Version,
		rowEdit{ID: line, Key: key.For(srcDone), Translation: "<i>もしもし？</i>"})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
	fixed := decode[rowsResponse](t, rec.Body.Bytes())
	if _, ok := tagBadge(fixed.Results[0].Badges); ok {
		t.Errorf("直した行にバッジが残っている: %+v", fixed.Results[0].Badges)
	}
	if got := mustCount(t, fixed.Counts, "tag_unbalanced"); got.Count != 0 || got.Rows != 0 {
		t.Errorf("直したあとの件数 %d ／ 行数 %d。0に戻るはず", got.Count, got.Rows)
	}
}

// TestStartupTagBadgeFollowsTheSavedText は、起動時に当たっていた行を直すと
// バッジが外れ、別の形で壊したままなら注記がいまの訳のものになることを見る。
func TestStartupTagBadgeFollowsTheSavedText(t *testing.T) {
	root := newCountsRoot(t)
	game := newCountsGameWithTranslation(t, "<b>もしもし？")
	s := newTestServer(t, Options{Root: root, Game: game, UILang: "ja"})

	before := getLines(t, s, "ja")
	was := mustCount(t, before.Counts, "tag_unbalanced")
	if was.Count != 1 || was.Rows != 1 {
		t.Fatalf("起動時に1件1行のはず: %+v", was)
	}
	line := idOf(t, before.Lines, key.For(srcDone))

	// 別の形で壊したまま。件数は動かず、注記だけがいまの訳のものになる。
	rec := save(t, s, "ja", before.Version,
		rowEdit{ID: line, Key: key.For(srcDone), Translation: "もしもし？</i>"})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
	still := decode[rowsResponse](t, rec.Body.Bytes())
	badge, ok := tagBadge(still.Results[0].Badges)
	if !ok {
		t.Fatalf("壊れたままの行にバッジが無い")
	}
	if badge.Note != "開始の無い終了タグ: </i>" {
		t.Errorf("注記が起動時のまま: %q", badge.Note)
	}
	if got := mustCount(t, still.Counts, "tag_unbalanced"); got.Count != 1 || got.Rows != 1 {
		t.Errorf("件数 %d ／ 行数 %d。動かないはず", got.Count, got.Rows)
	}

	// 直す。起動時の判定に引きずられず、バッジも件数も消える。
	rec = save(t, s, "ja", still.Version,
		rowEdit{ID: line, Key: key.For(srcDone), Translation: "もしもし？"})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
	fixed := decode[rowsResponse](t, rec.Body.Bytes())
	if _, ok := tagBadge(fixed.Results[0].Badges); ok {
		t.Errorf("直した行に起動時のバッジが残っている")
	}
	if got := mustCount(t, fixed.Counts, "tag_unbalanced"); got.Count != 0 || got.Rows != 0 {
		t.Errorf("直したあとの件数 %d ／ 行数 %d。0に戻るはず", got.Count, got.Rows)
	}
}
