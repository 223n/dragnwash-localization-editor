package diff

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/order"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
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
	switch {
	case r.OrderUnclosed > 0:
		// 閉じない引用符で再生順を1行も使えない。下の「読めていません」より先に、
		// どこを直せばよいかを言う。どのカテゴリも判定しないので、「台本から
		// 消えた行など」と絞った言い方はしない。
		fmt.Fprintf(b, "            %d行目で開いた引用符がファイルの終わりまで閉じないので、再生順を読めません。\n", r.OrderUnclosed)
		b.WriteString("            直すまで、どのカテゴリも判定しません。\n")
	case r.OrderKeys == 0:
		// 再生順が読めていないと、「再生順に無い」を根拠にするカテゴリが
		// どれも成り立たない。判定を止めてあることを、件数より先に言う。
		b.WriteString("            再生順を読めていません。台本から消えた行などは判定しません。\n")
		b.WriteString("            data/script_order.csv の場所と、key 列・line_id 列を確かめてください。\n")
	case r.OrderLineIDs == 0:
		// キーは読めていて、台詞IDだけが無い。台本から消えた行はキーだけで
		// 判定でき、件数も出る。上と同じく「台本から消えた行などは判定しません」と
		// 書くと、すぐ下の件数と食い違う。止まるのは台詞IDを要るカテゴリだけなので、
		// そのカテゴリを名指しする。
		fmt.Fprintf(b, "            再生順に台詞ID (line_id) がありません。%sは判定しません。\n",
			lineIDCategoryNames())
		b.WriteString("            data/script_order.csv の line_id 列を確かめてください。\n")
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

// lineIDCategoryNames は、再生順の台詞IDが無いと判定できないカテゴリの名前を
// 「」で囲み、表示順に「と」でつないで返す。
//
// 名前を文面に書き込まず表（[categoryTable] の needsOrderLineIDs）から引くのは、
// 印を足し引きしたときに、見出しだけが古い名指しのまま残らないようにするため。
// どのカテゴリかは [OrderLineIDCategories] に尋ねる。画面の断り書きも同じ関数から
// 引くので、2つの名指しはずれない。
func lineIDCategoryNames() string {
	var names []string
	for _, c := range OrderLineIDCategories() {
		names = append(names, "「"+c.String()+"」")
	}
	return strings.Join(names, "と")
}

// writeTextLocale は1ロケール分を書く。
func (r *Report) writeTextLocale(b *strings.Builder, opt TextOptions, sum Summary) {
	b.WriteString("\n")
	if sum.PublishedUnclosed > 0 {
		// 行を1つも使っていないので、「ハッシュ 0 行」とは書かない。0 行と書くと、
		// 公開ファイルが空だと読まれる。
		fmt.Fprintf(b, "%s  %s   %s\n", sum.Locale, relPath(opt.Root, sum.PublishedPath),
			unclosedText(sum.PublishedUnclosed))
		b.WriteString("    このロケールは、直すまでどのカテゴリも判定しません。\n")
	} else {
		fmt.Fprintf(b, "%s  %s   ハッシュ %d 行 / 台詞ID %d 行",
			sum.Locale, relPath(opt.Root, sum.PublishedPath), sum.HashRows, sum.LineRows)
		if sum.BrokenRows > 0 {
			// 公開ファイルにこの行があること自体がおかしい。直し方は validate が言うので
			// ここでは数だけ出して、そちらへ送る。
			fmt.Fprintf(b, " / 形も分からない %d 行（dwloc validate で確かめてください）", sum.BrokenRows)
		}
		b.WriteString("\n")
	}

	switch {
	case sum.WorkingUnclosed > 0:
		// 読まなかったのでも無いのでもない。「--no-working」や「書き出してください」と
		// 書くと、直す先を取り違える。
		fmt.Fprintf(b, "    作業コピー  %s   %s\n", relPath(opt.Root, sum.WorkingPath), unclosedText(sum.WorkingUnclosed))
		b.WriteString("                未翻訳と publish で捨てられる行などは判定しません。\n")
	case sum.HasWorking:
		fmt.Fprintf(b, "    作業コピー  %s   %d 行", relPath(opt.Root, sum.WorkingPath), sum.WorkingRows)
		if sum.SourceMissing > 0 {
			fmt.Fprintf(b, " / 原文が未取得 %d 行", sum.SourceMissing)
		}
		b.WriteString("\n")
	case sum.WorkingExists:
		fmt.Fprintf(b, "    作業コピー  読みませんでした（%s、--no-working）\n", relPath(opt.Root, sum.WorkingPath))
		b.WriteString("                未翻訳と publish で捨てられる行は判定しません。\n")
	default:
		fmt.Fprintf(b, "    作業コピー  ありません（%s）\n", relPath(opt.Root, sum.WorkingPath))
		b.WriteString("                未翻訳と publish で捨てられる行は判定できません。\n")
		b.WriteString("                ゲーム内で作業コピーを書き出すと判定できるようになります。\n")
	}

	// はみ出しの記録は、作業コピーが無くても読めることがある（ゲーム内で
	// レイアウトの検査だけを走らせた場合）。読めたときだけ出す。無いほうが
	// 普通なので、無いことをここで毎回言わない。判定できないことは
	// カテゴリの行が言う。閉じない引用符で読めなかったときは、直す先を言うために出す。
	switch {
	case sum.LayoutRisksUnclosed > 0:
		fmt.Fprintf(b, "    はみ出しの記録  %s   %s\n",
			relPath(opt.Root, sum.LayoutRisksPath), unclosedText(sum.LayoutRisksUnclosed))
	case sum.HasLayoutRisks:
		fmt.Fprintf(b, "    はみ出しの記録  %s   %d 行\n",
			relPath(opt.Root, sum.LayoutRisksPath), sum.LayoutRiskRows)
	}

	findings := r.localeFindings(sum.Locale)
	for _, status := range []Status{StatusTodo, StatusReview, StatusInfo} {
		fmt.Fprintf(b, "\n  %s\n", status)
		for _, c := range categories {
			if c.Status() != status {
				continue
			}
			writeCategory(b, opt, sum, c, findings[c], findings[CatCarryover], r.hintIgnoreFile(findings[c]))
		}
	}
}

// writeCategory はカテゴリ1つ分（件数の1行と、必要なら内訳）を書く。
func writeCategory(b *strings.Builder, opt TextOptions, sum Summary, c Category, list, carry []Finding, hintIgnore bool) {
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
	case CatVanished:
		writeCarryHint(b, sum, list, carry)
	case CatCarryover:
		writeCarryBreakdown(b, sum)
	case CatNotPublished:
		writeNodeBreakdown(b, list, hintIgnore)
	case CatScriptGap:
		writeScriptGapDetail(b, list)
	}
	if !listed {
		return
	}

	shown := list
	if c == CatCarryover {
		shown = carryCopiedFirst(shown)
	}
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
//
// 見る順は [Summary.canJudge] と同じにしてある。引き継ぎ候補は再生順と旧再生順の
// 両方を要るので、順が食い違うと「再生順は読めているのに再生順を読めていません」
// と書くことになる。
//
// 再生順については、キーが無いのか、台詞IDや norm 列だけが無いのかで理由を分ける。
// 台詞IDだけが無いとき、台本から消えた行などはキーだけで判定でき、件数も出る。
// そこで「再生順を読めていません」と書くと、その件数と食い違う。text 形式は見出しで
// 補えるが、csv の標準エラー（cmd/dwloc の warnHeldCategories）には見出しが無く、
// この文面だけが出る。キーも無いときは、台詞IDだけを要るカテゴリ（台本に無い
// 台詞ID行）も norm 列を要るカテゴリ（引き継ぎ元の候補）も「再生順を読めていません」に
// する。見出しと画面の断り書きがそう書くので、ここだけ台詞IDや norm 列のことを言うと、
// その列だけを直しに行かせることになる。norm はキーのある行からしか拾わないので、
// キーが無ければ norm も必ず無い。
//
// norm 列の確かめを再生順のまとまりに入れたので、見る順は canJudge（キー → 台詞ID →
// norm 列 → はみ出しの記録）と同じになった。
//
// 文字列ではなく [reason.Reason] を返すのは、画面（internal/web）が目録で文面を
// 差し替えるためである。日本語の文面は Text に入ったまま残るので、この関数を
// %s で書式に渡す CLI 側は、文字列を返していたころと同じ文面を出す。
func judgeBlockReason(sum Summary, c Category) reason.Reason {
	if why, ok := unclosedBlockReason(sum, c); ok {
		return why
	}
	if c.needsWorking() && !sum.HasWorking {
		if sum.WorkingExists {
			return reason.New(reason.JudgeWorkingNotRead, "作業コピーを読んでいません")
		}
		return reason.New(reason.JudgeWorkingMissing, "作業コピーがありません")
	}
	if (c.needsOrderKeys() && !sum.OrderKeys) || (c.needsOrderLineIDs() && !sum.OrderLineIDs) ||
		(c.needsOrderNorms() && !sum.OrderNorms) {
		if !sum.OrderKeys {
			return reason.New(reason.JudgeOrderUnreadable, "再生順を読めていません")
		}
		if c.needsOrderLineIDs() && !sum.OrderLineIDs {
			return reason.New(reason.JudgeOrderNoLineIDs, "再生順に台詞ID (line_id) がありません")
		}
		return reason.New(reason.JudgeOrderNoNorms, "再生順に norm 列がありません")
	}
	if c.needsLayoutRisks() && !sum.HasLayoutRisks {
		if sum.LayoutRisksExist {
			return reason.New(reason.JudgeLayoutRisksNotRead,
				"はみ出しの記録を読んでいません")
		}
		return reason.New(reason.JudgeNoLayoutRisks,
			"ゲーム内で Check translation layout を走らせた記録がありません")
	}
	if c.needsOldOrder() && (!sum.OldOrder || sum.OldOrderStale) {
		if sum.OldOrderReason != "" {
			// 旧再生順を取り出せない理由は load.go が作る。識別子も一緒に
			// 運ばれてくるので、ここで当て直さない。当て直すと、番兵を見る
			// 場所が2つになる。
			return reason.New(sum.OldOrderReasonID, sum.OldOrderReason)
		}
		return reason.New(reason.JudgeOldOrderUnreadable, "1つ前の再生順を読めていません")
	}
	return reason.New(reason.JudgeOrderUnreadable, "再生順を読めていません")
}

// unclosedText は、閉じない引用符で読めなかったファイルの見出しに添える文を返す。
// line は引用符が開いた物理行。
func unclosedText(line int) string {
	return fmt.Sprintf("%d行目で開いた引用符がファイルの終わりまで閉じないので、読めません", line)
}

// unclosedBlockReason は、閉じない引用符で読めなかったファイルのせいでそのカテゴリを
// 判定しなかったときの理由を返す。そうでなければ第2戻り値が false。
//
// 見る順は [Summary.unclosedBlocks] と同じにしてある。どのファイルかは、パスではなく
// 役割（再生順・公開ファイル・作業コピー・はみ出しの記録）で言う。text 形式では
// ロケールの見出しにパスが出ており、画面も断り書きにパスを出す。理由の文にパスを
// 入れると、このパッケージは表示の基準になるルートを知らないので、手元の絶対パスが
// csv の標準エラーへそのまま出る。
func unclosedBlockReason(sum Summary, c Category) (reason.Reason, bool) {
	line := func(id, what string, n int) reason.Reason {
		return reason.New(id, fmt.Sprintf("%sの%d行目の引用符が閉じません", what, n), "line", strconv.Itoa(n))
	}
	switch {
	case sum.OrderUnclosed > 0:
		return line(reason.JudgeOrderUnclosed, "再生順", sum.OrderUnclosed), true
	case sum.PublishedUnclosed > 0:
		return line(reason.JudgePublishedUnclosed, "公開ファイル", sum.PublishedUnclosed), true
	case c.needsWorking() && sum.WorkingUnclosed > 0:
		return line(reason.JudgeWorkingUnclosed, "作業コピー", sum.WorkingUnclosed), true
	case c.needsLayoutRisks() && sum.LayoutRisksUnclosed > 0:
		return line(reason.JudgeLayoutRisksUnclosed, "はみ出しの記録", sum.LayoutRisksUnclosed), true
	case c.comparesLocales() && len(sum.OthersUnclosed) > 0:
		locales := strings.Join(sum.OthersUnclosed, ", ")
		return reason.New(reason.JudgeOtherPublishedUnclosed,
			"ほかのロケール（"+locales+"）の公開ファイルの引用符が閉じません", "locales", locales), true
	}
	return reason.Reason{}, false
}

// carryCopiedFirst は「複製」を先にした写しを返す。元の並びは変えない。
//
// --limit で切り詰められるのは末尾なので、並べ替えないと複製が落ちうる。実データの
// ゲーム更新では複製は24件中1件で、キー順だと19番目に来る。既定の --limit 20 では
// たまたま残るが、候補が1件増えれば消える。消えると翻訳者は「複製 1 件」という
// 内訳だけを見せられ、どの行かを知る手立てが無いまま、いまも再生されている行の訳を
// 消すことになる。落としてよいのは移動のほうで、そちらは取りこぼしても
// 「台本から消えた行」に同じ行が残る。
//
// 並べ替えるのは text の一覧だけで、Report.Findings と CSV の並びはそのまま。
// CSV は表計算で好きに並べ替えられるので、こちらで先回りする理由が無い。
func carryCopiedFirst(list []Finding) []Finding {
	out := make([]Finding, len(list))
	copy(out, list)
	// 安定ソートにして、複製どうし・移動どうしはキー順のまま残す。
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CarryKind == CarryCopied && out[j].CarryKind != CarryCopied
	})
	return out
}

// writeCarryHint は、消えた行のうち引き継ぎ候補が付いた件数を書く。
//
// 同じ行が下の「引き継ぎ候補」にもう一度並ぶので、二重に読ませないよう先に断る。
// 実データのゲーム更新では23件が23件とも重なり、一覧が丸ごと二度出る。
// 何も言わないと、翻訳者は同じ23行を目で突き合わせてから重複に気づくことになる。
//
// 数えるのは、実際に両方へ出ている行だけ。「複製」は旧キーがいまも再生順に
// あるので「台本から消えた行」には出ない。「移動」も、公開行の section が 'UI' だと
// 別のカテゴリへ回るので必ずしも重ならない。Summary.CarryMoved をそのまま
// 書くと、重なりの数が「台本から消えた行」の件数を超えることがある。
func writeCarryHint(b *strings.Builder, sum Summary, vanished, carry []Finding) {
	if !sum.canJudge(CatCarryover) {
		return
	}
	carried := make(map[string]struct{}, len(carry))
	for _, f := range carry {
		carried[f.Key] = struct{}{}
	}
	n := 0
	for _, f := range vanished {
		if _, ok := carried[f.Key]; ok {
			n++
		}
	}
	if n == 0 {
		return
	}
	fmt.Fprintf(b, "        うち %d 件には引き継ぎ候補があります（下の「引き継ぎ候補」に移し先を書いてあります）。\n", n)
}

// writeCarryBreakdown は引き継ぎ候補の内訳（移動と複製）を書く。
//
// 分けて書くのは、旧行の始末が正反対になるから。「移動」の旧行はもう再生順に
// 無いので訳ごと移してよいが、「複製」の旧行はいまも別の場所で再生される。
// そちらの訳まで消すと、生きている行が英語に戻る。
//
// 0 件の側は書かない。実データのゲーム更新では複製が1件しか出ず、
// 毎回「複製 0 件」と並べても読む手がかりにならない。
func writeCarryBreakdown(b *strings.Builder, sum Summary) {
	if sum.CarryMoved > 0 {
		fmt.Fprintf(b, "        移動 %d 件   旧キーはもう再生順にありません。訳を移してください。\n", sum.CarryMoved)
	}
	if sum.CarryCopied > 0 {
		fmt.Fprintf(b, "        複製 %d 件   旧キーは別の行で生きています。元の行の訳は残してください。\n", sum.CarryCopied)
	}
	if sum.CarryMoved > 0 {
		b.WriteString("        「移動」の行は上の「台本から消えた行」にも出ます（同じ行の別の見方です）。\n")
	}
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
//
// 件ではなく行で数える。同じ行が2つのカテゴリに出るからで、実データのゲーム更新
// では「台本から消えた行」23件と「引き継ぎ候補」24件が、23行ぶん重なる。のべで
// 数えると 47 件になるが、翻訳者が開く行は 24 行しかない。最後の1行は「あと何行
// 見ればよいか」を伝えるためのものなので、重ねずに数えるほうを選んだ。
//
// のべ件数も添えるのは、上の一覧の数字（カテゴリごとの件数）を足しても締めの数に
// ならないため。黙って減らすと、どちらかが間違っているように見える。
func (r *Report) writeTextFooter(b *strings.Builder) {
	b.WriteString("\n")
	if len(r.Locales) == 0 {
		b.WriteString("報告するロケールがありません。\n")
		return
	}
	rows := r.RowCountByStatus(StatusReview)
	if rows == 0 {
		b.WriteString("要確認はありません。（--all で内訳、--strict で要作業も終了コード 1）\n")
		return
	}
	if total := r.CountByStatus(StatusReview); total != rows {
		fmt.Fprintf(b, "要確認が %d 行あります（カテゴリをまたぐ重なりを含めて、のべ %d 件）。\n", rows, total)
		return
	}
	fmt.Fprintf(b, "要確認が %d 行あります。\n", rows)
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

	// 引き継ぎ候補だけは、移動か複製かを行の先頭に置く。
	//
	// 末尾の note にも同じ語が入るが、そちらは CSV と共通の自己完結した文面で、
	// 位置・話者・キー・訳を並べたあとに来る。実データの1行は200桁を超え、
	// 24行のうち複製は1行しかない。右端まで読まないと分からない置き方だと、
	// その1行を見落として、いまも再生されている行の訳を消すことになる。
	// 目が縦に走るのは左端なので、旧行を消してよいかどうかは左端に出す。
	if f.Category == CatCarryover && f.CarryKind != CarryNone {
		parts = append(parts, f.CarryKind.String())
	}

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
