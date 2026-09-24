package csvfile

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

/*
ここは、上流の tools/hash-strings.ps1 の読み方と、dwloc の2つの読み手を
突き合わせる試験である（全体を解釈する読み手へ移す作業の PR0）。

入力の表は testdata/upstream/cases.json、上流の正解は testdata/upstream/expected.json
にある。正解は scripts/upstream-fixtures.ps1 が上流の Read-Csv を AST で取り出して
作ったもので、手では直さない（作り方は testdata/upstream/README.md）。CI には pwsh も
上流のリポジトリも無いので、ふだんは保存した正解とだけ比べる。

上流と違ってよいのは、下の表（[mainReaderDiffs]、[wholeReaderDiffs]、
[lineReaderDiffs]）に載せた入力だけである。表の各行は、どう違うか
（[describeReadDiff] の出力）まで固定する。表に無い入力で違いが出たとき、表にある
入力が一致するようになったとき、違い方が変わったときは、どれも落ちる。

PR0 では、全体を解釈する新しい読み手で上流にそろえる違いを「PR1 で直す」として
表に載せていた（ReadPowerShellWhole が列名の重複を確かめない3件）。PR1 で主の読み手
[ReadPowerShell] が入り、その3件は主の読み手では上流と一致する。守り専用のまま残る
ReadPowerShellWhole の表では、同じ3件を「守り専用の読み方」に移した。閉じない引用符は、
上流と同じく飲み込むのではなく型付きの誤りになるので、主の読み手の表では
diffIntended として加わる（docs/port-spec.md「上流と意図して違える点」の閉じない
引用符の行）。
*/

// upstreamDir は入力の表と正解の置き場。試験はパッケージのディレクトリを
// カレントにして走るので、そこからの相対で引く。
var upstreamDir = filepath.Join("..", "..", "testdata", "upstream")

// 上流を走らせて正解を作り直し、保存した正解と突き合わせるための環境変数。
const (
	// upstreamRepoEnv は上流のリポジトリ。git show で tools/hash-strings.ps1 を取り出す。
	upstreamRepoEnv = "DWLOC_UPSTREAM_REPO"
	// upstreamRefEnv は取り出すコミット。無ければ正解に記録したコミットを使う。
	upstreamRefEnv = "DWLOC_UPSTREAM_REF"
	// upstreamPwshEnv は pwsh の実行ファイル。無ければ PATH の pwsh を探す。
	upstreamPwshEnv = "DWLOC_PWSH"
)

// fixtureCases は cases.json の中身。
type fixtureCases struct {
	PublishedDefault string `json:"published_default"`
	Data             struct {
		ScriptOrder string `json:"script_order"`
		LevelFlow   string `json:"level_flow"`
	} `json:"data"`
	Cases []fixtureCase `json:"cases"`
}

// fixtureCase は入力1つ。
type fixtureCase struct {
	Name string `json:"name"`
	Note string `json:"note"`
	// As は text を上流の通しの実行でどこに置くか（working か published）。
	As   string `json:"as"`
	Text string `json:"text"`
	// Published は As が working のときの公開ファイル。無ければ PublishedDefault。
	Published *string `json:"published"`
}

// fixtureExpected は expected.json の中身。
type fixtureExpected struct {
	Upstream struct {
		Commit string `json:"commit"`
		Script string `json:"script"`
		Blob   string `json:"blob"`
	} `json:"upstream"`
	Pwsh struct {
		Version string `json:"version"`
		OS      string `json:"os"`
	} `json:"pwsh"`
	CasesSHA256 string           `json:"cases_sha256"`
	Cases       []fixtureExpCase `json:"cases"`
}

// fixtureExpCase は入力1つの正解。
type fixtureExpCase struct {
	Name string `json:"name"`
	Read struct {
		Error   *fixtureError `json:"error"`
		Columns []string      `json:"columns"`
		Records []struct {
			Line   int       `json:"line"`
			End    int       `json:"end"`
			Values []*string `json:"values"`
		} `json:"records"`
	} `json:"read"`
}

// fixtureError は上流の例外。message は pwsh の言語で変わるので比べない。
type fixtureError struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// loadUpstreamFixture は入力の表と正解を読み、正解が今の表から作られたものかを確かめる。
func loadUpstreamFixture(t *testing.T) (fixtureCases, fixtureExpected) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(upstreamDir, "cases.json"))
	if err != nil {
		t.Fatalf("入力の表が読めない: %v", err)
	}
	var cases fixtureCases
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("入力の表を解釈できない: %v", err)
	}
	exp := readExpected(t, filepath.Join(upstreamDir, "expected.json"))
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

func readExpected(t *testing.T, path string) fixtureExpected {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("正解が読めない: %v", err)
	}
	var exp fixtureExpected
	if err := json.Unmarshal(raw, &exp); err != nil {
		t.Fatalf("%s を解釈できない: %v", path, err)
	}
	return exp
}

// readView は、ある読み手が1つの入力を読んだ結果を、比べやすい形にしたもの。
type readView struct {
	// err は読めなかった理由。読めたら空。
	err     string
	records []readRecord
}

// readRecord は1レコード。line と end は物理行の範囲。
type readRecord struct {
	line, end int
	columns   []string
	values    []string
}

// errDuplicateColumns は、列名の重複で読めなかったことを表す印。上流の例外と
// dwloc の誤りを同じ印にそろえて比べる。
const errDuplicateColumns = "列名の重複"

// upstreamView は正解の read を readView にする。
//
// 上流の値の $null（列が足りない行）は空文字にする。上流はどの値も [string] で
// 受けて使うので、$null と空文字は同じに扱われる（移植仕様「境界条件」）。
func upstreamView(c fixtureExpCase) readView {
	if e := c.Read.Error; e != nil {
		if strings.HasPrefix(e.ID, "AlreadyPresentPSMemberInfoInternalCollectionAdd,") {
			return readView{err: errDuplicateColumns}
		}
		return readView{err: "上流の例外 " + e.ID}
	}
	var v readView
	for _, r := range c.Read.Records {
		values := make([]string, len(r.Values))
		for i, p := range r.Values {
			if p != nil {
				values[i] = *p
			}
		}
		v.records = append(v.records, readRecord{line: r.Line, end: r.End, columns: c.Read.Columns, values: values})
	}
	return v
}

// errUnclosedPrefix は、閉じない引用符で読めなかったことを表す印の頭。後ろに行が付く。
const errUnclosedPrefix = "閉じない引用符 L"

// mainView は主の読み手 [ReadPowerShell] で読んだ結果。
func mainView(text string) readView {
	f, err := ReadPowerShell([]byte(text))
	if err != nil {
		var dup *DuplicateColumnError
		var unclosed *UnclosedQuoteError
		switch {
		case errors.As(err, &dup):
			return readView{err: errDuplicateColumns}
		case errors.As(err, &unclosed):
			return readView{err: errUnclosedPrefix + strconv.Itoa(unclosed.Line)}
		}
		return readView{err: err.Error()}
	}
	var v readView
	for _, r := range f.Records {
		v.records = append(v.records, rowRecord(r.Row, r.Line, r.EndLine))
	}
	return v
}

// wholeView は [ReadPowerShellWhole] で読んだ結果。
func wholeView(text string) readView {
	w := ReadPowerShellWhole([]byte(text))
	var v readView
	for _, r := range w.Rows {
		v.records = append(v.records, rowRecord(r.Row, r.Line, r.EndLine))
	}
	return v
}

// lineView は行単位の読み手（[ReadPowerShellTable]）で読んだ結果。
func lineView(text string) readView {
	table, err := ReadPowerShellTable([]byte(text))
	if err != nil {
		var dup *DuplicateColumnError
		if errors.As(err, &dup) {
			return readView{err: errDuplicateColumns}
		}
		return readView{err: err.Error()}
	}
	var v readView
	for _, r := range table.Rows {
		v.records = append(v.records, rowRecord(r.Row, r.Line, r.Line))
	}
	return v
}

func rowRecord(row Row, line, end int) readRecord {
	columns := row.Columns()
	values := make([]string, len(columns))
	for i, c := range columns {
		values[i] = row.Get(c)
	}
	return readRecord{line: line, end: end, columns: columns, values: values}
}

// defaultColumnName は、ConvertFrom-Csv が名前の空の列に振る既定の名前（H1 など）。
var defaultColumnName = regexp.MustCompile(`^H\d+$`)

// sameColumns は列名の並びが同じかを返す。dwloc は名前の空の列を空のまま持ち、
// 上流は H1 のような名前を振る。名前で引くかぎり結果は変わらないので同じとみなす
// （[checkDuplicateColumns] の注記）。
func sameColumns(up, got []string) bool {
	return slices.EqualFunc(up, got, func(u, g string) bool {
		return u == g || (g == "" && defaultColumnName.MatchString(u))
	})
}

// describeReadDiff は、上流と dwloc の読んだ結果の違いを1行ずつの説明にする。
// 同じなら空を返す。レコードは開始行で突き合わせる。行をまたぐレコードを
// 行単位で読むと数がずれるので、並びの順で突き合わせると違いが後ろへ連なるため。
func describeReadDiff(up, got readView) []string {
	if up.err != "" || got.err != "" {
		if up.err == got.err {
			return nil
		}
		return []string{fmt.Sprintf("読めるか: 上流 %s / dwloc %s", orReadable(up.err), orReadable(got.err))}
	}
	ups := make(map[int]readRecord, len(up.records))
	for _, r := range up.records {
		ups[r.line] = r
	}
	gots := make(map[int]readRecord, len(got.records))
	for _, r := range got.records {
		gots[r.line] = r
	}
	lines := make([]int, 0, len(ups)+len(gots))
	for l := range ups {
		lines = append(lines, l)
	}
	for l := range gots {
		if _, ok := ups[l]; !ok {
			lines = append(lines, l)
		}
	}
	slices.Sort(lines)

	var out []string
	for _, l := range lines {
		u, inUp := ups[l]
		g, inGot := gots[l]
		switch {
		case !inGot:
			out = append(out, fmt.Sprintf("上流だけ %s %q", span(u), u.values))
		case !inUp:
			out = append(out, fmt.Sprintf("dwloc だけ %s %q", span(g), g.values))
		default:
			out = append(out, recordDiff(u, g)...)
		}
	}
	return out
}

func recordDiff(u, g readRecord) []string {
	var out []string
	if u.end != g.end {
		out = append(out, fmt.Sprintf("L%d の終わり: 上流 %d / dwloc %d", u.line, u.end, g.end))
	}
	if !sameColumns(u.columns, g.columns) {
		return append(out, fmt.Sprintf("L%d の列: 上流 %q / dwloc %q", u.line, u.columns, g.columns))
	}
	for i := range u.values {
		if u.values[i] != g.values[i] {
			out = append(out, fmt.Sprintf("L%d %s: 上流 %q / dwloc %q", u.line, u.columns[i], u.values[i], g.values[i]))
		}
	}
	return out
}

func span(r readRecord) string {
	if r.line == r.end {
		return "L" + strconv.Itoa(r.line)
	}
	return fmt.Sprintf("L%d-%d", r.line, r.end)
}

func orReadable(err string) string {
	if err == "" {
		return "読める"
	}
	return err
}

// diffKind は、上流と違ってよい理由の分類。
type diffKind string

const (
	// diffIntended は、上流と意図して違える点（docs/port-spec.md「上流と意図して
	// 違える点」）。直さない。
	diffIntended diffKind = "意図して違える"
	// diffGuardOnly は、PR1 まで publish の守りが使っていた読み手
	// （[ReadPowerShellWhole]）だけの違い。主の読み手 [ReadPowerShell] は上流と
	// そろっている。PR2 で publish の守りが主の読み手へ移り、この読み手はどこからも
	// 使われていない。
	diffGuardOnly diffKind = "守り専用の読み方"
	// diffLineBased は、1物理行を1レコードとして読むことから来る違い。PR2 で
	// publish・diff・order が全体を解釈する読み手へ移り、この読み手はどこからも
	// 使われていない。
	diffLineBased diffKind = "行単位の読み方"
)

// knownDiff は、上流と違ってよい入力1つ。
type knownDiff struct {
	kind diffKind
	// why はなぜ違ってよいか。
	why string
	// diff は [describeReadDiff] の出力をそのまま固定したもの。
	diff []string
}

// 上流と違ってよい理由。意図して違えるものは docs/port-spec.md の「上流と意図して
// 違える点」の表と同じことを書く。
const (
	whyLoneCR = "単独の CR も行の区切りにする。上流の Remove-NonRecords は単独の CR を行末と見なさず、" +
		"その手前を捨てる（上流の不具合。写さない）"
	whyLoneCRUnquoted = "引用符で囲まない値の中の単独の CR も、行の区切りにする規則のまま読む。値は CR の手前で切れ" +
		"（訳が「い」）、続きの「ち」は上流と同じく別のレコードになる。上流は CR の手前を捨ててその行を失い、ゲームは" +
		"引用の外の CR を捨てて「いち」と読む。読み方の規則は意図して違える。切れた訳を公開しないよう、publish は" +
		"形の確かめで止める（docs/port-spec.md「上流と意図して違える点」、FindCRCuts、cmd/dwloc の publish の試験）"
	whyCROnly = "CR だけで改行したファイルも読む。上流は Remove-NonRecords が全行を捨て、" +
		"訳がすべて消える（上流の不具合。写さない）"
	whyCommaRow = "',' だけの行は空行相当として落とす。上流は空の値のレコードにし、publish の集計で " +
		"malformed dropped に数える。違うのは集計の数だけ"
	whyHashHeader = "'#' で始まるヘッダーを飛ばさずにヘッダーとして読む。上流の ConvertFrom-Csv はそのレコードを" +
		"飛ばし、次のレコード（データ）をヘッダーにする。dwloc は写さない。publish は、最初の列名が '#' で" +
		"始まるヘッダーを形の確かめ (a) で止める（cmd/dwloc の publish の試験）"
	whyBareQuote = "引用の外かどうかを、フィールドの先頭で開いた引用だけで決める。上流の Remove-NonRecords は " +
		"'\"' の偶奇で決めるので、裸の引用符の後ろのコメント行や見出しをレコードにする" +
		"（列が多いと公開ファイルに漏れる。上流の不具合。写さない）"
	whyCultureHash = "行頭の '#' は序数で比べる。上流の StartsWith('#') はカルチャに依存する照合で、" +
		"U+00AD のように照合上無視される文字を飛ばす。Go では照合表を持てない"
	whyWholeDupColumns = "ReadPowerShellWhole は publish の守り専用で、列名の重複を確かめない（行単位の読み手が" +
		"先に確かめる）。主の読み手 ReadPowerShell は、データが0件でも DuplicateColumnError を返して上流とそろう"
	whyUnclosed = "閉じない引用符は型付きの誤り（UnclosedQuoteError）にし、値を返さない。上流はファイルの終わりまでを" +
		"1つの値に飲み込み、英語の原文ごと書く（上流の報告 #11。写さない）"
	whyLineSplit    = "1物理行を1レコードとして読むので、引用した値が最初の行で切れ、続きの行が別のレコードになる"
	whyLineUnclosed = "行単位では、閉じない引用符が行の終わりで閉じたことになる。上流はファイルの終わりまでを" +
		"1つの値に飲み込む"
	whyLineSpaceOnly = "行単位の読み手は半角空白とタブだけを空白と見るので、全角空白や NO-BREAK SPACE だけの行が" +
		"レコードになる。上流は Trim で落とす"
	whyLineDupNoData = "データ行の無いファイルでは列名の重複を確かめない（003ed1e の $lines.Count -lt 2 に合わせた）。" +
		"上流 main はデータが0件でも例外にする"
)

// wholeReaderDiffs は、[ReadPowerShellWhole] が上流と違ってよい入力。
var wholeReaderDiffs = map[string]knownDiff{
	"lone-cr-in-quoted-translation": {diffIntended, whyLoneCR, []string{
		`dwloc だけ L2-3 ["7692c3ad3540bb80" "L01 Ember" "Ember_1_intro" "1" "Ember" "one" "い\rち"]`,
		`上流だけ L3 ["ち\"" "" "" "" "" "" ""]`,
	}},
	"lone-cr-line-end": {diffIntended, whyLoneCR, []string{
		`dwloc だけ L2 ["0123456789abcdef" "UI" "" "" "UI" "a"]`,
	}},
	"lone-cr-unquoted-value": {diffIntended, whyLoneCRUnquoted, []string{
		`dwloc だけ L2 ["7692c3ad3540bb80" "L01 Ember" "Ember_1_intro" "1" "Ember" "one" "い"]`,
	}},
	"cr-only-published": {diffIntended, whyCROnly, []string{
		`dwloc だけ L2 ["0123456789abcdef" "UI" "" "" "UI" "a"]`,
		`dwloc だけ L3 ["fedcba9876543210" "UI" "" "" "UI" "b"]`,
	}},
	"cr-only-working": {diffIntended, whyCROnly, []string{
		`dwloc だけ L2 ["0123456789abcdef" "" "a2"]`,
		`dwloc だけ L3 ["2cf24dba5fb0a30e" "hello" "x"]`,
	}},
	"comma-only-row": {diffIntended, whyCommaRow, []string{
		`上流だけ L3 ["" "" ""]`,
	}},
	"hash-header-quoted": {diffIntended, whyHashHeader, []string{
		`dwloc だけ L2 ["7692c3ad3540bb80" "one" "いち"]`,
		`L3 の列: 上流 ["7692c3ad3540bb80" "one" "いち"] / dwloc ["#key" "source_en" "translation"]`,
	}},
	"hash-header-leading-space": {diffIntended, whyHashHeader, []string{
		`dwloc だけ L2 ["7692c3ad3540bb80" "one" "いち"]`,
		`L3 の列: 上流 ["7692c3ad3540bb80" "one" "いち"] / dwloc ["#key" "source_en" "translation"]`,
	}},
	"dup-columns-with-data": {diffGuardOnly, whyWholeDupColumns, []string{
		`読めるか: 上流 列名の重複 / dwloc 読める`,
	}},
	"dup-columns-no-data": {diffGuardOnly, whyWholeDupColumns, []string{
		`読めるか: 上流 列名の重複 / dwloc 読める`,
	}},
	"dup-columns-blank-after": {diffGuardOnly, whyWholeDupColumns, []string{
		`読めるか: 上流 列名の重複 / dwloc 読める`,
	}},
	"bare-quote-then-comment": {diffIntended, whyBareQuote, []string{
		`上流だけ L3 ["# note" "b" "c" "d" "e" "COMMENT-TAIL"]`,
	}},
	"bare-quote-then-heading": {diffIntended, whyBareQuote, []string{
		`上流だけ L4 ["# ===== UI and other text =====" "" "" "" "" ""]`,
	}},
	"quote-after-closing-quote": {diffIntended, whyBareQuote, []string{
		`上流だけ L3 ["# note" "b" "c" "d" "e" "TAIL"]`,
	}},
	"soft-hyphen-comment": {diffIntended, whyCultureHash, []string{
		`dwloc だけ L3 ["\u00ad# note" "" "" "" "" ""]`,
	}},
}

// mainReaderDiffs は、主の読み手 [ReadPowerShell] が上流と違ってよい入力。
//
// 読み方は [ReadPowerShellWhole] と同じ区切りの関数に載っているので、その表から
// 守り専用の違い（列名の重複）を除き、閉じない引用符の4件を加えたものになる。
// 表を写して持たないのは、読み方の違いを2か所で直す形にしないためである。
var mainReaderDiffs = func() map[string]knownDiff {
	out := make(map[string]knownDiff, len(wholeReaderDiffs)+4)
	for name, d := range wholeReaderDiffs {
		if d.kind != diffGuardOnly {
			out[name] = d
		}
	}
	for name, line := range map[string]int{
		"unclosed-to-eof":           2,
		"unclosed-last-line":        3,
		"unclosed-header":           1,
		"unclosed-published-middle": 2,
	} {
		out[name] = knownDiff{diffIntended, whyUnclosed, []string{
			"読めるか: 上流 読める / dwloc " + errUnclosedPrefix + strconv.Itoa(line),
		}}
	}
	return out
}()

// lineReaderDiffs は、行単位の読み手（[ReadPowerShellTable]）が上流と違ってよい入力。
//
// PR1 まで publish・diff・order はこの読み手で読んでいた。PR2 でそれらが全体を
// 解釈する読み手へ移り、diffLineBased の行は利用者から見えない違いになった。
// 「どのレコードも1行に収まるファイルでは、全体を解釈する読み方と結果が同じ」ことを
// 確かめる回帰の表として残す。
var lineReaderDiffs = map[string]knownDiff{
	"ml-translation-lf": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "いち\nに" / dwloc "いち"`,
		`dwloc だけ L3 ["に\"" "" "" "" "" "" ""]`,
	}},
	"ml-translation-crlf": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "いち\r\nに" / dwloc "いち"`,
		`dwloc だけ L3 ["に\"" "" "" "" "" "" ""]`,
	}},
	"ml-translation-blank-lines": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 5 / dwloc 2`,
		`L2 translation: 上流 "いち\n\n  \nさん" / dwloc "いち"`,
		`dwloc だけ L5 ["さん\"" "" "" "" "" "" ""]`,
	}},
	"ml-source-real-shape": {diffLineBased, whyLineSplit, []string{
		`L3 の終わり: 上流 5 / dwloc 3`,
		`L3 source_en: 上流 "para1\n\npara2" / dwloc "para1"`,
		`dwloc だけ L5 ["para2\"" "" "" "" "" "" ""]`,
	}},
	"ml-source-translated": {diffLineBased, whyLineSplit, []string{
		`L3 の終わり: 上流 5 / dwloc 3`,
		`L3 source_en: 上流 "para1\n\npara2" / dwloc "para1"`,
		`L3 translation: 上流 "段落の訳" / dwloc ""`,
		`dwloc だけ L5 ["para2\"" "段落の訳" "" "" "" "" ""]`,
	}},
	"ml-source-translated-2col": {diffLineBased, whyLineSplit, []string{
		`L3 の終わり: 上流 5 / dwloc 3`,
		`L3 source_en: 上流 "para1\n\npara2" / dwloc "para1"`,
		`L3 translation: 上流 "段落の訳" / dwloc ""`,
		`dwloc だけ L5 ["para2\"" "段落の訳"]`,
	}},
	"ml-source-mixed-lines": {diffLineBased, whyLineSplit, []string{
		`L3 の終わり: 上流 6 / dwloc 3`,
		`L3 source_en: 上流 "Menu:\n\n# not a comment\n,,left, right, up" / dwloc "Menu:"`,
		`dwloc だけ L6 ["" "" "left" "right" "up\"" "" ""]`,
	}},
	"ml-translation-only-newline": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "\n" / dwloc ""`,
	}},
	"ml-published-crlf": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "line1\nline2" / dwloc "line1"`,
		`dwloc だけ L3 ["line2\"" "" "" "" "" ""]`,
	}},
	"ml-hash-line-in-translation": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "一行目\n# 二行目" / dwloc "一行目"`,
	}},
	"ml-hash-line-in-published": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "first\n#second" / dwloc "first"`,
	}},
	"ml-published-translation": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "line1\nline2" / dwloc "line1"`,
		`dwloc だけ L3 ["line2\"" "" "" "" "" ""]`,
	}},
	"ml-continuation-looks-like-row": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 source_en: 上流 "Step one\n,UI,,,UI,y" / dwloc "Step one"`,
		`L2 translation: 上流 "手順の訳" / dwloc ""`,
		`dwloc だけ L3 ["" "UI" "" "" "UI" "y\"" "手順の訳"]`,
	}},
	"ml-line-id-translation": {diffLineBased, whyLineSplit, []string{
		`L3 の終わり: 上流 4 / dwloc 3`,
		`L3 translation: 上流 "一行目\n二行目" / dwloc "一行目"`,
		`dwloc だけ L4 ["二行目\"" "" "" "" "" "" ""]`,
	}},
	"ml-header": {diffLineBased, whyLineSplit, []string{
		`dwloc だけ L2 ["lation\"" "" ""]`,
		`L3 の列: 上流 ["key" "source_en" "trans\nlation"] / dwloc ["key" "source_en" "trans"]`,
	}},
	"ml-source-crlf-key-mismatch": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 4 / dwloc 2`,
		`L2 source_en: 上流 "para1\r\n\r\npara2" / dwloc "para1"`,
		`L2 translation: 上流 "段落の訳" / dwloc ""`,
		`dwloc だけ L4 ["para2\"" "段落の訳" "" "" "" "" ""]`,
	}},
	"unclosed-to-eof": {diffLineBased, whyLineUnclosed, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "いち\n3fc4ccfe745870e2,two,に\n" / dwloc "いち"`,
		`dwloc だけ L3 ["3fc4ccfe745870e2" "two" "に"]`,
	}},
	"unclosed-last-line": {diffLineBased, whyLineUnclosed, []string{
		`L3 translation: 上流 "b\n" / dwloc "b"`,
	}},
	"unclosed-header": {diffLineBased, whyLineUnclosed, []string{
		`dwloc だけ L2 ["7692c3ad3540bb80" "one"]`,
	}},
	"unclosed-published-middle": {diffLineBased, whyLineUnclosed, []string{
		`L2 の終わり: 上流 5 / dwloc 2`,
		`L2 translation: 上流 "a\nfedcba9876543210,UI,,,UI,b\n# note\n6d2caf9200536549,UI,,,UI,c\n" / dwloc "a"`,
		`dwloc だけ L3 ["fedcba9876543210" "UI" "" "" "UI" "b"]`,
		`dwloc だけ L5 ["6d2caf9200536549" "UI" "" "" "UI" "c"]`,
	}},
	"swallow-3col": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "いち\n,two,に" / dwloc "いち"`,
		`dwloc だけ L3 ["" "two" "に\""]`,
	}},
	"swallow-7col-hash-close": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 4 / dwloc 2`,
		`L2 translation: 上流 "いち\r\n3fc4ccfe745870e2,UI,,,UI,two,\r\n# note " / dwloc "いち"`,
		`dwloc だけ L3 ["3fc4ccfe745870e2" "UI" "" "" "UI" "two" ""]`,
	}},
	"swallow-2col-hash-close": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 4 / dwloc 2`,
		`L2 translation: 上流 "いち\ntwo,\n# note " / dwloc "いち"`,
		`dwloc だけ L3 ["two" ""]`,
	}},
	"swallow-7col-empty-key-english": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "いち\r\n,UI,,,UI,Secret" / dwloc "いち"`,
		`dwloc だけ L3 ["" "UI" "" "" "UI" "Secret, English" ""]`,
	}},
	"swallow-6col-published": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "いち\nGood day" / dwloc "いち"`,
		`dwloc だけ L3 ["Good day, friend" "UI" "" "" "UI" "こんにちは"]`,
	}},
	"swallow-2col-own-quote": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "いち\nAlpha line" / dwloc "いち"`,
		`dwloc だけ L3 ["Alpha line" ""]`,
	}},
	"swallow-7col-empty-key-own-quote": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "いち\r\n,UI,,,UI,Alpha line" / dwloc "いち"`,
		`dwloc だけ L3 ["" "UI" "" "" "UI" "Alpha line" ""]`,
	}},
	"swallow-6col-published-own-quote": {diffLineBased, whyLineSplit, []string{
		`L2 の終わり: 上流 3 / dwloc 2`,
		`L2 translation: 上流 "いち\nAlpha line" / dwloc "いち"`,
		`dwloc だけ L3 ["Alpha line" "" "" "" "" ""]`,
	}},
	"lone-cr-in-quoted-translation": {diffIntended, whyLoneCR, []string{
		`dwloc だけ L2 ["7692c3ad3540bb80" "L01 Ember" "Ember_1_intro" "1" "Ember" "one" "い"]`,
	}},
	"lone-cr-line-end": {diffIntended, whyLoneCR, []string{
		`dwloc だけ L2 ["0123456789abcdef" "UI" "" "" "UI" "a"]`,
	}},
	"lone-cr-unquoted-value": {diffIntended, whyLoneCRUnquoted, []string{
		`dwloc だけ L2 ["7692c3ad3540bb80" "L01 Ember" "Ember_1_intro" "1" "Ember" "one" "い"]`,
	}},
	"cr-only-published": {diffIntended, whyCROnly, []string{
		`dwloc だけ L2 ["0123456789abcdef" "UI" "" "" "UI" "a"]`,
		`dwloc だけ L3 ["fedcba9876543210" "UI" "" "" "UI" "b"]`,
	}},
	"cr-only-working": {diffIntended, whyCROnly, []string{
		`dwloc だけ L2 ["0123456789abcdef" "" "a2"]`,
		`dwloc だけ L3 ["2cf24dba5fb0a30e" "hello" "x"]`,
	}},
	"comma-only-row": {diffIntended, whyCommaRow, []string{
		`上流だけ L3 ["" "" ""]`,
	}},
	"hash-header-quoted": {diffIntended, whyHashHeader, []string{
		`dwloc だけ L2 ["7692c3ad3540bb80" "one" "いち"]`,
		`L3 の列: 上流 ["7692c3ad3540bb80" "one" "いち"] / dwloc ["#key" "source_en" "translation"]`,
	}},
	"hash-header-leading-space": {diffIntended, whyHashHeader, []string{
		`dwloc だけ L2 ["7692c3ad3540bb80" "one" "いち"]`,
		`L3 の列: 上流 ["7692c3ad3540bb80" "one" "いち"] / dwloc ["#key" "source_en" "translation"]`,
	}},
	"dup-columns-no-data": {diffLineBased, whyLineDupNoData, []string{
		`読めるか: 上流 列名の重複 / dwloc 読める`,
	}},
	"bare-quote-then-comment": {diffIntended, whyBareQuote, []string{
		`上流だけ L3 ["# note" "b" "c" "d" "e" "COMMENT-TAIL"]`,
	}},
	"bare-quote-then-heading": {diffIntended, whyBareQuote, []string{
		`上流だけ L4 ["# ===== UI and other text =====" "" "" "" "" ""]`,
	}},
	"quote-after-closing-quote": {diffIntended, whyBareQuote, []string{
		`上流だけ L3 ["# note" "b" "c" "d" "e" "TAIL"]`,
	}},
	"ideographic-space-line": {diffLineBased, whyLineSpaceOnly, []string{
		`dwloc だけ L3 ["\u3000" "" "" "" "" "" ""]`,
	}},
	"nbsp-line": {diffLineBased, whyLineSpaceOnly, []string{
		`dwloc だけ L3 ["\u00a0" "" "" "" "" "" ""]`,
	}},
	"soft-hyphen-comment": {diffIntended, whyCultureHash, []string{
		`dwloc だけ L3 ["\u00ad# note" "" "" "" "" ""]`,
	}},
}

// TestReadersAgainstUpstream は、2つの読み手を上流の正解と突き合わせる。
func TestReadersAgainstUpstream(t *testing.T) {
	cases, exp := loadUpstreamFixture(t)

	readers := []struct {
		name  string
		read  func(string) readView
		known map[string]knownDiff
	}{
		{"ReadPowerShell", mainView, mainReaderDiffs},
		{"ReadPowerShellWhole", wholeView, wholeReaderDiffs},
		{"ReadPowerShellTable", lineView, lineReaderDiffs},
	}
	for _, reader := range readers {
		t.Run(reader.name, func(t *testing.T) {
			counts := map[diffKind]int{}
			for i, c := range cases.Cases {
				d := describeReadDiff(upstreamView(exp.Cases[i]), reader.read(c.Text))
				pin, known := reader.known[c.Name]
				switch {
				case len(d) == 0 && known:
					t.Errorf("%s: 上流と一致するようになった。表から外す（%s: %s）", c.Name, pin.kind, pin.why)
				case len(d) > 0 && !known:
					t.Errorf("%s: 上流と違う（表に無い）\n%s", c.Name, quoteLines(d))
				case known && !slices.Equal(d, pin.diff):
					t.Errorf("%s: 違い方が変わった\n got:\n%s\nwant:\n%s", c.Name, quoteLines(d), quoteLines(pin.diff))
				}
				if known {
					counts[pin.kind]++
				}
			}
			for name := range reader.known {
				if !slices.ContainsFunc(cases.Cases, func(c fixtureCase) bool { return c.Name == name }) {
					t.Errorf("表の %s は入力の表に無い", name)
				}
			}
			t.Logf("上流と違う入力: %v（全 %d 件）", counts, len(cases.Cases))
		})
	}
}

func quoteLines(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "\t%q,\n", l)
	}
	return b.String()
}

// TestUpstreamFixtureIsReproducible は、上流を実際に走らせて正解を作り直し、
// 保存した正解と同じになるかを見る。
//
// DWLOC_UPSTREAM_REPO に上流のリポジトリを渡したときだけ走る。CI には上流も
// pwsh も無いので、飛ばせることが必須。手元の pwsh の版で作り直すので、
// 保存した正解（pwsh 7.4.6、Linux）と版や OS が違っても同じ結果になるかも
// これで確かめられる。例外の文面は pwsh の言語で変わるので比べない。
//
//	DWLOC_UPSTREAM_REPO=<上流> go test ./internal/csvfile -run UpstreamFixture
func TestUpstreamFixtureIsReproducible(t *testing.T) {
	repo := os.Getenv(upstreamRepoEnv)
	if repo == "" {
		t.Skipf("%s が無いので、上流を走らせる突き合わせは飛ばす", upstreamRepoEnv)
	}
	pwsh := os.Getenv(upstreamPwshEnv)
	if pwsh == "" {
		found, err := exec.LookPath("pwsh")
		if err != nil {
			t.Fatalf("%s が指定されているのに pwsh が見つからない（%s で場所を渡せる）", upstreamRepoEnv, upstreamPwshEnv)
		}
		pwsh = found
	}
	_, exp := loadUpstreamFixture(t)

	ref := os.Getenv(upstreamRefEnv)
	if ref == "" {
		ref = exp.Upstream.Commit
	}
	// 上流のリポジトリは読むだけ。git show は作業ツリーにもブランチにも触れない。
	script, err := exec.Command("git", "-C", repo, "show", ref+":"+exp.Upstream.Script).Output()
	if err != nil {
		t.Fatalf("上流の %s を取り出せない: %v", exp.Upstream.Script, err)
	}
	commit, err := exec.Command("git", "-C", repo, "rev-parse", ref+"^{commit}").Output()
	if err != nil {
		t.Fatalf("%s のコミットを決められない: %v", ref, err)
	}
	if blob := gitBlobName(script); blob != exp.Upstream.Blob {
		t.Logf("上流の %s は正解を作ったときと違う（正解 %s、%s は %s）", exp.Upstream.Script, exp.Upstream.Blob, ref, blob)
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "hash-strings.ps1")
	if err := os.WriteFile(scriptPath, script, 0o644); err != nil {
		t.Fatal(err)
	}
	freshPath := filepath.Join(dir, "fresh.json")
	cmd := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-File",
		filepath.Join("..", "..", "scripts", "upstream-fixtures.ps1"),
		"-Script", scriptPath, "-Commit", strings.TrimSpace(string(commit)),
		"-Out", freshPath, "-WorkDir", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("正解を作り直せない: %v\n%s", err, out)
	}
	fresh := readExpectedRaw(t, freshPath)
	saved := readExpectedRaw(t, filepath.Join(upstreamDir, "expected.json"))
	t.Logf("pwsh %s（%s）で作り直した", fresh.Pwsh.Version, fresh.Pwsh.OS)
	if fresh.CasesSHA256 != saved.CasesSHA256 || len(fresh.Cases) != len(saved.Cases) {
		t.Fatalf("入力の表が違う（作り直し %s の %d 件、保存 %s の %d 件）",
			fresh.CasesSHA256, len(fresh.Cases), saved.CasesSHA256, len(saved.Cases))
	}
	for i := range saved.Cases {
		if !bytes.Equal(fresh.Cases[i], saved.Cases[i]) {
			t.Errorf("%d 件目が保存した正解と違う\n作り直し: %s\n保存:     %s", i, fresh.Cases[i], saved.Cases[i])
		}
	}
}

// rawExpected は、正解を入力ごとの JSON のまま持つ。例外の文面は取り除いてある。
type rawExpected struct {
	Pwsh struct {
		Version string `json:"version"`
		OS      string `json:"os"`
	} `json:"pwsh"`
	CasesSHA256 string   `json:"cases_sha256"`
	Cases       [][]byte `json:"-"`
}

// readExpectedRaw は正解を読み、入力ごとに例外の文面を除いた JSON にそろえる。
func readExpectedRaw(t *testing.T, path string) rawExpected {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("正解が読めない: %v", err)
	}
	var doc struct {
		rawExpected
		Cases []map[string]any `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s を解釈できない: %v", path, err)
	}
	out := doc.rawExpected
	for _, c := range doc.Cases {
		for _, part := range []string{"read", "run"} {
			if m, ok := c[part].(map[string]any); ok {
				if e, ok := m["error"].(map[string]any); ok {
					delete(e, "message")
				}
			}
		}
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		out.Cases = append(out.Cases, b)
	}
	return out
}

// gitBlobName は git hash-object と同じ名前を返す。
func gitBlobName(content []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}
