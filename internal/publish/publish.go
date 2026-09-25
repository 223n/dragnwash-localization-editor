package publish

import (
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/order"
)

const (
	// HeaderLine は公開CSVの1行目。入力のヘッダーが何であれ常にこの6列
	// （移植仕様「公開CSV生成 R22」）。
	HeaderLine = "key,section,node,order,speaker,translation"

	// CommentPrefix は公開CSVのコメント行の先頭文字。この文字で始まる行は
	// 読み込み時に落とされる（csvfile.ReadPowerShell）。
	CommentPrefix = "#"

	// SectionMarker はセクション見出しの印。行は "# ===== 文言 =====" の形になる。
	SectionMarker = "# ====="
	// NodeMarker はノード見出しの印。行は "# --- 文言 ---" の形になる。
	NodeMarker = "# ---"

	// UISectionName は再生順に無かったキーの section 列に入る値（R25）。
	UISectionName = "UI"
	// UIFallbackSpeaker は入力に speaker が無かった UI 行の speaker 列に入る値（R25）。
	UIFallbackSpeaker = "UI"
	// UISectionTitle は UI 行の前に出す見出しの文言（R25）。
	UISectionTitle = "UI and other text (not part of the dialogue script)"
	// OrphanLineSectionTitle は再生順に置けなかった台詞ID行の見出しの文言（R26）。
	OrphanLineSectionTitle = "Per-line translations not found in the script order"

	// nodeTitlePhaseSeparator は phase をノード名の前に置くときの区切り。
	nodeTitlePhaseSeparator = ": "
	// nodeTitleConditionPrefix は condition をノード名の後ろに置くときの前置き。
	nodeTitleConditionPrefix = " | if "
)

// 入力の列名。照合は大文字小文字を区別しない（csvfile.Row の性質）。
// ヘッダーが "Key,Source_EN,Translation,Speaker" の作業コピーでも読める
// （移植仕様「公開CSV生成 / 敵対検証」[high]）。
const (
	colKey         = "key"
	colSourceEn    = "source_en"
	colTranslation = "translation"
	colSpeaker     = "speaker"
)

// inputRow は入力から採用した1行。どちらの値もトリムしない（移植仕様 R19）。
type inputRow struct {
	Speaker     string
	Translation string
}

// collected は入力CSVを1回走査して集めた結果。
type collected struct {
	// rows はキー（16桁の小文字16進）から採用した行への対応。
	//
	// 元実装は大文字小文字を区別しない Hashtable だが、入る側のキーは
	// ハッシュ結果か小文字化済みの16桁16進、引く側も order.NormalizeKey 済みで、
	// どちらも必ず小文字。素のマップで等価になる。
	rows map[string]inputRow
	// inputOrder は rows への登録順＝入力ファイルでの登場順。R25 の出力順になる。
	inputOrder []string
	// lines は台詞ID行の表。
	lines *lineTable
	// stats は走査中に数えた集計。
	stats Stats
}

// collect は入力CSVを走査して出力に使う材料を集める（移植仕様 R11〜R21）。
//
// 各行の処理順は元実装のとおりで、入れ替えてはいけない:
//
//	台詞ID判定 → 小文字化 → ハッシュ照合/16桁判定（ここでカウンタ加算）
//	→ 重複キー判定 → 訳の空判定 → 採用
//
// 重複判定と空判定がカウンタ加算より後ろにあるため、出力されない行も
// Converted / Kept に数えられる（R18 の goNote）。また空の訳の行は rows に
// 入らないので、同じキーで「空が先、訳ありが後」でも後の行が採用される（R20）。
//
// 読み方は上流 main と同じく全体を解釈する（[csvfile.ReadPowerShell]）。引用符で
// 囲んだ値は物理行をまたいで1つの値になり、値の中の改行は読んだとおりに書く
// （CRLF も LF も直さない。上流とバイト一致させるため）。閉じない引用符は誤りに
// なるが、呼び出し側（cmd/dwloc と画面の書き出し）は組み立てより前に形の確かめ
// （[CheckTargetShape]）を通すので、ふつうはここまで来ない。
func collect(inputCSV []byte) (*collected, error) {
	f, err := csvfile.ReadPowerShell(inputCSV)
	if err != nil {
		return nil, err
	}
	records := f.Rows()
	c := &collected{
		rows:  make(map[string]inputRow, len(records)),
		lines: newLineTable(),
	}
	for _, rec := range records {
		k, how := rowKey(rec)
		switch how {
		case keyLineID:
			// 台詞ID行はどのカウンタにも入らない（R12）。
			if tr := rec.Get(colTranslation); tr != "" {
				c.lines.Add(k, tr)
			}
			continue
		case keyConverted:
			c.stats.Converted++
		case keyKept:
			c.stats.Kept++
		default:
			c.stats.Dropped++ // R15 / R17
			continue
		}

		if _, dup := c.rows[k]; dup {
			continue // R18。先勝ち
		}
		who := rec.Get(colSpeaker)    // R19
		tr := rec.Get(colTranslation) // R19
		if tr == "" {
			// R20。未翻訳の行は公開しない。
			//
			// 元実装は `$tr -eq ''` で、これは InvariantCultureIgnoreCase の比較。
			// U+00AD や U+200D のように照合上無視される文字だけからなる訳も
			// 「空」と判定して捨てる（pwsh 7.6.6 で実測）。Go では照合表を持てない
			// ので序数比較のままにしてある。そうした訳は捨てずに公開する向きに
			// ずれるが、実データの13ロケールには該当する行が無い。
			continue
		}
		c.rows[k] = inputRow{Speaker: who, Translation: tr} // R21
		c.inputOrder = append(c.inputOrder, k)
	}
	return c, nil
}

// keyOutcome は入力の1行のキーをどう決めたか。集計の加算はこの値で分ける。
type keyOutcome int

const (
	// keyDropped は形式が合わずに捨てる行（移植仕様 R15 / R17）。
	keyDropped keyOutcome = iota
	// keyLineID は台詞ID行（R12）。キーは入力にあった綴りのまま。
	keyLineID
	// keyConverted は source_en からキーを計算した行（R14 / R15）。
	keyConverted
	// keyKept は source_en が無く、key が既に16桁キーだった行（R16）。
	keyKept
)

// rowKey は入力の1行からキーを決める（移植仕様 R11〜R17）。
//
// 判定の順は元実装のとおりで、入れ替えてはいけない。台詞ID判定が先にあるので、
// 台詞ID行はハッシュ処理へ進まない。
//
// [collect] から切り出してあるのは、同じ規則を [CheckLoss] も使うためである。
// いまの公開ファイルのどの行がどのキーで引かれるかは、この関数が決めたとおりで
// なければならない。2か所に書くと、守りが見ているキーと publish が書くキーが
// 食い違い、「失われないはずのものが失われた」と報せる側へも、その逆へもずれる。
func rowKey(rec csvfile.Row) (string, keyOutcome) {
	// key 列だけトリムする。source_en / translation / speaker はトリムしない
	// （移植仕様 R11 / 境界条件「トリムの非対称性」）。
	// 列が無い場合は空文字になるが、この先で空文字を key.For へ渡す経路は
	// 無いので key.HashOfEmpty が紛れ込むことはない。
	rawKey := strings.TrimSpace(rec.Get(colKey))
	if key.LooksLikeLineID(rawKey) {
		return rawKey, keyLineID // R12
	}

	k := strings.ToLower(rawKey) // R13
	src := rec.Get(colSourceEn)  // R14。トリムしない
	switch {
	case src != "":
		// R15。不一致は「ハッシュで上書き」ではなく「行を捨てる」。
		// source_en が書き換えられたのに古いキーが残っている行を見つけるための仕掛け。
		hashed := key.For(src)
		if k != "" && k != hashed {
			return "", keyDropped
		}
		return hashed, keyConverted
	case key.LooksLike(k):
		// R16。元実装の `-match '^[0-9a-f]{16}$'` と同じ判定。
		return k, keyKept
	default:
		return "", keyDropped // R17
	}
}

// Keyless は、publish がレコード rec のキーを決められずに捨てるか（移植仕様 R17）を
// 返す。原文（source_en。トリムしない）が空で、key 列（前後の空白を除く）が台詞ID でも、
// 小文字にして16桁のキーの形でもないレコードで、訳を書いても公開されない。source_en
// 列の無い形（公開ファイルの6列・3列・2列）では、原文は空と同じに見る。
//
// 原文があって key 列と合わないレコード（R15）は含めない。それも捨てられるが、原文の
// 書き換えに古いキーが追いついていない行を見つけるための仕掛けで、diff が「捨てられる
// 行」として別に知らせる。
//
// 画面の保存（internal/edit）は、これに当たるレコードを編集させない。判定は [rowKey]
// に任せ、規則を2か所に書かない。書くと、編集させない行と publish が捨てる行
// （集計の malformed dropped）が食い違う。
func Keyless(rec csvfile.Row) bool {
	_, how := rowKey(rec)
	return how == keyDropped && rec.Get(colSourceEn) == ""
}

// Build は公開CSVのバイト列を1ファイル分組み立てる。
//
// data は script_order.csv と level_flow.csv を読んだもの。nil を渡すと再生順が
// 空の場合と同じ扱いになり、見出しは一切出ず、採用した行は全て末尾へ回る
// （移植仕様「境界条件」）。
//
// inputCSV は変換元（作業コピーか公開ファイル自身）。existingCSV は既存の公開
// ファイルで、ヘッダー直下のコメントを写すためだけに使う（R23）。ファイルが
// 無いときは nil を渡す。入力と出力が同じファイルのときは、同じバイト列を
// 両方に渡してよい（元実装も同じファイルを2回読む）。
//
// エラーを返すのは、入力のヘッダー列名が重複しているとき（元実装も
// ConvertFrom-Csv の例外で処理全体が止まる。R29）と、入力に閉じない引用符が
// あるとき（[csvfile.UnclosedQuoteError]。元実装は後ろを飲み込んで書く）だけ。
func Build(data *order.Data, inputCSV, existingCSV []byte) ([]byte, Stats, error) {
	c, err := collect(inputCSV)
	if err != nil {
		return nil, Stats{}, err
	}
	if data == nil {
		data = &order.Data{}
	}

	out := make([]byte, 0, len(inputCSV)+len(existingCSV))
	out = appendRaw(out, HeaderLine) // R22
	for _, line := range HeaderComments(existingCSV) {
		out = appendRaw(out, line) // R23
	}

	out, done := appendPlayOrder(out, data, c) // R24
	out, leftCount := appendLeftovers(out, data, c, done)
	out = appendOrphanLines(out, c) // R26

	c.stats.InPlayOrder = len(done)
	c.stats.Other = leftCount
	return out, c.stats, nil
}

// appendPlayOrder は再生順に沿って本体を書く（移植仕様 R24）。
// 返す集合は「再生順の中に出力できたキー」で、元実装の $done にあたる。
func appendPlayOrder(out []byte, data *order.Data, c *collected) ([]byte, map[string]struct{}) {
	done := make(map[string]struct{}, len(c.rows))

	// 見出しの再出力判定は「直前に出力した行」との比較で行う。過去に出したことが
	// あるかは見ない（移植仕様「敵対検証」[high] R24）。実データには同一セクション内で
	// ノードが飛び飛びに再登場する箇所が2件（L08 Conrad / Conrad_finished_jerkoff_3、
	// L13 Ryan / Ryan_5_intro）あり、seen 集合で書くと全ロケールで2行足りなくなる。
	//
	// 比較は3値で行う。元実装の $lastSection / $lastNode の初期値は $null で、
	// PowerShell の -ne は $null を空文字と別物として扱うため、値が
	// 「列が無い（$null）」「空文字」「それ以外」の3通りに割れる
	// （[nullableDiffers] 参照）。
	var last nullableText
	var lastNode nullableText

	for _, e := range data.Entries {
		_, inRows := c.rows[e.Key]
		_, alreadyDone := done[e.Key]
		hashRow := inRows && !alreadyDone

		translation, found := c.lines.Lookup(e.LineID)
		lineRow := e.LineID != "" && found

		// 見出しの挿入より先に判定する。1行も書かないセクション／ノードの
		// 見出しは出ない（R24a）。$lastNode も、実際に行を書いた行でしか更新されない。
		if !hashRow && !lineRow {
			continue
		}

		section := nullableText{value: e.Section, set: e.HasSection}
		node := nullableText{value: e.Node, set: e.HasNode}

		if nullableDiffers(section, last) {
			out = appendRaw(out, "")
			out = appendRaw(out, sectionHeader(data.SectionTitle(e.Section)))
			last = section
			// セクションが変わるとノードの記憶は $null に戻る。別セクションに
			// 同名ノードが現れれば見出しは再度出る（R24c）。
			lastNode = nullableText{}
		}
		if nullableDiffers(node, lastNode) {
			out = appendRaw(out, nodeHeader(nodeTitle(e)))
			lastNode = node
		}

		// ハッシュ行と台詞ID行の両方が該当する場合はハッシュ行が先。
		// 同じ order 番号の2行が続けて出る（R24i）。
		if hashRow {
			row := c.rows[e.Key]
			out = appendRow(out, e.Key, e.Section, e.Node, e.OrderText, speakerFor(data, e, row), row.Translation)
			done[e.Key] = struct{}{}
		}
		if lineRow {
			// 台詞ID行の speaker は入力の値ではなく script_order のその行の値（R24g）。
			out = appendRow(out, e.LineID, e.Section, e.Node, e.OrderText, e.Speaker, translation)
			c.lines.Remove(e.LineID)
			c.stats.LineKept++
		}
	}
	return out, done
}

// appendLeftovers は再生順に無かったキーを末尾へ書き、書いた件数を返す（移植仕様 R25）。
//
// 見出しと section='UI' はどちらも「再生順が1件以上読めていること」に依存する。
// 再生順が空なら見出しも出ず、section 列は空文字になる。
func appendLeftovers(out []byte, data *order.Data, c *collected, done map[string]struct{}) ([]byte, int) {
	left := make([]string, 0, len(c.inputOrder))
	for _, k := range c.inputOrder {
		if _, ok := done[k]; ok {
			continue
		}
		left = append(left, k)
	}
	if len(left) == 0 {
		return out, 0
	}

	hasOrder := len(data.Entries) > 0
	if hasOrder {
		out = appendRaw(out, "")
		out = appendRaw(out, sectionHeader(UISectionTitle))
	}
	section := ""
	if hasOrder {
		section = UISectionName
	}
	for _, k := range left {
		row := c.rows[k]
		who := row.Speaker
		if who == "" {
			who = UIFallbackSpeaker
		}
		// 元実装は section をエスケープに通さないが、"UI" も空文字も
		// エスケープ対象文字を含まないので結果は変わらない。
		out = appendRow(out, k, section, "", "", who, row.Translation)
	}
	return out, len(left)
}

// appendOrphanLines は再生順に置けなかった台詞ID行を末尾へ書く（移植仕様 R26）。
// この見出しだけは再生順の有無に関係なく出る。
func appendOrphanLines(out []byte, c *collected) []byte {
	remaining := c.lines.Remaining()
	if len(remaining) == 0 {
		return out
	}
	out = appendRaw(out, "")
	out = appendRaw(out, sectionHeader(OrphanLineSectionTitle))
	for _, e := range remaining {
		out = appendRow(out, e.id, "", "", "", "", e.translation)
		c.stats.LineKept++
	}
	return out
}

// HeaderComments は既存の公開ファイルから、ヘッダー直下のコメント行を取り出す
// （移植仕様 R23）。言語名・暫定訳の注意書き・クレジットを引き継ぐための処理。
//
// 1行目を飛ばして順に見ていき、次のいずれかに当たった時点で打ち切る:
//
//	'#' で始まらない行（空行を含む）
//	"# =====" で始まる行（セクション見出し）
//	"# ---" で始まる行（ノード見出し）
//
// 打ち切り条件は OR なので、空行が来た時点で終わる。実データでは
// de/strings.csv が4行引き継ぎ、ja/strings.csv は2行目が空行なので0行になる。
//
// 判定は行の生の先頭一致で、トリムしない。返す行に改行は含まない。
// 元実装の StartsWith がカルチャ依存であることは再現しない（理由は
// csvfile.ReadPowerShellRows の同じ判定に付けた注記と同じ）。
func HeaderComments(existingCSV []byte) []string {
	lines := csvfile.SplitNetLines(csvfile.TrimBOMString(string(existingCSV)))
	if len(lines) < 2 {
		return nil
	}
	var out []string
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, CommentPrefix) ||
			strings.HasPrefix(line, SectionMarker) ||
			strings.HasPrefix(line, NodeMarker) {
			break
		}
		out = append(out, line)
	}
	return out
}

// speakerFor はハッシュ行の speaker 列の値を決める（移植仕様 R24e）。
//
// 優先順:
//
//	(1) script_order 由来の全話者を "/" で連結した値
//	(2) 入力行の speaker
//	(3) その script_order 行の speaker
//
// (1) は話者表にキーがあれば無条件に使う。表に載るのは speaker が非空の行が
// 1つでもあるときだけで、そのとき連結結果が空文字になることはない。したがって
// order.Data.SpeakersFor が空文字を返すことと「表に無い」ことは同値になり、
// 元実装の ContainsKey 判定と一致する。
func speakerFor(data *order.Data, e order.Entry, row inputRow) string {
	if who := data.SpeakersFor(e.Key); who != "" {
		return who
	}
	if row.Speaker != "" {
		return row.Speaker
	}
	return e.Speaker
}

// nodeTitle はノード見出しの文言を組み立てる（移植仕様 R24d）。
//
//	intro: Ryan_1_intro
//	Alexander_SexScene                                         （Cutscene は phase が空）
//	cum: Conrad_Outro_3 | if $conrad_jerked_off_3 $conrad_used_mount_3
func nodeTitle(e order.Entry) string {
	var b strings.Builder
	if e.Phase != "" {
		b.WriteString(e.Phase)
		b.WriteString(nodeTitlePhaseSeparator)
	}
	b.WriteString(e.Node)
	if e.Condition != "" {
		b.WriteString(nodeTitleConditionPrefix)
		b.WriteString(e.Condition)
	}
	return b.String()
}

// sectionHeader はセクション見出しの1行を作る。
func sectionHeader(title string) string {
	return SectionMarker + " " + title + " ====="
}

// nodeHeader はノード見出しの1行を作る。
func nodeHeader(title string) string {
	return NodeMarker + " " + title + " ---"
}

// appendRow は本体の1行を dst に足す。
//
// key と order はCSVエスケープを通さず素のまま連結する（移植仕様 R24f）。
// 現行データは16桁16進・"line:xxxxxxxx"・数字だけなので問題にならないが、
// カンマや引用符を含む値が来ると出力CSVが壊れる。元実装がそうなっているので
// ここでも直さない。直すと既存の公開ファイルとバイト非互換になる。
func appendRow(dst []byte, k, section, node, orderText, speaker, translation string) []byte {
	dst = append(dst, k...)
	dst = append(dst, ',')
	dst = append(dst, csvfile.EscapeField(section)...)
	dst = append(dst, ',')
	dst = append(dst, csvfile.EscapeField(node)...)
	dst = append(dst, ',')
	dst = append(dst, orderText...)
	dst = append(dst, ',')
	dst = append(dst, csvfile.EscapeField(speaker)...)
	dst = append(dst, ',')
	dst = append(dst, csvfile.EscapeField(translation)...)
	return append(dst, csvfile.LineTerminator...)
}

// appendRaw は1行をエスケープせずそのまま足す。見出し行とコメント行に使う。
func appendRaw(dst []byte, line string) []byte {
	dst = append(dst, line...)
	return append(dst, csvfile.LineTerminator...)
}

// nullableText は「列が無い（$null）」と「値が空文字」を区別して持つ文字列。
// set が false のときが $null にあたる。
type nullableText struct {
	value string
	set   bool
}

// nullableDiffers は元実装の `$e.section -ne $lastSection` と同じ判定を返す。
//
// pwsh 7.6.6 での実測にもとづく:
//
//	$null -ne $null   -> False   どちらも列が無い。見出しは出ない
//	''    -ne $null   -> True    空文字と $null は別物。見出しが出る
//	'x'   -ne 'X'     -> False   大文字小文字は区別しない
//
// ヘッダーに無い列のプロパティは $null になるので、section 列そのものが無い
// script_order.csv では見出しが1行も出ない。初期値 $lastSection も $null なので
// 先頭行でも出ない。
func nullableDiffers(a, b nullableText) bool {
	if a.set != b.set {
		return true
	}
	if !a.set {
		return false
	}
	return !equalFold(a.value, b.value)
}

// equalFold は見出しの変化判定に使う文字列比較。
//
// 元実装の比較は PowerShell の `-ne` で、大文字小文字を区別しない
// （移植仕様「境界条件」）。実データの section / node は ASCII なので
// csvfile.FoldASCII による畳み込みで等価になる。
//
// 正確には PowerShell の -eq / -ne は InvariantCultureIgnoreCase なので、
// 結合文字列（"e"+U+0301）と合成済み文字（U+00E9）も等しいと判定する。
// Go の標準ライブラリだけでは照合表を持てないため、そこまでは再現しない。
// 実データの section / node は ASCII の識別子なので影響しない。
func equalFold(a, b string) bool {
	return a == b || csvfile.FoldASCII(a) == csvfile.FoldASCII(b)
}
