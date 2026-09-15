package validate

import (
	"strconv"
	"strings"
)

// Problem は見つかった問題1件。
//
// 元実装は整形ずみの文字列1本を積むだけで、種別も重大度も持たない
// （移植仕様「形式検証 / データ構造 / Problem」）。ここでは編集画面から
// 「この行が悪い」と指せるように3つに分けるが、[Problem.String] で組み立てた
// 文字列は元実装の1行と同じになる。
//
// 値そのもの（key や訳文や原文）はどのフィールドにも入れない。
// 報告は公開リポジトリのCIログに出るため（パッケージコメント参照）。
type Problem struct {
	// Path は表示用のパス。リポジトリルートからの相対をスラッシュ区切りにしたもので、
	// ルート配下でなければ絶対パスがそのまま入る（元実装の display()）。
	Path string

	// Line は元ファイルの1始まりの物理行番号。コメント行と空行を除いたあとの
	// 番号ではない。行を特定できない問題（ヘッダー不正など）では0。
	//
	// 引用フィールドが複数行にまたがるレコードでは、その先頭の物理行を指す。
	Line int

	// Message は本文。元実装の文言をそのまま使う。
	Message string
}

// String は元実装が1行として出力する形に組み立てる。
//
// 規則（移植仕様「形式検証 R24」）: 行番号つきは "{表示パス}:{行番号}: {本文}"、
// 行番号なしは "{表示パス}: {本文}"。
func (p Problem) String() string {
	var b strings.Builder
	b.WriteString(p.Path)
	if p.Line > 0 {
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(p.Line))
	}
	b.WriteString(": ")
	b.WriteString(p.Message)
	return b.String()
}

// Report は元実装が標準出力へ書く内容をそのまま組み立てる。
//
// 規則（移植仕様「形式検証 R22 / R23」）: 問題を収集順に1行ずつ出し、
// 1件以上あれば空行を1行はさんで "N problem(s)." を出す。1件でも "(s)" は付く。
// 問題が無ければ "translations OK" だけを出す。いずれも末尾は改行1個。
//
// 元実装は問題を標準エラーではなく標準出力へ書く。CIのログの拾い方が変わるので
// 移植側でも変えない。終了コードは呼び出し側が len(problems) から決める。
func Report(problems []Problem) string {
	if len(problems) == 0 {
		return "translations OK\n"
	}
	var b strings.Builder
	for _, p := range problems {
		b.WriteString(p.String())
		b.WriteByte('\n')
	}
	// 元実装の print(f"\n{n} problem(s).") は、直前の行の改行に続けて
	// 空行を1行入れる。
	b.WriteByte('\n')
	b.WriteString(strconv.Itoa(len(problems)))
	b.WriteString(" problem(s).\n")
	return b.String()
}
