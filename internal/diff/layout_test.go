package diff

import (
	"errors"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
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

// TestParseLayoutRisksDuplicateColumn は、列名が重複した記録をエラーにすることを
// 確かめる。どちらの列を読むか決められないまま読むと、別の列の値で行を結び付ける。
func TestParseLayoutRisksDuplicateColumn(t *testing.T) {
	_, err := ParseLayoutRisks([]byte("source_en,source_en,ratio\na,b,1.5\n"))
	var dup *csvfile.DuplicateColumnError
	if !errors.As(err, &dup) {
		t.Fatalf("csvfile.DuplicateColumnError ではない: %v", err)
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

	t.Run("比の大きい行が先にあれば後の小さい行で置き換えない", func(t *testing.T) {
		// 並びに関わらず最大を採ること。先に大きいほうが来る並びで、後の小さい
		// 行に上書きされると、いちばんはみ出す場所を見落とす。
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderTwo,
			"Translations/ja/strings.csv": published,
			"Translations/_discovered/layout_risks.csv": layoutRisksHeader +
				layoutRow(srcHello, "こんにちは", "y", "90", "30", "3.0", "Canvas/B") +
				layoutRow(srcHello, "こんにちは", "x", "240", "180", "1.33", "Canvas/A"),
		}, true)
		rep := Compare(repo, nil)
		if f := findingOf(t, rep, CatLayoutRisk); !strings.Contains(f.Note, "3.0") {
			t.Errorf("比の大きい行を採っていない: %q", f.Note)
		}
	})

	t.Run("比が同じなら先の行を採る", func(t *testing.T) {
		// 実行ごとに報告が変わらないように、同点は先に書かれた行で決める。
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderTwo,
			"Translations/ja/strings.csv": published,
			"Translations/_discovered/layout_risks.csv": layoutRisksHeader +
				layoutRow(srcHello, "こんにちは", "x", "240", "120", "2.0", "Canvas/A") +
				layoutRow(srcHello, "こんにちは", "y", "60", "30", "2.0", "Canvas/B"),
		}, true)
		rep := Compare(repo, nil)
		f := findingOf(t, rep, CatLayoutRisk)
		if f.NoteReason.ID != reason.NoteLayoutRisk {
			t.Fatalf("注記が違う: %q", f.NoteReason.ID)
		}
		if !strings.Contains(f.Note, "x 方向") || !strings.Contains(f.Note, "240") {
			t.Errorf("先の行を採っていない: %q", f.Note)
		}
	})

	t.Run("訳が空の行には付けない", func(t *testing.T) {
		// 訳の無いまま測った記録（原文が出ていた）と、まだ訳の無い作業コピーの行は
		// 空どうしで一致してしまう。訳が無ければ短くする相手がいない。
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderTwo,
			"Translations/ja/strings.csv": published,
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold," + srcBye + ",\n",
			"Translations/_discovered/layout_risks.csv": layoutRisksHeader +
				layoutRow(srcBye, "", "x", "240", "180", "1.33", "Canvas/Label"),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatLayoutRisk]; got != 0 {
			t.Errorf("訳の無い行に付けた: got %d", got)
		}
	})

	t.Run("作業コピーに同じキーが2行あっても1件", func(t *testing.T) {
		// 画面はバッジをキー単位で付けるので、2回数えると件数とバッジが食い違う。
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderTwo,
			"Translations/ja/strings.csv": published,
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n",
			"Translations/_discovered/layout_risks.csv": layoutRisksHeader +
				layoutRow(srcHello, "こんにちは", "x", "240", "180", "1.33", "Canvas/Label"),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatLayoutRisk]; got != 1 {
			t.Errorf("件数が違う: got %d, want 1", got)
		}
	})

	t.Run("publish が捨てる作業コピーの行には付けない", func(t *testing.T) {
		// key と原文のハッシュが食い違う行は publish が捨てる。訳を短くしても
		// 公開ファイルには載らないので、先に「publish で捨てられる行」を直す。
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderTwo,
			"Translations/ja/strings.csv": published,
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyThanks + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n",
			"Translations/_discovered/layout_risks.csv": layoutRisksHeader +
				layoutRow(srcHello, "こんにちは", "x", "240", "180", "1.33", "Canvas/Label"),
		}, true)
		rep := Compare(repo, nil)
		got := counts(t, rep, "ja")
		if got[CatDropped] != 1 {
			t.Fatalf("前提が崩れている: publish で捨てられる行 = %d 件, want 1", got[CatDropped])
		}
		if got[CatLayoutRisk] != 0 {
			t.Errorf("捨てられる行に付けた: got %d", got[CatLayoutRisk])
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
