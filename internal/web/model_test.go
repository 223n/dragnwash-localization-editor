package web

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
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
	// 台詞IDだけが無いときの断りの頭。名指しの中身は TestLineIDNoteNamesWhatIsHeld が見る。
	lineIDsHead, _, _ := strings.Cut(note("note.order_line_ids_missing"), "{categories}")
	// 引き継ぎ候補を止めたことの断りの頭。理由の中身は集計ごとに変わる。
	oldHeldHead, _, _ := strings.Cut(note("note.old_order_held"), "{reason}")

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
			notWant: []string{note("note.order_unreadable"), lineIDsHead, note("note.no_source"),
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
			sum:     func() diff.Summary { sum := base(); sum.OrderKeys = false; return sum },
			want:    []string{note("note.order_unreadable")},
			notWant: []string{lineIDsHead},
		},
		{
			// キーも台詞IDも無いときは、キーが無いほうの断りだけにする。キーが無ければ
			// 台詞IDの要るカテゴリも止まっているので、そちらを並べても言うことが増えない。
			name: "再生順のキーも台詞IDも読めない", hasSource: true,
			sum: func() diff.Summary {
				sum := base()
				sum.OrderKeys, sum.OrderLineIDs = false, false
				return sum
			},
			want:    []string{note("note.order_unreadable")},
			notWant: []string{lineIDsHead},
		},
		{
			// キーは読めているので、台本から消えた行は判定できる。「台本から消えた行
			// などは判定しません」と書くと、件数の欄と食い違う。
			name: "再生順の台詞IDだけを読めない", hasSource: true,
			sum:     func() diff.Summary { sum := base(); sum.OrderLineIDs = false; return sum },
			want:    []string{lineIDsHead},
			notWant: []string{note("note.order_unreadable")},
		},
		{
			// 1つ前の再生順だけが無い。引き継ぎ候補を止めた理由はそれだけなので、
			// ここで断る。
			name: "1つ前の再生順が無い", hasSource: true,
			sum:  func() diff.Summary { sum := base(); sum.OldOrder = false; return sum },
			want: []string{oldHeldHead},
		},
		{
			// いまの再生順に台詞IDが無ければ、引き継ぎ候補はそのせいで止まっていて、
			// すぐ上の断りが名指ししている。1つ前の再生順も無いからといってここでも
			// 断ると、理由（JudgeBlockReason はいまの再生順の欠けを先に返す）まで
			// 同じ文が2度並ぶ。
			name: "台詞IDも1つ前の再生順も無い", hasSource: true,
			sum: func() diff.Summary {
				sum := base()
				sum.OrderLineIDs, sum.OldOrder = false, false
				return sum
			},
			want:    []string{lineIDsHead},
			notWant: []string{oldHeldHead},
		},
		{
			name: "再生順のキーも1つ前の再生順も無い", hasSource: true,
			sum: func() diff.Summary {
				sum := base()
				sum.OrderKeys, sum.OldOrder = false, false
				return sum
			},
			want:    []string{note("note.order_unreadable")},
			notWant: []string{oldHeldHead},
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

// TestLineIDNoteNamesWhatIsHeld は、再生順のキーは読めていて台詞IDだけが無いときの
// 断り書きが、実際に保留にしたカテゴリだけを名指しすることを見る。
//
// 台本から消えた行はキーだけで判定でき、件数の欄に数が出る。そこで「台本から消えた
// 行などは判定しません」と書くと、同じ画面の件数と食い違い、どちらを信じればよいか
// 分からなくなる（CLI の text 形式の見出しも同じ理由で書き分けた。internal/diff の
// TestWriteTextHeaderNamesWhatIsHeld）。名指しは件数の欄の「判定していません」
// （[diff.Summary.CanJudge] が false のカテゴリ）と一致させる。表の印を変えたときに、
// 断り書きだけが古い名指しのまま残らないようにするためである。
func TestLineIDNoteNamesWhatIsHeld(t *testing.T) {
	for _, lang := range []string{"ja", "en"} {
		t.Run(lang, func(t *testing.T) {
			s := newTestServer(t, Options{UILang: lang})
			cat := s.cat.lookup(lang)
			// 台詞IDのほかは何もかも読めた集計。ほかの理由で止まるカテゴリを混ぜない。
			sum := diff.Summary{
				Locale: "ja", HasWorking: true, OrderKeys: true, OrderLineIDs: false,
				OrderNorms: true, HasLayoutRisks: true, OldOrder: true,
			}
			head, _, _ := strings.Cut(s.cat.T(cat, "note.order_line_ids_missing"), "{categories}")
			got := ""
			for _, n := range s.buildNotes(cat, s.target("ja"), sum, true) {
				if strings.HasPrefix(n, head) {
					got = n
				}
			}
			if got == "" {
				t.Fatalf("台詞IDだけが無いことの断りが無い（頭 %q）", head)
			}

			// 全カテゴリは、実際の集計の件数から取る（件数には全カテゴリが入る）。
			all := s.summary("ja").Counts
			if len(all) == 0 {
				t.Fatal("見本の集計に件数が無い")
			}
			named := 0
			for c := range all {
				quoted := s.cat.T(cat, "note.category_quote", "name", s.categoryLabel(cat, c))
				in := strings.Contains(got, quoted)
				if held := !sum.CanJudge(c); in != held {
					t.Errorf("%s: 判定していない = %v なのに、断り書きで名指ししたか = %v: %q", c.ID(), held, in, got)
				}
				if in {
					named++
				}
			}
			if named == 0 {
				t.Errorf("1つも名指ししていない: %q", got)
			}
			// 台本から消えた行はキーだけで判定できる。名指ししないことは上で見たが、
			// 見本の集計が前提どおりであることもここで確かめておく。
			if !sum.CanJudge(diff.CatVanished) {
				t.Error("台本から消えた行を判定していない。集計の前提が崩れている")
			}
			if hasNote([]string{got}, s.cat.T(cat, "note.order_unreadable")) {
				t.Errorf("キーが無いときの断りと同じ文になっている: %q", got)
			}
			if lang == "en" && hasJapanese(got) {
				t.Errorf("英語の断り書きに日本語が混ざっている: %q", got)
			}
		})
	}
}

// TestLineIDCountsGiveTheLineIDReason は、再生順のキーは読めていて台詞IDだけが
// 無いときに、件数の欄の「判定していません（理由）」が、台詞IDが無いことを
// 理由にすることを見る。
//
// 理由を「再生順を読めていません」にすると、同じ件数の欄で台本から消えた行に
// 数が出ていることと食い違う（キーは読めているので判定できている）。断り書き
// （TestLineIDNoteNamesWhatIsHeld）が台詞IDのことを言っているのに、件数の欄だけが
// 再生順を丸ごと読めていないように書くことにもなる。
func TestLineIDCountsGiveTheLineIDReason(t *testing.T) {
	for _, lang := range []string{"ja", "en"} {
		t.Run(lang, func(t *testing.T) {
			s := newTestServer(t, Options{UILang: lang})
			cat := s.cat.lookup(lang)
			noLineIDsKey := "reason." + reason.JudgeOrderNoLineIDs
			want := s.cat.T(cat, noLineIDsKey)
			if want == noLineIDsKey {
				t.Fatalf("%s の目録に %s が無い", lang, noLineIDsKey)
			}
			if want == s.cat.T(cat, "reason."+reason.JudgeOrderUnreadable) {
				t.Fatalf("キーが無いときの理由と同じ文になっている: %q", want)
			}

			// 台詞IDのほかは何もかも読めた集計。ほかの理由で止まるカテゴリを混ぜない。
			// 全カテゴリは、実際の集計の件数から取る（件数には全カテゴリが入る）。
			sum := judgedSummary("ja", s.summary("ja").Counts)
			sum.OrderLineIDs = false
			counts := s.buildCounts(cat, "ja", sum, nil)

			held := 0
			for _, c := range counts {
				if c.Judged {
					continue
				}
				held++
				if c.Reason != want {
					t.Errorf("%s の理由が %q、%q を期待", c.Category, c.Reason, want)
				}
			}
			if held == 0 {
				t.Fatal("判定していないカテゴリが1つも無い。集計の前提が崩れている")
			}
			// 台本から消えた行はキーだけで判定できる。理由と食い違う相手が
			// 実際に件数の欄にあることを確かめておく。
			if !mustCount(t, counts, diff.CatVanished.ID()).Judged {
				t.Error("台本から消えた行を判定していない。集計の前提が崩れている")
			}
			if lang == "en" && hasJapanese(want) {
				t.Errorf("英語の理由に日本語が混ざっている: %q", want)
			}
		})
	}
}
