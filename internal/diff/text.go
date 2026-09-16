package diff

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/order"
)

// TextOptions は text 形式の出力の指定。
type TextOptions struct {
	// Root は表示するパスの基準。空なら絶対パスのまま出す。
	Root string
	// All は参考のカテゴリも1件ずつ並べるかどうか。既定は件数と理由だけ。
	All bool
	// Limit は1カテゴリに並べる上限。0 で全件。
	// 件数そのものは上限に関係なく必ず出る。
	Limit int
}

// textClipRunes は一覧に出す訳文・原文の長さの上限（文字数）。
// これを超える分は畳む。行を折り返さないのは、端末で目で追う道具だから。
const textClipRunes = 40

// WriteText は人が読む形式で報告を書く。
//
// 並べ方は 要作業 → 要確認 → 参考 に固定する。翻訳者が「まず何を見ればいいか」を
// 自分で決めなくて済むようにするため。--only のような絞り込みを用意せず、
// --all と --limit だけにしてあるのも同じ理由による。
func (r *Report) WriteText(w io.Writer, opt TextOptions) error {
	var b strings.Builder
	r.writeTextHeader(&b, opt)
	for _, sum := range r.Locales {
		r.writeTextLocale(&b, opt, sum)
	}
	r.writeTextFooter(&b)

	_, err := io.WriteString(w, b.String())
	return err
}

// writeTextHeader は全体の前置きを書く。
func (r *Report) writeTextHeader(b *strings.Builder, opt TextOptions) {
	orderPath := relPath(opt.Root, r.OrderPath)
	if orderPath == "" {
		orderPath = "（場所が分かりません）"
	}
	fmt.Fprintf(b, "再生順      %s   %d 行 / キー %d 種 / 台詞ID %d 件\n",
		orderPath, r.OrderRows, r.OrderKeys, r.OrderLineIDs)
	if r.OrderKeys == 0 || r.OrderLineIDs == 0 {
		// 再生順が読めていないと、「再生順に無い」を根拠にするカテゴリが
		// どれも成り立たない。判定を止めてあることを、件数より先に言う。
		b.WriteString("            再生順を読めていません。台本から消えた行などは判定しません。\n")
		b.WriteString("            data/script_order.csv の場所と、key 列・line_id 列を確かめてください。\n")
	}

	if len(r.Locales) < r.ReadLocales {
		fmt.Fprintf(b, "公開        %d ロケールを読みました（比較のため、報告しないロケールも読みます）\n", r.ReadLocales)
	} else {
		fmt.Fprintf(b, "公開        %d ロケールを読みました\n", r.ReadLocales)
	}
	if len(r.EmptyLocales) > 0 {
		// publish はこれらを対象にしない（書き出す元が無い）。この道具では
		// 「訳が1件も無い」という最大の要作業なので、必ず名前を出す。
		fmt.Fprintf(b, "            訳が1件もないロケール: %s\n", strings.Join(r.EmptyLocales, "、"))
	}
}

// writeTextLocale は1ロケール分を書く。
func (r *Report) writeTextLocale(b *strings.Builder, opt TextOptions, sum Summary) {
	b.WriteString("\n")
	fmt.Fprintf(b, "%s  %s   ハッシュ %d 行 / 台詞ID %d 行",
		sum.Locale, relPath(opt.Root, sum.PublishedPath), sum.HashRows, sum.LineRows)
	if sum.BrokenRows > 0 {
		// 公開ファイルにこの行があること自体がおかしい。直し方は validate が言うので
		// ここでは数だけ出して、そちらへ送る。
		fmt.Fprintf(b, " / 形も分からない %d 行（dwloc validate で確かめてください）", sum.BrokenRows)
	}
	b.WriteString("\n")

	if sum.HasWorking {
		fmt.Fprintf(b, "    作業コピー  %s   %d 行", relPath(opt.Root, sum.WorkingPath), sum.WorkingRows)
		if sum.SourceMissing > 0 {
			fmt.Fprintf(b, " / 原文が未取得 %d 行", sum.SourceMissing)
		}
		b.WriteString("\n")
	} else if sum.WorkingExists {
		fmt.Fprintf(b, "    作業コピー  読みませんでした（%s、--no-working）\n", relPath(opt.Root, sum.WorkingPath))
		b.WriteString("                未翻訳と publish で捨てられる行は判定しません。\n")
	} else {
		fmt.Fprintf(b, "    作業コピー  ありません（%s）\n", relPath(opt.Root, sum.WorkingPath))
		b.WriteString("                未翻訳と publish で捨てられる行は判定できません。\n")
		b.WriteString("                ゲーム内で作業コピーを書き出すと判定できるようになります。\n")
	}

	findings := r.localeFindings(sum.Locale)
	for _, status := range []Status{StatusTodo, StatusReview, StatusInfo} {
		fmt.Fprintf(b, "\n  %s\n", status)
		for _, c := range categories {
			if c.Status() != status {
				continue
			}
			writeCategory(b, opt, sum, c, findings[c], r.hintIgnoreFile(findings[c]))
		}
	}
}

// writeCategory はカテゴリ1つ分（件数の1行と、必要なら内訳）を書く。
func writeCategory(b *strings.Builder, opt TextOptions, sum Summary, c Category, list []Finding, hintIgnore bool) {
	label := pad(c.String(), categoryNameWidth())

	if !sum.canJudge(c) {
		fmt.Fprintf(b, "    %s判定していません（%s）\n", label, judgeBlockReason(sum, c))
		return
	}

	count := sum.Counts[c]
	listed := c.Status() != StatusInfo || opt.All
	suffix := ""
	if count > 0 && !listed && c != CatNotPublished {
		suffix = "（--all で一覧）"
	}
	fmt.Fprintf(b, "    %s%d 件%s\n", label, count, suffix)
	if count == 0 {
		return
	}

	for _, line := range c.detail() {
		fmt.Fprintf(b, "        %s\n", line)
	}
	switch c {
	case CatNotPublished:
		writeNodeBreakdown(b, list, hintIgnore)
	case CatScriptGap:
		writeScriptGapDetail(b, list)
	}
	if !listed {
		return
	}

	shown := list
	if opt.Limit > 0 && len(shown) > opt.Limit {
		shown = shown[:opt.Limit]
	}
	for _, f := range shown {
		fmt.Fprintf(b, "        %s\n", findingLine(f))
	}
	if rest := len(list) - len(shown); rest > 0 {
		fmt.Fprintf(b, "        （残り %d 件は --limit 0 で出ます）\n", rest)
	}
}

// judgeBlockReason は、そのカテゴリを判定しなかった理由を短く返す。
//
// 「0 件」と書けない場面はここに集まる。ファイルが無いのか、読まないと
// 指定されたのかを分けるのは、後者に「ゲーム内で書き出してください」と
// 促すと、すでに済んでいる作業をやり直させることになるため。
func judgeBlockReason(sum Summary, c Category) string {
	if c.needsWorking() && !sum.HasWorking {
		if sum.WorkingExists {
			return "作業コピーを読んでいません"
		}
		return "作業コピーがありません"
	}
	return "再生順を読めていません"
}

// writeNodeBreakdown は「どのロケールにも訳が無い行」をノード別に畳んで書く。
//
// 一覧にしないのは、実データで13ロケールとも同じ32件が出るため。毎日32件の
// 「できない作業」を並べると道具が信用されなくなる。28件は到達しない Unused
// ノードにあり、残りも Translations/ignore.txt の除外パターン（Yarn Spinner の
// サンプル文言）と符合する。
// hintIgnore はその ignore.txt の見当を添えるかどうか（[Report.hintIgnoreFile]）。
func writeNodeBreakdown(b *strings.Builder, list []Finding, hintIgnore bool) {
	type bucket struct {
		label string
		count int
	}
	index := make(map[string]int)
	var buckets []bucket
	unused := 0
	for _, f := range list {
		label := joinNonEmpty(" / ", f.Section, f.Node)
		if label == "" {
			label = "（位置が分かりません）"
		}
		if i, ok := index[label]; ok {
			buckets[i].count++
		} else {
			index[label] = len(buckets)
			buckets = append(buckets, bucket{label: label, count: 1})
		}
		if f.Section == order.UnusedSection {
			unused++
		}
	}
	// 件数の多い順。同数はノード名順にして、実行ごとに並びが変わらないようにする。
	sort.SliceStable(buckets, func(i, j int) bool {
		if buckets[i].count != buckets[j].count {
			return buckets[i].count > buckets[j].count
		}
		return buckets[i].label < buckets[j].label
	})

	width := 0
	for _, bk := range buckets {
		width = max(width, displayWidth(bk.label))
	}
	for _, bk := range buckets {
		fmt.Fprintf(b, "            %s%d\n", pad(bk.label, width+4), bk.count)
	}
	if unused > 0 {
		fmt.Fprintf(b, "        うち %d 件は Unused（今のゲームでは到達しません）です。\n", unused)
	}
	if hintIgnore {
		b.WriteString("        Translations/ignore.txt で記録の対象外になっている可能性があります。\n")
	}
}

// writeScriptGapDetail は「台本に無い台詞行」の説明を書く。
//
// 孤児とは断定しない。実データの17件は git 履歴の script_order.csv 全4版の
// どれにも載っておらず、消えたのではなく最初から載っていない。両方の可能性を書く。
func writeScriptGapDetail(b *strings.Builder, list []Finding) {
	var names []string
	seen := make(map[string]struct{}, len(list))
	for _, f := range list {
		if f.Speaker == "" {
			continue
		}
		if _, dup := seen[f.Speaker]; dup {
			continue
		}
		seen[f.Speaker] = struct{}{}
		names = append(names, f.Speaker)
	}
	sort.Strings(names)
	if len(names) > 0 {
		fmt.Fprintf(b, "        再生順にありませんが、%s の台詞として記録されています。\n", strings.Join(names, "、"))
	}
	b.WriteString("        data/script_order.csv がゲームの台詞を全部は持っていないためか、\n")
	b.WriteString("        原文が変わって取り残された訳です。公開ファイルだけでは決められません。\n")
}

// writeTextFooter は締めの1行を書く。
func (r *Report) writeTextFooter(b *strings.Builder) {
	b.WriteString("\n")
	if len(r.Locales) == 0 {
		b.WriteString("報告するロケールがありません。\n")
		return
	}
	if n := r.CountByStatus(StatusReview); n > 0 {
		fmt.Fprintf(b, "要確認が %d 件あります。\n", n)
		return
	}
	b.WriteString("要確認はありません。（--all で内訳、--strict で要作業も終了コード 1）\n")
}

// localeFindings はそのロケールの Finding をカテゴリ別に分ける。
func (r *Report) localeFindings(locale string) map[Category][]Finding {
	out := make(map[Category][]Finding, len(categories))
	for _, f := range r.Findings {
		if f.Locale != locale {
			continue
		}
		out[f.Category] = append(out[f.Category], f)
	}
	return out
}

// findingLine は一覧の1行を組み立てる。
//
//	L01 Ryan / Ryan_1_PhoneTutorial / 3   Phone   1e3f4c...   訳: もしもし？
//
// キーは省略せず16桁そのまま出す。公開ファイルを開いて grep する相手なので、
// 縮めると使えない。
func findingLine(f Finding) string {
	parts := []string{}

	pos := joinNonEmpty(" / ", f.Section, f.Node, f.OrderText)
	if pos == "" {
		pos = "（位置が分かりません）"
	}
	parts = append(parts, pos)
	if f.Speaker != "" {
		parts = append(parts, f.Speaker)
	}
	parts = append(parts, f.Key)
	switch {
	case f.Translation != "":
		parts = append(parts, "訳: "+clip(f.Translation))
	case f.SourceEn != "":
		parts = append(parts, "原文: "+clip(f.SourceEn))
	}
	// カテゴリ共通の理由は見出しに書いてあるので、行ごとに違う理由だけ足す。
	if f.Note != "" && f.Note != f.Category.note() {
		parts = append(parts, "（"+f.Note+"）")
	}
	return strings.Join(parts, "   ")
}

// joinNonEmpty は空でない要素だけを sep で連ねる。
func joinNonEmpty(sep string, values ...string) string {
	kept := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" {
			kept = append(kept, v)
		}
	}
	return strings.Join(kept, sep)
}

// clip は長い文字列を畳む。改行とタブは1行に収まるよう空白に置き換える。
func clip(v string) string {
	v = strings.ReplaceAll(v, "\r\n", " ")
	v = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(v)
	runes := []rune(v)
	if len(runes) <= textClipRunes {
		return v
	}
	return string(runes[:textClipRunes]) + "…"
}

// categoryNameWidth はカテゴリ名の欄の幅。全カテゴリ名がそろう幅に余白を足す。
func categoryNameWidth() int {
	width := 0
	for _, c := range categories {
		width = max(width, displayWidth(c.String()))
	}
	return width + 4
}

// pad は表示幅が width になるまで右に空白を足す。既に超えていればそのまま返す。
func pad(v string, width int) string {
	if n := width - displayWidth(v); n > 0 {
		return v + strings.Repeat(" ", n)
	}
	return v
}

// displayWidth は等幅端末での表示幅を数える。日本語などの全角文字を2として数える。
//
// 標準ライブラリに東アジアの文字幅の表が無いので、範囲の表で近似する。
// 桁がずれても意味は変わらない（見た目の問題）ので、外部依存を足してまで
// 正確にする価値は無いと判断した。
func displayWidth(v string) int {
	width := 0
	for _, r := range v {
		if isWide(r) {
			width += 2
			continue
		}
		width++
	}
	return width
}

// isWide は全角として数える文字かを返す。
func isWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F: // ハングル字母
		return true
	case r >= 0x2E80 && r <= 0x303E: // CJK部首・かな補助・記号
		return true
	case r >= 0x3041 && r <= 0x33FF: // かな・ハングル・CJK互換
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK拡張A
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // CJK統合漢字
		return true
	case r >= 0xA000 && r <= 0xA4CF: // イ文字
		return true
	case r >= 0xAC00 && r <= 0xD7A3: // ハングル音節
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK互換漢字
		return true
	case r >= 0xFE30 && r <= 0xFE6F: // CJK互換形・小字形
		return true
	case r >= 0xFF00 && r <= 0xFF60: // 全角英数・記号
		return true
	case r >= 0xFFE0 && r <= 0xFFE6: // 全角記号
		return true
	case r >= 0x20000 && r <= 0x3FFFD: // CJK拡張B以降
		return true
	default:
		return false
	}
}

// relPath は報告に出すパスを root からの相対にしてスラッシュ区切りで返す。
//
// 手元の絶対パスには利用者名が入ることがあり、CIのログや不具合報告へ貼られると
// そのまま漏れる。cmd/dwloc の displayPath と同じ方針だが、あちらは main
// パッケージにあるので参照できない。
func relPath(root, path string) string {
	if path == "" {
		return ""
	}
	if root == "" {
		return filepath.ToSlash(path)
	}
	absRoot, rootErr := filepath.Abs(root)
	absPath, pathErr := filepath.Abs(path)
	if rootErr != nil || pathErr != nil {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return filepath.ToSlash(path)
	}
	// filepath.Rel は ".." を含む結果も成功で返すので、外へ出たものは自分で弾く。
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// hintIgnoreFile は「どのロケールにも訳が無い行」に ignore.txt の見当を
// 添えてよいかを返す。
//
// 添えないのは、再生順のキーが丸ごとこのカテゴリに出ているとき。どのロケールも
// まだ何も公開していない状態では全キーがここへ落ちるのが当たり前で、除外
// パターンとは何の関係も無い。実データのように1602種のうち32件だけが残るときに、
// 初めて意味のある見当になる。
func (r *Report) hintIgnoreFile(list []Finding) bool {
	return r.OrderKeys > 0 && len(list) < r.OrderKeys
}
