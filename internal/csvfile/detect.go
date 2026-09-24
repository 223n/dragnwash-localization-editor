package csvfile

import (
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/key"
)

/*
ここは、全体を解釈して読むと訳や原文を取り違える形を見つける関数である。
読み手（[ReadPowerShell]）は読み方の規則どおりに読むだけで、引用符の閉じ誤りと
正当な複数行の値を見分けない。見分けは、ここの関数が形を見て行う。止めるか
どうかは呼び出し側（publish の形の確かめ、画面の読み取り専用の判定）が決める。
publish と画面は同じ関数を呼ぶ。別々に書くと、片方だけが止める形ができる。

物理行を単独で読むときは、行単位の読み方（[parsePowerShellField] と [FieldOffsets]）
を使う。行単位の読み手を検出のためにパッケージの中に残しているのは、このためである。
*/

// RecordSign は、続きの物理行がレコードを飲み込まれたものだと疑う理由。
//
// SignKeyShaped と SignSameColumns は、その物理行を単独で読むとレコードに見える
// ことを表す。正当な複数行の値でも当たる。SignTextAfterQuote は、全体を解釈して
// 読んだときの引用の閉じ方が、飲み込んだときにしか起きない形であることを表す。
// その行が単独で読むとレコードに見えるかは問わない。
type RecordSign int

const (
	// SignKeyShaped は、最初の値がキーの形（16桁の16進か台詞ID）であること。
	SignKeyShaped RecordSign = iota + 1
	// SignSameColumns は、区切りの数（引用の外のカンマの数+1）がヘッダーの列数と
	// 同じであること。
	SignSameColumns
	// SignTextAfterQuote は、行をまたいだ引用がこの物理行で閉じ、閉じ引用符の
	// すぐ後ろに文字が続くこと（`"いち` の次の行の `"Alpha line` のような形）。
	SignTextAfterQuote
)

// String は理由の名前を返す。試験の出力と、呼び出し側の理由の文に使う。
func (s RecordSign) String() string {
	switch s {
	case SignKeyShaped:
		return "key-shaped"
	case SignSameColumns:
		return "same-columns"
	case SignTextAfterQuote:
		return "text-after-quote"
	}
	return "unknown"
}

// keyShaped は、値がキーの形かを返す。
//
// publish がキーを決めるときと同じ扱いで見る。前後の空白を除き（移植仕様 R11）、
// 台詞ID はそのまま、それ以外は小文字にしてから16桁の16進かを見る（R13、R16）。
func keyShaped(v string) bool {
	v = strings.TrimSpace(v)
	return key.LooksLikeLineID(v) || key.LooksLike(strings.ToLower(v))
}

// looksLikeRecord は、1物理行を単独で読むとレコードに見えるかを返す。columns は
// ヘッダーの区切りの数（ヘッダーのセグメントの Offsets の個数）。
func looksLikeRecord(line string, columns int) (RecordSign, bool) {
	if keyShaped(parsePowerShellField(line, 0, false).value) {
		return SignKeyShaped, true
	}
	if len(FieldOffsets(line)) == columns {
		return SignSameColumns, true
	}
	return 0, false
}

// textAfterQuoteLines は、seg のフィールドのうち、行をまたいだ引用が閉じたすぐ
// 後ろに文字が続くものについて、その閉じ引用符の物理行を返す。
//
// 飲み込まれた行が自分の値を引用符で開く形（`one,"いち` の次に `"Alpha line` が
// 来るなど）では、その開き引用符が前の行の閉じ忘れた引用を閉じ、後ろの文字は
// 閉じ引用符の後ろの文字として値に足される（ConvertFrom-Csv の規則。`"a"x` は ax）。
// 続きの物理行を単独で読むと、引用が開いたまま行が終わるので、区切りの数が
// ヘッダーより少なくなり、キーの形にも当たらない。単独で読む見方だけでは漏れるので、
// 全体を解釈したときの閉じ方で見る。
//
// 閉じ引用符の後ろに文字を書く書き手は無い（上流の Escape-Csv も、ゲームの
// WorkingCopy.cs が使う CsvReader.Escape も値全体を引用し、閉じ引用符の後ろは
// 区切りか改行になる）ので、正当な複数行の値（値が改行で終わるものを含む）では
// 当たらない。同じ行の中で開いて閉じた引用の
// 後ろの文字（`"a"x`）は、飲み込みの証拠にならないので見ない。
func textAfterQuoteLines(segs Segments, seg Segment) map[int]bool {
	var lines map[int]bool
	for _, start := range seg.Offsets {
		f := parsePowerShellField(segs.Text, start, true)
		if f.tailAt == 0 {
			continue
		}
		if line := segs.lineAt(f.tailAt); line > segs.lineAt(start) {
			if lines == nil {
				lines = make(map[int]bool)
			}
			lines[line] = true
		}
	}
	return lines
}

// Swallow は、行をまたぐレコードが飲み込んだと見られる物理行1つ。
type Swallow struct {
	// ID・Line・EndLine は、飲み込んだレコード（ヘッダーのこともある）の
	// セグメントの ID と物理行の範囲。
	ID, Line, EndLine int
	// SwallowedLine は、飲み込まれたと疑う続きの物理行。
	SwallowedLine int
	// Sign は疑う理由。レコードに閉じ引用符の後ろに文字が続く行があれば、その
	// レコードは [SignTextAfterQuote] の行だけを返す（[FindSwallows]）。そうでなければ、
	// 単独で読むとレコードに見える理由（キーの形、区切りの数の順）を採る。
	Sign RecordSign
}

// FindSwallows は、引用符が別の行で閉じて後ろの行を値に飲み込んだと見られる
// ところを返す（決まったことの 4）。
//
// 行をまたぐレコードの続きの物理行を1行ずつ単独で読み、レコードに見えれば
// 返す。レコードに見えるのは、最初の値がキーの形のときか、区切りの数がヘッダーの
// 列数と同じときである。後者は、7列の作業コピーでキー列が空の英文の行を
// 飲み込む形や、source_en,translation の2列の作業コピーで次の行を飲み込む形を
// 捕まえるためにある（キーの形だけでは漏れる）。
//
// 単独で読んでもレコードにならない行（空行・空白だけの行・'#' で始まる行）は
// 見ない。見ると、原文の段落のあいだの空行や、値の中の '#' の行で止まる。
//
// 飲み込まれた行が自分の値を引用符で開く形では、その行を単独で読むと引用が
// 開いたまま終わり、上の2つに当たらない。そのため、行をまたいだ引用が閉じた
// すぐ後ろに文字が続く物理行も返す（[SignTextAfterQuote]。見方は
// [textAfterQuoteLines]）。こちらは行の中身ではなく全体を解釈したときの閉じ方で
// 見るので、'#' で始まる行でも当たる。
//
// レコードにこの行が1つでもあれば、そのレコードはこの行だけを返し、ほかの続きの
// 行がレコードに見えるかは見ない（同じ行がキーの形や区切りの数に当たっても、
// 理由は [SignTextAfterQuote] にする）。閉じ引用符の後ろに文字を書く書き手は無いので、
// そのレコードは正当な複数行の値ではありえない。単独で読むとレコードに見える理由は
// 正当な値でも当たるので、呼び出し側は確かめたうえで通せる（publish の
// --accept-multiline）。その理由で返すと、通してはならない飲み込みが通せる形になり、
// 英語の原文やキーが訳として公開される。飲み込まれた行は、値を開いた行から閉じ引用符の
// 行までのあいだにあり、どちらもレコードの範囲（Line〜EndLine）に入るので、どこを
// 直すかは範囲と閉じ引用符の行で言える。
//
// 正当な複数行の値でも当たることがある（原文の2行目がカンマを多く含むなど）。
// そのため、呼び出し側は、確かめたうえで通す指定を用意すること。閉じない引用符の
// レコードは見ない。そちらは [UnclosedQuoteError] が先に止める。
func FindSwallows(segs Segments) []Swallow {
	header, ok := segs.Header()
	if !ok {
		return nil
	}
	columns := len(header.Offsets)
	var out []Swallow
	for _, seg := range segs.List {
		if !seg.HasFields() || !seg.MultiLine() || seg.Unclosed() {
			continue
		}
		tails := textAfterQuoteLines(segs, seg)
		for n := seg.Line + 1; n <= seg.EndLine; n++ {
			sign, ok := RecordSign(0), false
			switch line, _ := segs.PhysicalLine(n); {
			case tails[n]:
				sign, ok = SignTextAfterQuote, true
			case len(tails) > 0:
				// 閉じ引用符の後ろに文字が続く行があるレコード。この行は返さない。
			case strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "#"):
				sign, ok = looksLikeRecord(line, columns)
			}
			if ok {
				out = append(out, Swallow{ID: seg.ID, Line: seg.Line, EndLine: seg.EndLine, SwallowedLine: n, Sign: sign})
			}
		}
	}
	return out
}

// CRCut は、引用の外の単独の CR で値が切れたと見られるところ1つ。
type CRCut struct {
	// ID・Line・EndLine は、単独の CR で終わるレコード（切れた値の前半を持つ）の
	// セグメントの ID と物理行の範囲。
	ID, Line, EndLine int
	// NextLine は、単独の CR の次の物理行（切れた値の後半）。
	NextLine int
}

// FindCRCuts は、引用符で囲まない値の中の単独の CR で、値が切れたと見られる
// ところを返す（PR0 の lone-cr-unquoted-value。決まったことの 11）。
//
// 読み方の規則では、引用の外の単独の CR はレコードの区切りである（上流と意図して
// 違える点）。そのため `い\rち` のような値は「い」で切れ、「ち」は別のレコードに
// なる。ゲームの CsvReader は引用の外の CR を捨てて「いち」と読むので、publish が
// そのまま書くと訳を失う。
//
// 見分け方はファイルで分ける（決まったことの 14）。
//
//   - 行の区切り（引用の外の終端）がすべて単独の CR のファイルには当てない。
//     CR だけで改行したファイルは読むと決めてある（上流と意図して違える点）。
//     そうしたファイルでは、どの単独の CR も行の区切りで、値の中の CR と見分ける
//     手がかりが無い。見出しのコメント行・空行・空のレコード・列の足りない
//     レコードの前でも単独の CR で改行するので、当てると正当なファイルが止まる。
//   - それ以外のファイル（LF や CRLF で改行した行が1つでもあるもの）では、単独の
//     CR で終わるレコードのうち、次のセグメント（区切りの関数が分けたとおりのもの）が
//     レコードに見えないものを返す。次が空行（空白だけの行を含む）・'#' で始まる行・
//     空のレコード（"," や `""`）でも返す。値が CR の直後の '#' で切れる形
//     （`い\r# ち`）を見逃さないためである。
//
// 次のセグメントがレコードに見えれば、行末の単独の CR で改行しただけと見て返さない。
// レコードに見えるのは、最初の値がキーの形か、区切りの数（Offsets の個数）が
// ヘッダーの列数と同じときである（[FindSwallows] と同じ見方）。区切りはレコード全体で
// 数える。次の物理行だけを単独で読むと、キー列の空いた行の原文が行をまたぐとき、
// 引用が開いたまま行が終わって区切りが足りず、正当な行末の CR で当たってしまう。
func FindCRCuts(segs Segments) []CRCut {
	header, ok := segs.Header()
	if !ok || crOnlyLineBreaks(segs) {
		return nil
	}
	columns := len(header.Offsets)
	var out []CRCut
	for i, seg := range segs.List {
		if !seg.HasFields() || seg.Term != TermCR || i+1 >= len(segs.List) {
			continue
		}
		next := segs.List[i+1]
		if next.Kind == SegmentRecord && (keyShaped(next.Fields[0]) || len(next.Offsets) == columns) {
			continue
		}
		out = append(out, CRCut{ID: seg.ID, Line: seg.Line, EndLine: seg.EndLine, NextLine: next.Line})
	}
	return out
}

// crOnlyLineBreaks は、引用の外の行の区切りがすべて単独の CR かを返す。行の区切りが
// 1つも無いファイル（1行だけで改行の無いもの）は false。
//
// 見るのはセグメントの終端（引用の外の改行）だけで、引用の中の改行は見ない。
// 引用した値の中の LF は、行の区切りではなく値の一部だからである。
func crOnlyLineBreaks(segs Segments) bool {
	seen := false
	for _, seg := range segs.List {
		switch seg.Term {
		case TermCR:
			seen = true
		case TermLF, TermCRLF:
			return false
		}
	}
	return seen
}

// ValueSpot は、レコードの値1つの場所。
type ValueSpot struct {
	// ID・Line・EndLine は、その値を持つレコードの ID と物理行の範囲。
	ID, Line, EndLine int
	// Column は列名。
	Column string
}

// LoneCRValues は、単独の CR（後ろに LF の続かない CR）を含む値を返す。
//
// 引用符で囲んだ値の中の単独の CR は、読み手は値に残す。上流の hash-strings.ps1 は、
// そのレコードをあとで落とす（Remove-NonRecords が単独の CR を行末と見なさない）。
// 公開ファイルに残すと、上流の道具を通したときに訳が消えるので、publish は止めて
// LF に直すよう案内する（決まったことのそのほか 1）。
func LoneCRValues(f PowerShellFile) []ValueSpot {
	var out []ValueSpot
	for _, r := range f.Records {
		for _, col := range r.Columns() {
			if hasLoneCR(r.Get(col)) {
				out = append(out, ValueSpot{ID: r.ID, Line: r.Line, EndLine: r.EndLine, Column: col})
			}
		}
	}
	return out
}

// hasLoneCR は、後ろに LF の続かない CR があるかを返す。
func hasLoneCR(v string) bool {
	for i := 0; i < len(v); i++ {
		if v[i] == '\r' && (i+1 >= len(v) || v[i+1] != '\n') {
			return true
		}
	}
	return false
}

// LineBreakValues は、columns の列の値のうち CR か LF を含むものを返す。
// columns を渡さなければ、すべての列を見る。
//
// 再生順（data/script_order.csv）と見出しの表（data/level_flow.csv）の値は、
// publish が見出しのコメント行（'# ===== … =====' と '# --- … ---'）へ、
// 引用せずにそのまま書く（上流と同じ）。全体を解釈して読むと値に改行が入りうるので、
// そのまま書くと見出しの2行目が '#' で始まらない行になり、次に読むときデータの
// 行として読まれる。見出しに使う値に改行があれば、publish は止める
// （決まったことのそのほか 8）。
func LineBreakValues(f PowerShellFile, columns ...string) []ValueSpot {
	var out []ValueSpot
	for _, r := range f.Records {
		names := columns
		if len(names) == 0 {
			names = r.Columns()
		}
		for _, col := range names {
			if v, ok := r.Lookup(col); ok && strings.ContainsAny(v, "\r\n") {
				out = append(out, ValueSpot{ID: r.ID, Line: r.Line, EndLine: r.EndLine, Column: col})
			}
		}
	}
	return out
}

// Disagreement は、主の読み手とゲームの読み方（C# の移植）で値が食い違う
// レコード1つ。
type Disagreement struct {
	// ID・Line・EndLine は、主の読み手のレコードの ID と物理行の範囲。
	ID, Line, EndLine int
	// Column は食い違った最初の列。ゲームの読み方にそのレコードが見つからなければ空。
	Column string
}

// CSharpDisagreements は、主の読み手が読んだレコードのうち、ゲームの読み方
// （[ReadCSharpRows]）で読むと値が違うものを返す。
//
// 2つの読み方は、たいていのファイルで同じ値を返す（agree_test と実物の作業コピー）。
// 割れるのは、フィールドの途中の '"'（ゲームはそこから引用を始める。移植仕様
// 「CSVとキー生成 R6」）、引用符で囲まない値の前後の空白（ConvertFrom-Csv は削り、
// ゲームは削らない）、引用の外の単独の CR（ゲームは捨てる）などである。そうした
// レコードを画面から保存すると、翻訳者が見ている値とゲームが表示する値が食い違う。
// 保存はこのレコードを編集させない（PR3）。
//
// レコードは、key 列の値（前後の空白を除く）か、key が空なら source_en の値で
// 突き合わせる。同じ値のレコードが複数あれば、出現順に組にする。どちらも空の
// レコードは突き合わせられないので見ない。比べるのは、主の読み手のヘッダーの
// すべての列である。
func CSharpDisagreements(f PowerShellFile) []Disagreement {
	game := make(map[string][]Row)
	for _, r := range ReadCSharpRows([]byte(f.Segments.Text)) {
		if id, ok := recordIdentity(r); ok {
			game[id] = append(game[id], r)
		}
	}
	seen := make(map[string]int)
	var out []Disagreement
	for _, r := range f.Records {
		id, ok := recordIdentity(r.Row)
		if !ok {
			continue
		}
		n := seen[id]
		seen[id]++
		d := Disagreement{ID: r.ID, Line: r.Line, EndLine: r.EndLine}
		if n >= len(game[id]) {
			out = append(out, d)
			continue
		}
		g := game[id][n]
		for _, col := range r.Columns() {
			if v, ok := g.Lookup(col); !ok || v != r.Get(col) {
				d.Column = col
				out = append(out, d)
				break
			}
		}
	}
	return out
}

// recordIdentity は、2つの読み方のレコードを突き合わせる鍵を返す。
func recordIdentity(r Row) (string, bool) {
	if k := strings.TrimSpace(r.Get("key")); k != "" {
		return "key:" + k, true
	}
	if s := r.Get("source_en"); s != "" {
		return "source_en:" + s, true
	}
	return "", false
}
