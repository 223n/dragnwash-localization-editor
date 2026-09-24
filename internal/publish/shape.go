package publish

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

/*
ここは「1行ずつ読むと訳を黙って失う形」の守りである。

publish は入力を1物理行=1レコードで読む（csvfile.ReadPowerShellRows）。上流の
hash-strings.ps1 は f816618 から全文を解釈する読み方に移ったが、この移植は
行単位のまま据え置いている（理由は csvfile.ReadPowerShellRows の doc コメント）。

行単位の読み方には、訳を黙って失う入力がある。[CheckLoss] ではそれを捕まえ
られない。CheckLoss はいまの公開ファイルを同じ行単位の読み方で読むので、
読み違えた結果どうしを見比べることになり、食い違いが見えないからである。
そこで、読んだ結果ではなくファイルの形そのものを見て、書く前に止める。

	(a) ヘッダーに key 列も source_en 列も無い、または translation 列が無い
	    どの行もキーか訳を引けず、すべて捨てられる。source_en,translation だけの
	    作業コピーは上流も受け付ける正規の形なので通す。
	(b) いまの公開ファイル（書き出し先）に、行をまたぐレコードがある
	    1行ずつ読むと、値が最初の行で切れて訳が切り詰められる。いまの公開
	    ファイルも同じ読み方で読むので、切り詰めた訳が「失われていない」と見える。
	(c) 入力に行をまたぐレコードがあり、そこに訳が入っているか、1行ずつ読んだ
	    訳と全体を読んだ訳が食い違う
	    訳が入っていなければ止めない。ゲーム側の作業コピーの実物には、原文が
	    行をまたいで訳が空のレコードがあり（上流の報告 #7）、それで止めると
	    そのロケールの publish が常に塞がる。
	(d) 空でない行があるのに、1行ずつ読むと1行も読めないか、ヘッダーが無い
	    行の区切りが、読み手の分ける LF・CRLF・CR 以外になっているときに起きる。
	(e) 開いた引用符がファイルの終わりまで閉じない（上流の報告 #11）
	    全体を読むと、そこから後ろがすべて1つの値になる。上流の全文の読み方は
	    英語の原文まで訳に飲み込み、公開ファイルへ漏らす。

「行をまたぐ」は、引用符の偶奇を行ごとに数えて決めるのではない。
ConvertFrom-Csv と同じく、フィールドの先頭で開いた引用だけを数える
（csvfile.ReadPowerShellWhole）。引用符なしのフィールドの途中の '"'
（5" screen のような値）はただの文字で、行単位で読んでも全体を読んでも
値は同じなので、止める理由にならない。

どの形も、止めるときの終了コードは「訳が失われる」（[CheckLoss]）と同じ 1 に
そろえる。読めなくて確かめられなかったのではなく、読んだうえで「書けば訳を
失う」と分かったからである。
*/

// Hazard は、1行ずつ読むと訳を黙って失う形1件。
type Hazard struct {
	// Locale はロケール名。--path で走らせたときは空になる。
	Locale string
	// Path はその形があるファイル。
	Path string
	// Current は、そのファイルがいまの公開ファイル（書き出し先）かどうか。
	// false なら入力（作業コピー）である。入力と書き出し先が同じファイルなら true。
	Current bool
	// Line と EndLine は、形の崩れがある物理行の範囲（1始まり）。1行だけなら
	// 同じ値になる。ファイル全体のこと（ヘッダーが見つからないなど）なら両方 0。
	Line    int
	EndLine int
	// Why は止める理由。
	Why reason.Reason
}

// CheckTargetShape は、t の入力といまの書き出し先を読み、1行ずつ読むと訳を
// 失う形が無いかを確かめる。返りが空なら、どちらにもその形は無い。
//
// 入力と書き出し先が同じファイル（作業コピーの無いロケールと --path）なら、
// 書き出し先として1回だけ確かめる。書き出し先の確かめのほうが厳しい
// （行をまたぐレコードがあれば、訳の有無によらず止める）。
//
// 誤りを返すのは、ファイルを読めないときだけである。[ShapeError] に包み、
// どのファイルかを添える。
//
// ヘッダーの列名の重複は、ここでは誤りにも形の崩れにもしない。組み立て（[Build]）と
// 失われる訳の確かめ（[CheckLoss]）が同じ読み方で読んで誤りを返し、呼び出し側は
// そこで「変換できない」「確かめられない」と伝える。形の確かめは組み立てより前に
// 走るので、ここで誤りにすると、入力の列名の重複が「変換できない」でなく「読めない」と
// 伝わってしまう。
func CheckTargetShape(t Target) ([]Hazard, error) {
	var out []Hazard
	same := filepath.Clean(t.Input) == filepath.Clean(t.Output)
	if !same {
		input, err := os.ReadFile(t.Input)
		if err != nil {
			return nil, &ShapeError{Path: t.Input, Err: err}
		}
		found, err := CheckInputShape(input)
		if err != nil {
			return nil, &ShapeError{Path: t.Input, Err: err}
		}
		out = append(out, place(found, t.Locale, t.Input, false)...)
	}

	current, err := readIfExists(t.Output)
	if err != nil {
		return nil, &ShapeError{Path: t.Output, Err: err}
	}
	if current == nil {
		return out, nil
	}
	found, err := CheckCurrentShape(current)
	if err != nil {
		return nil, &ShapeError{Path: t.Output, Err: err}
	}
	return append(out, place(found, t.Locale, t.Output, true)...), nil
}

// ShapeError は、形を確かめるためにファイルを読めなかったこと。
type ShapeError struct {
	// Path は読めなかったファイル。
	Path string
	// Err は元の誤り。
	Err error
}

func (e *ShapeError) Error() string { return e.Path + ": " + e.Err.Error() }

// Unwrap は元の誤りを返す。
func (e *ShapeError) Unwrap() error { return e.Err }

// CheckInputShape は、入力（作業コピー）に (a)(c)(d)(e) の形が無いかを確かめる。
// 返す Hazard の Locale と Path は空で、Current は false。
//
// ヘッダーの列名が重複していれば何も返さない（理由は [CheckTargetShape]）。
func CheckInputShape(input []byte) ([]Hazard, error) {
	table, err := csvfile.ReadPowerShellTable(input)
	if err != nil {
		return nil, ignoreDuplicate(err)
	}
	whole := csvfile.ReadPowerShellWhole(input)

	var out []Hazard
	out = append(out, headerHazards(table)...)
	out = append(out, multilineInputHazards(table, whole)...)
	out = append(out, unreadHazards(table, input)...)
	out = append(out, unclosedHazards(whole)...)
	sortHazards(out)
	return out, nil
}

// CheckCurrentShape は、いまの公開ファイル（書き出し先）に (a)(b)(d)(e) の形が
// 無いかを確かめる。返す Hazard の Locale と Path は空で、Current は false
// （呼び出し側が埋める）。
//
// ヘッダーの列名が重複していれば何も返さない（理由は [CheckTargetShape]）。
func CheckCurrentShape(current []byte) ([]Hazard, error) {
	table, err := csvfile.ReadPowerShellTable(current)
	if err != nil {
		return nil, ignoreDuplicate(err)
	}
	whole := csvfile.ReadPowerShellWhole(current)

	var out []Hazard
	out = append(out, headerHazards(table)...)
	// (b) 訳の有無によらず止める。入力のほうと違い、ここにある訳はもう
	// コミット済みで、切り詰めた訳を書けばそれが失われる。行をまたぐ値が
	// 原文だけ、という作業コピーの事情はいまの公開ファイルには無い（公開
	// ファイルは原文を持たない）。
	for _, span := range multilineSpans(whole) {
		out = append(out, Hazard{Line: span.line, EndLine: span.end,
			Why: reason.New(reason.PublishMultilineCurrent,
				"引用符で囲んだ値が行をまたいでいる。1行ずつ読むと、値が最初の行で切れて訳が切り詰められる")})
	}
	out = append(out, unreadHazards(table, current)...)
	out = append(out, unclosedHazards(whole)...)
	sortHazards(out)
	return out, nil
}

// ignoreDuplicate は、読み手の誤りのうち列名の重複を無かったことにする
// （理由は [CheckTargetShape]）。
func ignoreDuplicate(err error) error {
	var dup *csvfile.DuplicateColumnError
	if errors.As(err, &dup) {
		return nil
	}
	return err
}

// headerHazards は (a) を確かめる。ヘッダーが無いファイルは (d) に任せる。
//
// 列名の照合は [csvfile.Row] と同じく大文字小文字を区別しない。
func headerHazards(table csvfile.PowerShellTable) []Hazard {
	if table.HeaderLine == 0 {
		return nil
	}
	has := func(name string) bool {
		want := csvfile.FoldASCII(name)
		return slices.ContainsFunc(table.Header, func(c string) bool { return csvfile.FoldASCII(c) == want })
	}
	var out []Hazard
	if !has(colKey) && !has(colSourceEn) {
		out = append(out, Hazard{Line: table.HeaderLine, EndLine: table.HeaderLine,
			Why: reason.New(reason.PublishNoKeyColumn,
				"ヘッダーに key 列も source_en 列も無いので、どの行もキーを決められず、すべて捨てられる")})
	}
	if !has(colTranslation) {
		out = append(out, Hazard{Line: table.HeaderLine, EndLine: table.HeaderLine,
			Why: reason.New(reason.PublishNoTranslationColumn,
				"ヘッダーに translation 列が無いので、すべての行が訳の無い行として読まれる")})
	}
	return out
}

// lineSpan は物理行の範囲。
type lineSpan struct {
	line, end int
}

// multilineSpans は、行をまたぐレコード（ヘッダーを含む）の範囲を並べる。
// 閉じない引用符のレコードは (e) で出すので入れない。
func multilineSpans(whole csvfile.PowerShellWhole) []lineSpan {
	var out []lineSpan
	if whole.Header.MultiLine() && !whole.Header.Unclosed {
		out = append(out, lineSpan{whole.Header.Line, whole.Header.EndLine})
	}
	for _, r := range whole.Rows {
		if r.MultiLine() && !r.Unclosed {
			out = append(out, lineSpan{r.Line, r.EndLine})
		}
	}
	return out
}

// multilineInputHazards は (c) を確かめる。
//
// 止めるのは次のどちらかに当たるときだけである。
//
//   - 行をまたぐレコードに、空でない訳が入っている。1行ずつ読むと、その訳は
//     切り詰められるか（訳そのものが行をまたぐとき）、行ごと捨てられる
//     （原文が行をまたぎ、1行目の原文のハッシュがキーと合わないとき）
//   - 1行ずつ読んだ訳と全体を読んだ訳が食い違う。続きの行が、1行ずつ読むと
//     別のキーの訳として読まれることがある
//
// 訳の入っていない行をまたぐレコードは、どちらの読み方でも公開されない
// （R20）ので止めない。そこで止めると、原文が行をまたぐだけの実物の作業コピー
// で publish が常に塞がる。
//
// 閉じない引用符があると全体の読み方は後ろを丸ごと飲み込むので、突き合わせは
// しない。(e) で止まるので、それで足りる。
func multilineInputHazards(table csvfile.PowerShellTable, whole csvfile.PowerShellWhole) []Hazard {
	spans := multilineSpans(whole)
	if len(spans) == 0 {
		return nil
	}

	var out []Hazard
	reported := make(map[lineSpan]bool, len(spans))
	for _, r := range whole.Rows {
		if !r.MultiLine() || r.Unclosed || r.Get(colTranslation) == "" {
			continue
		}
		span := lineSpan{r.Line, r.EndLine}
		reported[span] = true
		out = append(out, Hazard{Line: span.line, EndLine: span.end,
			Why: reason.New(reason.PublishMultilineTranslated,
				"引用符で囲んだ値が行をまたぎ、この行には訳が入っている。1行ずつ読むと、この訳は切り詰められるか、行ごと捨てられる")})
	}
	if whole.UnclosedLine > 0 {
		return out
	}

	lineRows := make([]csvfile.Row, len(table.Rows))
	for i, r := range table.Rows {
		lineRows[i] = r.Row
	}
	wholeRows := make([]csvfile.Row, len(whole.Rows))
	for i, r := range whole.Rows {
		wholeRows[i] = r.Row
	}
	if sameTranslations(collectRows(lineRows), collectRows(wholeRows)) {
		return out
	}

	// どのレコードのせいかを、1行ずつ読んだ行のうち、またいだ範囲の中から
	// 訳として読まれてしまう行があるかで決める。
	diverges := reason.New(reason.PublishMultilineDiverges,
		"引用符で囲んだ値が行をまたぎ、1行ずつ読むと続きの行が別の訳として読まれる")
	found := false
	for _, span := range spans {
		if reported[span] {
			continue
		}
		for _, r := range table.Rows {
			if r.Line >= span.line && r.Line <= span.end && publishes(r.Row) {
				out = append(out, Hazard{Line: span.line, EndLine: span.end, Why: diverges})
				found = true
				break
			}
		}
	}
	if !found && len(out) == 0 {
		// 上で決められなかったときも、食い違いがあることは確かなので止める。
		// 食い違いは行をまたぐレコードからしか生まれないので、最初のものを指す。
		out = append(out, Hazard{Line: spans[0].line, EndLine: spans[0].end, Why: diverges})
	}
	return out
}

// publishes は、1行ずつ読んだ行が公開される訳を持つかを返す（[collectRows] が
// rows か lines へ入れる行）。
func publishes(row csvfile.Row) bool {
	if row.Get(colTranslation) == "" {
		return false
	}
	_, how := rowKey(row)
	return how != keyDropped
}

// sameTranslations は、2つの読み方で集めた訳が同じかを返す。
//
// 見るのは「訳のある行の集合」と「その訳」だけである。並び順と speaker は
// 見ない。守りたいのは訳で、並びや speaker が変わっても訳は失われない。
func sameTranslations(a, b *collected) bool {
	if len(a.rows) != len(b.rows) || len(a.lines.entries) != len(b.lines.entries) {
		return false
	}
	for k, r := range a.rows {
		if o, ok := b.rows[k]; !ok || o.Translation != r.Translation {
			return false
		}
	}
	for _, e := range a.lines.entries {
		if tr, ok := b.lines.Lookup(e.id); !ok || tr != e.translation {
			return false
		}
	}
	return true
}

// unreadHazards は (d) を確かめる。
//
// 読み手が分ける改行（LF・CRLF・CR）よりも広く行を分けて、レコードになる行を
// 数える。広く分けて2行以上あるのに、読み手が1行もデータ行を読めなければ、
// 行の区切りを読み違えている。U+2028 や U+0085 で行を分けたファイルが当たる。
//
// CR だけの改行のファイルはここでは止まらない。読み手は単独の CR でも行を
// 分けるので、そのファイルは正しく読める（書き出すと LF になる）。
func unreadHazards(table csvfile.PowerShellTable, data []byte) []Hazard {
	n := countRecordLines(data)
	unread := (table.HeaderLine == 0 && n > 0) || (len(table.Rows) == 0 && n >= 2)
	if !unread {
		return nil
	}
	return []Hazard{{Why: reason.New(reason.PublishRowsUnread,
		"空でない行があるのに、1行ずつ読むと1行も読めない。行の区切りが LF・CRLF・CR 以外になっている")}}
}

// countRecordLines は、Python の str.splitlines と同じ広さで行を分け、
// '#' の行と空行相当の行を除いた行を数える。
func countRecordLines(data []byte) int {
	lines := strings.FieldsFunc(csvfile.TrimBOMString(string(data)), isAnyLineBreak)
	n := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, CommentPrefix) {
			continue
		}
		if _, ok := csvfile.ParsePowerShellRecord(line); ok {
			n++
		}
	}
	return n
}

// isAnyLineBreak は、Python の str.splitlines が行の区切りにする文字かを返す。
func isAnyLineBreak(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// unclosedHazards は (e) を確かめる。
func unclosedHazards(whole csvfile.PowerShellWhole) []Hazard {
	if whole.UnclosedLine == 0 {
		return nil
	}
	end := whole.Header.EndLine
	if n := len(whole.Rows); n > 0 && whole.Rows[n-1].Unclosed {
		end = whole.Rows[n-1].EndLine
	}
	return []Hazard{{Line: whole.UnclosedLine, EndLine: end,
		Why: reason.New(reason.PublishUnclosedQuote,
			"開いた引用符がファイルの終わりまで閉じない。全体として読むと、ここから後ろがすべて1つの値になる")}}
}

// place は Hazard に、どのロケールのどのファイルかを埋める。
func place(found []Hazard, locale, path string, current bool) []Hazard {
	for i := range found {
		found[i].Locale = locale
		found[i].Path = path
		found[i].Current = current
	}
	return found
}

// sortHazards は行の順に並べる。同じ行なら見つけた順のまま。
func sortHazards(hs []Hazard) {
	slices.SortStableFunc(hs, func(a, b Hazard) int { return a.Line - b.Line })
}
