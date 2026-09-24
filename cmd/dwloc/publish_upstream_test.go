package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

/*
ここは、いまの dwloc publish の振る舞いを、上流の tools/hash-strings.ps1 を通しで
走らせた結果と突き合わせて固定する試験である（全体を解釈する読み手へ移す作業の PR0）。

入力は testdata/upstream/cases.json、上流の結果は testdata/upstream/expected.json の
run にある（作り方は testdata/upstream/README.md）。入力ごとに、上流と同じ
tools/・data/・Translations/ の並びを一時ディレクトリに作り、dwloc publish --no-game を
走らせる。書けば出力のバイトと集計の1行を、止まればどこで止まったかを上流と比べる。

上流と違ってよいのは [publishDiffs] に載せた入力だけで、どう違うかまで固定する。
pubUndecided の行は、上流と違えてよいかをまだ決めていない点で、いまの振る舞いを
固定するだけである。意図して違える点と混ぜないのは、仕様の上で決まったように
読まれないためである。

PR0 と PR1 では、publish が全体を解釈する読み手へ移る PR2 で振る舞いが変わる箇所を
「PR2 で変わる」として分けていた。PR2 で publish が全体を解釈して読むようになり、
その行は、上流と同じバイトを書くようになったもの（表から外した）と、止めることを
意図したもの（飲み込み、単独の CR、'#' で始まるヘッダー。pubIntended へ移した）の
どちらかになった。分類そのものも無くした。

集計の1行のうち「kept from the published file」は比べない。dwloc の集計の1行には
この項目が無い（上流の #10。いまの公開ファイルにだけある行を引き継ぐ処理は、この
移植の範囲外で、追従するかはまだ決めていない。いまの dwloc は訳が失われる確かめで
止まる）。
*/

// publishFixtureDir は入力の表と正解の置き場。
var publishFixtureDir = filepath.Join("..", "..", "testdata", "upstream")

// publishFixtureLocale は、上流の通しの実行で使ったロケールの名前。
const publishFixtureLocale = "xx"

// publishFixture は cases.json と expected.json のうち、この試験が使う部分。
type publishFixture struct {
	PublishedDefault string `json:"published_default"`
	Data             struct {
		ScriptOrder string `json:"script_order"`
		LevelFlow   string `json:"level_flow"`
	} `json:"data"`
	Cases []publishFixtureCase `json:"cases"`
}

// publishFixtureCase は入力1つ。
type publishFixtureCase struct {
	Name string `json:"name"`
	// As は text をどこに置くか。working なら作業コピー、published なら公開ファイル。
	As   string `json:"as"`
	Text string `json:"text"`
	// Published は As が working のときの公開ファイル。無ければ published_default。
	Published *string `json:"published"`
}

// publishExpected は expected.json の run の部分。
type publishExpected struct {
	CasesSHA256 string `json:"cases_sha256"`
	Cases       []struct {
		Name string `json:"name"`
		Run  struct {
			Error *struct {
				ID string `json:"id"`
			} `json:"error"`
			Summary *struct {
				Converted         int `json:"converted"`
				AlreadyHashed     int `json:"already_hashed"`
				PerLine           int `json:"per_line"`
				MalformedDropped  int `json:"malformed_dropped"`
				KeptFromPublished int `json:"kept_from_published"`
				InPlayOrder       int `json:"in_play_order"`
				Other             int `json:"other"`
			} `json:"summary"`
			Output *string `json:"output"`
		} `json:"run"`
	} `json:"cases"`
}

// loadPublishFixture は入力の表と正解を読み、正解が今の表から作られたものかを確かめる。
func loadPublishFixture(t *testing.T) (publishFixture, publishExpected) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(publishFixtureDir, "cases.json"))
	if err != nil {
		t.Fatalf("入力の表が読めない: %v", err)
	}
	var cases publishFixture
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("入力の表を解釈できない: %v", err)
	}
	expRaw, err := os.ReadFile(filepath.Join(publishFixtureDir, "expected.json"))
	if err != nil {
		t.Fatalf("正解が読めない: %v", err)
	}
	var exp publishExpected
	if err := json.Unmarshal(expRaw, &exp); err != nil {
		t.Fatalf("正解を解釈できない: %v", err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != exp.CasesSHA256 {
		t.Fatalf("正解は別の入力の表から作られている（表 %s、正解 %s）。scripts/upstream-fixtures.ps1 で作り直す",
			got, exp.CasesSHA256)
	}
	if len(exp.Cases) != len(cases.Cases) {
		t.Fatalf("入力 %d 件に正解が %d 件", len(cases.Cases), len(exp.Cases))
	}
	for i := range cases.Cases {
		if cases.Cases[i].Name != exp.Cases[i].Name {
			t.Fatalf("%d 件目の名前が表 %q と正解 %q で違う", i, cases.Cases[i].Name, exp.Cases[i].Name)
		}
	}
	return cases, exp
}

// publishView は、publish を1回走らせた結果を比べやすい形にしたもの。
type publishView struct {
	// outcome は書いたか、どこで止まったか。
	outcome string
	// output と summary は書いたときだけ埋まる。summary は集計の1行の数で、
	// converted、already hashed、per-line、malformed dropped、in play order、other の順。
	output  string
	summary [6]int
}

const (
	outcomeWrote  = "書く"
	outcomeFailed = "変換できない"
)

// summaryLabels は publishView.summary の各項目の名前（集計の1行の文言）。
var summaryLabels = [6]string{"converted", "already hashed", "per-line", "malformed dropped", "in play order", "other"}

// dwlocSummary は dwloc の集計の1行（publish.Stats.LogLine）から数を取り出す。
var dwlocSummary = regexp.MustCompile(`: (\d+) converted, (\d+) already hashed, (\d+) per-line, ` +
	`(\d+) malformed dropped, (\d+) in play order, (\d+) other$`)

// lossCount は、失われる訳の報告の最後の行から件数を取り出す。
var lossCount = regexp.MustCompile(`失われる訳が (\d+) 件あります`)

// upstreamPublish は正解の run を publishView にする。上流が例外で止まったときは
// 何も書かない（ConvertFrom-Csv の列名の重複など）。
func upstreamPublish(t *testing.T, name string, exp publishExpected, i int) publishView {
	t.Helper()
	run := exp.Cases[i].Run
	if run.Error != nil {
		return publishView{outcome: outcomeFailed}
	}
	if run.Summary == nil || run.Output == nil {
		t.Fatalf("%s: 上流の正解に集計か出力が無い", name)
	}
	s := run.Summary
	return publishView{
		outcome: outcomeWrote,
		output:  *run.Output,
		summary: [6]int{s.Converted, s.AlreadyHashed, s.PerLine, s.MalformedDropped, s.InPlayOrder, s.Other},
	}
}

// publishFixturePublished は、上流の通しの実行で使った公開ファイルのルート相対パス。
const publishFixturePublished = "Translations/" + publishFixtureLocale + "/strings.csv"

// fixtureRun は、上流の通しの実行と同じ並びのリポジトリを作り、
// dwloc publish --no-game を走らせて、そのリポジトリと結果をそのまま返す。
//
// extra は dwloc publish に足す指定（--accept-multiline など）。
func fixtureRun(t *testing.T, cases publishFixture, i int, extra ...string) (root string, code int, stdout, stderr string) {
	t.Helper()
	c := cases.Cases[i]
	published := publishFixturePublished
	files := map[string]string{
		"data/script_order.csv": cases.Data.ScriptOrder,
		"data/level_flow.csv":   cases.Data.LevelFlow,
	}
	switch c.As {
	case "working":
		files["Translations/_discovered/"+publishFixtureLocale+".working.csv"] = c.Text
		files[published] = cases.PublishedDefault
		if c.Published != nil {
			files[published] = *c.Published
		}
	case "published":
		files[published] = c.Text
	default:
		t.Fatalf("%s: as の値が分からない: %q", c.Name, c.As)
	}
	root = makeTree(t, files)
	code, stdout, stderr = runCLI(append([]string{"publish", "--root", root, "--no-game"}, extra...)...)
	return root, code, stdout, stderr
}

// runFixturePublish は [fixtureRun] で dwloc publish を走らせ、結果を比べやすい形に
// する。
func runFixturePublish(t *testing.T, cases publishFixture, i int, extra ...string) publishView {
	t.Helper()
	c := cases.Cases[i]
	root, code, stdout, stderr := fixtureRun(t, cases, i, extra...)
	switch code {
	case exitOK:
		m := dwlocSummary.FindStringSubmatch(strings.TrimSpace(strings.SplitN(stdout, "\n", 2)[0]))
		if m == nil {
			t.Fatalf("%s: 集計の1行が読めない\n%s", c.Name, stdout)
		}
		v := publishView{outcome: outcomeWrote, output: readFile(t, root, publishFixturePublished)}
		for k := range v.summary {
			v.summary[k], _ = strconv.Atoi(m[k+1])
		}
		return v
	case exitError:
		return publishView{outcome: outcomeFailed}
	case exitProblems:
		return publishView{outcome: stopReason(t, c.Name, root, stderr)}
	}
	t.Fatalf("%s: 終了コード %d\n%s", c.Name, code, stderr)
	return publishView{}
}

// stopReason は、publish が終了コード1で止まった理由を短く書く。形の崩れで
// 止まったときは、どの形をどの行で見つけたかを publish.CheckTargetShape に尋ねる
// （publish が止まるときに呼ぶのと同じ関数）。
func stopReason(t *testing.T, name, root, stderr string) string {
	t.Helper()
	if m := lossCount.FindStringSubmatch(stderr); m != nil {
		return "止まる（失われる訳 " + m[1] + " 件）"
	}
	if !strings.Contains(stderr, "読み違える形が") {
		t.Fatalf("%s: 止まった理由が分からない\n%s", name, stderr)
	}
	targets, err := publish.DiscoverTargets(root)
	if err != nil || len(targets) != 1 {
		t.Fatalf("%s: 対象を決められない: %v %+v", name, err, targets)
	}
	hazards, err := publish.CheckTargetShape(targets[0])
	if err != nil {
		t.Fatalf("%s: CheckTargetShape: %v", name, err)
	}
	var found []string
	for _, h := range hazards {
		where := "入力"
		if h.Current {
			where = "公開ファイル"
		}
		found = append(found, fmt.Sprintf("%s %s %s", where, lineRange(h.Line, h.EndLine), h.Why.ID))
	}
	return "止まる（形: " + strings.Join(found, "、") + "）"
}

// describePublishDiff は、上流と dwloc の publish の違いを1行ずつの説明にする。
// 同じなら空を返す。
func describePublishDiff(up, got publishView) []string {
	if up.outcome != got.outcome {
		return []string{fmt.Sprintf("上流 %s / dwloc %s", up.outcome, got.outcome)}
	}
	if up.outcome != outcomeWrote {
		return nil
	}
	var out []string
	for k := range up.summary {
		if up.summary[k] != got.summary[k] {
			out = append(out, fmt.Sprintf("%s: 上流 %d / dwloc %d", summaryLabels[k], up.summary[k], got.summary[k]))
		}
	}
	if up.output != got.output {
		ul, gl := strings.Split(up.output, "\n"), strings.Split(got.output, "\n")
		for n := 0; n < max(len(ul), len(gl)); n++ {
			u, g := lineAt(ul, n), lineAt(gl, n)
			if u != g {
				out = append(out, fmt.Sprintf("出力の %d 行目: 上流 %q / dwloc %q", n+1, u, g))
				break
			}
		}
	}
	return out
}

// lineAt は n 番目の行を返す。無ければ「（無い）」。
func lineAt(lines []string, n int) string {
	if n < len(lines) {
		return lines[n]
	}
	return "（無い）"
}

// publishDiffKind は、上流と違ってよい理由の分類。
type publishDiffKind string

const (
	// pubIntended は、上流と意図して違える点（docs/port-spec.md「上流と意図して
	// 違える点」）。
	pubIntended publishDiffKind = "意図して違える"
	// pubUndecided は、上流と違うが、違えてよいかをまだ決めていない点
	// （docs/port-spec.md「上流と違うが未決の点」）。いまの振る舞いを固定する
	// だけで、正しいとはしない。決まったら pubIntended へ移すか、直して表から外す。
	pubUndecided publishDiffKind = "未決"
)

// knownPublishDiff は、上流と違ってよい入力1つ。
type knownPublishDiff struct {
	kind publishDiffKind
	// why はなぜ違うか。
	why string
	// diff は [describePublishDiff] の出力をそのまま固定したもの。
	diff []string
}

// 上流と違う理由。
const (
	whyPubAcceptable = "正しい複数行の値だが、続きの行が単独で読むとレコードに見える（2列の作業コピーでは" +
		"区切りの数がヘッダーの列数 2 と同じ、7列では原文の2行目がカンマを多く含み7列）。飲み込みの確かめ (f) で止め、" +
		"確かめたうえでレコード単位で通す指定（--accept-multiline <ロケール>:<key>）で上流と同じバイトを書く" +
		"（[TestPublishAcceptMultilineMatchesUpstream]）"
	whyPubHeaderColumns = "ヘッダーに key 列も source_en 列も無いか、translation 列が無いので、形の確かめ (a) で止める。" +
		"上流はすべての行を捨て、ヘッダーとコメントだけを書く"
	whyPubUnclosed = "閉じない引用符 (e)。上流は後ろの行（英語の原文を含む）を訳に飲み込んで書く（上流の報告 #11）。" +
		"dwloc は読み手の型付きの誤りを、形の確かめで終了コード1と直し方の案内にして止める。ヘッダーで開いたときは、" +
		"列名にファイルの終わりまでが入るので、(a) の列が無いとは言わない"
	whyPubSwallow = "引用符が別の行で閉じ、後ろの行を飲み込む形。上流は英語の原文やキーを訳に入れて書く。" +
		"dwloc は飲み込みの確かめ (f) で止める（csvfile.FindSwallows）。続きの行がレコードに見える形は" +
		"確かめたうえで通す指定で通せるが、閉じ引用符の後ろに文字が続く形は通せない"
	whyPubLoneCRQuoted = "引用した訳の中の単独の CR。上流はその行を失う（Remove-NonRecords が単独の CR を行末と見なさない）。" +
		"dwloc は単独の CR として止め、LF に直すよう案内する（決まったことのそのほか 1）"
	whyPubLoneCRSource = "原文（source_en）の中の単独の CR。上流は Remove-NonRecords が単独の CR を行末と見なさず、" +
		"CR の手前を捨て、CR の後ろの残り（閉じ引用符を含む）を最初の列にしたレコードを読む。7列の作業コピー（key 列のキー、" +
		"台詞ID）ではその行を書かず、2列の作業コピーではその残りから作った別のキー（ゲームが引かないキー）で訳を書く" +
		"（上流の不具合。写さない）。dwloc は形の確かめ (g) で止め、原文を直してよいかをその行のキーの決まり方で分けて" +
		"案内する（決まったことの 19 と 20。publish.LoneCRKeyKind）。2列では続きの行が2列に見えるので飲み込みの確かめ (f) にも" +
		"当たり、そのレコードを指定で通しても (g) で止まる（[TestPublishAcceptMultilineKeepsOtherStops]）"
	whyPubLoneCR         = "単独の CR も行の区切りにして読む。上流は単独の CR の手前を捨て、その行の訳を失う（上流の不具合。写さない）"
	whyPubLoneCRUnquoted = "引用符で囲まない値の中の単独の CR（どちらの道具の書き手も作らない、手で書いた形）。" +
		"ゲームは引用の外の CR を捨てて「いち」と読み、上流はその行を失う。dwloc は行の区切りの規則のまま読むと" +
		"切れた訳「い」を公開してしまうので、形の確かめ (g) で止め、CR を取り除くか値を引用符で囲むよう案内する" +
		"（決まったことの 11 と 14。見つけるのは csvfile.FindCRCuts）"
	whyPubCROnly     = "CR だけで改行したファイルも読む。上流は全行を失い、ヘッダーだけを書く（上流の不具合。写さない）"
	whyPubCommaRow   = "',' だけの行を黙って落とす。上流は空のレコードとして malformed dropped に数える。違うのは集計の数だけ"
	whyPubHashHeader = "'#' で始まるヘッダー（読んだ最初の列名が '#' で始まるもの）。上流はそのヘッダーを飛ばして" +
		"データの行をヘッダーにし、訳を1行も公開しない。dwloc は写さず、形の確かめ (a) で止める（決まったことの 12 と 15）。" +
		"(a) を「key 列が無い」に広げると、正当な source_en,translation の2列の作業コピーまで止まるので、" +
		"'#' を見る判定を別に置く"
	whyPubBareQuote = "裸の引用符の後ろのコメント行や見出しを、引用の外として落とす。上流はレコードとして読み、" +
		"列が多ければ公開ファイルに書く（上流の不具合。写さない）"
	whyPubCultureHash = "U+00AD のあとに '#' が続く行をデータとして読み、malformed dropped に数える。" +
		"上流の StartsWith('#') はカルチャに依存する照合でコメントとして落とす（Go では照合表を持てない）"
	whyPubEnglishKey = "key 列の英文をハッシュにして公開する上流の #9。追従するかは、全体を解釈する読み手へ移す" +
		"この作業の範囲外で、まだ決めていない。いまの dwloc はその行を捨てるので、いまの公開ファイルの訳が失われるとして止める"
	whyPubKeptFromPublished = "いまの公開ファイルにだけある行を引き継ぐ上流の #10。追従するかは、全体を解釈する読み手へ" +
		"移すこの作業の範囲外で、まだ決めていない。いまの dwloc は引き継がず、その訳が失われるとして止める"
)

// publishDiffs は、いまの dwloc publish が上流と違う入力。
var publishDiffs = map[string]knownPublishDiff{
	// 行をまたぐ訳・原文・台詞ID行の訳（ml-translation-*、ml-source-translated、
	// ml-published-*、ml-hash-line-in-*、ml-line-id-translation）と、表計算ソフトで
	// 保存し直した形（ml-source-crlf-key-mismatch。上流と同じくその行を落として書き、
	// 原文の CRLF を LF にするとキーが合うことを知らせる）は、上流と同じバイトを書く
	// ので、ここに無い。
	"ml-source-translated-2col": {pubIntended, whyPubAcceptable, []string{
		`上流 書く / dwloc 止まる（形: 入力 3〜5行目 publish_swallow_same_columns）`,
	}},
	"ml-continuation-looks-like-row": {pubIntended, whyPubAcceptable, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_swallow_same_columns）`,
	}},
	"ml-header": {pubIntended, whyPubHeaderColumns, []string{
		`上流 書く / dwloc 止まる（形: 入力 1〜2行目 publish_no_translation_column）`,
	}},
	"unclosed-to-eof": {pubIntended, whyPubUnclosed, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_unclosed_quote）`,
	}},
	"unclosed-last-line": {pubIntended, whyPubUnclosed, []string{
		`上流 書く / dwloc 止まる（形: 公開ファイル 3行目 publish_unclosed_quote）`,
	}},
	"unclosed-header": {pubIntended, whyPubUnclosed, []string{
		`上流 書く / dwloc 止まる（形: 入力 1〜2行目 publish_unclosed_quote）`,
	}},
	"unclosed-published-middle": {pubIntended, whyPubUnclosed, []string{
		`上流 書く / dwloc 止まる（形: 公開ファイル 2〜5行目 publish_unclosed_quote）`,
	}},
	"swallow-3col": {pubIntended, whyPubSwallow, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_swallow_same_columns）`,
	}},
	"swallow-7col-hash-close": {pubIntended, whyPubSwallow, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜4行目 publish_swallow_key_shaped）`,
	}},
	"swallow-2col-hash-close": {pubIntended, whyPubSwallow, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜4行目 publish_swallow_same_columns）`,
	}},
	// 次の2件の3行目は、単独で読むとヘッダーと同じ列の数に見えるが、閉じ引用符の後ろに
	// 文字が続くので、通せない理由（text_after_quote）で止まる。
	"swallow-7col-empty-key-english": {pubIntended, whyPubSwallow, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_swallow_text_after_quote）`,
	}},
	"swallow-6col-published": {pubIntended, whyPubSwallow, []string{
		`上流 書く / dwloc 止まる（形: 公開ファイル 2〜3行目 publish_swallow_text_after_quote）`,
	}},
	"swallow-2col-own-quote": {pubIntended, whyPubSwallow, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_swallow_text_after_quote）`,
	}},
	"swallow-7col-empty-key-own-quote": {pubIntended, whyPubSwallow, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_swallow_text_after_quote）`,
	}},
	"swallow-6col-published-own-quote": {pubIntended, whyPubSwallow, []string{
		`上流 書く / dwloc 止まる（形: 公開ファイル 2〜3行目 publish_swallow_text_after_quote）`,
	}},
	"lone-cr-in-quoted-translation": {pubIntended, whyPubLoneCRQuoted, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_lone_cr）`,
	}},
	// 原文の中の単独の CR は、キーの決まり方（key 列、台詞ID、2列）ごとに1件ずつ置く。
	// 上流は、7列ではその行を書かず、2列では別のキー（f7a0c0aa975ae1e8。CR の後ろの
	// 残り `Beta"` から作ったもの）で書く。
	"lone-cr-source-key": {pubIntended, whyPubLoneCRSource, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_lone_cr）`,
	}},
	"lone-cr-source-line-id": {pubIntended, whyPubLoneCRSource, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_lone_cr）`,
	}},
	"lone-cr-source-2col": {pubIntended, whyPubLoneCRSource, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_swallow_same_columns、入力 2〜3行目 publish_lone_cr）`,
	}},
	"lone-cr-line-end": {pubIntended, whyPubLoneCR, []string{
		`already hashed: 上流 2 / dwloc 3`,
		`other: 上流 2 / dwloc 3`,
		`出力の 4 行目: 上流 "fedcba9876543210,UI,,,UI,b" / dwloc "0123456789abcdef,UI,,,UI,a"`,
	}},
	"lone-cr-unquoted-value": {pubIntended, whyPubLoneCRUnquoted, []string{
		`上流 書く / dwloc 止まる（形: 入力 2行目 publish_cr_cut）`,
	}},
	"cr-only-published": {pubIntended, whyPubCROnly, []string{
		`already hashed: 上流 0 / dwloc 2`,
		`other: 上流 0 / dwloc 2`,
		`出力の 3 行目: 上流 "（無い）" / dwloc "# ===== UI and other text (not part of the dialogue script) ====="`,
	}},
	"cr-only-working": {pubIntended, whyPubCROnly, []string{
		`converted: 上流 0 / dwloc 1`,
		`already hashed: 上流 0 / dwloc 1`,
		`other: 上流 0 / dwloc 2`,
		`出力の 5 行目: 上流 "（無い）" / dwloc "# ===== UI and other text (not part of the dialogue script) ====="`,
	}},
	"comma-only-row": {pubIntended, whyPubCommaRow, []string{
		`malformed dropped: 上流 1 / dwloc 0`,
	}},
	"hash-header-unquoted": {pubIntended, whyPubHeaderColumns, []string{
		`上流 書く / dwloc 止まる（形: 入力 2行目 publish_no_key_column、入力 2行目 publish_no_translation_column）`,
	}},
	"hash-header-quoted": {pubIntended, whyPubHashHeader, []string{
		`上流 書く / dwloc 止まる（形: 入力 1行目 publish_hash_header）`,
	}},
	"hash-header-leading-space": {pubIntended, whyPubHashHeader, []string{
		`上流 書く / dwloc 止まる（形: 入力 1行目 publish_hash_header）`,
	}},
	// hash-header-quoted-space と hash-header-nbsp は、上流と同じバイトを書くので
	// ここに無い。上流はそのヘッダーを飛ばさない（読んだ最初の値が '#' で始まらない）。
	// (a) の csvfile.PowerShellHeader.CommentLike も読んだ最初の値そのもので見るので、
	// 止まらずに上流と同じに書く（決まったことの 15）。
	"bare-quote-then-comment": {pubIntended, whyPubBareQuote, []string{
		`converted: 上流 1 / dwloc 0`,
		`other: 上流 3 / dwloc 2`,
		`出力の 5 行目: 上流 "fa515818c52bd108,UI,,,e,COMMENT-TAIL" / dwloc "fedcba9876543210,UI,,,UI,b"`,
	}},
	"bare-quote-then-heading": {pubIntended, whyPubBareQuote, []string{
		`converted: 上流 1 / dwloc 0`,
	}},
	"quote-after-closing-quote": {pubIntended, whyPubBareQuote, []string{
		`converted: 上流 1 / dwloc 0`,
		`other: 上流 3 / dwloc 2`,
		`出力の 5 行目: 上流 "fa515818c52bd108,UI,,,e,TAIL" / dwloc "fedcba9876543210,UI,,,UI,b"`,
	}},
	"soft-hyphen-comment": {pubIntended, whyPubCultureHash, []string{
		`malformed dropped: 上流 0 / dwloc 1`,
	}},
	"english-in-key-column": {pubUndecided, whyPubEnglishKey, []string{
		`上流 書く / dwloc 止まる（失われる訳 1 件）`,
	}},
	"published-row-missing-from-working": {pubUndecided, whyPubKeptFromPublished, []string{
		`上流 書く / dwloc 止まる（失われる訳 1 件）`,
	}},
}

// TestPublishAgainstUpstream は、いまの publish の振る舞いを上流の通しの実行と突き合わせる。
func TestPublishAgainstUpstream(t *testing.T) {
	cases, exp := loadPublishFixture(t)
	counts := map[publishDiffKind]int{}
	for i, c := range cases.Cases {
		d := describePublishDiff(upstreamPublish(t, c.Name, exp, i), runFixturePublish(t, cases, i))
		pin, known := publishDiffs[c.Name]
		switch {
		case len(d) == 0 && known:
			t.Errorf("%s: 上流と一致するようになった。表から外す（%s: %s）", c.Name, pin.kind, pin.why)
		case len(d) > 0 && !known:
			t.Errorf("%s: 上流と違う（表に無い）\n%s", c.Name, quotePublishLines(d))
		case known && !slices.Equal(d, pin.diff):
			t.Errorf("%s: 違い方が変わった\n got:\n%s\nwant:\n%s", c.Name, quotePublishLines(d), quotePublishLines(pin.diff))
		}
		if known {
			counts[pin.kind]++
		}
	}
	for name := range publishDiffs {
		if !slices.ContainsFunc(cases.Cases, func(c publishFixtureCase) bool { return c.Name == name }) {
			t.Errorf("表の %s は入力の表に無い", name)
		}
	}
	t.Logf("上流と違う入力: %v（全 %d 件）", counts, len(cases.Cases))
}

// acceptableFixtures は、正しい複数行の値なのに飲み込みの確かめ (f) が当たる入力。
// 表では「意図して違える」（止まる）として固定し、確かめたうえで通す指定で書けることを
// [TestPublishAcceptMultilineMatchesUpstream] で固定する。
var acceptableFixtures = []string{"ml-continuation-looks-like-row", "ml-source-translated-2col"}

// acceptGuide は、止めたときの直し方が案内する、通すための指定を取り出す。値は、
// シェルに貼れるよう二重引用符で囲んで出ることがある（shellWord）。
var acceptGuide = regexp.MustCompile(`--accept-multiline ("[^"]*"|\S+) を付けると書けます。`)

// pasteWords は、1行をシェルに貼ったときと同じく語に分ける。空白で分け、二重引用符の
// 中の空白では分けず、二重引用符は取り除く。PowerShell・cmd・bash に共通する読み方
// だけをまねる（$ などの読み替えはしない。そうした文字のある値は、案内が書き直すよう
// 添える）。
func pasteWords(line string) []string {
	var words []string
	var cur strings.Builder
	inWord, quoted := false, false
	for _, r := range line {
		switch {
		case r == '"':
			quoted, inWord = !quoted, true
		case (r == ' ' || r == '\t') && !quoted:
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

// guidedAccepts は、止めたときの直し方が案内した指定を、案内のとおりにシェルへ貼った
// ときの引数（--accept-multiline と値の組）にして返す。同じ指定は1組にまとめる。
// 貼ると1語にならない案内は、試験を落とす。
func guidedAccepts(t *testing.T, stderr string) []string {
	t.Helper()
	var args []string
	for _, m := range acceptGuide.FindAllStringSubmatch(stderr, -1) {
		words := pasteWords("--accept-multiline " + m[1])
		if len(words) != 2 {
			t.Errorf("案内した指定が、貼ると1語にならない: %s → %q", m[1], words)
			continue
		}
		if !slices.Contains(args, words[1]) {
			args = append(args, words...)
		}
	}
	return args
}

// TestPublishAcceptMultilineMatchesUpstream は、正しい複数行の値なのに飲み込みの確かめで
// 止まる入力が、確かめたうえで通す指定（--accept-multiline）を付けると、上流と同じ
// バイトと集計で書けることを見る（決まったことの 4 と 16）。
//
// 指定は、止めたときの直し方に出たものを、シェルに貼ったときと同じく語に分けて
// そのまま渡す（guidedAccepts）。指定はレコード単位（<ロケール>:<key>）で、直し方が
// 写せる形で出すことも、ここで確かめる。
func TestPublishAcceptMultilineMatchesUpstream(t *testing.T) {
	cases, exp := loadPublishFixture(t)
	for _, name := range acceptableFixtures {
		i := slices.IndexFunc(cases.Cases, func(c publishFixtureCase) bool { return c.Name == name })
		if i < 0 {
			t.Fatalf("入力の表に %s が無い", name)
		}
		if pin, ok := publishDiffs[name]; !ok || pin.kind != pubIntended || pin.why != whyPubAcceptable {
			t.Errorf("%s: 表で、確かめたうえで通す形として止めることを固定していない: %+v", name, pin)
		}
		_, code, _, stderr := fixtureRun(t, cases, i)
		if code != exitProblems {
			t.Fatalf("%s: 指定が無いときの終了コード = %d、1 を期待\n%s", name, code, stderr)
		}
		extra := guidedAccepts(t, stderr)
		for i := 1; i < len(extra); i += 2 {
			if !strings.HasPrefix(extra[i], publishFixtureLocale+":") {
				t.Errorf("%s: 案内した指定がレコード単位（%s:<key>）でない: %s", name, publishFixtureLocale, extra[i])
			}
		}
		if len(extra) == 0 {
			t.Fatalf("%s: 通すための指定を案内していない\n%s", name, stderr)
		}
		got := runFixturePublish(t, cases, i, extra...)
		if d := describePublishDiff(upstreamPublish(t, name, exp, i), got); len(d) > 0 {
			t.Errorf("%s: 案内どおりに指定しても上流と違う（%v）\n%s", name, extra, quotePublishLines(d))
		}
	}
}

// shapeStopReasons は、表の違い方 diff が形の確かめで止まることだけを言うなら、
// 止めた理由の識別子を並べて返す。そうでなければ nil。
func shapeStopReasons(diff []string) []string {
	if len(diff) == 0 {
		return nil
	}
	var out []string
	for _, line := range diff {
		if !strings.Contains(line, "dwloc 止まる（形: ") {
			return nil
		}
		out = append(out, publishReasonID.FindAllString(line, -1)...)
	}
	return out
}

// publishReasonID は、表の違い方に書いた形の理由の識別子。
var publishReasonID = regexp.MustCompile(`publish_[a-z_]+`)

// acceptableShapeReason は、形の理由が、確かめたうえで通す指定で通せる形かを返す
// （publish.Hazard.AcceptableShape と同じ。続きの行が単独で読むとレコードに見える形）。
func acceptableShapeReason(id string) bool {
	return id == reason.PublishSwallowKeyShaped || id == reason.PublishSwallowSameColumns
}

// TestPublishAcceptMultilineKeepsOtherStops は、確かめたうえで通す指定
// （--accept-multiline）を付けても、通せない形で止まる入力が書かれないことを見る。
// 対象は表の違い方のうち、形の確かめで止まり、通せない理由を含むもの。
//
// 指定はレコード単位（<ロケール>:<key>）なので、止まった形のレコードを名指す指定を
// 付ける。
//
//   - 通せない形だけで止まる入力: 指定は当たらない指定として終了コード2で止まる。
//     形に key の無い入力（単独の CR、閉じない引用符など）は、名指せるレコードが無いので、
//     そのロケールの架空の key で確かめる。
//   - 通せる形と通せない形の両方で止まる入力（2列の作業コピーの原文の単独の CR。
//     続きの行が2列に見える）: 通せる形のレコードを名指す指定を付けると、その行は
//     通るが、通せない形で終了コード1のまま止まる。
//
// どちらも公開ファイルは1バイトも変わらない。
//
// 閉じ引用符の後ろに文字が続く飲み込みは、続きの行が単独で読むとレコードに見えても
// 通さない（swallow-7col-empty-key-english、swallow-6col-published）。通すと、英語の
// 原文やキーが訳として公開される。この2件は、そのレコードを名指して確かめる。
func TestPublishAcceptMultilineKeepsOtherStops(t *testing.T) {
	cases, _ := loadPublishFixture(t)
	var checked, named, mixed []string
	for i, c := range cases.Cases {
		pin, ok := publishDiffs[c.Name]
		if !ok || !slices.ContainsFunc(shapeStopReasons(pin.diff), func(id string) bool { return !acceptableShapeReason(id) }) {
			continue
		}
		checked = append(checked, c.Name)
		root, code, _, _ := fixtureRun(t, cases, i)
		if code != exitProblems {
			t.Fatalf("%s: 指定が無いときの終了コード = %d、1 を期待", c.Name, code)
		}
		targets, err := publish.DiscoverTargets(root)
		if err != nil || len(targets) != 1 {
			t.Fatalf("%s: 対象を決められない: %v %+v", c.Name, err, targets)
		}
		hazards, err := publish.CheckTargetShape(targets[0])
		if err != nil {
			t.Fatalf("%s: CheckTargetShape: %v", c.Name, err)
		}
		published := cases.PublishedDefault
		if c.As == "published" {
			published = c.Text
		} else if c.Published != nil {
			published = *c.Published
		}
		unchanged := func(root, spec string) {
			t.Helper()
			if got := readFile(t, root, publishFixturePublished); got != published {
				t.Errorf("%s: %s を付けたら公開ファイルが変わった", c.Name, spec)
			}
		}

		var keys, acceptable []string
		stops := 0
		for _, h := range hazards {
			if h.Acceptable() {
				acceptable = append(acceptable, "--accept-multiline", publishFixtureLocale+":"+h.Key)
			} else {
				stops++
			}
			if publish.NameableKey(h.Key) && !slices.Contains(keys, h.Key) {
				keys = append(keys, h.Key)
			}
		}
		if len(acceptable) > 0 {
			mixed = append(mixed, c.Name)
			root, code, stdout, stderr := fixtureRun(t, cases, i, acceptable...)
			if code != exitProblems {
				t.Errorf("%s: 通せる形を指定した終了コード = %d、1 を期待\n%s", c.Name, code, stderr)
			}
			if !strings.Contains(stderr, publishAcceptText) || !strings.Contains(stderr, shapeStopText) ||
				!strings.Contains(stderr, fmt.Sprintf("読み違える形が %d か所あります。", stops)) || stdout != "" {
				t.Errorf("%s: 通せる形を指定したときの報告が違う\n%s%s", c.Name, stderr, stdout)
			}
			unchanged(root, strings.Join(acceptable, " "))
			continue
		}

		if len(keys) > 0 {
			named = append(named, c.Name)
		} else {
			keys = []string{shapeKeyUnrelated}
		}
		for _, k := range keys {
			spec := publishFixtureLocale + ":" + k
			root, code, stdout, stderr := fixtureRun(t, cases, i, "--accept-multiline", spec)
			if code != exitError {
				t.Errorf("%s: %s を付けた終了コード = %d、2 を期待\n%s", c.Name, spec, code, stderr)
				continue
			}
			if !strings.Contains(stderr, "--accept-multiline の指定が、通せる行に当たりません: "+spec) ||
				strings.Contains(stderr, "として通します") || stdout != "" {
				t.Errorf("%s: %s を付けたときの報告が違う\n%s%s", c.Name, spec, stderr, stdout)
			}
			unchanged(root, spec)
		}
	}
	for _, name := range []string{"swallow-7col-empty-key-english", "swallow-6col-published"} {
		if !slices.Contains(named, name) {
			t.Errorf("%s を、そのレコードを名指して確かめていない（表で通せない理由で止まることを固定していない）", name)
		}
	}
	if !slices.Contains(mixed, "lone-cr-source-2col") {
		t.Errorf("lone-cr-source-2col を、通せる形のレコードを名指して確かめていない")
	}
	t.Logf("確かめた入力: %d 件（止まった形のレコードを名指したもの %d 件、通せる形と通せない形の両方で止まるもの %d 件）",
		len(checked), len(named), len(mixed))
}

// portSpecPath は移植仕様。表の分類を、仕様の表と突き合わせるのに使う。
var portSpecPath = filepath.Join("..", "..", "docs", "port-spec.md")

// 移植仕様で、上流との違いを並べた表の見出し。
const (
	specIntendedHeading  = "### 上流と意図して違える点"
	specUndecidedHeading = "### 上流と違うが未決の点"
)

// specHeading は Markdown の見出しの行。節の終わりを決めるのに使う。
var specHeading = regexp.MustCompile(`(?m)^#{1,6} `)

// fixtureInputName は入力の名前の形（cases.json の name）。表の最後の列から、
// 見出しの「試験の入力」や「（すべての入力）」のような文を除くのに使う。
var fixtureInputName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// specInputs は、移植仕様の見出し heading の節にある表から、最後の列（試験の入力）に
// 書いた入力の名前を集める。
func specInputs(t *testing.T, spec, heading string) map[string]bool {
	t.Helper()
	_, section, ok := strings.Cut(spec, "\n"+heading+"\n")
	if !ok {
		t.Fatalf("移植仕様に見出し %q が無い", heading)
	}
	if loc := specHeading.FindStringIndex(section); loc != nil {
		section = section[:loc[0]]
	}
	names := map[string]bool{}
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for _, name := range strings.Split(cells[len(cells)-1], "、") {
			if name = strings.TrimSpace(name); fixtureInputName.MatchString(name) {
				names[name] = true
			}
		}
	}
	if len(names) == 0 {
		t.Fatalf("移植仕様の %q の節に、試験の入力を並べた表が無い", heading)
	}
	return names
}

// TestPublishDiffKindsMatchPortSpec は、[publishDiffs] の分類が移植仕様の表と
// 食い違っていないかを見る。
//
// 「意図して違える」と「未決」は、どちらも違い方を固定するだけで試験の結果は
// 変わらない。そのため分類を取り違えても [TestPublishAgainstUpstream] は落ちず、
// まだ決めていない違いが、仕様の上で決まったように読まれる。仕様の2つの表と
// 分類がそろっていることを、ここで確かめる。
func TestPublishDiffKindsMatchPortSpec(t *testing.T) {
	raw, err := os.ReadFile(portSpecPath)
	if err != nil {
		t.Fatalf("移植仕様が読めない: %v", err)
	}
	spec := string(raw)
	intended := specInputs(t, spec, specIntendedHeading)
	undecided := specInputs(t, spec, specUndecidedHeading)

	cases, _ := loadPublishFixture(t)
	for _, table := range []map[string]bool{intended, undecided} {
		for name := range table {
			if !slices.ContainsFunc(cases.Cases, func(c publishFixtureCase) bool { return c.Name == name }) {
				t.Errorf("移植仕様の表の %s は入力の表に無い", name)
			}
		}
	}

	for name, pin := range publishDiffs {
		switch pin.kind {
		case pubIntended:
			if !intended[name] || undecided[name] {
				t.Errorf("%s: %s として固定しているが、移植仕様の「%s」の表に無いか、「%s」の表にある",
					name, pin.kind, strings.TrimPrefix(specIntendedHeading, "### "), strings.TrimPrefix(specUndecidedHeading, "### "))
			}
		case pubUndecided:
			if !undecided[name] || intended[name] {
				t.Errorf("%s: %s として固定しているが、移植仕様の「%s」の表に無いか、「%s」の表にある",
					name, pin.kind, strings.TrimPrefix(specUndecidedHeading, "### "), strings.TrimPrefix(specIntendedHeading, "### "))
			}
		}
	}
	for name := range undecided {
		if pin, ok := publishDiffs[name]; !ok || pin.kind != pubUndecided {
			t.Errorf("%s: 移植仕様では未決だが、試験は %s として固定していない", name, pubUndecided)
		}
	}
}

func quotePublishLines(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "\t%q,\n", l)
	}
	return b.String()
}
