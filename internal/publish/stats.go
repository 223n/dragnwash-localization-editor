package publish

import "fmt"

// Stats は1ファイル分の集計。元実装が最後に1行ログを出すときの数値と同じもの
// （移植仕様「公開CSV生成 R28」）。
//
// Converted / Kept / Dropped は「形式として妥当だったか」を数えるだけで、
// その行が実際に出力されたかとは一致しない。重複キーの行や訳が空の行も、
// 形式が妥当なら Converted / Kept に入る。元実装が重複判定（R18）と訳の空判定（R20）を
// カウンタ加算より後ろに置いているためで、ログの数値を合わせるにはこの順が要る。
type Stats struct {
	// Converted は source_en からキーを計算した行数。
	Converted int
	// Kept は source_en が無く、key が既に16桁キーだった行数。
	Kept int
	// LineKept は出力できた台詞ID行の数。再生順の中に置けた分（R24）と、
	// 置けずに末尾へ回した分（R26）の合計。
	LineKept int
	// Dropped は形式が合わずに捨てた行数。source_en とキーが食い違う行（R15）と、
	// source_en も16桁キーも無い行（R17）。
	Dropped int
	// InPlayOrder は再生順の中に出力できたハッシュ行の数（元実装の $done.Count）。
	InPlayOrder int
	// Other は再生順に無く UI 見出しの下へ回したキーの数（元実装の $left.Count）。
	Other int
}

// LogLine は元実装の Write-Host と同じ書式の1行を返す。
//
// ラベルの並びが集計処理の順と違う（per-line が malformed dropped より前に来る）が、
// 位置と値の対応は元実装のとおり。入れ替えないこと（移植仕様「敵対検証」[low] R28）。
func (s Stats) LogLine(output, input string) string {
	return fmt.Sprintf(
		"%s <- %s: %d converted, %d already hashed, %d per-line, %d malformed dropped, %d in play order, %d other",
		output, input, s.Converted, s.Kept, s.LineKept, s.Dropped, s.InPlayOrder, s.Other)
}
