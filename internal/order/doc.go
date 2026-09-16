// Package order は、ゲームが台詞を再生する順（data/script_order.csv）と、
// レベルの見出し文言（data/level_flow.csv）を扱う。
//
// 公開CSVの行並びは、このパッケージが返す [Entry] の並びで決まる。
// script_order.csv のファイル出現順がそのまま「ゲームの再生順」であり、元実装は
// order 列でも section 名でも並べ替えない（移植仕様「スクリプト順 R15」）。
// したがって読み込み時にも並べ替えない。ファイル順が出力順の唯一の根拠になる。
//
// 元実装は2つあり、同じ2ファイルを読む。
//
//   - src/DragNWashLocalization/ScriptOrder.cs の Load（ゲーム内Mod）
//   - tools/hash-strings.ps1 の冒頭（公開CSVの生成）
//
// 違うのはCSVの読み方と大文字小文字の扱いで、読み方は [LoadCSharp] と
// [LoadPowerShell] で選ぶ（internal/csvfile のパッケージコメント参照）。
// 実データの data/script_order.csv と data/level_flow.csv には引用符・コメント行・
// 空行・複数行フィールドが1つも無いため、この2ファイルに関するかぎり両者の
// 結果は一致する。大文字小文字の扱いは [Data.SectionTitle] と
// [Data.SpeakersFor] のコメントに書いた方針でそろえた。
//
// このパッケージが引き受けないもの:
//
//   - ファイルの場所決め（data/ と Translations/_discovered/ のどちらを採るか）と
//     mtime によるキャッシュ（移植仕様「スクリプト順 R1/R2」）。置き場所の知識は
//     呼び出し側にあるので、読んだバイト列を渡してもらう。採用したパスを覚えたい
//     ときは [Data.Source] に入れる。
//   - WriteOrdered（移植仕様「スクリプト順 R15〜R22」）。見出しをいつ出すかは
//     出力側の関心事なので、ここは見出しの「文言」までを受け持つ。
package order
