package diff

import "github.com/223n/dragnwash-localization-editor/internal/reason"

// このファイルは判定を1つも足さない。すでにある [Summary.canJudge] と
// judgeBlockReason と [Status.id] と、カテゴリの表の印（needsOrderLineIDs）と
// 表示順（[categories]）を、パッケージの外から読めるようにするだけである。
//
// 外へ出す理由。画面（internal/web）はカテゴリごとの件数を出すが、0 という数字には
// 「1件も無い」と「判定していない」の2つの意味がある。後者を 0 件と書くと
// 「移すべき訳は無い」と読まれ、翻訳者は訳を捨てる（doc.go の引き継ぎ候補の項）。
//
// その区別を画面側で組み直すと、「どのカテゴリが作業コピーや旧再生順を要るか」
// という前提の写しが2か所にできる。写しはいずれずれる。ずれたときに壊れるのは
// 「判定していないのに 0 件と書く」側で、それはこのパッケージが doc.go で
// 名指しで避けている誤りそのものである。だから判断はここに1つだけ置き、
// 外へは読み取りの窓だけを開ける。
//
// 既存の関数は1行も変えていない。ここにあるのは委譲と、表の印を読むことだけである。

// CanJudge はそのカテゴリを判定できたかを返す。
//
// false のとき [Summary.Counts] の 0 は「1件も無い」ではなく「判定していない」を
// 意味する。件数を出す側は、false なら数ではなく [Summary.JudgeBlockReason] を
// 出すこと。
func (s Summary) CanJudge(c Category) bool {
	return s.canJudge(c)
}

// JudgeBlockReason は、そのカテゴリを判定しなかった理由を短く返す。
//
// [Summary.CanJudge] が true のときの戻り値に意味は無い。
//
// 返すのは識別子と置換の組と、組み立て済みの日本語をまとめた [reason.Reason]。
// 文面だけが要るなら String() を通せばよい（%s も %v もそれを使う）。画面は
// 識別子を鍵にして目録から訳された文面を引き、鍵が無ければ文面へ落とす。
//
// 文面を返さずに識別子だけにすることはしない。ここが返す理由には、
// [OldOrderSource] を差し替えた呼び出し側が作った、名前の無いものが混ざる。
func (s Summary) JudgeBlockReason(c Category) reason.Reason {
	return judgeBlockReason(s, c)
}

// Categories は全カテゴリを表示順に返す。
//
// 表示順の表（[categories]）の写しを返すだけで、判定は足さない。csv の警告
// （cmd/dwloc の warnHeldCategories）が、判定を保留したカテゴリを text 形式の
// 本文と同じ順に名指しするために開ける。画面の件数の欄（internal/web の
// buildCounts）も、この順で並べる。[Summary.Counts] の鍵を Category の値で
// 並べても表示順にはならない（後から足したカテゴリの値は並びの最後にある）。
//
// 写しを返すのは、呼び出し側が並べ替えても表示順の表が動かないようにするため。
func Categories() []Category {
	return append([]Category(nil), categories...)
}

// OrderLineIDCategories は、再生順の台詞IDが読めていないと判定できないカテゴリを、
// 表示順に返す。
//
// 表（categoryTable の needsOrderLineIDs）の印を読むだけで、判定は足さない。
// 画面の断り書き（internal/web の buildNotes）が、再生順のキーは読めていて台詞IDだけが
// 無いときに、保留にしたカテゴリを名指しするために開ける。text 形式の見出し
// （lineIDCategoryNames）もここから引くので、印を足し引きしても2つの名指しはずれない。
// csv の警告（cmd/dwloc の warnHeldCategories）は台詞IDに限らず保留した全カテゴリを
// 書くので、ここではなく [Summary.CanJudge] から名指しを決める。
func OrderLineIDCategories() []Category {
	var out []Category
	for _, c := range categories {
		if c.needsOrderLineIDs() {
			out = append(out, c)
		}
	}
	return out
}

// ID は CSV に書くのと同じ ASCII 識別子を返す。
//
// [Category.ID] と対になる。画面の CSS の類名や、表示名の差し替え表の鍵に使う。
// 日本語名（[Status.String]）を鍵にすると、表示名を変えた瞬間に対応が切れる。
func (s Status) ID() string {
	return s.id()
}
