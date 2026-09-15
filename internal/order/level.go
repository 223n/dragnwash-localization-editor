package order

import (
	"strconv"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
)

const (
	// flagListSeparator は level_flow.csv が複数値を連ねるときの区切り。
	// 半角スペース・パイプ・半角スペースの3文字ちょうど。
	flagListSeparator = " | "
	// flagListJoin は見出しに出すときの区切り。
	flagListJoin = ", "
)

// LevelMeta は level_flow.csv の1行から、見出しに要る列だけを取り出したもの。
//
// level_flow.csv には他に flow_asset, intro, progress_dialogs, idle_dialogs,
// nag_dialogs, phone, outro, jerkoff_dialog, cum_dialog, mount_start,
// mount_finish, spawn_flag, player_spawn 列があるが、元実装もここでは読まない
// （移植仕様「スクリプト順 / データ構造」）。
type LevelMeta struct {
	// Index は level 列の値で 0 始まり。表に出る番号は Index+1 になる。
	Index int
	// Dragon は dragon 列。Ryan / Conrad / Alexander。
	Dragon string
	// Weather は weather 列。Sunny / Rainy / Night、空もある。
	Weather string
	// SetFlags は set_flags 列の " | " を ", " に置換した値。
	SetFlags string
	// EndFlags は end_flags 列の " | " を ", " に置換した値。
	EndFlags string
}

// Section はこのレベルのセクション名を返す。script_order.csv の section 列と
// 突き合わせるためのキーで、番号は2桁ゼロ埋めになる（例 "L01 Ryan"）。
//
// 元実装は `$"L{Index + 1:00} {Dragon}"`（ScriptOrder.cs）と
// `('L{0:00} {1}' -f ($idx + 1), $l.dragon)`（hash-strings.ps1）で、
// 移植仕様「スクリプト順 R10」「公開CSV生成 R6」にあたる。
//
// ゼロ埋めの有無が [LevelMeta.Header] と食い違うのは元実装どおり。
// こちらは2桁ゼロ埋め、見出しはゼロ埋めなし（移植仕様「公開CSV生成 / 境界条件」）。
func (m LevelMeta) Section() string {
	return "L" + pad2(m.Index+1) + " " + m.Dragon
}

// Header はこのレベルの見出し文言を返す。
//
//	Level 5: Conrad (Rainy) | sets level_5 | ends MedkitCompleted, level_5_complete
//
// 天気・sets・ends は、値が空でないときだけこの順で後置される（移植仕様
// 「スクリプト順 R11」「公開CSV生成 R6」）。空欄のときは " ()" のような空の
// パーツも出さない。番号はゼロ埋めしない。
func (m LevelMeta) Header() string {
	var b strings.Builder
	b.WriteString("Level ")
	b.WriteString(strconv.Itoa(m.Index + 1))
	b.WriteString(": ")
	b.WriteString(m.Dragon)
	if m.Weather != "" {
		b.WriteString(" (")
		b.WriteString(m.Weather)
		b.WriteString(")")
	}
	if m.SetFlags != "" {
		b.WriteString(" | sets ")
		b.WriteString(m.SetFlags)
	}
	if m.EndFlags != "" {
		b.WriteString(" | ends ")
		b.WriteString(m.EndFlags)
	}
	return b.String()
}

// ParseLevels は level_flow.csv の行を [LevelMeta] に変換する。
//
// level 列の扱いは元実装の2つが食い違う（移植仕様「スクリプト順 R8」）。
//
//	C#         `if (!int.TryParse(Get(row, "level"), out int index)) continue;` で行を捨てる
//	PowerShell `$idx = [int]$l.level` で変換する。失敗すれば例外で処理全体が止まる
//
// ここでは次のように寄せた:
//
//   - 空文字と、level 列そのものが無い行は Index=0 として残す。
//     pwsh 7.6.6 で [int]” と [int]$null がどちらも 0 を返すことを実測した。
//     捨てるとレベル1の見出し文言が元実装と食い違う。
//   - "abc" のように整数として読めない値の行は捨てる。PowerShell は例外で
//     止まるが、見出しが1つ欠けるだけで済ませ、翻訳ファイル全体の書き出しを
//     巻き添えにしないため。
//
// なお PowerShell の [int] は "3.7" を 4 に、"0x10" を 16 に、"1e2" を 100 に
// 黙って変換する。そこまでは再現せず、いずれも行を捨てる。実データの level 列は
// 0〜14 が全行埋まっているので、どの選択でも出力は変わらない。
//
// set_flags / end_flags の " | " だけを ", " に置き換える。パイプ単体や、
// 前後の空白の数が違うものは置換されない（移植仕様「スクリプト順 R9」）。
// weather と dragon は無加工。
//
// 並べ替えはしない。返り値はファイルの出現順で、同じ [LevelMeta.Section] を
// 持つ行が複数あるときの勝ち負けは [Data] の引き当て側で決まる（後勝ち）。
func ParseLevels(rows []csvfile.Row) []LevelMeta {
	levels := make([]LevelMeta, 0, len(rows))
	for _, row := range rows {
		raw := row.Get("level")
		index, ok := parseInt32(raw)
		if !ok {
			// 空文字（列が無い場合を含む）だけは 0 として残す。
			if strings.TrimSpace(raw) != "" {
				continue
			}
			index = 0
		}
		levels = append(levels, LevelMeta{
			Index:    index,
			Dragon:   row.Get("dragon"),
			Weather:  row.Get("weather"),
			SetFlags: strings.ReplaceAll(row.Get("set_flags"), flagListSeparator, flagListJoin),
			EndFlags: strings.ReplaceAll(row.Get("end_flags"), flagListSeparator, flagListJoin),
		})
	}
	return levels
}

// pad2 は .NET の数値書式 "00"（最小2桁のゼロ埋め）と同じ文字列を作る。
//
// Go の fmt の %02d は幅に符号を含めるため、-1 が "-1" になって .NET の "-01" と
// 食い違う（移植仕様「スクリプト順 R10」のGoでの注意）。実データの level は
// 0〜14 なので負値は出ないが、手編集の level_flow.csv では起こりうる。
func pad2(n int) string {
	digits := strconv.Itoa(n)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	if len(digits) < 2 {
		digits = "0" + digits
	}
	return sign + digits
}
