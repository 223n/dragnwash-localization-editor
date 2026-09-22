package diff

import (
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// layoutRisksHeader はゲームが書く layout_risks.csv のヘッダー。
const layoutRisksHeader = "source_en,translation,axis,required_px,available_px,ratio,object_path\n"

// layoutRow は layout_risks.csv の1行を組み立てる。
func layoutRow(source, translation, axis, required, available, ratio, path string) string {
	return strings.Join([]string{source, translation, axis, required, available, ratio, path}, ",") + "\n"
}

func TestParseLayoutRisks(t *testing.T) {
	data := []byte(layoutRisksHeader +
		layoutRow(srcHello, "こんにちは", "x", "240", "180", "1.33", "Canvas/Label") +
		// 原文が空の行は捨てる。どの行の話か決められない。
		layoutRow("", "訳だけ", "x", "10", "10", "1.0", "Canvas/Other") +
		// 比が数として読めない行も残す。読めなかったことは Ratio 0 で持つ。
		layoutRow(srcBye, "さようなら", "y", "50", "40", "n/a", "Canvas/Label2"))

	risks, err := ParseLayoutRisks(data)
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	if len(risks) != 2 {
		t.Fatalf("%d 件。2件を期待", len(risks))
	}
	first := risks[0]
	if first.SourceEn != srcHello || first.Translation != "こんにちは" {
		t.Errorf("1件目が違う: %+v", first)
	}
	if first.Axis != "x" || first.RequiredPx != "240" || first.AvailablePx != "180" {
		t.Errorf("大きさの列が違う: %+v", first)
	}
	if first.RatioText != "1.33" || first.Ratio != 1.33 {
		t.Errorf("比が違う: %q / %v", first.RatioText, first.Ratio)
	}
	if first.ObjectPath != "Canvas/Label" {
		t.Errorf("場所が違う: %q", first.ObjectPath)
	}
	if risks[1].RatioText != "n/a" || risks[1].Ratio != 0 {
		t.Errorf("読めない比の扱いが違う: %q / %v", risks[1].RatioText, risks[1].Ratio)
	}
}

func TestCompareLayoutRisk(t *testing.T) {
	published := publishedHeader +
		keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
		keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold,さようなら\n"

	t.Run("測ったときの訳と同じ行に付く", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderTwo,
			"Translations/ja/strings.csv": published,
			"Translations/_discovered/layout_risks.csv": layoutRisksHeader +
				layoutRow(srcHello, "こんにちは", "x", "240", "180", "1.33", "Canvas/Label"),
		}, true)
		rep := Compare(repo, nil)

		if got := counts(t, rep, "ja")[CatLayoutRisk]; got != 1 {
			t.Fatalf("件数が違う: got %d, want 1", got)
		}
		if rep.Status() != StatusReview {
			t.Errorf("要確認のはず: got %s", rep.Status())
		}
		f := findingOf(t, rep, CatLayoutRisk)
		if f.Key != keyHello {
			t.Errorf("キーが違う: got %s, want %s", f.Key, keyHello)
		}
		if f.NoteReason.ID != reason.NoteLayoutRisk {
			t.Errorf("注記が違う: %q", f.NoteReason.ID)
		}
		for _, want := range []string{"1.33", "240", "180"} {
			if !strings.Contains(f.Note, want) {
				t.Errorf("%q が注記に無い: %q", want, f.Note)
			}
		}
	})

	t.Run("訳が違う行には付けない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderTwo,
			"Translations/ja/strings.csv": published,
			// 別の言語で測った結果（訳が ja のものと違う）。
			"Translations/_discovered/layout_risks.csv": layoutRisksHeader +
				layoutRow(srcHello, "Hallo", "x", "240", "180", "1.33", "Canvas/Label"),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatLayoutRisk]; got != 0 {
			t.Errorf("別の言語の結果を付けた: got %d", got)
		}
	})

	t.Run("同じキーに複数あれば比のいちばん大きい行", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderTwo,
			"Translations/ja/strings.csv": published,
			"Translations/_discovered/layout_risks.csv": layoutRisksHeader +
				layoutRow(srcHello, "こんにちは", "x", "240", "180", "1.33", "Canvas/A") +
				layoutRow(srcHello, "こんにちは", "y", "90", "30", "3.0", "Canvas/B"),
		}, true)
		rep := Compare(repo, nil)

		if got := counts(t, rep, "ja")[CatLayoutRisk]; got != 1 {
			t.Fatalf("件数が違う: got %d, want 1", got)
		}
		if f := findingOf(t, rep, CatLayoutRisk); !strings.Contains(f.Note, "3.0") {
			t.Errorf("比の大きい行を採っていない: %q", f.Note)
		}
	})

	t.Run("作業コピーがあるときはそちらの訳と比べる", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderTwo,
			"Translations/ja/strings.csv": published,
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",みじかく\n",
			// 測ったのは作業コピーの訳。公開ファイルの訳とは違う。
			"Translations/_discovered/layout_risks.csv": layoutRisksHeader +
				layoutRow(srcHello, "みじかく", "x", "240", "180", "1.33", "Canvas/Label"),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatLayoutRisk]; got != 1 {
			t.Errorf("作業コピーの訳と比べていない: got %d", got)
		}
	})

	t.Run("記録が無ければ判定しない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderTwo,
			"Translations/ja/strings.csv": published,
		}, true)
		rep := Compare(repo, nil)

		sum := rep.Locales[0]
		if sum.CanJudge(CatLayoutRisk) {
			t.Error("記録が無いのに判定できることになっている")
		}
		if why := sum.JudgeBlockReason(CatLayoutRisk); why.ID != reason.JudgeNoLayoutRisks {
			t.Errorf("理由が違う: %q (%q)", why.ID, why.Text)
		}
	})

	t.Run("記録が空なら0件と言える", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":                     orderTwo,
			"Translations/ja/strings.csv":               published,
			"Translations/_discovered/layout_risks.csv": layoutRisksHeader,
		}, true)
		rep := Compare(repo, nil)

		sum := rep.Locales[0]
		if !sum.CanJudge(CatLayoutRisk) {
			t.Error("記録を読めているのに判定できないことになっている")
		}
		if got := sum.Counts[CatLayoutRisk]; got != 0 {
			t.Errorf("件数が違う: got %d, want 0", got)
		}
	})

	t.Run("--no-working では読まない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderTwo,
			"Translations/ja/strings.csv": published,
			"Translations/_discovered/layout_risks.csv": layoutRisksHeader +
				layoutRow(srcHello, "こんにちは", "x", "240", "180", "1.33", "Canvas/Label"),
		}, false)
		rep := Compare(repo, nil)

		sum := rep.Locales[0]
		if sum.HasLayoutRisks {
			t.Error("--no-working で読んでいる")
		}
		if !sum.LayoutRisksExist {
			t.Error("ファイルがあることを見ていない")
		}
		if why := sum.JudgeBlockReason(CatLayoutRisk); why.ID != reason.JudgeLayoutRisksNotRead {
			t.Errorf("理由が違う: %q (%q)", why.ID, why.Text)
		}
	})

	t.Run("公開ファイルに無いキーの記録は出ない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderTwo,
			"Translations/ja/strings.csv": published,
			"Translations/_discovered/layout_risks.csv": layoutRisksHeader +
				layoutRow(srcThanks, "ありがとう", "x", "240", "180", "1.33", "Canvas/Label"),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatLayoutRisk]; got != 0 {
			t.Errorf("行に結び付かない記録を数えた: got %d", got)
		}
		// 念のため、キーそのものは公開ファイルに無いことを確かめる。
		if strings.Contains(published, key.For(srcThanks)) {
			t.Fatal("見本の作り方が間違っている")
		}
	})
}
