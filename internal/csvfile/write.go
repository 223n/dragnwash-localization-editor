package csvfile

import (
	"io"
	"strings"
)

// LineTerminator は書き出しの改行。常に LF。
//
// 元実装の PowerShell は StringBuilder.AppendLine（= Environment.NewLine）を使うので
// Windows で走らせれば CRLF を書くが、リポジトリに入っている5ファイルはいずれも
// CR を1バイトも含まない。core.autocrlf=input により index でも作業ツリーでも LF で、
// CRLF は実行直後の一時的な状態にすぎない（移植仕様「公開CSV生成 / 敵対検証」
// [medium] R27、「実データの形式 R1」）。環境非依存にするため LF 固定とする。
const LineTerminator = "\n"

// escapeTargets は引用符で囲むかどうかを決める文字の集合。
//
// カンマ・二重引用符・CR・LF の4種だけ。'#'、タブ、セミコロン、前後の空白は
// 対象外である点に注意（移植仕様 R3 / R14）。
const escapeTargets = ",\"\r\n"

// EscapeField は1フィールドを書き出し用にエスケープする。
//
// 規則（移植仕様「公開CSV生成 R3」/「CSVとキー生成 R14」）: 値に
// カンマ・二重引用符・CR・LF のいずれかを含むときだけ全体を二重引用符で囲み、
// 内部の " を "" に倍化する。それ以外は素通し。空文字は空文字のまま。
//
// Go の csv.Writer は「先頭ルーンが空白なら引用」「フィールドが `\.` なら引用」
// という規則を追加で持つため出力が一致しない。使ってはいけない。
//
// 既知の非対称: '#' を引用しないので、'#' で始まる値を先頭列に書くと、読み戻し
// 時にコメント行として丸ごと消える。元実装もそうなっており、直すと既存の公開
// ファイルとバイト非互換になるため、ここでは元実装の挙動を保つ。
func EscapeField(v string) string {
	if !strings.ContainsAny(v, escapeTargets) {
		return v
	}
	return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
}

// JoinFields は各フィールドを [EscapeField] に通してカンマで連結する。改行は付けない。
func JoinFields(fields ...string) string {
	var b strings.Builder
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(EscapeField(f))
	}
	return b.String()
}

// AppendLine は1行ぶん（エスケープ済みのフィールド列と LF）を dst の末尾に足す。
//
// 元実装の StringBuilder.AppendLine に対応する。改行は常に LF
// （[LineTerminator] のコメント参照）。
func AppendLine(dst []byte, fields ...string) []byte {
	for i, f := range fields {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = append(dst, EscapeField(f)...)
	}
	return append(dst, LineTerminator...)
}

// WriteLine は1行ぶんを w へ書く。
func WriteLine(w io.Writer, fields ...string) error {
	_, err := io.WriteString(w, JoinFields(fields...)+LineTerminator)
	return err
}
