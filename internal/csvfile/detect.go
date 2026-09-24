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

// RecordSign は、物理行を単独で読むとレコードに見える理由。
type RecordSign int

const (
	// SignKeyShaped は、最初の値がキーの形（16桁の16進か台詞ID）であること。
	SignKeyShaped RecordSign = iota + 1
	// SignSameColumns は、区切りの数（引用の外のカンマの数+1）がヘッダーの列数と
	// 同じであること。
	SignSameColumns
)

// String は理由の名前を返す。試験の出力と、呼び出し側の理由の文に使う。
func (s RecordSign) String() string {
	switch s {
	case SignKeyShaped:
		return "key-shaped"
	case SignSameColumns:
		return "same-columns"
	}
	return "unknown"
}

// looksLikeRecord は、1物理行を単独で読むとレコードに見えるかを返す。columns は
// ヘッダーの区切りの数（ヘッダーのセグメントの Offsets の個数）。
//
// キーの形は、publish がキーを決めるときと同じ扱いで見る。前後の空白を除き
// （移植仕様 R11）、台詞ID はそのまま、それ以外は小文字にしてから16桁の16進かを
// 見る（R13、R16）。
func looksLikeRecord(line string, columns int) (RecordSign, bool) {
	first := strings.TrimSpace(parsePowerShellField(line, 0, false).value)
	if key.LooksLikeLineID(first) || key.LooksLike(strings.ToLower(first)) {
		return SignKeyShaped, true
	}
	if len(FieldOffsets(line)) == columns {
		return SignSameColumns, true
	}
	return 0, false
}

// Swallow は、行をまたぐレコードが飲み込んだと見られる物理行1つ。
type Swallow struct {
	// ID・Line・EndLine は、飲み込んだレコード（ヘッダーのこともある）の
	// セグメントの ID と物理行の範囲。
	ID, Line, EndLine int
	// SwallowedLine は、単独で読むとレコードに見える続きの物理行。
	SwallowedLine int
	// Sign はレコードに見える理由。
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
		for n := seg.Line + 1; n <= seg.EndLine; n++ {
			line, _ := segs.PhysicalLine(n)
			if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if sign, ok := looksLikeRecord(line, columns); ok {
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
// 返すのは、単独の CR で終わるレコードのうち、次の物理行が単独ではレコードに
// 見えないもの（[FindSwallows] と同じ見方）である。次の行がレコードに見えれば、
// 行末の単独の CR で改行しただけ（CR だけの改行のファイルなど）と見て返さない。
// 次の行が空行・空白だけの行・'#' で始まる行のときも返さない。CR だけの改行の
// ファイルは、見出しのコメント行や空行の前でも単独の CR で改行するので、そこで
// 止めると、意図して読んでいる CR だけのファイルが通らなくなる。代わりに、
// 値が CR の直後の '#' で切れる形は見逃す。
func FindCRCuts(segs Segments) []CRCut {
	header, ok := segs.Header()
	if !ok {
		return nil
	}
	columns := len(header.Offsets)
	var out []CRCut
	for _, seg := range segs.List {
		if !seg.HasFields() || seg.Term != TermCR {
			continue
		}
		next := seg.EndLine + 1
		line, _ := segs.PhysicalLine(next)
		if next > segs.Lines || strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, ok := looksLikeRecord(line, columns); ok {
			continue
		}
		out = append(out, CRCut{ID: seg.ID, Line: seg.Line, EndLine: seg.EndLine, NextLine: next})
	}
	return out
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
