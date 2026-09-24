package csvfile

import (
	"fmt"
	"strings"
)

// UnclosedQuoteError は、開いた引用符がファイルの終わりまで閉じないことを表す。
//
// 上流の hash-strings.ps1 の全文の読み方（ConvertFrom-Csv）は、そこから後ろを
// すべて1つの値として読み、英語の原文まで訳に飲み込んで公開ファイルへ書く
// （上流の報告 #11）。値を返すと、確かめ忘れた呼び出し側から同じ漏れが起きるので、
// [ReadPowerShell] は値を返さずにこの誤りを返す。どの呼び出し側も同じ扱いになる。
type UnclosedQuoteError struct {
	// Line は引用符が開いた物理行（1始まり）。
	Line int
}

func (e *UnclosedQuoteError) Error() string {
	return fmt.Sprintf("%d行目で開いた引用符がファイルの終わりまで閉じない", e.Line)
}

// PowerShellFile は、全体を解釈する読み手（[ReadPowerShell] と
// [ReadPowerShellMarked]）が読んだ結果。
type PowerShellFile struct {
	// Segments は区切りの関数（[SplitSegments]）の結果。バイト位置と ID は
	// ここから引く。
	Segments Segments
	// Header はヘッダー。レコードが1つも無ければ ID が 0 になる。
	Header PowerShellHeader
	// Records はデータのレコード。空のレコード（"," など）は入らない。
	Records []PowerShellRecord
	// Unclosed は閉じない引用符。無ければ nil。
	Unclosed *UnclosedQuoteError
	// Duplicate はヘッダーの列名の重複。無ければ nil。
	Duplicate *DuplicateColumnError
}

// Err は、読み方の誤りを1つ返す。無ければ nil。
//
// 閉じない引用符を先に返す。そこから後ろ（ヘッダーの列名のこともある）が
// 1つの値に崩れているので、列名の重複より先に直すべきだからである。
func (f PowerShellFile) Err() error {
	if f.Unclosed != nil {
		return f.Unclosed
	}
	if f.Duplicate != nil {
		return f.Duplicate
	}
	return nil
}

// Rows は、データのレコードの値だけを読んだ順に並べて返す。レコードが1つも
// 無ければ nil。
//
// 行番号も ID も要らない使い手（publish の集め方、diff、order）のための入口である。
// 行単位の [ReadPowerShellRows] と同じ形で返すので、使い手は読み方だけを替えられる。
func (f PowerShellFile) Rows() []Row {
	if len(f.Records) == 0 {
		return nil
	}
	rows := make([]Row, len(f.Records))
	for i, r := range f.Records {
		rows[i] = r.Row
	}
	return rows
}

// PowerShellHeader はヘッダーのレコード。
type PowerShellHeader struct {
	// Fields は列名。
	Fields []string
	// ID はヘッダーのセグメントの ID。ヘッダーが無ければ 0。
	ID int
	// Line と EndLine は物理行の範囲。ヘッダーが無ければ 0。
	Line, EndLine int
	// Unclosed は、ヘッダーの中で開いた引用符が閉じなかったかどうか。
	Unclosed bool
}

// CommentLike は、読んだ最初の列名そのものが '#' で始まるかを返す。
//
// 行頭が '#' の物理行はコメントとして落ちるので、ここに来るのは `"#key"` や
// ` #key`（先頭の半角空白は読み手が落とす）のように、引用符や空白の後ろに '#' が
// あるヘッダーだけである。上流の ConvertFrom-Csv はこのヘッダーを飛ばし、次の
// レコード（データ）をヘッダーにするので、そのファイルの訳を1行も公開しない。
// dwloc はこれをヘッダーとして返し、呼び出し側（publish の形の確かめ (a)）がこれを
// 見て止める。(a) を「key 列が無い」に広げると、正当な source_en,translation の2列の
// 作業コピーまで止まるので、'#' を見るこの判定を別に置く。
//
// 判定は上流の飛ばし方に合わせ、空白を除かずに見る。引用の中の空白（`" #key"`）や、
// 読み手が落とさない NO-BREAK SPACE の後ろに '#' があるヘッダーは、上流では飛ばさずに
// その名前の列になり、source_en から訳を書く（pwsh 7.4.6 と 7.6.6 で実測）。訳を
// 失う形ではないので、dwloc も止めずに上流と同じに書く（入力の表の
// hash-header-quoted-space、hash-header-nbsp）。
func (h PowerShellHeader) CommentLike() bool {
	return len(h.Fields) > 0 && strings.HasPrefix(h.Fields[0], "#")
}

// PowerShellRecord はデータのレコード1つ。
type PowerShellRecord struct {
	// Row はヘッダーと対応づけた値。
	Row
	// ID はレコードのセグメントの ID。行の同定に使う。
	ID int
	// Line と EndLine は物理行の範囲。表示と報告の照合に使う。
	Line, EndLine int
	// Unclosed は、このレコードの中で開いた引用符が閉じなかったかどうか。
	// [ReadPowerShell] の結果では常に false。
	Unclosed bool
}

// MultiLine は、レコードが2物理行以上にまたがるかを返す。
func (r PowerShellRecord) MultiLine() bool { return r.EndLine > r.Line }

// ReadPowerShell は、上流 main の tools/hash-strings.ps1 の Read-Csv と同じく、
// ファイル全体を1つの文字列として読む（主の読み手）。
//
// 区切りは [SplitSegments] が決める。引用符で囲んだ値は物理行をまたいで1つの値に
// なり、値の中の LF・CRLF・単独の CR はそのまま残る。引用の外の空行・空白だけの
// 行・'#' で始まる行は落とす。最初のレコードがヘッダーで、"," や `""` のように
// 読むとフィールドが空になるデータのレコードは黙って落とす（上流は空の値の
// レコードとして返し、publish の集計で malformed dropped に数える。出力は変わらず、
// 違うのは集計の数だけである）。
//
// 誤りは2つで、どちらのときも値は返さない。
//
//   - 閉じない引用符: [*UnclosedQuoteError]。上流は後ろを丸ごと1つの値に飲み込む。
//   - ヘッダーの列名の重複: [*DuplicateColumnError]。データが0件でも返す。上流 main の
//     ConvertFrom-Csv は、データが0件でも重複で例外を投げる（pwsh 7.6.6 で実測）。
//     行単位の [ReadPowerShellRows] は、移植の基準 003ed1e の `$lines.Count -lt 2` に
//     合わせてデータが0件なら確かめなかったが、止まる側へそろえる。
//
// 両方に当たるときは閉じない引用符を返す（[PowerShellFile.Err]）。
//
// 最初の列名が '#' で始まるヘッダー（`"#key"` など）は、上流と違って飛ばさずに
// ヘッダーとして返す（[PowerShellHeader.CommentLike]）。
//
// publish・diff・order は、全体を解釈する読み手へ移す作業（docs/port-spec.md）の
// PR2 から、この読み手で読む。edit は PR2 から行の種類を区切りの関数で決め、保存の
// 単位をレコードへ移すのは PR3 である。
func ReadPowerShell(data []byte) (PowerShellFile, error) {
	f := ReadPowerShellMarked(data)
	if err := f.Err(); err != nil {
		return PowerShellFile{}, err
	}
	return f, nil
}

// ReadPowerShellMarked は [ReadPowerShell] と同じ読み方をするが、誤りを返さず、
// 閉じない引用符と列名の重複を印（Unclosed と Duplicate）として付けて返す。
//
// 壊れたファイルでも画面に並べるための入口である。閉じない引用符のあるファイルでは、
// 引用符が開いたレコードの値にファイルの終わりまでが入っている。その値を訳として
// 書いたり公開したりしてはいけない。画面はファイル全体を読み取り専用にし、
// そのレコードより後ろを物理行のまま並べる（[Segments.PhysicalLine]）。
// 列名が重複したときの値は [NewRow] のとおり後の列が勝つ。
func ReadPowerShellMarked(data []byte) PowerShellFile {
	f := PowerShellFile{Segments: SplitSegments(data)}
	for _, seg := range f.Segments.List {
		switch seg.Kind {
		case SegmentHeader:
			f.Header = PowerShellHeader{Fields: seg.Fields, ID: seg.ID,
				Line: seg.Line, EndLine: seg.EndLine, Unclosed: seg.Unclosed()}
			f.Duplicate = checkDuplicateColumns(seg.Fields)
		case SegmentRecord:
			f.Records = append(f.Records, PowerShellRecord{Row: NewRow(f.Header.Fields, seg.Fields),
				ID: seg.ID, Line: seg.Line, EndLine: seg.EndLine, Unclosed: seg.Unclosed()})
		}
	}
	if line := f.Segments.UnclosedLine; line > 0 {
		f.Unclosed = &UnclosedQuoteError{Line: line}
	}
	return f
}
