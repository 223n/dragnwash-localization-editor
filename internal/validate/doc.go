// Package validate は、公開ずみの Translations/<ロケール>/strings.csv が
// 公開してよい形をしているかを検査する。tools/check-translations.py の移植で、
// 元実装は CI がすべてのPull Requestで走らせている。
//
// このパッケージが守りたいものは2つある。
//
//   - 公開ファイルが機械で読める形を保つこと（ヘッダー、キーの形、重複、空の訳）。
//   - 英語原文が公開リポジトリに漏れないこと。原文は作品そのものなので、
//     source_en 列のあるファイルや Translations/_discovered/ の作業コピーが
//     コミットされていたら止める。
//
// 2つめの目的から、報告メッセージには key の値も原文も入れない。
// 元実装にも「on a public repository the report is visible, and a plain-text key
// is the game's script」というコメントがある。CIのログは誰でも読めるため、
// 「何行目がどう悪いか」までは書くが「その行に何が書いてあるか」は書かない。
// [Problem] が値を持たないのはそのため。
//
// # 検査の入口
//
//   - [CheckFile] はバイト列1つ分を検査する。ファイルを開かないので、
//     編集中のバッファをそのまま渡せる。
//   - [CheckTree] はリポジトリルートを受け取り、Translations 配下を丸ごと歩く。
//     元実装の main に対応する。
//
// # 読み方は internal/csvfile に任せる
//
// 行の残し方（空白だけの行と '#' 始まりの行を物理行単位で落とす）と、
// 残った行の並びから報告用の物理行番号を復元する規則は
// [csvfile.ReadPythonLines] と [csvfile.ParsePythonRecords] が持っている。
// Go の encoding/csv では再現できない（引用符で囲まないフィールド中の裸の '"'
// でパースが止まり、以降の行がまるごと無検査になる）ため、そちらは使わない。
// 詳しくは internal/csvfile のパッケージコメントを参照。
//
// # 元実装と意図的に変えたところ
//
//   - 正規表現の末尾 '$' の扱い。Python の '$' は末尾の改行1個の手前にも当たるため、
//     元実装では "UI\n" が識別子として通る。ここでは通さない（[looksLikeIdentifier]）。
//   - 例外で落ちる経路を落ちないようにした。Translations が無い・ファイルが読めない
//     ときは [CheckTree] がエラーを返す。不正なUTF-8・巨大なフィールド・NULバイトは
//     元実装なら異常終了するが、ここでは検査を続ける。
//   - 出力は常にUTF-8。元実装は Windows のロケール依存で、非ASCIIを含む
//     メッセージを出そうとすると UnicodeEncodeError で落ちうる。
//
// いずれも「元実装が問題なしと言うものを問題ありにしない」方向に倒してある。
// 元リポジトリの13ロケールに対しては、元実装と同じく問題0件になる。
package validate
