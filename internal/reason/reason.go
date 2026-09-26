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
	// JudgeOrderUnreadable は再生順のキーを読めていないこと。
	//
	// 台詞IDだけを要るカテゴリでも、キーも読めていなければこちらになる。再生順が
	// 丸ごと読めていないときに台詞IDのことだけを言うと、直すべきものを取り違える。
	JudgeOrderUnreadable = "judge_order_unreadable"
	// JudgeOrderNoLineIDs は再生順のキーは読めているが、台詞ID (line_id) が
	// 1件も無いこと。
	//
	// JudgeOrderUnreadable と分けてあるのは、キーだけで判定できるカテゴリ
	// （台本から消えた行など）には件数が出ているからである。同じ「再生順を
	// 読めていません」と書くと、その件数と食い違う。
	JudgeOrderNoLineIDs = "judge_order_no_line_ids"
	// JudgeOldOrderUnreadable は1つ前の版の再生順を取り出せていないこと。
	// より細かい理由が分かるときは OldOrder* のほうが入る。
	JudgeOldOrderUnreadable = "judge_old_order_unreadable"
	// JudgeOrderNoNorms は再生順に norm 列の値が1つも無いこと。
	// 正規化した英文のハッシュが無いと、引き継ぎ元を探せない。
	JudgeOrderNoNorms = "judge_order_no_norms"
	// JudgeNoLayoutRisks はゲームが測ったはみ出しの記録が無いこと。
	// 測っていないので、「はみ出す行は無い」とは言えない。
	JudgeNoLayoutRisks = "judge_no_layout_risks"
	// JudgeLayoutRisksNotRead はその記録があるのに読まなかったこと（--no-working）。
	JudgeLayoutRisksNotRead = "judge_layout_risks_not_read"
	// JudgeOrderUnclosed は、再生順（data/script_order.csv）で開いた引用符が
	// ファイルの終わりまで閉じないこと。置換は line（引用符が開いた物理行）。
	//
	// 全体を解釈して読むと、その行から後ろがすべて1つの値に崩れる。再生順は
	// どのカテゴリの判定にも位置の根拠にも使うので、報告全体を判定しない。
	JudgeOrderUnclosed = "judge_order_unclosed"
	// JudgePublishedUnclosed は、そのロケールの公開ファイルで開いた引用符が
	// 閉じないこと。置換は line。そのロケールはどのカテゴリも判定しない。
	JudgePublishedUnclosed = "judge_published_unclosed"
	// JudgeWorkingUnclosed は、作業コピーで開いた引用符が閉じないこと。
	// 置換は line。作業コピーを要るカテゴリを判定しない。
	JudgeWorkingUnclosed = "judge_working_unclosed"
	// JudgeLayoutRisksUnclosed は、はみ出しの記録で開いた引用符が閉じないこと。
	// 置換は line。
	JudgeLayoutRisksUnclosed = "judge_layout_risks_unclosed"
	// JudgeOtherPublishedUnclosed は、ほかのロケールの公開ファイルで開いた引用符が
	// 閉じないこと。置換は locales（そのロケールの名前を ", " でつないだもの）。
	// 他のロケールと比べるカテゴリを判定しない。
	JudgeOtherPublishedUnclosed = "judge_other_published_unclosed"
)

// 1つ前の版の再生順を取り出せない理由（internal/diff の oldorder.go と load.go）。
const (
	// OldOrderNoGit は git を起動できないこと。
	OldOrderNoGit = "old_order_no_git"
	// OldOrderNoRepository は git リポジトリでないか、コミットが無いこと。
	OldOrderNoRepository = "old_order_no_repository"
	// OldOrderNotTracked は再生順が git の管理下に無いこと。
	// git add もしていないか、リポジトリの外にある。
	OldOrderNotTracked = "old_order_not_tracked"
	// OldOrderNotCommitted は再生順を git add しただけで、まだコミットしていないこと。
	// git はこれを追跡中と答えるので、OldOrderNotTracked とは分けてある。
	OldOrderNotCommitted = "old_order_not_committed"
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
	// NoteDroppedSourceCRLF は source_en のハッシュが key と合わないが、原文の中の
	// CRLF を LF にすると合うこと。表計算ソフトなどで作業コピーを保存し直すと起きる
	// （決まったことのそのほか 7）。
	NoteDroppedSourceCRLF = "note_dropped_source_crlf"
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

// 引き継ぎ元の候補の注記（internal/diff の carrySource.cause）。
//
// 置換は source（旧キーと位置を " / " で連ねたもの）、key、pos、distance。
// 引き継ぎ先（NoteCarry*）と分けてあるのは、報告が付く行と指す向きが逆だから。
const (
	// NoteCarryFromSame は正規化した英文が一致したこと（書式と記号だけの違い）。
	NoteCarryFromSame = "note_carry_from_same"
	// NoteCarryFromSimilar は指紋が近いこと。置換の distance にその距離が入る。
	NoteCarryFromSimilar = "note_carry_from_similar"
)

// NoteLayoutRisk はゲームが測ってはみ出しの恐れがあると出た行であること
// （internal/diff の LayoutRisk.cause）。
//
// 置換は ratio、axis、required、available。どれもゲームが書いた綴りのまま渡す。
const NoteLayoutRisk = "note_layout_risk"

// NoteLocaleGap は他のロケールにあってこのロケールに無いこと。置換は count。
const NoteLocaleGap = "note_locale_gap"

// タグの開閉がそろわない行の注記（internal/diff の TagBalance.Note）。
//
// 置換は tags（当たったタグを空白で連ねたもの）。両方あるときだけ unclosed と
// unopened に分かれる。3つに分けてあるのは、片方が空のときに「開始の無い
// 終了タグ: 」のような見出しだけを画面に残さないためである。
const (
	// NoteTagUnclosed は開始があるのに終了が無いタグがあること。置換は tags。
	NoteTagUnclosed = "note_tag_unclosed"
	// NoteTagUnopened は終了だけがあるタグがあること。置換は tags。
	NoteTagUnopened = "note_tag_unopened"
	// NoteTagUnclosedUnopened は両方あること。置換は unclosed と unopened。
	NoteTagUnclosedUnopened = "note_tag_unclosed_unopened"
)

// 原文とタグの構成が違う行の注記（internal/diff の TagDiff.Note）。
//
// 開閉の3つ（NoteTag*）と分けてあるのは、直し方が違うからである。開閉は
// 訳の中だけで直せるが、こちらは原文の側を見て合わせる。置換の作りは
// 同じで、片方だけのときは tags、両方あるときは missing と extra に分かれる。
const (
	// NoteTagMissing は原文にあって訳に無いタグがあること。置換は tags。
	NoteTagMissing = "note_tag_missing"
	// NoteTagExtra は訳にあって原文に無いタグがあること。置換は tags。
	NoteTagExtra = "note_tag_extra"
	// NoteTagMissingExtra は両方あること。置換は missing と extra。
	NoteTagMissingExtra = "note_tag_missing_extra"
)

// 保存できない理由（internal/edit の file.go）。
const (
	// EditNoHeader はヘッダー行が無いこと。
	EditNoHeader = "edit_no_header"
	// EditBadHeader はヘッダーが受理される4種のいずれでもないこと。
	// 置換は line（行番号）と text（そのヘッダー行）。
	EditBadHeader = "edit_bad_header"
	// EditFieldCount はフィールド数がヘッダーと合わないこと。
	// 置換は header（ヘッダーの列数）と row（その行の列数）。
	EditFieldCount = "edit_field_count"
	// EditNoSuchLine はその ID の行が無いこと。
	EditNoSuchLine = "edit_no_such_line"
	// EditNotDataLine はデータ行でないこと。置換は kind（行の種類）。
	EditNotDataLine = "edit_not_data_line"
	// EditNoNUL は訳に NUL が入っていること。
	EditNoNUL = "edit_no_nul"
	// EditBadUTF8 は訳が正しいUTF-8でないこと。
	EditBadUTF8 = "edit_bad_utf8"
	// EditLineLooksLikeRecord は、書こうとした訳の行（多くは改行の後ろの行）が、その
	// 行だけで読むとレコードに見えること（キーの形で始まるか、列の数がヘッダーと同じ。
	// csvfile.FindSwallows）。publish は引用符の閉じ誤りの疑いとして止めるので、書かない。
	// 置換は line（訳の何行目か。1始まり）。
	EditLineLooksLikeRecord = "edit_line_looks_like_record"
	// EditUnclosedQuote は、開いた引用符がファイルの終わりまで閉じないこと。
	// 置換は line（引用符が開いた物理行）。ファイル全体を読み取り専用にする。
	EditUnclosedQuote = "edit_unclosed_quote"
	// EditCROnly は、ファイルの行の区切りがすべて単独の CR であること。ゲームの
	// 読み方（CsvReader）は引用の外の CR を捨てるので、このファイルを1行と読む。
	// ファイル全体を読み取り専用にする。
	EditCROnly = "edit_cr_only"
	// EditSwallow は、行をまたぐレコードが後ろの行を値に飲み込んだと見られること
	// （引用符の閉じ誤りの疑い。csvfile.FindSwallows）。置換は line（飲み込まれたと
	// 疑う物理行）。
	EditSwallow = "edit_swallow"
	// EditGameDisagrees は、ゲームの読み方（CsvReader の移植）で読むと、そのレコードの
	// 値が違って読まれること（csvfile.CSharpDisagreements）。置換は column（食い違った
	// 最初の列）。
	EditGameDisagrees = "edit_game_disagrees"
	// EditGameMissesRecord は、ゲームの読み方でそのレコードが見つからないこと
	// （csvfile.CSharpDisagreements の Column が空）。
	EditGameMissesRecord = "edit_game_misses_record"
	// EditNoKeyOrSource は、原文（source_en 列。無い形のファイルでは空と同じ）が空で、
	// key 列（前後の空白を除く）が16桁のキー（小文字にして16桁の16進）でも台詞ID でも
	// なく、publish がキーを決められないこと。publish はこのレコードを捨てる（移植仕様
	// R17。判定は publish.Keyless）ので、書いた訳は公開されない（決まったことの 24）。
	// 識別子の名前は、key 列も原文も空のレコードだけを当てていたころのもので、画面と
	// 目録が使うので変えていない。
	EditNoKeyOrSource = "edit_no_key_or_source"
	// EditRecheckFailed は、書く前の事後確認（決まったことのそのほか 4）が外れたこと。
	// 書き換えたレコードを読み直すと、書いた訳のほかの値や、ほかの行まで変わって
	// 読める。置換は line（そのレコードの最初の物理行）。
	EditRecheckFailed = "edit_recheck_failed"
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
	// PublishBaseDrift は、ゲームに入っている公開ファイルがコミット済みと
	// 食い違っていること。置換は locale と count。
	//
	// このとき作業コピーは別の土台の上に建っているので、入力にすると新しい
	// コミットが古い版へ巻き戻る。訳は消えないので PublishRowGone では捕まらない。
	PublishBaseDrift = "publish_base_drift"
)

// 読むと訳や原文を取り違える形（internal/publish の shape.go）。
//
// publish は上流 main と同じくファイル全体を解釈して読む。読み手は規則どおりに
// 読むだけで、引用符の閉じ誤りと正当な複数行の値を見分けない。そのまま書くと、
// 英語の原文が訳として公開されたり、訳が黙って落ちたりする形を、書く前にファイルの
// 形そのものから見つける。どのファイルの何行目かは、理由の外に持つ。
const (
	// PublishNoKeyColumn は、ヘッダーに key 列も source_en 列も無いこと。
	// どの行もキーを決められず、すべて捨てられる。
	PublishNoKeyColumn = "publish_no_key_column"
	// PublishNoTranslationColumn は、ヘッダーに translation 列が無いこと。
	// すべての行が訳の無い行として読まれる。
	PublishNoTranslationColumn = "publish_no_translation_column"
	// PublishHashHeader は、ヘッダーの最初の列名が '#' で始まること。上流はその
	// ヘッダーを飛ばし、次の行をヘッダーにするので、訳を1行も公開しない。
	PublishHashHeader = "publish_hash_header"
	// PublishRowsUnread は、空でない行があるのに1行も読めないこと（行の区切りが
	// LF・CRLF・CR 以外）。
	PublishRowsUnread = "publish_rows_unread"
	// PublishUnclosedQuote は、開いた引用符がファイルの終わりまで閉じないこと。
	PublishUnclosedQuote = "publish_unclosed_quote"
	// PublishSwallowKeyShaped は、行をまたぐレコードの続きの物理行が、単独で読むと
	// キーの形で始まるレコードに見えること。置換は line（その物理行）。
	PublishSwallowKeyShaped = "publish_swallow_key_shaped"
	// PublishSwallowSameColumns は、続きの物理行が、単独で読むとヘッダーと同じ
	// 列の数のレコードに見えること。置換は line。
	PublishSwallowSameColumns = "publish_swallow_same_columns"
	// PublishSwallowTextAfterQuote は、行をまたいだ引用がその物理行で閉じ、閉じ
	// 引用符のすぐ後ろに文字が続くこと。置換は line。
	PublishSwallowTextAfterQuote = "publish_swallow_text_after_quote"
	// PublishLoneCR は、値の中に単独の CR（後ろに LF の続かない CR）があること。
	// 置換は column（列名）。原文（source_en 列）の値なら key_kind（その行のキーの
	// 決まり方。internal/publish の LoneCRKey*）も入る。原文を直してよいかは
	// キーの決まり方で変わり、CLI の直し方がこれで分かれる。目録の文面は key_kind を
	// 使わない。
	PublishLoneCR = "publish_lone_cr"
	// PublishCRCut は、引用符で囲まない値が引用の外の単独の CR で切れたと見られる
	// こと。置換は line（切れた後半が読まれる物理行）。
	PublishCRCut = "publish_cr_cut"
	// PublishOrderLineBreak は、再生順のデータの値のうち、publish が引用せずに
	// そのまま書く値（見出しの文言と order 列）に CR か LF があること。置換は
	// column（列名）。
	PublishOrderLineBreak = "publish_order_line_break"
)

// all は [All] が返す並び。定義した順のまま持つ。
var all = []string{
	JudgeWorkingNotRead, JudgeWorkingMissing, JudgeOrderUnreadable, JudgeOrderNoLineIDs,
	JudgeOldOrderUnreadable, JudgeOrderNoNorms, JudgeNoLayoutRisks, JudgeLayoutRisksNotRead,
	JudgeOrderUnclosed, JudgePublishedUnclosed, JudgeWorkingUnclosed, JudgeLayoutRisksUnclosed,
	JudgeOtherPublishedUnclosed,

	OldOrderNoGit, OldOrderNoRepository, OldOrderNotTracked, OldOrderNotCommitted,
	OldOrderOnlyOneVersion, OldOrderGitFailed, OldOrderUnreadable, OldOrderNoLineIDs,
	OldOrderStale,

	NoteUntranslated, NoteVanished, NoteStrayLineID, NoteNotPublished,
	NoteScriptGap, NoteUnknownOrigin,

	NoteDroppedBroken, NoteDroppedMismatch, NoteDroppedSourceCRLF,

	NoteCarryMoved, NoteCarryCopied, NoteCarryCopiedUnknown,

	NoteCarryFromSame, NoteCarryFromSimilar,

	NoteLayoutRisk,

	NoteLocaleGap,

	NoteTagUnclosed, NoteTagUnopened, NoteTagUnclosedUnopened,

	NoteTagMissing, NoteTagExtra, NoteTagMissingExtra,

	EditNoHeader, EditBadHeader, EditFieldCount,
	EditNoSuchLine, EditNotDataLine, EditNoNUL, EditBadUTF8,
	EditLineLooksLikeRecord, EditUnclosedQuote, EditCROnly, EditSwallow,
	EditGameDisagrees, EditGameMissesRecord, EditNoKeyOrSource, EditRecheckFailed,

	PublishRowGone, PublishTranslationCleared, PublishBaseDrift,

	PublishNoKeyColumn, PublishNoTranslationColumn, PublishHashHeader, PublishRowsUnread,
	PublishUnclosedQuote, PublishSwallowKeyShaped, PublishSwallowSameColumns,
	PublishSwallowTextAfterQuote, PublishLoneCR, PublishCRCut, PublishOrderLineBreak,
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
