package diff

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

/*
ここの試験は、開いた引用符がファイルの終わりまで閉じないファイルを、読み込みの誤りに
せず、そのファイルに依る判定だけを「判定していません」にして続けることを固定する
（決まったことの 3）。

全体を解釈する読み手へ移す作業（docs/port-spec.md）の PR2 の前半では、全体を解釈する
読み手（csvfile.ReadPowerShell）の型付きの誤りがそのまま読み込み全体の誤りになり、
dwloc diff は終了コード2で止まり、dwloc edit は起動できなかった
（TestLoadUnclosedQuoteIsAnErrorForNow）。その前の行単位の読み方は、その物理行の終わりで
閉じたものとして何事も無く読んでいた。

見本の英文と訳はどれも架空の文である。
*/

// unclosedBase は、閉じない引用符を1か所だけ入れて試すための、どれも読める一式。
// ja は作業コピーとはみ出しの記録を持ち、de は ja に無い行を持つ。
func unclosedBase() map[string]string {
	return map[string]string{
		"data/script_order.csv": orderTwo,
		"data/level_flow.csv":   "level,dragon,weather,set_flags,end_flags\n0,Ryan,Sunny,,\n",
		"Translations/ja/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
		"Translations/de/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hallo\n" +
			keyUI + ",UI,,,UI,Start\n",
		"Translations/_discovered/ja.working.csv": workingHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n" +
			keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold," + srcBye + ",\n",
		"Translations/_discovered/layout_risks.csv": layoutRisksHeader +
			layoutRow(srcHello, "こんにちは", "x", "240", "180", "1.33", "Canvas/Label"),
	}
}

// TestUnclosedQuoteIsNotJudged は、閉じない引用符のあるファイルごとに、どのカテゴリを
// 判定しないか、何を理由に書くかを固定する。
//
// 閉じない引用符のファイルは、そのファイルの行を1つも使わない。全体を解釈して読むと、
// 引用符が開いた行から後ろ（英語の原文を含むこともある）が1つの値に崩れるので、
// 半分だけ使うと、どこまでが正しい行かを決められない。
func TestUnclosedQuoteIsNotJudged(t *testing.T) {
	all := Categories()
	working := []Category{CatUntranslated, CatCarryFrom, CatDropped, CatTagMismatch}
	compares := []Category{CatLocaleGap, CatNotPublished}

	tests := []struct {
		name string
		// file と content は、一式の中で差し替えるファイル。
		file, content string
		// line は引用符が開いた物理行。
		line int
		// held は ja と de で判定しないカテゴリと、その理由の識別子。
		held map[string]map[Category]string
		// unclosed は Report.Unclosed に入るファイル（ルートからの相対）。
		unclosed []string
	}{
		{
			// ja のカテゴリはすべて止まる。de はロケールどうしを比べるカテゴリだけが
			// 止まり、ja の名前が理由に入る。
			name: "公開ファイル",
			file: "Translations/ja/strings.csv",
			content: publishedHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"こんにちは\n" +
				keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold,さようなら\n",
			line: 2,
			held: map[string]map[Category]string{
				"ja": heldAs(all, reason.JudgePublishedUnclosed),
				"de": heldAs(compares, reason.JudgeOtherPublishedUnclosed),
			},
			unclosed: []string{"Translations/ja/strings.csv"},
		},
		{
			// 作業コピーを要るカテゴリだけが止まる。de は作業コピーを持たないので
			// 作業コピーの理由で止まる（閉じない引用符とは関係が無い）。
			name: "作業コピー",
			file: "Translations/_discovered/ja.working.csv",
			content: workingHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n" +
				keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold," + srcBye + ",\"さ\n",
			line: 3,
			held: map[string]map[Category]string{
				"ja": heldAs(working, reason.JudgeWorkingUnclosed),
				"de": heldAs(working, reason.JudgeWorkingMissing),
			},
			unclosed: []string{"Translations/_discovered/ja.working.csv"},
		},
		{
			// はみ出しの記録は作業コピーと同じフォルダーの1つのファイルで、どのロケールも
			// 同じファイルを読む。止まるのははみ出しの恐れだけで、ファイルは1回だけ数える。
			name: "はみ出しの記録",
			file: "Translations/_discovered/layout_risks.csv",
			content: layoutRisksHeader +
				"\"" + srcHello + ",こんにちは,x,240,180,1.33,Canvas/Label\n",
			line: 2,
			held: map[string]map[Category]string{
				"ja": heldAs([]Category{CatLayoutRisk}, reason.JudgeLayoutRisksUnclosed),
				"de": heldAs([]Category{CatLayoutRisk}, reason.JudgeLayoutRisksUnclosed),
			},
			unclosed: []string{"Translations/_discovered/layout_risks.csv"},
		},
		{
			// 再生順は報告全体を止める。
			name: "再生順",
			file: "data/script_order.csv",
			content: "section,phase,node,order,line_id,key,speaker,condition\n" +
				"L01 Ryan,intro,Ryan_1_intro,1,line:aaaa1111," + keyHello + ",\"Ryan,\n" +
				"L01 Ryan,intro,Ryan_1_intro,2,line:bbbb2222," + keyBye + ",Kobold,\n",
			line: 2,
			held: map[string]map[Category]string{
				"ja": heldAs(all, reason.JudgeOrderUnclosed),
				"de": heldAs(all, reason.JudgeOrderUnclosed),
			},
			unclosed: []string{"data/script_order.csv"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := unclosedBase()
			files[tt.file] = tt.content
			root := writeTree(t, files)
			repo, err := LoadWith(root, Options{Working: true, OldOrder: fixedOldOrder(orderTwo)})
			if err != nil {
				t.Fatalf("読み込みの誤りにしている: %v", err)
			}
			rep := Compare(repo, nil)

			for _, sum := range rep.Locales {
				held := tt.held[sum.Locale]
				for _, c := range all {
					why, wantHeld := held[c]
					if wantHeld && tt.name == "作業コピー" && sum.Locale == "de" {
						// de は作業コピーが無いので、閉じない引用符と関係なく止まる。
						// ここでは、ja の作業コピーの理由が de に混ざらないことを見る。
						if got := sum.JudgeBlockReason(c).ID; sum.CanJudge(c) || got != why {
							t.Errorf("de の %s: CanJudge=%v 理由=%s、%s で止まるはず", c, sum.CanJudge(c), got, why)
						}
						continue
					}
					if !wantHeld {
						if !sum.CanJudge(c) && isUnclosedReason(sum.JudgeBlockReason(c).ID) {
							t.Errorf("%s の %s を閉じない引用符のせいで止めている: %s", sum.Locale, c, sum.JudgeBlockReason(c))
						}
						continue
					}
					if sum.CanJudge(c) {
						t.Errorf("%s の %s を判定している", sum.Locale, c)
						continue
					}
					got := sum.JudgeBlockReason(c)
					if got.ID != why {
						t.Errorf("%s の %s の理由 = %s（%s）、%s を期待", sum.Locale, c, got.ID, got, why)
					}
				}
			}
			// 判定していないカテゴリの行は、1件も出さない。
			for _, f := range rep.Findings {
				if _, held := tt.held[f.Locale][f.Category]; held {
					t.Errorf("判定していない %s の %s に行がある: %+v", f.Locale, f.Category, f)
				}
			}

			var got []string
			for _, u := range rep.Unclosed {
				rel, err := filepath.Rel(root, u.Path)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, filepath.ToSlash(rel))
				if u.Line != tt.line {
					t.Errorf("%s の行 = %d、%d を期待", rel, u.Line, tt.line)
				}
			}
			if !slices.Equal(got, tt.unclosed) {
				t.Errorf("Report.Unclosed = %v、%v を期待", got, tt.unclosed)
			}
		})
	}
}

// heldAs は、cats のどれも why で止めることを表す表を作る。
func heldAs(cats []Category, why string) map[Category]string {
	out := make(map[Category]string, len(cats))
	for _, c := range cats {
		out[c] = why
	}
	return out
}

// isUnclosedReason は、閉じない引用符のせいで止めたときの理由の識別子かを返す。
func isUnclosedReason(id string) bool {
	switch id {
	case reason.JudgeOrderUnclosed, reason.JudgePublishedUnclosed, reason.JudgeWorkingUnclosed,
		reason.JudgeLayoutRisksUnclosed, reason.JudgeOtherPublishedUnclosed:
		return true
	}
	return false
}

// TestUnclosedQuoteLoadsTheRest は、閉じない引用符のファイルを読まずに、残りのファイルを
// いつもどおり読むことを見る。
func TestUnclosedQuoteLoadsTheRest(t *testing.T) {
	t.Run("作業コピー", func(t *testing.T) {
		files := unclosedBase()
		files["Translations/_discovered/ja.working.csv"] = workingHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",\"こんにちは\n"
		repo := newRepo(t, files, true)
		ja := localeNamed(t, repo, "ja")
		// 読まなかったのでも無いのでもない。WorkingExists は立ち、HasWorking は倒れる。
		if !ja.WorkingExists || ja.HasWorking || ja.WorkingUnclosed != 2 || ja.Working != nil {
			t.Errorf("作業コピーの状態 = exists %v has %v unclosed %d rows %d",
				ja.WorkingExists, ja.HasWorking, ja.WorkingUnclosed, len(ja.Working))
		}
		if len(ja.Published) != 1 || !ja.HasLayoutRisks {
			t.Errorf("ほかのファイルを読めていない: published %d layout %v", len(ja.Published), ja.HasLayoutRisks)
		}
	})
	t.Run("はみ出しの記録", func(t *testing.T) {
		files := unclosedBase()
		files["Translations/_discovered/layout_risks.csv"] = layoutRisksHeader + "\"x\n"
		repo := newRepo(t, files, true)
		ja := localeNamed(t, repo, "ja")
		// 「ありません」ではなく、あるのに読めなかった。
		if !ja.LayoutRisksExist || ja.HasLayoutRisks || ja.LayoutRisksUnclosed != 2 {
			t.Errorf("はみ出しの記録の状態 = exists %v has %v unclosed %d",
				ja.LayoutRisksExist, ja.HasLayoutRisks, ja.LayoutRisksUnclosed)
		}
		if !ja.HasWorking {
			t.Error("作業コピーを読めていない")
		}
	})
	t.Run("--no-working では作業コピーを確かめない", func(t *testing.T) {
		files := unclosedBase()
		files["Translations/_discovered/ja.working.csv"] = workingHeader + "\"x\n"
		repo := newRepo(t, files, false)
		ja := localeNamed(t, repo, "ja")
		if ja.WorkingUnclosed != 0 || ja.HasWorking || !ja.WorkingExists {
			t.Errorf("読まないと言った作業コピーを見ている: unclosed %d has %v", ja.WorkingUnclosed, ja.HasWorking)
		}
		if rep := Compare(repo, nil); len(rep.Unclosed) != 0 {
			t.Errorf("読まなかったファイルを Unclosed に入れている: %+v", rep.Unclosed)
		}
	})
	t.Run("再生順", func(t *testing.T) {
		files := unclosedBase()
		files["data/script_order.csv"] = "section,key\n\"L01,aaaaaaaaaaaaaaaa\n"
		repo := newRepo(t, files, true)
		if repo.OrderUnclosed != 2 || len(repo.Order.Entries) != 0 {
			t.Errorf("再生順の状態 = unclosed %d entries %d", repo.OrderUnclosed, len(repo.Order.Entries))
		}
		if len(repo.Locales) != 2 || len(localeNamed(t, repo, "ja").Published) != 1 {
			t.Errorf("ロケールを読めていない: %+v", repo.Locales)
		}
	})
	t.Run("列名の重複と重なれば閉じない引用符を採る", func(t *testing.T) {
		// 読み手は閉じない引用符を先に返す（列名にファイルの終わりまでが入ることが
		// あるので、先に直すべきだから）。重複の誤りで読み込み全体を止めない。
		files := unclosedBase()
		files["Translations/ja/strings.csv"] = "key,key,node,order,speaker,translation\n" +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"こんにちは\n"
		repo := newRepo(t, files, true)
		if got := localeNamed(t, repo, "ja").PublishedUnclosed; got != 2 {
			t.Errorf("PublishedUnclosed = %d、2 を期待", got)
		}
	})
}

// TestUnclosedLevelFlowDoesNotBlock は、見出しの表（data/level_flow.csv）の閉じない
// 引用符では、どの判定も止めないことを見る。このパッケージは見出しの文言を使わない。
// publish と画面の書き出しは、形の確かめで止める。
func TestUnclosedLevelFlowDoesNotBlock(t *testing.T) {
	files := unclosedBase()
	files["data/level_flow.csv"] = "level,dragon,weather,set_flags,end_flags\n0,\"Ryan,Sunny,,\n"
	repo := newRepo(t, files, true)
	if repo.OrderUnclosed != 0 || len(repo.Order.Entries) != 2 {
		t.Errorf("再生順の状態 = unclosed %d entries %d", repo.OrderUnclosed, len(repo.Order.Entries))
	}
	rep := Compare(repo, nil)
	if len(rep.Unclosed) != 0 {
		t.Errorf("Report.Unclosed = %+v、空を期待", rep.Unclosed)
	}
	for _, sum := range rep.Locales {
		for _, c := range Categories() {
			if !sum.CanJudge(c) && isUnclosedReason(sum.JudgeBlockReason(c).ID) {
				t.Errorf("%s の %s を閉じない引用符のせいで止めている", sum.Locale, c)
			}
		}
	}
}

// TestUnclosedFilesOfUnreportedLocales は、--locale で絞ったときに Report.Unclosed へ
// 入るファイルを固定する。
//
// 公開ファイルは、報告しないロケールのものも入る。報告するロケールの「他のロケールに
// あって無い行」を止めるからである。作業コピーとはみ出しの記録は、報告するロケールの
// ものだけが入る。
func TestUnclosedFilesOfUnreportedLocales(t *testing.T) {
	files := unclosedBase()
	files["Translations/ja/strings.csv"] = publishedHeader + keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"x\n"
	files["Translations/_discovered/ja.working.csv"] = workingHeader + "\"x\n"
	root := writeTree(t, files)
	repo, err := LoadWith(root, Options{Working: true})
	if err != nil {
		t.Fatal(err)
	}

	rep := Compare(repo, []string{"de"})
	if len(rep.Unclosed) != 1 || !strings.HasSuffix(filepath.ToSlash(rep.Unclosed[0].Path), "Translations/ja/strings.csv") {
		t.Errorf("Report.Unclosed = %+v、ja の公開ファイルだけを期待", rep.Unclosed)
	}
	de := rep.Locales[0]
	if de.CanJudge(CatLocaleGap) || !slices.Equal(de.OthersUnclosed, []string{"ja"}) {
		t.Errorf("de の他のロケールにあって無い行を判定している: others %v", de.OthersUnclosed)
	}
	if got := de.JudgeBlockReason(CatLocaleGap); got.Text != "ほかのロケール（ja）の公開ファイルの引用符が閉じません" {
		t.Errorf("理由 = %q", got.Text)
	}

	rep = Compare(repo, []string{"ja"})
	want := []publish.UnclosedFile{
		{Path: filepath.Join(root, "Translations", "ja", "strings.csv"), Line: 2},
		{Path: filepath.Join(root, "Translations", "_discovered", "ja.working.csv"), Line: 2},
	}
	if !slices.Equal(rep.Unclosed, want) {
		t.Errorf("Report.Unclosed = %+v、%+v を期待", rep.Unclosed, want)
	}
}

// TestWriteTextUnclosed は、text 形式で閉じない引用符をどう書くかを固定する。
//
// ファイルの見出しには、件数ではなく何行目で開いた引用符が閉じないかを書く。
// 「ハッシュ 0 行」と書くと、公開ファイルが空だと読まれる。カテゴリの行は
// 「判定していません（…）」になる。
func TestWriteTextUnclosed(t *testing.T) {
	cases := []struct {
		name, file, content string
		want                []string
		absent              []string
	}{
		{
			name:    "公開ファイル",
			file:    "Translations/ja/strings.csv",
			content: publishedHeader + keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"こんにちは\n",
			want: []string{
				"ja  Translations/ja/strings.csv   2行目で開いた引用符がファイルの終わりまで閉じないので、読めません\n",
				"    このロケールは、直すまでどのカテゴリも判定しません。\n",
				"判定していません（公開ファイルの2行目の引用符が閉じません）",
				"判定していません（ほかのロケール（ja）の公開ファイルの引用符が閉じません）",
			},
			absent: []string{"ja  Translations/ja/strings.csv   ハッシュ"},
		},
		{
			name:    "作業コピー",
			file:    "Translations/_discovered/ja.working.csv",
			content: workingHeader + keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold," + srcBye + ",\"さ\n",
			want: []string{
				"    作業コピー  Translations/_discovered/ja.working.csv   2行目で開いた引用符がファイルの終わりまで閉じないので、読めません\n",
				"                未翻訳と publish で捨てられる行などは判定しません。\n",
				"判定していません（作業コピーの2行目の引用符が閉じません）",
			},
			absent: []string{"--no-working", "ゲーム内で作業コピーを書き出すと"},
		},
		{
			name:    "はみ出しの記録",
			file:    "Translations/_discovered/layout_risks.csv",
			content: layoutRisksHeader + "\"x\n",
			want: []string{
				"    はみ出しの記録  Translations/_discovered/layout_risks.csv   2行目で開いた引用符がファイルの終わりまで閉じないので、読めません\n",
				"判定していません（はみ出しの記録の2行目の引用符が閉じません）",
			},
		},
		{
			name:    "再生順",
			file:    "data/script_order.csv",
			content: "section,key\n\"L01,aaaaaaaaaaaaaaaa\n",
			want: []string{
				"            2行目で開いた引用符がファイルの終わりまで閉じないので、再生順を読めません。\n",
				"            直すまで、どのカテゴリも判定しません。\n",
				"判定していません（再生順の2行目の引用符が閉じません）",
			},
			// 「再生順を読めていません」の見出しと重ねない。
			absent: []string{"台本から消えた行などは判定しません", "判定していません（再生順を読めていません）"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := unclosedBase()
			files[tc.file] = tc.content
			root := writeTree(t, files)
			repo, err := LoadWith(root, Options{Working: true, OldOrder: fixedOldOrder(orderTwo)})
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			if err := Compare(repo, nil).WriteText(&b, TextOptions{Root: root}); err != nil {
				t.Fatal(err)
			}
			out := b.String()
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("%q が無い:\n%s", w, out)
				}
			}
			// 出てはいけない文は ja の見出しより後ろで見る。de は作業コピーを持たない
			// ので、de の見出しには「書き出すと判定できる」が出る。
			_, ja, _ := strings.Cut(out, "\nja  ")
			for _, a := range tc.absent {
				if strings.Contains(ja, a) {
					t.Errorf("%q が出ている:\n%s", a, out)
				}
			}
		})
	}
}

// localeNamed は repo の中から名前でロケールを引く。
func localeNamed(t *testing.T, repo *Repo, name string) Locale {
	t.Helper()
	for _, loc := range repo.Locales {
		if loc.Name == name {
			return loc
		}
	}
	t.Fatalf("ロケール %s が無い", name)
	return Locale{}
}
