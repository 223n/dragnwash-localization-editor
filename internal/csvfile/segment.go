package csvfile

import (
	"sort"
	"strings"
)

// SegmentKind はセグメントの種類。
type SegmentKind int

const (
	// SegmentBlank は、レコードの境目にある空行か空白だけの物理行。
	SegmentBlank SegmentKind = iota + 1
	// SegmentComment は、レコードの境目にある '#' で始まる物理行。
	SegmentComment
	// SegmentHeader は、最初のレコード。中身が "," のように空でもヘッダーになる。
	SegmentHeader
	// SegmentRecord は、ヘッダーより後ろのデータのレコード。
	SegmentRecord
	// SegmentEmpty は、ヘッダーより後ろのレコードのうち、読むとフィールドが0個か
	// 空の1個になるもの（"," や `""` の行）。データとしては落とす。
	SegmentEmpty
)

// String は種類の名前を返す。試験の出力と、呼び出し側の理由の文に使う。
func (k SegmentKind) String() string {
	switch k {
	case SegmentBlank:
		return "blank"
	case SegmentComment:
		return "comment"
	case SegmentHeader:
		return "header"
	case SegmentRecord:
		return "record"
	case SegmentEmpty:
		return "empty"
	}
	return "unknown"
}

// Terminator はセグメントを終える改行。本体の後ろに付いていたものをそのまま持つ。
type Terminator string

const (
	// TermNone は改行が無いこと。ファイルの最後のセグメントだけがなりうる。
	TermNone Terminator = ""
	// TermLF は "\n"。
	TermLF Terminator = "\n"
	// TermCRLF は "\r\n"。
	TermCRLF Terminator = "\r\n"
	// TermCR は単独の "\r"。
	TermCR Terminator = "\r"
)

// Segment はファイルの一続きの部分1つ。コメント行・空行は1物理行で1つ、
// レコードは引用符で囲んだ値が行をまたげば複数の物理行で1つになる。
type Segment struct {
	// ID は1始まりの通し番号。[Segments] の List[i].ID は i+1 である。0 は
	// 「セグメントが無い」を表すために空けてある。
	//
	// 同じバイト列からは必ず同じ ID が付く。行番号と違い、ほかのレコードの値に
	// 改行が増えても変わらないので、行の同定にはこちらを使う。
	ID int
	// Kind はセグメントの種類。
	Kind SegmentKind
	// Line と EndLine は、セグメントが占める物理行の範囲（1始まり、両端を含む）。
	// 物理行は "\r\n" / "\n" / "\r" で分け、BOM は数えない。エディターの行番号と同じ。
	// 表示と報告の照合に使う値で、同定には使わない。
	Line, EndLine int
	// Start と End は本体のバイト範囲 [Start, End)。位置はファイルの先頭から数え、
	// BOM の長さも含める。本体に終端は入らない。
	Start, End int
	// Term は本体の後ろの終端。次のセグメントは End+len(Term) から始まる。
	Term Terminator
	// Fields は ConvertFrom-Csv の規則で読んだ値。ヘッダー・レコード・空のレコードの
	// ときだけ入る。行末の「値が空、かつ引用符で閉じられていない」フィールドは
	// 落とす（[ParsePowerShellRecord] と同じ契約）。
	Fields []string
	// Offsets はフィールドの開始位置（ファイルの先頭から）。Fields と同じく
	// ヘッダー・レコード・空のレコードのときだけ入る。個数は引用の外のカンマの数+1 で、
	// 末尾の空フィールドも1つと数える（[FieldOffsets] と同じ契約）。そのため
	// Fields より長いことがある。最終フィールドを差し替える保存が使う。
	Offsets []int
	// OpenLine は、このセグメントの中で開いてファイルの終わりまで閉じなかった
	// 引用符の物理行。閉じていれば 0。
	OpenLine int
}

// Unclosed は、このセグメントの中で開いた引用符がファイルの終わりまで閉じなかった
// かを返す。そうしたセグメントは必ずファイルの最後のセグメントになる。
func (s Segment) Unclosed() bool { return s.OpenLine > 0 }

// MultiLine は、セグメントが2物理行以上にまたがるかを返す。
func (s Segment) MultiLine() bool { return s.EndLine > s.Line }

// HasFields は、フィールドを持つ種類（ヘッダー・レコード・空のレコード）かを返す。
func (s Segment) HasFields() bool {
	return s.Kind == SegmentHeader || s.Kind == SegmentRecord || s.Kind == SegmentEmpty
}

// Segments は [SplitSegments] が分けた結果。
type Segments struct {
	// Text は読んだバイト列そのもの（BOM を含む）。
	Text string
	// BOM は先頭の UTF-8 BOM の長さ。0 か 3。
	BOM int
	// List はセグメントの並び。BOM の直後からファイルの終わりまで、隙間も重なりも
	// 無く並ぶ。
	List []Segment
	// Lines は物理行の数。末尾の改行の後ろに空の行は数えない（[SplitNetLines] と同じ）。
	Lines int
	// UnclosedLine は、閉じないままファイルの終わりまで続いた引用符が開いた物理行。
	// そうした引用符が無ければ 0。あれば最後のセグメントの OpenLine と同じ値になる。
	UnclosedLine int

	// lineStarts は各物理行の先頭の位置（ファイルの先頭から）。
	lineStarts []int
}

// SplitSegments は、ファイル全体を1つの文字列として、コメント行・空行・レコードの
// 並びに分ける。どのバイト列でも誤りにせず、必ず結果を返す。
//
// 読み方の規則は、この関数だけが持つ。主の読み手（[ReadPowerShell]）、PR1 までの
// 守り専用の [ReadPowerShellWhole]、飲み込みの検出（[FindSwallows]）は、どれもこの
// 結果の上に載っている。保存（internal/edit）も、ここで分けたレコードの最終フィールドを
// 差し替える。書く側と読む側で区切りが割れると、訳ではない値を
// 書き換えてしまうので、規則を2か所に持たない。
//
// 上流の hash-strings.ps1（f816618 と c8fda90）は、ファイル全体を1つの文字列にして
// Remove-NonRecords で引用の外の空行・空白だけの行・'#' で始まる行を落とし、
// ConvertFrom-Csv へ渡す。ここも同じ行を落とすが、次の点を上流と違える。
//
//   - 物理行の区切りは "\r\n" / "\n" / "\r" の3つ（[SplitNetLines] と同じ）。上流の
//     Remove-NonRecords は正規表現 `[^\r\n]*(?:\r?\n|$)` で行を分けるので、単独の
//     "\r" を行末と見なさず、その手前の文字列を捨てる。CR だけで改行したファイルでは
//     訳がすべて消える。上流の不具合として扱い、写さない。
//   - 物理行が引用の外かどうかを、行ごとの '"' の数の偶奇ではなく、フィールドの
//     先頭（先頭の空白の後ろを含む）で開いた引用だけで決める（ConvertFrom-Csv と
//     同じ規則）。上流の Remove-NonRecords は偶奇で決めるので、引用符なしの
//     フィールドの途中に裸の '"' がある行（5" screen のような値）の後ろのコメント行を
//     レコードとして残し、列が多ければ公開ファイルに書く。これも写さない。
//   - コメント行の中の '"' は数えない。コメントはレコードを開かない（ゲームの
//     CsvReader もコメントの中を見ない）。
//   - 行頭の '#' は序数で比べる。上流の StartsWith('#') はカルチャに依存する照合で、
//     U+00AD のように照合上無視される文字を飛ばすが、Go の標準ライブラリだけでは
//     照合表を持てない。
//
// 空行の判定は strings.TrimSpace で、.NET の Trim（Char.IsWhiteSpace）と同じ集合に
// なる。全角空白や NO-BREAK SPACE だけの行も空行として落ちる。
//
// 閉じない引用符は、ファイルの終わりまでを1つの値にし、開いた行を OpenLine と
// UnclosedLine に残す。ここでは誤りにしない。誤りにするのは主の読み手である。
func SplitSegments(data []byte) Segments {
	text := string(data)
	s := Segments{Text: text}
	if strings.HasPrefix(text, utf8BOM) {
		s.BOM = len(utf8BOM)
	}
	s.lineStarts = lineStartsFrom(text, s.BOM)
	s.Lines = len(s.lineStarts)

	haveHeader := false
	for pos := s.BOM; pos < len(text); {
		// ここは必ずレコードの境目（引用の外）で、物理行の先頭である。
		seg := Segment{ID: len(s.List) + 1, Start: pos, Line: s.lineAt(pos)}
		body, next := physicalLine(text, pos)
		switch {
		case strings.TrimSpace(body) == "":
			seg.Kind = SegmentBlank
			seg.End = pos + len(body)
		case strings.HasPrefix(body, "#"):
			seg.Kind = SegmentComment
			seg.End = pos + len(body)
		default:
			fields, offsets, end, openAt := parseRecordAt(text, pos)
			seg.Fields, seg.Offsets, seg.End = fields, offsets, end
			if openAt >= 0 {
				seg.OpenLine = s.lineAt(openAt)
				s.UnclosedLine = seg.OpenLine
			}
			switch {
			case !haveHeader:
				// "," や `""` の行もヘッダーになる。上流でも Trim で空にならないので
				// ヘッダーの候補から外れない。
				seg.Kind = SegmentHeader
				haveHeader = true
			case len(fields) == 0 || (len(fields) == 1 && fields[0] == ""):
				seg.Kind = SegmentEmpty
			default:
				seg.Kind = SegmentRecord
			}
			next = afterLineBreak(text, end)
		}
		seg.EndLine = s.lineAt(seg.End)
		seg.Term = Terminator(text[seg.End:next])
		s.List = append(s.List, seg)
		pos = next
	}
	return s
}

// Body はセグメントの本体（終端を含まない）を返す。
func (s Segments) Body(seg Segment) string { return s.Text[seg.Start:seg.End] }

// Header はヘッダーのセグメントを返す。レコードが1つも無ければ false。
func (s Segments) Header() (Segment, bool) {
	for _, seg := range s.List {
		if seg.Kind == SegmentHeader {
			return seg, true
		}
	}
	return Segment{}, false
}

// PhysicalLine は n 行目（1始まり）の物理行の本体と終端を返す。範囲の外なら
// 空と TermNone を返す。
//
// 行をまたぐレコードの続きの行を単独で読むとき（[FindSwallows]）や、閉じない
// 引用符より後ろを物理行のまま並べるときに使う。
func (s Segments) PhysicalLine(n int) (string, Terminator) {
	if n < 1 || n > len(s.lineStarts) {
		return "", TermNone
	}
	start := s.lineStarts[n-1]
	body, next := physicalLine(s.Text, start)
	return body, Terminator(s.Text[start+len(body) : next])
}

// lineAt は位置 p を含む物理行の番号（1始まり）を返す。p がファイルの終わりなら
// 最後の行を返す。行が無ければ 0。
func (s Segments) lineAt(p int) int {
	if p >= len(s.Text) {
		p = len(s.Text) - 1
	}
	return sort.Search(len(s.lineStarts), func(i int) bool { return s.lineStarts[i] > p })
}

// parseRecordAt は s の start から1レコードを読む。start はレコードの境目で、
// 物理行の先頭でなければならない。
//
// 返すのは、値（行末の空フィールドの捨て方は [parsePowerShellFields] と同じ）、
// フィールドの開始位置（末尾の空フィールドも数える。[FieldOffsets] と同じ）、
// レコードの終わりの位置（レコードを終える改行の位置か、文字列の長さ）、
// 閉じなかった引用符の位置（無ければ -1）である。
//
// フィールドは [parsePowerShellField] の wholeText で読むので、引用の外の改行が
// レコードを終え、引用の中の改行は値に入る。レコードの本体には引用の外の改行が
// 無いので、本体に [FieldOffsets] を掛けても同じ位置が出る（試験で確かめてある）。
func parseRecordAt(s string, start int) (fields []string, offsets []int, end, openAt int) {
	openAt = -1
	i := start
	for {
		offsets = append(offsets, i)
		f := parsePowerShellField(s, i, true)
		if f.unclosed() {
			// 閉じない引用符は文字列の終わりまでを読んでいる。その引用符の位置を返す。
			openAt = strings.IndexByte(s[i:], '"') + i
		}
		if f.end >= len(s) || s[f.end] != ',' {
			if f.value != "" || f.closed {
				fields = append(fields, f.value)
			}
			return fields, offsets, f.end, openAt
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

// lineStartsFrom は、位置 from から後ろの各物理行の先頭の位置を並べて返す。
// 行の分け方は [SplitNetLines] と同じで、末尾の改行の後ろに空の行は数えない。
func lineStartsFrom(s string, from int) []int {
	if from >= len(s) {
		return nil
	}
	starts := []int{from}
	for p := from; ; {
		_, next := physicalLine(s, p)
		if next >= len(s) {
			return starts
		}
		starts = append(starts, next)
		p = next
	}
}
