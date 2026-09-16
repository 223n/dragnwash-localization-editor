package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// sourceRepoEnv は元実装のリポジトリの場所を上書きする環境変数。
const sourceRepoEnv = "DRAGNWASH_SOURCE_REPO"

// sourceRepoCandidates は環境変数が無いときに探す場所。
var sourceRepoCandidates = []string{
	`C:\dev\223n\dragnwash-localization\.claude\worktrees\translator-editor-research-fdf740`,
	`C:\dev\223n\dragnwash-localization`,
}

// 実データの実測値。数が変わったらデータが変わったということなので、
// そのときは「テストを直す」のではなく「何が変わったか」を先に見ること。
const (
	realLocales      = 13   // Translations 直下のロケール数。ignore.txt はファイルなので入らない
	realOrderRows    = 1839 // data/script_order.csv の行数
	realOrderKeys    = 1602 // うちキーの種類数
	realOrderLineIDs = 1839 // 台詞IDは全行ユニークで非空
	realHashRows     = 1680 // 各ロケールの公開ハッシュ行。13ロケールとも同じ
	realNotPublished = 32   // どのロケールにも訳が無い行
	realScriptGap    = 17   // 台本に無い台詞行
	realUnknownOrig  = 93   // 由来を判定できない行
	realResidual     = 110  // 再生順に無い公開ハッシュキー（17 + 93）
)

// sourceRepo は元実装のリポジトリの場所を返す。見つからなければテストを飛ばす。
// CI には元リポジトリが無いので、飛ばせることが必須。
func sourceRepo(t *testing.T) string {
	t.Helper()

	candidates := sourceRepoCandidates
	if env := os.Getenv(sourceRepoEnv); env != "" {
		candidates = []string{env}
	}
	for _, root := range candidates {
		if _, err := os.Stat(filepath.Join(root, "data", "script_order.csv")); err == nil {
			return root
		}
	}
	t.Skipf("元実装のリポジトリが見つからないので飛ばす（%s で場所を指定できる）", sourceRepoEnv)
	return ""
}

// loadRealRepo は元リポジトリを読む。元リポジトリのファイルは読むだけで、
// 絶対に書き換えない。
func loadRealRepo(t *testing.T) *Repo {
	t.Helper()
	repo, err := Load(sourceRepo(t), true)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	if len(repo.Locales) != realLocales {
		t.Fatalf("ロケール数が違う: got %d, want %d", len(repo.Locales), realLocales)
	}
	return repo
}

// TestRealDataCounts は実データの件数が実測値どおりであることを確かめる。
//
// いちばん大事なのは CatVanished が0件であることで、ここが110件に膨らむなら
// 「再生順に無い公開キー＝孤児」という素朴な判定に退化したということ。
func TestRealDataCounts(t *testing.T) {
	repo := loadRealRepo(t)
	rep := Compare(repo, nil)

	if rep.OrderRows != realOrderRows {
		t.Errorf("再生順の行数が違う: got %d, want %d", rep.OrderRows, realOrderRows)
	}
	if rep.OrderKeys != realOrderKeys {
		t.Errorf("再生順のキーの種類が違う: got %d, want %d", rep.OrderKeys, realOrderKeys)
	}
	if rep.OrderLineIDs != realOrderLineIDs {
		t.Errorf("再生順の台詞IDが違う: got %d, want %d", rep.OrderLineIDs, realOrderLineIDs)
	}
	if rep.Status() != StatusInfo {
		t.Errorf("現 HEAD では要確認も要作業も出ないはず: got %s", rep.Status())
	}

	want := map[Category]int{
		CatUntranslated:  0, // 作業コピーが無いので判定そのものが走らない
		CatLocaleGap:     0,
		CatVanished:      0,
		CatDropped:       0,
		CatStrayLineID:   0,
		CatNotPublished:  realNotPublished,
		CatScriptGap:     realScriptGap,
		CatUnknownOrigin: realUnknownOrig,
	}
	for _, sum := range rep.Locales {
		t.Run(sum.Locale, func(t *testing.T) {
			if sum.HasWorking {
				t.Fatalf("元リポジトリに作業コピーがある: %s", sum.WorkingPath)
			}
			if sum.HashRows != realHashRows {
				t.Errorf("ハッシュ行の数が違う: got %d, want %d", sum.HashRows, realHashRows)
			}
			if sum.BrokenRows != 0 {
				t.Errorf("形の分からない行がある: %d", sum.BrokenRows)
			}
			for _, c := range categories {
				if sum.Counts[c] != want[c] {
					t.Errorf("%s の件数が違う: got %d, want %d", c, sum.Counts[c], want[c])
				}
			}
			// 3つの参考カテゴリで、再生順に無い公開キー110件をちょうど割り切る。
			residual := sum.Counts[CatVanished] + sum.Counts[CatScriptGap] + sum.Counts[CatUnknownOrigin]
			if residual != realResidual {
				t.Errorf("再生順に無い公開キーの合計が違う: got %d, want %d", residual, realResidual)
			}
		})
	}
}

// TestRealDataLocalesShareKeys は13ロケールの公開ハッシュキーが完全に同じ集合で
// あることを確かめる。「他のロケールにあって無い行」が0件になる根拠がこれで、
// この前提が崩れたときに件数だけを見て慌てないようにする。
func TestRealDataLocalesShareKeys(t *testing.T) {
	repo := loadRealRepo(t)

	union := make(map[string]struct{})
	sets := make(map[string]map[string]struct{}, len(repo.Locales))
	for _, loc := range repo.Locales {
		set := hashKeySet(loc.Published)
		sets[loc.Name] = set
		for k := range set {
			union[k] = struct{}{}
		}
	}
	if len(union) != realHashRows {
		t.Fatalf("和集合の大きさが違う: got %d, want %d", len(union), realHashRows)
	}
	for name, set := range sets {
		if len(set) != realHashRows {
			t.Errorf("%s のキー数が違う: got %d, want %d", name, len(set), realHashRows)
		}
		for k := range union {
			if _, ok := set[k]; !ok {
				t.Errorf("%s に %s が無い", name, k)
			}
		}
	}
}

// TestRealDataResidualIsUI は「再生順に無い公開キー」「section が UI」
// 「node が空」「order が空」の4つが同じ集合であることを確かめる。
//
// 3つの列が同じ1ビットの言い換えでしかないことの裏取り。どれか1つを見れば
// 足りる、という設計の根拠がここにある。
func TestRealDataResidualIsUI(t *testing.T) {
	repo := loadRealRepo(t)
	idx := newOrderIndex(repo.Order)

	for _, loc := range repo.Locales {
		t.Run(loc.Name, func(t *testing.T) {
			residual, uiSection, emptyNode, emptyOrder := 0, 0, 0, 0
			for _, row := range loc.Published {
				if row.Kind != KindHash {
					continue
				}
				if _, inOrder := idx.first[row.Key]; inOrder {
					continue
				}
				residual++
				if row.Section == "UI" {
					uiSection++
				}
				if row.Node == "" {
					emptyNode++
				}
				if row.OrderText == "" {
					emptyOrder++
				}
			}
			if residual != realResidual {
				t.Fatalf("再生順に無い公開キーの数が違う: got %d, want %d", residual, realResidual)
			}
			if uiSection != residual || emptyNode != residual || emptyOrder != residual {
				t.Errorf("4集合が一致しない: 再生順の外 %d / section=UI %d / node 空 %d / order 空 %d",
					residual, uiSection, emptyNode, emptyOrder)
			}
		})
	}
}

// TestRealDataScriptGapSpeakers は「台本に無い台詞行」17件の話者の内訳が
// 13ロケールで一致することを確かめる。
//
// これが UI 文言ではなく会話であることの根拠。増えたときに気づけるように
// 数を固定しておく（運用の方針が決まるまでは参考のまま）。
func TestRealDataScriptGapSpeakers(t *testing.T) {
	repo := loadRealRepo(t)
	rep := Compare(repo, nil)

	want := map[string]int{"Ryan": 8, "Conrad": 9}
	for _, sum := range rep.Locales {
		got := make(map[string]int)
		for _, f := range rep.Findings {
			if f.Locale != sum.Locale || f.Category != CatScriptGap {
				continue
			}
			got[f.Speaker]++
		}
		if len(got) != len(want) {
			t.Errorf("%s: 話者の種類が違う: %v", sum.Locale, got)
		}
		for name, n := range want {
			if got[name] != n {
				t.Errorf("%s: %s の件数が違う: got %d, want %d", sum.Locale, name, got[name], n)
			}
		}
	}
}

// TestRealDataNotPublishedNodes は「どのロケールにも訳が無い行」32件の
// ノード別の内訳が実測どおりで、13ロケールで一致することを確かめる。
//
// 13人が独立に同じ32行を訳し忘れたとは考えにくく、Translations/ignore.txt の
// 除外パターンと符合する。だから要作業ではなく参考に置いてある。
func TestRealDataNotPublishedNodes(t *testing.T) {
	repo := loadRealRepo(t)
	rep := Compare(repo, nil)

	want := map[string]int{
		"Unused / Start":                           24,
		"L08 Conrad / Conrad_Outro":                3,
		"L13 Ryan / Ryan_5_Required_ryan_romanced": 1,
		"Unused / Alexander_Outro":                 1,
		"Unused / ConradBeatup_Outro":              1,
		"Unused / RyanDate_Outro":                  1,
		"Unused / RyanExploded_Outro":              1,
	}
	for _, sum := range rep.Locales {
		got := make(map[string]int)
		unused := 0
		for _, f := range rep.Findings {
			if f.Locale != sum.Locale || f.Category != CatNotPublished {
				continue
			}
			got[f.Section+" / "+f.Node]++
			if f.Section == "Unused" {
				unused++
			}
		}
		if len(got) != len(want) {
			t.Errorf("%s: ノードの種類が違う: %v", sum.Locale, sortedKeys(got))
		}
		for node, n := range want {
			if got[node] != n {
				t.Errorf("%s: %s の件数が違う: got %d, want %d", sum.Locale, node, got[node], n)
			}
		}
		if unused != 28 {
			t.Errorf("%s: Unused の件数が違う: got %d, want 28", sum.Locale, unused)
		}
	}
}

// TestRealDataEmptyLineIDsAreNotReported は「再生順に line_id があるのに公開CSVに
// 台詞ID行が無い」ことを一切報告しないことを、実データで確かめる。
//
// 公開の台詞ID行は tok=0 〜 ja=41 と数が違い、再生順の1839件の大半には対応する
// 行が無い。ここを検出に加えると1ロケールあたり最大1839件の誤検出が出る。
func TestRealDataEmptyLineIDsAreNotReported(t *testing.T) {
	repo := loadRealRepo(t)
	rep := Compare(repo, nil)
	idx := newOrderIndex(repo.Order)

	for _, loc := range repo.Locales {
		published := make(map[string]struct{})
		for _, row := range loc.Published {
			if row.Kind == KindLineID {
				published[row.Key] = struct{}{}
			}
		}
		// 誤検出の余地が本当にあることを先に確かめる。
		missing := 0
		for id := range idx.lineIDs {
			if _, ok := published[id]; !ok {
				missing++
			}
		}
		if missing == 0 {
			t.Fatalf("%s: 訳の無い台詞IDが1件も無い。テストの前提が崩れている", loc.Name)
		}
		for _, f := range rep.Findings {
			if f.Locale != loc.Name {
				continue
			}
			if strings.HasPrefix(f.Key, "line:") {
				t.Errorf("%s: 台詞IDを報告している: %s（%s）", loc.Name, f.Key, f.Category)
			}
		}
	}
}

// TestRealDataOutputs は実データの報告を両方の形式で書き出せることを確かめる。
// 数は他のテストが見ているので、ここは「書けること」と「絶対パスが出ないこと」だけ。
func TestRealDataOutputs(t *testing.T) {
	root := sourceRepo(t)
	repo := loadRealRepo(t)
	rep := Compare(repo, []string{"ja"})

	var text strings.Builder
	if err := rep.WriteText(&text, TextOptions{Root: root, Limit: 20}); err != nil {
		t.Fatalf("text で書けない: %v", err)
	}
	if strings.Contains(text.String(), root) {
		t.Error("絶対パスが出ている")
	}
	if !strings.Contains(text.String(), "要確認はありません。") {
		t.Errorf("締めの行が違う:\n%s", text.String())
	}

	var csv strings.Builder
	if err := rep.WriteCSV(&csv); err != nil {
		t.Fatalf("csv で書けない: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(csv.String(), "\n"), "\n")
	if len(lines)-1 != realNotPublished+realScriptGap+realUnknownOrig {
		t.Errorf("CSV の行数が違う: got %d", len(lines)-1)
	}
	// 作業コピーが無いので、英語原文がログへ出ることはない。
	for _, line := range lines[1:] {
		fields := strings.Split(line, ",")
		if len(fields) > 8 && fields[8] != "" {
			t.Errorf("source_en 列に値が入っている: %s", line)
			break
		}
	}
	t.Logf("text %d 行 / csv %d 行", strings.Count(text.String(), "\n"), len(lines)-1)
}

// 2026-09-14 のゲーム更新を再現するためのコミット。
// このコミットの data/script_order.csv と、その1つ前の公開ファイルを組み合わせると、
// 「再生順は新しくなったが公開ファイルはまだ古い」状態になる。
const (
	updateCommit = "0490f89" // Re-key the packs for the September 14 game update
	// 実測値。旧 script_order にしか無いキー23種のうち、訳を持つ23件と一致する。
	updateVanished = 23
	// 同じ入力に素朴な規則（再生順に無い公開キー＝孤児）を当てたときの件数。
	// 差の 68 件がまるごと誤検出になる。
	updateNaive = 91
)

// TestRealDataGameUpdate はゲーム更新の直後を再現して、「台本から消えた行」が
// 消えた行だけを拾うことを確かめる。
//
// 現 HEAD では13ロケールとも0件なので、このテストが無いと「何も検出しない実装」
// でも実データのテストが通ってしまう。誤検出を出さないことと同じくらい、
// 出すべきときに出すことが大事なので、更新の瞬間を再現して両方を測る。
//
// 元リポジトリへは git show で読むだけ。組み立てたファイルは t.TempDir() にしか
// 置かない。git が無い環境やコミットが見つからない環境では飛ばす。
func TestRealDataGameUpdate(t *testing.T) {
	src := sourceRepo(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無いので飛ばす")
	}

	root := t.TempDir()
	files := map[string]string{
		// 新しい再生順。
		filepath.Join(root, "data", "script_order.csv"): updateCommit + ":data/script_order.csv",
		// 更新前の公開ファイル。
		filepath.Join(root, "Translations", "ja", "strings.csv"): updateCommit + "^:Translations/ja/strings.csv",
		filepath.Join(root, "Translations", "de", "strings.csv"): updateCommit + "^:Translations/de/strings.csv",
	}
	for path, rev := range files {
		out, err := exec.Command("git", "-C", src, "show", rev).Output()
		if err != nil {
			t.Skipf("%s を取り出せないので飛ばす: %v", rev, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	repo, err := Load(root, false)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	rep := Compare(repo, nil)
	if rep.Status() != StatusReview {
		t.Errorf("更新の直後は要確認になるはず: got %s", rep.Status())
	}

	idx := newOrderIndex(repo.Order)
	for i, sum := range rep.Locales {
		if sum.Counts[CatVanished] != updateVanished {
			t.Errorf("%s: 台本から消えた行が違う: got %d, want %d",
				sum.Locale, sum.Counts[CatVanished], updateVanished)
		}
		// 素朴な規則との差。ここが縮んだら誤検出を出す実装に戻ったということ。
		naive := 0
		for _, row := range repo.Locales[i].Published {
			if row.Kind != KindHash {
				continue
			}
			if _, inOrder := idx.first[row.Key]; !inOrder {
				naive++
			}
		}
		if naive != updateNaive {
			t.Errorf("%s: 素朴な規則の件数が違う: got %d, want %d", sum.Locale, naive, updateNaive)
		}
	}
}

// sortedKeys は報告メッセージ用にマップのキーを並べる。
func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
