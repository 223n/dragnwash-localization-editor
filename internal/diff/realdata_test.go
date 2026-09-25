package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/order"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/sourcerepo"
)

// 実データの試験は上流 main に追従する（改善の決定 31）。
//
// 以前は上流 003ed1e の実測値（13 ロケール、再生順 1839 行、公開ハッシュ行 1680、
// どのロケールにも訳が無い行 32、台本に無い台詞行 17、由来を判定できない行 93 など）を
// 決め打ちにして、データの変化を捕まえるつもりでいた。いまの main（16 ロケール、
// 公開ハッシュ行 1697、由来を判定できない行 110）を渡すと、コードと関係なく落ちて
// いた。
//
// いまは、件数を入力から数える。数え方は読み手（internal/csvfile と ReadRows）を
// 通さず、物理行をカンマで分けたもの（sourcerepo.ContentLines）から求める。カテゴリの
// 判定の正しさは合成の見本の試験（compare_test.go など）が見る。ここで見るのは、
// 実データの規模と形で、判定が入力から数えた集合とちょうど合うことと、上流の HEAD が
// 満たすはずの性質（台本から消えた行が無い、など）である。
//
// ゲーム更新を再現する試験（TestRealDataGameUpdate など）は、決まったコミット
// （0490f89）を git show で読むので、その版の実測値のまま決め打ちにしてある。

// sourceRepo は元実装のリポジトリの場所を返す。環境変数（sourcerepo.Env）で指定した
// 場所に無ければ落とし、指定していなくて見つからなければ飛ばす（sourcerepo.Find）。
// CI には元リポジトリが無いので、飛ばせることが必須。
func sourceRepo(t *testing.T) string {
	t.Helper()
	return sourcerepo.Find(t, "data", "script_order.csv")
}

// loadRealRepo は元リポジトリを読む。元リポジトリのファイルは読むだけで、
// 絶対に書き換えない。ロケールの数は、Translations 直下から数えたもの
// （sourcerepo.Locales）と比べる。
func loadRealRepo(t *testing.T) *Repo {
	t.Helper()
	root := sourceRepo(t)
	repo, err := Load(root, true)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	if want := len(sourcerepo.Locales(t, root)); len(repo.Locales) != want {
		t.Fatalf("ロケール数が違う: got %d, want %d", len(repo.Locales), want)
	}
	return repo
}

// realOrder は、data/script_order.csv を読み手を通さずに数えたもの。
type realOrder struct {
	// rows はデータ行の数。
	rows int
	// keys は、key 列の値（前後の空白を除いて小文字にしたもの。空は除く）の集合。
	keys map[string]struct{}
	// lineIDs は、line_id 列の値（空は除く）の集合。
	lineIDs map[string]struct{}
}

// countRealOrder は data/script_order.csv を物理行から数える。data/ の2ファイルには
// 引用符・コメント行・空行が無い（internal/order の TestRealDataLoadersAgree の前提）。
func countRealOrder(t *testing.T, root string) realOrder {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "data", "script_order.csv"))
	if err != nil {
		t.Fatal(err)
	}
	lines := sourcerepo.ContentLines(raw)
	header, _ := sourcerepo.PlainFields(lines[0].Text)
	col := func(name string) int {
		for i, h := range header {
			if strings.EqualFold(h, name) {
				return i
			}
		}
		t.Fatalf("script_order.csv に %s 列が無い", name)
		return -1
	}
	keyCol, lineIDCol := col("key"), col("line_id")
	out := realOrder{keys: map[string]struct{}{}, lineIDs: map[string]struct{}{}}
	for _, line := range lines[1:] {
		fields, ok := sourcerepo.PlainFields(line.Text)
		if !ok {
			t.Fatalf("script_order.csv の %d行目に引用符がある", line.Number)
		}
		out.rows++
		if k := strings.ToLower(strings.TrimSpace(fields[keyCol])); k != "" {
			out.keys[k] = struct{}{}
		}
		if id := fields[lineIDCol]; id != "" {
			out.lineIDs[id] = struct{}{}
		}
	}
	return out
}

// realPublished は、1ロケールの公開ファイルを読み手を通さずに数えたもの。
type realPublished struct {
	// hashRows は、key 列が16桁のハッシュキーの行の数。
	hashRows int
	// keys はそのキーの集合。
	keys map[string]struct{}
}

// countRealPublished は公開ファイルを物理行から数える。key 列は最初の列で、キーに
// 引用符やカンマは入らないので、最初のカンマまでを key の値とする。
func countRealPublished(t *testing.T, path string) realPublished {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := realPublished{keys: map[string]struct{}{}}
	for _, line := range sourcerepo.ContentLines(raw)[1:] {
		first, _, _ := strings.Cut(line.Text, ",")
		if k := strings.ToLower(strings.TrimSpace(first)); key.LooksLike(k) {
			out.hashRows++
			out.keys[k] = struct{}{}
		}
	}
	return out
}

// TestRealDataCounts は、実データの報告の件数が、入力から数えた集合とちょうど
// 合うことを確かめる。
//
// いちばん大事なのは「台本から消えた行」が0件であることで、公開ファイルを
// いまの再生順で作り直した上流の HEAD では、再生順に無い公開キーはすべて UI の
// 行になっている（TestRealDataResidualIsUI）。ここが膨らむなら「再生順に無い
// 公開キー＝孤児」という素朴な判定に退化したということ。
func TestRealDataCounts(t *testing.T) {
	root := sourceRepo(t)
	repo := loadRealRepo(t)
	rep := Compare(repo, nil)
	ord := countRealOrder(t, root)

	if rep.OrderRows != ord.rows {
		t.Errorf("再生順の行数が違う: got %d, want %d", rep.OrderRows, ord.rows)
	}
	if rep.OrderKeys != len(ord.keys) {
		t.Errorf("再生順のキーの種類が違う: got %d, want %d", rep.OrderKeys, len(ord.keys))
	}
	if rep.OrderLineIDs != len(ord.lineIDs) {
		t.Errorf("再生順の台詞IDが違う: got %d, want %d", rep.OrderLineIDs, len(ord.lineIDs))
	}
	if rep.Status() == StatusReview {
		t.Errorf("上流の HEAD では要確認が出ないはず: got %s", rep.Status())
	}

	// 全ロケールの公開ハッシュキーを、読み手を通さずに集める。
	published := make(map[string]realPublished, len(repo.Locales))
	union := make(map[string]struct{})
	for _, loc := range repo.Locales {
		p := countRealPublished(t, loc.PublishedPath)
		published[loc.Name] = p
		for k := range p.keys {
			union[k] = struct{}{}
		}
	}
	// どのロケールにも訳が無い行は、再生順のキーのうち、どの公開ファイルにも無いもの。
	notPublished := 0
	for k := range ord.keys {
		if _, ok := union[k]; !ok {
			notPublished++
		}
	}

	for _, sum := range rep.Locales {
		t.Run(sum.Locale, func(t *testing.T) {
			if sum.HasWorking {
				t.Fatalf("元リポジトリに作業コピーがある: %s", sum.WorkingPath)
			}
			p := published[sum.Locale]
			if sum.HashRows != p.hashRows {
				t.Errorf("ハッシュ行の数が違う: got %d, want %d", sum.HashRows, p.hashRows)
			}
			if sum.BrokenRows != 0 {
				t.Errorf("形の分からない行がある: %d", sum.BrokenRows)
			}
			// 作業コピーが無いので判定そのものが走らないもの（未翻訳など）と、上流の
			// HEAD では出ないはずのもの（台本から消えた行と、消えた行が無いので
			// 引き継ぎ先を探す相手もいない引き継ぎ候補など）。
			for _, c := range []Category{CatUntranslated, CatVanished, CatCarryover, CatCarryFrom,
				CatDropped, CatStrayLineID, CatTagMismatch, CatLayoutRisk} {
				if sum.Counts[c] != 0 {
					t.Errorf("%s の件数が違う: got %d, want 0", c, sum.Counts[c])
				}
			}
			// 他のロケールにあって無い行は、公開ハッシュキーの和集合からこのロケールの
			// キーを引いた残り。上流 main では全ロケールが同じキーを持つので0件になる。
			gap := 0
			for k := range union {
				if _, ok := p.keys[k]; !ok {
					gap++
				}
			}
			if sum.Counts[CatLocaleGap] != gap {
				t.Errorf("%s の件数が違う: got %d, want %d", CatLocaleGap, sum.Counts[CatLocaleGap], gap)
			}
			if sum.Counts[CatNotPublished] != notPublished {
				t.Errorf("%s の件数が違う: got %d, want %d", CatNotPublished, sum.Counts[CatNotPublished], notPublished)
			}
			// 3つの参考カテゴリで、再生順に無い公開キーをちょうど割り切る。
			residual := 0
			for k := range p.keys {
				if _, ok := ord.keys[k]; !ok {
					residual++
				}
			}
			got := sum.Counts[CatVanished] + sum.Counts[CatScriptGap] + sum.Counts[CatUnknownOrigin]
			if got != residual {
				t.Errorf("再生順に無い公開キーの合計が違う: got %d, want %d", got, residual)
			}
			t.Logf("ハッシュ行 %d、どのロケールにも訳が無い行 %d、台本に無い台詞行 %d、由来を判定できない行 %d、タグの開閉がそろわない行 %d",
				sum.HashRows, sum.Counts[CatNotPublished], sum.Counts[CatScriptGap], sum.Counts[CatUnknownOrigin], sum.Counts[CatTagUnbalanced])
		})
	}
}

// TestRealDataResidualIsUI は「再生順に無い公開キー」「section が UI」
// 「node が空」「order が空」の4つが同じ集合であることを確かめる。
//
// 3つの列が同じ1ビットの言い換えでしかないことの裏取り。どれか1つを見れば
// 足りる、という設計の根拠がここにある。publish が再生順に置けなかった行を
// UI の見出しの下へ回すので、上流の HEAD の公開ファイルはこの形になる。
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
				if row.Section == publish.UISectionName {
					uiSection++
				}
				if row.Node == "" {
					emptyNode++
				}
				if row.OrderText == "" {
					emptyOrder++
				}
			}
			if uiSection != residual || emptyNode != residual || emptyOrder != residual {
				t.Errorf("4集合が一致しない: 再生順の外 %d / section=UI %d / node 空 %d / order 空 %d",
					residual, uiSection, emptyNode, emptyOrder)
			}
		})
	}
}

// TestRealDataScriptGapSpeakers は「台本に無い台詞行」がどれも話者を持つ（UI の
// 文言ではなく会話である）ことと、同じキーを持つロケールどうしで話者の内訳が
// 一致することを確かめる。
//
// 以前は 003ed1e の内訳（2人の話者で 8 件と 9 件）を決め打ちにしていた。上流 main に
// 追従するので、件数は決め打ちにしない。
func TestRealDataScriptGapSpeakers(t *testing.T) {
	repo := loadRealRepo(t)
	rep := Compare(repo, nil)

	byLocale := make(map[string]map[string]int)
	for _, f := range rep.Findings {
		if f.Category != CatScriptGap {
			continue
		}
		if f.Speaker == "" || f.Speaker == publish.UIFallbackSpeaker {
			t.Errorf("%s: 話者の無い行を「台本に無い台詞行」にしている: %s", f.Locale, f.Key)
		}
		if byLocale[f.Locale] == nil {
			byLocale[f.Locale] = make(map[string]int)
		}
		byLocale[f.Locale][f.Speaker]++
	}
	sameKeys := localesWithSameKeys(repo)
	first := byLocale[sameKeys[0]]
	for _, name := range sameKeys[1:] {
		got := byLocale[name]
		if len(got) != len(first) {
			t.Errorf("%s: 話者の種類が %s と違う: %v と %v", name, sameKeys[0], sortedKeys(got), sortedKeys(first))
			continue
		}
		for speaker, n := range first {
			if got[speaker] != n {
				t.Errorf("%s: %s の件数が %s と違う: got %d, want %d", name, speaker, sameKeys[0], got[speaker], n)
			}
		}
	}
}

// localesWithSameKeys は、最初のロケールと公開ハッシュキーの集合がまったく同じ
// ロケールの名前を、最初のロケールを先頭に並べて返す。
func localesWithSameKeys(repo *Repo) []string {
	base := hashKeySet(repo.Locales[0].Published)
	out := []string{repo.Locales[0].Name}
	for _, loc := range repo.Locales[1:] {
		set := hashKeySet(loc.Published)
		if len(set) != len(base) {
			continue
		}
		same := true
		for k := range base {
			if _, ok := set[k]; !ok {
				same = false
				break
			}
		}
		if same {
			out = append(out, loc.Name)
		}
	}
	return out
}

// TestRealDataNotPublishedNodes は「どのロケールにも訳が無い行」が、どのロケールでも
// 同じキーの集合で、どの行も再生順から位置（section と node）を借りていることを
// 確かめる。
//
// 全ロケールが独立に同じ行を訳し忘れたとは考えにくく、Translations/ignore.txt の
// 除外パターンと符合する。だから要作業ではなく参考に置いてある。以前は 003ed1e の
// ノード別の内訳を決め打ちにしていた。
func TestRealDataNotPublishedNodes(t *testing.T) {
	repo := loadRealRepo(t)
	rep := Compare(repo, nil)

	byLocale := make(map[string][]string)
	for _, f := range rep.Findings {
		if f.Category != CatNotPublished {
			continue
		}
		if f.Section == "" || f.Node == "" {
			t.Errorf("%s: 位置の無い行がある: %s", f.Locale, f.Key)
		}
		byLocale[f.Locale] = append(byLocale[f.Locale], f.Key)
	}
	want := byLocale[repo.Locales[0].Name]
	for _, loc := range repo.Locales[1:] {
		got := byLocale[loc.Name]
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: どのロケールにも訳が無い行が %s と違う: %d 件と %d 件", loc.Name, repo.Locales[0].Name, len(got), len(want))
		}
	}
}

// TestRealDataEmptyLineIDsAreNotReported は「再生順に line_id があるのに公開CSVに
// 台詞ID行が無い」ことを一切報告しないことを、実データで確かめる。
//
// 公開の台詞ID行はロケールによって数が違い（0 件のロケールもある）、再生順の
// 台詞IDの大半には対応する行が無い。ここを検出に加えると、1ロケールあたり
// 再生順の行の数に近い誤検出が出る。
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
// 数は他のテストが見ているので、ここは「書けること」と「絶対パスが出ないこと」と、
// csv の行の数が報告の件数と合うことだけ。
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
	if rep.Status() != StatusReview && !strings.Contains(text.String(), "要確認はありません。") {
		t.Errorf("締めの行が違う:\n%s", text.String())
	}

	var csv strings.Builder
	if err := rep.WriteCSV(&csv); err != nil {
		t.Fatalf("csv で書けない: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(csv.String(), "\n"), "\n")
	if len(lines)-1 != len(rep.Findings) {
		t.Errorf("CSV の行数が違う: got %d, want %d（報告の件数）", len(lines)-1, len(rep.Findings))
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

	// 旧版と新版で key が変わった行の数。旧版・新版とも1839行で、line_id の集合は
	// 完全に一致する（旧だけ0・新だけ0）。変わったのは key だけ。
	updateChangedRows = 25
	// 引き継ぎ候補の実測値。変わった25行の旧キーは24種で、24種とも引き継ぎ先が
	// 1つに定まる（line:71d57aeb と line:a1f9de62 が同じ旧キー・同じ新キー）。
	// 候補は公開ファイルの行に付くので、キーの種類と同じ24件になる。
	updateCarryover = 24
	// その内訳。23件は旧キーがもう再生順のどこにも無い「移動」。
	// 1件は旧キー d12499a3f17512de が Unused/RyanMuddy_Intro に残る「複製」で、
	// 旧行の訳を消すと、いまも再生される行が英語に戻る。
	updateCarryMoved  = 23
	updateCarryCopied = 1

	// 旧・新の script_order.csv のキー集合の差。
	// 新しく現れた24キーは全部が再キー付けで、本当に新しく足された台詞は0件。
	// 「残り1件は本物の新規追加」ではない（旧実装のコメントは事実と違っていた）。
	updateOldOnlyKeys = 23
	updateNewOnlyKeys = 24
	updateCommonKeys  = 1578

	// updateOrderRows は、この更新の旧版と新版の再生順の行数。どちらも1839行で、
	// 台詞IDは全行で重ならず空も無い。
	updateOrderRows = 1839
)

// orderKeysAt は元リポジトリの指定した版の script_order.csv から、キーの集合を作る。
// 読むだけで、元リポジトリには一切書き込まない。
func orderKeysAt(t *testing.T, rev string) map[string]struct{} {
	t.Helper()
	out := gitShowAt(t, rev)
	data, err := order.LoadPowerShell(out, nil)
	if err != nil {
		t.Fatalf("%s を読めない: %v", rev, err)
	}
	keys := make(map[string]struct{}, len(data.Entries))
	for _, e := range data.Entries {
		if e.Key != "" {
			keys[e.Key] = struct{}{}
		}
	}
	return keys
}

// gameUpdateRepo は 2026-09-14 のゲーム更新の直後を t.TempDir() に組み立てて読む。
//
// 元リポジトリへは git show で読むだけで、絶対に書き換えない。git が無い環境や
// コミットが見つからない環境では、呼んだテストごと飛ばす。
func gameUpdateRepo(t *testing.T) *Repo {
	t.Helper()
	return gameUpdateRepoWith(t, nil)
}

// gameUpdateRepoWith は再生順のCSVに手を入れてから組み立てる。
// editOrder が nil なら元のまま。挿入や削除を足した状態を作るために使う。
//
// 旧再生順は 0490f89^ の版を [Options.OldOrder] で直に渡す。組み立て先は
// t.TempDir() で git リポジトリではないので、既定の [GitOldOrder] には任せられない。
// ここを関数で差せるようにしてあるおかげで、元リポジトリを読むだけで
// 「更新の直後」を再現できる。
func gameUpdateRepoWith(t *testing.T, editOrder func([]byte) []byte) *Repo {
	t.Helper()
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
		out := gitShowAt(t, rev)
		if editOrder != nil && strings.HasSuffix(rev, "data/script_order.csv") {
			out = editOrder(out)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 更新前の再生順。公開ファイルと同じ 0490f89^ の版。
	oldOrder := gitShowAt(t, updateCommit+"^:data/script_order.csv")
	repo, err := LoadWith(root, Options{
		OldOrder: func(string, string) ([]byte, error) { return oldOrder, nil },
	})
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	if repo.OldOrder == nil {
		t.Fatalf("旧再生順を渡したのに読めていない: %s", repo.OldOrderReason)
	}
	return repo
}

// gitShowAt は元リポジトリの指定した版の中身を取り出す。
// 読むだけで、元リポジトリには一切書き込まない。
func gitShowAt(t *testing.T, rev string) []byte {
	t.Helper()
	out, err := exec.Command("git", "-C", sourceRepo(t), "show", rev).Output()
	if err != nil {
		t.Skipf("%s を取り出せないので飛ばす: %v", rev, err)
	}
	return out
}

// carryPairs はそのロケールの引き継ぎ候補を「引き継ぎ元 → 引き継ぎ先」で取り出す。
func carryPairs(rep *Report, locale string) map[string]string {
	out := make(map[string]string)
	for _, f := range rep.Findings {
		if f.Locale == locale && f.Category == CatCarryover {
			out[f.Key] = f.CarryTo
		}
	}
	return out
}

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
	repo := gameUpdateRepo(t)
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

// TestRealDataGameUpdateLineIDs は、旧版と新版の再生順が台詞IDで過不足なく
// 対応することを確かめる。引き継ぎ候補の前提そのもの。
//
// 台詞IDの集合がずれていたら、突き合わせの土台が崩れているということなので、
// 候補の件数を見る前にここで止める。
func TestRealDataGameUpdateLineIDs(t *testing.T) {
	repo := gameUpdateRepo(t)

	oldIDs := keysByLineID(repo.OldOrder.Entries)
	newIDs := keysByLineID(repo.Order.Entries)
	if len(oldIDs) != updateOrderRows || len(newIDs) != updateOrderRows {
		t.Fatalf("台詞IDの数が違う: 旧 %d / 新 %d, want %d", len(oldIDs), len(newIDs), updateOrderRows)
	}

	oldOnly, newOnly, changed := 0, 0, 0
	for id, from := range oldIDs {
		to, ok := newIDs[id]
		if !ok {
			oldOnly++
			continue
		}
		if from != to {
			changed++
		}
	}
	for id := range newIDs {
		if _, ok := oldIDs[id]; !ok {
			newOnly++
		}
	}
	if oldOnly != 0 || newOnly != 0 {
		t.Errorf("台詞IDの集合が一致しない: 旧だけ %d / 新だけ %d", oldOnly, newOnly)
	}
	if changed != updateChangedRows {
		t.Errorf("キーが変わった行が違う: got %d, want %d", changed, updateChangedRows)
	}
}

// TestRealDataGameUpdateCarryover は、同じゲーム更新で引き継ぎ候補が24件出て、
// その全部が「旧版でそのキーだった行が、新版で別のキーになった」形に
// なっていることを確かめる。
//
// 件数だけでなく1件ずつ形を検算するのは、間違った引き継ぎ候補が、翻訳者に
// 間違った訳を別の行へ移させることになるため。訳を失うより悪い結果になるので、
// 「何件出たか」より「出たものが全部正しいか」を先に見る。
func TestRealDataGameUpdateCarryover(t *testing.T) {
	repo := gameUpdateRepo(t)
	rep := Compare(repo, nil)
	idx := newOrderIndex(repo.Order)

	// 旧版の再生順と突き合わせて、「消えたキー」「現れたキー」を先に確定させる。
	// 候補の正しさは、この2つの集合の中に収まっているかどうかで測る。
	oldKeys := orderKeysAt(t, updateCommit+"^:data/script_order.csv")
	gone, fresh, common := make(map[string]struct{}), make(map[string]struct{}), 0
	for k := range oldKeys {
		if _, live := idx.first[k]; live {
			common++
			continue
		}
		gone[k] = struct{}{}
	}
	for k := range idx.first {
		if _, was := oldKeys[k]; !was {
			fresh[k] = struct{}{}
		}
	}
	if len(gone) != updateOldOnlyKeys || len(fresh) != updateNewOnlyKeys || common != updateCommonKeys {
		t.Fatalf("キー集合の差が違う: 旧だけ %d（want %d） / 新だけ %d（want %d） / 共通 %d（want %d）",
			len(gone), updateOldOnlyKeys, len(fresh), updateNewOnlyKeys, common, updateCommonKeys)
	}

	// 旧版の台詞IDから引いた「この更新で key が変わった旧キー → 新キー」。
	// 実装とは別に、テストの側でも同じ組を作って突き合わせる。
	oldIDs := keysByLineID(repo.OldOrder.Entries)
	newIDs := keysByLineID(repo.Order.Entries)
	want := make(map[string]string)
	for id, from := range oldIDs {
		if to, ok := newIDs[id]; ok && from != to {
			want[from] = to
		}
	}
	if len(want) != updateCarryover {
		t.Fatalf("キーが変わった旧キーの種類が違う: got %d, want %d", len(want), updateCarryover)
	}

	for i, sum := range rep.Locales {
		t.Run(sum.Locale, func(t *testing.T) {
			if sum.Counts[CatCarryover] != updateCarryover {
				t.Errorf("引き継ぎ候補が違う: got %d, want %d", sum.Counts[CatCarryover], updateCarryover)
			}
			if sum.CarryMoved != updateCarryMoved || sum.CarryCopied != updateCarryCopied {
				t.Errorf("移動と複製の内訳が違う: 移動 %d（want %d） / 複製 %d（want %d）",
					sum.CarryMoved, updateCarryMoved, sum.CarryCopied, updateCarryCopied)
			}
			// 「台本から消えた行」は減らさない。移動の23件は同じ行を別の見方で
			// 足しただけで、複製の1件は旧キーが生きているのでこちらには出ない。
			if sum.Counts[CatVanished] != updateVanished {
				t.Errorf("台本から消えた行が違う: got %d, want %d", sum.Counts[CatVanished], updateVanished)
			}

			mine := hashKeySet(repo.Locales[i].Published)
			targets := make(map[string]string, updateCarryover)
			for _, f := range rep.Findings {
				if f.Locale != sum.Locale || f.Category != CatCarryover {
					continue
				}
				// 引き継ぎ元は、訳を持っていて、旧版の台詞IDから引ける組のキー。
				if _, ok := mine[f.Key]; !ok {
					t.Errorf("引き継ぎ元が公開ファイルに無い: %s", f.Key)
				}
				if want[f.Key] != f.CarryTo {
					t.Errorf("台詞IDから引いた組と違う: %s -> got %s, want %s",
						f.Key, f.CarryTo, want[f.Key])
				}
				if f.Translation == "" {
					t.Errorf("移す訳が無い行を候補にしている: %s", f.Key)
				}
				// 引き継ぎ先は、再生順にあって、まだ訳の無いキー。
				if _, live := idx.first[f.CarryTo]; !live {
					t.Errorf("引き継ぎ先が再生順に無い: %s -> %s", f.Key, f.CarryTo)
				}
				if _, ok := mine[f.CarryTo]; ok {
					t.Errorf("既に訳のあるキーを引き継ぎ先にしている: %s -> %s", f.Key, f.CarryTo)
				}
				if _, ok := fresh[f.CarryTo]; !ok {
					t.Errorf("この更新で現れたのではないキーを引き継ぎ先にしている: %s -> %s", f.Key, f.CarryTo)
				}
				if prev, dup := targets[f.CarryTo]; dup {
					t.Errorf("同じ引き継ぎ先が2度出ている: %s と %s -> %s", prev, f.Key, f.CarryTo)
				}
				targets[f.CarryTo] = f.Key
				// 移動と複製の区別が、旧キーが生きているかと合っていること。
				_, alive := idx.first[f.Key]
				if alive != (f.CarryKind == CarryCopied) {
					t.Errorf("移動と複製の区別が違う: %s は再生順に %v なのに %v",
						f.Key, alive, f.CarryKind)
				}
				if alive {
					if _, ok := gone[f.Key]; ok {
						t.Errorf("生きているキーが「旧だけ」に入っている: %s", f.Key)
					}
				} else if _, ok := gone[f.Key]; !ok {
					t.Errorf("旧版の再生順にも無いキーを引き継ぎ元にしている: %s", f.Key)
				}
				// note にも同じキーを入れてあるので、CSV だけを見ても分かる。
				if !strings.Contains(f.Note, f.CarryTo) {
					t.Errorf("note に引き継ぎ先が入っていない: %q", f.Note)
				}
				if !strings.Contains(f.Note, f.CarryKind.String()) {
					t.Errorf("note に移動か複製かが入っていない: %q", f.Note)
				}
			}
			if len(targets) != updateCarryover {
				t.Errorf("引き継ぎ先の種類が違う: got %d, want %d", len(targets), updateCarryover)
			}
			// 消えたキーのうち訳を持つものが、全部拾われていること。
			// 出しすぎないことと同じくらい、出すべきときに出すことが大事。
			sources := make(map[string]struct{}, len(targets))
			for _, from := range targets {
				sources[from] = struct{}{}
			}
			for k := range gone {
				if _, ok := mine[k]; !ok {
					continue
				}
				if _, matched := sources[k]; !matched {
					t.Errorf("消えたキーに引き継ぎ先を示せていない: %s", k)
				}
			}
			// 新しく現れた24キーは全部が再キー付け。本物の新規追加は0件なので、
			// 引き継ぎ元を示せないキーは残らない。
			if rest := len(fresh) - len(targets); rest != 0 {
				t.Errorf("引き継ぎ元を示せなかったキーがある: %d 件", rest)
			}
		})
	}
}

// insertedKey は挿入の試験で足す行のキー。実データのどのキーとも重ならない。
var insertedKey = key.For("dwloc diff test: a line that did not exist before")

// nodeKey は section と node の組を1つの文字列にする。
// 区切りに NUL を使うのは、CSV の列の値に現れない文字だから。
// "A" + "B/C" と "A/B" + "C" のような取り違えが起きない。
func nodeKey(section, node string) string {
	return section + "\x00" + node
}

// insertOrderRow は再生順のCSVの先頭のノードに1行足す。
//
// 足したあとはそのノードの order を振り直す。1行挿入すると後続の番号が全部ずれる、
// という現実の形をそのまま作るため。位置 (section,node,order) を引き当てる方式は
// これだけで17件中15件が誤検出になった。
//
// 足す行の台詞IDは旧版に無いものにする。「本当に新しく足された台詞」を表すので、
// 引き継ぎ元は無いのが正しい。
func insertOrderRow(t *testing.T, csvBytes []byte) []byte {
	t.Helper()
	f, err := csvfile.ReadPowerShell(csvBytes)
	if err != nil {
		t.Fatalf("再生順を読めない: %v", err)
	}
	rows := f.Rows()
	if len(rows) == 0 {
		t.Fatal("再生順が空")
	}
	target := nodeKey(rows[0].Get("section"), rows[0].Get("node"))

	header := rows[0].Columns()
	var b strings.Builder
	b.WriteString(csvfile.JoinFields(header...))
	b.WriteString(csvfile.LineTerminator)

	fields := func(r csvfile.Row) []string {
		out := make([]string, len(header))
		for i, name := range header {
			out[i] = r.Get(name)
		}
		return out
	}
	set := func(values []string, name, value string) {
		for i, col := range header {
			if csvfile.FoldASCII(col) == csvfile.FoldASCII(name) {
				values[i] = value
			}
		}
	}
	write := func(values []string) {
		b.WriteString(csvfile.JoinFields(values...))
		b.WriteString(csvfile.LineTerminator)
	}

	inserted := false
	for _, r := range rows {
		values := fields(r)
		if nodeKey(r.Get("section"), r.Get("node")) != target {
			write(values)
			continue
		}
		if !inserted {
			// ノードの先頭に、まだ誰も訳していない台詞を1行足す。
			extra := fields(r)
			set(extra, "key", insertedKey)
			set(extra, "line_id", "line:dwloctest")
			write(extra)
			inserted = true
		}
		// 足した分だけ後続の番号がずれる。
		if n, err := strconv.Atoi(strings.TrimSpace(r.Get("order"))); err == nil {
			set(values, "order", strconv.Itoa(n+1))
		}
		write(values)
	}
	if !inserted {
		t.Fatal("行を挿入できなかった")
	}
	return []byte(b.String())
}

// TestRealDataCarryoverSurvivesInsertedLine は、台詞を1行足しても引き継ぎ候補が
// 1件も変わらないことを確かめる。
//
// 足す行は、キーが変わった25行が並ぶノードの先頭に入れ、そのノードの order を
// 振り直す。1行の挿入で後続の番号が全部ずれるという現実の形を作るためで、
// 位置 (section,node,order) を引き当てる方式はこれだけで壊れた。
//
// 台詞IDで突き合わせるいまの方式は、挿入も並べ替えも原理的に効かない。それでも
// 実データの規模で固定しておくのは、「位置を見ない」という設計がのちの変更で
// 崩れていないことを、毎回測って確かめられるようにするため。
func TestRealDataCarryoverSurvivesInsertedLine(t *testing.T) {
	base := gameUpdateRepo(t)
	baseRep := Compare(base, nil)
	want := carryPairs(baseRep, "ja")
	if len(want) != updateCarryover {
		t.Fatalf("挿入前の候補が違う: got %d, want %d", len(want), updateCarryover)
	}

	repo := gameUpdateRepoWith(t, func(csvBytes []byte) []byte {
		return insertOrderRow(t, csvBytes)
	})
	if len(repo.Order.Entries) != len(base.Order.Entries)+1 {
		t.Fatalf("挿入できていない: got %d 行, want %d 行",
			len(repo.Order.Entries), len(base.Order.Entries)+1)
	}
	rep := Compare(repo, nil)

	for _, sum := range rep.Locales {
		got := carryPairs(rep, sum.Locale)
		if len(got) != updateCarryover {
			t.Errorf("%s: 挿入後の候補が違う: got %d, want %d", sum.Locale, len(got), updateCarryover)
		}
		for from, to := range want {
			if got[from] != to {
				t.Errorf("%s: 候補が変わった: %s -> %s（挿入前は %s）", sum.Locale, from, got[from], to)
			}
		}
		for _, f := range rep.Findings {
			if f.Locale != sum.Locale || f.Category != CatCarryover {
				continue
			}
			if f.CarryTo == insertedKey {
				t.Errorf("%s: 足した行を引き継ぎ先にしている: %s", sum.Locale, f.Key)
			}
		}
		if sum.Counts[CatVanished] != updateVanished {
			t.Errorf("%s: 台本から消えた行が変わった: got %d, want %d",
				sum.Locale, sum.Counts[CatVanished], updateVanished)
		}
	}
}

// TestRealDataCarryoverNeedsNoRewrite は、引き継ぎ候補を出しても公開ファイルの
// 行をひとつも書き換えないことを確かめる。報告するだけの機能であることの裏取り。
func TestRealDataCarryoverNeedsNoRewrite(t *testing.T) {
	repo := gameUpdateRepo(t)
	before := make([][]Row, len(repo.Locales))
	for i, loc := range repo.Locales {
		before[i] = append([]Row(nil), loc.Published...)
	}

	Compare(repo, nil)

	for i, loc := range repo.Locales {
		if len(loc.Published) != len(before[i]) {
			t.Fatalf("%s: 行数が変わっている", loc.Name)
		}
		for j, row := range loc.Published {
			if row != before[i][j] {
				t.Errorf("%s: %d 行目が書き換わっている: %+v", loc.Name, j, row)
			}
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
