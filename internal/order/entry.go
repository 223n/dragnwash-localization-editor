package order

import (
	"strconv"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
)

// Entry は script_order.csv の1行で、1つの台詞の1回の出現を表す。
//
// 同じ英文（同じ [Entry.Key]）は複数のノード・複数の話者に現れる。実データでは
// 1839行に対してキーが1602種、最大29回現れるキーがある
// （移植仕様「スクリプト順 R28」「公開CSV生成 / 境界条件」）。
//
// 文字列のフィールドは [Entry.Key] を除いて一切正規化しない。トリムも小文字化も
// しないので、ファイルにある綴りがそのまま入る（移植仕様「スクリプト順 R4」）。
type Entry struct {
	// Section はセクション名。実データでは "L01 Ryan"〜"L15 Alexander" と
	// "Cutscene" / "Reaction" / "Unused" の18種。見出し文言に直すには
	// [Data.SectionTitle] を使う。
	Section string
	// HasSection は section 列がヘッダーにあったかどうか。列そのものが無い
	// ファイルでは false になり、Section は空文字になる。
	//
	// 空文字との区別が要るのは見出しの変化判定のため。元実装は
	// `$e.section -ne $lastSection` で生のプロパティ値を比べており、列が無い
	// ときの値は $null、$lastSection の初期値も $null なので先頭行で見出しが
	// 出ない。一方 section 列があって値が空文字なら `'' -ne $null` が真に
	// なって見出しが出る（移植仕様「公開CSV生成 / 敵対検証」[low] の
	// '' と $null の区別。列欠損側は仕様に無く、pwsh 7.6.6 で実測した）。
	HasSection bool
	// Phase は intro, phone, progress, idle, nag, picnic, jerkoff, cum,
	// mount_start, mount_finish, outro のいずれか。
	// Cutscene / Reaction / Unused のセクションでは空。
	Phase string
	// Node は Yarn のノード名。
	Node string
	// HasNode は node 列がヘッダーにあったかどうか。意味は [Entry.HasSection] と同じ。
	HasNode bool
	// Order はノード内の連番（1始まり）。解析できない値は 0 で、行は捨てない
	// （移植仕様「スクリプト順 R5」）。
	//
	// 注意: ここが飛び飛びになるのは正常。公開CSVには同じキーの2回目以降が
	// 出ないため、出力側で欠番が生じる（移植仕様「スクリプト順 / 抽出が
	// 取りこぼしていた規則」）。
	Order int
	// OrderText は order 列の生の値。
	//
	// 公開CSVの order 列は、元実装 tools/hash-strings.ps1 が `$e.order` を
	// 文字列のまま連結して書いている。出力側は [Entry.Order] ではなくこちらを
	// 使うこと。10進で書き戻すと " 7" や "007" が "7" に化けて、元実装と
	// バイト一致しなくなる。
	OrderText string
	// LineID は Yarn の台詞ID（"line:xxxxxxxx"）。実データでは全行が非空かつ
	// 重複なし。形の判定は internal/key の LooksLikeLineID を使う。
	LineID string
	// Key は英文のハッシュキー。この構造体で唯一 [NormalizeKey] を通る値。
	Key string
	// Speaker は話者名（"Ryan" "Kobold" "Phone" など）。空もある。
	// 正規化しないので、同じ人物の綴り違いは別人として扱われる。
	Speaker string
	// Condition は親ノードがジャンプの前に読んだ変数名を、半角スペース区切りで
	// 連結した文字列（例 "$conrad_jerked_off_3 $conrad_used_mount_3"）。空もある。
	Condition string
}

// NormalizeKey は key 列の値を、前後の空白を除いてから小文字にする。
//
// 元実装はどちらも `key.Trim().ToLowerInvariant()`（ScriptOrder.cs:256、
// hash-strings.ps1 の `([string]$e.key).Trim().ToLowerInvariant()`）で、
// 移植仕様「スクリプト順 R4」「公開CSV生成 R9」にあたる。
//
// 差異として確認できている点:
//
//   - C# の Trim() は char.IsWhiteSpace 基準で、U+00A0 や U+0085 も落とす。
//     Go の strings.TrimSpace は unicode.IsSpace 基準で、この2文字は同じく落ちる。
//     実運用のキーは16桁hexなので、残る細かな差が問題になる場面は無い。
//   - ToLowerInvariant と strings.ToLower は非ASCIIで挙動が割れうるが、
//     キーは [0-9a-f] しか取らないので影響しない（移植仕様「未決の点」）。
//
// 空文字を渡すと空文字が返る。空のキーは internal/key の HashOfEmpty のような
// 「本物に見えるキー」とは別物で、ここでハッシュ化は一切しない。
func NormalizeKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// ParseEntries は script_order.csv の行を、ゲーム内Mod（ScriptOrder.cs）と同じ
// 規則で [Entry] に変換する。
//
// 並べ替えはしない。行の順序が再生順そのものだからで、これを崩すと公開CSVの
// 並びが変わる（移植仕様「スクリプト順 R15」）。
//
// key 列が無い行と、key 列の値が空文字の行は捨てる（移植仕様「スクリプト順 R3」）。
// 判定はトリムの前に行うため、空白だけの key を持つ行は捨てられず、Key が空文字の
// [Entry] になる。元実装の `if (string.IsNullOrEmpty(key)) continue;` が
// IsNullOrWhiteSpace ではないことに由来する。
//
// この絞り込みは C# 側だけの規則なので、公開CSV生成の経路では使わないこと。
// そちらは [ParseAllEntries] を使う。
//
// 列名の照合は大文字小文字を区別しない（csvfile.Row の性質）。ヘッダーが
// "Key,Section,..." のファイルでも読める。
func ParseEntries(rows []csvfile.Row) []Entry {
	return parseEntries(rows, true)
}

// ParseAllEntries は script_order.csv の行を、公開CSV生成（tools/hash-strings.ps1）と
// 同じ規則で [Entry] に変換する。[ParseEntries] と違い、key が空の行も捨てない。
//
// 元実装の `foreach ($e in $order)` は Read-Csv が返したレコードを無条件に回して
// おり、key 空行のスキップは存在しない（hash-strings.ps1:148-171）。捨てると
// 2つの食い違いが出る:
//
//   - key が空で line_id を持つ行に対応する台詞ID訳が、台本中の位置ではなく
//     末尾の「Per-line translations ...」ブロックへ落ち、section/node/order/speaker
//     が空になる
//   - 「再生順が1件以上あるか」の判定（$order.Count -gt 0）が変わり、UI 見出しと
//     section='UI' が出なくなる
//
// 現行の data/script_order.csv には key が空の行が無いので、実データでの出力は
// どちらの関数でも同じになる。
func ParseAllEntries(rows []csvfile.Row) []Entry {
	return parseEntries(rows, false)
}

// parseEntries は [ParseEntries] と [ParseAllEntries] の共通部分。
func parseEntries(rows []csvfile.Row, dropEmptyKey bool) []Entry {
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		rawKey := row.Get("key")
		if dropEmptyKey && rawKey == "" {
			continue
		}
		orderText := row.Get("order")
		section, hasSection := row.Lookup("section")
		node, hasNode := row.Lookup("node")
		entries = append(entries, Entry{
			Section:    section,
			HasSection: hasSection,
			Phase:      row.Get("phase"),
			Node:       node,
			HasNode:    hasNode,
			Order:      parseOrder(orderText),
			OrderText:  orderText,
			LineID:     row.Get("line_id"),
			Key:        NormalizeKey(rawKey),
			Speaker:    row.Get("speaker"),
			Condition:  row.Get("condition"),
		})
	}
	return entries
}

// parseOrder は order 列を数に直す。解析できなければ 0 を返す。
//
// 元実装は `int.TryParse(o, out int order);` と戻り値を捨てており、失敗時は
// out 引数の既定値 0 がそのまま使われる（移植仕様「スクリプト順 R5」）。
// [parseInt32] に寄せてあるのは、`order, _ := strconv.Atoi(s)` と書くと
// 範囲外の値で 0 ではなく巨大値が入ってしまうため
// （移植仕様「スクリプト順 / 敵対検証」[medium] R5）。
func parseOrder(raw string) int {
	value, ok := parseInt32(raw)
	if !ok {
		return 0
	}
	return value
}

// parseInt32 は .NET の int.TryParse（既定の NumberStyles.Integer）に相当する解析。
//
// 合わせてある点:
//
//   - 前後の空白を許す（AllowLeadingWhite / AllowTrailingWhite）。
//     strconv 側は空白を受け付けないので、先に落とす。
//   - 先頭の符号を許す（AllowLeadingSign）。"+5" も -5 も通る。
//   - 32bit の範囲を外れたら失敗。int.TryParse は Int32 なので、
//     "3000000000" は .NET では失敗する。bitSize を 32 にしてそろえる。
//   - 基数は10固定。bitSize 以外の理由で strconv が受け付ける "0x10" や
//     アンダースコア区切りは、base を 0 にしないかぎり通らない。
func parseInt32(raw string) (int, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
	if err != nil {
		return 0, false
	}
	return int(value), true
}
