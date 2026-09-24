package validate

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
)

// acceptedHeaders は受理されるヘッダー3種。先頭が現行の形で、残り2つは古い形。
//
// 規則（移植仕様「形式検証 R12」）: 比較はリストの完全一致。要素数・順序・
// 各文字列が1バイトでも違えば不一致とする。トリムも大文字小文字の正規化もしない。
// BOM は csvfile が先に剥がしているので "U+FEFF + key" にはならない。
//
// 「英語原文の列が混ざっていないこと」を見る専用の検査は元実装に無い。
// source_en 列のあるファイルは、この完全一致に落ちて R13 のヘッダー不正になる。
// つまり原文混入の検出はヘッダー検査に丸ごと乗っている。
// 逆に、受理されるヘッダーのまま speaker 列や translation 列に英文を入れた
// ファイルは検出できない。元実装がそうなので、ここでも増やさない。
var acceptedHeaders = [][]string{
	{"key", "section", "node", "order", "speaker", "translation"},
	{"key", "speaker", "translation"},
	{"key", "translation"},
}

// AcceptedHeaders は受理されるヘッダー3種を複製して返す。
// 先頭が公開ファイルの現行の形（key,section,node,order,speaker,translation）。
func AcceptedHeaders() [][]string {
	out := make([][]string, 0, len(acceptedHeaders))
	for _, h := range acceptedHeaders {
		out = append(out, slices.Clone(h))
	}
	return out
}

// CheckFile は公開ファイル1つ分のバイト列を検査し、見つかった問題を返す。
// 問題が無ければ長さ0のスライスを返す。
//
// name は報告に出す表示用のパス。[CheckTree] はリポジトリ相対のスラッシュ区切りを
// 渡すが、この関数自体は中身を見ないので、画面に出したい名前を何でも渡してよい。
//
// data はファイルの生のバイト列。先頭の UTF-8 BOM は csvfile が剥がす。
// 不正なUTF-8が混ざっていてもエラーにはしない（元実装は UnicodeDecodeError で
// 異常終了する。パッケージコメント「元実装と意図的に変えたところ」参照）。
//
// 検査の順序は上流 dev の check_file のまま（移植仕様「形式検証 R11〜R21」）:
//
//  1. CSV として読めなければ（フィールドが長すぎる）、"could not be parsed as CSV"
//     1件だけを返す。
//  2. コメントと空行を除いたレコードが1つも無ければ "empty file" 1件だけを返す。
//  3. ヘッダーが3種のいずれでもなければ、その1件だけを返して行の検査はしない。
//  4. 以降、各行について「フィールド数 → キーの形 → 重複 → 空の訳 →
//     section → node」の順に見る。フィールド数が合わない行は残りを飛ばす。
func CheckFile(name string, data []byte) []Problem {
	problems := []Problem{}

	// 物理行番号を覚えたまま、コメント行と空行を除いたレコードにする。
	// 報告に出る行番号はすべてこの対応付けから来る（移植仕様 R9 / R10）。
	records, err := csvfile.ReadPythonRecords(data)
	if err != nil {
		// 上流（f816618）は csv.Error を捕まえて、この1件だけを返す。途中までに
		// 読めた行の問題も出さない。行番号は reader.line_num で、長すぎる
		// フィールドが複数行にまたがるときは、超えた文字のある行を指す。
		line, message := 0, err.Error()
		var perr *csvfile.PythonParseError
		if errors.As(err, &perr) {
			line, message = perr.Line, perr.Message
		}
		return append(problems, Problem{
			Path: name, Line: line,
			Message: "could not be parsed as CSV (" + message + ")",
		})
	}
	if len(records) == 0 {
		// 規則（R11）: サイズ0のファイルだけでなく、全行がコメントか完全な空行の
		// ファイルもここに来る。行番号は付かない。空白だけの行はレコードとして
		// 残るので、ここではなく次のヘッダー不正になる（上流 f816618）。
		return append(problems, Problem{Path: name, Message: "empty file"})
	}

	header := records[0].Fields
	if !slices.ContainsFunc(acceptedHeaders, func(h []string) bool { return slices.Equal(h, header) }) {
		// 規則（R13）: ヘッダーが違うファイルは、行を1つも見ずに即座に返す。
		// ヘッダーがずれていれば列の意味も行番号の意味も当てにならないため。
		return append(problems, Problem{
			Path: name,
			Message: fmt.Sprintf(
				"header is %s; the published file must be "+
					"'key,section,node,order,speaker,translation' (run tools/hash-strings.ps1 before committing)",
				pythonRepr(header)),
		})
	}

	// 列名→添字。section と node の有無を見るためだけに使う（R14）。
	col := make(map[string]int, len(header))
	for i, column := range header {
		col[column] = i
	}
	width := len(header)

	// seen はキー→最初に現れた報告用行番号。ファイル単位で持つので、
	// ロケールをまたいだ同じキーは重複として扱わない（移植仕様
	// 「抽出が取りこぼしていた規則」）。照合は正規化なしの完全一致で、
	// 台詞IDは大文字を許すため line:Abc と line:abc は別のキーになる。
	seen := make(map[string]int)

	// ヘッダー行そのものは検査しない。元実装もヘッダーを読んだあとの
	// ループでしか検査せず、seen にも登録しない。
	for _, record := range records[1:] {
		n := record.Number

		if len(record.Fields) != width {
			// 規則（R16）: 列数が合わない行は、以降の検査をすべて飛ばす。
			// seen にも登録しないので、この行のキーは後続行の重複判定の
			// 基準にもならない。
			problems = append(problems, Problem{
				Path: name, Line: n,
				Message: fmt.Sprintf("expected %d fields, got %d", width, len(record.Fields)),
			})
			continue
		}

		// 規則（R17）: 列名ではなく位置で取る。ヘッダー3種のいずれでも
		// key は先頭、translation は最終列。
		k := record.Fields[0]
		translation := record.Fields[len(record.Fields)-1]

		// 規則（R18）: 16桁の小文字16進か台詞IDのどちらか。
		// キーの値はメッセージに出さない（パッケージコメント参照）。
		//
		// 元実装より厳しい点が1つある。Python の '$' は文字列末尾に加えて
		// 「末尾の改行1個の手前」にも当たるので、引用フィールドが複数行に
		// またがって key が "185f8db32271fe25" + LF になったファイルを
		// check-translations.py は通す。ここでは通さない。理由は
		// [looksLikeIdentifier] と同じで、改行を許すと CRLF と LF で判定が
		// 割れるため、どちらでも落ちる側にそろえてある。見逃しではなく
		// 過検出の方向にだけずれる。
		if !key.LooksLike(k) && !key.LooksLikeLineID(k) {
			problems = append(problems, Problem{
				Path: name, Line: n,
				Message: "key is not 16 lowercase hex digits or a line ID",
			})
		}

		// 規則（R19）: 参照先は常に「1回目に現れた行」。3回目の重複も
		// 2回目ではなく1回目を指す。キーの形が不正でも登録するので、
		// 不正なキーの重複も見つかる。
		if first, ok := seen[k]; ok {
			problems = append(problems, Problem{
				Path: name, Line: n,
				Message: fmt.Sprintf("duplicate key (see line %d)", first),
			})
		} else {
			seen[k] = n
		}

		// 規則（R20）: 空白だけの訳も空とみなす。値そのものは書き換えない。
		if csvfile.IsPythonBlank(translation) {
			problems = append(problems, Problem{Path: name, Line: n, Message: "empty translation"})
		}

		// 規則（R21）: section と node はゲーム内部の識別子で、文になることはない。
		// 列があるときだけ見るので、3列・2列のヘッダーでは発動しない。
		// order 列と speaker 列はどのヘッダーでも一切検査されない。
		for _, column := range []string{"section", "node"} {
			i, ok := col[column]
			if !ok {
				continue
			}
			if !looksLikeIdentifier(record.Fields[i]) {
				problems = append(problems, Problem{
					Path: name, Line: n,
					Message: column + " does not look like an identifier",
				})
			}
		}
	}
	return problems
}

// looksLikeIdentifier は section / node の値が識別子らしいかを返す。
//
// 規則（移植仕様「形式検証 R21」）: 元実装の正規表現は
// `^(?:[A-Za-z0-9_]*|L\d\d [A-Za-z]+|UI)$`。3つめの "UI" は1つめに含まれるので
// 冗長だが、元実装に合わせて意味を変えない。1つめは空文字も通す
// （実データに node と order が空の UI 行がある）。
//
// 正規表現を使わずに書いてあるのは、Python と Go で '$' の意味が違うため。
// Python の '$' は文字列末尾だけでなく「末尾の改行1個の手前」にも当たるので、
// 元実装では "UI\n" が通る。ここでは通さない。
//
// この厳格化には理由がある。Go の encoding/csv は引用フィールド内の "\r\n" を
// "\n" に潰すが csvfile は潰さない。潰さないまま '$' だけを忠実に真似ると、
// CRLFのファイルで元実装が「識別子でない」と言う値をこちらは通してしまい、
// 判定が逆転する（移植仕様「敵対検証」[low]）。改行を許さない側にそろえれば
// CRLFでもLFでも不一致になり、元実装より厳しい方向にだけずれる。
// 値に改行が入るのは引用フィールドが複数行にまたがるときだけで、
// 実データの13ロケールには1件も無い。
func looksLikeIdentifier(v string) bool {
	if isWordString(v) {
		return true
	}
	return isLevelSection(v)
}

// isWordString は `[A-Za-z0-9_]*` に当たるかを返す。空文字は true。
// バイト単位で見る。非ASCIIは先頭バイトが 0x80 以上なのでどのみち外れる。
func isWordString(v string) bool {
	for i := 0; i < len(v); i++ {
		c := v[i]
		ok := (c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			c == '_'
		if !ok {
			return false
		}
	}
	return true
}

// isLevelSection は `L\d\d [A-Za-z]+` に当たるかを返す。
// 'L' + 数字ちょうど2桁 + 半角スペース1個 + ASCII英字1文字以上、で終わり。
// 実データの "L01 Ryan" や "L15 Alexander" がこれに当たる。
// 区切りはスペースちょうど1個なので "L01  Ryan" は外れ、語は1つだけなので
// "L01 Ryan Extra" も外れる。
//
// 数字の判定に unicode.IsDigit を使うのは、Python の `\d` が str パターンでは
// Unicode の十進数字（カテゴリ Nd）すべてに当たるため。実測で
// `IDENT.match("L٠١ Ryan")`（アラビア数字）は True になる。ASCII限定が意図
// だったかは元コードから読み取れないので、判定を変えずに写す方を採った。
func isLevelSection(v string) bool {
	rest, ok := strings.CutPrefix(v, "L")
	if !ok {
		return false
	}
	for range 2 {
		r, size := utf8.DecodeRuneInString(rest)
		if size == 0 || !unicode.IsDigit(r) {
			return false
		}
		rest = rest[size:]
	}
	rest, ok = strings.CutPrefix(rest, " ")
	if !ok || rest == "" {
		return false
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}
