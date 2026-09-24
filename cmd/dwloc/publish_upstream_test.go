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
)

/*
ここは、いまの dwloc publish の振る舞いを、上流の tools/hash-strings.ps1 を通しで
走らせた結果と突き合わせて固定する試験である（全体を解釈する読み手へ移す作業の PR0）。

入力は testdata/upstream/cases.json、上流の結果は testdata/upstream/expected.json の
run にある（作り方は testdata/upstream/README.md）。入力ごとに、上流と同じ
tools/・data/・Translations/ の並びを一時ディレクトリに作り、dwloc publish --no-game を
走らせる。書けば出力のバイトと集計の1行を、止まればどこで止まったかを上流と比べる。

上流と違ってよいのは [publishDiffs] に載せた入力だけで、どう違うかまで固定する。
pubPR2 の行は、publish・diff・order が全体を解釈する読み手へ移る PR2 で振る舞いが
変わる箇所である。PR2 でそこが変わると、この試験が落ちて教えてくれる。そのときは
表を直す（上流と同じになったなら行を消し、意図して違えるなら pubIntended へ移す）。

集計の1行のうち「kept from the published file」は比べない。dwloc の集計の1行には
この項目が無い（上流の #10。いまの公開ファイルにだけある行を引き継ぐ処理は、この
移植の範囲外で、dwloc は訳が失われる確かめで止まる）。
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

// runFixturePublish は、上流の通しの実行と同じ並びのリポジトリを作り、
// dwloc publish --no-game を走らせる。
func runFixturePublish(t *testing.T, cases publishFixture, i int) publishView {
	t.Helper()
	c := cases.Cases[i]
	published := "Translations/" + publishFixtureLocale + "/strings.csv"
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
	root := makeTree(t, files)

	code, stdout, stderr := runCLI("publish", "--root", root, "--no-game")
	switch code {
	case exitOK:
		m := dwlocSummary.FindStringSubmatch(strings.TrimSpace(strings.SplitN(stdout, "\n", 2)[0]))
		if m == nil {
			t.Fatalf("%s: 集計の1行が読めない\n%s", c.Name, stdout)
		}
		v := publishView{outcome: outcomeWrote, output: readFile(t, root, published)}
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
	// 違える点」）。PR2 のあとも違う。
	pubIntended publishDiffKind = "意図して違える"
	// pubPR2 は、PR2（publish・diff・order を全体を解釈する読み手へ移す）で
	// 振る舞いが変わる箇所。
	pubPR2 publishDiffKind = "PR2 で変わる"
)

// knownPublishDiff は、上流と違ってよい入力1つ。
type knownPublishDiff struct {
	kind publishDiffKind
	// why はなぜ違うか。pubPR2 では、PR2 でどう変わる見込みかも書く。
	why string
	// diff は [describePublishDiff] の出力をそのまま固定したもの。
	diff []string
}

// 上流と違う理由。pubPR2 では、PR2 でどう変わる見込みかも書く（見込みは
// 決まったことからの推測で、PR2 の実装で確かめる）。
const (
	whyPubMultilineInput = "入力に、訳の入った行をまたぐレコードがあるので、形の確かめ (c) で止める。" +
		"PR2 で全体を解釈して読むと止まらず、上流と同じバイトを書く見込み"
	whyPubTwoColumnSource = "入力（source_en,translation の2列）に、訳の入った行をまたぐレコードがあるので (c) で止める。" +
		"PR2 では正しい訳として読めるが、続きの行の区切りの数がヘッダーの列数（2）と同じなので、" +
		"飲み込みの確かめ (f) で止まる見込み（確かめたうえで通す指定で書く）"
	whyPubRowLikeContinuation = "原文の続きの行が、1行だけで読むとヘッダーと同じ7列のレコードに見える。いまは (c) で止める。" +
		"PR2 でも飲み込みの確かめ (f) で止まる見込み（確かめたうえで通す指定で書く）"
	whyPubMultilineCount = "訳の空の、行をまたぐレコードを行単位で読み、2件の malformed dropped に数える" +
		"（出力は上流と同じ。実物の ja 作業コピーと同じ形）。PR2 で上流と同じ集計になる見込み"
	whyPubMultilineCurrent = "いまの公開ファイルに行をまたぐレコードがあるので、形の確かめ (b) で止める。" +
		"PR2 で (b) をやめると、上流と同じバイトを書く見込み"
	whyPubHeaderColumns = "ヘッダーに key 列か translation 列が無いので、形の確かめ (a) で止める。" +
		"上流はすべての行を捨て、ヘッダーとコメントだけを書く"
	whyPubCRLFSource = "表計算ソフトで保存し直した形（原文の LF が CRLF）。いまは (c) で止める。" +
		"PR2 では上流と同じくその行を落として書き、原文の CRLF を LF にするとキーが合うことを知らせる見込み"
	whyPubUnclosed = "閉じない引用符 (e)。上流は後ろの行（英語の原文を含む）を訳に飲み込んで書く（上流の報告 #11）。" +
		"dwloc は止める。PR2 でも止める（読み手の型付きの誤りを、終了コード1と直し方の案内にする）"
	whyPubSwallow = "引用符が別の行で閉じ、後ろの行を飲み込む形。上流は英語の原文やキーを訳に入れて書く。" +
		"いまは (b)(c) で止める。PR2 でも止めるが、理由が飲み込みの確かめ (f) に変わる見込み"
	whyPubLoneCRQuoted = "引用した訳の中の単独の CR。上流はその行を失う。いまは (c) で止める。" +
		"PR2 でも止めるが、理由が単独の CR（LF に直す案内）に変わる見込み"
	whyPubLoneCR         = "単独の CR も行の区切りにして読む。上流は単独の CR の手前を捨て、その行の訳を失う（上流の不具合。写さない）"
	whyPubLoneCRUnquoted = "引用符で囲まない値の中の単独の CR。dwloc は行の区切りとして値を切り（訳が「い」になる）、" +
		"上流はその行を失う。docs/port-spec.md の「残る隙間」で、どちらの道具の書き手も作らない形"
	whyPubCROnly     = "CR だけで改行したファイルも読む。上流は全行を失い、ヘッダーだけを書く（上流の不具合。写さない）"
	whyPubCommaRow   = "',' だけの行を黙って落とす。上流は空のレコードとして malformed dropped に数える。違うのは集計の数だけ"
	whyPubHashHeader = "'#' で始まるヘッダーをそのまま読み、source_en 列から訳を書く。上流はそのヘッダーを飛ばして" +
		"データの行をヘッダーにし、何も書かない。PR2 では形の確かめ (a) で止まる見込み"
	whyPubDupNoData = "データ行の無いファイルで、列名の重複を確かめずに書く。上流は例外で止まる。" +
		"PR2 の読み手はデータが0件でも誤りにするので、上流と同じく書かない見込み"
	whyPubBareQuote = "裸の引用符の後ろのコメント行や見出しを、引用の外として落とす。上流はレコードとして読み、" +
		"列が多ければ公開ファイルに書く（上流の不具合。写さない）"
	whyPubSpaceOnly = "全角空白や NO-BREAK SPACE だけの行を行単位で読み、malformed dropped に数える。上流は落とす。" +
		"PR2 で上流と同じ集計になる見込み"
	whyPubCultureHash = "U+00AD のあとに '#' が続く行をデータとして読み、malformed dropped に数える。" +
		"上流の StartsWith('#') はカルチャに依存する照合でコメントとして落とす（Go では照合表を持てない）"
	whyPubEnglishKey = "key 列の英文をハッシュにして公開する上流の #9 は写さない。dwloc はその行を捨てるので、" +
		"いまの公開ファイルの訳が失われるとして止める"
	whyPubKeptFromPublished = "いまの公開ファイルにだけある行を引き継ぐ上流の #10 は写さない。dwloc はその訳が" +
		"失われるとして止める"
)

// publishDiffs は、いまの dwloc publish が上流と違う入力。
var publishDiffs = map[string]knownPublishDiff{
	"ml-translation-lf": {pubPR2, whyPubMultilineInput, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_multiline_translated）`,
	}},
	"ml-translation-crlf": {pubPR2, whyPubMultilineInput, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_multiline_translated）`,
	}},
	"ml-translation-blank-lines": {pubPR2, whyPubMultilineInput, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜5行目 publish_multiline_translated）`,
	}},
	"ml-source-real-shape": {pubPR2, whyPubMultilineCount, []string{
		`converted: 上流 3 / dwloc 2`,
		`malformed dropped: 上流 0 / dwloc 2`,
	}},
	"ml-source-translated": {pubPR2, whyPubMultilineInput, []string{
		`上流 書く / dwloc 止まる（形: 入力 3〜5行目 publish_multiline_translated）`,
	}},
	"ml-source-translated-2col": {pubPR2, whyPubTwoColumnSource, []string{
		`上流 書く / dwloc 止まる（形: 入力 3〜5行目 publish_multiline_translated）`,
	}},
	"ml-source-mixed-lines": {pubPR2, whyPubMultilineCount, []string{
		`converted: 上流 3 / dwloc 2`,
		`malformed dropped: 上流 0 / dwloc 2`,
	}},
	"ml-translation-only-newline": {pubPR2, whyPubMultilineInput, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_multiline_translated）`,
	}},
	"ml-published-crlf": {pubPR2, whyPubMultilineCurrent, []string{
		`上流 書く / dwloc 止まる（形: 公開ファイル 2〜3行目 publish_multiline_current）`,
	}},
	"ml-hash-line-in-translation": {pubPR2, whyPubMultilineInput, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_multiline_translated）`,
	}},
	"ml-hash-line-in-published": {pubPR2, whyPubMultilineCurrent, []string{
		`上流 書く / dwloc 止まる（形: 公開ファイル 2〜3行目 publish_multiline_current）`,
	}},
	"ml-published-translation": {pubPR2, whyPubMultilineCurrent, []string{
		`上流 書く / dwloc 止まる（形: 公開ファイル 2〜3行目 publish_multiline_current）`,
	}},
	"ml-continuation-looks-like-row": {pubPR2, whyPubRowLikeContinuation, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_multiline_translated）`,
	}},
	"ml-line-id-translation": {pubPR2, whyPubMultilineInput, []string{
		`上流 書く / dwloc 止まる（形: 入力 3〜4行目 publish_multiline_translated）`,
	}},
	"ml-header": {pubIntended, whyPubHeaderColumns, []string{
		`上流 書く / dwloc 止まる（形: 入力 1行目 publish_no_translation_column）`,
	}},
	"ml-source-crlf-key-mismatch": {pubPR2, whyPubCRLFSource, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜4行目 publish_multiline_translated）`,
	}},
	"unclosed-to-eof": {pubIntended, whyPubUnclosed, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_unclosed_quote）`,
	}},
	"unclosed-last-line": {pubIntended, whyPubUnclosed, []string{
		`上流 書く / dwloc 止まる（形: 公開ファイル 3行目 publish_unclosed_quote）`,
	}},
	"unclosed-header": {pubIntended, whyPubUnclosed, []string{
		`上流 書く / dwloc 止まる（形: 入力 1行目 publish_no_translation_column、入力 1〜2行目 publish_unclosed_quote）`,
	}},
	"unclosed-published-middle": {pubIntended, whyPubUnclosed, []string{
		`上流 書く / dwloc 止まる（形: 公開ファイル 2〜5行目 publish_unclosed_quote）`,
	}},
	"swallow-3col": {pubPR2, whyPubSwallow, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_multiline_translated）`,
	}},
	"swallow-7col-hash-close": {pubPR2, whyPubSwallow, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜4行目 publish_multiline_translated）`,
	}},
	"swallow-2col-hash-close": {pubPR2, whyPubSwallow, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜4行目 publish_multiline_translated）`,
	}},
	"swallow-7col-empty-key-english": {pubPR2, whyPubSwallow, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_multiline_translated）`,
	}},
	"swallow-6col-published": {pubPR2, whyPubSwallow, []string{
		`上流 書く / dwloc 止まる（形: 公開ファイル 2〜3行目 publish_multiline_current）`,
	}},
	"lone-cr-in-quoted-translation": {pubPR2, whyPubLoneCRQuoted, []string{
		`上流 書く / dwloc 止まる（形: 入力 2〜3行目 publish_multiline_translated）`,
	}},
	"lone-cr-line-end": {pubIntended, whyPubLoneCR, []string{
		`already hashed: 上流 2 / dwloc 3`,
		`other: 上流 2 / dwloc 3`,
		`出力の 4 行目: 上流 "fedcba9876543210,UI,,,UI,b" / dwloc "0123456789abcdef,UI,,,UI,a"`,
	}},
	"lone-cr-unquoted-value": {pubIntended, whyPubLoneCRUnquoted, []string{
		`malformed dropped: 上流 0 / dwloc 1`,
		`in play order: 上流 1 / dwloc 2`,
		`出力の 7 行目: 上流 "3fc4ccfe745870e2,L01 Ember,Ember_1_intro,2,Moss,に" / dwloc "7692c3ad3540bb80,L01 Ember,Ember_1_intro,1,Ember,い"`,
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
	"hash-header-quoted": {pubPR2, whyPubHashHeader, []string{
		`converted: 上流 0 / dwloc 2`,
		`malformed dropped: 上流 1 / dwloc 0`,
		`in play order: 上流 0 / dwloc 2`,
		`出力の 5 行目: 上流 "（無い）" / dwloc "# ===== Level 1: Ember (Rainy) ====="`,
	}},
	"hash-header-leading-space": {pubPR2, whyPubHashHeader, []string{
		`converted: 上流 0 / dwloc 2`,
		`malformed dropped: 上流 1 / dwloc 0`,
		`in play order: 上流 0 / dwloc 2`,
		`出力の 5 行目: 上流 "（無い）" / dwloc "# ===== Level 1: Ember (Rainy) ====="`,
	}},
	"dup-columns-no-data": {pubPR2, whyPubDupNoData, []string{
		`上流 変換できない / dwloc 書く`,
	}},
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
	"ideographic-space-line": {pubPR2, whyPubSpaceOnly, []string{
		`malformed dropped: 上流 0 / dwloc 1`,
	}},
	"nbsp-line": {pubPR2, whyPubSpaceOnly, []string{
		`malformed dropped: 上流 0 / dwloc 1`,
	}},
	"soft-hyphen-comment": {pubIntended, whyPubCultureHash, []string{
		`malformed dropped: 上流 0 / dwloc 1`,
	}},
	"english-in-key-column": {pubIntended, whyPubEnglishKey, []string{
		`上流 書く / dwloc 止まる（失われる訳 1 件）`,
	}},
	"published-row-missing-from-working": {pubIntended, whyPubKeptFromPublished, []string{
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

func quotePublishLines(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "\t%q,\n", l)
	}
	return b.String()
}
