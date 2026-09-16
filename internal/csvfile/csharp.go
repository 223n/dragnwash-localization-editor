package csvfile

import "strings"

// ParseCSharpRecords は src/DragNWashLocalization/CsvReader.cs の Parse を移植した
// 状態機械で、テキスト全体をレコードの並びに分ける。エラーは返さない。
//
// 判定順序が挙動そのものなので、元コードの順番をそのまま保つ（移植仕様 R3）。
//
//	(1) 引用の中なら、そこで処理を終える
//	(2) レコード先頭の '#' ならコメントとして行末まで読み飛ばす
//	(3) '"' / ',' / '\r' / '\n' / それ以外 で分岐する
//
// 各分岐の規則:
//
//   - R4 引用の中では '"' 以外はすべてデータ。'#'、','、CR、LF も値に入る。
//     引用中の "" はリテラルの " 1個、単独の " は引用の終了（その " は値に入らない）。
//     引用内の CRLF は "\r\n" の2文字として値に残る。
//   - R5 '#' がコメントになるのは「引用の外」かつ「このレコードでまだ1つも
//     フィールドを確定していない」かつ「現在のフィールドが空」のときだけ。
//     `a,#x` の '#' も ` #x` の '#' もただの文字。読み飛ばす範囲は次の LF まで
//     （その LF も消費する）。引用を一切考慮しないので、`#"a<改行>b"` のような行では
//     引用内の改行でスキップが終わり、残りが次のレコードの先頭になる。
//   - R6 引用の外の '"' はフィールドの途中でも引用開始になり、その '"' は値に入らない。
//     `ab"cd"ef` は abcdef に、`"abc"def` は abcdef になる。エラーにはならない。
//   - R7 引用の外の ',' は区切り。空でも1フィールドとして確定する。
//   - R8 引用の外の '\r' は位置を問わず捨てる。`a\rb` は ab になる。
//     '\r' はフィールド長を増やさないので、コメント判定や空行判定の「空」条件を壊さない。
//   - R9 引用の外の '\n' はレコード終端。ただしフィールドが1つも無く現在のフィールドも
//     空なら（＝空行）レコードを作らずに読み飛ばす。したがって ',' だけの行は
//     ["", ""] の2列レコードになり、`""` だけの行は空行として捨てられる。
//   - R11 走査後、フィールドが残っているか確定済みフィールドがあるときだけ末尾
//     レコードを1件足す。末尾が改行のファイルで余分な空レコードは作らない。
//     引用が閉じないまま終わっても例外にせず、そこまでを最終フィールドにする。
//
// 走査はバイト単位で行う。分岐対象がすべて ASCII で、UTF-8 の多バイト文字は
// 後続バイトが 0x80 以上のため default 節でそのまま積まれる。正しい UTF-8 で
// あるかぎり C# の char 単位の走査と同じ結果になる。
func ParseCSharpRecords(text string) [][]string {
	var records [][]string
	var fields []string
	var field strings.Builder

	inQuotes := false
	i := 0
	n := len(text)

	for i < n {
		c := text[i]

		if inQuotes {
			if c == '"' {
				if i+1 < n && text[i+1] == '"' {
					field.WriteByte('"')
					i += 2
					continue
				}
				inQuotes = false
				i++
				continue
			}
			field.WriteByte(c)
			i++
			continue
		}

		// レコードの先頭にある '#' だけがコメント（公開ファイルのセクション見出し）。
		if c == '#' && len(fields) == 0 && field.Len() == 0 {
			for i < n && text[i] != '\n' {
				i++
			}
			i++
			continue
		}

		switch c {
		case '"':
			inQuotes = true
			i++
		case ',':
			fields = append(fields, field.String())
			field.Reset()
			i++
		case '\r':
			i++
		case '\n':
			// 空行はレコードにしない（公開ファイルは見出しの間隔あけに使っている）。
			if len(fields) == 0 && field.Len() == 0 {
				i++
				break
			}
			fields = append(fields, field.String())
			field.Reset()
			records = append(records, fields)
			fields = nil
			i++
		default:
			field.WriteByte(c)
			i++
		}
	}

	if field.Len() > 0 || len(fields) > 0 {
		fields = append(fields, field.String())
		records = append(records, fields)
	}

	return records
}

// ReadCSharpRows は CsvReader.ReadRows を移植したもので、ゲーム内Modと同じ
// 読み方で行を返す。エラーは返さない（元実装にも try/catch は無く、CSVの中身が
// どれだけ壊れていても例外を投げない）。
//
// レコードが0件なら行も0件。先頭レコードがヘッダーになるので、ヘッダー行しか
// ないファイルも行0件（移植仕様 R12）。コメント行と空行はレコードにならないため、
// ファイル冒頭にコメントや空行があっても、その次の実データ行がヘッダーになる。
// ヘッダー名はトリムも正規化もしない（引用解除だけは行う）。
func ReadCSharpRows(data []byte) []Row {
	records := ParseCSharpRecords(TrimBOMString(string(data)))
	if len(records) == 0 {
		return nil
	}
	header := records[0]
	rows := make([]Row, 0, len(records)-1)
	for _, record := range records[1:] {
		rows = append(rows, NewRow(header, record))
	}
	return rows
}
