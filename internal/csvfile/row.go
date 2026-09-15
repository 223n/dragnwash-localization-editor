package csvfile

// Row はヘッダーと対応づけた1行。
//
// 元実装は C# 側が Dictionary(StringComparer.OrdinalIgnoreCase)、PowerShell 側が
// PSObject のプロパティで、どちらも列名の照合が大文字小文字を区別しない。
// Go には大小無視のマップが無いので、キーを [FoldASCII] で正規化して持つ。
// 厳密一致で実装すると、ヘッダーが Key,Source_EN,Translation のファイルで全行が
// key="" / source_en="" 扱いになり、出力が黙って全損する
// （移植仕様「公開CSV生成 / 敵対検証」[high]）。
//
// 保持する列はヘッダーにある列だけ。行のフィールドがヘッダーより多くても、
// 余った分は捨てる。少なければ空文字で埋める。したがって「その列が無い」は
// ヘッダーに無い場合だけを指し、行の長さには依存しない（移植仕様 R13）。
type Row struct {
	// columns はヘッダーの綴りを初出順に持つ。大文字小文字だけが違う同名列は
	// 初出の綴りだけを残す（C# のインデクサ代入が既存キーの綴りを保つのに合わせる）。
	columns []string
	// values は正規化した列名から値への対応。
	values map[string]string
}

// NewRow はヘッダーと1レコードから [Row] を作る。
//
// 同名の列（大文字小文字違いを含む）がヘッダーにあるとき、値は後の列が勝ち、
// 綴りは初出が残る。元実装 CsvReader.cs:35 の dict[header[c]] = ... が
// インデクサ代入であることに由来する（移植仕様「抽出が取りこぼしていた規則」）。
// ヘッダー名が空文字でも列として成立する。
func NewRow(header, record []string) Row {
	row := Row{
		columns: make([]string, 0, len(header)),
		values:  make(map[string]string, len(header)),
	}
	for i, name := range header {
		value := ""
		if i < len(record) {
			value = record[i]
		}
		folded := FoldASCII(name)
		if _, dup := row.values[folded]; !dup {
			row.columns = append(row.columns, name)
		}
		row.values[folded] = value
	}
	return row
}

// Lookup は列の値と「その列がヘッダーにあるか」を返す。
//
// 欠損と空文字を区別できる唯一のアクセサなので、こちらを第一に使う。
// 元実装の C# 側は row.TryGetValue(col, out v) が false のとき変数が null になり、
// 呼び出し側（TranslationStore.cs の ResolveKey）はその null を key?.Trim() の形で
// そのまま流して「キー無し」に落とす。一方 Go で欠損を空文字に潰すと、空文字が
// SHA-256("") のハッシュ e3b0c44298fc1c14 という「本物に見えるキー」に化ける
// （移植仕様「CSVとキー生成 / 敵対検証」[medium] R15、internal/key の HashOfEmpty）。
// 欠損を空文字にしてハッシュ関数へ渡さないこと。
func (r Row) Lookup(name string) (string, bool) {
	v, ok := r.values[FoldASCII(name)]
	return v, ok
}

// Get は列の値を返す。ヘッダーに無い列は空文字を返す。
//
// 欠損を空文字に潰してよい場面だけで使う。元実装にも同じ潰し方をしている箇所が
// あり、ScriptOrder.cs:283 は row.TryGetValue(col, out v) ? v ?? "" : "" と書いている。
func (r Row) Get(name string) string {
	return r.values[FoldASCII(name)]
}

// Has は列がヘッダーにあるかを返す。
//
// PowerShell 側の $r.PSObject.Properties['source_en'] による存在判定に対応する。
// 行ごとの列数には依存せず、ヘッダーにあるかどうかだけを見る。
func (r Row) Has(name string) bool {
	_, ok := r.values[FoldASCII(name)]
	return ok
}

// Columns はヘッダーの綴りを初出順に返す。呼び出し側が書き換えても
// [Row] には影響しない複製を返す。
func (r Row) Columns() []string {
	out := make([]string, len(r.columns))
	copy(out, r.columns)
	return out
}

// Len は列の数を返す。
func (r Row) Len() int {
	return len(r.columns)
}
