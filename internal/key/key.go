// Package key は、公開CSVの key 列に入る値を扱う。
//
// 公開されている Translations/<locale>/strings.csv は英語原文を持たない。
// 各行は原文そのもののハッシュで引かれる。そのため、原文からキーを作る規則と、
// ある文字列がキーの形をしているかどうかの判定規則が、この移植の要になる。
//
// 元実装は src/DragNWashLocalization/TranslationKey.cs と tools/hash-strings.ps1。
// 両者は同じキーを計算する（TranslationKey.cs の冒頭コメントに明記がある）。
package key

import (
	"crypto/sha256"
	"encoding/hex"
)

const (
	// Length はキーの長さ。16進の「桁数」であってバイト数ではない。
	// 元実装の TranslationKey.Length = 16 に対応する。
	// ダイジェストから取るバイト数は Length/2 = 8（64bit）。
	Length = 16

	// LineIDPrefix は台詞ID（Yarn の line ID）の接頭辞。
	// 元実装の TranslationKey.LinePrefix = "line:" に対応する。
	LineIDPrefix = "line:"

	// lineIDMaxLength は台詞IDの最大長。元実装が value.Length > 64 で弾くため、
	// 64 は「含む」。接頭辞5文字を除いた実体部は 1〜59 文字となり、
	// tools/hash-strings.ps1 の $lineIdPattern = '^line:[A-Za-z0-9_.\-]{1,59}$' と一致する。
	lineIDMaxLength = 64

	// HashOfEmpty は空文字に対する For の戻り値。
	//
	// 元実装（TranslationKey.Hash）は null だけを空文字で弾いており、空文字は
	// 素通りして SHA-256("") のハッシュになる。つまりこの値は「本物に見える
	// 16桁キー」で、LooksLike はこれに true を返す。
	//
	// C# では「key 列そのものが無い」ときに変数が null になって Hash(null)=="" の
	// 経路へ落ちるが、Go には null が無いので、欠損を空文字で表して For に渡すと
	// 黙ってこの実在しうるキーが生まれる。呼び出し側は欠損を空文字で表現せず、
	// comma-ok などで「列が無い」ことを持ち回り、欠損時は For を呼ばないこと。
	// 移植仕様「CSVとキー生成 / 敵対検証で見つかった食い違い [medium] R15」参照。
	HashOfEmpty = "e3b0c44298fc1c14"
)

// For は原文からキーを作る。
//
// 規則（移植仕様 R15）: 文字列の UTF-8 バイト列に対する SHA-256 の先頭8バイトを、
// 小文字16進で連結した16桁。入力への加工（トリム・Unicode正規化・書式タグ除去）は
// 一切行わない。元実装のコメントも "exactly as TMP received it (no trimming,
// tags included)" と明記している。前後の空白も、TMPの書式タグも、改行も、
// すべてハッシュ対象に含む。
//
// 空文字については HashOfEmpty のコメントを参照。
//
// 不正な UTF-8 バイト列は契約外。元実装は .NET の StreamReader が U+FFFD へ
// 置換したあとの文字列を受け取るが、置換される個数が .NET と Go で一致しない
// ため、バイト等価は保証しない（移植仕様「敵対検証」[low] R2 参照）。
// ここでは入力バイト列をそのままハッシュする。
func For(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:Length/2])
}

// LooksLike は value がキーの形をしているかを返す。
//
// 規則（移植仕様 R17）: 長さがちょうど 16 で、全文字が [0-9a-f] のときだけ true。
// 大文字 A-F は false。元実装 TranslationKey.LooksLikeKey は小文字しか受け付けず、
// 呼び出し側（TranslationStore.cs:251）が必要に応じて ToLowerInvariant してから渡す。
// その非対称をここで吸収しない。「形が正しいか」だけを見る判定であって、
// 実在するキーかどうかは見ない。
//
// 元実装の長さ判定は UTF-16 コード単位長だが、ここではバイト長で比較する。
// 非 ASCII が混ざると両者はずれるが、その場合いずれも16進判定で false になるため
// 結論は変わらない。
func LooksLike(value string) bool {
	if len(value) != Length {
		return false
	}
	// 元実装はバイト単位の OR 比較。regexp を使わないのは、仕様書の文字クラス表記
	// [0-9a-f] をそのまま正規表現にすると意図せぬ解釈が混ざりうるため。
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// LooksLikeLineID は value が台詞ID（Yarn の line ID）の形をしているかを返す。
//
// 規則（移植仕様 R16）: 長さが 5（"line:" の長さ）より大きく 64 以下で、
// Ordinal（大文字小文字を区別する）比較で "line:" から始まり、接頭辞より後ろの
// 全文字が英数字か '_' '-' '.' のいずれか、のときだけ true。
// tools/hash-strings.ps1 の -cmatch '^line:[A-Za-z0-9_.\-]{1,59}$' と同じ判定になる。
//
// 境界: "line:" ちょうど（長さ5）は false。"LINE:xxx" は Ordinal 比較なので false。
// 接頭辞より後ろに ':' は許されない。
//
// 実装は元実装と同じくバイト単位の OR 比較にする。移植仕様の文字クラス表記
// [0-9a-zA-Z_-.] は正規表現として読むと '_'(0x5F) から '.'(0x2E) への逆順レンジに
// なり、Go の regexp ではコンパイルエラーになる（移植仕様「敵対検証」[low] R16）。
// 元実装の実体は3文字の OR であってレンジではない。
//
// 長さ判定は元実装が UTF-16 コード単位長、ここはバイト長。非 ASCII を含む値は
// どちらの経路でも最終的に false になるため結論は変わらない。
func LooksLikeLineID(value string) bool {
	if len(value) <= len(LineIDPrefix) || len(value) > lineIDMaxLength {
		return false
	}
	// 大文字小文字を区別する前方一致。strings.HasPrefix は Ordinal 比較と等価。
	if value[:len(LineIDPrefix)] != LineIDPrefix {
		return false
	}
	for i := len(LineIDPrefix); i < len(value); i++ {
		c := value[i]
		ok := (c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			c == '_' || c == '-' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}
