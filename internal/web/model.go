package web

import (
	"sort"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/edit"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// 画面に出す行の種類。ファイルの物理行の種類（edit.Kind）を、描き方の違いだけに
// まとめ直したもの。判定ではなく描き分けなので、ここで決めてよい。
const (
	// lineKindHeading は見出しとして描く行。ファイルにあるコメント行そのもの。
	lineKindHeading = "heading"
	// lineKindData は1行ぶんの訳として描く行。
	lineKindData = "data"
)

// 見出しの深さ。ファイルにある印（publish.SectionMarker / publish.NodeMarker）に
// 対応する。order パッケージから組み直さないのは、画面を常にファイルの写しに
// するためである。ファイルに無い見出しは出さないし、ファイルにある見出しは消さない。
const (
	headingSection = "section"
	headingNode    = "node"
	headingOther   = "other"
)

// キーの形。internal/key の判定をそのまま写す。
//
// これは「どう描くか」の材料にしかしない。壊れた行が何行あるかは
// [diff.Summary.BrokenRows] が数えているので、画面はそちらを出す。
const (
	keyKindHash  = "hash"
	keyKindLine  = "line"
	keyKindOther = "other"
)

// badgeView は1行に付ける状態バッジ。
//
// 中身はすべて [diff.Finding] から来る。画面はここに何も足さない。
type badgeView struct {
	// Category はカテゴリの ASCII 識別子（[diff.Category.ID]）。
	Category string `json:"category"`
	// Label は表示名。目録に "category.<識別子>" があればその値、無ければ
	// internal/diff が持つ名前。diff にカテゴリが増えても、目録が追いつくまでは
	// diff の名前で出る。名前が出ないより、訳されていない名前が出るほうがよい。
	Label string `json:"label"`
	// Status は重さの ASCII 識別子（[diff.Status.ID]）。
	Status string `json:"status"`
	// Note は [diff.Finding.Note]。行ごとの短い理由。
	Note string `json:"note,omitempty"`
}

// lineView は画面に出す1行。
type lineView struct {
	// Number は1始まりの物理行番号。
	Number int `json:"n"`
	// Kind は描き方（[lineKindHeading] か [lineKindData]）。
	Kind string `json:"kind"`

	// Heading は見出しの深さ。Kind が [lineKindHeading] のときだけ入る。
	Heading string `json:"heading,omitempty"`
	// Text は生テキスト（改行を除く）。書き換えずにそのまま出す。
	//
	// 見出し行では見出しの文、データ行では編集できないときだけ入る。
	// 編集できない行は [edit.Line.Translation] が空を返す（列がずれているので、
	// 最終フィールドが訳とは限らないため）。読むための画面でその行だけ中身が
	// 見えなくなるのは困るので、生の行をそのまま渡して画面に出せるようにする。
	Text string `json:"text,omitempty"`

	// Key は先頭フィールド。
	Key string `json:"key,omitempty"`
	// KeyKind はキーの形（[keyKindHash] / [keyKindLine] / [keyKindOther]）。
	KeyKind string `json:"keyKind,omitempty"`
	// Speaker は speaker 列の値。列が無いファイルでは空。
	Speaker string `json:"speaker,omitempty"`
	// Source は source_en 列の値。作業コピーにしかない列なので、公開ファイルでは空。
	Source string `json:"source,omitempty"`
	// Translation は最終列（訳）。
	Translation string `json:"translation,omitempty"`
	// Editable はこの行の訳を書き換えてよいか（[edit.Line.Editable]）。
	//
	// この段では編集させないが、書き換えられない行は見た目で分かるようにしておく。
	Editable bool `json:"editable"`
	// Reason は Editable が false のときの理由。
	Reason string `json:"reason,omitempty"`
	// Badges は状態バッジ。
	Badges []badgeView `json:"badges,omitempty"`
}

// countView はカテゴリ1つぶんの件数。
type countView struct {
	Category    string `json:"category"`
	Label       string `json:"label"`
	Status      string `json:"status"`
	StatusLabel string `json:"statusLabel"`
	// Judged が false のとき、Count に意味は無い。画面は数ではなく Reason を出す。
	Judged bool   `json:"judged"`
	Count  int    `json:"count"`
	Reason string `json:"reason,omitempty"`
}

// statView は「数えたもの」1つぶん。
//
// 数を文にせず、名前と数を分けて渡す。画面は「行: 12」の形で出す。複数形の
// 規則を持ち込まないための形でもある。
type statView struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

// linesResponse は GET /api/lines の応答。
type linesResponse struct {
	Locale string `json:"locale"`
	// Version は読んだときのファイルの版（バイト列全体の SHA-256）。
	// 画面はこれを覚えておき、保存要求の baseVersion に載せる。手前でファイルが
	// 変わっていれば待ち受けが照合で弾くので、黙って上書きすることがない。
	Version string `json:"version"`
	// Path は表示用のパス。ルートからの相対で、スラッシュ区切り。
	// クライアントはこれを受け取るだけで、要求に載せることはない。
	Path string `json:"path"`
	// Columns はファイルのヘッダー行の列名。そのまま出す（データ側の語彙なので訳さない）。
	Columns []string `json:"columns"`
	// SourceColumn は source_en 列があるか。無ければ原文の欄は常に空になる。
	SourceColumn bool `json:"sourceColumn"`
	// ReadOnlyReason はファイル全体が読み取り専用のときの理由。
	ReadOnlyReason string `json:"readOnlyReason,omitempty"`

	// Rows はデータ行の数。画面の見出しに「行: 1721」と出す値。
	//
	// 画面に数えさせないために、ここで数えて渡す。Lines の中を数え上げる処理を
	// 画面側に置くと、そこが「画面が自分で数える」場所の1つ目になる。
	Rows int `json:"rows"`

	Lines  []lineView  `json:"lines"`
	Counts []countView `json:"counts"`
	Stats  []statView  `json:"stats"`
	// Notes は「何を読んだか」の短い文。作業コピーの有無など、
	// [diff.Summary] から引いたものだけを入れる。
	Notes []string `json:"notes"`
}

// statusOrder は件数を並べる順。
//
// internal/diff の text 出力と同じ 要作業 → 要確認 → 参考 にそろえる。翻訳者が
// 「まず何を見ればいいか」を自分で決めなくて済むようにするための並びで、
// 道具ごとに違うと決め直しになる。
var statusOrder = []diff.Status{diff.StatusTodo, diff.StatusReview, diff.StatusInfo}

// buildLines は1ロケール分の画面データを組む。
//
// 行の並びと見出しは file（＝ファイルの物理行）から、状態バッジと件数は
// sum / findings（＝internal/diff）から取る。この2つを混ぜないことがこの関数の
// 役目で、画面に新しい判断を置かないという約束はここで守られる。
func (s *server) buildLines(cat *Catalog, locale, displayPath string, file *edit.File,
	sum diff.Summary, findings []diff.Finding) *linesResponse {

	columns := file.Header()
	idx := columnIndex(columns)

	resp := &linesResponse{
		Locale:         locale,
		Version:        file.Version(),
		Path:           displayPath,
		Columns:        columns,
		SourceColumn:   idx.source >= 0,
		ReadOnlyReason: s.reasonText(cat, file.ReadOnlyCause()),
	}

	// 起動後に訳が入ったキーは「未翻訳」のバッジを外す。局所更新はここだけで、
	// カテゴリの割り当てそのものは起動時のまま動かさない。
	filled := s.filledKeys(locale)
	badges := s.badgesByKey(cat, findings, filled)

	lines := file.Lines()
	resp.Lines = make([]lineView, 0, len(lines))
	dataRows := 0
	for _, line := range lines {
		switch line.Kind {
		case edit.KindComment:
			resp.Lines = append(resp.Lines, headingView(line))
		case edit.KindData:
			dataRows++
			resp.Lines = append(resp.Lines, s.dataView(cat, line, idx, badges))
		default:
			// 空行とヘッダー行は出さない。空行はファイルの間隔で、ヘッダー行は
			// 列名として Columns に入れてある。どちらも1行として並べると、
			// 番号だけの行が混ざって読みにくくなる。
		}
	}

	resp.Rows = dataRows
	resp.Counts = s.buildCounts(cat, locale, sum)
	resp.Stats = s.buildStats(cat, sum, len(lines), dataRows)
	resp.Notes = s.buildNotes(cat, locale, sum, idx.source >= 0)
	return resp
}

// headingView はコメント行を見出しにする。中身は書き換えない。
func headingView(line edit.Line) lineView {
	body := strings.TrimRight(line.Text, "\r\n")
	return lineView{
		Number:  line.Number,
		Kind:    lineKindHeading,
		Heading: headingLevel(body),
		Text:    body,
	}
}

// headingLevel は見出しの深さを返す。
func headingLevel(body string) string {
	switch {
	case strings.HasPrefix(body, publish.SectionMarker):
		return headingSection
	case strings.HasPrefix(body, publish.NodeMarker):
		return headingNode
	default:
		return headingOther
	}
}

// dataView はデータ行を画面の1行にする。
//
// 編集できない理由は目録から引く。internal/edit が持つ日本語をそのまま出すと、
// 英語の画面でその行だけ日本語になる。
func (s *server) dataView(cat *Catalog, line edit.Line, idx columns, badges map[string][]badgeView) lineView {
	v := lineView{
		Number:      line.Number,
		Kind:        lineKindData,
		Key:         line.Key(),
		Speaker:     idx.at(line.Fields, idx.speaker),
		Source:      idx.at(line.Fields, idx.source),
		Translation: line.Translation(),
		Editable:    line.Editable,
		Reason:      s.reasonText(cat, line.Cause),
	}
	if !line.Editable {
		// 訳として出せない行は、生の行を渡して読めるようにする。
		// 改行は含めない（画面に出すのは1行ぶんの文字列）。
		v.Text = strings.TrimRight(line.Text, "\r\n")
	}
	v.KeyKind = keyKind(v.Key)
	v.Badges = badges[v.Key]
	return v
}

// keyKind はキーの形を返す。判定は internal/key に任せる。
func keyKind(value string) string {
	switch {
	case key.LooksLike(value):
		return keyKindHash
	case key.LooksLikeLineID(value):
		return keyKindLine
	default:
		return keyKindOther
	}
}

// badgesByKey はキーごとの状態バッジを作る。
//
// 同じキーが同じカテゴリで2回出ることがある（引き継ぎ候補は行ごとに候補が付く）。
// バッジは「その行に何が当たっているか」を示すものなので、カテゴリで重複を消す。
// 数を知りたいときのために件数は別に出してあるので、ここで数えなおさない。
func (s *server) badgesByKey(cat *Catalog, findings []diff.Finding,
	filled map[string]struct{}) map[string][]badgeView {

	out := make(map[string][]badgeView)
	seen := make(map[string]map[string]struct{})
	for _, f := range findings {
		if f.Key == "" {
			// キーが空の Finding は行と結び付けられない。件数には入っているので、
			// ここで落としても数字は減らない。
			continue
		}
		id := f.Category.ID()
		if seen[f.Key] == nil {
			seen[f.Key] = make(map[string]struct{})
		}
		if _, dup := seen[f.Key][id]; dup {
			continue
		}
		if f.Category == diff.CatUntranslated {
			if _, ok := filled[f.Key]; ok {
				// 起動後に訳が入った行。件数から引いたぶんと同じ扱いにする。
				// ここを残すと、いま訳した行に「未翻訳」が付いたままになる。
				continue
			}
		}
		seen[f.Key][id] = struct{}{}
		out[f.Key] = append(out[f.Key], badgeView{
			Category: id,
			Label:    s.categoryLabel(cat, f.Category),
			Status:   f.Category.Status().ID(),
			// 識別子が入っていなければ Note をそのまま出す。
			//
			// NoteReason.Text には Note と同じ文字列が入る決まりだが、両方を
			// 別々の欄で持つ以上、片方を入れ忘れる書き方ができてしまう。その
			// とき reasonText は空を返すので、注記が日本語へ落ちるのではなく
			// 画面から消える。消えるのは落ちるより悪いので、ここで拾う。
			// 入れ忘れそのものは TestFindingNotesCarryTheirReason が落とす。
			Note: s.findingNote(cat, f),
		})
	}
	return out
}

// buildCounts はカテゴリごとの件数を、要作業 → 要確認 → 参考 の順に並べる。
//
// 判定できていないカテゴリは Judged を false にして理由を入れる。0 件と書くと
// 「もう何も残っていない」と読まれる（internal/diff の doc.go）。
func (s *server) buildCounts(cat *Catalog, locale string, sum diff.Summary) []countView {
	// [diff.Summary.Counts] には全カテゴリが入っている。並びは Category の値の順が
	// そのまま internal/diff の表示順なので、値で並べ替えるだけでよい。
	all := make([]diff.Category, 0, len(sum.Counts))
	for c := range sum.Counts {
		all = append(all, c)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })

	out := make([]countView, 0, len(all))
	for _, status := range statusOrder {
		for _, c := range all {
			if c.Status() != status {
				continue
			}
			view := countView{
				Category:    c.ID(),
				Label:       s.categoryLabel(cat, c),
				Status:      status.ID(),
				StatusLabel: s.statusLabel(cat, status),
				Judged:      sum.CanJudge(c),
				Count:       sum.Counts[c],
			}
			if !view.Judged {
				view.Reason = s.reasonText(cat, sum.JudgeBlockReason(c))
			}
			if view.Judged && c == diff.CatUntranslated {
				// 起動後に訳が入ったぶんだけ引く。引くだけで、カテゴリの
				// 再判定はしない。0 を下回らせないのは、起動時に「未翻訳」で
				// なかった行へ訳を入れても引かないため（filled との積で数える）
				// だが、念のため床を置く。
				view.Count = view.Count - s.untranslatedFilled(locale)
				if view.Count < 0 {
					view.Count = 0
				}
			}
			out = append(out, view)
		}
	}
	return out
}

// buildStats は「数えたもの」を並べる。数はすべて [diff.Summary] か、
// ファイルの行数そのもの。
func (s *server) buildStats(cat *Catalog, sum diff.Summary, fileLines, dataRows int) []statView {
	stats := []statView{
		{Label: s.cat.T(cat, "stats.file_lines"), Value: fileLines},
		{Label: s.cat.T(cat, "stats.data_lines"), Value: dataRows},
		{Label: s.cat.T(cat, "stats.hash_rows"), Value: sum.HashRows},
		{Label: s.cat.T(cat, "stats.line_rows"), Value: sum.LineRows},
	}
	if sum.BrokenRows > 0 {
		// 0 のときは出さない。実データでは13ロケールとも0件で、毎回「0」と
		// 並べても読む手がかりにならない。1件でも出たらファイルが壊れている。
		stats = append(stats, statView{Label: s.cat.T(cat, "stats.broken_rows"), Value: sum.BrokenRows})
	}
	if sum.HasWorking {
		stats = append(stats,
			statView{Label: s.cat.T(cat, "stats.working_rows"), Value: sum.WorkingRows},
			statView{Label: s.cat.T(cat, "stats.source_missing"), Value: sum.SourceMissing},
		)
	}
	return stats
}

// buildNotes は「何を読んだか」を短い文で並べる。
//
// 件数の欄に出る「判定していません（理由）」と重ならないよう、ここには
// 読んだものと読めなかったものだけを書く。
func (s *server) buildNotes(cat *Catalog, locale string, sum diff.Summary, hasSource bool) []string {
	var notes []string
	if s.untranslatedFilled(locale) > 0 {
		// 件数を局所更新したことを断る。できないこと（カテゴリの再判定）を
		// 黙っていると、翻訳者は画面の数字を publish 後の状態だと読む。
		notes = append(notes, s.cat.T(cat, "note.counts_local"))
	}
	if !hasSource {
		// ヘッダーに source_en 列が無い。原文の欄が空のままになる理由を、
		// ここで言い切る。画面側で「source 列が無い → 作業コピーが無いから」と
		// 組み立てると、根拠と結論の対応が待ち受けと画面の2か所に割れる。
		notes = append(notes, s.cat.T(cat, "note.no_source"))
	}
	switch {
	case sum.HasWorking:
		notes = append(notes, s.cat.T(cat, "note.working_read", "path", s.displayPath(sum.WorkingPath)))
	case sum.WorkingExists:
		notes = append(notes, s.cat.T(cat, "note.working_skipped", "path", s.displayPath(sum.WorkingPath)))
	default:
		notes = append(notes, s.cat.T(cat, "note.working_none", "path", s.displayPath(sum.WorkingPath)))
	}
	if !sum.OrderKeys || !sum.OrderLineIDs {
		notes = append(notes, s.cat.T(cat, "note.order_unreadable"))
	}
	if !sum.OldOrder || sum.OldOrderStale {
		// 断り書きの外枠も、その中に入る理由も、どちらも目録から引く。
		// 内側だけ日本語のまま差し込むと、英語の文の途中に日本語が挟まる。
		why := s.reasonText(cat, sum.JudgeBlockReason(diff.CatCarryover))
		notes = append(notes, s.cat.T(cat, "note.old_order_held", "reason", why))
	}
	return notes
}

// reasonText は internal/diff と internal/edit が返した理由を、画面に出す文面にする。
//
// 識別子があれば目録を引き、無ければ元の日本語をそのまま返す。目録に鍵が無い
// ときも同じで、鍵をそのまま画面に出すことはしない。訳されていない文が出るほうが、
// 何も出ないよりよい。
//
// 落ちたことに人が気づく必要は無い。鍵の抜けは [reason.All] をなぞる試験が
// 見つけるし、識別子を持たない理由（[diff.OldOrderSource] を差し替えた
// 呼び出し側が作る誤り）はそもそも訳しようがない。
// findingNote は行に添える注記を返す。
//
// [diff.Finding] は日本語の Note と識別子つきの NoteReason を別々に持つ。
// 目録を引けるのは後者だが、前者しか入っていない Finding が作られても
// 注記を失わないようにする。
func (s *server) findingNote(cat *Catalog, f diff.Finding) string {
	if f.NoteReason.Empty() {
		return f.Note
	}
	return s.reasonText(cat, f.NoteReason)
}

func (s *server) reasonText(cat *Catalog, why reason.Reason) string {
	if why.ID == "" {
		return why.Text
	}
	// 局所変数に key という名前を使わない。このファイルは internal/key を
	// 取り込んでいて、隠すとあとで1行足したときに解決先が変わる。
	if text := s.cat.T(cat, "reason."+why.ID, why.Args...); text != "reason."+why.ID {
		return text
	}
	return why.Text
}

// categoryLabel はカテゴリの表示名を返す。
//
// 目録に無ければ internal/diff が持つ名前をそのまま使う。diff にカテゴリが
// 増えたとき、目録が追いつくまでのあいだ鍵が生で出るのを避けるため。
func (s *server) categoryLabel(cat *Catalog, c diff.Category) string {
	if label := s.cat.T(cat, "category."+c.ID()); label != "category."+c.ID() {
		return label
	}
	return c.String()
}

// statusLabel は重さの表示名を返す。無ければ internal/diff の名前。
func (s *server) statusLabel(cat *Catalog, st diff.Status) string {
	if label := s.cat.T(cat, "status."+st.ID()); label != "status."+st.ID() {
		return label
	}
	return st.String()
}

// columns は列名から添字を引いたもの。
//
// 受理されるヘッダーは4種あり、speaker 列も source_en 列も無い形がある
// （edit.AcceptedHeaders）。位置を決め打ちにすると、2列のファイルで
// 別の列を話者として出すことになる。
type columns struct {
	speaker int
	source  int
}

// columnIndex はヘッダーから列の位置を引く。無い列は -1。
func columnIndex(header []string) columns {
	idx := columns{speaker: -1, source: -1}
	for i, name := range header {
		switch name {
		case "speaker":
			idx.speaker = i
		case "source_en":
			idx.source = i
		}
	}
	return idx
}

// at は列が実在するときだけ値を返す。
func (c columns) at(fields []string, i int) string {
	if i < 0 || i >= len(fields) {
		return ""
	}
	return fields[i]
}
