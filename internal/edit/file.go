package edit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// Kind は行（セグメント）の種類。
type Kind int

// 種類は、publish と同じ区切りの関数（csvfile.SplitSegments）が分けたセグメントから
// 決める。閉じない引用符から後ろの物理行は、レコードとして解釈しない生の行として、
// 空白だけなら [KindBlank]、ほかは [KindData] にする。どちらも編集させない。
const (
	// KindComment はレコードの境目にある、生の先頭1文字が '#' の行。作業コピーと
	// 公開ファイルの `# ===== ... =====` / `# --- ... ---` の見出しがこれ。
	KindComment Kind = iota
	// KindBlank は空行相当の行。レコードの境目にある空行・空白だけの行（全角空白や
	// NO-BREAK SPACE だけの行も）と、ヘッダーより後ろの、読むと値がどれも空になる
	// レコード（"," や `""`、",,,,,," の行）。
	KindBlank
	// KindHeader は最初のレコード（区切りの関数のヘッダー）。"," の行でもヘッダーに
	// なる（受理はされない）。
	KindHeader
	// KindData はヘッダーより後ろのレコードと、レコードとして解釈しない生の行。
	KindData
)

// String は種類の名前を返す。テストの失敗メッセージ向け。
func (k Kind) String() string {
	switch k {
	case KindComment:
		return "comment"
	case KindBlank:
		return "blank"
	case KindHeader:
		return "header"
	case KindData:
		return "data"
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Line は1つの行（セグメント）の見え方。コメント行と空行は1物理行で1つ、レコードは
// 引用符で囲んだ値が行をまたげば複数の物理行で1つになる。
//
// [File.Lines] が返す複製で、書き換えても [File] には反映されない。書き換えは
// [File.SetTranslation] を通す。
type Line struct {
	// ID は1始まりの通し番号で、区切りの関数のセグメントの ID と同じ値になる。
	// 行の同定にはこれを使う。自分の保存では変わらない（書く前の事後確認で確かめる）。
	//
	// 閉じない引用符から後ろは、セグメントに分けずに1物理行ずつ並べ、引用符が開いた
	// セグメントの ID から続けて番号を振る。そのファイルは読み取り専用なので、この
	// 番号で書くことは無い。
	ID int
	// Number は最初の物理行（1始まり）、EndNumber は最後の物理行。1物理行に収まる
	// 行では同じ値になる。表示と報告の照合にだけ使い、同定には使わない。
	Number, EndNumber int
	// Kind は行の種類。
	Kind Kind
	// Text は終端（行を終える改行）を含む生のバイト列。行をまたぐレコードでは、
	// 途中の改行も含む。ファイルの最後の行に改行が無ければ終端を含まない。
	Text string
	// Fields は KindData のレコードとヘッダーだけに入る。全体を解釈して読んだ値
	// （csvfile.SplitSegments の Fields）を、区切りの数（Offsets の個数）まで空文字で
	// 埋めたもの。埋めるのは、未訳の行 `a,b,` で落ちる末尾の空フィールドを戻すため。
	// 値の中の改行は、ファイルにあるとおり（LF・CRLF・単独の CR）のまま入る。
	Fields []string
	// Editable はこの行の訳を書き換えてよいか。
	Editable bool
	// Reason は Editable が false のときの理由。
	Reason string
	// Cause は [Line.Reason] と同じ理由を、識別子と置換の組で持つ。
	//
	// 文面と別に持つのは、画面（internal/web）が目録で差し替えるためである。
	// Cause.Text は常に Reason と同じ文字列になる。
	Cause reason.Reason

	// orig は、読み込んだ（または最後に保存した）ときの Text。書く直前の確かめ
	// （[File.verify]）が、触っていない行が1バイトも変わらないことを見るのに使う。
	orig string
	// origKind は、読み込んだ（または最後に保存した）ときの Kind。書き換えたレコードを
	// 1つずつ確かめ直すとき（[File.culprit]）、確かめに入れないレコードを書き換える前の
	// 形で見るのに使う。訳を消して空行相当になったレコードは、Kind だけが変わる。
	origKind Kind
	// term は Text の終わりの終端（"\r\n" / "\n" / "\r" / ""）。
	term string
	// last は最終フィールドの開始位置（Text の先頭から）。KindData のレコードだけ。
	last int
	// columns は区切りの数（csvfile.FieldOffsets の個数）。KindData のレコードだけ。
	columns int
}

// setReason は編集できない理由を、文面と識別子の両方へ一度に入れる。
//
// 別々に代入できる形にすると、片方だけ書き換えた行がいずれ現れる。そのとき
// 画面は古い理由を英語で出し、CLI は新しい理由を日本語で出す。入口を1つにする。
func (l *Line) setReason(why reason.Reason) {
	l.Reason, l.Cause = why.Text, why
}

// Translation は最終フィールド（訳）を返す。
//
// データ行でないとき、および編集できないときは空を返す。列が足りない行の最終
// フィールドは訳ではなく別の列（speaker など）なので、それを訳として返すと画面の
// 訳欄に無関係な値が並ぶ。行の中身は [Line.Text] で生のまま見られる。
func (l Line) Translation() string {
	if l.Kind != KindData || !l.Editable || len(l.Fields) == 0 {
		return ""
	}
	return l.Fields[len(l.Fields)-1]
}

// Key は先頭フィールド（キー）を返す。データ行でなければ空。
func (l Line) Key() string {
	if l.Kind != KindData || len(l.Fields) == 0 {
		return ""
	}
	return l.Fields[0]
}

// body は Text から終端を除いたもの。
func (l Line) body() string { return l.Text[:len(l.Text)-len(l.term)] }

// File は1ファイル分の編集モデル。並行に使ってはいけない。
type File struct {
	path string
	// bom は先頭にあった UTF-8 BOM（無ければ空）。書き出しで先頭に戻す。
	bom string
	// header は受理されたヘッダー。読み取り専用で開いたときは nil。
	header []string
	lines  []Line
	// physical はファイルの物理行の数（csvfile.Segments の Lines）。
	physical int
	// version は読み込んだ（または最後に保存した）バイト列全体の SHA-256。
	version string
	dirty   bool
	// touched は、読み込み（または最後の保存）のあとに書き換えたレコードの、lines での
	// 添字。書く直前の確かめ（[File.verify]）が見る。
	touched        map[int]bool
	readOnly       bool
	readOnlyReason string
	// readOnlyCause は readOnlyReason と同じ理由を、識別子と置換の組で持つ。
	// 画面が目録から訳された文面を引くための鍵になる。
	readOnlyCause reason.Reason
}

// Parse はバイト列を編集モデルにする。壊れた入力でも誤りは返さない。
// ヘッダーが受理できないファイル、開いた引用符がファイルの終わりまで閉じない
// ファイル、行の区切りがすべて単独の CR のファイルは、読み取り専用の [File] になる
// （[File.ReadOnly] と [File.ReadOnlyReason] を見ること）。
//
// 行は、publish と同じ全体を解釈する読み方の区切り（csvfile.SplitSegments）の
// セグメントを1つずつ並べたものである。引用符で囲んだ値が物理行をまたぐレコードも
// 1つの行になり、値の中の '#' の行や空行を、見出しや区切りと取り違えない。
//
// 保存したいなら [Open] を使う。Parse で作った File は保存先を持たない。
func Parse(data []byte) *File {
	f := &File{version: hashBytes(data)}

	whole := csvfile.ReadPowerShellMarked(data)
	segs := whole.Segments
	f.bom = segs.Text[:segs.BOM]
	f.physical = segs.Lines

	headerIndex := -1
	var headerBody string
	// records は、KindData のレコードの f.lines での添字。編集可否は、ヘッダーが
	// 決まってから求める（[File.judge]）。
	var records []int
	for _, seg := range segs.List {
		if seg.Unclosed() {
			// 閉じない引用符が開いたセグメントから後ろは、レコードとして解釈せず、
			// 物理行のまま並べる（決まったことの 3）。
			f.appendRaw(segs, seg)
			break
		}
		line := Line{ID: seg.ID, Number: seg.Line, EndNumber: seg.EndLine,
			Text: segs.Text[seg.Start : seg.End+len(seg.Term)], term: string(seg.Term)}
		line.orig = line.Text
		line.Kind = kindOf(seg)
		line.origKind = line.Kind
		switch line.Kind {
		case KindHeader:
			headerIndex, headerBody = len(f.lines), segs.Body(seg)
		case KindData:
			line.Fields = padFields(seg.Fields, len(seg.Offsets))
			line.last = seg.Offsets[len(seg.Offsets)-1] - seg.Start
			line.columns = len(seg.Offsets)
			records = append(records, len(f.lines))
		}
		f.lines = append(f.lines, line)
	}

	// ヘッダーの受理は、いままでどおり生テキストの完全一致で見る（[matchHeader]）。
	// どの行をヘッダーにするかだけを区切りの関数にそろえた（決まったことのそのほか 8）。
	var header []string
	if headerIndex >= 0 {
		header = matchHeader(headerBody)
	}
	if header != nil {
		f.header = header
		f.lines[headerIndex].Fields = slices.Clone(header)
		// 形の検出は publish と同じ関数を呼ぶ（csvfile の detect.go）。閉じない引用符の
		// ファイルは全体を読み取り専用にするので見ない。
		var swallows map[int]csvfile.Swallow
		var games map[int]csvfile.Disagreement
		if segs.UnclosedLine == 0 {
			swallows = make(map[int]csvfile.Swallow)
			for _, s := range csvfile.FindSwallows(segs) {
				if _, seen := swallows[s.ID]; !seen {
					swallows[s.ID] = s
				}
			}
			games = make(map[int]csvfile.Disagreement)
			for _, d := range csvfile.CSharpDisagreements(whole) {
				games[d.ID] = d
			}
		}
		for _, i := range records {
			id := f.lines[i].ID
			sw, swallowed := swallows[id]
			game, disagrees := games[id]
			f.judge(&f.lines[i], swallowed, sw, disagrees, game)
		}
	}

	switch {
	case segs.UnclosedLine > 0:
		// 閉じない引用符は、ヘッダーの形より先に言う。ヘッダーの中で開いたときは、
		// ヘッダーにファイルの終わりまでが入っているので、受理されない理由より
		// 引用符のほうが直す先を指す（publish の形の確かめも (e) だけを出す）。
		f.markReadOnly(reason.New(reason.EditUnclosedQuote,
			fmt.Sprintf("%d行目で開いた引用符がファイルの終わりまで閉じない（そこから後ろがすべて1つの値になる）", segs.UnclosedLine),
			"line", strconv.Itoa(segs.UnclosedLine)))
	case headerIndex < 0:
		f.markReadOnly(reason.New(reason.EditNoHeader,
			"ヘッダー行が無い（空のファイルか、コメントと空行だけのファイル）"))
	case header == nil:
		// ヘッダー行そのものは %q で引用してから渡す。引用を目録の側にやらせると、
		// 言語ごとに引用符が変わり、同じファイルの同じ行が別の綴りで出る。
		number := f.lines[headerIndex].Number
		f.markReadOnly(reason.New(reason.EditBadHeader, fmt.Sprintf(
			"%d行目のヘッダーが %q で、受理される4種のいずれでもない（"+
				"key,section,node,order,speaker,translation / key,speaker,translation / key,translation / "+
				"key,section,node,order,speaker,source_en,translation）",
			number, headerBody),
			"line", strconv.Itoa(number),
			"text", fmt.Sprintf("%q", headerBody)))
	case csvfile.CROnlyLineBreaks(segs):
		// ゲームの読み方（CsvReader）は引用の外の CR を捨てるので、このファイルを
		// 1行と読み、どの訳も表示しない。dwloc の publish は CR も行の区切りにして読む
		// （上流の道具は訳をすべて落とす）ので、画面から書いた訳は公開ファイルには
		// 入りうるが、翻訳者はホットリロードで確かめられない。どのレコードも値が割れる
		// （csvfile.CSharpDisagreements）ので、1行ずつ断るより、ファイルの形を直す先と
		// して1つの理由で言う。
		f.markReadOnly(reason.New(reason.EditCROnly,
			"行の区切りが CR だけのファイル（ゲームはこのファイルを1行と読むので、画面からは書かない）"))
	}
	return f
}

// kindOf はセグメントの種類から行の種類を決める。
//
// 読むと値がどれも空になるレコードは、空行相当にする。"," や `""` の行（区切りの
// 関数の空のレコード）に加えて、",,,,,," の行もこれに入る（改善の ui-15）。キーも
// 原文も空なので publish はこの行を捨て（移植仕様 R17）、ここへ打った訳は黙って落ちる。
func kindOf(seg csvfile.Segment) Kind {
	switch seg.Kind {
	case csvfile.SegmentComment:
		return KindComment
	case csvfile.SegmentHeader:
		return KindHeader
	case csvfile.SegmentRecord:
		if !allEmpty(seg.Fields) {
			return KindData
		}
	}
	return KindBlank
}

// allEmpty は値がどれも空かを返す。
func allEmpty(fields []string) bool {
	for _, v := range fields {
		if v != "" {
			return false
		}
	}
	return true
}

// padFields は、値を区切りの数まで空文字で埋めた複製を返す。
func padFields(fields []string, columns int) []string {
	out := slices.Clone(fields)
	for len(out) < columns {
		out = append(out, "")
	}
	return out
}

// appendRaw は、閉じない引用符が開いたセグメント seg の最初の物理行からファイルの
// 終わりまでを、1物理行ずつ生の行として並べる。ID は seg の ID から続けて振る。
func (f *File) appendRaw(segs csvfile.Segments, seg csvfile.Segment) {
	id := seg.ID
	for n := seg.Line; n <= segs.Lines; n++ {
		body, term := segs.PhysicalLine(n)
		kind := rawKind(body)
		f.lines = append(f.lines, Line{ID: id, Number: n, EndNumber: n, Kind: kind,
			Text: body + string(term), orig: body + string(term), origKind: kind, term: string(term)})
		id++
	}
}

// rawKind は、レコードとして解釈しない物理行（閉じない引用符から後ろの行）の種類を
// 返す。空白だけの行は空行相当、ほかは生の行を見せるためにデータ行にする。'#' で
// 始まっていても見出しにはしない。引用符で囲んだ値の中の行で、publish はコメント
// として落とさない。
func rawKind(body string) Kind {
	if strings.TrimSpace(body) == "" {
		return KindBlank
	}
	return KindData
}

// judge は、受理したヘッダーのもとで、データのレコード1つの編集可否を決める。
//
// swallowed と sw は、そのレコードに飲み込みの疑いがあるか（csvfile.FindSwallows）と
// その最初の1件、disagrees と game は、ゲームの読み方と値が割れるか
// （csvfile.CSharpDisagreements）とその1件。理由は、直す先を指す順（列の数、飲み込み、
// ゲームの読み方との食い違い、訳の改行）に1つだけ付ける。
func (f *File) judge(line *Line, swallowed bool, sw csvfile.Swallow, disagrees bool, game csvfile.Disagreement) {
	line.Editable = false
	switch {
	case line.columns != len(f.header):
		// 安全弁。列が多い行を最終フィールドの位置で切ると、余った列を巻き込んで
		// 壊す。列が少ない行は別の列を訳だと思って書き換える。どちらも直せない
		// 壊し方なので、編集させずに翻訳者へ見せる。
		line.setReason(reason.New(reason.EditFieldCount,
			fmt.Sprintf("フィールド数がヘッダーと合わない（ヘッダーは%d列、この行は%d列）",
				len(f.header), line.columns),
			"header", strconv.Itoa(len(f.header)), "row", strconv.Itoa(line.columns)))
	case swallowed:
		// 引用符の閉じ誤りで、後ろの行（英語の原文やキー）を値に飲み込んでいる疑い。
		// そのまま訳を書くと、飲み込んだ行ごと publish へ渡る。正当な複数行の値でも
		// 当たるが、画面には確かめたうえで通す指定を置かない（publish の
		// --accept-multiline はレコードごとに、publish の側で指定する）。
		line.setReason(reason.New(reason.EditSwallow,
			fmt.Sprintf("%d行目がこのレコードの値に飲み込まれて見える（引用符の閉じ誤りの疑い）", sw.SwallowedLine),
			"line", strconv.Itoa(sw.SwallowedLine)))
	case disagrees && game.Column == "":
		line.setReason(reason.New(reason.EditGameMissesRecord,
			"ゲームの読み方（CsvReader）では、このレコードが見つからない"))
	case disagrees:
		// フィールドの途中の '"' などで、ゲームの読み方と値が割れる（移植仕様「CSVと
		// キー生成 R6」）。書くと、翻訳者が見ている値とゲームが表示する値が食い違う。
		line.setReason(reason.New(reason.EditGameDisagrees,
			fmt.Sprintf("ゲームの読み方（CsvReader）では %s 列の値が違って読まれる", game.Column),
			"column", game.Column))
	default:
		judgeTranslation(line)
	}
}

// judgeTranslation は、ほかの理由に当たらないレコードの編集可否を、訳の値で決める。
func judgeTranslation(line *Line) {
	if tr := line.Fields[len(line.Fields)-1]; strings.ContainsAny(tr, "\r\n") {
		// 訳への改行の入力は PR4 で足す（決まったことの 1）。いまの画面は改行を空白に
		// 置き換えるので、開いて1字打つと、翻訳者が見ていない改行まで消える。
		line.setReason(reason.New(reason.EditMultilineTranslation,
			"訳に改行がある（改行の入る訳は、まだ画面から書き換えられない）"))
		return
	}
	line.Editable = true
	line.setReason(reason.Reason{})
}

// Open はファイルを読んで編集モデルにする。読み取りに失敗したときだけ誤りを返す。
// 中身が読み取り専用になる場合も、誤りではなく [File] として返る。
func Open(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f := Parse(data)
	f.path = path
	return f, nil
}

// Path は保存先。[Parse] で作った File では空。
func (f *File) Path() string { return f.path }

// Header は受理されたヘッダーの複製。読み取り専用で開いたときは nil。
func (f *File) Header() []string { return slices.Clone(f.header) }

// Version は読み込んだ（または最後に保存した）バイト列全体の SHA-256 の16進。
// 保存時の照合に使うので、画面へ渡して次の保存要求で返してもらうとよい。
func (f *File) Version() string { return f.version }

// ReadOnly はファイル全体が読み取り専用かを返す。
func (f *File) ReadOnly() bool { return f.readOnly }

// ReadOnlyReason は読み取り専用にした理由。そうでなければ空。
func (f *File) ReadOnlyReason() string { return f.readOnlyReason }

// ReadOnlyCause は [File.ReadOnlyReason] と同じ理由を、識別子と置換の組で返す。
// 読み取り専用でなければ空。
func (f *File) ReadOnlyCause() reason.Reason { return f.readOnlyCause }

// Dirty は読み込み後に1バイトでも変えたかを返す。
func (f *File) Dirty() bool { return f.dirty }

// PhysicalLines はファイルの物理行の数を返す。区切りは "\r\n" / "\n" / "\r" の3種で、
// BOM は数えない。末尾の改行の後ろに空の行は数えない。
func (f *File) PhysicalLines() int { return f.physical }

// Lines はすべての行を複製して返す。
func (f *File) Lines() []Line {
	out := make([]Line, len(f.lines))
	for i, line := range f.lines {
		line.Fields = slices.Clone(line.Fields)
		out[i] = line
	}
	return out
}

// Line は ID で1行を引く。無ければ第2戻り値が false。
func (f *File) Line(id int) (Line, bool) {
	i, ok := f.indexOf(id)
	if !ok {
		return Line{}, false
	}
	line := f.lines[i]
	line.Fields = slices.Clone(line.Fields)
	return line, true
}

// SetTranslation は ID が id のレコードの最終フィールド（訳）を value に差し替える。
//
// 書き換えるのは最終フィールドの開始位置からレコードの本体の終わり（レコードを
// 終える改行の手前）までだけで、ほかの行にも、このレコードの前半にも触れない。
// 終端はそのレコードが元々持っていた種類（CRLF / LF / CR / 無し）のまま残る。
// 原文が行をまたぐレコードも、訳が1行に収まるかぎり書ける。
//
// 差し替えた結果がいまのレコードと1バイトも変わらないなら、何もせず nil を返す
// （[File.Dirty] も立たない）。
//
// 書き換える前に、差し替えたレコードだけを読み直して確かめる（書く前の事後確認の
// 前半。[recheckRecord]）。外れたら書き換えずに、理由 reason.EditRecheckFailed の
// [NotEditableError] を返す。ファイル全体の確かめは [File.Save] が書く直前に行う。
//
// 誤りを返す場合:
//
//   - ファイル全体が読み取り専用: [ErrReadOnly]
//   - 行が無い / データ行でない / 編集できない行 / 読み直すと合わない: [NotEditableError]
//   - value に CR か LF が入っている: [InvalidValueError]
//
// value の CR / LF を拒むのは、訳への改行の入力を PR4 で足すからである
// （決まったことの 1）。
func (f *File) SetTranslation(id int, value string) error {
	if f.readOnly {
		return fmt.Errorf("%w: %s", ErrReadOnly, f.readOnlyReason)
	}
	i, ok := f.indexOf(id)
	if !ok {
		return notEditable(id, 0, reason.New(reason.EditNoSuchLine, "そんな行は無い"))
	}
	line := &f.lines[i]
	if line.Kind != KindData {
		// 種類の名前（comment / blank / header / data）は ASCII のまま渡す。
		// ファイルの見え方を指す語で、[Kind.String] と doc コメントが同じ綴りを
		// 使っている。訳すと、画面と説明が別の語で同じものを指すことになる。
		return notEditable(id, line.Number, reason.New(reason.EditNotDataLine,
			"データ行ではない（"+line.Kind.String()+"）", "kind", line.Kind.String()))
	}
	if !line.Editable {
		return notEditable(id, line.Number, line.Cause)
	}
	if strings.ContainsAny(value, "\r\n") {
		return invalidValue(id, line.Number, reason.New(reason.EditNoNewline, "訳に改行は入れられない"))
	}
	if strings.ContainsRune(value, 0) {
		// NUL は Python の csv.reader が _csv.Error にする値だが、
		// internal/validate はその再現をしていない。ここで止めないと
		// 誰も気づかないまま公開ファイルまで届く。
		return invalidValue(id, line.Number, reason.New(reason.EditNoNUL, "訳に NUL は入れられない"))
	}
	if !utf8.ValidString(value) {
		// 不正なUTF-8も後段のどこも検出しない（validate の doc コメント参照）。
		// 書けない値は書かせない、という CR/LF と同じ扱いにする。
		//
		// HTTP の経路からここは立たない。[encoding/json] が不正なバイトを
		// U+FFFD へ置き換えてしまい、届く文字列はもう正しい UTF-8 だからである。
		// そちらは internal/web が復号する前に本文のバイト列を見て止めている
		// （handleRows の "error.bad_utf8"）。だからここを消してよい、とは
		// ならない。このパッケージを直に使う側には、まだここしか無い。
		return invalidValue(id, line.Number, reason.New(reason.EditBadUTF8, "訳が正しいUTF-8ではない"))
	}

	// 最終フィールドの開始位置から本体の終わりまで。ここより前は1バイトも触らない。
	text := line.body()[:line.last] + escapeTranslation(value) + line.term
	if text == line.Text {
		return nil
	}
	fields, ok := recheckRecord(*line, text, value)
	if !ok {
		return notEditable(id, line.Number, recheckReason(line.Number))
	}
	line.Text = text
	line.Fields = fields
	if allEmpty(fields) {
		// 訳を消した結果、値がどれも空になった（キーの空いた2列の行を "," にしたとき
		// など）。読み直すと空行相当になるので（[kindOf]）、モデルもそろえる。訳を
		// 消すことは断らない。そのレコードは、キーも原文も空なので publish が捨てる
		// （移植仕様 R17）。消す前の訳も公開されていないので、消して失うものは無い。
		line.Kind = KindBlank
		line.Fields = nil
		line.Editable = false
		line.setReason(reason.New(reason.EditNotRecord, "この行はレコードとして読まれない"))
	}
	if f.touched == nil {
		f.touched = make(map[int]bool)
	}
	f.touched[i] = true
	f.dirty = true
	return nil
}

// recheckRecord は、差し替えたレコードの生のバイト列 text を読み直し、書いてよい形かを
// 確かめる（書く前の事後確認の前半。決まったことのそのほか 4）。書いてよければ、
// 読み直した値（区切りの数まで空文字で埋めたもの）を返す。
//
// レコードはレコードの境目（引用の外の物理行の先頭）から始まり、読み方は後ろへ向かう
// だけなので、text だけを読んでもファイルの中で読むのと同じ値になる。ファイル全体の
// 確かめは [File.Save] が書く直前に1回行う（[File.verify]）。
//
// 確かめること:
//
//   - 区切りの関数で1つのセグメントとして読め、引用符が閉じ、本体の終わりと終端が
//     差し替えた位置のとおりで、物理行の数が変わらない
//   - 区切りの数が変わらず、訳より前の値が元と同じで、訳を value として読む
//   - ゲームの読み方（CsvReader の移植）でも同じ値に読む
func recheckRecord(orig Line, text, value string) ([]string, bool) {
	segs := csvfile.SplitSegments([]byte(text))
	if len(segs.List) != 1 {
		return nil, false
	}
	seg := segs.List[0]
	if seg.Unclosed() || seg.End != len(text)-len(orig.term) || string(seg.Term) != orig.term ||
		seg.EndLine-seg.Line != orig.EndNumber-orig.Number || len(seg.Offsets) != orig.columns {
		return nil, false
	}
	fields := padFields(seg.Fields, len(seg.Offsets))
	if !slices.Equal(fields[:len(fields)-1], orig.Fields[:len(orig.Fields)-1]) || fields[len(fields)-1] != value {
		return nil, false
	}
	if game := csvfile.ParseCSharpRecords(text); len(game) != 1 || !slices.Equal(game[0], fields) {
		return nil, false
	}
	return fields, true
}

// recheckReason は、書く前の事後確認が外れたときの理由を作る。line はそのレコードの
// 最初の物理行。
func recheckReason(line int) reason.Reason {
	return reason.New(reason.EditRecheckFailed,
		fmt.Sprintf("%d行目から始まるレコードを書き換えて読み直すと、書いた訳のほかまで変わって読める", line),
		"line", strconv.Itoa(line))
}

// verify は、書こうとしているバイト列 out をファイル全体として読み直し、編集モデルと
// 同じに読めることを確かめる（書く前の事後確認の後半。決まったことのそのほか 4）。
//
// 確かめること:
//
//   - セグメントの数・ID・物理行の範囲・種類・バイト列が、どの行もモデルと同じ
//     （ID が保存の前後で変わらないことと、触っていない行が1バイトも変わらないこと）
//   - 書き換えたレコードの値が、ファイル全体を読んでもモデルと同じ
//   - 書き換えたレコードが、飲み込みの疑い（csvfile.FindSwallows）にも、ゲームの
//     読み方との食い違い（csvfile.CSharpDisagreements）にも当たらない
//   - 書き換えたレコードのほかは、ゲームの読み方（CsvReader の移植）で読んだ値が、
//     保存の前後で変わらない（[File.gameChange]）
//
// 外れたら、原因の書き換えたレコードを指す [RecheckError] を返す（[File.culprit]）。
func (f *File) verify(out []byte) error {
	at := f.mismatch(out, f.touched)
	if at < 0 {
		return nil
	}
	return f.recheckError(f.culprit(at))
}

// mismatch は、touched の添字のレコードを書き換えたバイト列 out をファイル全体として
// 読み直し（[File.verify] の確かめ）、編集モデルと同じに読めなければ、外れたところの
// f.lines での添字を返す。同じに読めれば -1。
//
// touched に無いレコードは、読み込んだとき（または最後に保存したとき）のままのはずの
// レコードとして見る。f.touched のうち touched に無いレコード（[File.culprit] が1つずつ
// 確かめ直すとき）も、書き換える前のバイト列と種類で見る。
func (f *File) mismatch(out []byte, touched map[int]bool) int {
	whole := csvfile.ReadPowerShellMarked(out)
	segs := whole.Segments
	for i, line := range f.lines {
		if i >= len(segs.List) {
			return i
		}
		seg := segs.List[i]
		// 触っていない行は、読み込んだとき（または最後に保存したとき）のバイト列と比べる。
		// モデルと比べるだけでは、モデルの側で壊れた行を見逃す。
		want, kind := line.orig, line.Kind
		switch {
		case touched[i]:
			want = line.Text
		case f.touched[i]:
			kind = line.origKind
		}
		if seg.Unclosed() || seg.ID != line.ID || seg.Line != line.Number || seg.EndLine != line.EndNumber ||
			kindOf(seg) != kind || segs.Text[seg.Start:seg.End+len(seg.Term)] != want {
			return i
		}
		if touched[i] && line.Kind == KindData && !slices.Equal(padFields(seg.Fields, len(seg.Offsets)), line.Fields) {
			return i
		}
	}
	if len(segs.List) != len(f.lines) {
		return len(f.lines)
	}
	for _, s := range csvfile.FindSwallows(segs) {
		if touched[s.ID-1] {
			return s.ID - 1
		}
	}
	for _, d := range csvfile.CSharpDisagreements(whole) {
		if touched[d.ID-1] {
			return d.ID - 1
		}
	}
	return f.gameChange(whole, out, touched)
}

// culprit は、書く直前の確かめが f.lines の添字 at で外れたとき、原因として指す書き換えた
// レコードの添字を返す。
//
// 書き換えたレコードが2つ以上あれば、1つずつ、そのレコードだけを書き換えたバイト列で
// 確かめ直し（[File.mismatch]）、それだけでも外れる最初のレコードを指す。外れたところ（at）
// から決めると、原因ではないレコードを指すことがある。キーも原文も空のレコードの訳を
// 書き換えて、ゲームの引用の閉じる位置が動き、後ろのレコードをゲームが見つけられなく
// なる形では、同じ要求で後ろのレコードも書き換えていると、外れるのはその後ろのレコードの
// 食い違いとしてである。画面は指したレコードの送り直しを止め、ほかを送り直すので、単独なら
// 書ける後ろのレコードを止め、原因のレコードを次の要求で送り直すことになる（PR3 の検証の
// 指摘）。
//
// どのレコードも単独では外れない（組み合わせたときだけ外れる）ときと、書き換えたレコードが
// 1つのときは、at かそれより前で最も近い書き換えたレコードを指し、無ければ最初の書き換えた
// レコードを指す。区切りは前から後ろへ読むので、at で外れた原因は、ふつう at かそれより前の
// 書き換えにある。
func (f *File) culprit(at int) int {
	touched := slices.Sorted(maps.Keys(f.touched))
	if len(touched) > 1 {
		for _, i := range touched {
			only := map[int]bool{i: true}
			if f.mismatch(f.bytesWith(only), only) >= 0 {
				return i
			}
		}
	}
	blame := -1
	for _, i := range touched {
		if i <= at {
			blame = i
		}
	}
	if blame < 0 {
		blame = touched[0]
	}
	return blame
}

// gameChange は、touched の添字のレコード（書き換えたレコード）のほかで、ゲームの読み方
// （CsvReader の移植。csvfile.ReadCSharpRows）で読んだ値が、保存の前後で変わったところを
// 探し、f.lines での添字を返す。変わらなければ -1。保存の前は、読み込んだとき（または
// 最後に保存したとき）のバイト列である。
//
// 引用符の崩れたレコードがあると、ゲームはそこから引用を始め、後ろのレコードを値に
// 取り込むことがある。そうしたレコードは編集させないが、キーも原文も空のレコードは
// ゲームが引かないので、食い違いの判定（csvfile.CSharpDisagreements）に入らず、編集
// できる。その訳を書き換えると、ゲームの引用の閉じる位置が動き、触っていない後ろの
// レコードをゲームが見つけられなくなることがある（PR3 の検証で再現した）。publish の
// 読み方は変わらないので、ほかの確かめでは見つからない。
//
// 比べるのは、ゲームが訳を引ける鍵（csvfile.RecordIdentity。key、無ければ source_en）
// ごとの行である。鍵の無い行はゲームが使わないので比べない。書き換えたレコードの鍵の
// 行は、訳の列だけが変わってよい（その値が書いた訳であることは、食い違いの判定が見る）。
// 変わった鍵が2つ以上あれば、ファイルの前にあるほうを返す。その鍵のレコードが publish の
// 読み方に無ければ（ゲームだけが読む行）、ファイルの終わり（len(f.lines)）を返す。
func (f *File) gameChange(whole csvfile.PowerShellFile, out []byte, touched map[int]bool) int {
	before, after := gameView(f.bytesWith(nil)), gameView(out)
	rewritten := make(map[string]bool)
	first := make(map[string]int)
	for _, r := range whole.Records {
		id, ok := csvfile.RecordIdentity(r.Row)
		if !ok {
			continue
		}
		if touched[r.ID-1] {
			rewritten[id] = true
		}
		if _, seen := first[id]; !seen {
			first[id] = r.ID - 1
		}
	}
	at := -1
	blame := func(id string) {
		i, ok := first[id]
		if !ok {
			i = len(f.lines)
		}
		if at < 0 || i < at {
			at = i
		}
	}
	for id, rows := range after {
		if !sameGameRows(before[id], rows, rewritten[id]) {
			blame(id)
		}
	}
	for id := range before {
		if _, ok := after[id]; !ok {
			blame(id)
		}
	}
	return at
}

// gameView は、data をゲームの読み方で読み、突き合わせの鍵ごとに行を並べる。
func gameView(data []byte) map[string][]csvfile.Row {
	out := make(map[string][]csvfile.Row)
	for _, r := range csvfile.ReadCSharpRows(data) {
		if id, ok := csvfile.RecordIdentity(r); ok {
			out[id] = append(out[id], r)
		}
	}
	return out
}

// sameGameRows は、同じ鍵の行の並び a と b が、同じ列に同じ値を持つかを返す。
// translation が true なら、訳の列（translation）の違いは見ない。
func sameGameRows(a, b []csvfile.Row, translation bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		cols := a[i].Columns()
		if !slices.Equal(cols, b[i].Columns()) {
			return false
		}
		for _, c := range cols {
			if translation && csvfile.FoldASCII(c) == csvfile.FoldASCII("translation") {
				continue
			}
			if a[i].Get(c) != b[i].Get(c) {
				return false
			}
		}
	}
	return true
}

// bytesWith は、only の添字のレコードだけを書き換えたバイト列を返す。ほかの行は、
// 読み込んだとき（または最後に保存したとき）のバイト列のままにする。only が空なら、
// 読み込んだとき（または最後に保存したとき）のバイト列になる。
func (f *File) bytesWith(only map[int]bool) []byte {
	n := len(f.bom)
	for i, line := range f.lines {
		if only[i] {
			n += len(line.Text)
		} else {
			n += len(line.orig)
		}
	}
	out := make([]byte, 0, n)
	out = append(out, f.bom...)
	for i, line := range f.lines {
		if only[i] {
			out = append(out, line.Text...)
		} else {
			out = append(out, line.orig...)
		}
	}
	return out
}

// recheckError は、f.lines の添字 i の書き換えたレコードを指す、書く直前の確かめの誤りを
// 作る（指すレコードは [File.culprit] が決める）。
func (f *File) recheckError(i int) *RecheckError {
	line := f.lines[i]
	return &RecheckError{ID: line.ID, Line: line.Number, Cause: recheckReason(line.Number)}
}

// escapeTranslation は訳をCSVの1フィールドとして書ける形にする。
//
// csvfile.EscapeField に、前後に空白がある値を引用する規則を足したもの。
// EscapeField 自体は変えない。publish と WorkingCopy.cs が書き出す
// バイト列と一致していることが、13ロケールのバイト一致テストの土台だからである。
//
// 足す理由は、この値を読む相手が1つではないこと。同じファイルを次の3つが読む。
//
//	ゲーム内Mod   CsvReader.cs（移植は csvfile.ParseCSharpRecords）。空白を削らない
//	publish       ConvertFrom-Csv（移植は csvfile.ReadPowerShell）。
//	              引用符なしフィールドの先頭空白を全部、末尾空白を1個残して削る
//	validate      Python の csv.reader。空白を削らない
//
// 引用せずに ` 訳 ` と書くと、この3つが別々の値を返す。翻訳者が画面で見ている値と、
// ホットリロードでゲームが実際に表示する値が食い違い、しかも画面の値を入れ直すと
// バイトがまた変わる（冪等でない）。引用すれば3つとも書いたままの値を返す。
//
// 前後に空白が無ければ EscapeField と同じ結果になるので、ふつうの訳では
// 出力は1バイトも変わらない。
func escapeTranslation(value string) string {
	escaped := csvfile.EscapeField(value)
	if strings.HasPrefix(escaped, `"`) {
		// EscapeField が既に引用している（カンマ・引用符・改行を含む）。
		return escaped
	}
	if value == "" || strings.TrimLeft(value, " 	") == value && strings.TrimRight(value, " 	") == value {
		return escaped
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

// Bytes は現在の中身をバイト列にする。行の生テキストを順に連結するだけなので、
// 1行も編集していなければ読み込んだバイト列と完全に一致する。
func (f *File) Bytes() []byte {
	n := len(f.bom)
	for _, line := range f.lines {
		n += len(line.Text)
	}
	out := make([]byte, 0, n)
	out = append(out, f.bom...)
	for _, line := range f.lines {
		out = append(out, line.Text...)
	}
	return out
}

// indexOf は ID から f.lines の添字を引く。
// ID は1始まりの連番なので添字は id-1 だが、入力が壊れていても
// 落ちないように範囲と実際の ID を確かめる。
func (f *File) indexOf(id int) (int, bool) {
	i := id - 1
	if i < 0 || i >= len(f.lines) || f.lines[i].ID != id {
		return 0, false
	}
	return i, true
}

// markReadOnly はファイル全体を読み取り専用にする。
// データ行はどれも編集させず、理由を入れておく。
//
// 閉じない引用符のファイルでは、引用符が開いたレコードより前の行は読めていて
// （[File.judge] が編集できると決めていることがある）、そこからも編集可否を
// 倒す。ファイル全体が書けない以上、直す先はファイル全体の理由のほうである。
func (f *File) markReadOnly(why reason.Reason) {
	f.readOnly = true
	f.readOnlyReason = why.Text
	f.readOnlyCause = why
	for i := range f.lines {
		if f.lines[i].Kind == KindData {
			f.lines[i].Editable = false
			f.lines[i].setReason(why)
		}
	}
}

// hashBytes は版を計算する。バイト列全体の SHA-256 の16進。
// BOM も改行も含めた全バイトが対象なので、1バイトでも違えば別の版になる。
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
