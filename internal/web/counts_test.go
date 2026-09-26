package web

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
)

// この束は、条件のチップに添える数（[countView.Rows]）が「そのチップだけを
// 選んだときに一覧へ並ぶ行数」と一致していることを見る。
//
// 画面は state.filter に入ったカテゴリ識別子を、行に付いたバッジと突き合わせる
// だけである（app.js の matchesFilter）。だから「並ぶ行数」は、応答の中で
// そのカテゴリのバッジが付いているデータ行の数と同じものになる。ここで見て
// いるのはその一致で、画面側の当てはめそのものは Go から動かせない。
//
// 件数（[countView.Count]、internal/diff が見つけた数）とは別の数である。
// 2つが食い違う場面を見本として持っておくのがこの束の要点で、食い違いは
// 事故ではなく常態である（[TestNotPublishedHasNoRowToShow]）。

// この束が使う原文。キーはこの文から作る（[key.For]）。
//
// 適当な16桁を置くと、publish は source_en のハッシュがキーと一致しない行を
// 採らないので、作業コピーの行が全部「publish で捨てられる行」になり、
// 「未翻訳」が1件も立たない（実際にそうなった）。実データと同じく、キーは
// 原文から作る。
const (
	// srcDone は訳が入っている行。再生順にも公開ファイルにもある。
	srcDone = "Hello?"
	// srcTodo は訳がまだ無い行。再生順にはあるが、公開ファイルには無い。
	// publish は訳の空の行を書かないので、実データでもこの形になる。
	srcTodo = "Hi there!"
	// srcTodo2 は同上。件数が2件になるようにもう1つ置く。1件だと、保存で
	// 1件減ったときに0件になり、減ったのか判定が変わったのかが読めない。
	srcTodo2 = "Anything else?"
	// srcVanished は公開ファイルにあるが再生順に無い行。「台本から消えた行」。
	srcVanished = "Gone from the script"
	// srcUI は公開ファイルにあるが再生順に無く、section も speaker も UI の行。
	// 「由来を判定できない行」。
	srcUI = "Settings"
)

// newCountsRoot は、この束のための翻訳リポジトリを作る。
//
// 要点は srcTodo と srcTodo2 で、再生順にはあるのに、どのロケールの公開ファイル
// にも無い。internal/diff は「どのロケールにも訳が無い行」と見るが、公開ファイルを
// 並べているかぎり、そのキーの行はファイルのどこにも無い。件数は出るのに1行も
// 出せない、という食い違いの見本である。実データでは16ロケールとも32件が
// この形だった。
func newCountsRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("data/script_order.csv", strings.Join([]string{
		"section,phase,node,order,line_id,key,speaker,condition",
		"L01 Ryan,intro,Ryan_1_intro,1,line:aaaaaaaa," + key.For(srcDone) + ",Ryan,",
		"L01 Ryan,intro,Ryan_1_intro,2,line:bbbbbbbb," + key.For(srcTodo) + ",Kobold,",
		"L01 Ryan,intro,Ryan_1_intro,3,line:cccccccc," + key.For(srcTodo2) + ",Ryan,",
		"",
	}, "\n"))

	write("Translations/ja/strings.csv", strings.Join([]string{
		"key,section,node,order,speaker,translation",
		"",
		"# ===== Level 1: Ryan (Sunny) =====",
		"# --- intro: Ryan_1_intro ---",
		key.For(srcDone) + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？",
		key.For(srcVanished) + ",L01 Ryan,Ryan_1_intro,4,Ryan," + jaVanished,
		"",
		"# ===== UI =====",
		key.For(srcUI) + ",UI,,,UI,設定",
		"",
	}, "\n"))

	write("Translations/he/strings.csv", strings.Join([]string{
		"key,section,node,order,speaker,translation",
		"",
		"# --- intro: Ryan_1_intro ---",
		key.For(srcDone) + ",L01 Ryan,Ryan_1_intro,1,Ryan,שלום",
		"",
	}, "\n"))

	return root
}

// newCountsGame は [newCountsRoot] に対応する作業コピーをゲーム側に作る。
//
// 公開ファイルにある行も全部入れる。実データの ja がそうなっていて、作業コピーを
// 並べているあいだは9カテゴリすべてで件数と行数が一致した。公開ファイルにしか
// 無い行を落とすと、実データには無い食い違いを見本にすることになる。
func newCountsGame(t *testing.T) string {
	t.Helper()

	game := filepath.Join(t.TempDir(), "BepInEx", "plugins", "DragNWashLocalization")
	dir := filepath.Join(game, "Translations", "_discovered")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"key,section,node,order,speaker,source_en,translation",
		key.For(srcDone) + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcDone + ",もしもし？",
		key.For(srcTodo) + ",L01 Ryan,Ryan_1_intro,2,Kobold," + srcTodo + ",",
		key.For(srcTodo2) + ",L01 Ryan,Ryan_1_intro,3,Ryan," + srcTodo2 + ",",
		key.For(srcVanished) + ",L01 Ryan,Ryan_1_intro,4,Ryan," + srcVanished + "," + jaVanished,
		key.For(srcUI) + ",UI,,,UI," + srcUI + ",設定",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "ja.working.csv"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return game
}

// newCountsGameWithDuplicate は、同じキーの行が2行ある作業コピーを作る。
//
// 実データの16ロケールにも ja の作業コピーにも重複キーは1件も無い（実測）。
// それでも見本を持つのは、作業コピーが Mod の書き出した CSV で、重複を止める
// 仕掛けがどこにも無いためである（dwloc validate も公開ファイルしか見ない）。
// ゲームの作業コピーの1174行目を複製して試したときは、チップ31に対して32行出た。
func newCountsGameWithDuplicate(t *testing.T) string {
	t.Helper()

	game := newCountsGame(t)
	path := filepath.Join(game, "Translations", "_discovered", "ja.working.csv")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dup := key.For(srcTodo) + ",L01 Ryan,Ryan_1_intro,2,Kobold," + srcTodo + ",\n"
	if err := os.WriteFile(path, append(body, []byte(dup)...), 0o644); err != nil {
		t.Fatal(err)
	}
	return game
}

// badgeRows は、応答の中でカテゴリごとにバッジの付いたデータ行を数える。
//
// 画面が条件を当てはめたときに残る行と同じ数え方をする。app.js は行ごとの
// バッジからカテゴリ識別子を控え（renderBadges）、選ばれた識別子を1つでも
// 持つ行を出す（matchesFilter）。ここでもバッジだけを見る。
func badgeRows(lines []lineView) map[string]int {
	out := make(map[string]int)
	for _, line := range lines {
		if line.Kind != lineKindData {
			continue
		}
		for _, b := range line.Badges {
			out[b.Category] = out[b.Category] + 1
		}
	}
	return out
}

// TestChipRowsCountTheBadgedRowsInTheList は、チップの数が「いま返している
// 一覧の中で、そのカテゴリのバッジが付いているデータ行の数」と一致することを見る。
//
// 「そのチップを押したときに並ぶ行数」ではない。並ぶ行は、保存のあと（一覧を
// 組み直さない）・隠さない行（入力欄が開いている、未保存、保存できない、競合）・
// 検索語の3つでこれとずれる。どれも待ち受けが知らない画面の状態で、Go からは
// 動かせない。だからここで押さえるのは数の出どころだけである（ずれる3つは
// doc.go の「押したときに並ぶ行数ではない」に実測を書いてある）。
//
// 作業コピーの有無で並べるファイルが変わる（公開ファイル／作業コピー）ので、
// 両方で見る。実データで件数との食い違いが出たのは前者だけだったが、この
// 一致はどちらでも同じである。
func TestChipRowsCountTheBadgedRowsInTheList(t *testing.T) {
	root := newCountsRoot(t)
	for _, tc := range []struct {
		name string
		opt  Options
	}{
		{"公開ファイルを並べる", Options{Root: root, UILang: "ja"}},
		{"作業コピーを並べる", Options{Root: root, Game: newCountsGame(t), UILang: "ja"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t, tc.opt)
			got := getLines(t, s, "ja")

			rows := badgeRows(got.Lines)
			seen := 0
			for _, c := range got.Counts {
				if c.Rows != rows[c.Category] {
					t.Errorf("%s: チップの数が %d、バッジの付く行は %d 行",
						c.Category, c.Rows, rows[c.Category])
				}
				seen = seen + c.Rows
			}
			// 全カテゴリ0行のまま通っては、確かめたことにならない。
			if seen == 0 {
				t.Error("バッジの付く行が1つも無い。見本になっていない")
			}
			// 応答に出ていないカテゴリの行が残っていないこと。件数の欄に
			// 並ばないカテゴリのバッジが行に付いていたら、そのバッジは
			// どの条件でも選べない。
			for id, n := range rows {
				if !hasCount(got.Counts, id) {
					t.Errorf("%s のバッジが %d 行に付いているのに、件数の欄に無い", id, n)
				}
			}
		})
	}
}

// hasCount は件数の欄にそのカテゴリがあるか。
func hasCount(counts []countView, id string) bool {
	for _, c := range counts {
		if c.Category == id {
			return true
		}
	}
	return false
}

// mustCount は件数の欄から1カテゴリを取り出す。無ければ落とす。
func mustCount(t *testing.T, counts []countView, id string) countView {
	t.Helper()

	c, ok := countOf(counts, id)
	if !ok {
		t.Fatalf("件数の欄に %s が無い", id)
	}
	return c
}

// TestNotPublishedHasNoRowToShow は、食い違う場面を見本として押さえる。
//
// 「どのロケールにも訳が無い行」は、そのキーがこのロケールのファイルに無い
// からこそ見つかったものである。件数には出るが、いま並べているファイルには
// 行として1つも無い。実データで公開ファイルを並べたときは、16ロケールとも
// 件数32／行0 だった。ここに32と書いて押させると、1行も出ない。
//
// 件数のほうは消さない。あれはあれで正しい（起動時の判定が見つけた件数）。
func TestNotPublishedHasNoRowToShow(t *testing.T) {
	s := newTestServer(t, Options{Root: newCountsRoot(t), UILang: "ja"})
	got := getLines(t, s, "ja")

	c := mustCount(t, got.Counts, "not_published")
	if !c.Judged {
		t.Fatalf("判定できていない: %s", c.Reason)
	}
	if c.Count == 0 {
		t.Fatal("件数が0。食い違いの見本になっていない")
	}
	if c.Rows != 0 {
		t.Errorf("チップの数が %d。このカテゴリの行はファイルに1つも無い", c.Rows)
	}
	if !c.RowsDiffer {
		t.Errorf("件数 %d と行数 %d が違うのに、食い違いとして渡していない", c.Count, c.Rows)
	}
	// 「（0 行）」と書かせないための欄。件数が立っているのに0行なのは
	// 「もう何も残っていない」ではなく「この一覧には出せない」である。
	if !c.NoRowHere {
		t.Errorf("件数 %d ／ 行数 %d なのに、行として出せないと渡していない", c.Count, c.Rows)
	}
	if n := badgeRows(got.Lines)["not_published"]; n != 0 {
		t.Errorf("バッジが %d 行に付いている。公開ファイルにそのキーの行は無いはず", n)
	}

	// 画面側の道。数の代わりに「この一覧には出せません」と書く。
	js := uiSource(t, "ui/app.js")
	if !strings.Contains(js, `t("ui.chip_no_row_here")`) {
		t.Error("行として出せないカテゴリのチップに数を出さない道が app.js に無い")
	}
	if !strings.Contains(js, "c.noRowHere") {
		t.Error("どちらを書くかを待ち受けの判断から取っていない")
	}
	for _, bad := range []string{"c.rows === 0", "c.rows == 0", "c.count > 0"} {
		if strings.Contains(js, bad) {
			t.Errorf("app.js が %q を自分で判断している。待ち受けが渡した欄を使うこと", bad)
		}
	}
}

// TestZeroRowsAndNoRowHereAreDifferent は、0件0行と「件数はあるが0行」を
// 別のものとして渡していることを見る。
//
// 件数も0の0行はそのとおり「何も無い」なので「（0 行）」でよい。件数が立って
// いるのに0行は「この一覧には出せない」で、意味が逆になる。同じ0を同じ字で
// 書くと、この違いが画面から消える。
func TestZeroRowsAndNoRowHereAreDifferent(t *testing.T) {
	s := newTestServer(t, Options{Root: newCountsRoot(t), UILang: "ja"})
	got := getLines(t, s, "ja")

	zero := 0
	for _, c := range got.Counts {
		if !c.Judged || c.Rows != 0 {
			continue
		}
		if c.Count == 0 {
			zero = zero + 1
			if c.NoRowHere {
				t.Errorf("%s: 0件0行なのに「この一覧には出せません」を渡している", c.Category)
			}
			continue
		}
		if !c.NoRowHere {
			t.Errorf("%s: 件数 %d ／ 0行なのに「（0 行）」と書かせている", c.Category, c.Count)
		}
	}
	if zero == 0 {
		t.Fatal("0件0行のカテゴリが1つも無い。見分けの見本になっていない")
	}
}

// TestWorkingCopyMakesUntranslatedShowable は、作業コピーを並べているときに
// 「未翻訳」の件数と行数が一致することを見る。
//
// 翻訳者がいちばん押す条件である。ここが食い違うと、押した先で数が合わない
// 経験を毎回することになる。実データの ja（--game あり）では 32／32 で一致した。
func TestWorkingCopyMakesUntranslatedShowable(t *testing.T) {
	s := newTestServer(t, Options{Root: newCountsRoot(t), Game: newCountsGame(t), UILang: "ja"})
	got := getLines(t, s, "ja")

	c := mustCount(t, got.Counts, "untranslated")
	if !c.Judged {
		t.Fatalf("判定できていない: %s", c.Reason)
	}
	if c.Count == 0 {
		t.Fatal("未翻訳が0件。見本になっていない")
	}
	if c.Rows != c.Count {
		t.Errorf("件数 %d と行数 %d が違う", c.Count, c.Rows)
	}
	if c.RowsDiffer {
		t.Error("一致しているのに、食い違いとして渡している")
	}

	// 作業コピーにそのキーの行があるので、「どのロケールにも訳が無い行」の
	// ほうからは外れている。件数も行数も0で、食い違わない。
	np := mustCount(t, got.Counts, "not_published")
	if np.Count != 0 || np.Rows != 0 || np.RowsDiffer {
		t.Errorf("作業コピーがあるのに件数 %d ／ 行数 %d（食い違い %v）",
			np.Count, np.Rows, np.RowsDiffer)
	}
}

// TestNotJudgedCategoriesShowNoNumber は、判定できていないカテゴリを
// 「0 行」と言い切っていないことを見る。
//
// 数そのものは渡す（押せば0行なのは本当である）が、食い違いとしては渡さない。
// 画面はこのとき数を出さず「未判定」と書く（app.js の chipRowsText）。0 と
// 書くと「もう何も残っていない」と読まれる、というのは internal/diff の
// doc.go と buildCounts が名指ししている約束そのものである。
func TestNotJudgedCategoriesShowNoNumber(t *testing.T) {
	s := newTestServer(t, Options{Root: newCountsRoot(t), UILang: "ja"})
	got := getLines(t, s, "ja")

	held := 0
	for _, c := range got.Counts {
		if c.Judged {
			continue
		}
		held = held + 1
		if c.RowsDiffer {
			t.Errorf("%s: 判定していないのに、件数と行数の食い違いを渡している", c.Category)
		}
	}
	if held == 0 {
		t.Fatal("判定できていないカテゴリが1つも無い。見本になっていない")
	}

	js := uiSource(t, "ui/app.js")
	if !strings.Contains(js, `t("ui.chip_not_judged")`) {
		t.Error("判定できていないカテゴリのチップに数を出さない道が app.js に無い")
	}
	// 理由は押した場所のそばに置く。押したあとに出るのは「条件に合う行が
	// ありません」だけで、なぜ出ないのか（作業コピーがありません、など）は
	// 件数の欄にしか無かった。375px 幅では数百px離れている。
	if !strings.Contains(js, "wrap.title = note") {
		t.Error("チップに件数の欄の文を添えていない。押しても理由が手元に出ない")
	}
	if !strings.Contains(js, "statusLabel, rows, countText(c)") {
		t.Error("チップの添え書きを件数の欄と同じ文から作っていない")
	}
}

// TestSearchGetsItsOwnEmptyMessage は、検索語のせいで0行になったときに
// 別の文言を出すことを見る。
//
// 「条件を外すと全部出ます」は、検索語のせいで0行になったときには嘘になる。
// 実データの de で zzzznotfound と打つと、条件を1つも選んでいないのに0行に
// なり、外す条件が無いのに条件を外せと言われる（外しても0行のままである）。
func TestSearchGetsItsOwnEmptyMessage(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, "function applyView()")
	if start < 0 {
		t.Fatal("applyView が無い")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("applyView の終わりが分からない")
	}
	body := js[start : start+end]
	for _, want := range []string{`t("ui.no_rows_search")`, `t("ui.no_rows")`} {
		if !strings.Contains(body, want) {
			t.Errorf("1行も出なかったときの文言に %s が無い", want)
		}
	}
	// 分けるのは検索の欄に字があるかどうかだけ。ここで行を数え直したり、
	// 条件の中身を見て理由を当てたりはしない。
	if !strings.Contains(body, "} else if (q) {") {
		t.Error("検索語の有無で分けていない")
	}
}

// TestSavedCountsReachTheChips は、保存の応答で来た数がチップに届くことを見る。
//
// 保存のあとに条件を組み直すことはできない。組み直すと、触っている最中の選択が
// 跳ねる（app.js の render の注記）。だからといって数を置いていくと、訳を1行
// 入れた直後に「32 行」と書いてあるチップが31行しか出さなくなる。数だけ写す
// 道が要る。
func TestSavedCountsReachTheChips(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, "function onSaved(")
	if start < 0 {
		t.Fatal("onSaved が無い")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("onSaved の終わりが分からない")
	}
	body := js[start : start+end]
	if !strings.Contains(body, "updateChipRows(") {
		t.Error("保存のあとにチップの数を写していない。訳を入れた行のぶんだけ古くなる")
	}
	if strings.Contains(body, "buildFilters(") {
		t.Error("保存のあとに条件を組み直している。触っている最中の選択が跳ねる")
	}
}

// TestSaveResponseCountsRowsToo は、保存の応答にも行数が入っていることを見る。
//
// 画面側の道（[TestSavedCountsReachTheChips]）があっても、待ち受けが数え直して
// いなければ、写る値が古いままになる。
func TestSaveResponseCountsRowsToo(t *testing.T) {
	root := newCountsRoot(t)
	s := newTestServer(t, Options{Root: root, Game: newCountsGame(t), UILang: "ja"})

	before := getLines(t, s, "ja")
	was := mustCount(t, before.Counts, "untranslated")

	// 訳が空の行を1つ探して、訳を入れる。
	line := 0
	for _, l := range before.Lines {
		if l.Kind == lineKindData && l.Key == key.For(srcTodo) {
			line = l.ID
		}
	}
	if line == 0 {
		t.Fatal("訳を入れる行が見つからない")
	}
	rec := save(t, s, "ja", before.Version,
		rowEdit{ID: line, Key: key.For(srcTodo), Translation: "こんにちは"})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
	after := decode[rowsResponse](t, rec.Body.Bytes())

	now := mustCount(t, after.Counts, "untranslated")
	if now.Count != was.Count-1 {
		t.Errorf("件数が %d → %d。1件減るはず", was.Count, now.Count)
	}
	if now.Rows != now.Count {
		t.Errorf("件数 %d と行数 %d が違う。訳が入った行はバッジも落ちている", now.Count, now.Rows)
	}
	if now.RowsDiffer {
		t.Error("1行訳しただけで、件数と行数が食い違っている")
	}
}

// TestDuplicateKeyRowsLoseTheBadgeTogether は、同じキーの行が2行あるとき、
// 片方を保存すると2行ともバッジを落とすこと、そして画面もそう描き直すことを見る。
//
// 待ち受けはキー単位でバッジを付け直す（badgesByKey）。訳が入ったキーのバッジは
// そのキーのどの行からも落ち、行数もそのぶん減る。画面が応答に入っている行だけを
// 描き直すと、もう一方が古いバッジを付けたまま残り、チップの数だけが2つぶん減る。
// しかも件数と行数は待ち受けの中では一致してしまうので、食い違いの断りも出ない。
func TestDuplicateKeyRowsLoseTheBadgeTogether(t *testing.T) {
	root := newCountsRoot(t)
	s := newTestServer(t, Options{Root: root, Game: newCountsGameWithDuplicate(t), UILang: "ja"})

	before := getLines(t, s, "ja")
	was := mustCount(t, before.Counts, "untranslated")
	if was.Rows != was.Count+1 {
		t.Fatalf("件数 %d ／ 行数 %d。重複した1行ぶん多いはず", was.Count, was.Rows)
	}

	line := 0
	for _, l := range before.Lines {
		if l.Kind == lineKindData && l.Key == key.For(srcTodo) && line == 0 {
			line = l.ID
		}
	}
	if line == 0 {
		t.Fatal("訳を入れる行が見つからない")
	}
	rec := save(t, s, "ja", before.Version,
		rowEdit{ID: line, Key: key.For(srcTodo), Translation: "こんにちは"})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d: %s", rec.Code, rec.Body.String())
	}
	after := decode[rowsResponse](t, rec.Body.Bytes())

	now := mustCount(t, after.Counts, "untranslated")
	if now.Rows != was.Rows-2 {
		t.Errorf("行数が %d → %d。同じキーの2行とも落ちるはず", was.Rows, now.Rows)
	}
	// 読み直すと、複製したほうの行からもバッジが消えている。
	again := getLines(t, s, "ja")
	if n := badgeRows(again.Lines)["untranslated"]; n != now.Rows {
		t.Errorf("バッジの付く行が %d 行、チップの数が %d", n, now.Rows)
	}

	// 画面側の道。応答に入っている行だけでなく、同じキーの行を描き直すこと。
	js := uiSource(t, "ui/app.js")
	if !strings.Contains(js, "function renderBadgesForKey(") {
		t.Fatal("キーで描き直す道が app.js に無い")
	}
	if !strings.Contains(js, "renderBadgesForKey(entry, r.badges)") {
		t.Error("保存の結果を、同じキーの行へ描き直していない")
	}
	if !strings.Contains(js, "state.byKey.get(entry.key)") {
		t.Error("同じキーの行を引く控えが無い")
	}
}

// TestNoticesStartFolded は、近道の一覧・絞り込みの断り書き・起動したときの
// 状態の一帯・書き出しが、どれも畳んだ状態で始まることを見る。
//
// 実測（実データの ja、1721行）で、2つとも開くと一覧の始まりが 1280幅で
// 671.0px → 821.3px、375幅で 1212.6px → 1669.9px まで下がる。読むのは1度で
// 足りるのに、一覧より上にずっと居座っていた。
//
// 一帯（#panel-fold）の中身は [TestPanelFoldKeepsTheFilePathReadable] が見る。
// あちらは「畳んでもファイル名が読めること」を見る試験で、ここは「3つとも
// 閉じて始まること」を見る試験である。
func TestNoticesStartFolded(t *testing.T) {
	html := uiSource(t, "ui/index.html")

	for _, id := range []string{"keys", "finder-note"} {
		// 中身を書き込む先は今までどおりの p のまま。app.js が書き込む先を
		// summary へ移すと、開いた瞬間に見出しが本文へ入れ替わる。
		if !strings.Contains(html, `<p id="`+id+`" class="notice keys">`) {
			t.Errorf("%s が畳みの中の p でなくなっている", id)
		}
		if !strings.Contains(html, `<summary id="`+id+`-title">`) {
			t.Errorf("%s に見出し（summary）が無い。畳むと何が入っているか読めない", id)
		}
	}
	// 書き出しは畳みではなくなった。帯のボタンとメニュー（popover）で、押さえは
	// [TestExportMenuLivesInTheBar]。
	if n := strings.Count(html, ` class="fold" hidden>`); n != 3 {
		t.Errorf("畳みが %d 個。近道の一覧・絞り込みの断り書き・起動したときの状態の3つのはず", n)
	}
	if strings.Contains(html, `class="fold" open`) || strings.Contains(html, "<details open") {
		t.Error("畳みが開いた状態で始まっている")
	}
	// 文言が入るまでは出さない。summary は焦点を受ける要素なので、目録が届く
	// までのあいだ、名前を持たない開閉要素が並ぶ（実測: 空でも高さ 36.19px、
	// tabIndex 0、focus() が通る）。出すのは、近道の一覧と絞り込みの断り書きが
	// app.js の applyCatalog、一帯（#panel-fold）は render である。あれの
	// summary に入るのは目録の文ではなく値なので、目録だけでは名前が入らない
	// （[TestPanelFoldKeepsTheFilePathReadable] が並びを見る）。
	js := uiSource(t, "ui/app.js")
	for _, want := range []string{
		"el.keysFold.hidden = false;",
		"el.finderFold.hidden = false;",
		"el.panelFold.hidden = false;",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js に %q が無い。文言が入っても畳みが出てこない", want)
		}
	}
	// 条件の群の読み上げに触らないこと。断り書きは .finder の外の兄弟のままで、
	// 中へ移すと群の関係が切れる。
	if !strings.Contains(html, `<span id="filters" class="filters" role="group" aria-labelledby="filter-label">`) {
		t.Error("条件の群の role と aria-labelledby が変わっている")
	}
	assertFoldsHoldNoLiveRegion(t, html)
}

// TestPanelFoldKeepsTheFilePathReadable は、起動したときの状態の一帯を畳んでも
// 「ファイル」の欄が読めることを見る。
//
// 畳みきってはいけない一帯である。「いま直しているのがどのファイルか」は、
// リポジトリの公開ファイルなのかゲーム側の作業コピーなのかを画面から読める
// 唯一の場所で、閉じた details の中身は支援技術の木からも外れる（実測:
// 閉じた畳みの中の p は checkVisibility() が false）。だから #file-path だけは
// summary の中に置いてある。
//
// 畳めているかどうかは [TestNoticesStartFolded] が見る。ここは「畳んでも
// ファイル名が残ること」だけを見る。
func TestPanelFoldKeepsTheFilePathReadable(t *testing.T) {
	html := uiSource(t, "ui/index.html")

	fold := foldBody(t, html, "panel-fold")
	summary := between(t, fold, "<summary>", "</summary>")

	// ファイル名は summary の中。ここから出ると、畳んだ画面から消える。
	if !strings.Contains(summary, `id="file-path"`) {
		t.Error("#file-path が summary の中に無い。畳むとどのファイルを直しているか読めない")
	}
	// summary が受け付けるのは語句なので p は置けない。中身を入れる先の id は
	// 変えていないので、app.js から見た書き込み先は今までどおりである。
	if !strings.Contains(summary, `<span id="file-path" class="path">`) {
		t.Error("#file-path が summary の中の span でない")
	}
	// 中に見張りを入れないこと。[assertFoldsHoldNoLiveRegion] が頁全体で同じことを
	// 見ているが、あちらは畳みを字面で数えて回るので、この畳みが数えられている
	// かどうかはここで直に押さえる。
	for _, bad := range []string{"aria-live", `role="status"`, `role="alert"`} {
		if strings.Contains(fold, bad) {
			t.Errorf("一帯の畳みの中に %s がある。閉じているあいだ木から外れて告知されない", bad)
		}
	}

	// 残りは畳みの中。summary の外にあること。
	for _, id := range []string{"game-path", "notes", "counts", "stats"} {
		if strings.Contains(summary, `id="`+id+`"`) {
			t.Errorf("#%s が summary の中にある。畳んでも隠れない", id)
		}
		if !strings.Contains(fold, `id="`+id+`"`) {
			t.Errorf("#%s が畳みの外にある。畳んでも残ってしまう", id)
		}
	}

	// 画面側。ファイル名を書き込む先は el.path のまま。見出しに添える字は
	// 目録から入れる（画面に文字列を直接書かない）。
	js := uiSource(t, "ui/app.js")
	for _, want := range []string{
		`el.path.textContent = t("ui.file") + ": " + data.path;`,
		`el.panelMore.textContent = t("ui.panel_more");`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js に %q が無い", want)
		}
	}

	// 出すのは名前が入ったあと。目録が届いた時点（applyCatalog）で出すと、
	// --locale を省いた起動ではロケールを選ぶまで /api/lines を叩かないので、
	// 添え字だけの畳みが焦点の順に残る（実測: 1280幅で一帯の高さ 47.4px、
	// tabIndex 0、#file-path と #notes と #counts はどれも空）。--locale の
	// 既定は省略で、ダブルクリックで開いたときもこの経路である。
	if body := functionBody(t, js, "applyCatalog"); strings.Contains(body, "el.panelFold.hidden") {
		t.Error("一帯の畳みを applyCatalog で出している。ロケールを選ぶまで名前が入らない")
	}
	body := functionBody(t, js, "render")
	pathAt := strings.Index(body, `el.path.textContent = t("ui.file")`)
	showAt := strings.Index(body, "el.panelFold.hidden = false;")
	if pathAt < 0 || showAt < 0 {
		t.Fatalf("render がファイル名を入れて畳みを出していない（名前 %d、畳み %d）",
			pathAt, showAt)
	}
	if showAt < pathAt {
		t.Error("ファイル名を入れる前に一帯の畳みを出している")
	}
}

// functionBody は app.js の関数1つの中身を返す。
//
// 名前で引き、2字下げの閉じ括弧までを切り出す。この頁の関数はすべて即時関数の
// 中にあり、2字下げで書かれている。字面で追うのは、この試験集がブラウザーを
// 起こさずに app.js を読む作りだからである。
func functionBody(t *testing.T, js, name string) string {
	t.Helper()

	at := strings.Index(js, "function "+name+"(")
	if at < 0 {
		t.Fatalf("app.js に %s が無い", name)
	}
	rest := js[at:]
	end := strings.Index(rest, "\n  }")
	if end < 0 {
		t.Fatalf("%s の終わりが分からない", name)
	}
	return rest[:end]
}

// foldBody は id の畳みの中身（<details> から </details> まで）を返す。
func foldBody(t *testing.T, html, id string) string {
	t.Helper()

	at := strings.Index(html, `<details id="`+id+`"`)
	if at < 0 {
		t.Fatalf("#%s が畳み（details）でない", id)
	}
	end := strings.Index(html[at:], "</details>")
	if end < 0 {
		t.Fatalf("#%s の畳みの終わりが分からない", id)
	}
	return html[at : at+end]
}

// between は開きと閉じに挟まれた部分を返す。
func between(t *testing.T, body, opening, closing string) string {
	t.Helper()

	at := strings.Index(body, opening)
	if at < 0 {
		t.Fatalf("%s が無い", opening)
	}
	rest := body[at+len(opening):]
	end := strings.Index(rest, closing)
	if end < 0 {
		t.Fatalf("%s が閉じていない", opening)
	}
	return rest[:end]
}

// assertFoldsHoldNoLiveRegion は、畳みの中に見張り（aria-live / role="status" /
// role="alert"）が入っていないことを見る。
//
// 閉じた details は中身を描かないので、支援技術の木からも外れる（実測:
// 閉じた畳みの中の p は checkVisibility() が false）。[TestAlertsAreAnnounced] が
// #message と #empty に hidden を使わせないのと同じ理由がここにも要る。いまは
// 見張り（#shown・#save-state・#message・#export-state・#empty）が5つとも畳みの
// 外にあるが、外から見張っていないと、あとから中へ移しても何も落ちない。
// 書き出しのメニュー（popover）も閉じると同じく木から外れる。あちらは
// [TestExportMenuLivesInTheBar] が見る。
func assertFoldsHoldNoLiveRegion(t *testing.T, html string) {
	t.Helper()

	rest := html
	for {
		at := strings.Index(rest, `class="fold"`)
		if at < 0 {
			return
		}
		end := strings.Index(rest[at:], "</details>")
		if end < 0 {
			t.Fatal("畳みの終わりが分からない")
		}
		fold := rest[at : at+end]
		for _, bad := range []string{"aria-live", `role="status"`, `role="alert"`} {
			if strings.Contains(fold, bad) {
				t.Errorf("畳みの中に %s がある。閉じているあいだ木から外れて告知されない", bad)
			}
		}
		rest = rest[at+end:]
	}
}

// TestUIKeysAreInBothCatalogs は、app.js が引く鍵が ja と en の両方にあることを見る。
//
// 目録に無い鍵を引くと、t は鍵そのものを返す。画面に "ui.chip_rows" と出て、
// 落ちも警告もしない。鍵を足すときに片方の目録へ入れ忘れるのがいちばん
// 起きやすいので、字面で押さえる。
func TestUIKeysAreInBothCatalogs(t *testing.T) {
	js := uiSource(t, "ui/app.js")
	ja := uiSource(t, "ui/i18n/ja.json")
	en := uiSource(t, "ui/i18n/en.json")

	for _, m := range regexp.MustCompile(`\bt\("([a-z0-9_.]+)"`).FindAllStringSubmatch(js, -1) {
		for _, c := range []struct {
			lang string
			body string
		}{{"ja", ja}, {"en", en}} {
			if !strings.Contains(c.body, `"`+m[1]+`":`) {
				t.Errorf("%s.json に %s が無い。画面に鍵がそのまま出る", c.lang, m[1])
			}
		}
	}
}
