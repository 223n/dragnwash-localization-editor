package diff

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/order"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
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
//
// 旧再生順の取り方は既定（git）のまま。一時ディレクトリは git リポジトリでは
// ないので、引き継ぎ候補は「判定していません」になる。旧再生順が要るテストは
// [newRepoWith] に [fixedOldOrder] を渡すこと。
func newRepo(t *testing.T, files map[string]string, useWorking bool) *Repo {
	t.Helper()
	return newRepoWith(t, files, Options{Working: useWorking})
}

// newRepoWith は [newRepo] と同じものを、指定を変えて読み込む。
func newRepoWith(t *testing.T, files map[string]string, opt Options) *Repo {
	t.Helper()
	repo, err := LoadWith(writeTree(t, files), opt)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	return repo
}

// writeTree は一時ディレクトリに files を書き、そのルートを返す。読み込みはしない。
// 読み込みが失敗することを確かめるテストのために [newRepoWith] から分けてある。
func writeTree(t *testing.T, files map[string]string) string {
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
	return root
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

	t.Run("再生順の列名の重複はエラー", func(t *testing.T) {
		// 再生順を読めないまま進めると、「再生順に無い」を根拠にするカテゴリが
		// 全部止まる。黙って0行として扱わず、読めないことを呼び出し側へ返す。
		root := writeTree(t, map[string]string{
			"data/script_order.csv":       "section,key,key\nL01 Ryan," + keyHello + "," + keyBye + "\n",
			"Translations/ja/strings.csv": publishedHeader,
		})
		_, err := Load(root, true)
		if err == nil {
			t.Fatal("エラーにならない")
		}
		var dup *csvfile.DuplicateColumnError
		if !errors.As(err, &dup) {
			t.Errorf("列名の重複として伝わっていない: %v", err)
		}
		if !strings.Contains(err.Error(), "再生順のデータを読めません") {
			t.Errorf("何を読めなかったかが文面に無い: %v", err)
		}
	})

	t.Run("作業コピーの列名の重複はファイルの場所を添えて返す", func(t *testing.T) {
		// パスを文面に埋めず FileError で持ち回ることを確かめる。cmd/dwloc は
		// これを取り出してルートからの相対パスに直す。手元の絶対パスには
		// 利用者名が入ることがあり、CIのログへそのまま出ると漏れる。
		root := writeTree(t, map[string]string{
			"data/script_order.csv":                   orderCSV1,
			"Translations/ja/strings.csv":             publishedHeader,
			"Translations/_discovered/ja.working.csv": "key,key,translation\na,b,c\n",
		})
		_, err := Load(root, true)
		var fileErr *FileError
		if !errors.As(err, &fileErr) {
			t.Fatalf("FileError ではない: %v", err)
		}
		want := filepath.Join(root, publish.TranslationsDir, publish.DiscoveredDir, "ja"+publish.WorkingSuffix)
		if fileErr.Path != want {
			t.Errorf("場所が違う:\n got  %s\n want %s", fileErr.Path, want)
		}
		// Unwrap で元の誤りまで辿れること。cmd/dwloc は種類を見て文面を選ぶ。
		var dup *csvfile.DuplicateColumnError
		if !errors.As(err, &dup) {
			t.Errorf("元の誤りまで辿れない: %v", err)
		}
		if got, wantText := fileErr.Error(), filepath.ToSlash(want)+": "+fileErr.Err.Error(); got != wantText {
			t.Errorf("文面が違う:\n got  %s\n want %s", got, wantText)
		}
	})

	t.Run("--no-working なら壊れた作業コピーで止まらない", func(t *testing.T) {
		// --no-working は「公開ファイルだけで何が言えるか」を見るための指定。
		// 読まないと言ったファイルが壊れているせいで、報告そのものが出なくなっては困る。
		root := writeTree(t, map[string]string{
			"data/script_order.csv":                   orderCSV1,
			"Translations/ja/strings.csv":             publishedHeader,
			"Translations/_discovered/ja.working.csv": "key,key,translation\na,b,c\n",
		})
		repo, err := Load(root, false)
		if err != nil {
			t.Fatalf("読み込みに失敗した: %v", err)
		}
		loc := repo.Locales[0]
		if !loc.WorkingExists || loc.HasWorking {
			t.Errorf("作業コピーの有無の扱いが違う: exists=%v read=%v", loc.WorkingExists, loc.HasWorking)
		}
	})

	t.Run("はみ出しの記録の列名の重複はファイルの場所を添えて返す", func(t *testing.T) {
		root := writeTree(t, map[string]string{
			"data/script_order.csv":                     orderCSV1,
			"Translations/ja/strings.csv":               publishedHeader,
			"Translations/_discovered/layout_risks.csv": "source_en,source_en,ratio\na,b,1.5\n",
		})
		_, err := Load(root, true)
		var fileErr *FileError
		if !errors.As(err, &fileErr) {
			t.Fatalf("FileError ではない: %v", err)
		}
		want := filepath.Join(root, publish.TranslationsDir, publish.DiscoveredDir, LayoutRisksFile)
		if fileErr.Path != want {
			t.Errorf("場所が違う:\n got  %s\n want %s", fileErr.Path, want)
		}
	})

	t.Run("--no-working なら壊れたはみ出しの記録で止まらない", func(t *testing.T) {
		// はみ出しの記録はゲームが測った値で、--no-working の外にある。
		// 見るのはファイルの有無だけ（「ありません」と「読みませんでした」を
		// 書き分けるため）なので、中身が壊れていても読み込みは止めない。
		root := writeTree(t, map[string]string{
			"data/script_order.csv":                     orderCSV1,
			"Translations/ja/strings.csv":               publishedHeader,
			"Translations/_discovered/layout_risks.csv": "source_en,source_en,ratio\na,b,1.5\n",
		})
		repo, err := Load(root, false)
		if err != nil {
			t.Fatalf("読まないはずの記録が壊れていて止まった: %v", err)
		}
		loc := repo.Locales[0]
		if !loc.LayoutRisksExist {
			t.Error("記録があることを見ていない")
		}
		if loc.HasLayoutRisks || len(loc.LayoutRisks) != 0 {
			t.Errorf("--no-working で記録を読んでいる: %+v", loc.LayoutRisks)
		}
		rep := Compare(repo, nil)
		if why := rep.Locales[0].JudgeBlockReason(CatLayoutRisk); why.ID != reason.JudgeLayoutRisksNotRead {
			t.Errorf("理由が違う: %q (%q)", why.ID, why.Text)
		}
	})

	t.Run("Translations 直下のファイルはロケールにしない", func(t *testing.T) {
		// 実データの Translations/ignore.txt がこれにあたる。publish も対象に
		// しないので、「訳が1件もないロケール」として名前を出すと嘘になる。
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       orderCSV1,
			"Translations/ja/strings.csv": publishedHeader,
			"Translations/ignore.txt":     "*Sample*\n",
		}, true)
		if len(repo.EmptyLocales) != 0 {
			t.Errorf("ファイルを空のロケールとして数えている: %q", repo.EmptyLocales)
		}
		if len(repo.Locales) != 1 || repo.Locales[0].Name != "ja" {
			t.Errorf("ロケールの列挙が違う: %+v", repo.Locales)
		}
	})
}

// TestReadUnreadableFile は、読めないファイルを「無い」に倒さないことを確かめる。
//
// readRowsFile と readLayoutRisks は、ファイルが無いときだけ nil を返す約束である。
// 読めない（ここではディレクトリが置かれている）のに nil を返すと、公開ファイルなら
// 「訳が1件も無い」、はみ出しの記録なら「測っていない」と報告することになる。
func TestReadUnreadableFile(t *testing.T) {
	dir := t.TempDir()

	if rows, err := readRowsFile(dir); err == nil {
		t.Errorf("ディレクトリを読めたことにしている: %d 行", len(rows))
	}
	if risks, err := readLayoutRisks(dir); err == nil {
		t.Errorf("ディレクトリを読めたことにしている: %v", risks)
	}

	missing := filepath.Join(dir, "missing.csv")
	if rows, err := readRowsFile(missing); err != nil || rows != nil {
		t.Errorf("無いファイルは0行のはず: rows=%v err=%v", rows, err)
	}
	if risks, err := readLayoutRisks(missing); err != nil || risks != nil {
		t.Errorf("無いファイルは nil のはず: risks=%v err=%v", risks, err)
	}
}

// TestReportedLocales は --locale の名前の照合を確かめる。
//
// 完全一致を先に試し、外れたときだけ大文字小文字を無視する（cmd/dwloc と同じ方針）。
// 無視を先にすると、pt-BR と pt-br が並ぶリポジトリで片方を指したつもりが両方出る。
func TestReportedLocales(t *testing.T) {
	locales := []Locale{{Name: "de"}, {Name: "ja"}, {Name: "pt-BR"}, {Name: "pt-br"}}

	tests := []struct {
		name   string
		report []string
		want   []bool
	}{
		{name: "指定が無ければ全部", report: nil, want: []bool{true, true, true, true}},
		{name: "完全一致", report: []string{"ja"}, want: []bool{false, true, false, false}},
		{name: "大文字でも当たる", report: []string{"JA"}, want: []bool{false, true, false, false}},
		{name: "完全一致があれば綴りの違う方は出さない", report: []string{"pt-br"}, want: []bool{false, false, false, true}},
		{name: "完全一致が無ければ大文字小文字を無視して全部", report: []string{"PT-BR"}, want: []bool{false, false, true, true}},
		{name: "当たらない名前は黙って無視する", report: []string{"xx"}, want: []bool{false, false, false, false}},
		{name: "複数を並べられる", report: []string{"de", "Ja"}, want: []bool{true, true, false, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reportedLocales(locales, tt.report)
			if len(got) != len(tt.want) {
				t.Fatalf("長さが違う: got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("%s: got %v, want %v", locales[i].Name, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestRowCountByStatus は締めの1行に使う「行の数」の数え方を固定する。
//
// 同じロケールの同じキーは1行と数え、ロケールが違えば別の行と数える。キーが空の
// Finding（key 列が壊れた作業コピーの行）は1件ずつ別に数える。空どうしを同じ行と
// 見なすと、何行壊れていても1行に潰れ、直す行数を少なく伝えることになる。
func TestRowCountByStatus(t *testing.T) {
	rep := &Report{Findings: []Finding{
		{Locale: "ja", Category: CatVanished, Key: keyHello},
		{Locale: "ja", Category: CatCarryover, Key: keyHello}, // 同じ行の別の見方
		{Locale: "de", Category: CatVanished, Key: keyHello},  // ロケールが違えば別の行
		{Locale: "ja", Category: CatDropped, Key: ""},
		{Locale: "ja", Category: CatDropped, Key: ""},
		{Locale: "ja", Category: CatUntranslated, Key: keyBye}, // 要作業は数えない
	}}

	if got := rep.RowCountByStatus(StatusReview); got != 4 {
		t.Errorf("要確認の行数 = %d, want 4", got)
	}
	if got := rep.CountByStatus(StatusReview); got != 5 {
		t.Errorf("要確認ののべ件数 = %d, want 5", got)
	}
	if got := rep.RowCountByStatus(StatusTodo); got != 1 {
		t.Errorf("要作業の行数 = %d, want 1", got)
	}
	if got := rep.RowCountByStatus(StatusInfo); got != 0 {
		t.Errorf("参考の行数 = %d, want 0", got)
	}
}

// TestCompareEmptyKeyDroppedRowsAreCountedApart は、key 列が空の壊れた行が
// 締めの1行で1行に潰れないことを、読み込みから通して確かめる。
func TestCompareEmptyKeyDroppedRowsAreCountedApart(t *testing.T) {
	repo := newRepo(t, map[string]string{
		"data/script_order.csv":       orderCSV1,
		"Translations/ja/strings.csv": publishedHeader + keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
		// key も source_en も空で訳だけがある行が2つ。publish はどちらも捨てる。
		"Translations/_discovered/ja.working.csv": workingHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n" +
			",,,,,,勝手に足した訳1\n" +
			",,,,,,勝手に足した訳2\n",
	}, true)
	rep := Compare(repo, nil)

	if got := rep.Locales[0].Counts[CatDropped]; got != 2 {
		t.Fatalf("publish で捨てられる行 = %d 件, want 2", got)
	}
	var b strings.Builder
	if err := rep.WriteText(&b, TextOptions{}); err != nil {
		t.Fatalf("WriteText が失敗した: %v", err)
	}
	if !strings.Contains(b.String(), "要確認が 2 行あります。") {
		t.Errorf("壊れた行を1行に潰している:\n%s", b.String())
	}
}

// TestComparePositionSpeakers は、再生順から借りる話者が publish と同じ値になる
// ことを確かめる。同じ英文を2人が話すなら全員を連結し、話者のいない行は空のまま。
// 公開ファイルの speaker 列と見比べたときに食い違わないようにするため。
func TestComparePositionSpeakers(t *testing.T) {
	repo := newRepo(t, map[string]string{
		"data/script_order.csv": orderFile(
			orderRow{"L01 Ryan", "intro", "Ryan_1_intro", "1", "line:aaaa1111", keyHello, "Ryan", ""},
			orderRow{"L02 Kobold", "", "Kobold_1", "4", "line:bbbb2222", keyHello, "Kobold", ""},
			orderRow{"L01 Ryan", "intro", "Ryan_1_intro", "2", "line:cccc3333", keyBye, "", ""},
		),
		// de だけが keyHello を持つ。ja には「他のロケールにあって無い行」として出る。
		"Translations/de/strings.csv": publishedHeader + keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan/Kobold,Hallo\n",
		"Translations/ja/strings.csv": publishedHeader,
	}, false)
	rep := Compare(repo, []string{"ja"})

	var gap, none Finding
	for _, f := range rep.Findings {
		switch {
		case f.Category == CatLocaleGap && f.Key == keyHello:
			gap = f
		case f.Category == CatNotPublished && f.Key == keyBye:
			none = f
		}
	}
	if gap.Key == "" || none.Key == "" {
		t.Fatalf("前提の Finding が無い: %+v", rep.Findings)
	}
	if want := "Ryan" + order.SpeakerSeparator + "Kobold"; gap.Speaker != want {
		t.Errorf("話者の連結が違う: got %q, want %q", gap.Speaker, want)
	}
	// 位置は最初の出現から取る。publish が公開ファイルに書くのも最初の出現。
	if gap.Section != "L01 Ryan" || gap.Node != "Ryan_1_intro" || gap.OrderText != "1" {
		t.Errorf("位置が最初の出現ではない: %q / %q / %q", gap.Section, gap.Node, gap.OrderText)
	}
	if none.Speaker != "" {
		t.Errorf("話者のいない行に話者が入っている: %q", none.Speaker)
	}
}

// TestCompareWithoutOrder は、再生順を持たない Repo を渡しても止まらず、
// 再生順を根拠にするカテゴリを判定しないことを確かめる。
//
// Load は Order を nil にしないが、Repo は外から組み立てられる。nil を渡されて
// 落ちると、画面ごと開けなくなる。
func TestCompareWithoutOrder(t *testing.T) {
	rows, err := ReadRows([]byte(publishedHeader + keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n"))
	if err != nil {
		t.Fatalf("見本を読めない: %v", err)
	}
	rep := Compare(&Repo{Locales: []Locale{{Name: "ja", Published: rows}}}, nil)

	if rep.OrderRows != 0 || rep.OrderKeys != 0 || rep.OrderLineIDs != 0 {
		t.Errorf("再生順の数が 0 ではない: %d / %d / %d", rep.OrderRows, rep.OrderKeys, rep.OrderLineIDs)
	}
	sum := rep.Locales[0]
	if sum.CanJudge(CatVanished) {
		t.Error("再生順が無いのに台本から消えた行を判定している")
	}
	if len(rep.Findings) != 0 {
		t.Errorf("報告が出ている: %+v", rep.Findings)
	}
}
