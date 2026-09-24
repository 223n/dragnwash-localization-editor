package csvfile

import (
	"sort"
	"strings"
)

// WholeRecord は [ReadPowerShellWhole] が読んだ1レコード。
type WholeRecord struct {
	// Row はヘッダーと対応づけた値。ヘッダーそのもののレコードでは空。
	Row
	// Line はレコードの先頭の1始まりの物理行番号。
	Line int
	// EndLine はレコードの最後の物理行番号。引用符で囲んだ値が行をまたぐと
	// Line より大きくなる。
	EndLine int
	// Unclosed は、このレコードの中で開いた引用符が、ファイルの終わりまで
	// 閉じなかったかどうか。
	Unclosed bool
}

// MultiLine は、レコードが2物理行以上にまたがっているかを返す。
func (r WholeRecord) MultiLine() bool { return r.EndLine > r.Line }

// PowerShellWhole は [ReadPowerShellWhole] が読んだ結果。
type PowerShellWhole struct {
	// Header はヘッダーのレコード。Fields に列名が入る。レコードが1つも無ければ
	// Line が 0 になる。
	Header WholeHeader
	// Rows はデータのレコード。空行相当のレコードは入らない。
	Rows []WholeRecord
	// UnclosedLine は、閉じないままファイルの終わりまで続いた引用符が開いた
	// 物理行の番号。そうした引用符が無ければ 0。
	UnclosedLine int
}

// WholeHeader はヘッダーのレコード。
type WholeHeader struct {
	// Fields は列名。
	Fields []string
	// Line / EndLine / Unclosed は [WholeRecord] と同じ意味。
	Line     int
	EndLine  int
	Unclosed bool
}

// MultiLine は、ヘッダーが2物理行以上にまたがっているかを返す。
func (h WholeHeader) MultiLine() bool { return h.EndLine > h.Line }

// ReadPowerShellWhole は、ファイル全体を1つの文字列として読む。
//
// 上流の hash-strings.ps1 は f816618 から、Read-Csv がファイル全体を1つの文字列に
// したうえで ConvertFrom-Csv へ渡すようになった。その読み方では、引用符で囲んだ
// 値は物理行をまたいで1つの値になる。c8fda90 からは、その前に Remove-NonRecords が
// 引用の外にある空行・空白だけの行・'#' で始まる行を落とす。この関数は同じ規則で
// 読む。
//
// publish が実際に使う読み方は、これまでどおり1物理行を1レコードとする
// [ReadPowerShellRows] である。この関数は、行単位の読み方では読み違えるファイルを
// 見つけるためだけにある（internal/publish の守り）。2つの読み方の結果を
// 突き合わせ、訳が食い違うなら publish は書かずに止まる。全文の読み方へ
// 移していないのは、移すと diff・order・edit まで結果が変わるためである
// （[ReadPowerShellRows] の doc コメント）。
//
// 上流と意図して違えている点:
//
//   - 物理行の区切りは '\r\n' / '\n' / '\r' の3つで、[SplitNetLines] と同じにする。
//     上流の Remove-NonRecords は正規表現 `[^\r\n]*(?:\r?\n|$)` で行を分けるので、
//     単独の '\r' を行末と見なさず、その手前の文字列を捨てる。CR だけの改行の
//     ファイルでは訳がすべて消える（上流の不具合として扱う）。行単位の読み手と
//     同じ区切りにしておけば、ここでの「行をまたぐ」は「行単位の読み手が値を
//     切る」とちょうど重なる。
//   - "," だけの行を、全文の ConvertFrom-Csv は空の値2つのレコードとして返す
//     （pwsh 7.6.6 で実測）。ここでは行単位の読み方と同じく空行相当として
//     落とす。key も訳も持たないレコードなので、訳の突き合わせは変わらない。
//
// 列名の重複は確かめない。行単位の読み方が先に確かめている。重複したときの
// 値は [NewRow] のとおり後の列が勝つ。
func ReadPowerShellWhole(data []byte) PowerShellWhole {
	s := TrimBOMString(string(data))
	starts := lineStarts(s)
	// lineAt は s の位置 p を含む物理行の番号を返す。
	lineAt := func(p int) int {
		if p >= len(s) {
			p = len(s) - 1
		}
		return sort.Search(len(starts), func(i int) bool { return starts[i] > p })
	}

	var w PowerShellWhole
	haveHeader := false
	pos := 0
	for pos < len(s) {
		// ここは必ずレコードの境目（引用の外）である。
		body, next := physicalLine(s, pos)
		if strings.TrimSpace(body) == "" || strings.HasPrefix(body, "#") {
			// Remove-NonRecords が落とす行。コメントの中の引用符はレコードを開かない
			// （ゲームの CsvReader もコメントの中を見ない）ので、偶奇も動かさない。
			pos = next
			continue
		}

		fields, end, openAt := parseWholeRecord(s, pos)
		line, last := lineAt(pos), lineAt(end)
		unclosed := openAt >= 0
		if unclosed {
			w.UnclosedLine = lineAt(openAt)
		}
		pos = afterLineBreak(s, end)

		if !haveHeader {
			haveHeader = true
			w.Header = WholeHeader{Fields: fields, Line: line, EndLine: last, Unclosed: unclosed}
			continue
		}
		if len(fields) == 0 || (len(fields) == 1 && fields[0] == "") {
			// 空行相当。[ParsePowerShellRecord] と同じ規則で落とす。
			continue
		}
		w.Rows = append(w.Rows, WholeRecord{
			Row:  NewRow(w.Header.Fields, fields),
			Line: line, EndLine: last, Unclosed: unclosed,
		})
	}
	return w
}

// parseWholeRecord は s の start から1レコードを読む。
//
// 返すのはフィールドと、レコードの終わりの位置（レコードを終える改行の位置か、
// 文字列の長さ）と、閉じなかった引用符の位置（無ければ -1）である。
// 行末のフィールドを捨てる規則は [parsePowerShellFields] と同じ。
func parseWholeRecord(s string, start int) (fields []string, end, openAt int) {
	openAt = -1
	i := start
	for {
		fieldStart := i
		f := parsePowerShellField(s, i, true)
		if f.unclosed() {
			// 閉じない引用符は文字列の終わりまでを読んでいる。その引用符の位置を返す。
			openAt = strings.IndexByte(s[fieldStart:], '"') + fieldStart
		}
		if f.end >= len(s) || s[f.end] != ',' {
			if f.value != "" || f.closed {
				fields = append(fields, f.value)
			}
			return fields, f.end, openAt
		}
		fields = append(fields, f.value)
		i = f.end + 1
	}
}

// physicalLine は pos から始まる物理行の本体（改行を含まない）と、次の行の
// 先頭の位置を返す。区切りは [SplitNetLines] と同じ3種。
func physicalLine(s string, pos int) (body string, next int) {
	i := pos
	for i < len(s) && s[i] != '\r' && s[i] != '\n' {
		i++
	}
	return s[pos:i], afterLineBreak(s, i)
}

// afterLineBreak は、位置 p にある改行の次の位置を返す。p が文字列の終わりなら
// そのまま返す。"\r\n" は1つの改行として飛ばす。
func afterLineBreak(s string, p int) int {
	switch {
	case p >= len(s):
		return len(s)
	case s[p] == '\r' && p+1 < len(s) && s[p+1] == '\n':
		return p + 2
	default:
		return p + 1
	}
}

// lineStarts は各物理行の先頭の位置を並べて返す。行の分け方は [SplitNetLines]
// と同じで、末尾の改行の後ろに空の行は数えない。
func lineStarts(s string) []int {
	if s == "" {
		return nil
	}
	starts := []int{0}
	for p := 0; p < len(s); {
		_, next := physicalLine(s, p)
		if next >= len(s) {
			break
		}
		starts = append(starts, next)
		p = next
	}
	return starts
}
