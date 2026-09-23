package web

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// ここは画面の1行と件数を組み立てるところ（model.go）の試験。
//
// HTTP を通すと見本のリポジトリから作りにくい形（壊れた行のある集計、読めな
// かった再生順、起動時の判定と食い違う控え）を、組み立ての関数へ直に渡して見る。

func TestHeadingLevel(t *testing.T) {
	// 見出しの深さはファイルにある印だけで決める。印の無いコメント行（翻訳者が
	// 書いた1行メモ）は other にする。section や node にすると、絞り込みで
	// 見出しを捨てる順（doc.go「見出しは3段」）が狂う。
	cases := []struct {
		body string
		want string
	}{
		{publish.SectionMarker + " Level 1: Ryan (Sunny) =====", headingSection},
		{publish.NodeMarker + " intro: Ryan_1_intro ---", headingNode},
		{"# メモ: ここはあとで直す", headingOther},
		{"#", headingOther},
		// 印は先頭から見る。途中にあっても見出しの深さにはしない。
		{"# メモ " + publish.SectionMarker, headingOther},
	}
	for _, tc := range cases {
		if got := headingLevel(tc.body); got != tc.want {
			t.Errorf("headingLevel(%q) が %q、%q を期待", tc.body, got, tc.want)
		}
	}
}

func TestBadgesDropDuplicateCategories(t *testing.T) {
	// 同じキーが同じカテゴリで2回出ることがある（引き継ぎ候補は行ごとに候補が付く）。
	// バッジは「その行に何が当たっているか」なので、カテゴリで重複を消す。
	// 残すのは先に出たほうである。キーの無い Finding は行と結び付けられないので
	// バッジにしない（件数には入っている）。
	s := newTestServer(t, Options{UILang: "ja"})
	ja := s.cat.lookup("ja")
	findings := []diff.Finding{
		{Key: keyKept, Category: diff.CatVanished, Note: "1つ目"},
		{Key: keyKept, Category: diff.CatVanished, Note: "2つ目"},
		{Key: keyKept, Category: diff.CatUnknownOrigin},
		{Key: "", Category: diff.CatVanished, Note: "キーが無い"},
	}

	got := s.badgesByKey(ja, findings, nil, nil)

	var ids []string
	for _, b := range got[keyKept] {
		ids = append(ids, b.Category)
	}
	want := diff.CatVanished.ID() + "," + diff.CatUnknownOrigin.ID()
	if strings.Join(ids, ",") != want {
		t.Errorf("バッジが %v、%s を期待", ids, want)
	}
	if len(got[keyKept]) > 0 && got[keyKept][0].Note != "1つ目" {
		t.Errorf("先に出た注記が残っていない: %q", got[keyKept][0].Note)
	}
	if _, ok := got[""]; ok {
		t.Error("キーの無い Finding がバッジになった")
	}
}

func TestFindingNoteWithoutReason(t *testing.T) {
	// 識別子の無い Finding（日本語の Note だけが入っている）でも注記を落とさない。
	// 目録を引けないからといって空にすると、注記が画面から消える。消えるのは、
	// 訳されずに出るより悪い。
	s := newTestServer(t, Options{UILang: "en"})
	en := s.cat.lookup("en")
	f := diff.Finding{Key: keyKept, Category: diff.CatVanished, Note: "識別子の無い注記"}

	if got := s.findingNote(en, f); got != f.Note {
		t.Errorf("注記が %q、%q を期待", got, f.Note)
	}
	badges := s.badgesByKey(en, []diff.Finding{f}, nil, nil)
	if len(badges[keyKept]) != 1 || badges[keyKept][0].Note != f.Note {
		t.Errorf("バッジの注記が落ちた: %+v", badges[keyKept])
	}
}

func TestLabelsFallBackToDiffNames(t *testing.T) {
	// 目録にカテゴリや重さの名前が無いときは internal/diff の名前を出す。
	// diff にカテゴリが増えて目録が追いつくまでのあいだ、鍵（category.xxx）が
	// 画面に生で出ないようにする。
	s := newTestServer(t, Options{UILang: "en"})
	en := s.cat.lookup("en")
	// 目録は待ち受けごとに読み込むので、ここで消してもほかの試験には響かない。
	for _, c := range s.cat.byLang {
		delete(c.Messages, "category."+diff.CatVanished.ID())
		delete(c.Messages, "status."+diff.StatusReview.ID())
	}

	if got := s.categoryLabel(en, diff.CatVanished); got != diff.CatVanished.String() {
		t.Errorf("カテゴリ名が %q、%q を期待", got, diff.CatVanished.String())
	}
	if got := s.statusLabel(en, diff.StatusReview); got != diff.StatusReview.String() {
		t.Errorf("重さの名前が %q、%q を期待", got, diff.StatusReview.String())
	}
	// 目録にある名前は目録から引いたまま。
	if got, want := s.categoryLabel(en, diff.CatUnknownOrigin),
		s.cat.T(en, "category."+diff.CatUnknownOrigin.ID()); got != want {
		t.Errorf("目録にある名前が %q、%q を期待", got, want)
	}
}

// judgedSummary は、どのカテゴリも判定できたことにした集計を返す。
//
// 件数は counts のとおりにする。組み立ての関数へ直に渡して、判定できなかった
// ことによる分かれ道を通らずに数の扱いだけを見るために使う。
func judgedSummary(locale string, counts map[diff.Category]int) diff.Summary {
	return diff.Summary{
		Locale:         locale,
		HasWorking:     true,
		OrderKeys:      true,
		OrderLineIDs:   true,
		OrderNorms:     true,
		HasLayoutRisks: true,
		OldOrder:       true,
		Counts:         counts,
	}
}

func TestCountsNeverGoBelowZero(t *testing.T) {
	// 起動時の件数から、局所更新のぶんを引く。起動時の判定と控えが食い違っても
	// （判定は 0 件なのに、控えの上では直した行がある）、負の数は出さない。
	// 「-1 件」は読み手に意味が通らないうえ、件数と行数の食い違いとして出てしまう。
	s := newTestServer(t, Options{UILang: "ja"})
	ja := s.cat.lookup("ja")
	s.overlay.addUntranslated("ja", keyKept)
	s.markFilled("ja", keyKept, true)
	s.overlay.addTagged("ja", keyKept2)
	s.markTags("ja", keyKept2, diff.CheckTags("<b>閉じた</b>"))

	sum := judgedSummary("ja", map[diff.Category]int{
		diff.CatUntranslated:  0,
		diff.CatTagUnbalanced: 0,
	})
	counts := s.buildCounts(ja, "ja", sum, nil)

	for _, id := range []string{diff.CatUntranslated.ID(), diff.CatTagUnbalanced.ID()} {
		c := mustCount(t, counts, id)
		if !c.Judged {
			t.Fatalf("%s が判定済みになっていない", id)
		}
		if c.Count != 0 {
			t.Errorf("%s の件数が %d、0 を期待", id, c.Count)
		}
		if c.RowsDiffer || c.NoRowHere {
			t.Errorf("%s が行数と食い違うと言っている: %+v", id, c)
		}
	}
}

func TestStatsShowBrokenRowsOnlyWhenThereAreSome(t *testing.T) {
	// 壊れた行の数は1件でもあるときだけ出す。実データでは全ロケールとも0件で、
	// 毎回「0」と並べても読む手がかりにならない。1件でも出たらファイルが壊れている。
	s := newTestServer(t, Options{UILang: "ja"})
	ja := s.cat.lookup("ja")
	label := s.cat.T(ja, "stats.broken_rows")

	cases := []struct {
		name   string
		broken int
	}{
		{"壊れた行が無い", 0},
		{"壊れた行がある", 2},
	}
	for _, tc := range cases {
		stats := s.buildStats(ja, diff.Summary{BrokenRows: tc.broken}, 10, 8)
		var found *statView
		for i := range stats {
			if stats[i].Label == label {
				found = &stats[i]
			}
		}
		switch {
		case tc.broken == 0 && found != nil:
			t.Errorf("%s: 0 件なのに出している", tc.name)
		case tc.broken > 0 && found == nil:
			t.Errorf("%s: 出していない: %+v", tc.name, stats)
		case tc.broken > 0 && found.Value != tc.broken:
			t.Errorf("%s: 数が %d、%d を期待", tc.name, found.Value, tc.broken)
		}
	}
}

func TestNotesSayWhatCouldNotBeRead(t *testing.T) {
	// 断り書きには、読んだものと読めなかったものを書く。読めなかったことを
	// 黙ると、翻訳者は「判定していません」の理由を追えない。どれもパスを出すときは
	// ルートからの相対にする（手元の絶対パスには利用者名が入ることがある）。
	s := newTestServer(t, Options{UILang: "ja"})
	ja := s.cat.lookup("ja")
	root := s.opt.Root
	target := s.target("ja")
	working := filepath.Join(root, "Translations", "_discovered", "ja.working.csv")
	layout := filepath.Join(root, "Translations", "_discovered", "ja.layout_risks.csv")
	note := func(key string, kv ...string) string { return s.cat.T(ja, key, kv...) }

	// 何もかも読めた集計。ここから1か所ずつ崩す。
	base := func() diff.Summary {
		return diff.Summary{
			Locale: "ja", WorkingPath: working,
			OrderKeys: true, OrderLineIDs: true, OldOrder: true,
		}
	}
	cases := []struct {
		name      string
		sum       func() diff.Summary
		hasSource bool
		want      []string
		notWant   []string
	}{
		{
			name: "作業コピーを読んだ", hasSource: true,
			sum:  func() diff.Summary { sum := base(); sum.HasWorking = true; return sum },
			want: []string{note("note.working_read", "path", "Translations/_discovered/ja.working.csv")},
			notWant: []string{note("note.order_unreadable"), note("note.no_source"),
				note("note.working_none", "path", "Translations/_discovered/ja.working.csv")},
		},
		{
			name: "作業コピーはあるが読んでいない", hasSource: true,
			sum:  func() diff.Summary { sum := base(); sum.WorkingExists = true; return sum },
			want: []string{note("note.working_skipped", "path", "Translations/_discovered/ja.working.csv")},
			notWant: []string{note("note.working_read", "path", "Translations/_discovered/ja.working.csv"),
				note("note.working_none", "path", "Translations/_discovered/ja.working.csv")},
		},
		{
			name: "作業コピーが無い", hasSource: true,
			sum:  base,
			want: []string{note("note.working_none", "path", "Translations/_discovered/ja.working.csv")},
		},
		{
			name: "はみ出しの記録を読んだ", hasSource: true,
			sum: func() diff.Summary {
				sum := base()
				sum.HasLayoutRisks, sum.LayoutRisksPath = true, layout
				return sum
			},
			want: []string{note("note.layout_risks_read", "path", "Translations/_discovered/ja.layout_risks.csv")},
		},
		{
			name: "再生順のキーを読めない", hasSource: true,
			sum:  func() diff.Summary { sum := base(); sum.OrderKeys = false; return sum },
			want: []string{note("note.order_unreadable")},
		},
		{
			name: "再生順の台詞IDを読めない", hasSource: true,
			sum:  func() diff.Summary { sum := base(); sum.OrderLineIDs = false; return sum },
			want: []string{note("note.order_unreadable")},
		},
		{
			name: "原文の列が無い", hasSource: false,
			sum:  base,
			want: []string{note("note.no_source")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			notes := s.buildNotes(ja, target, tc.sum(), tc.hasSource)
			for _, want := range tc.want {
				if !hasNote(notes, want) {
					t.Errorf("断り書きに %q が無い: %q", want, notes)
				}
			}
			for _, bad := range tc.notWant {
				if hasNote(notes, bad) {
					t.Errorf("断り書きに %q が出ている: %q", bad, notes)
				}
			}
			for _, n := range notes {
				if strings.Contains(n, root) || strings.Contains(n, filepath.ToSlash(root)) {
					t.Errorf("絶対パスが出ている: %q", n)
				}
			}
		})
	}
}
