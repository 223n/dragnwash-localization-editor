package order

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
)

// 見出しが固定文言になるセクション名。script_order.csv の section 列に実際に入る値。
const (
	CutsceneSection = "Cutscene"
	ReactionSection = "Reaction"
	UnusedSection   = "Unused"
)

// 固定のセクション見出し文言。公開CSVの "# ===== ... =====" 行に出る文字列で、
// 元実装 Section-Title / SectionTitle の switch に書かれているものと同一。
const (
	CutsceneTitle = "Cutscenes (started by game code)"
	ReactionTitle = "Dragon reactions (started by game code)"
	UnusedTitle   = "Unused nodes (not reachable in the current game)"
)

// SpeakerSeparator は話者を連ねるときの区切り。"Ryan/Alexander" の "/"。
const SpeakerSeparator = "/"

// fixedSectionTitles は固定文言の対応表。キーは csvfile.FoldASCII で畳んである
// （[Data.SectionTitle] の大文字小文字の方針を参照）。
var fixedSectionTitles = map[string]string{
	csvfile.FoldASCII(CutsceneSection): CutsceneTitle,
	csvfile.FoldASCII(ReactionSection): ReactionTitle,
	csvfile.FoldASCII(UnusedSection):   UnusedTitle,
}

// Data は script_order.csv と level_flow.csv を読んだ結果。
//
// 値のコピーは作らないこと（遅延構築の索引を持つため）。常に *Data で回す。
// [Data.Entries] と [Data.Levels] を後から書き換えても、一度作った索引は
// 作り直されない。元実装の話者表も同じ性質を持つ
// （ScriptOrder.cs の `if (_speakers == null) BuildSpeakers();`）。
type Data struct {
	// Entries は script_order.csv のファイル出現順。並べ替えない。
	Entries []Entry
	// Levels は level_flow.csv のファイル出現順。引き当ては [Data.LevelFor]。
	Levels []LevelMeta
	// Source は読み込み元のパス。このパッケージは設定しない。ログや診断のために
	// 呼び出し側が入れる（元実装の data.Source に対応）。
	Source string

	levelsOnce sync.Once
	levelExact map[string]LevelMeta
	levelFold  map[string]LevelMeta

	speakersOnce sync.Once
	speakers     map[string][]string
}

// LoadCSharp は、ゲーム内Mod（ScriptOrder.cs + CsvReader.cs）と同じ読み方で
// 2つのCSVを読む。エラーは返さない。元実装のCSV解析がどんな壊れた入力でも
// 例外を投げないため。
//
// levelFlowCSV に nil を渡すと Levels は空になる。元実装で level_flow.csv が
// 無いときと同じ状態で、セクション見出しは [Data.SectionTitle] のフォールバックに
// 落ちる（移植仕様「スクリプト順 R7」）。
func LoadCSharp(scriptOrderCSV, levelFlowCSV []byte) *Data {
	return &Data{
		Entries: ParseEntries(csvfile.ReadCSharpRows(scriptOrderCSV)),
		Levels:  ParseLevels(csvfile.ReadCSharpRows(levelFlowCSV)),
	}
}

// LoadPowerShell は、公開CSV生成（tools/hash-strings.ps1 の Read-Csv）と同じ
// 読み方で2つのCSVを読む。
//
// ヘッダーの列名が重複しているときだけエラーを返す。元実装では
// ConvertFrom-Csv が例外を投げ、$ErrorActionPreference = 'Stop' によって
// 処理全体が止まる（移植仕様「公開CSV生成 / 敵対検証」[low]）。どちらのファイルで
// 起きたかが分かるように包んで返す。
//
// levelFlowCSV に nil を渡すと Levels は空になる。
func LoadPowerShell(scriptOrderCSV, levelFlowCSV []byte) (*Data, error) {
	orderRows, err := csvfile.ReadPowerShellRows(scriptOrderCSV)
	if err != nil {
		return nil, fmt.Errorf("script_order.csv: %w", err)
	}
	flowRows, err := csvfile.ReadPowerShellRows(levelFlowCSV)
	if err != nil {
		return nil, fmt.Errorf("level_flow.csv: %w", err)
	}
	return &Data{
		// key が空の行も残す。数も並びも元実装の $order と一致させるため
		// （[ParseAllEntries] を参照）。
		Entries: ParseAllEntries(orderRows),
		Levels:  ParseLevels(flowRows),
	}, nil
}

// LevelFor はセクション名に対応する [LevelMeta] を返す。
//
// 同じセクション名の行が複数あるときは、ファイルで後にある行が勝つ（移植仕様
// 「スクリプト順 R10」「公開CSV生成 R6」。元実装はどちらも辞書へのインデクサ代入）。
//
// 引き当ては完全一致を先に試し、外れたら大文字小文字を無視して引き直す。
// 元実装の2つが食い違っている箇所で、C# は StringComparer.Ordinal、PowerShell は
// 既定の Hashtable なので大小無視になる（移植仕様「公開CSV生成 / 未決の点」）。
// 実データのセクション名は両ファイルで綴りが完全に一致しているため、この順なら
// どちらの元実装とも同じ結果になる。食い違うのは手編集で大小がずれた場合だけで、
// そのときは「見出しを黙って失う」より「大小を無視してでも見出しを出す」方を選んだ。
func (d *Data) LevelFor(section string) (LevelMeta, bool) {
	d.levelsOnce.Do(d.buildLevels)
	if meta, ok := d.levelExact[section]; ok {
		return meta, true
	}
	meta, ok := d.levelFold[csvfile.FoldASCII(section)]
	return meta, ok
}

// buildLevels は [Data.Levels] から引き当て用の索引を作る。
func (d *Data) buildLevels() {
	d.levelExact = make(map[string]LevelMeta, len(d.Levels))
	d.levelFold = make(map[string]LevelMeta, len(d.Levels))
	for _, meta := range d.Levels {
		section := meta.Section()
		// 後勝ち。元実装の辞書代入と同じ。
		d.levelExact[section] = meta
		d.levelFold[csvfile.FoldASCII(section)] = meta
	}
}

// SectionTitle はセクション名を見出し文言に直す。
//
// 規則（移植仕様「公開CSV生成 R7」、ScriptOrder.cs の SectionTitle も同じ）:
//
//	(1) Cutscene / Reaction / Unused の3つは固定文言
//	(2) それ以外は level_flow.csv 由来の対応表を引き、[LevelMeta.Header] を返す
//	(3) 対応表にも無ければセクション名をそのまま返す
//
// C# 版は (2) を先に引いてから (1) の switch に落とすが、レベル側のセクション名は
// 必ず "L" で始まるので両者は交わらず、結果は変わらない。
//
// (1) の判定は大文字小文字を無視する。元実装の PowerShell の switch が大小を
// 区別しないためで、C# の switch とは食い違う箇所（移植仕様「公開CSV生成 /
// 未決の点」）。(2) の引き当ての方針は [Data.LevelFor] を参照。
//
// 空文字を渡すと空文字が返る。元実装も空セクションを特別扱いしない。
func (d *Data) SectionTitle(section string) string {
	if title, ok := FixedSectionTitle(section); ok {
		return title
	}
	if meta, ok := d.LevelFor(section); ok {
		return meta.Header()
	}
	return section
}

// FixedSectionTitle は Cutscene / Reaction / Unused の固定見出しを返す。
// 3つ以外は第2戻り値が false になる。判定は大文字小文字を無視する。
func FixedSectionTitle(section string) (string, bool) {
	title, ok := fixedSectionTitles[csvfile.FoldASCII(section)]
	return title, ok
}

// Speakers はそのキーの英文を話す話者を、script_order.csv の出現順・重複なしで
// 返す。未登録のキーには nil を返す。
//
// 規則（移植仕様「スクリプト順 R12」「公開CSV生成 R9」）: [Data.Entries] を
// ファイル順に1回走査し、Speaker が空の行は数えず、同じ話者名は1回だけ数える。
// 重複判定は完全一致なので、"Ryan" と "ryan" は別の話者として並ぶ
// （元実装の List<string>.Contains が序数比較。実データでは起きない）。
// 話者が全出現で空のキーは表に載らない。
//
// 返すのは複製なので、呼び出し側が書き換えても [Data] には影響しない。
func (d *Data) Speakers(key string) []string {
	d.speakersOnce.Do(d.buildSpeakers)
	list, ok := d.speakers[NormalizeKey(key)]
	if !ok {
		return nil
	}
	return slices.Clone(list)
}

// SpeakersFor は [Data.Speakers] の結果を "/" で連結して返す。公開CSVの speaker 列に
// そのまま入る値で、"Ryan/Alexander" や "Phone/Ryan/Alexander/Conrad/Kobold" になる。
// 未登録のキーには空文字を返す。
//
// 引数のキーは [NormalizeKey] を通してから引く。表に入っているキーは正規化済み
// なので、正規化済みのキーを渡すかぎり元実装と同じ結果になる。元実装は C# が
// 序数比較の辞書、PowerShell が大小無視の Hashtable と割れており、ここでは
// 引く側をそろえて両方に合う形にした（移植仕様「公開CSV生成 / 未決の点」）。
func (d *Data) SpeakersFor(key string) string {
	d.speakersOnce.Do(d.buildSpeakers)
	return strings.Join(d.speakers[NormalizeKey(key)], SpeakerSeparator)
}

// IsShared はそのキーの英文を2人以上の話者が話すかを返す。
//
// 同じ話者が何度話しても [Data.Speakers] の重複排除で1人のままなので false。
// 未登録のキーも false（移植仕様「スクリプト順 R14」）。
//
// 翻訳者に「この英文は複数人が話すので、台詞ID行で1行ずつ訳し分けられる」と
// 知らせるための判定。実データでは1602種のキーのうち29種が該当する。
func (d *Data) IsShared(key string) bool {
	d.speakersOnce.Do(d.buildSpeakers)
	return len(d.speakers[NormalizeKey(key)]) > 1
}

// buildSpeakers は話者表を作る。[Data.Entries] のファイル順がそのまま話者の
// 並び順になる。
func (d *Data) buildSpeakers() {
	d.speakers = make(map[string][]string, len(d.Entries))
	for _, e := range d.Entries {
		if e.Speaker == "" {
			continue
		}
		list := d.speakers[e.Key]
		// 話者は1キーあたり数人なので線形探索で足りる。元実装の
		// List<string>.Contains も同じ探索。
		if slices.Contains(list, e.Speaker) {
			continue
		}
		d.speakers[e.Key] = append(list, e.Speaker)
	}
}
