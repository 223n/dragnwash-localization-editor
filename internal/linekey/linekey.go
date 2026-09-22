// Package linekey は、台詞の英文を「少し書き換わっても同じ行だと分かる形」に
// 直す規則を持つ。
//
// # 何のためにあるか
//
// 公開ファイルの key は英文そのもののハッシュである（internal/key）。1文字でも
// 変わればキーが変わり、訳は何も言わずに英語へ戻る。翻訳リポジトリの
// CONTRIBUTING.ja.md も「ゲームのアップデートでIDが変わった台詞は、何も言わずに
// ハッシュの行の訳に戻ります」と書いている。
//
// そこで、記号・大文字小文字・書式タグ・空白の違いを落とした形（[Normalize]）の
// ハッシュと、文字の並びの近さを測る指紋（[Fingerprint]）を使う。前者が一致すれば
// 「同じ台詞の書式だけが変わった」、後者が近ければ「言い回しが変わった同じ台詞」の
// 見当が付く。
//
// data/script_order.csv には、この2つと正規化後の文字数が norm / fp / nlen 列として
// コミットされている。つまり、翻訳リポジトリを持っているだけで、git の履歴にも
// ゲーム側の台本にも頼らずに突き合わせができる。
//
// # 元実装
//
// 移植元は翻訳リポジトリの tools/linekeys.py である。同ファイルの説明によれば、
// これは Drag'n Wash ModFramework の tools/linekeys.py の写しで、定義の本体は
// 同リポジトリの src/DragNWash.ModFramework.Dialogue/LineKey.cs にある。
// LineKey.cs は手元に無いので、突き合わせたのは Python 側だけである。
//
// 上流はこの仕組みを experimental（Dialogue 1.1）と書いている。定義が変われば
// norm と fp の意味も変わるので、上流を追うときはここも見直すこと。
//
// # Python との差
//
// 文字の分類だけ、言語の持つ表に頼っている。
//
//   - 空白は Python の str.isspace() に合わせる。Go の [unicode.IsSpace] は
//     U+001C〜U+001F（ファイル区切りなどの制御文字）を空白と見ないので、
//     そこだけ足している。
//   - 英数字は Python の str.isalnum() に合わせ、[unicode.IsLetter] と
//     [unicode.IsNumber] の和にする。IsDigit（Nd のみ）にすると、½ や Ⅷ の
//     ような数値文字を落として Python と違う正規化になる。
package linekey

import (
	"encoding/hex"
	"math/bits"
	"strings"
	"unicode"

	"github.com/223n/dragnwash-localization-editor/internal/key"
)

const (
	// MinFuzzyLength は指紋で突き合わせてよい正規化後の最短の長さ。
	// 元実装の MIN_FUZZY_LENGTH。短い文（"Yes" "Wonderful!"）は、
	// 3文字窓の指紋が近くても別の台詞であることが多い。
	MinFuzzyLength = 12

	// MaxFuzzyDistance は同じ台詞と見なしてよい指紋の距離の上限。
	// 元実装の MAX_FUZZY_DISTANCE。
	MaxFuzzyDistance = 10

	// FingerprintBits は指紋の桁数（ビット）。SimHash の幅。
	FingerprintBits = 64
)

// Normalize は英文から、書式と体裁の違いを落とす。
//
// 落とすのは4つ。<> で囲まれた書式タグ、空白の連なり（1個の半角空白にする）、
// 英数字でない文字、そしてASCIIの大文字（小文字にする）。前後の空白は残らない。
//
// 元実装（linekeys.py の normalize）を1文ずつ写したものである。順序に意味が
// あるので、条件の並びを入れ替えないこと。
//
//   - タグの中は "<" から ">" までを読み飛ばす。入れ子は考えない。">" が
//     来ないまま文が終われば、そこから先は全部落ちる（"<b>bold" は "bold" に
//     なるが、"<b" は空になる）。
//   - 空白は、次に英数字が来て、かつ既に1文字でも出力していれば1個だけ入れる。
//     この2つの条件で、先頭の空白も末尾の空白も残らない。
//   - 英数字でない文字は落とすが、直前の空白の記憶は消さない。"a ,b" と
//     "a, b" がどちらも "a b" になるのはこのためである。
//   - 小文字化するのは A-Z だけである。非ASCIIの大文字（Ä など）はそのまま。
//     ハッシュを取る前の形が元実装と1バイトも違ってはいけない。
func Normalize(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	inTag := false
	pendingSpace := false
	wrote := false
	for _, c := range text {
		if inTag {
			if c == '>' {
				inTag = false
			}
			continue
		}
		if c == '<' {
			inTag = true
			continue
		}
		if isSpace(c) {
			pendingSpace = true
			continue
		}
		if !isAlnum(c) {
			continue
		}
		if pendingSpace && wrote {
			out.WriteByte(' ')
		}
		pendingSpace = false
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out.WriteRune(c)
		wrote = true
	}
	return out.String()
}

// NormalizedKey は [Normalize] を通した文字列のハッシュキーを返す。
// data/script_order.csv の norm 列に入っている値である。
func NormalizedKey(text string) string {
	return key.For(Normalize(text))
}

// Fingerprint は正規化した文字列の指紋（64bitのSimHash）を返す。
//
// 作り方は元実装（linekeys.py の fingerprint）のとおり。正規化した文字列を
// 3文字ずつの窓に切り、窓ごとに FNV-1a 64 のハッシュを取り、ビットごとに
// 「1なら+1、0なら-1」を数えて、正の桁だけを立てる。数えた結果が 0 の桁は
// 立てない。
//
// 窓は文字（ルーン）単位で、ハッシュはその窓のUTF-8のバイト列に対して取る。
// バイト単位で切ると、日本語のような多バイト文字で元実装と違う指紋になる。
//
// 正規化後が空なら 0 を返す。3文字未満なら全体を1つの窓として扱う。
func Fingerprint(text string) uint64 {
	n := []rune(Normalize(text))
	if len(n) == 0 {
		return 0
	}
	var votes [FingerprintBits]int
	count := func(window string) {
		h := fnv1a64(window)
		for b := 0; b < FingerprintBits; b++ {
			if (h>>uint(b))&1 == 1 {
				votes[b]++
			} else {
				votes[b]--
			}
		}
	}
	if len(n) < 3 {
		count(string(n))
	} else {
		for i := 0; i+3 <= len(n); i++ {
			count(string(n[i : i+3]))
		}
	}
	var out uint64
	for b := 0; b < FingerprintBits; b++ {
		if votes[b] > 0 {
			out |= 1 << uint(b)
		}
	}
	return out
}

// FingerprintText は [Fingerprint] を16桁の小文字16進で返す。
// data/script_order.csv の fp 列に入っている形である。
func FingerprintText(text string) string {
	var b [8]byte
	fp := Fingerprint(text)
	for i := 7; i >= 0; i-- {
		b[i] = byte(fp)
		fp >>= 8
	}
	return hex.EncodeToString(b[:])
}

// ParseFingerprint は fp 列の値を数に直す。16桁の16進でなければ false を返す。
//
// 読めない値を 0 として通さないのは、0 が「指紋が無い」の意味で使われている
// ためである（元実装の Record が fp 列の空を 0 にし、resolve が 0 の行を
// 突き合わせから外す）。壊れた値を 0 に丸めると、静かに対象から外れる。
func ParseFingerprint(s string) (uint64, bool) {
	if len(s) != 16 {
		return 0, false
	}
	raw, err := hex.DecodeString(strings.ToLower(s))
	if err != nil {
		return 0, false
	}
	var out uint64
	for _, b := range raw {
		out = out<<8 | uint64(b)
	}
	return out, true
}

// Distance は2つの指紋で違っているビットの数（ハミング距離）を返す。
func Distance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// fnv1a64 は FNV-1a の64bit版。文字列のUTF-8のバイト列に対して取る。
func fnv1a64(s string) uint64 {
	const (
		offset = 0xCBF29CE484222325
		prime  = 0x100000001B3
	)
	h := uint64(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	return h
}

// isSpace は Python の str.isspace() と同じ判定を返す。
func isSpace(c rune) bool {
	// U+001C〜U+001F は Python では空白だが、Go の unicode.IsSpace では
	// 空白でない。台詞に出る文字ではないが、正規化がずれれば norm 列の
	// ハッシュがずれるので、判定はそろえておく。
	if c >= 0x1C && c <= 0x1F {
		return true
	}
	return unicode.IsSpace(c)
}

// isAlnum は Python の str.isalnum() と同じ判定を返す。
func isAlnum(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsNumber(c)
}
