package diff

import "github.com/223n/dragnwash-localization-editor/internal/reason"

// このファイルは判定を1つも足さない。すでにある [Summary.canJudge] と
// judgeBlockReason と [Status.id] を、パッケージの外から読めるようにするだけである。
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
// 既存の関数は1行も変えていない。ここにあるのは委譲だけである。

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

// ID は CSV に書くのと同じ ASCII 識別子を返す。
//
// [Category.ID] と対になる。画面の CSS の類名や、表示名の差し替え表の鍵に使う。
// 日本語名（[Status.String]）を鍵にすると、表示名を変えた瞬間に対応が切れる。
func (s Status) ID() string {
	return s.id()
}
