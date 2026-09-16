package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// テストで使う英文とそのキー。key.For を通して作るので、ハッシュを手で書かない。
var (
	srcHello   = "Hello"
	srcBye     = "Goodbye"
	srcThanks  = "Thanks"
	srcUI      = "Start"
	srcWow     = "Wow!"
	srcOptions = "Options"
	keyHello   = key.For(srcHello)
	keyBye     = key.For(srcBye)
	keyThanks  = key.For(srcThanks)
	keyUI      = key.For(srcUI)
	keyWow     = key.For(srcWow)
	keyOptions = key.For(srcOptions)
	orderCSV1  = orderFile(orderRow{"L01 Ryan", "intro", "Ryan_1_intro", "1", "line:aaaa1111", keyHello, "Ryan", ""})
	orderTwo   = orderFile(orderRow{"L01 Ryan", "intro", "Ryan_1_intro", "1", "line:aaaa1111", keyHello, "Ryan", ""}, orderRow{"L01 Ryan", "intro", "Ryan_1_intro", "2", "line:bbbb2222", keyBye, "Kobold", ""})
	orderShare = orderFile(
		orderRow{"L01 Ryan", "intro", "Ryan_1_intro", "1", "line:aaaa1111", keyHello, "Ryan", ""},
		orderRow{"Unused", "", "Start", "1", "line:cccc3333", keyThanks, "Kobold", ""},
	)
)

// orderRow は script_order.csv の1行。列は section,phase,node,order,line_id,key,speaker,condition。
type orderRow struct {
	section, phase, node, orderText, lineID, key, speaker, condition string
}

// orderFile は script_order.csv の中身を組み立てる。
func orderFile(rows ...orderRow) string {
	var b strings.Builder
	b.WriteString("section,phase,node,order,line_id,key,speaker,condition\n")
	for _, r := range rows {
		b.WriteString(strings.Join([]string{
			r.section, r.phase, r.node, r.orderText, r.lineID, r.key, r.speaker, r.condition,
		}, ","))
		b.WriteString("\n")
	}
	return b.String()
}

// newRepo は一時ディレクトリに翻訳リポジトリを作って読み込む。
// files のキーはルートからのスラッシュ区切りの相対パス。
func newRepo(t *testing.T, files map[string]string, useWorking bool) *Repo {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("ディレクトリを作れない: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("ファイルを書けない: %v", err)
		}
	}
	repo, err := Load(root, useWorking)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	return repo
}

// counts は1ロケール分の件数を取り出す。
func counts(t *testing.T, rep *Report, locale string) map[Category]int {
	t.Helper()
	for _, sum := range rep.Locales {
		if sum.Locale == locale {
			return sum.Counts
		}
	}
	t.Fatalf("ロケール %s の要約が無い", locale)
	return nil
}

func TestCompareCategories(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		useWorking bool
		report     []string
		locale     string
		want       map[Category]int
		wantStatus Status
	}{
		{
			name: "再生順にあって訳もある行は何も出ない",
			files: map[string]string{
				"data/script_order.csv": orderCSV1,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
			},
			locale:     "ja",
			want:       map[Category]int{},
			wantStatus: StatusInfo,
		},
		{
			name: "section が UI でない行は台本から消えた行",
			files: map[string]string{
				"data/script_order.csv": orderCSV1,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
					keyBye + ",L01 Ryan,Ryan_1_intro,2,Ryan,さようなら\n",
			},
			locale:     "ja",
			want:       map[Category]int{CatVanished: 1},
			wantStatus: StatusReview,
		},
		{
			name: "section が UI で speaker も UI なら由来を判定できない行",
			files: map[string]string{
				"data/script_order.csv": orderCSV1,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
					keyUI + ",UI,,,UI,はじめる\n",
			},
			locale:     "ja",
			want:       map[Category]int{CatUnknownOrigin: 1},
			wantStatus: StatusInfo,
		},
		{
			name: "section が UI で speaker が空でも由来を判定できない行",
			files: map[string]string{
				"data/script_order.csv": orderCSV1,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
					keyUI + ",UI,,,,はじめる\n",
			},
			locale:     "ja",
			want:       map[Category]int{CatUnknownOrigin: 1},
			wantStatus: StatusInfo,
		},
		{
			name: "section が UI でも話者名が残っていれば台本に無い台詞行",
			files: map[string]string{
				"data/script_order.csv": orderCSV1,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
					keyBye + ",UI,,,Conrad,さようなら\n",
			},
			locale:     "ja",
			want:       map[Category]int{CatScriptGap: 1},
			wantStatus: StatusInfo,
		},
		{
			name: "再生順にある台詞ID行は何も出ない",
			files: map[string]string{
				"data/script_order.csv": orderCSV1,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
					"line:aaaa1111,L01 Ryan,Ryan_1_intro,1,Ryan,やあ\n",
			},
			locale:     "ja",
			want:       map[Category]int{},
			wantStatus: StatusInfo,
		},
		{
			name: "再生順に無い台詞ID行は要確認",
			files: map[string]string{
				"data/script_order.csv": orderCSV1,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
					"line:dddd4444,,,,,やあ\n",
			},
			locale:     "ja",
			want:       map[Category]int{CatStrayLineID: 1},
			wantStatus: StatusReview,
		},
		{
			name: "どのロケールにも訳が無い行",
			files: map[string]string{
				"data/script_order.csv": orderTwo,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
				"Translations/de/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hallo\n",
			},
			locale:     "ja",
			want:       map[Category]int{CatNotPublished: 1},
			wantStatus: StatusInfo,
		},
		{
			name: "他のロケールにあって無い行",
			files: map[string]string{
				"data/script_order.csv": orderTwo,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
					keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold,さようなら\n",
				"Translations/de/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hallo\n",
			},
			locale:     "de",
			want:       map[Category]int{CatLocaleGap: 1},
			wantStatus: StatusTodo,
		},
		{
			name: "報告を絞っても母集合は全ロケールから作る",
			files: map[string]string{
				"data/script_order.csv": orderTwo,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
					keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold,さようなら\n",
				"Translations/de/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hallo\n",
			},
			// de だけを報告しても、ja が持っていることを根拠に「他のロケールにある」と言える。
			report:     []string{"de"},
			locale:     "de",
			want:       map[Category]int{CatLocaleGap: 1},
			wantStatus: StatusTodo,
		},
		{
			name: "作業コピーの未翻訳",
			files: map[string]string{
				"data/script_order.csv": orderTwo,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
				"Translations/_discovered/ja.working.csv": workingHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n" +
					keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold," + srcBye + ",\n",
			},
			useWorking: true,
			locale:     "ja",
			// keyBye は未翻訳の1件だけ。作業コピーが手元にあるキーは
			// 「どのロケールにも訳が無い行」から外す。両方に出すと、
			// 1行の仕事が2件に見える。
			want:       map[Category]int{CatUntranslated: 1},
			wantStatus: StatusTodo,
		},
		{
			name: "原文が未取得の行と台詞ID行は未翻訳にしない",
			files: map[string]string{
				"data/script_order.csv": orderCSV1,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
				"Translations/_discovered/ja.working.csv": workingHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n" +
					keyUI + ",UI,,,UI,,\n" +
					"line:aaaa1111,L01 Ryan,Ryan_1_intro,1,Ryan,,\n",
			},
			useWorking: true,
			locale:     "ja",
			want:       map[Category]int{},
			wantStatus: StatusInfo,
		},
		{
			name: "publish で捨てられる行",
			files: map[string]string{
				"data/script_order.csv": orderCSV1,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
				"Translations/_discovered/ja.working.csv": workingHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n" +
					"English,,,,,,勝手に足した訳\n" +
					keyThanks + ",UI,,,UI," + srcBye + ",不一致\n",
			},
			useWorking: true,
			locale:     "ja",
			want:       map[Category]int{CatDropped: 2},
			wantStatus: StatusReview,
		},
		{
			name: "--no-working では作業コピーを読まない",
			files: map[string]string{
				"data/script_order.csv": orderCSV1,
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
				"Translations/_discovered/ja.working.csv": workingHeader +
					"English,,,,,,勝手に足した訳\n",
			},
			useWorking: false,
			locale:     "ja",
			want:       map[Category]int{},
			wantStatus: StatusInfo,
		},
		{
			name: "再生順が無いと再生順を根拠にするカテゴリは判定しない",
			files: map[string]string{
				"Translations/ja/strings.csv": publishedHeader +
					keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
					keyUI + ",UI,,,UI,はじめる\n",
			},
			locale: "ja",
			// 再生順のキーが1種も読めていないので、「再生順に無い」を根拠に
			// するカテゴリは判定しない。判定すると、公開行のほぼ全部が
			// 「台本から消えた行」に化ける（実データの ja で1570件）。
			want:       map[Category]int{},
			wantStatus: StatusInfo,
		},
		{
			name: "公開ファイルが空でも読める",
			files: map[string]string{
				"data/script_order.csv":       orderCSV1,
				"Translations/ja/strings.csv": "",
			},
			locale:     "ja",
			want:       map[Category]int{CatNotPublished: 1},
			wantStatus: StatusInfo,
		},
		{
			name: "公開ファイルがヘッダーだけでも読める",
			files: map[string]string{
				"data/script_order.csv":       orderCSV1,
				"Translations/ja/strings.csv": publishedHeader,
			},
			locale:     "ja",
			want:       map[Category]int{CatNotPublished: 1},
			wantStatus: StatusInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRepo(t, tt.files, tt.useWorking)
			rep := Compare(repo, tt.report)

			got := counts(t, rep, tt.locale)
			for _, c := range categories {
				if got[c] != tt.want[c] {
					t.Errorf("%s の件数が違う: got %d, want %d", c, got[c], tt.want[c])
				}
			}
			if rep.Status() != tt.wantStatus {
				t.Errorf("重さが違う: got %s, want %s", rep.Status(), tt.wantStatus)
			}
		})
	}
}

// TestCompareEmptyLineIDIsNotReported は「再生順に line_id があるのに公開CSVに
// 台詞ID行が無い」ことを、どのカテゴリにも入れないことを固定する。
//
// ここを検出に加えると1ロケールあたり最大1839件の誤検出が一気に出る。
// 設計上いちばん壊れやすい箇所なので、規則ではなくテストで守る。
func TestCompareEmptyLineIDIsNotReported(t *testing.T) {
	repo := newRepo(t, map[string]string{
		"data/script_order.csv": orderShare,
		// 再生順には line:aaaa1111 と line:cccc3333 があるが、公開側には1行も無い。
		"Translations/ja/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
			keyThanks + ",Unused,Start,1,Kobold,ありがとう\n",
	}, false)

	rep := Compare(repo, nil)
	if len(rep.Findings) != 0 {
		t.Fatalf("空の台詞ID行を報告している: %+v", rep.Findings)
	}
	if rep.OrderLineIDs != 2 {
		t.Fatalf("台詞IDの数が違う: got %d, want 2", rep.OrderLineIDs)
	}
}

func TestCompareSummary(t *testing.T) {
	repo := newRepo(t, map[string]string{
		"data/script_order.csv": orderTwo,
		"Translations/ja/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
			"line:aaaa1111,L01 Ryan,Ryan_1_intro,1,Ryan,やあ\n" +
			"English,,,,,壊れた行\n",
		"Translations/_discovered/ja.working.csv": workingHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n" +
			keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold,,\n",
	}, true)

	rep := Compare(repo, nil)
	if len(rep.Locales) != 1 {
		t.Fatalf("ロケール数が違う: %d", len(rep.Locales))
	}
	sum := rep.Locales[0]
	tests := []struct {
		name      string
		got, want int
	}{
		{"ハッシュ行", sum.HashRows, 1},
		{"台詞ID行", sum.LineRows, 1},
		{"形の分からない行", sum.BrokenRows, 1},
		{"作業コピーの行", sum.WorkingRows, 2},
		{"原文が未取得の行", sum.SourceMissing, 1},
		{"再生順の行", rep.OrderRows, 2},
		{"再生順のキー", rep.OrderKeys, 2},
		{"再生順の台詞ID", rep.OrderLineIDs, 2},
		{"読んだロケール", rep.ReadLocales, 1},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s の数が違う: got %d, want %d", tt.name, tt.got, tt.want)
		}
	}
	if !sum.HasWorking {
		t.Error("作業コピーを読んだことになっていない")
	}
}

// TestCompareFindingOrder は Finding の並びがロケール順 → カテゴリ順 → キー順で
// あることを確かめる。並びが実行ごとに変わると、CSV の差分が毎回出てしまう。
func TestCompareFindingOrder(t *testing.T) {
	repo := newRepo(t, map[string]string{
		"data/script_order.csv": orderCSV1,
		"Translations/ja/strings.csv": publishedHeader +
			keyBye + ",L01 Ryan,Ryan_1_intro,2,Ryan,さようなら\n" +
			keyThanks + ",L01 Ryan,Ryan_1_intro,3,Ryan,ありがとう\n" +
			keyUI + ",UI,,,UI,はじめる\n",
		"Translations/de/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hallo\n",
	}, false)

	rep := Compare(repo, nil)
	var seen []string
	for _, f := range rep.Findings {
		seen = append(seen, f.Locale+"/"+f.Category.ID()+"/"+f.Key)
	}
	if len(seen) == 0 {
		t.Fatal("報告が1件も無い")
	}
	for i := 1; i < len(seen); i++ {
		prev, cur := rep.Findings[i-1], rep.Findings[i]
		if prev.Locale != cur.Locale {
			continue
		}
		if prev.Category > cur.Category {
			t.Fatalf("カテゴリの並びが崩れている: %v", seen)
		}
		if prev.Category == cur.Category && prev.Key > cur.Key {
			t.Fatalf("キーの並びが崩れている: %v", seen)
		}
	}
	// ロケールはディレクトリ名順（de が先）。
	if rep.Findings[0].Locale != "de" {
		t.Errorf("ロケールの並びが違う: got %s, want de", rep.Findings[0].Locale)
	}
}

func TestLoad(t *testing.T) {
	t.Run("Translations が無ければエラー", func(t *testing.T) {
		if _, err := Load(t.TempDir(), true); err == nil {
			t.Fatal("エラーにならない")
		}
	})

	t.Run("再生順が無くてもエラーにしない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"Translations/ja/strings.csv": publishedHeader,
		}, true)
		if len(repo.Order.Entries) != 0 {
			t.Errorf("再生順が空ではない: %d", len(repo.Order.Entries))
		}
		if !strings.HasSuffix(filepath.ToSlash(repo.OrderPath), "data/script_order.csv") {
			t.Errorf("再生順のパスが違う: %s", repo.OrderPath)
		}
	})

	t.Run("作業コピーが無くてもパスは答える", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"Translations/ja/strings.csv": publishedHeader,
		}, true)
		loc := repo.Locales[0]
		if loc.HasWorking {
			t.Error("作業コピーがあることになっている")
		}
		want := filepath.Join(repo.Root, publish.TranslationsDir, publish.DiscoveredDir, "ja"+publish.WorkingSuffix)
		if loc.WorkingPath != want {
			t.Errorf("作業コピーのパスが違う:\n got  %s\n want %s", loc.WorkingPath, want)
		}
	})

	t.Run("公開ファイルが無くても作業コピーだけで読める", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"Translations/ja/.keep":                   "",
			"Translations/_discovered/ja.working.csv": workingHeader + keyHello + ",,,,," + srcHello + ",\n",
		}, true)
		if len(repo.Locales) != 1 {
			t.Fatalf("ロケール数が違う: %d", len(repo.Locales))
		}
		loc := repo.Locales[0]
		if len(loc.Published) != 0 {
			t.Errorf("公開行があることになっている: %d", len(loc.Published))
		}
		if !loc.HasWorking || len(loc.Working) != 1 {
			t.Errorf("作業コピーを読めていない: %+v", loc)
		}
	})

	t.Run("列名の重複はエラー", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, publish.TranslationsDir, "ja", publish.StringsFile)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("key,key,translation\na,b,c\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(root, true); err == nil {
			t.Fatal("エラーにならない")
		}
	})
}
