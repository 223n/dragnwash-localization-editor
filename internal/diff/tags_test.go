package diff

import (
	"slices"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

func TestCheckTags(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		unclosed []string
		unopened []string
	}{
		{name: "タグが無い", text: "こんにちは"},
		{name: "閉じている", text: "<i>強調</i>"},
		{name: "閉じていない", text: "<i>強調", unclosed: []string{"<i>"}},
		{name: "終了だけ", text: "強調</i>", unopened: []string{"</i>"}},
		{name: "両方", text: "<b>太字</i>", unclosed: []string{"<b>"}, unopened: []string{"</i>"}},
		// 実データの原文にある書き方。1行の終わりまで効かせるので終了タグが無い。
		{name: "値つきのタグ", text: "<size=80%>小さい", unclosed: []string{"<size=80%>"}},
		{name: "値つきを閉じる", text: "<size=80%>小さい</size>"},
		{name: "値に空白", text: `<gradient="yellow - orange">虹</gradient>`},
		// 入れ違いは通す。TextMeshPro は入れ違いを直して描く。
		{name: "入れ違い", text: "<b><i>両方</b></i>"},
		{name: "大文字小文字", text: "<I>強調</i>"},
		{name: "自分で閉じる", text: "1行目<br/>2行目"},
		{name: "同じ名前を2回", text: "<i>a</i><i>b", unclosed: []string{"<i>"}},
		{name: "綴りは書かれたまま", text: "<size=60%>a<size=80%>b</size>", unclosed: []string{"<size=60%>"}},
		{name: "不等号だけ", text: "a < b > c"},
		{name: "名前が英字で始まらない", text: "<1>数字</1>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckTags(tt.text)
			if !slices.Equal(got.Unclosed, tt.unclosed) {
				t.Errorf("Unclosed: got %q, want %q", got.Unclosed, tt.unclosed)
			}
			if !slices.Equal(got.Unopened, tt.unopened) {
				t.Errorf("Unopened: got %q, want %q", got.Unopened, tt.unopened)
			}
			if got.Balanced() != (len(tt.unclosed) == 0 && len(tt.unopened) == 0) {
				t.Errorf("Balanced が %v", got.Balanced())
			}
		})
	}
}

func TestTagBalanceNote(t *testing.T) {
	tests := []struct {
		name string
		b    TagBalance
		id   string
		text string
	}{
		{name: "そろっている", b: TagBalance{}, id: "", text: ""},
		{
			name: "閉じていない", b: TagBalance{Unclosed: []string{"<i>", "<size=80%>"}},
			id: reason.NoteTagUnclosed, text: "閉じていないタグ: <i> <size=80%>",
		},
		{
			name: "終了だけ", b: TagBalance{Unopened: []string{"</b>"}},
			id: reason.NoteTagUnopened, text: "開始の無い終了タグ: </b>",
		},
		{
			name: "両方", b: TagBalance{Unclosed: []string{"<i>"}, Unopened: []string{"</b>"}},
			id: reason.NoteTagUnclosedUnopened, text: "閉じていないタグ: <i>／開始の無い終了タグ: </b>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.b.Note()
			if got.ID != tt.id || got.Text != tt.text {
				t.Errorf("got %q / %q, want %q / %q", got.ID, got.Text, tt.id, tt.text)
			}
		})
	}
}

// TestCompareTagUnbalanced は、タグの開閉がそろわない行がカテゴリとして出ること、
// 見る行が publish の入力になる側（作業コピーがあればそれ、無ければ公開ファイル）
// であることを見る。
func TestCompareTagUnbalanced(t *testing.T) {
	t.Run("公開ファイルだけのとき", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv": orderTwo,
			"Translations/ja/strings.csv": publishedHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,<i>こんにちは\n" +
				keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold,<b>さようなら</b>\n",
		}, false)
		rep := Compare(repo, nil)

		got := counts(t, rep, "ja")
		if got[CatTagUnbalanced] != 1 {
			t.Fatalf("件数が違う: got %d, want 1", got[CatTagUnbalanced])
		}
		if rep.Status() != StatusInfo {
			t.Errorf("参考のカテゴリなので終了コードに効かないはず: got %s", rep.Status())
		}
		f := findingOf(t, rep, CatTagUnbalanced)
		if f.Key != keyHello {
			t.Errorf("キーが違う: got %s, want %s", f.Key, keyHello)
		}
		if f.Note != "閉じていないタグ: <i>" || f.NoteReason.ID != reason.NoteTagUnclosed {
			t.Errorf("注記が違う: %q / %q", f.Note, f.NoteReason.ID)
		}
		// 位置は公開行から写す。
		if f.Section != "L01 Ryan" || f.Speaker != "Ryan" {
			t.Errorf("位置が写っていない: %+v", f)
		}
	})

	t.Run("作業コピーがあるときはそちらを見る", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv": orderTwo,
			// 公開ファイル側は閉じていないが、作業コピーでは直っている。
			"Translations/ja/strings.csv": publishedHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,<i>こんにちは\n",
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",<i>こんにちは</i>\n" +
				keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold," + srcBye + ",さようなら</b>\n",
		}, true)
		rep := Compare(repo, nil)

		got := counts(t, rep, "ja")
		if got[CatTagUnbalanced] != 1 {
			t.Fatalf("件数が違う: got %d, want 1", got[CatTagUnbalanced])
		}
		f := findingOf(t, rep, CatTagUnbalanced)
		if f.Key != keyBye {
			t.Errorf("作業コピーの行を見ていない: got %s, want %s", f.Key, keyBye)
		}
		if f.NoteReason.ID != reason.NoteTagUnopened {
			t.Errorf("注記が違う: %q", f.NoteReason.ID)
		}
	})

	t.Run("作業コピーで同じキーが2行あっても1件", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderCSV1,
			"Translations/ja/strings.csv": publishedHeader,
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",<i>a\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",<b>b\n",
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatTagUnbalanced]; got != 1 {
			t.Errorf("件数が違う: got %d, want 1", got)
		}
	})

	t.Run("訳が空の行は見ない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderCSV1,
			"Translations/ja/strings.csv": publishedHeader,
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,<i>" + srcHello + ",\n",
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatTagUnbalanced]; got != 0 {
			t.Errorf("原文のタグを訳のものとして数えている: got %d", got)
		}
	})

	t.Run("公開ファイルの形の分からない行は見ない", func(t *testing.T) {
		// 形の分からない行は internal/validate が拾う。どの列が訳なのか決められない
		// 行の中身をタグの話として出すと、直す場所を取り違える。
		repo := newRepo(t, map[string]string{
			"data/script_order.csv": orderCSV1,
			"Translations/ja/strings.csv": publishedHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
				"English,,,,,<i>勝手に足した訳\n",
		}, false)
		rep := Compare(repo, nil)
		if rep.Locales[0].BrokenRows != 1 {
			t.Fatalf("前提が崩れている: 形の分からない行 = %d", rep.Locales[0].BrokenRows)
		}
		if got := counts(t, rep, "ja")[CatTagUnbalanced]; got != 0 {
			t.Errorf("形の分からない行を見ている: got %d", got)
		}
	})
}

// findingOf はそのカテゴリの最初の Finding を返す。無ければ止める。
func findingOf(t *testing.T, rep *Report, c Category) Finding {
	t.Helper()
	for _, f := range rep.Findings {
		if f.Category == c {
			return f
		}
	}
	t.Fatalf("%s の Finding が無い", c)
	return Finding{}
}

func TestCompareTags(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		trans   string
		missing []string
		extra   []string
	}{
		{name: "どちらもタグが無い", source: "Hello", trans: "こんにちは"},
		{name: "そろっている", source: "<i>Hello</i>", trans: "<i>こんにちは</i>"},
		// TextMeshPro は入れ違いを直して描くので、並びは違いにしない。
		{name: "並びが違うだけ", source: "<b><i>a</i></b>", trans: "<i><b>あ</b></i>"},
		{name: "訳に無い", source: "<i>Hello</i>", trans: "こんにちは",
			missing: []string{"<i>", "</i>"}},
		{name: "訳に余る", source: "Hello", trans: "<i>こんにちは</i>",
			extra: []string{"<i>", "</i>"}},
		{name: "値を書き換えた", source: "<size=70%>hint", trans: "<size=60%>ヒント",
			missing: []string{"<size=70%>"}, extra: []string{"<size=60%>"}},
		{name: "数が足りない", source: "<i>a</i><i>b</i>", trans: "<i>あb</i>",
			missing: []string{"<i>", "</i>"}},
		{name: "大文字小文字は同じタグ", source: "<I>a</I>", trans: "<i>あ</i>"},
		{name: "閉じ括弧の前の空白は同じタグ", source: "<i >a</i>", trans: "<i>あ</i>"},
		{name: "値の大文字小文字も同じタグ", source: `<gradient="Gold">a</gradient>`,
			trans: `<gradient="gold">あ</gradient>`},
		{name: "自分で閉じるタグも数える", source: "a<br/>b", trans: "あb",
			missing: []string{"<br/>"}},
		{name: "綴りは書かれたまま返す", source: "<SIZE=70%>a", trans: "あ",
			missing: []string{"<SIZE=70%>"}},
		// 実データの原文にある書き方。開閉はそろっていないが、訳が同じ書き方なら
		// 構成はそろっている（そちらは [CheckTags] の担当）。
		{name: "原文が閉じずに使うタグ", source: "<size=80%>hint", trans: "<size=80%>ヒント"},
		{name: "タグに見えないもの", source: "a < b", trans: "あ < い"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareTags(tt.source, tt.trans)
			if !slices.Equal(got.Missing, tt.missing) {
				t.Errorf("Missing: got %q, want %q", got.Missing, tt.missing)
			}
			if !slices.Equal(got.Extra, tt.extra) {
				t.Errorf("Extra: got %q, want %q", got.Extra, tt.extra)
			}
			if got.Same() != (len(tt.missing) == 0 && len(tt.extra) == 0) {
				t.Errorf("Same が %v", got.Same())
			}
		})
	}
}

func TestTagDiffNote(t *testing.T) {
	tests := []struct {
		name string
		d    TagDiff
		id   string
		text string
	}{
		{name: "そろっている", d: TagDiff{}, id: "", text: ""},
		{
			name: "訳に無い", d: TagDiff{Missing: []string{"<i>", "</i>"}},
			id: reason.NoteTagMissing, text: "原文にあって訳に無いタグ: <i> </i>",
		},
		{
			name: "訳に余る", d: TagDiff{Extra: []string{"<b>"}},
			id: reason.NoteTagExtra, text: "訳にあって原文に無いタグ: <b>",
		},
		{
			name: "両方", d: TagDiff{Missing: []string{"<size=70%>"}, Extra: []string{"<size=60%>"}},
			id:   reason.NoteTagMissingExtra,
			text: "原文にあって訳に無いタグ: <size=70%>／訳にあって原文に無いタグ: <size=60%>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.d.Note()
			if got.ID != tt.id || got.Text != tt.text {
				t.Errorf("got %q / %q, want %q / %q", got.ID, got.Text, tt.id, tt.text)
			}
		})
	}
}

// タグを含む原文と、そのキー。CSVに直接書くので引用符とカンマは使わない。
var (
	srcEmph = "<i>Wonderful!</i>"
	srcHint = "<size=70%>(hint)"
	keyEmph = key.For(srcEmph)
	keyHint = key.For(srcHint)
)

// TestCompareTagMismatch は、原文とタグの構成が違う行がカテゴリとして出ること、
// 原文が要るので作業コピーが無ければ判定しないことを見る。
func TestCompareTagMismatch(t *testing.T) {
	t.Run("作業コピーが無いときは判定しない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv": orderCSV1,
			"Translations/ja/strings.csv": publishedHeader +
				keyEmph + ",L01 Ryan,Ryan_1_intro,1,Ryan,すばらしい\n",
		}, false)
		rep := Compare(repo, nil)

		if got := counts(t, rep, "ja")[CatTagMismatch]; got != 0 {
			t.Errorf("公開ファイルには原文が無いのに判定した: got %d", got)
		}
		if rep.Locales[0].JudgeBlockReason(CatTagMismatch).Empty() {
			t.Error("判定できない理由が空。0件と区別が付かない")
		}
	})

	t.Run("原文にあるタグが訳に無い", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderCSV1,
			"Translations/ja/strings.csv": publishedHeader,
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyEmph + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcEmph + ",すばらしい\n",
		}, true)
		rep := Compare(repo, nil)

		if got := counts(t, rep, "ja")[CatTagMismatch]; got != 1 {
			t.Fatalf("件数が違う: got %d, want 1", got)
		}
		if rep.Status() != StatusReview {
			t.Errorf("要確認のはず: got %s", rep.Status())
		}
		f := findingOf(t, rep, CatTagMismatch)
		if f.Key != keyEmph {
			t.Errorf("キーが違う: got %s, want %s", f.Key, keyEmph)
		}
		if f.Note != "原文にあって訳に無いタグ: <i> </i>" || f.NoteReason.ID != reason.NoteTagMissing {
			t.Errorf("注記が違う: %q / %q", f.Note, f.NoteReason.ID)
		}
		if f.Section != "L01 Ryan" || f.Speaker != "Ryan" {
			t.Errorf("位置が写っていない: %+v", f)
		}
	})

	t.Run("値を書き換えた行は両方に出る", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderCSV1,
			"Translations/ja/strings.csv": publishedHeader,
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyHint + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHint + ",<size=60%>（ヒント）\n",
		}, true)
		rep := Compare(repo, nil)

		if got := counts(t, rep, "ja")[CatTagMismatch]; got != 1 {
			t.Fatalf("件数が違う: got %d, want 1", got)
		}
		f := findingOf(t, rep, CatTagMismatch)
		if f.NoteReason.ID != reason.NoteTagMissingExtra {
			t.Errorf("注記が違う: %q", f.NoteReason.ID)
		}
	})

	t.Run("構成がそろっていれば出ない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderCSV1,
			"Translations/ja/strings.csv": publishedHeader,
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyEmph + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcEmph + ",<i>すばらしい</i>\n",
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatTagMismatch]; got != 0 {
			t.Errorf("原文どおりの訳で当たった: got %d", got)
		}
	})

	t.Run("訳が空の行は見ない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderCSV1,
			"Translations/ja/strings.csv": publishedHeader,
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyEmph + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcEmph + ",\n",
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatTagMismatch]; got != 0 {
			t.Errorf("未翻訳の行をタグの話として数えた: got %d", got)
		}
		if got := counts(t, rep, "ja")[CatUntranslated]; got != 1 {
			t.Errorf("未翻訳として出ていない: got %d", got)
		}
	})

	t.Run("原文が未取得の行は見ない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderCSV1,
			"Translations/ja/strings.csv": publishedHeader,
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,,<i>こんにちは</i>\n",
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatTagMismatch]; got != 0 {
			t.Errorf("原文の無い行で当たった: got %d", got)
		}
	})

	t.Run("同じキーが2行あっても1件", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderCSV1,
			"Translations/ja/strings.csv": publishedHeader,
			"Translations/_discovered/ja.working.csv": workingHeader +
				keyEmph + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcEmph + ",すばらしい\n" +
				keyEmph + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcEmph + ",すごい\n",
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatTagMismatch]; got != 1 {
			t.Errorf("件数が違う: got %d, want 1", got)
		}
	})
}
