package publish

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

/*
ここは「読むと訳や原文を取り違える形」の守りである。

publish は上流 main の hash-strings.ps1 と同じく、ファイル全体を1つの文字列として
解釈して読む（csvfile.ReadPowerShell）。引用符で囲んだ値は物理行をまたいで1つの値に
なる。読み手は規則どおりに読むだけで、引用符の閉じ誤りと正当な複数行の値を
見分けない。上流は閉じ誤りのある作業コピーから、英語の原文やほかの行のキーを訳として
公開ファイルへ書く（上流の報告 #11）。[CheckLoss] ではこれを捕まえられない。いまの
公開ファイルの訳は消えず、英文が足されるだけだからである。そこで、読んだ結果ではなく
ファイルの形そのものを見て、書く前に止める。

	(a) ヘッダーに key 列も source_en 列も無い、translation 列が無い、または最初の
	    列名が '#' で始まる
	    どの行もキーか訳を引けず、すべて捨てられる。'#' で始まるヘッダーは、上流が
	    飛ばして次の行（データ）をヘッダーにするので、訳を1行も公開しない。
	    source_en,translation だけの作業コピーは上流も受け付ける正規の形なので通す。
	(d) 空でない行があるのに1行も読めない
	    行の区切りが、読み手の分ける LF・CRLF・CR 以外（U+2028 など）になっている。
	(e) 開いた引用符がファイルの終わりまで閉じない（上流の報告 #11）
	    読み手が型付きの誤り（csvfile.UnclosedQuoteError）を返す。組み立ての誤り
	    （終了コード 2）にせず、ここで直し方の案内として出す。
	(f) 飲み込み（引用符が別の行で閉じ、後ろの行を値に飲み込んだと見られる形）
	    見分けは csvfile.FindSwallows。正当な複数行の値でも当たることがある（原文の
	    2行目がカンマを多く含むなど）ので、単独で読むとレコードに見える行（キーの形、
	    ヘッダーと同じ列の数）は、確かめたうえで通す指定（cmd/dwloc の
	    --accept-multiline）で通せる。閉じ引用符の後ろに文字が続く形は、どの書き手も
	    作らないので通さない。続きの行がレコードに見えても、同じレコードに閉じ引用符の
	    後ろに文字が続く行があれば、そちらの理由で止める（csvfile.FindSwallows）。
	(g) 単独の CR
	    値の中の単独の CR（上流の道具はその行を落とす）と、引用の外の単独の CR で
	    切れた値（ゲームは CR を捨ててつなげて読み、publish は切れた前半だけを書く）。
	(h) 再生順のデータ（data/script_order.csv と data/level_flow.csv）の値のうち、
	    publish が引用せずにそのまま書く値（見出しの文言と order 列）の改行
	    （[CheckOrderShape]。再生順のデータの閉じない引用符 (e) も同じ関数が見る）。

確かめるファイルは、入力（作業コピー）、いまの公開ファイル（書き出し先）、ゲーム側の
公開ファイル（土台の確かめ [CheckBase] が読むもの）の3つで、どれも同じ関数
（[CheckShape]）を通す。入力と書き出し先が同じファイル（作業コピーの無いロケールと
--path）なら1回だけ確かめる。飲み込みの守りは、入力と書き出し先が同じ経路でも
効かなければならない。そのファイルの中の訳を、同じファイルのいまの訳と比べても、
食い違いは見えないからである。

止めるときの終了コードは「訳が失われる」（[CheckLoss]）と同じ 1 にそろえる。
読めなくて確かめられなかったのではなく、読んだうえで「書けば訳を取り違える」と
分かったからである。
*/

// Hazard は、読むと訳や原文を取り違える形1件。
type Hazard struct {
	// Locale はロケール名。--path で走らせたときは空になる。
	Locale string
	// Path はその形があるファイル。
	Path string
	// Current は、そのファイルがいまの公開ファイル（書き出し先）かどうか。
	// 入力と書き出し先が同じファイルなら true。
	Current bool
	// GameBase は、そのファイルがゲーム側の公開ファイル（[Target.GameBase]）かどうか。
	// Current・GameBase・Order がどれも false なら入力（作業コピー）である。
	GameBase bool
	// Order は、そのファイルが再生順のデータ（data/script_order.csv か
	// data/level_flow.csv。[CheckOrderShape]）かどうか。そのときの Locale は空で、
	// どのロケールの出力にも効く。
	Order bool
	// Line と EndLine は、形の崩れがある物理行の範囲（1始まり）。1行だけなら
	// 同じ値になる。ファイル全体のこと（ヘッダーが見つからないなど）なら両方 0。
	Line    int
	EndLine int
	// Why は止める理由。
	Why reason.Reason
}

// Acceptable は、確かめたうえで通す指定で通してよい形かを返す。
//
// 通せるのは、行をまたぐレコードの続きの行が、単独で読むとレコードに見える形
// （キーの形、ヘッダーと同じ列の数）だけである。正当な複数行の値でも当たる
// （原文の2行目がカンマを多く含む、source_en,translation の2列の作業コピーで原文が
// 行をまたぐ）ので、止めたままにすると、そのロケールを publish できなくなる。
// ほかの形は直せば通る（閉じ引用符の後ろに文字を書く書き手は無く、単独の CR は
// LF に直せる）ので、通させない。
//
// 通せるかは理由の識別子だけで決める。続きの行がレコードに見えても、同じレコードに
// 閉じ引用符の後ろに文字が続く行があれば、csvfile.FindSwallows がその行だけを
// text-after-quote（reason.PublishSwallowTextAfterQuote）で返すので、ここで通せる
// 形にはならない。その順を崩すと、
// 英語の原文やキーを飲み込んだ訳が、通す指定で公開される。
func (h Hazard) Acceptable() bool {
	return h.Why.ID == reason.PublishSwallowKeyShaped || h.Why.ID == reason.PublishSwallowSameColumns
}

// CheckTargetShape は、t の入力・いまの書き出し先・ゲーム側の公開ファイルを読み、
// 読むと訳や原文を取り違える形が無いかを確かめる。返りが空なら、どれにもその形は無い。
//
// 入力と書き出し先が同じファイル（作業コピーの無いロケールと --path）なら、
// 書き出し先として1回だけ確かめる。ゲーム側の公開ファイルは、書き出し先とゲーム側の
// 両方があるときだけ確かめる。土台の確かめ（[CheckBase]）がそのファイルを読むのは
// その場合だけだからである（cmd/dwloc の reportBaseDrift）。
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
		out = append(out, place(CheckShape(input), t.Locale, t.Input, false, false)...)
	}

	current, err := readIfExists(t.Output)
	if err != nil {
		return nil, &ShapeError{Path: t.Output, Err: err}
	}
	if current == nil {
		return out, nil
	}
	out = append(out, place(CheckShape(current), t.Locale, t.Output, true, false)...)

	if t.GameBase == "" {
		return out, nil
	}
	game, err := readIfExists(t.GameBase)
	if err != nil {
		return nil, &ShapeError{Path: t.GameBase, Err: err}
	}
	if game == nil {
		return out, nil
	}
	return append(out, place(CheckShape(game), t.Locale, t.GameBase, false, true)...), nil
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

// CheckShape は、1つのファイルに (a)(d)(e)(f)(g) の形が無いかを確かめる。
// 入力・いまの公開ファイル・ゲーム側の公開ファイルのどれにも同じ確かめをかける。
// 返す Hazard の Locale・Path・Current・GameBase は空で、呼び出し側が埋める。
//
// 誤りは返さない。読み手の誤りのうち、閉じない引用符は (e) として返し、列名の
// 重複は見ない（理由は [CheckTargetShape]）。
func CheckShape(data []byte) []Hazard {
	f := csvfile.ReadPowerShellMarked(data)

	var out []Hazard
	out = append(out, headerHazards(f)...)
	out = append(out, unreadHazards(f, data)...)
	out = append(out, unclosedHazards(f)...)
	out = append(out, swallowHazards(f)...)
	out = append(out, loneCRHazards(f)...)
	out = append(out, crCutHazards(f)...)
	sortHazards(out)
	return out
}

// headerHazards は (a) を確かめる。ヘッダーが無いファイルは (d) に任せる。
//
// ヘッダーの中で開いた引用符が閉じないときは見ない。列名にファイルの終わりまでが
// 飲み込まれているので、列が無いと言っても直す先を指さない。(e) だけで出す。
//
// 列名の照合は [csvfile.Row] と同じく大文字小文字を区別しない。
func headerHazards(f csvfile.PowerShellFile) []Hazard {
	h := f.Header
	if h.ID == 0 || h.Unclosed {
		return nil
	}
	has := func(name string) bool {
		want := csvfile.FoldASCII(name)
		return slices.ContainsFunc(h.Fields, func(c string) bool { return csvfile.FoldASCII(c) == want })
	}
	var out []Hazard
	if h.CommentLike() {
		out = append(out, Hazard{Line: h.Line, EndLine: h.EndLine,
			Why: reason.New(reason.PublishHashHeader,
				"ヘッダーの最初の列名が '#' で始まる。上流の tools/hash-strings.ps1 はこのヘッダーを飛ばして次の行をヘッダーにするので、このファイルの訳を1行も公開しない")})
	}
	if !has(colKey) && !has(colSourceEn) {
		out = append(out, Hazard{Line: h.Line, EndLine: h.EndLine,
			Why: reason.New(reason.PublishNoKeyColumn,
				"ヘッダーに key 列も source_en 列も無いので、どの行もキーを決められず、すべて捨てられる")})
	}
	if !has(colTranslation) {
		out = append(out, Hazard{Line: h.Line, EndLine: h.EndLine,
			Why: reason.New(reason.PublishNoTranslationColumn,
				"ヘッダーに translation 列が無いので、すべての行が訳の無い行として読まれる")})
	}
	return out
}

// unreadHazards は (d) を確かめる。
//
// 読み手が分ける改行（LF・CRLF・CR）よりも広く行を分けて、レコードになる行を
// 数える。広く分けて2行以上あるのに、読み手が1行もデータのレコードを読めなければ
// （あるいは1行でもあるのにヘッダーが無ければ）、行の区切りを読み違えている。
// U+2028 や U+0085 で行を分けたファイルが当たる。
//
// 見るのは、読み手が行の区切りにしない改行の文字がファイルにあるときだけである。
// そうした文字が無ければ、広く分けても読み手と同じ行に分かれるので、読み違えは
// 起きない。行をまたぐヘッダーだけでデータの無いファイルは、広く分けると2行に
// なるが、読み手は正しく0件と読んでいる。
//
// CR だけの改行のファイルはここでは止まらない。読み手は単独の CR でも行を
// 分けるので、そのファイルは正しく読める（書き出すと LF になる）。
func unreadHazards(f csvfile.PowerShellFile, data []byte) []Hazard {
	if !strings.ContainsFunc(string(data), isForeignLineBreak) {
		return nil
	}
	n := countRecordLines(data)
	unread := (f.Header.ID == 0 && n > 0) || (len(f.Records) == 0 && n >= 2)
	if !unread {
		return nil
	}
	return []Hazard{{Why: reason.New(reason.PublishRowsUnread,
		"空でない行があるのに、1行も読めない。行の区切りが LF・CRLF・CR 以外になっている")}}
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
	return r == '\n' || r == '\r' || isForeignLineBreak(r)
}

// isForeignLineBreak は、Python の str.splitlines が行の区切りにするが、読み手
// （LF・CRLF・CR で分ける）は区切りにしない文字かを返す。
func isForeignLineBreak(r rune) bool {
	switch r {
	case '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// unclosedHazards は (e) を確かめる。
func unclosedHazards(f csvfile.PowerShellFile) []Hazard {
	if f.Unclosed == nil {
		return nil
	}
	last := f.Segments.List[len(f.Segments.List)-1]
	return []Hazard{{Line: f.Unclosed.Line, EndLine: last.EndLine,
		Why: reason.New(reason.PublishUnclosedQuote,
			"開いた引用符がファイルの終わりまで閉じない。全体として読むと、ここから後ろがすべて1つの値になる")}}
}

// swallowHazards は (f) を確かめる。見分けは [csvfile.FindSwallows] で、飲み込まれたと
// 疑う物理行1つにつき1件を返す。範囲は飲み込んだレコード（ヘッダーのこともある）の
// 物理行で、疑う物理行は理由の置換 line に入れる。
func swallowHazards(f csvfile.PowerShellFile) []Hazard {
	var out []Hazard
	for _, s := range csvfile.FindSwallows(f.Segments) {
		line := strconv.Itoa(s.SwallowedLine)
		var why reason.Reason
		switch s.Sign {
		case csvfile.SignKeyShaped:
			why = reason.New(reason.PublishSwallowKeyShaped,
				line+"行目が、単独で読むとキーの形で始まるレコードに見える。引用符が閉じ損ね、後ろの行を値に飲み込んでいると見られる",
				"line", line)
		case csvfile.SignSameColumns:
			why = reason.New(reason.PublishSwallowSameColumns,
				line+"行目が、単独で読むとヘッダーと同じ列の数のレコードに見える。引用符が閉じ損ね、後ろの行を値に飲み込んでいると見られる",
				"line", line)
		default:
			why = reason.New(reason.PublishSwallowTextAfterQuote,
				line+"行目で、行をまたいだ引用が閉じたすぐ後ろに文字が続く。引用符が閉じ損ね、後ろの行を値に飲み込んでいると見られる",
				"line", line)
		}
		out = append(out, Hazard{Line: s.Line, EndLine: s.EndLine, Why: why})
	}
	return out
}

// loneCRHazards は (g) のうち、値の中の単独の CR を確かめる。
//
// 読み手は、引用符で囲んだ値の中の単独の CR を値に残す。上流の hash-strings.ps1 は、
// そのレコードをあとで落とす（Remove-NonRecords が単独の CR を行末と見なさず、その
// 手前を捨てる）。公開ファイルに残すと、上流の道具を通したときに訳が消える
// （決まったことのそのほか 1）。
//
// 止めるのは訳の入ったレコードだけである。訳の空のレコードはどちらの道具でも
// 公開されないので、原文に単独の CR があっても失うものが無い（原文は翻訳者には
// 直せない。直すとキーが変わる）。訳の入ったレコードの原文で止めたときも、原文を
// 直させず、訳を空に戻すよう案内する（直し方は cmd/dwloc が列で分ける）。閉じない
// 引用符のレコードは (e) に任せる。
func loneCRHazards(f csvfile.PowerShellFile) []Hazard {
	translated := make(map[int]bool, len(f.Records))
	for _, r := range f.Records {
		translated[r.ID] = !r.Unclosed && r.Get(colTranslation) != ""
	}
	var out []Hazard
	for _, v := range csvfile.LoneCRValues(f) {
		if !translated[v.ID] {
			continue
		}
		out = append(out, Hazard{Line: v.Line, EndLine: v.EndLine,
			Why: reason.New(reason.PublishLoneCR,
				v.Column+" 列の値に単独の CR（後ろに LF の続かない CR）がある。上流の tools/hash-strings.ps1 は、この行を落とす",
				"column", v.Column)})
	}
	return out
}

// crCutHazards は (g) のうち、引用の外の単独の CR で切れた値を確かめる
// （決まったことの 11 と 14。見分けは [csvfile.FindCRCuts]）。
//
// 訳の有無によらず止める。切れた後半が訳を持っていることがあり（`k,UI,,,UI,原\r文,訳`
// なら「文,訳」が別の行になる）、その訳は黙って落ちる。
func crCutHazards(f csvfile.PowerShellFile) []Hazard {
	var out []Hazard
	for _, c := range csvfile.FindCRCuts(f.Segments) {
		next := strconv.Itoa(c.NextLine)
		out = append(out, Hazard{Line: c.Line, EndLine: c.EndLine,
			Why: reason.New(reason.PublishCRCut,
				"引用符で囲まない値が行の終わりの単独の CR で切れ、"+next+"行目にある続きが別の行として読まれる。ゲームは CR を捨ててつなげて読む",
				"line", next)})
	}
	return out
}

// orderRawColumns は、再生順のデータの列のうち、publish が引用せずにそのまま
// 書く値の列。ファイル名から引く。
//
// script_order.csv の section・node・phase・condition は見出しの行（'# ===== … ====='
// と '# --- … ---'）に、order は本体の行に、エスケープせずに書く（上流と同じ。
// 移植仕様 R24f）。level_flow.csv の dragon・weather・set_flags・end_flags は、
// セクションの見出しの文言（order.LevelMeta.Header）になる。key と line_id もそのまま
// 書くが、入力の訳のキーと一致した行しか書かないので、改行の入った値は書かれない。
var orderRawColumns = map[string][]string{
	scriptOrderFile: {"section", "phase", "node", "condition", "order"},
	levelFlowFile:   {"dragon", "weather", "set_flags", "end_flags"},
}

// CheckOrderShape は、root 配下の再生順のデータ（data/script_order.csv と
// data/level_flow.csv）を読み、公開ファイルを読み違える形にする値が無いかを確かめる。
// 返りが空なら、どちらにもその形は無い。ファイルが無ければ確かめない。
//
//	(e) 開いた引用符がファイルの終わりまで閉じない
//	    再生順を読めない。読み込みの誤り（終了コード 2）ではなく、どのファイルの
//	    何行目かを直し方とともに出すために、ここで見る。
//	(h) publish が引用せずにそのまま書く値（[orderRawColumns]）に CR か LF がある
//	    全体を解釈して読むと、引用した値に改行が入りうる。そのまま見出しの行へ書くと、
//	    見出しの2行目が '#' で始まらない行になり、次に publish で読むときデータの
//	    行として読まれる。その行に '"' があれば、後ろの行を値に飲み込む
//	    （決まったことのそのほか 8。上流は同じ壊れ方をする）。行単位で読んでいた
//	    あいだは、値が行をまたがないので起きなかった。
//
// どちらも、確かめたうえで通す指定（--accept-multiline）では通さない。直せる形で、
// 翻訳の正しさとは関係が無いからである。誤りを返すのは、ファイルを読めないときだけで、
// [ShapeError] に包む。
func CheckOrderShape(root string) ([]Hazard, error) {
	var out []Hazard
	for _, path := range []string{ScriptOrderPath(root), LevelFlowPath(root)} {
		data, err := readIfExists(path)
		if err != nil {
			return nil, &ShapeError{Path: path, Err: err}
		}
		if data == nil {
			continue
		}
		found := checkOrderFile(data, orderRawColumns[filepath.Base(path)])
		for i := range found {
			found[i].Path, found[i].Order = path, true
		}
		out = append(out, found...)
	}
	return out, nil
}

// checkOrderFile は、再生順のデータ1つに (e) と (h) の形が無いかを確かめる。
// columns は (h) で見る列。
func checkOrderFile(data []byte, columns []string) []Hazard {
	f := csvfile.ReadPowerShellMarked(data)
	if f.Unclosed != nil {
		// そこから後ろが1つの値に崩れているので、値の改行は見ない。直す先は
		// 閉じない引用符である。
		return unclosedHazards(f)
	}
	var out []Hazard
	for _, v := range csvfile.LineBreakValues(f, columns...) {
		out = append(out, Hazard{Line: v.Line, EndLine: v.EndLine,
			Why: reason.New(reason.PublishOrderLineBreak,
				v.Column+" 列の値に改行がある。publish はこの値を引用せずにそのまま書くので、改行の後ろが次に読むとき別の行として読まれる",
				"column", v.Column)})
	}
	return out
}

// place は Hazard に、どのロケールのどのファイルかを埋める。
func place(found []Hazard, locale, path string, current, gameBase bool) []Hazard {
	for i := range found {
		found[i].Locale = locale
		found[i].Path = path
		found[i].Current = current
		found[i].GameBase = gameBase
	}
	return found
}

// sortHazards は行の順に並べる。同じ行なら見つけた順のまま。
func sortHazards(hs []Hazard) {
	slices.SortStableFunc(hs, func(a, b Hazard) int { return a.Line - b.Line })
}
