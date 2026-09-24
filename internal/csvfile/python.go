package csvfile

import (
	"fmt"
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

// ReadPythonRecords は公開ファイル1つ分のバイト列を、上流 dev の
// tools/check-translations.py の check_file が検査に回すレコードの並びにする。
// コメントと空行はここで除かれ、残ったレコードの先頭がヘッダーになる。
//
// 上流は f816618 と c8fda90 で読み方を変えた。003ed1e までは「空白だけの行」と
// 「'#' で始まる行」を物理行のまま落としてから csv.reader に渡していた。いまは
// ファイル全体を csv.reader に通し、次の2つだけを捨てる。
//
//   - 0フィールドのレコード（完全な空行）。空白やタブだけの行は1フィールドの
//     レコードとして残り、ヘッダーより上にあればヘッダーとして扱われる。
//   - コメントの行。どの物理行がコメントかは、引用符の偶奇を行をまたいで
//     持ち回って決める（[pythonCommentLines]）。複数行にまたがる引用値の途中に
//     ある '#' 行はデータとして残る。
//
// 上流と意図して変えたところが2つある。
//
//   - コメント行はパーサーに渡さずに落とす。上流はコメント行も csv.reader に
//     通してから捨てるので、`# メモ,"開いたまま` のように引用符を開いたままの
//     コメント行があると、後ろの行がそのコメントのレコードに飲み込まれて
//     検査されなくなる。ゲーム（CsvReader.cs）はコメント行をレコードとして
//     読まないので、そちらにそろえた。ただし、引用値の途中（パーサーがまだ
//     レコードを読んでいる最中）に来た '#' 行はそのまま渡す。上流もゲームも
//     そこを値の一部として読むため。
//   - 行は "\r\n" / "\n" / "\r" だけで割る（[SplitPythonLines]）。上流の
//     comment_lines は str.splitlines() で行番号を振るので、U+2028 や \v などでも
//     行が割れ、csv.reader の行番号とずれる。ずれるとコメント行がデータとして
//     検査されたり、データ行が検査から漏れたりするので、写さない。
//
// フィールドが [PythonFieldLimit] を超えたときは [*PythonParseError] を返す。
// 上流はこれを1件の問題にして、そのファイルのほかの検査を打ち切る。
func ReadPythonRecords(data []byte) ([]PythonRecord, error) {
	lines := SplitPythonLines(data)
	records, err := parsePythonRecords(lines, pythonCommentLines(lines))
	if err != nil {
		return nil, err
	}
	kept := records[:0]
	for _, r := range records {
		// 上流の `if not row: continue`。空行は番号を消費するだけで、
		// 検査の対象にもヘッダーの候補にもならない。
		if len(r.Fields) == 0 {
			continue
		}
		kept = append(kept, r)
	}
	return kept, nil
}

// pythonCommentLines は、上流の comment_lines と同じ規則で、各物理行が
// コメントかどうかを返す。
//
// 上流（c8fda90）:
//
//	for number, line in enumerate(text.splitlines(), start=1):
//	    if not in_quotes and line[:1] == "#":
//	        found.add(number)
//	        continue
//	    if line.count('"') % 2 == 1:
//	        in_quotes = not in_quotes
//
// 偶奇は引用符で囲まないフィールド中の裸の '"' も数える。`5" 画面` のような
// 値のあとでは偶奇が反転したままになり、続く '#' 行はコメントと見なされない。
// 上流がそう読むので、ここでも同じにする（ゲームの CsvReader もフィールド途中の
// '"' から引用を始めるので、そのファイルはゲームでも後ろの行を飲み込む。
// 移植仕様「CSVとキー生成 R6」からの推定で、ゲームでは確かめていない）。
// コメント行の引用符は数えない。コメントはレコードを開かない、という前提による。
//
// '#' の判定は生の先頭1文字の前方一致なので、先頭に空白がある " # x" は
// コメントにならない。これは 003ed1e から変わっていない。
func pythonCommentLines(lines []Line) []bool {
	comments := make([]bool, len(lines))
	inQuotes := false
	for i, line := range lines {
		if !inQuotes && strings.HasPrefix(line.Text, "#") {
			comments[i] = true
			continue
		}
		if strings.Count(line.Text, `"`)%2 == 1 {
			inQuotes = !inQuotes
		}
	}
	return comments
}

// IsPythonSpace は Python の str.isspace() と同じ空白判定を行う。
//
// Go の unicode.IsSpace は U+001C〜U+001F を空白に含めないが、Python は含める。
// 実測で Python の '\x1c'.isspace() は True。この差は「訳が空か」や、
// credits.txt・credits.csv・fallback.txt の値の前後の空白の落とし方を変える
// （移植仕様「形式検証 / 敵対検証」[medium]）。U+0085 と U+00A0 は両者一致する。
//
// 003ed1e の上流は空白だけの行を物理行ごと落としていたので、この差が行番号の
// ずれにもなった。いまの上流（f816618 以降）は行を落とさないので、その経路は無い。
func IsPythonSpace(r rune) bool {
	if r >= 0x1C && r <= 0x1F {
		return true
	}
	return unicode.IsSpace(r)
}

// IsPythonBlank は Python の `not s.strip()` と同じ判定を返す。空文字は true。
//
// 「訳が空か」（形式検証 R20）と、credits.csv の「空の行か」をこの判定が決める。
// internal/validate も同じ関数を使うこと。別々に持つと、片方で空と見なして
// 片方で見なさない文字が生まれ、上流と判定が割れる。
func IsPythonBlank(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return !IsPythonSpace(r) }) < 0
}

// TrimPythonSpace は Python の str.strip()（引数なし）と同じく、前後の空白を落とす。
// 空白の集合は [IsPythonSpace] に従う。
func TrimPythonSpace(s string) string {
	return strings.TrimFunc(s, IsPythonSpace)
}

// PythonFieldLimit は Python の csv.field_size_limit() の既定値。単位は文字。
//
// これより長いフィールドを読むと csv.Error になる。上流の check_file はそれを
// 捕まえて「could not be parsed as CSV」の1件にする（f816618）。
const PythonFieldLimit = 131072

// PythonParseError は Python の csv.Error に当たる。
//
// 既定の dialect（strict=False）で実際に起きるのは、フィールドが
// [PythonFieldLimit] を超えたときだけ。行に NUL があるときの csv.Error は
// Python 3.11 で無くなった（上流の CI は 3.12）。
type PythonParseError struct {
	// Line は Python の reader.line_num に当たる。エラーが起きた文字のある物理行で、
	// レコードが複数行にまたがるときは先頭行ではなくその行を指す。
	Line int
	// Message は str(exc) に当たる英文。上流はこれを括弧の中にそのまま出す。
	Message string
}

func (e *PythonParseError) Error() string {
	return fmt.Sprintf("%d行目: %s", e.Line, e.Message)
}

// fieldLimitError は、フィールドが長すぎたときの [PythonParseError] を作る。
// 文面は CPython の _csv.c の "field larger than field limit (%ld)" のまま。
func fieldLimitError(line int) *PythonParseError {
	return &PythonParseError{
		Line:    line,
		Message: fmt.Sprintf("field larger than field limit (%d)", PythonFieldLimit),
	}
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
	// （Python の reader.line_num に対応する）。読み飛ばしたコメント行も数える。
	Consumed int
}

// ParsePythonRecords は Python の csv.reader（既定の dialect、strict=False）と
// 同じ状態機械で、与えられた行をすべてレコードへ分ける。コメントは扱わない。
//
// 公開ファイルは [ReadPythonRecords] で読むこと。こちらは textures/credits.csv の
// ように、上流が `list(csv.reader(fh))` をそのまま使うファイルのためにある。
// 空行は0フィールドのレコードとして返すので、要らなければ呼び出し側で捨てる。
//
// 行番号は [Line] が持っているので、レコードの先頭行の番号を
// [PythonRecord.Number] として返す。上流の「直前のレコードの reader.line_num の
// 次の行」と同じ対応付けになる（移植仕様「形式検証 R15」）。
//
// フィールドが [PythonFieldLimit] 文字を超えると、そこで読むのをやめて
// [*PythonParseError] を返す。それまでに読み終えたレコードも一緒に返す。
//
// 元実装との差として意図的に再現しないもの:
//
//   - 不正な UTF-8 での UnicodeDecodeError。ここではバイト列をそのまま値に入れる。
//
// 逆に、次の癖は再現する。引用符で囲まないフィールド中の裸の二重引用符は
// エラーにせずそのまま値に入れる（Go の encoding/csv は LazyQuotes を立てないと
// ここでパースを打ち切り、以降の全行が検査されなくなる）。閉じ引用符の直後に
// 文字が続く `"x"y"` は xy" になる。空行は0フィールドのレコードになる。
// NUL はただの文字として値に入る（Python 3.11 以降と同じ）。
func ParsePythonRecords(lines []Line) ([]PythonRecord, error) {
	return parsePythonRecords(lines, nil)
}

// parsePythonRecords は [ParsePythonRecords] の本体。comments が nil でなければ、
// レコードの境目に来た comments[i] が真の行をパーサーに渡さずに飛ばす。
//
// 飛ばすのはレコードの境目だけにする。引用値の途中（パーサーがまだ前のレコードを
// 読んでいる最中）に来た行は、偶奇の上でコメントに見えても値の一部として渡す。
// 上流は全行を csv.reader に通したうえで「コメント行から始まるレコード」だけを
// 捨てるので、途中の行はそのまま値に入る。ここで落とすと値が変わるうえ、閉じ
// 引用符がその行にあれば、後ろの行まで値に飲み込まれる。
func parsePythonRecords(lines []Line, comments []bool) ([]PythonRecord, error) {
	var out []PythonRecord
	p := &pythonParser{}
	consumed := 0

	for consumed < len(lines) {
		if comments != nil && comments[consumed] {
			// 上流はコメント行も csv.reader に通すので、長すぎるフィールドを含む
			// コメント行でも「could not be parsed as CSV」になる。レコードには
			// しないまま、その判定だけを合わせる。
			if limitExceeded(lines[consumed].Text) {
				return out, fieldLimitError(lines[consumed].Number)
			}
			consumed++
			continue
		}
		start := consumed
		complete := false
		for consumed < len(lines) {
			line := lines[consumed]
			consumed++
			// バイト単位で走査する。分岐対象がすべて ASCII で、UTF-8 の多バイト文字は
			// default 節でそのまま積まれるため、Python の文字単位の走査と一致する。
			for i := 0; i < len(line.Text); i++ {
				if !p.process(int(line.Text[i])) {
					// Python の reader.line_num はこの行を読んだ時点の累計なので、
					// レコードの先頭行ではなく、いま読んでいる行を指す。
					return out, fieldLimitError(line.Number)
				}
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
	return out, nil
}

// limitExceeded は、1行だけを新しいパーサーで読んだときに、フィールドが
// [PythonFieldLimit] を超えるかを返す。コメント行の判定に使う。
//
// 行の終わりで読むのをやめ、引用が閉じていなくても後ろの行へは進まない。
// コメント行の引用符はレコードを開かない、という [ReadPythonRecords] の前提に
// 合わせるため。
func limitExceeded(text string) bool {
	p := &pythonParser{}
	for i := 0; i < len(text); i++ {
		if !p.process(int(text[i])) {
			return true
		}
	}
	return false
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
	// fieldLen はいまのフィールドの文字数。_csv.c の field_len に当たり、
	// [PythonFieldLimit] と比べる。バイト数ではない。
	fieldLen int
}

func (p *pythonParser) saveField() {
	p.fields = append(p.fields, p.field.String())
	p.field.Reset()
	p.fieldLen = 0
}

// add はフィールドに1バイト足す。_csv.c の parse_add_char に当たり、足すと
// 上限を超えるときは足さずに false を返す。
//
// 上限は文字数で数える。UTF-8 の継続バイト（10xxxxxx）は前の文字の続きなので
// 数えない。「あ」を131073個並べた値は、バイト数では上限の3倍を超えるが、
// 上流では文字数で1つ超えたところで初めてエラーになる。
func (p *pythonParser) add(c byte) bool {
	if c&0xC0 != 0x80 {
		if p.fieldLen >= PythonFieldLimit {
			return false
		}
		p.fieldLen++
	}
	p.field.WriteByte(c)
	return true
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

// process は1文字（または行末の擬似文字）を読む。フィールドが上限を超えたときだけ
// false を返す。_csv.c の parse_process_char が -1 を返す経路に当たる。
func (p *pythonParser) process(c int) bool {
	switch p.state {
	case pyStartRecord:
		if c == pyEOL {
			// 空行。0フィールドのレコードとして確定する。
			return true
		}
		if c == '\n' || c == '\r' {
			p.state = pyEatCRNL
			return true
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
			p.state = pyInField
			return p.add(byte(c))
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
			return p.add(byte(c))
		}

	case pyInQuotedField:
		switch {
		case c == pyEOL:
			// 何もしない。行の改行文字そのものは上の走査で値に入っている。
			// これにより引用フィールド内の改行が保持され、CRLF も "\r\n" のまま残る。
			// 値に入った改行も上限の文字数に数える（_csv.c と同じ）。
		case c == '"':
			p.state = pyQuoteInQuotedField
		default:
			return p.add(byte(c))
		}

	case pyQuoteInQuotedField:
		switch {
		case c == '"':
			// "" はリテラルの " 1個。
			p.state = pyInQuotedField
			return p.add('"')
		case c == ',':
			p.saveField()
			p.state = pyStartField
		case c == pyEOL || c == '\n' || c == '\r':
			p.saveField()
			p.state = pyEndOfLineState(c)
		default:
			// strict=False なので閉じ引用符の直後の文字はそのまま値に入る。
			// `"x"y"` が xy" になるのはこの分岐。
			p.state = pyInField
			return p.add(byte(c))
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
	return true
}

// pyEndOfLineState は行末に達したあとの状態を返す。擬似文字なら1レコード確定、
// 実在の改行文字なら残りの改行を読み飛ばす状態へ移る。
func pyEndOfLineState(c int) pythonState {
	if c == pyEOL {
		return pyStartRecord
	}
	return pyEatCRNL
}
