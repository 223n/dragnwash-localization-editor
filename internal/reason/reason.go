// Package reason は「なぜそうなったか」を、文面ではなく識別子と置換の組で持つ。
//
// # なぜ別のパッケージにするか
//
// internal/diff と internal/edit は理由を日本語の文字列として持っている。
// CLI（dwloc diff）はその文面をそのまま出すので消せないが、画面（internal/web）は
// 目録で差し替えたい。差し替えるには鍵が要る。
//
// 鍵を internal/web に置くと、diff と edit が web を参照することになって層が
// 逆転する。diff に置くと edit が diff を参照することになる。どちらも、
// 理由を1つ足すたびに依存の向きを考え直す形である。だから、文字を1つも持たない
// 識別子だけのパッケージを一番下に置き、diff と edit と web の3つがここを見る。
//
// このパッケージは画面のことを何も知らない。持っているのは ASCII の識別子と、
// 「識別子と置換の組と、組み立て済みの文面」を1つにまとめる入れ物だけである。
// 文面の組み立ては internal/web が目録を引いて行う。
//
// # 文面を2か所に持つことについて
//
// 同じ日本語が internal/diff（または internal/edit）と ja.json の両方にある。
// 写しはいずれずれる。それでもこの形にしたのは、CLI の出力を1バイトも変えない
// という条件があるからである。CLI は目録を読まない（--ui-lang があるのは
// edit だけ）ので、日本語は元の場所に残さなければならない。
//
// ずれたことに気づけるよう、[All] が返す識別子が ja と en の目録にそろって
// いることを internal/web の試験が見ている。
package reason

// Reason は理由1つ分。
//
// 3つを一緒に持つのは、受け取る側が2種類あるからである。目録を持つ側
// （internal/web）は ID と Args から訳された文面を組み立て、目録を持たない側
// （CLI、テストの失敗メッセージ）は Text をそのまま出す。どちらか一方しか
// 持たない形にすると、もう一方が理由を出せなくなる。
type Reason struct {
	// ID は ASCII の安定した識別子。目録の鍵は "reason." + ID になる。
	//
	// 空のこともある。[OldOrderSource] を差し替えた呼び出し側が返す誤りのように、
	// このパッケージが名前を付けていない理由が残っているため。空のときは
	// 受け取る側が Text へ落とす。
	ID string
	// Args は置換の名前と値を交互に並べたもの。目録の {name} に対応する。
	//
	// map ではなく並びにしてあるのは、internal/web の expand がこの形で
	// 受け取るからである。map にすると、渡す直前に並びへ直す場所が要る。
	Args []string
	// Text は組み立て済みの日本語。目録に鍵が無いときの落としどころ。
	//
	// 訳されていない文が出るほうが、何も出ないよりよい。空文字を返すと、
	// 画面から理由が消えるだけで、どこが抜けているのか分からなくなる。
	Text string
}

// New は識別子と文面から [Reason] を作る。args は名前と値を交互に並べる。
func New(id, text string, args ...string) Reason {
	// args は写して持つ。呼び出し側が渡した並びをそのまま抱えると、あとで
	// その並びを並べ替えたときに理由の中身が変わる。All() が写しを返すのと
	// 同じ理由で、このパッケージの外から中身を動かせないようにしておく。
	return Reason{ID: id, Args: append([]string(nil), args...), Text: text}
}

// String は組み立て済みの文面を返す。
//
// fmt の %s と %v がこれを使うので、日本語のままでよい側（CLI、テストの失敗
// メッセージ）は Reason をそのまま書式に渡せる。文字列から Reason へ変えた
// 呼び出し側の書き換えが、これで最小限になる。
func (r Reason) String() string { return r.Text }

// Empty は理由が1つも入っていないかを返す。
func (r Reason) Empty() bool { return r.ID == "" && r.Text == "" }

// 判定できない理由（internal/diff の judgeBlockReason）。
const (
	// JudgeWorkingNotRead は作業コピーがあるのに --no-working で読まなかったこと。
	JudgeWorkingNotRead = "judge_working_not_read"
	// JudgeWorkingMissing は作業コピーのファイルが無いこと。
	JudgeWorkingMissing = "judge_working_missing"
	// JudgeOrderUnreadable は再生順のキーか台詞IDを読めていないこと。
	JudgeOrderUnreadable = "judge_order_unreadable"
	// JudgeOldOrderUnreadable は1つ前の版の再生順を取り出せていないこと。
	// より細かい理由が分かるときは OldOrder* のほうが入る。
	JudgeOldOrderUnreadable = "judge_old_order_unreadable"
)

// 1つ前の版の再生順を取り出せない理由（internal/diff の oldorder.go と load.go）。
const (
	// OldOrderNoGit は git を起動できないこと。
	OldOrderNoGit = "old_order_no_git"
	// OldOrderNoRepository は git リポジトリでないか、コミットが無いこと。
	OldOrderNoRepository = "old_order_no_repository"
	// OldOrderNotTracked は再生順が git の管理下に無いこと。
	OldOrderNotTracked = "old_order_not_tracked"
	// OldOrderOnlyOneVersion は再生順の履歴が1版しか無いこと。
	OldOrderOnlyOneVersion = "old_order_only_one_version"
	// OldOrderGitFailed は git は動いたが取り出しに失敗したこと。
	OldOrderGitFailed = "old_order_git_failed"
	// OldOrderUnreadable は取り出せたが再生順として読めないこと。
	OldOrderUnreadable = "old_order_unreadable"
	// OldOrderNoLineIDs は読めたが台詞IDとキーの組が1つも無いこと。
	OldOrderNoLineIDs = "old_order_no_line_ids"
	// OldOrderStale は読めた旧版が、いまの版と同じ内容に見えること。
	OldOrderStale = "old_order_stale"
)

// カテゴリごとの注記（internal/diff の categoryTable）。
const (
	// NoteUntranslated は訳が空であること。
	NoteUntranslated = "note_untranslated"
	// NoteVanished は前回の公開時は再生順にあったこと。
	NoteVanished = "note_vanished"
	// NoteStrayLineID は再生順に同じ台詞IDが無いこと。
	NoteStrayLineID = "note_stray_line_id"
	// NoteNotPublished はどのロケールにも訳が無いこと。
	NoteNotPublished = "note_not_published"
	// NoteScriptGap は再生順に無いこと。
	NoteScriptGap = "note_script_gap"
	// NoteUnknownOrigin は再生順に無いが UI 文言かもしれないこと。
	NoteUnknownOrigin = "note_unknown_origin"
)

// publish が行を捨てる理由（internal/diff の droppedReason）。
const (
	// NoteDroppedBroken は key が16桁hexでも台詞IDでもないこと。
	NoteDroppedBroken = "note_dropped_broken"
	// NoteDroppedMismatch は source_en のハッシュが key と合わないこと。
	NoteDroppedMismatch = "note_dropped_mismatch"
)

// 引き継ぎ候補の注記（internal/diff の carryTarget.note）。
//
// 置換は target（引き継ぎ先のキーと位置を " / " で連ねたもの）と live
// （旧キーがいまも生きている位置）。key と pos も名前で引けるよう一緒に渡すが、
// 目録の文面が使うのは target のほうである。位置が空のときに " / " を
// 添えないという分岐を、目録の側に持ち込まないためにまとめてある。
const (
	// NoteCarryMoved は移動（旧キーはもう再生順に無い）。
	NoteCarryMoved = "note_carry_moved"
	// NoteCarryCopied は複製で、旧キーが生きている位置まで分かる場合。
	NoteCarryCopied = "note_carry_copied"
	// NoteCarryCopiedUnknown は複製だが、生きている位置が分からない場合。
	NoteCarryCopiedUnknown = "note_carry_copied_unknown"
)

// NoteLocaleGap は他のロケールにあってこのロケールに無いこと。置換は count。
const NoteLocaleGap = "note_locale_gap"

// 保存できない理由（internal/edit の file.go）。
const (
	// EditNoHeader はヘッダー行が無いこと。
	EditNoHeader = "edit_no_header"
	// EditBadHeader はヘッダーが受理される4種のいずれでもないこと。
	// 置換は line（行番号）と text（そのヘッダー行）。
	EditBadHeader = "edit_bad_header"
	// EditNotRecord はその行がレコードとして読まれないこと。
	EditNotRecord = "edit_not_record"
	// EditFieldCount はフィールド数がヘッダーと合わないこと。
	// 置換は header（ヘッダーの列数）と row（その行の列数）。
	EditFieldCount = "edit_field_count"
	// EditNoSuchLine はその行番号が無いこと。
	EditNoSuchLine = "edit_no_such_line"
	// EditNotDataLine はデータ行でないこと。置換は kind（行の種類）。
	EditNotDataLine = "edit_not_data_line"
	// EditNoNewline は訳に改行が入っていること。
	EditNoNewline = "edit_no_newline"
	// EditNoNUL は訳に NUL が入っていること。
	EditNoNUL = "edit_no_nul"
	// EditBadUTF8 は訳が正しいUTF-8でないこと。
	EditBadUTF8 = "edit_bad_utf8"
)

// 書き出すと訳が失われる理由（internal/publish の guard.go）。
//
// publish は入力から公開ファイルを作り直すので、入力が途中までだったり壊れて
// いたりすると、コミット済みの訳がその場で消える。消える行1つずつに付く理由が
// ここに入る。
const (
	// PublishRowGone は、いまの公開ファイルにある行が新しい出力に無いこと。
	PublishRowGone = "publish_row_gone"
	// PublishTranslationCleared は、行はあるが訳が空になること。
	PublishTranslationCleared = "publish_translation_cleared"
)

// all は [All] が返す並び。定義した順のまま持つ。
var all = []string{
	JudgeWorkingNotRead, JudgeWorkingMissing, JudgeOrderUnreadable, JudgeOldOrderUnreadable,

	OldOrderNoGit, OldOrderNoRepository, OldOrderNotTracked, OldOrderOnlyOneVersion,
	OldOrderGitFailed, OldOrderUnreadable, OldOrderNoLineIDs, OldOrderStale,

	NoteUntranslated, NoteVanished, NoteStrayLineID, NoteNotPublished,
	NoteScriptGap, NoteUnknownOrigin,

	NoteDroppedBroken, NoteDroppedMismatch,

	NoteCarryMoved, NoteCarryCopied, NoteCarryCopiedUnknown,

	NoteLocaleGap,

	EditNoHeader, EditBadHeader, EditNotRecord, EditFieldCount,
	EditNoSuchLine, EditNotDataLine, EditNoNewline, EditNoNUL, EditBadUTF8,

	PublishRowGone, PublishTranslationCleared,
}

// All はこのパッケージが名前を付けた識別子を全部返す。
//
// 目録がそろっているかを確かめるための窓である。internal/web の試験がここを
// 回し、ja と en に "reason." + ID の鍵があることを見る。識別子を足したのに
// 目録へ足し忘れると、英語の画面にそこだけ日本語が出る。それを人の目に
// 頼らずに見つけるための並びなので、識別子を足したらここにも足すこと。
func All() []string {
	out := make([]string, len(all))
	copy(out, all)
	return out
}
