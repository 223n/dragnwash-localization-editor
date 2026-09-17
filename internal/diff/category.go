package diff

import "github.com/223n/dragnwash-localization-editor/internal/reason"

// Status は報告の重さ。値が大きいほど重い。
//
// 3段にしてあるのは、翻訳者が「まず何を見ればいいか」を自分で決めなくて済む
// ようにするため。並べ方も 要作業 → 要確認 → 参考 に固定する。
type Status int

const (
	// StatusInfo は参考。件数が変わっても普通は何もしなくてよい。
	StatusInfo Status = iota
	// StatusTodo は要作業。翻訳者が手を動かせば減る。
	StatusTodo
	// StatusReview は要確認。放っておくと訳が失われうるので、人が見る。
	StatusReview
)

// String は画面に出す日本語名を返す。
func (s Status) String() string {
	switch s {
	case StatusTodo:
		return "要作業"
	case StatusReview:
		return "要確認"
	case StatusInfo:
		return "参考"
	default:
		return "参考"
	}
}

// id は CSV に書く ASCII 識別子を返す。
//
// 日本語名と分けてあるのは、CSV を表計算に貼ったあとで並べ替えや絞り込みに
// 使う値だから。表示名を変えたときに、既に書かれたCSVの意味が変わらないようにする。
func (s Status) id() string {
	switch s {
	case StatusTodo:
		return "todo"
	case StatusReview:
		return "review"
	case StatusInfo:
		return "info"
	default:
		return "info"
	}
}

// Category は報告の種別。
//
// 並び順がそのまま表示順になる。要作業2つ → 要確認3つ → 参考3つ。
type Category int

const (
	// CatUntranslated は作業コピーに原文があって訳が空の行。
	CatUntranslated Category = iota
	// CatLocaleGap は他のロケールにあって、このロケールに無い行。
	CatLocaleGap
	// CatVanished は前回の公開時は再生順にあったのに、いまの再生順に無い行。
	CatVanished
	// CatCarryover は訳の引き継ぎ先の候補がある行。
	CatCarryover
	// CatDropped は publish を回すと捨てられる作業コピーの行。
	CatDropped
	// CatStrayLineID は再生順に無い台詞ID行。
	CatStrayLineID
	// CatNotPublished はどのロケールにも訳が無い行。
	CatNotPublished
	// CatScriptGap は再生順に無いが、話者名が残っている公開行。
	CatScriptGap
	// CatUnknownOrigin は由来を判定できない公開行。
	CatUnknownOrigin
)

// categories は表示順に並べた全カテゴリ。
var categories = []Category{
	CatUntranslated, CatLocaleGap,
	CatVanished, CatCarryover, CatDropped, CatStrayLineID,
	CatNotPublished, CatScriptGap, CatUnknownOrigin,
}

// categoryInfo はカテゴリごとの固定の情報。
type categoryInfo struct {
	name   string // 画面に出す日本語名
	id     string // CSV に書く ASCII 識別子
	status Status // 重さ。カテゴリごとに固定
	// needsWorking は作業コピーが無いと判定できないかどうか。
	// 判定できないことを 0 件と書くと「作業は残っていない」と読まれるので、
	// 表示側で「判定できません」と書き分けるための印。
	needsWorking bool
	// needsOrderKeys は再生順のキーが1種も読めていないと判定できないかどうか。
	// 「再生順に無い」を根拠にするカテゴリに立てる。再生順が空のまま当てると、
	// 公開行のほぼ全部がそのカテゴリに化ける。
	needsOrderKeys bool
	// needsOrderLineIDs は再生順の台詞IDについて同じ意味。
	needsOrderLineIDs bool
	// needsOldOrder は1つ前の版の再生順が無いと判定できないかどうか。
	// git から取り出せない環境（git が無い、リポジトリでない、履歴が1版しかない）
	// でも道具そのものは動くので、ここも「0 件」ではなく理由を書く印として立てる。
	needsOldOrder bool
	// note は Finding.Note の既定値。CSV の note 列に入る。
	note string
	// noteID は note に対応する安定した識別子（internal/reason）。
	//
	// 文面と別に持つのは、CLI が note をそのまま出す一方で、画面（internal/web）は
	// 目録で差し替えるためである。文面を消して識別子だけにすると CLI が壊れる。
	noteID string
	// detail は一覧の前に出す説明。空なら出さない。
	detail []string
}

// カテゴリごとの note。Finding.Note に入り、CSV の最終列になる。
const (
	noteUntranslated    = "訳が空です"
	noteVanished        = "前回の公開時は再生順にありました"
	noteStrayLineID     = "再生順に同じ台詞IDがありません"
	noteNotPublished    = "どのロケールにも訳がありません"
	noteScriptGap       = "再生順にありません"
	noteUnknownOrigin   = "再生順にありませんが、UI 文言かもしれません"
	noteDroppedBroken   = "key が16桁hexでも line: でもありません"
	noteDroppedMismatch = "source_en のハッシュが key と一致しません"
)

// categoryTable はカテゴリの定義表。Category の値を添字にする。
var categoryTable = map[Category]categoryInfo{
	CatUntranslated: {
		name: "未翻訳", id: "untranslated", status: StatusTodo, needsWorking: true,
		note: noteUntranslated, noteID: reason.NoteUntranslated,
		detail: []string{
			"作業コピーに原文があり、訳が空の行です。",
		},
	},
	CatLocaleGap: {
		name: "他のロケールにあって無い行", id: "locale_gap", status: StatusTodo,
		detail: []string{
			"他のロケールには訳がありますが、このロケールにはありません。",
		},
	},
	CatVanished: {
		name: "台本から消えた行", id: "vanished", status: StatusReview, needsOrderKeys: true,
		note: noteVanished, noteID: reason.NoteVanished,
		detail: []string{
			"前回の公開時は再生順にありましたが、いまの再生順にありません。",
			"原文が変わってキーが変わった可能性があります。",
		},
	},
	CatCarryover: {
		// note は空。行ごとに引き継ぎ先が違うので、Finding.Note へ1件ずつ入れる。
		name: "引き継ぎ候補", id: "carryover", status: StatusReview,
		needsOrderKeys: true, needsOldOrder: true,
		detail: []string{
			"原文が変わってキーが変わった行の、引き継ぎ先の見当です。",
			"1つ前の版の再生順を git から読み、新旧を台詞ID (line_id) で突き合わせて求めます。",
			"訳は書き換えていません。中身を確かめてから、作業コピーで移してください。",
		},
	},
	CatDropped: {
		name: "publish で捨てられる行", id: "dropped", status: StatusReview, needsWorking: true,
		detail: []string{
			"このまま publish すると、これらの行の訳は公開ファイルに載りません。",
		},
	},
	CatStrayLineID: {
		name: "台本に無い台詞ID行", id: "stray_line_id", status: StatusReview, needsOrderLineIDs: true,
		note: noteStrayLineID, noteID: reason.NoteStrayLineID,
		detail: []string{
			"再生順に同じ台詞IDがありません。publish すると末尾のブロックへ回ります。",
		},
	},
	CatNotPublished: {
		name: "どのロケールにも訳が無い行", id: "not_published", status: StatusInfo,
		note: noteNotPublished, noteID: reason.NoteNotPublished,
	},
	CatScriptGap: {
		name: "台本に無い台詞行", id: "script_gap", status: StatusInfo, needsOrderKeys: true,
		note: noteScriptGap, noteID: reason.NoteScriptGap,
	},
	CatUnknownOrigin: {
		name: "由来を判定できない行", id: "unknown_origin", status: StatusInfo, needsOrderKeys: true,
		note: noteUnknownOrigin, noteID: reason.NoteUnknownOrigin,
		detail: []string{
			"公開ファイルだけでは UI 文言と孤児を見分けられません。",
		},
	},
}

// String は画面に出す日本語名を返す。
func (c Category) String() string {
	return categoryTable[c].name
}

// ID は CSV に書く ASCII 識別子を返す。
func (c Category) ID() string {
	return categoryTable[c].id
}

// Status はそのカテゴリの重さを返す。カテゴリごとに固定で、件数や内容では変わらない。
func (c Category) Status() Status {
	return categoryTable[c].status
}

// needsWorking は作業コピーが無いと判定できないカテゴリかを返す。
func (c Category) needsWorking() bool {
	return categoryTable[c].needsWorking
}

// needsOrderKeys は再生順のキーが読めていないと判定できないカテゴリかを返す。
func (c Category) needsOrderKeys() bool {
	return categoryTable[c].needsOrderKeys
}

// needsOrderLineIDs は再生順の台詞IDが読めていないと判定できないカテゴリかを返す。
func (c Category) needsOrderLineIDs() bool {
	return categoryTable[c].needsOrderLineIDs
}

// needsOldOrder は1つ前の版の再生順が無いと判定できないカテゴリかを返す。
func (c Category) needsOldOrder() bool {
	return categoryTable[c].needsOldOrder
}

// note は Finding.Note の既定値を返す。
func (c Category) note() string {
	return categoryTable[c].note
}

// noteReason は Finding.NoteReason の既定値を返す。
//
// 注記を持たないカテゴリ（引き継ぎ候補のように行ごとに違うもの）では空を返す。
// 空の理由は画面にも CSV にも何も出さないので、note と同じ扱いになる。
func (c Category) noteReason() reason.Reason {
	info := categoryTable[c]
	if info.note == "" {
		return reason.Reason{}
	}
	return reason.New(info.noteID, info.note)
}

// detail は一覧の前に出す説明を返す。
func (c Category) detail() []string {
	return categoryTable[c].detail
}
