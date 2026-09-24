package diff

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/linekey"
	"github.com/223n/dragnwash-localization-editor/internal/order"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// Finding は報告する1行。
//
// 位置（Section / Node / OrderText / Speaker）の出どころはカテゴリで違う。
// 公開ファイル由来のカテゴリはその行の列をそのまま写し、再生順由来のカテゴリは
// 再生順の最初の出現から取る。
type Finding struct {
	Locale   string
	Category Category

	Key         string
	Section     string
	Node        string
	OrderText   string
	Speaker     string
	SourceEn    string
	Translation string
	// Note は短い日本語の理由。CSV の最終列に入る。
	Note string
	// NoteReason は [Finding.Note] と同じ理由を、識別子と置換の組で持つ。
	//
	// Note と別に持つのは、CLI（text と CSV）が日本語の文面をそのまま出す一方で、
	// 画面（internal/web）は目録で差し替えるためである。NoteReason.Text は常に
	// Note と同じ文字列になる。CSV に列は足さない。11列という出力の形は
	// 使い方の説明にも書いてある約束で、列を増やすと表計算に貼る側の手順が変わる。
	NoteReason reason.Reason
	// CarryTo は訳の引き継ぎ先の新しいキー（[CatCarryover] のときだけ入る）。
	//
	// CSV には列を足さず、同じ内容を Note の文面にも入れてある。11列という
	// 出力の形は使い方の説明にも書いてある約束で、列を増やすと表計算に
	// 貼る側の手順が変わる。機械で読みたいときのためにこの欄を用意し、
	// 人が読む側は既にある note 列で足りるようにした。
	CarryTo string
	// CarryFrom は訳の引き継ぎ元の旧キー（[CatCarryFrom] のときだけ入る）。
	//
	// [Finding.CarryTo] と向きが逆である。CarryTo はこの行の訳をどこへ移すかで、
	// CarryFrom はこの行の訳をどこから持ってくるか。CSV には列を足さず、同じ
	// 内容を Note の文面にも入れてある（11列という約束を守るため）。
	CarryFrom string
	// CarryKind は引き継ぎ元の旧行を移すのか写すのか（[CatCarryover] のときだけ入る）。
	//
	// CarryTo と分けて持つのは、旧行の始末が正反対になるから。[CarryMoved] の
	// 旧行はもう再生順に無いので訳ごと移してよいが、[CarryCopied] の旧行は
	// いまも別の場所で再生される。そちらの訳を消すと、生きている行が英語に戻る。
	CarryKind CarryKind
}

// Summary は1ロケール分の要約。
type Summary struct {
	Locale        string
	PublishedPath string
	WorkingPath   string
	HasWorking    bool
	// WorkingExists は作業コピーのファイルが実在するか。
	// HasWorking が false でもこちらが true なら --no-working で読まなかっただけ。
	WorkingExists bool
	// OrderKeys は再生順のキーを1種以上読めたか。
	// これが false のあいだ、「再生順に無い」を根拠にするカテゴリは判定しない。
	OrderKeys bool
	// OrderLineIDs は再生順の台詞IDを1件以上読めたか。意味は OrderKeys と同じ。
	OrderLineIDs bool
	// OrderNorms は再生順の norm 列に値のある行を1つ以上読めたか。
	// これが false のあいだ、引き継ぎ元の候補は判定しない。
	OrderNorms bool
	// OldOrder は1つ前の版の再生順を読めたか。
	// これが false のあいだ、引き継ぎ候補は判定しない。
	OldOrder bool
	// OldOrderStale は、読めた旧再生順が本当に「更新前の版」かどうかを疑う印。
	//
	// 立つのは「旧版と新版でキーが1行も変わっていないのに、公開ファイルには
	// 台本から消えた行がある」とき。消えた行がある以上、どこかでキーが
	// 変わったはずで、それが見えないなら旧版として読んだものが既に更新後の
	// 内容になっている。この状態で「引き継ぎ候補 0 件」と書くと、23行の訳を
	// 捨ててよいと読まれる。
	OldOrderStale bool
	// OldOrderReason は旧再生順を読めなかった理由（[Repo.OldOrderReason]）。
	// 読めたときは空。
	OldOrderReason string
	// OldOrderReasonID は [Summary.OldOrderReason] に対応する識別子
	// （[Repo.OldOrderReasonID]）。名前を付けていない理由のときは空。
	OldOrderReasonID string

	// CarryMoved は引き継ぎ候補のうち「移動」の件数。
	// 旧キーがもう再生順に無いので、これらは「台本から消えた行」にも出る。
	CarryMoved int
	// CarryCopied は引き継ぎ候補のうち「複製」の件数。
	// 旧キーは別の行で生きているので、「台本から消えた行」には出ない。
	CarryCopied int

	// HashRows は公開ファイルのハッシュ行の数。
	HashRows int
	// LineRows は公開ファイルの台詞ID行の数。
	LineRows int
	// BrokenRows は公開ファイルの、どちらの形でもない行の数。
	// 実データでは13ロケールとも0件。1件でも出たらファイルが壊れている。
	BrokenRows int
	// WorkingRows は作業コピーの行数。
	WorkingRows int
	// HasLayoutRisks はゲームが測ったはみ出しの記録を読んだか。
	HasLayoutRisks bool
	// LayoutRisksExist はその記録のファイルが実在するか。
	// HasLayoutRisks が false でもこちらが true なら --no-working で読まなかっただけ。
	LayoutRisksExist bool
	// LayoutRisksPath はその記録の置き場所（[Locale.LayoutRisksPath]）。
	LayoutRisksPath string
	// LayoutRiskRows は読めた記録の行数。結び付けられた行の数ではない。
	LayoutRiskRows int
	// SourceMissing は作業コピーのハッシュ行のうち原文が未取得のものの数。
	//
	// ゲームが未ロードだと source_en が空になる（移植仕様「作業コピー生成 R7/R8」）。
	// これを未翻訳に混ぜると作業コピーのほぼ全行が誤検出になるので、
	// Finding にはせず件数だけ持つ。
	SourceMissing int

	// OrderUnclosed は [Repo.OrderUnclosed]。再生順で開いた引用符が閉じなかった
	// 物理行で、閉じていれば 0。0 でなければどのカテゴリも判定しない。
	OrderUnclosed int
	// PublishedUnclosed は [Locale.PublishedUnclosed]。0 でなければ、このロケールは
	// どのカテゴリも判定しない。
	PublishedUnclosed int
	// WorkingUnclosed は [Locale.WorkingUnclosed]。0 でなければ、作業コピーを要る
	// カテゴリを判定しない。
	WorkingUnclosed int
	// LayoutRisksUnclosed は [Locale.LayoutRisksUnclosed]。0 でなければ、はみ出しの
	// 記録を要るカテゴリを判定しない。
	LayoutRisksUnclosed int
	// OthersUnclosed は、公開ファイルで開いた引用符が閉じなかった、ほかのロケールの
	// 名前（ディレクトリ名順）。報告しないロケールも入る。1つでもあれば、ロケールどうしを
	// 比べるカテゴリを判定しない。
	OthersUnclosed []string

	// Counts はカテゴリごとの件数。全カテゴリに値が入る。
	Counts map[Category]int
}

// canJudge はそのカテゴリを判定できたかを返す。false のとき Counts の 0 は
// 「1件も無い」ではなく「判定していない」を意味する。
//
// 判定できないのは、閉じない引用符で読めなかったファイルに依るとき
// （[Summary.unclosedBlocks]）と、次の6つの場合しかない。作業コピーが要るのに
// 読んでいないとき、再生順のキーが要るのに読めていないとき、再生順の台詞IDが要るのに
// 無いとき、再生順に norm 列が無いとき、はみ出しの記録が要るのに読んでいないとき、
// 1つ前の版の再生順を取り出せないとき（または今の版と同じ内容に見えるとき）で、
// どれも「0 件」と書くと嘘になる。
func (s Summary) canJudge(c Category) bool {
	if s.unclosedBlocks(c) {
		return false
	}
	if c.needsWorking() && !s.HasWorking {
		return false
	}
	if c.needsOrderKeys() && !s.OrderKeys {
		return false
	}
	if c.needsOrderLineIDs() && !s.OrderLineIDs {
		return false
	}
	if c.needsOrderNorms() && !s.OrderNorms {
		return false
	}
	if c.needsLayoutRisks() && !s.HasLayoutRisks {
		return false
	}
	if c.needsOldOrder() && (!s.OldOrder || s.OldOrderStale) {
		return false
	}
	return true
}

// unclosedBlocks は、閉じない引用符で読めなかったファイルのせいで、そのカテゴリを
// 判定しないかを返す（決まったことの 3）。
//
// どのファイルがどのカテゴリに効くかは次のとおりで、見る順は judgeBlockReason と同じ。
//
//   - 再生順: 報告全体。どのカテゴリも再生順を判定か位置の根拠に使う。
//   - このロケールの公開ファイル: このロケールのすべてのカテゴリ。
//   - 作業コピー: 作業コピーを要るカテゴリ。タグの開閉とはみ出しの恐れは、
//     --no-working のときと同じく公開ファイルを見る。
//   - はみ出しの記録: はみ出しの恐れ。
//   - ほかのロケールの公開ファイル: ロケールどうしを比べるカテゴリ。
func (s Summary) unclosedBlocks(c Category) bool {
	switch {
	case s.OrderUnclosed > 0, s.PublishedUnclosed > 0:
		return true
	case c.needsWorking() && s.WorkingUnclosed > 0:
		return true
	case c.needsLayoutRisks() && s.LayoutRisksUnclosed > 0:
		return true
	case c.comparesLocales() && len(s.OthersUnclosed) > 0:
		return true
	}
	return false
}

// Report は比較の結果。
type Report struct {
	OrderPath string
	// OrderRows は再生順の行数。キーの種類数（OrderKeys）とは違う。
	// 同じ英文が複数のノードに現れるため、実データでは1839行に対してキーは1602種。
	OrderRows int
	// OrderKeys は再生順のキーの種類数（空のキーは数えない）。
	OrderKeys int
	// OrderLineIDs は再生順の台詞IDの種類数（空は数えない）。
	OrderLineIDs int
	// ReadLocales は読んだロケールの数。報告した数ではない。
	ReadLocales int
	// EmptyLocales は公開ファイルも作業コピーも無いロケールの名前
	// （[Repo.EmptyLocales]）。--locale で絞っても全件入る。
	EmptyLocales []string
	// OrderUnclosed は [Repo.OrderUnclosed]。
	OrderUnclosed int
	// Unclosed は、閉じない引用符で読めず、報告するロケールのどれかの判定を止めた
	// ファイル。並びは再生順、公開ファイル（ロケール順）、作業コピーとはみ出しの記録
	// （報告するロケールの順）で、同じファイルは1回だけ入る。
	//
	// 公開ファイルは報告しないロケールのものも入る。ロケールどうしを比べる
	// カテゴリを止めるからである。報告しないロケールの作業コピーとはみ出しの記録は
	// 入らない。報告するロケールの判定には効かない。
	//
	// 1つでもあれば、dwloc diff は終了コードを1にする（決まったことのそのほか 6）。
	// 判定していないカテゴリがある以上、「要確認なし」と言えない。
	Unclosed []publish.UnclosedFile

	// Locales は報告するロケールの要約。
	Locales []Summary
	// Findings はロケール順 → カテゴリ順 → キー順。
	Findings []Finding
}

// Status は Findings の中で最も重い [Status] を返す。1件も無ければ [StatusInfo]。
//
// 終了コードはこの値で決める。要作業が何件残っていても 0 のままにするのは、
// 翻訳者が毎日走らせる道具だから。作業が残っていることは異常ではなく、
// CI を常時赤にすると誰も見なくなる。
func (r *Report) Status() Status {
	worst := StatusInfo
	for _, f := range r.Findings {
		if s := f.Category.Status(); s > worst {
			worst = s
		}
	}
	return worst
}

// CountByStatus はその重さのカテゴリに属する Finding の件数を返す。
// 同じ行が2つのカテゴリに出ていれば2件と数える（のべ件数）。
func (r *Report) CountByStatus(s Status) int {
	n := 0
	for _, f := range r.Findings {
		if f.Category.Status() == s {
			n++
		}
	}
	return n
}

// RowCountByStatus はその重さの Finding が指している行の数を、重複を除いて返す。
//
// のべ件数と分けてあるのは、同じ行が2つのカテゴリに出るため。引き継ぎ候補の
// 「移動」は「台本から消えた行」にも出るので、実データのゲーム更新では
// 「のべ 47 件」だが、翻訳者が開く行は 24 行しかない。締めの1行が言いたいのは
// 「何行を見ればよいか」なので、そちらは行で数える。
//
// キーが空の Finding は1件ずつ別に数える。key 列が壊れた作業コピーの行がここへ
// 来るので、空文字どうしを同じ行と見なすと、何行壊れていても1行に潰れる。
//
// 終了コードには使わない。あちらは「要確認が1件でもあるか」だけを見るので、
// 数え方で答えが変わらない。
func (r *Report) RowCountByStatus(s Status) int {
	type row struct{ locale, key string }
	seen := make(map[row]struct{}, len(r.Findings))
	n := 0
	for _, f := range r.Findings {
		if f.Category.Status() != s {
			continue
		}
		if f.Key == "" {
			n++
			continue
		}
		k := row{locale: f.Locale, key: f.Key}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		n++
	}
	return n
}

// orderIndex は再生順を引きやすくしたもの。
type orderIndex struct {
	// first はキーから最初の出現への対応。位置の唯一の正解になる。
	//
	// 2回目以降ではなく最初の出現を持つのは、publish の appendPlayOrder が
	// done 集合で2回目以降を書かないため。公開ファイルに出るのは常に最初の出現。
	first map[string]order.Entry
	// lineIDs は台詞IDの集合。綴りのまま持つ（小文字化しない）。
	lineIDs map[string]struct{}
	// data は話者名を引くために持つ。
	data *order.Data

	// norms は正規化した英文のハッシュ（norm 列）から、その値を持つ行への対応。
	// 引き継ぎ元を探すのに使う（carryfrom.go）。
	norms map[string][]order.Entry
	// fuzzy は指紋で突き合わせられる行。fp が読め、正規化後の長さが足切りを
	// 超えるものだけが入る。
	fuzzy []order.Entry
	// fuzzyByNode は [orderIndex.fuzzy] をノードごとに分けた表。
	fuzzyByNode map[string][]order.Entry
	// hasNorms は norm 列に値のある行が1つでもあったか。
	//
	// 無いのは列そのものが無い版の script_order.csv を読んだときで、そのときは
	// 引き継ぎ元を「0 件」ではなく「判定していません」にする。
	hasNorms bool
}

// newOrderIndex は再生順から索引を作る。
func newOrderIndex(data *order.Data) *orderIndex {
	if data == nil {
		data = &order.Data{}
	}
	idx := &orderIndex{
		first:       make(map[string]order.Entry, len(data.Entries)),
		lineIDs:     make(map[string]struct{}, len(data.Entries)),
		data:        data,
		norms:       make(map[string][]order.Entry),
		fuzzyByNode: make(map[string][]order.Entry),
	}
	for _, e := range data.Entries {
		// キーが空の行は再生順の行としては残るが、公開ファイルのどのキーとも
		// 突き合わない。キーの種類数にも数えない。
		if e.Key != "" {
			if _, seen := idx.first[e.Key]; !seen {
				idx.first[e.Key] = e
			}
		}
		if e.LineID != "" {
			idx.lineIDs[e.LineID] = struct{}{}
		}
		// 引き継ぎ元を探すための索引。キーの無い行は訳の出どころにならないので
		// 入れない。
		if e.Key == "" {
			continue
		}
		if e.Norm != "" {
			idx.hasNorms = true
			idx.norms[e.Norm] = append(idx.norms[e.Norm], e)
		}
		if _, ok := linekey.ParseFingerprint(e.FP); ok && e.NLen >= linekey.MinFuzzyLength {
			idx.fuzzy = append(idx.fuzzy, e)
			if e.Node != "" {
				idx.fuzzyByNode[e.Node] = append(idx.fuzzyByNode[e.Node], e)
			}
		}
	}
	return idx
}

// position は再生順から見た位置を返す。
//
// speaker は publish と同じ優先順（全話者の連結 → その行の話者）で決める。
// 公開ファイルの speaker 列に入るのと同じ値になるので、報告と公開ファイルを
// 見比べたときに食い違わない。
func (idx *orderIndex) position(k string) (section, node, orderText, speaker string, ok bool) {
	e, ok := idx.first[k]
	if !ok {
		return "", "", "", "", false
	}
	speaker = idx.data.SpeakersFor(e.Key)
	if speaker == "" {
		speaker = e.Speaker
	}
	return e.Section, e.Node, e.OrderText, speaker, true
}

// Compare は読み終えた [Repo] を突き合わせる。
//
// report は報告するロケール名。nil なら全ロケールを報告する。名前の照合は
// 完全一致を先に試し、外れたら大文字小文字を無視する（cmd/dwloc の --locale と
// 同じ方針）。どれにも当たらない名前は黙って無視するので、指定の誤りは
// 呼び出し側が先に弾くこと。
//
// 比較の母集合は常に r.Locales 全体から作る。報告を絞ったからといって
// 母集合まで絞ると、「他のロケールにあって無い行」が絞った範囲の中だけの
// 比較になり、数字の意味が変わってしまう。
func Compare(r *Repo, report []string) *Report {
	idx := newOrderIndex(r.Order)

	rep := &Report{
		OrderPath:     r.OrderPath,
		OrderKeys:     len(idx.first),
		OrderLineIDs:  len(idx.lineIDs),
		ReadLocales:   len(r.Locales),
		EmptyLocales:  r.EmptyLocales,
		OrderUnclosed: r.OrderUnclosed,
	}
	if r.Order != nil {
		rep.OrderRows = len(r.Order.Entries)
	}
	unclosedPublished := publishedUnclosedLocales(r.Locales)

	// 全ロケールのハッシュキー集合と、キーごとの所有ロケール数。
	sets := make([]map[string]struct{}, len(r.Locales))
	owners := make(map[string]int)
	for i, loc := range r.Locales {
		set := hashKeySet(loc.Published)
		sets[i] = set
		for k := range set {
			owners[k]++
		}
	}

	// 母集合 U = 全ロケールの公開ハッシュキーの和集合 ∪ 再生順のキー。
	union := make(map[string]struct{}, len(owners)+len(idx.first))
	for k := range owners {
		union[k] = struct{}{}
	}
	for k := range idx.first {
		union[k] = struct{}{}
	}

	want := reportedLocales(r.Locales, report)
	for i, loc := range r.Locales {
		if !want[i] {
			continue
		}
		sum, findings := compareLocale(r, idx, loc, sets[i], owners, union, othersThan(unclosedPublished, loc.Name))
		rep.Locales = append(rep.Locales, sum)
		rep.Findings = append(rep.Findings, findings...)
	}
	rep.Unclosed = unclosedFiles(r, want)
	return rep
}

// publishedUnclosedLocales は、公開ファイルで開いた引用符が閉じなかったロケールの
// 名前を、並びの順（ディレクトリ名順）に返す。
func publishedUnclosedLocales(locales []Locale) []string {
	var out []string
	for _, loc := range locales {
		if loc.PublishedUnclosed > 0 {
			out = append(out, loc.Name)
		}
	}
	return out
}

// othersThan は names から self を除いた写しを返す。1つも残らなければ nil。
func othersThan(names []string, self string) []string {
	var out []string
	for _, name := range names {
		if name != self {
			out = append(out, name)
		}
	}
	return out
}

// unclosedFiles は [Report.Unclosed] を組み立てる。want は報告するロケールの印。
func unclosedFiles(r *Repo, want []bool) []publish.UnclosedFile {
	var out []publish.UnclosedFile
	seen := make(map[string]struct{})
	add := func(path string, line int) {
		if line == 0 {
			return
		}
		if _, dup := seen[path]; dup {
			// はみ出しの記録は、作業コピーと同じフォルダーにある1つのファイルを
			// 全ロケールが読むので、同じファイルが何度も来る。
			return
		}
		seen[path] = struct{}{}
		out = append(out, publish.UnclosedFile{Path: path, Line: line})
	}
	add(r.OrderPath, r.OrderUnclosed)
	for _, loc := range r.Locales {
		add(loc.PublishedPath, loc.PublishedUnclosed)
	}
	for i, loc := range r.Locales {
		if !want[i] {
			continue
		}
		add(loc.WorkingPath, loc.WorkingUnclosed)
		add(loc.LayoutRisksPath, loc.LayoutRisksUnclosed)
	}
	return out
}

// reportedLocales はどのロケールを報告するかを決める。
func reportedLocales(locales []Locale, report []string) []bool {
	want := make([]bool, len(locales))
	if len(report) == 0 {
		for i := range want {
			want[i] = true
		}
		return want
	}
	for _, name := range report {
		found := false
		for i, loc := range locales {
			if loc.Name == name {
				want[i] = true
				found = true
			}
		}
		if found {
			continue
		}
		for i, loc := range locales {
			if strings.EqualFold(loc.Name, name) {
				want[i] = true
			}
		}
	}
	return want
}

// hashKeySet は公開行のハッシュキーの集合を作る。
func hashKeySet(rows []Row) map[string]struct{} {
	set := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		if r.Kind == KindHash {
			set[r.Key] = struct{}{}
		}
	}
	return set
}

// compareLocale は1ロケール分の要約と報告を作る。othersUnclosed は、公開ファイルで
// 開いた引用符が閉じなかった、ほかのロケールの名前。
func compareLocale(r *Repo, idx *orderIndex, loc Locale,
	mine map[string]struct{}, owners map[string]int, union map[string]struct{},
	othersUnclosed []string) (Summary, []Finding) {

	sum := Summary{
		OrderUnclosed:       r.OrderUnclosed,
		PublishedUnclosed:   loc.PublishedUnclosed,
		WorkingUnclosed:     loc.WorkingUnclosed,
		LayoutRisksUnclosed: loc.LayoutRisksUnclosed,
		OthersUnclosed:      othersUnclosed,
		Locale:              loc.Name,
		PublishedPath:       loc.PublishedPath,
		WorkingPath:         loc.WorkingPath,
		HasWorking:          loc.HasWorking,
		WorkingExists:       loc.WorkingExists,
		OrderKeys:           len(idx.first) > 0,
		OrderLineIDs:        len(idx.lineIDs) > 0,
		OrderNorms:          idx.hasNorms,
		OldOrder:            r.OldOrder != nil,
		OldOrderReason:      r.OldOrderReason,
		OldOrderReasonID:    r.OldOrderReasonID,
		WorkingRows:         len(loc.Working),
		HasLayoutRisks:      loc.HasLayoutRisks,
		LayoutRisksExist:    loc.LayoutRisksExist,
		LayoutRisksPath:     loc.LayoutRisksPath,
		LayoutRiskRows:      len(loc.LayoutRisks),
		Counts:              make(map[Category]int, len(categories)),
	}
	for _, c := range categories {
		sum.Counts[c] = 0
	}

	// 台本から消えた行の数。旧再生順が本当に更新前のものかを疑う材料に使う。
	vanished := 0

	// カテゴリごとに集めてから、カテゴリ順に連結する。
	found := make(map[Category][]Finding, len(categories))
	add := func(c Category, f Finding) {
		f.Locale = loc.Name
		f.Category = c
		if f.Note == "" {
			f.Note = c.note()
			f.NoteReason = c.noteReason()
		}
		found[c] = append(found[c], f)
	}

	for _, row := range loc.Published {
		switch row.Kind {
		case KindHash:
			sum.HashRows++
			// 再生順のキーを1種も読めていないときは判定しない。
			// 「再生順に無い」は再生順が読めていて初めて意味を持つ根拠で、
			// 読めていないまま当てると公開行のほぼ全部（実データの ja で
			// 1570件）が「台本から消えた行」に化ける。
			if !sum.OrderKeys {
				continue
			}
			if _, inOrder := idx.first[row.Key]; inOrder {
				continue
			}
			cat := residualCategory(row)
			if cat == CatVanished {
				vanished++
			}
			add(cat, publishedFinding(row))
		case KindLineID:
			sum.LineRows++
			if !sum.OrderLineIDs {
				continue
			}
			if _, inOrder := idx.lineIDs[row.Key]; inOrder {
				continue
			}
			add(CatStrayLineID, publishedFinding(row))
		case KindBroken:
			// 公開ファイル側の形式不備は internal/validate が拾うので数えるだけ。
			// 二重に報告しても直し方は増えない。
			sum.BrokenRows++
		}
	}

	// 引き継ぎ候補。既存の判定には手を出さず、別のカテゴリとして足す。
	//
	// 「移動」の行は「台本から消えた行」にも出たままにする。候補が付いた行を
	// 元のカテゴリから外すと、候補の作り方を変えただけで「台本から消えた行」の
	// 件数が動くことになり、いちばん確かな判定がいちばん当てにならない候補に
	// 引きずられる。「複製」の行は旧キーが生きているので、そもそも重ならない。
	carry, sameTable := carryoverCandidates(idx, r.OldOrder, mine)
	if sum.OldOrder && looksStaleOldOrder(sameTable, vanished) {
		// 旧版として読んだものが、いまの版と同じ内容になっている疑い。
		// 台本から消えた行があるのにキーの変化が1つも見えないのは辻褄が合わない。
		sum.OldOrderStale = true
		sum.OldOrderReason = staleOldOrderReason
		sum.OldOrderReasonID = reason.OldOrderStale
	}
	if sum.canJudge(CatCarryover) {
		for _, row := range loc.Published {
			if row.Kind != KindHash {
				continue
			}
			target, ok := carry[row.Key]
			if !ok {
				continue
			}
			if row.Translation == "" {
				// 移す訳が無い。キーの対応としては正しくても、翻訳者に
				// できることが何も無いので出さない。
				continue
			}
			f := publishedFinding(row)
			f.CarryTo = target.key
			f.CarryKind = target.kind
			cause := target.cause()
			f.Note, f.NoteReason = cause.Text, cause
			switch target.kind {
			case CarryCopied:
				sum.CarryCopied++
			default:
				sum.CarryMoved++
			}
			add(CatCarryover, f)
		}
	}

	work := foldWorking(loc.Working)
	sum.SourceMissing = work.sourceMissing
	for _, row := range loc.Working {
		cause, dropped := droppedCause(row)
		if !dropped {
			continue
		}
		// publish に捨てられる行は、未翻訳かどうかを論じる前に消える。
		// 両方に出すと同じ行を2回直すように見えるので、捨てられる側だけを出す。
		f := workingFinding(idx, row)
		f.Note, f.NoteReason = cause.Text, cause
		add(CatDropped, f)
	}
	for _, k := range work.untranslatedOrder {
		add(CatUntranslated, workingFinding(idx, work.untranslated[k]))
	}

	// タグの開閉。訳だけを見るので、作業コピーが無くても判定できる。見る行は
	// publish の入力になる側（作業コピーがあればそれ、無ければ公開ファイル）。
	for _, f := range tagFindings(idx, loc) {
		add(CatTagUnbalanced, f)
	}

	// 引き継ぎ元の候補。作業コピーの未翻訳の行に、公開ファイルの旧キーを添える。
	// 引き継ぎ候補（CatCarryover）とは根拠も向きも違う（carryfrom.go の説明）。
	if sum.canJudge(CatCarryFrom) {
		for _, f := range carryFromFindings(idx, loc) {
			add(CatCarryFrom, f)
		}
	}

	// はみ出しの恐れ。ゲームが測った記録を行に結び付ける。
	if sum.canJudge(CatLayoutRisk) {
		for _, f := range layoutRiskFindings(idx, loc) {
			add(CatLayoutRisk, f)
		}
	}

	// 原文とタグの構成の突き合わせ。原文が要るので作業コピーだけを見る。
	// 開閉と重なることはある（原文が <i> を閉じずに使う行で、訳がその <i> を
	// 落としていれば両方に出る）。片方を落とさないのは、直し方が違うからである。
	if sum.canJudge(CatTagMismatch) {
		for _, f := range tagMismatchFindings(idx, loc) {
			add(CatTagMismatch, f)
		}
	}

	// 母集合からこのロケールの公開ハッシュキーを引いた残り。
	for k := range union {
		if _, have := mine[k]; have {
			continue
		}
		// 作業コピーが手元にあるキーは、この2つのカテゴリから外す。
		// 訳が入っていれば publish を回すだけで埋まり、訳が空なら既に
		// 「未翻訳」として出ている。どちらの場合も、ここに重ねて出すと
		// 1行の仕事が要作業2件に見える。
		if _, inWork := work.keys[k]; inWork {
			continue
		}
		f := Finding{Key: k}
		section, node, orderText, speaker, inOrder := idx.position(k)
		if inOrder {
			f.Section, f.Node, f.OrderText, f.Speaker = section, node, orderText, speaker
		}
		if others := owners[k]; others > 0 {
			if !inOrder {
				// 再生順に無いキーは、持っているロケールの公開行から位置を借りる。
				borrowPosition(r, k, &f)
			}
			f.Note = fmt.Sprintf("他の %d ロケールにあります", others)
			f.NoteReason = reason.New(reason.NoteLocaleGap, f.Note, "count", strconv.Itoa(others))
			add(CatLocaleGap, f)
			continue
		}
		add(CatNotPublished, f)
	}

	var findings []Finding
	for _, c := range categories {
		list := found[c]
		if !sum.canJudge(c) {
			// 判定していないカテゴリの行は出さない。閉じない引用符で公開ファイルを
			// 読めなかったロケールでは、自分のキーが空なので、ほかのロケールの
			// キーがすべて「他のロケールにあって無い行」に化ける。ほかの止め方
			// （作業コピーが無い、再生順を読めていない、など）は、材料が無いので
			// もともと1件も作らない。ここはそれを、止めたカテゴリすべてで確かにする。
			list = nil
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].Key < list[j].Key })
		sum.Counts[c] = len(list)
		findings = append(findings, list...)
	}
	return sum, findings
}

// workingFold はキー単位に畳んだ作業コピー。
type workingFold struct {
	// keys は publish が採用するキー全部。訳の有無を問わない。
	keys map[string]struct{}
	// untranslated は採用されるが訳が1行も入っていないキーと、その初出の行。
	untranslated map[string]Row
	// untranslatedOrder は untranslated のキーをファイル順に並べたもの。
	untranslatedOrder []string
	// sourceMissing は採用されるが原文が未取得のキーの数。
	sourceMissing int
}

// foldWorking は作業コピーの行をキー単位に畳む。
//
// 行単位で「訳が空なら未翻訳」と判定してはいけない。publish は訳が空の行を
// 採用表に入れないので、同じキーに「訳が空の行」と「訳のある行」が並んでいると
// 訳のあるほうが公開される（移植仕様「公開CSV生成 R18 / R20」）。行単位で見ると、
// publish が公開する訳があるのに未翻訳だと報告することになる。
//
// 生成直後の作業コピーに重複キーは出ないが、手編集やマージでは起きる。
func foldWorking(rows []Row) workingFold {
	w := workingFold{
		keys:         make(map[string]struct{}, len(rows)),
		untranslated: make(map[string]Row, len(rows)),
	}
	translated := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		k, adopted := PublishKey(row)
		if !adopted {
			continue
		}
		if _, seen := w.keys[k]; !seen {
			w.keys[k] = struct{}{}
			if row.SourceEn == "" {
				// ゲームが未ロードの行。訳を書けないので未翻訳には数えない
				// （移植仕様「作業コピー生成 R7/R8」）。件数だけ持つ。
				w.sourceMissing++
			}
		}
		if row.Translation != "" {
			translated[k] = struct{}{}
			continue
		}
		if row.SourceEn == "" {
			continue
		}
		if _, seen := w.untranslated[k]; !seen {
			w.untranslated[k] = row
			w.untranslatedOrder = append(w.untranslatedOrder, k)
		}
	}
	// 訳のある行が1つでもあれば、そのキーは publish が公開する。
	for k := range translated {
		if _, ok := w.untranslated[k]; ok {
			delete(w.untranslated, k)
		}
	}
	kept := w.untranslatedOrder[:0]
	for _, k := range w.untranslatedOrder {
		if _, ok := w.untranslated[k]; ok {
			kept = append(kept, k)
		}
	}
	w.untranslatedOrder = kept
	return w
}

// residualCategory は「再生順に無い公開ハッシュ行」を3つに割る。
//
// section 列が 'UI' でないことが、ファイルの中に残る唯一の時間差の証拠になる
// （パッケージコメント参照）。比較は publish が書く綴りとの完全一致で行う。
// 大文字小文字を無視すると、手で 'ui' と書き換えた行まで UI 扱いになり、
// 消えた行を静かに見逃す向きにずれる。
func residualCategory(row Row) Category {
	if row.Section != publish.UISectionName {
		return CatVanished
	}
	if row.Speaker != "" && row.Speaker != publish.UIFallbackSpeaker {
		return CatScriptGap
	}
	return CatUnknownOrigin
}

// publishedFinding は公開行をそのまま [Finding] に写す。
func publishedFinding(row Row) Finding {
	return Finding{
		Key:         row.Key,
		Section:     row.Section,
		Node:        row.Node,
		OrderText:   row.OrderText,
		Speaker:     row.Speaker,
		SourceEn:    row.SourceEn,
		Translation: row.Translation,
	}
}

// workingFinding は作業コピーの行を [Finding] に写す。
//
// 位置は行が持っている値を使う。作業コピーの section / node / order は
// 生成時の再生順から来ているので、たいていは埋まっている。3つとも空のときだけ
// いまの再生順から補う。ゲーム内で再生順を読めなかった状態で書き出された
// 作業コピー（移植仕様「作業コピー生成 R28」）を救うための補いなので、
// 片方だけ埋まっている行には手を出さない。
func workingFinding(idx *orderIndex, row Row) Finding {
	f := publishedFinding(row)
	// key 列が無い（あるいは空の）行では、publish が source_en から導くキーを使う。
	// 行の値をそのまま写すと Key が空のまま報告され、どの行のことか分からなくなる。
	if k, adopted := PublishKey(row); adopted {
		f.Key = k
	}
	if f.Section == "" && f.Node == "" && f.OrderText == "" {
		if section, node, orderText, speaker, ok := idx.position(f.Key); ok {
			f.Section, f.Node, f.OrderText = section, node, orderText
			if f.Speaker == "" {
				f.Speaker = speaker
			}
		}
	}
	return f
}

// borrowPosition は他のロケールの公開行から位置を借りる。
// 見つかった最初の1件だけを使う。13ロケールの同じキーの行は、
// publish が同じ再生順から書いている以上、位置が食い違うことはない。
func borrowPosition(r *Repo, k string, f *Finding) {
	for _, loc := range r.Locales {
		for _, row := range loc.Published {
			if row.Kind != KindHash || row.Key != k {
				continue
			}
			f.Section, f.Node, f.OrderText, f.Speaker = row.Section, row.Node, row.OrderText, row.Speaker
			return
		}
	}
}
