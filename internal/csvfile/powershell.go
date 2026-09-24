package csvfile

import (
	"fmt"
	"strings"
)

// DuplicateColumnError は入力CSVのヘッダーに同名の列が複数あることを表す。
//
// 元実装の ConvertFrom-Csv は「メンバー "a" は既に存在します」という例外を投げ、
// tools/hash-strings.ps1 は $ErrorActionPreference = 'Stop' を設定しているため、
// そこで処理全体が止まる（移植仕様「公開CSV生成 / 敵対検証」[low]）。
// 同名かどうかの判定は大文字小文字を区別しない。空の列名は重複に数えない
// （元実装は既定名 H1, H2 ... を振るため）。
type DuplicateColumnError struct {
	// Name は重複した列名。2つ目に現れた側の綴り。
	Name string
}

func (e *DuplicateColumnError) Error() string {
	return fmt.Sprintf("ヘッダーの列名 %q が重複している", e.Name)
}

// ReadPowerShellRows は tools/hash-strings.ps1 の Read-Csv を移植したもので、
// 公開CSVの生成と同じ読み方で行を返す。
//
// 手順（移植仕様「公開CSV生成 R4」）:
//
//	(1) BOM を剥がして物理行に分ける（.NET の File.ReadAllLines 相当）
//	(2) 行頭が '#' の行を落とす。トリムはしないので " #x" は残る
//	(3) 残りが2行未満なら0行を返す（エラーにはしない）
//	(4) 先頭の空行と空白だけの行を飛ばし、その次の行がヘッダー
//	(5) 以降の行を1物理行=1レコードとしてパースする
//
// (4) は上流の 55d2e09 / c8fda90 に合わせてある（[ReadPowerShellTable]）。
// それ以外は移植の基準 003ed1e の読み方のままである。
//
// (5) が効くので、引用フィールド内の改行はサポートされない。値に改行を含む行は
// 物理行ごとに別レコードへ割れて壊れる。これは '#' で始まるかどうかに関係なく
// 常に起きる（移植仕様「公開CSV生成 / 敵対検証」[medium] R4）。上流は f816618 で
// 全文を1つの文字列として解釈する読み方へ移ったが、ここは行単位のまま据え置いて
// ある。移すと publish だけでなく、同じ読み方を前提にしている diff・order・edit の
// 結果まで変わるためである。代わりに、行単位では読み違える形のファイルを
// [ReadPowerShellWhole] の結果と突き合わせて見つけ、publish が書く前に止める
// （internal/publish の守り）。
//
// ヘッダー名の重複だけは [DuplicateColumnError] を返す。それ以外の壊れ方
// （列数の過不足、閉じない引用符、裸の二重引用符）はエラーにしない。
func ReadPowerShellRows(data []byte) ([]Row, error) {
	numbered, err := ReadPowerShellRowsNumbered(data)
	if err != nil {
		return nil, err
	}
	if numbered == nil {
		return nil, nil
	}
	rows := make([]Row, len(numbered))
	for i, n := range numbered {
		rows[i] = n.Row
	}
	return rows, nil
}

// NumberedRow は [Row] に、それがどの物理行から来たかを添えたもの。
type NumberedRow struct {
	Row
	// Line は1始まりの物理行番号。[SplitNetLines] が分けた並びでの位置なので、
	// '#' で始まる行も空行も数に入る。人に「何行目か」を伝えるための値である。
	Line int
}

// ReadPowerShellRowsNumbered は [ReadPowerShellRows] と同じ読み方をしたうえで、
// 各行がファイルの何行目から来たかも返す。
//
// 行番号が要るのは、読んだ結果を人へ示す側だけである（internal/publish が
// 「どの行の訳が失われるか」を出すときに使う）。読み方そのものは1つでよいので、
// [ReadPowerShellRows] はこちらへ、こちらは [ReadPowerShellTable] へ委ねてある。
// 別々に書くと、'#' の落とし方やヘッダーの選び方といった規則が散り、片方だけが
// 直る形になる。
func ReadPowerShellRowsNumbered(data []byte) ([]NumberedRow, error) {
	table, err := ReadPowerShellTable(data)
	if err != nil {
		return nil, err
	}
	return table.Rows, nil
}

// PowerShellTable は [ReadPowerShellTable] が読んだ結果。行に加えて、どの行を
// ヘッダーに選んだかを持つ。
type PowerShellTable struct {
	// Header はヘッダーに選んだ行のフィールド。ヘッダーに選べる行が1つも無ければ nil。
	Header []string
	// HeaderLine はヘッダーの1始まりの物理行番号。ヘッダーが無ければ 0。
	HeaderLine int
	// Rows はデータ行。[ReadPowerShellRowsNumbered] が返すものと同じ。
	Rows []NumberedRow
}

// ReadPowerShellTable は [ReadPowerShellRows] と同じ読み方で読み、どの行を
// ヘッダーに選んだかも一緒に返す。
//
// ヘッダーが要るのは internal/publish の守りである。データ行が0件でも
// 「key 列や translation 列を引けるヘッダーか」を確かめたいので、Rows が空に
// なる場合（'#' を除いた残りが1行だけのとき）でも、選べるならヘッダーは埋める。
// そのときは元実装どおり列名の重複を確かめない（行が無ければ ConvertFrom-Csv も
// 例外を出さない）。
func ReadPowerShellTable(data []byte) (PowerShellTable, error) {
	lines := SplitNetLines(TrimBOMString(string(data)))

	// 落とした行があっても元の行番号を言えるように、行と番号を組で持つ。
	type numberedLine struct {
		text   string
		number int
	}
	kept := make([]numberedLine, 0, len(lines))
	for i, line := range lines {
		// 生の先頭1文字だけを見る前方一致。トリムしないので " #x" はデータ行。
		//
		// 元実装の $_.StartsWith('#') は .NET の文字列版 StartsWith で、既定では
		// カルチャ依存の照合になる。pwsh 7.6.6 で実測すると、照合上無視される
		// 文字（U+00AD など）が先頭にあっても真を返す。Go の標準ライブラリだけでは
		// 照合表を持てないため、ここは序数比較で割り切っている。元実装なら
		// コメントとして落ちる行が、こちらではデータ行として残る向きにずれる。
		// 実データの13ロケールには該当する行が1件も無い。
		if strings.HasPrefix(line, "#") {
			continue
		}
		kept = append(kept, numberedLine{text: line, number: i + 1})
	}

	// ヘッダーの前にある空行と空白だけの行を飛ばす。上流の hash-strings.ps1 は
	// 55d2e09 と c8fda90（Remove-NonRecords）で、ヘッダーを選ぶ前にこれらを落とす
	// ようになった。判定は `$body.Trim().Length -eq 0` で、.NET の Trim は
	// Char.IsWhiteSpace の文字を落とす。Go の strings.TrimSpace が使う
	// unicode.IsSpace と同じ集合である（全角空白 U+3000 も落ちる）。
	//
	// 以前は 003ed1e の ConvertFrom-Csv に合わせて完全な空行だけを飛ばしていた。
	// その読み方だと、空白だけの行が1本あるだけで列が0個のヘッダーになり、
	// 本物のヘッダーがデータ行へずれて全行が捨てられる。publish はそれを
	// 終了コード 0 のまま書き出し、ロケールの訳がまるごと消えていた
	// （いまの公開ファイルも同じ読み方で読むので、失われる訳の確かめも素通りした）。
	//
	// "," や '""' の行は、上流でも Trim で空にならないのでヘッダーになる。ここも
	// そのまま採る。データ行に使う空行相当の判定（[ParsePowerShellRecord] の
	// 第2戻り値）は持ち込まない。
	next := 0
	for next < len(kept) && strings.TrimSpace(kept[next].text) == "" {
		next++
	}
	var table PowerShellTable
	if next < len(kept) {
		// 列が0個や、名前の空の列だけのヘッダー（"," など）もそのまま採る。
		// その場合どの列も引けず、全データ行が「列なし」の [Row] になる。
		// 元実装も同じで、空の列名には H1 のような既定名が付くが、名前で引く
		// かぎり結果は変わらない。
		table.Header = parsePowerShellFields(kept[next].text)
		table.HeaderLine = kept[next].number
	}
	// 元実装の `if ($lines.Count -lt 2) { return @() }`。数えるのは空行を落とす前の行数。
	if len(kept) < 2 || next >= len(kept) {
		return table, nil
	}
	if err := checkDuplicateColumns(table.Header); err != nil {
		return PowerShellTable{}, err
	}

	for next++; next < len(kept); next++ {
		fields, ok := ParsePowerShellRecord(kept[next].text)
		if !ok {
			continue
		}
		table.Rows = append(table.Rows, NumberedRow{Row: NewRow(table.Header, fields), Line: kept[next].number})
	}
	return table, nil
}

// checkDuplicateColumns はヘッダーに同名の列（大文字小文字違いを含む）が
// あればエラーを返す。
//
// 空の列名は数えない。ConvertFrom-Csv は空の列名に H1, H2 ... の既定名を振って
// 警告を出すだけで、重複としては止まらない（pwsh 7.6.6 で実測）。
// "key,translation,,," のように空の列が2つ以上あるファイルを、元実装は公開できる。
func checkDuplicateColumns(header []string) error {
	seen := make(map[string]struct{}, len(header))
	for _, name := range header {
		if name == "" {
			continue
		}
		folded := FoldASCII(name)
		if _, dup := seen[folded]; dup {
			return &DuplicateColumnError{Name: name}
		}
		seen[folded] = struct{}{}
	}
	return nil
}

// ParsePowerShellRecord は1物理行を ConvertFrom-Csv と同じ規則でフィールドに分ける。
// 2つ目の戻り値は「この行がレコードになるか」で、空行相当なら false を返す。
//
// 挙動は pwsh 7.6.6 で実測した。移植仕様は「引用符なしフィールドは先頭空白を全除去、
// 末尾空白を1個残して除去」とだけ書いているが、実測するともう少し複雑で、
// 途中に空白や二重引用符があると末尾空白は削られない（[trimPowerShellTrailing] 参照）。
//
// レコードにならない行（実測値）:
//
//	""      空行
//	" "     空白だけの行。対象は半角スペースとタブのみで、全角空白はデータになる
//	","     フィールドがすべて空で、末尾フィールドが引用符なし
//	`""`    空の引用フィールド1つだけ
//	`"`     閉じていない引用符だけ
//
// 一方 ",," や `,""` はレコードになる。フィールドが0個または空1個になった行だけが
// 空行として落ちる、という規則で実測値すべてを説明できる。
//
// なお、この判定が当てはまるのはデータ行だけで、ヘッダー行には当てはまらない
// （[ReadPowerShellRows] を参照）。
func ParsePowerShellRecord(line string) ([]string, bool) {
	fields := parsePowerShellFields(line)
	if len(fields) == 0 || (len(fields) == 1 && fields[0] == "") {
		return nil, false
	}
	return fields, true
}

// parsePowerShellFields は1物理行をフィールドへ分ける。空行相当かどうかは見ない。
//
// 行末のフィールドは「値が空、かつ引用符で閉じられていない」ときだけ捨てる
// （pwsh 7.6.6 で実測）:
//
//	`b,"`    -> [b]        開いたままの引用符で中身が空。捨てる
//	`b,""`   -> [b, ""]    閉じた引用フィールド。空でも残る
//	`b,"x`   -> [b, x]     開いたままだが中身がある。残る
//	"b,"     -> [b]        引用符なしで空。捨てる
func parsePowerShellFields(line string) []string {
	var fields []string
	i := 0
	for {
		f := parsePowerShellField(line, i, false)
		if f.end >= len(line) {
			if f.value != "" || f.closed {
				fields = append(fields, f.value)
			}
			break
		}
		// end は区切りのカンマの位置。
		fields = append(fields, f.value)
		i = f.end + 1
	}
	return fields
}

// psField は [parsePowerShellField] が読んだ1フィールド。
type psField struct {
	// value はフィールドの値。
	value string
	// quoted は引用符で始まったかどうか（先頭の空白は数えない）。
	quoted bool
	// closed は引用符で始まり、対応する閉じ引用符も見つかったときだけ true。
	// 引用符なしのフィールドでは常に false になる。
	closed bool
	// end は終了位置。区切りのカンマの位置で、無ければ文字列の長さ。
	// wholeText のときは、レコードを終える改行（'\r' か '\n'）の位置のこともある。
	end int
}

// unclosed は、引用符で始まったのに閉じないまま終わったかを返す。
func (f psField) unclosed() bool { return f.quoted && !f.closed }

// parsePowerShellField は s の start から1フィールドを読む。
//
// wholeText が false のとき、s は1物理行（改行を含まない）である。ここまでの
// 読み方（[ParsePowerShellRecord]）はこちらだけを使う。
//
// wholeText が true のときは、s はファイル全体で、引用の外の '\r' と '\n' も
// フィールドを終わらせる（[ReadPowerShellWhole]）。引用の中の改行は値に入る。
// 上流が ConvertFrom-Csv に全文を1つの文字列で渡したときの読み方で、
// pwsh 7.6.6 で次を実測してある。引用の中の LF・CRLF・単独の CR はそのまま値に
// 残る。フィールドの途中の '"' と、閉じ引用符の後ろに続く '"' はただの文字で、
// 引用を開かない。先頭の空白の後ろの '"' は引用を開く。閉じない引用符は
// ファイルの終わりまでを値にする。
//
// 1物理行を渡すかぎり改行は現れないので、どちらでも結果は変わらない。それでも
// 切り替えにしてあるのは、[ParsePowerShellRecord] が公開の関数で、改行を含む
// 文字列を渡されたときの結果をここで変えないためである。
func parsePowerShellField(s string, start int, wholeText bool) psField {
	i := start
	n := len(s)
	stop := func(c byte) bool {
		return c == ',' || (wholeText && (c == '\r' || c == '\n'))
	}

	// 先頭の空白は読み飛ばす。対象は半角スペースとタブだけ。
	for i < n && (s[i] == ' ' || s[i] == '\t') {
		i++
	}

	if i < n && s[i] == '"' {
		f := psField{quoted: true}
		var b strings.Builder
		i++ // 開始の引用符
		for i < n {
			if s[i] == '"' {
				if i+1 < n && s[i+1] == '"' {
					b.WriteByte('"')
					i += 2
					continue
				}
				i++ // 閉じの引用符
				f.closed = true
				break
			}
			b.WriteByte(s[i])
			i++
		}
		// 閉じ引用符より後ろは次のカンマまでそのまま値に足す。`"a"x,b` は ax と b になる。
		// ただし空白しか無いときは丸ごと捨てる（`"q"  ` は q）。
		tailStart := i
		for i < n && !stop(s[i]) {
			i++
		}
		if tail := s[tailStart:i]; strings.TrimRight(tail, " \t") != "" {
			b.WriteString(tail)
		}
		f.value = b.String()
		f.end = i
		return f
	}

	// 引用符なし。二重引用符が途中に出てもただの文字（`he said "hi"` はそのまま）。
	valueStart := i
	for i < n && !stop(s[i]) {
		i++
	}
	return psField{value: trimPowerShellTrailing(s[valueStart:i]), end: i}
}

// trimPowerShellTrailing は引用符なしフィールドの末尾空白を ConvertFrom-Csv と同じ
// 規則で削る。
//
// pwsh 7.6.6 での実測値:
//
//	"x  "    -> "x "     末尾の空白は1文字だけ残る
//	"x    "  -> "x "     何文字あっても1文字
//	"x\t\t"  -> "x\t"    残るのは最初の1文字なのでタブが残る
//	"x y  "  -> "x y  "  途中に空白があると削られない
//	"x  y  " -> "x  y  " 同上
//	`x"  `   -> `x"  `   途中に二重引用符があると削られない
//	"x'y  "  -> "x'y "   引用符でなければ削られる
//
// つまり「最初に現れた空白より後ろがすべて空白で、かつそこまでに二重引用符が
// 無い」ときだけ、その空白1文字を残して切り詰める。先頭の空白は
// [parsePowerShellField] 側で既に落としてある。
func trimPowerShellTrailing(v string) string {
	p := strings.IndexAny(v, " \t")
	if p < 0 {
		return v
	}
	if strings.TrimRight(v[p:], " \t") != "" {
		// 最初の空白より後ろに空白以外が残る。末尾だけの空白ではないので触らない。
		return v
	}
	if strings.IndexByte(v[:p], '"') >= 0 {
		return v
	}
	return v[:p+1]
}
