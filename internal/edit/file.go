package edit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// Kind は物理行の種類。
type Kind int

const (
	// KindComment は生の先頭1文字が '#' の行。作業コピーと公開ファイルの
	// `# ===== ... =====` / `# --- ... ---` の見出しがこれ。
	KindComment Kind = iota
	// KindBlank は空行相当の行。csvfile.ParsePowerShellRecord の
	// 第2戻り値が false になる行（"", " ", ",", `""`, `"`）。
	KindBlank
	// KindHeader は最初に現れた「コメントでも空行相当でもない行」。
	KindHeader
	// KindData はヘッダーより後ろの、コメントでも空行相当でもない行。
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

// Line は1物理行の見え方。[File.Lines] が返す複製で、書き換えても
// [File] には反映されない。書き換えは [File.SetTranslation] を通す。
type Line struct {
	// Number は1始まりの物理行番号。行の同定にはこれを使う。
	Number int
	// Kind は行の種類。
	Kind Kind
	// Text は改行文字を含む生の行。末尾行に改行が無ければ含まない。
	Text string
	// Fields は KindData のときだけ入る。csvfile.ParsePowerShellRecord の
	// 結果を、区切りの数（csvfile.FieldOffsets の個数）まで空文字で埋めたもの。
	// 埋めるのは、未訳行 `a,b,` で落ちる末尾の空フィールドを戻すため。
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
// データ行でないとき、および列数がヘッダーと合わずに編集できないときは空を返す。
// 列が足りない行の最終フィールドは訳ではなく別の列（speaker など）なので、
// それを訳として返すと画面の訳欄に無関係な値が並ぶ。書き込み側は同じ理由で
// [File.SetTranslation] が止めているので、読み出し側もそろえる。
// 行の中身は [Line.Text] で生のまま見られる。
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

// File は1ファイル分の編集モデル。並行に使ってはいけない。
type File struct {
	path string
	// bom は先頭にあった UTF-8 BOM（無ければ空）。書き出しで先頭に戻す。
	bom string
	// header は受理されたヘッダー。読み取り専用で開いたときは nil。
	header []string
	lines  []Line
	// version は読み込んだ（または最後に保存した）バイト列全体の SHA-256。
	version        string
	dirty          bool
	readOnly       bool
	readOnlyReason string
	// readOnlyCause は readOnlyReason と同じ理由を、識別子と置換の組で持つ。
	// 画面が目録から訳された文面を引くための鍵になる。
	readOnlyCause reason.Reason
}

// Parse はバイト列を編集モデルにする。壊れた入力でも誤りは返さない。
// ヘッダーが受理できないファイルは読み取り専用の [File] になる
// （[File.ReadOnly] と [File.ReadOnlyReason] を見ること）。
//
// 保存したいなら [Open] を使う。Parse で作った File は保存先を持たない。
func Parse(data []byte) *File {
	f := &File{version: hashBytes(data)}

	// BOM は csvfile.SplitPythonLines が落とすので、ここで覚えておく。
	// 戻さないと BOM 付きファイルの1行目が変わってしまう。
	if trimmed := csvfile.TrimBOM(data); len(trimmed) != len(data) {
		f.bom = string(data[:len(data)-len(trimmed)])
	}

	// 1周目: 行の種類を決める。ヘッダーは「最初に現れたコメントでも
	// 空行相当でもない行」なので、これが決まるまで列数が分からない。
	headerIndex := -1
	for _, pl := range csvfile.SplitPythonLines(data) {
		body, _ := splitTerminator(pl.Text)
		line := Line{Number: pl.Number, Text: pl.Text}
		switch {
		case strings.HasPrefix(body, "#"):
			// 生の先頭1文字だけを見る前方一致。トリムしないので " #x" はデータ行。
			// csvfile.KeepContentLines と同じ判定にそろえてある。
			line.Kind = KindComment
		case !isRecord(body):
			line.Kind = KindBlank
		case headerIndex < 0:
			line.Kind = KindHeader
			headerIndex = len(f.lines)
		default:
			line.Kind = KindData
		}
		f.lines = append(f.lines, line)
	}

	if headerIndex < 0 {
		f.markReadOnly(reason.New(reason.EditNoHeader,
			"ヘッダー行が無い（空のファイルか、コメントと空行だけのファイル）"))
		return f
	}

	headerBody, _ := splitTerminator(f.lines[headerIndex].Text)
	header := matchHeader(headerBody)
	if header == nil {
		// ヘッダー行そのものは %q で引用してから渡す。引用を目録の側にやらせると、
		// 言語ごとに引用符が変わり、同じファイルの同じ行が別の綴りで出る。
		f.markReadOnly(reason.New(reason.EditBadHeader, fmt.Sprintf(
			"%d行目のヘッダーが %q で、受理される4種のいずれでもない（"+
				"key,section,node,order,speaker,translation / key,speaker,translation / key,translation / "+
				"key,section,node,order,speaker,source_en,translation）",
			f.lines[headerIndex].Number, headerBody),
			"line", strconv.Itoa(f.lines[headerIndex].Number),
			"text", fmt.Sprintf("%q", headerBody)))
		return f
	}
	f.header = header
	f.lines[headerIndex].Fields = slices.Clone(header)

	// 2周目: データ行のフィールドと編集可否を決める。
	for i := range f.lines {
		if f.lines[i].Kind == KindData {
			f.refresh(i)
		}
	}
	return f
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

// Lines はすべての物理行を複製して返す。
func (f *File) Lines() []Line {
	out := make([]Line, len(f.lines))
	for i, line := range f.lines {
		line.Fields = slices.Clone(line.Fields)
		out[i] = line
	}
	return out
}

// Line は物理行番号で1行を引く。無ければ第2戻り値が false。
func (f *File) Line(number int) (Line, bool) {
	i, ok := f.indexOf(number)
	if !ok {
		return Line{}, false
	}
	line := f.lines[i]
	line.Fields = slices.Clone(line.Fields)
	return line, true
}

// SetTranslation は number 行の最終フィールド（訳）を value に差し替える。
//
// 書き換えるのは最終フィールドの開始位置から行末（改行の手前）までだけで、
// 他の行にも、この行の前半にも触れない。改行文字はその行が元々持っていた
// 種類（CRLF / LF / CR / 無し）のまま残る。
//
// 差し替えた結果がいまの行と1バイトも変わらないなら、何もせず nil を返す
// （[File.Dirty] も立たない）。
//
// 誤りを返す場合:
//
//   - ファイル全体が読み取り専用: [ErrReadOnly]
//   - 行が無い / データ行でない / 列数がヘッダーと合わない: [NotEditableError]
//   - value に CR か LF が入っている: [InvalidValueError]
//
// value の CR / LF を拒むのは、公開ファイルの読み手（ConvertFrom-Csv の移植である
// csvfile.ReadPowerShellRows）が1物理行=1レコードで読むためである。引用符で
// 囲っても改行をまたぐ値は別レコードへ割れて壊れる。書けない値は書かせない。
func (f *File) SetTranslation(number int, value string) error {
	if f.readOnly {
		return fmt.Errorf("%w: %s", ErrReadOnly, f.readOnlyReason)
	}
	i, ok := f.indexOf(number)
	if !ok {
		return notEditable(number, reason.New(reason.EditNoSuchLine, "そんな行番号は無い"))
	}
	line := &f.lines[i]
	if line.Kind != KindData {
		// 種類の名前（comment / blank / header / data）は ASCII のまま渡す。
		// ファイルの見え方を指す語で、[Kind.String] と doc コメントが同じ綴りを
		// 使っている。訳すと、画面と説明が別の語で同じものを指すことになる。
		return notEditable(number, reason.New(reason.EditNotDataLine,
			"データ行ではない（"+line.Kind.String()+"）", "kind", line.Kind.String()))
	}
	if !line.Editable {
		return notEditable(number, line.Cause)
	}
	if strings.ContainsAny(value, "\r\n") {
		return invalidValue(number, reason.New(reason.EditNoNewline, "訳に改行は入れられない"))
	}
	if strings.ContainsRune(value, 0) {
		// NUL は Python の csv.reader が _csv.Error にする値だが、
		// internal/validate はその再現をしていない。ここで止めないと
		// 誰も気づかないまま公開ファイルまで届く。
		return invalidValue(number, reason.New(reason.EditNoNUL, "訳に NUL は入れられない"))
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
		return invalidValue(number, reason.New(reason.EditBadUTF8, "訳が正しいUTF-8ではない"))
	}

	body, term := splitTerminator(line.Text)
	offsets := csvfile.FieldOffsets(body)
	// 最終フィールドの開始位置から行末まで。ここより前は1バイトも触らない。
	start := offsets[len(offsets)-1]
	text := body[:start] + escapeTranslation(value) + term
	if text == line.Text {
		return nil
	}
	line.Text = text
	// 書き戻した行を読み直してモデルを更新する。escapeTranslation のおかげで
	// 読み戻した値は書いた値と一致するので、画面とファイルとゲームの3つが
	// 同じ値を指す。
	f.refresh(i)
	f.dirty = true
	return nil
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
//	publish       ConvertFrom-Csv（移植は csvfile.ParsePowerShellRecord）。
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

// refresh は i 番目のデータ行のフィールドと編集可否を、生テキストから求め直す。
func (f *File) refresh(i int) {
	line := &f.lines[i]
	body, _ := splitTerminator(line.Text)

	// 種類も数え直す。訳を消した結果その行が空行相当（"," など）になることがあり、
	// Kind を据え置くと「モデルはデータ行、読み直すと空行」という食い違いが残る。
	// 2列のヘッダーでキーが空の行で起きる。
	if !isRecord(body) {
		line.Kind = KindBlank
		line.Fields = nil
		line.Editable = false
		line.setReason(reason.New(reason.EditNotRecord, "この行はレコードとして読まれない"))
		return
	}
	line.Kind = KindData

	// 区切りの数は FieldOffsets で数える。ParsePowerShellRecord は末尾の空
	// フィールドを落とすので、未訳行 `a,b,` を2列と数えてしまう。
	offsets := csvfile.FieldOffsets(body)
	fields, _ := csvfile.ParsePowerShellRecord(body)
	for len(fields) < len(offsets) {
		fields = append(fields, "")
	}
	line.Fields = fields

	if len(offsets) != len(f.header) {
		// 安全弁。列が多い行を最終フィールドの位置で切ると、余った列を巻き込んで
		// 壊す。列が少ない行は別の列を訳だと思って書き換える。どちらも直せない
		// 壊し方なので、編集させずに翻訳者へ見せる。
		line.Editable = false
		line.setReason(reason.New(reason.EditFieldCount,
			fmt.Sprintf("フィールド数がヘッダーと合わない（ヘッダーは%d列、この行は%d列）",
				len(f.header), len(offsets)),
			"header", strconv.Itoa(len(f.header)), "row", strconv.Itoa(len(offsets))))
		return
	}
	line.Editable = true
	line.setReason(reason.Reason{})
}

// indexOf は物理行番号から f.lines の添字を引く。
// 行番号は1始まりの連番なので添字は number-1 だが、入力が壊れていても
// 落ちないように範囲と実際の番号を確かめる。
func (f *File) indexOf(number int) (int, bool) {
	i := number - 1
	if i < 0 || i >= len(f.lines) || f.lines[i].Number != number {
		return 0, false
	}
	return i, true
}

// markReadOnly はファイル全体を読み取り専用にする。
// データ行の Editable は false のまま、理由だけ入れておく。
func (f *File) markReadOnly(why reason.Reason) {
	f.readOnly = true
	f.readOnlyReason = why.Text
	f.readOnlyCause = why
	for i := range f.lines {
		if f.lines[i].Kind == KindData {
			f.lines[i].setReason(why)
		}
	}
}

// isRecord は行本体（改行を除いたもの）がレコードになるかを返す。
// 空行相当の判定は csvfile.ParsePowerShellRecord の第2戻り値をそのまま使う。
//
// csvfile.ReadPowerShellRows はヘッダーを探すときだけ別の判定を使う。飛ばすのは
// 空行と空白だけの行（.NET の Trim で空になる行）で、"," や '""' はヘッダーとして
// 採る（上流の hash-strings.ps1 の Remove-NonRecords に合わせたもの）。ここでは
// ヘッダーの前後で判定を変えず、一貫してこちらを使う。差が出るのはヘッダーの前に
// "," や '""' の行があるファイルと、全角空白だけの行があるファイルだけである。
// 前者ではこちらが次の行をヘッダーとして探しにいき、後者ではこちらが全角空白の行を
// ヘッダーにして読み取り専用で開く。どちらも実データには無い形。
func isRecord(body string) bool {
	_, ok := csvfile.ParsePowerShellRecord(body)
	return ok
}

// splitTerminator は行を本体と行末の改行に分ける。
// csvfile.SplitPythonLines が返す行は "\r\n" / "\n" / "\r" のいずれかで終わるか、
// 末尾行なら終端を持たない。
func splitTerminator(text string) (body, term string) {
	switch {
	case strings.HasSuffix(text, "\r\n"):
		return text[:len(text)-2], "\r\n"
	case strings.HasSuffix(text, "\n"), strings.HasSuffix(text, "\r"):
		return text[:len(text)-1], text[len(text)-1:]
	}
	return text, ""
}

// hashBytes は版を計算する。バイト列全体の SHA-256 の16進。
// BOM も改行も含めた全バイトが対象なので、1バイトでも違えば別の版になる。
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
