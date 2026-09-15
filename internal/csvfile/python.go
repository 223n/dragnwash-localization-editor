package csvfile

import (
	"strings"
	"unicode"
)

// Line は物理行。
type Line struct {
	// Number は1始まりの物理行番号。検証メッセージの行番号はこれを使う。
	Number int
	// Text は行末の改行文字を含む生の行。Python の newline="" と同じで、
	// "\r\n" は "\r\n" のまま残る。
	Text string
}

// SplitPythonLines は BOM を剥がしたうえで、Python の io.open(newline="") と同じ
// 規則で物理行に分ける。
//
// 終端は "\r\n" / "\n" / "\r" の3種で、終端文字は行に含めたまま返す。
// 終端を残すのは、引用フィールド内に埋め込まれた改行を壊さないため
// （移植仕様「形式検証 R8」）。CR単独を終端として認めるのも元実装に合わせている。
// bufio.Scanner の ScanLines は "\r" を落とすので使えない。
func SplitPythonLines(data []byte) []Line {
	s := TrimBOMString(string(data))

	var lines []Line
	start := 0
	number := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\n' && c != '\r' {
			continue
		}
		end := i + 1
		if c == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			end = i + 2
			i++
		}
		number++
		lines = append(lines, Line{Number: number, Text: s[start:end]})
		start = end
	}
	if start < len(s) {
		number++
		lines = append(lines, Line{Number: number, Text: s[start:]})
	}
	return lines
}

// KeepContentLines は「空白だけの行」と「'#' で始まる行」を落とす。
//
// 元実装（移植仕様「形式検証 R9」）:
//
//	kept = [(i, line) for i, line in enumerate(f, start=1)
//	        if line.strip() and not line.startswith("#")]
//
// '#' の判定は生の先頭1文字の前方一致なので、先頭に空白がある " # x" は残る。
// この除去はCSVの構文を見ずに物理行単位で行われるため、引用フィールドが複数行に
// またがっていて途中の行が空行や '#' 始まりだと、CSVの構造ごと壊れる。それも
// 含めて元実装の挙動である。
//
// 空判定は Python の str.strip() に合わせる（[IsPythonBlank] 参照）。
// この判定が残る行を決め、残った行の並びがそのまま報告用行番号の対応表になるので、
// 判定を1文字ぶん間違えると以降の全メッセージの行番号がずれる。
func KeepContentLines(lines []Line) []Line {
	kept := make([]Line, 0, len(lines))
	for _, line := range lines {
		if IsPythonBlank(line.Text) {
			continue
		}
		if strings.HasPrefix(line.Text, "#") {
			continue
		}
		kept = append(kept, line)
	}
	return kept
}

// ReadPythonLines は [SplitPythonLines] と [KeepContentLines] をまとめたもので、
// 形式検証の対象になる行だけを物理行番号つきで返す。
func ReadPythonLines(data []byte) []Line {
	return KeepContentLines(SplitPythonLines(data))
}

// IsPythonSpace は Python の str.isspace() と同じ空白判定を行う。
//
// Go の unicode.IsSpace は U+001C〜U+001F を空白に含めないが、Python は含める。
// 実測で Python の '\x1c'.isspace() は True。この差は「その行が残るか」を変え、
// 残る行の並びが報告用行番号を決めるため、行番号のずれとして表面化する
// （移植仕様「形式検証 / 敵対検証」[medium]）。U+0085 と U+00A0 は両者一致する。
func IsPythonSpace(r rune) bool {
	if r >= 0x1C && r <= 0x1F {
		return true
	}
	return unicode.IsSpace(r)
}

// IsPythonBlank は Python の `not s.strip()` と同じ判定を返す。空文字は true。
//
// 「その行が残るか」と「訳が空か」の両方をこの判定が決める。internal/validate も
// 同じ関数を使うこと。別々に持つと、片方で落ちて片方で残る文字が生まれ、
// 報告される行番号がずれる。
func IsPythonBlank(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return !IsPythonSpace(r) }) < 0
}

// PythonRecord は [ParsePythonRecords] が返す1レコード。
type PythonRecord struct {
	// Fields はフィールドの並び。列数の検査はしないので、行ごとに長さは変わりうる。
	Fields []string
	// Number は報告用の物理行番号。レコードが複数行にまたがる場合は先頭行を指す。
	Number int
	// Index は入力スライス内での先頭行の添字。
	Index int
	// Consumed はこのレコードを読み終えた時点で消費した行数
	// （Python の reader.line_num に対応する）。
	Consumed int
}

// ParsePythonRecords は Python の csv.reader（既定の dialect、strict=False）と
// 同じ状態機械で、与えられた行をレコードへ分ける。
//
// 入力は [KeepContentLines] を通した行を想定する。元実装は行番号を持たない行テキスト
// だけを csv.reader に渡し、行番号は別に持った origin 配列で復元している
// （移植仕様「形式検証 R10 / R15」）。ここでは [Line] が行番号を持っているので、
// 同じ対応付けを [PythonRecord.Number] として返す。対応付けの規則は元実装と同じで、
// 「そのレコードを読み始める直前までに消費した行数」を添字にする。
//
// 元実装との差として意図的に再現しないもの:
//
//   - csv.field_size_limit()（既定131072文字）超過の _csv.Error。
//     Go 側は制限を設けない（元実装より寛容になる）。
//   - 行に NUL が含まれるときの _csv.Error。同じく制限しない。
//   - 不正な UTF-8 での UnicodeDecodeError。ここではバイト列をそのまま値に入れる。
//
// 逆に、次の癖は再現する。引用符で囲まないフィールド中の裸の二重引用符は
// エラーにせずそのまま値に入れる（Go の encoding/csv は LazyQuotes を立てないと
// ここでパースを打ち切り、以降の全行が検査されなくなる）。閉じ引用符の直後に
// 文字が続く `"x"y"` は xy" になる。空行は0フィールドのレコードになる。
func ParsePythonRecords(lines []Line) []PythonRecord {
	var out []PythonRecord
	p := &pythonParser{}
	consumed := 0

	for consumed < len(lines) {
		start := consumed
		complete := false
		for consumed < len(lines) {
			text := lines[consumed].Text
			consumed++
			// バイト単位で走査する。分岐対象がすべて ASCII で、UTF-8 の多バイト文字は
			// default 節でそのまま積まれるため、Python の文字単位の走査と一致する。
			for i := 0; i < len(text); i++ {
				p.process(int(text[i]))
			}
			p.process(pyEOL)
			if p.state == pyStartRecord {
				complete = true
				break
			}
		}
		if !complete {
			// 入力が尽きた。元実装は「フィールドに文字が残っている」か「引用が
			// 閉じていない」ときだけ、そこまでをレコードとして返す。
			if p.field.Len() == 0 && p.state != pyInQuotedField {
				break
			}
			p.saveField()
		}
		out = append(out, PythonRecord{
			Fields:   p.takeFields(),
			Number:   lines[start].Number,
			Index:    start,
			Consumed: consumed,
		})
		p.state = pyStartRecord
	}
	return out
}

// pyEOL は行の終わりを表す擬似文字。Python の parse_process_char が行末に受け取る
// 値に対応する。実在のバイトと衝突しないよう負値にしている。
const pyEOL = -1

type pythonState int

const (
	pyStartRecord pythonState = iota
	pyStartField
	pyInField
	pyInQuotedField
	pyQuoteInQuotedField
	pyEatCRNL
)

// pythonParser は CPython の _csv.c の状態機械をそのまま写したもの。
// 既定の dialect（delimiter=','、quotechar='"'、doublequote=True、
// escapechar=None、skipinitialspace=False、strict=False）だけを扱う。
type pythonParser struct {
	state  pythonState
	field  strings.Builder
	fields []string
}

func (p *pythonParser) saveField() {
	p.fields = append(p.fields, p.field.String())
	p.field.Reset()
}

func (p *pythonParser) takeFields() []string {
	fields := p.fields
	p.fields = nil
	if fields == nil {
		// 空行は「0フィールドのレコード」。nil ではなく空スライスで返して、
		// レコードが無いこととの取り違えを防ぐ。
		fields = []string{}
	}
	return fields
}

func (p *pythonParser) process(c int) {
	switch p.state {
	case pyStartRecord:
		if c == pyEOL {
			// 空行。0フィールドのレコードとして確定する。
			return
		}
		if c == '\n' || c == '\r' {
			p.state = pyEatCRNL
			return
		}
		p.state = pyStartField
		fallthrough

	case pyStartField:
		switch {
		case c == pyEOL || c == '\n' || c == '\r':
			p.saveField()
			p.state = pyEndOfLineState(c)
		case c == '"':
			p.state = pyInQuotedField
		case c == ',':
			p.saveField()
		default:
			p.field.WriteByte(byte(c))
			p.state = pyInField
		}

	case pyInField:
		switch {
		case c == pyEOL || c == '\n' || c == '\r':
			p.saveField()
			p.state = pyEndOfLineState(c)
		case c == ',':
			p.saveField()
			p.state = pyStartField
		default:
			p.field.WriteByte(byte(c))
		}

	case pyInQuotedField:
		switch {
		case c == pyEOL:
			// 何もしない。行の改行文字そのものは上の走査で値に入っている。
			// これにより引用フィールド内の改行が保持され、CRLF も "\r\n" のまま残る。
		case c == '"':
			p.state = pyQuoteInQuotedField
		default:
			p.field.WriteByte(byte(c))
		}

	case pyQuoteInQuotedField:
		switch {
		case c == '"':
			// "" はリテラルの " 1個。
			p.field.WriteByte('"')
			p.state = pyInQuotedField
		case c == ',':
			p.saveField()
			p.state = pyStartField
		case c == pyEOL || c == '\n' || c == '\r':
			p.saveField()
			p.state = pyEndOfLineState(c)
		default:
			// strict=False なので閉じ引用符の直後の文字はそのまま値に入る。
			// `"x"y"` が xy" になるのはこの分岐。
			p.field.WriteByte(byte(c))
			p.state = pyInField
		}

	case pyEatCRNL:
		switch {
		case c == '\n' || c == '\r':
			// 何もしない。
		case c == pyEOL:
			p.state = pyStartRecord
		default:
			// 元実装はここで _csv.Error（"new-line character seen in unquoted field"）。
			// 行分割を元実装と同じにしているかぎり、改行の後ろに文字が続くことは
			// 無いので到達しない。到達しても例外にはせず読み飛ばす。
		}
	}
}

// pyEndOfLineState は行末に達したあとの状態を返す。擬似文字なら1レコード確定、
// 実在の改行文字なら残りの改行を読み飛ばす状態へ移る。
func pyEndOfLineState(c int) pythonState {
	if c == pyEOL {
		return pyStartRecord
	}
	return pyEatCRNL
}
