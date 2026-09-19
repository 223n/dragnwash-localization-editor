package diff

import (
	"slices"
	"testing"

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
