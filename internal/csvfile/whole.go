package csvfile

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
// 読み方は区切りの関数（[SplitSegments]）が決め、主の読み手（[ReadPowerShell]）と
// 同じである。違いは誤りの扱いだけで、この関数は誤りを返さない。閉じない引用符は
// UnclosedLine に印を付けるだけで、列名の重複は確かめない（重複したときの値は
// [NewRow] のとおり後の列が勝つ）。
//
// この関数は publish の守り（internal/publish の形の確かめ）専用である。publish は
// まだ1物理行を1レコードとする [ReadPowerShellRows] で読むので、行単位では読み違える
// ファイルを、この関数の結果と突き合わせて見つけて止める。列名の重複は行単位の
// 読み方が先に確かめている。全体を解釈する読み手へ移す作業（docs/port-spec.md）の
// PR2 で publish の読み手が [ReadPowerShell] へ移れば、この関数は使われなくなる。
//
// 上流と意図して違えている点は [SplitSegments] の doc コメントにある。"," だけの
// 行は、全文の ConvertFrom-Csv なら空の値2つのレコードとして返す（pwsh 7.6.6 で
// 実測）が、ここでは行単位の読み方と同じく空行相当として落とす。key も訳も
// 持たないレコードなので、訳の突き合わせは変わらない。
func ReadPowerShellWhole(data []byte) PowerShellWhole {
	segs := SplitSegments(data)
	w := PowerShellWhole{UnclosedLine: segs.UnclosedLine}
	for _, seg := range segs.List {
		switch seg.Kind {
		case SegmentHeader:
			w.Header = WholeHeader{Fields: seg.Fields, Line: seg.Line, EndLine: seg.EndLine, Unclosed: seg.Unclosed()}
		case SegmentRecord:
			w.Rows = append(w.Rows, WholeRecord{
				Row:  NewRow(w.Header.Fields, seg.Fields),
				Line: seg.Line, EndLine: seg.EndLine, Unclosed: seg.Unclosed(),
			})
		}
	}
	return w
}
