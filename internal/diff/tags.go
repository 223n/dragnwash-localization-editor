package diff

import (
	"regexp"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// tagToken は訳の中のタグ1つに当たる。
//
// 形は <名前>、</名前>、<名前=値>、<名前/> の4つ。名前は英字で始まる英数字と
// ハイフンで、値には < と > を入れられない。ゲームの文言（TextMeshPro の書式）で
// 実データに出るのは <i> <b> <size=80%> <gradient="yellow - orange"> の4種で、
// どれもこの形に収まる。
//
// 名前の後ろに空白を許すのは、<i > のような手打ちの揺れを別のタグと見ない
// ためである。値の中の空白（gradient の "yellow - orange"）は [^<>]* が受ける。
var tagToken = regexp.MustCompile(`<(/?)([A-Za-z][A-Za-z0-9-]*)(?:=[^<>]*)?\s*(/?)>`)

// TagBalance は訳の中のタグの開閉を突き合わせた結果。
type TagBalance struct {
	// Unclosed は開始があるのに終了が無いタグ。出てきた順で、書かれた綴りのまま。
	Unclosed []string
	// Unopened は終了だけがあるタグ。同上。
	Unopened []string
}

// Balanced は開閉がそろっているか。
func (b TagBalance) Balanced() bool {
	return len(b.Unclosed) == 0 && len(b.Unopened) == 0
}

// CheckTags は訳の中のタグの開閉を突き合わせる。
//
// 見るのは訳だけで、原文とは比べない。原文が閉じずに使っているタグ
// （実データでは <size=80%> のような大きさの指定。1行の終わりまで効かせる
// 書き方で、原文にも終了タグが無い）も、訳が同じ書き方なら当たる。原文と
// 比べる案もあったが、「訳の中で開いたまま閉じていないタグを見つける」という
// 定義のほうが選ばれた（2026-09-20）。原文と比べないので、作業コピーが無くても
// 判定できる。
//
// 突き合わせは名前で行う。終了タグは、同じ名前の開始タグのうちいちばん近い
// ものと組にする。<b><i></b></i> のような入れ違いは、名前がそろっていれば
// 開閉がそろっているものとして通す。TextMeshPro は入れ違いを直して描くので、
// 画面には崩れて出ない。名前の大文字小文字は区別しない（<I> と </i> は組になる）。
// <br/> のように自分で閉じるタグは数えない。
//
// 返す綴りは書かれたままにする。<size=80%> を <size> に縮めると、同じ行に
// <size=80%> と <size=60%> があるときにどちらが閉じていないのか読めない。
func CheckTags(text string) TagBalance {
	var b TagBalance
	// open は閉じていない開始タグの積み重ね。名前（小文字）と綴りを持つ。
	type opened struct{ name, token string }
	var open []opened
	for _, m := range tagToken.FindAllStringSubmatch(text, -1) {
		closing, name, self := m[1] == "/", strings.ToLower(m[2]), m[3] == "/"
		if self {
			continue
		}
		if !closing {
			open = append(open, opened{name: name, token: m[0]})
			continue
		}
		// いちばん近い同じ名前の開始タグを組にして外す。
		at := -1
		for i := len(open) - 1; i >= 0; i-- {
			if open[i].name == name {
				at = i
				break
			}
		}
		if at < 0 {
			b.Unopened = append(b.Unopened, m[0])
			continue
		}
		open = append(open[:at], open[at+1:]...)
	}
	for _, o := range open {
		b.Unclosed = append(b.Unclosed, o.token)
	}
	return b
}

// Note は結果を注記（[Finding.Note] と [Finding.NoteReason]）にする。
//
// 開閉がそろっているときは空の理由を返す。どちらか片方だけのときと両方の
// ときで識別子を分けるのは、目録の文面に「無いほうの見出し」を残さないため
// である。1つの文面に2つの置換を入れて片方を空にすると、「開始の無い終了タグ: 」
// のように見出しだけが画面に残る。
func (b TagBalance) Note() reason.Reason {
	unclosed := strings.Join(b.Unclosed, " ")
	unopened := strings.Join(b.Unopened, " ")
	switch {
	case len(b.Unclosed) > 0 && len(b.Unopened) > 0:
		return reason.New(reason.NoteTagUnclosedUnopened,
			"閉じていないタグ: "+unclosed+"／開始の無い終了タグ: "+unopened,
			"unclosed", unclosed, "unopened", unopened)
	case len(b.Unclosed) > 0:
		return reason.New(reason.NoteTagUnclosed, "閉じていないタグ: "+unclosed, "tags", unclosed)
	case len(b.Unopened) > 0:
		return reason.New(reason.NoteTagUnopened, "開始の無い終了タグ: "+unopened, "tags", unopened)
	default:
		return reason.Reason{}
	}
}

// tagFindings は「タグの開閉がそろわない行」を集める。
//
// 見る行は publish の入力になる側と同じで、作業コピーを読んでいればその行、
// 読んでいなければ公開ファイルの行である。両方を見ると同じキーの行が2回出る。
// 作業コピーでは publish が採らない行（key が壊れているなど）を飛ばす。その行は
// 「publish で捨てられる行」に出ていて、先にそちらを直さないと訳自体が公開に
// 載らない。
//
// 同じキーの行が2つあれば、先に出た行だけを報告する。画面（internal/web）は
// バッジをキー単位で付けるので、2回数えると件数とバッジの数が食い違う。
func tagFindings(idx *orderIndex, loc Locale) []Finding {
	var out []Finding
	seen := make(map[string]struct{})
	report := func(k string, f Finding, text string) {
		if k == "" || text == "" {
			return
		}
		if _, dup := seen[k]; dup {
			return
		}
		b := CheckTags(text)
		if b.Balanced() {
			return
		}
		seen[k] = struct{}{}
		note := b.Note()
		f.Note, f.NoteReason = note.Text, note
		out = append(out, f)
	}
	if loc.HasWorking {
		for _, row := range loc.Working {
			k, adopted := PublishKey(row)
			if !adopted {
				continue
			}
			report(k, workingFinding(idx, row), row.Translation)
		}
		return out
	}
	for _, row := range loc.Published {
		if row.Kind == KindBroken {
			// 形の分からない行は internal/validate が拾う。ここで訳を読んでも、
			// どの列が訳なのか決められない。
			continue
		}
		report(row.Key, publishedFinding(row), row.Translation)
	}
	return out
}

// TagDiff は原文と訳のタグの構成を突き合わせた結果。
//
// [TagBalance] と分けてあるのは、要る材料と直し方が違うからである。開閉は訳
// だけで判定できて訳の中で直せるが、こちらは原文が要るし、直すときは原文の
// 構造に合わせる。
type TagDiff struct {
	// Missing は原文にあって訳に無いタグ。原文での出現順で、書かれた綴りのまま。
	Missing []string
	// Extra は訳にあって原文に無いタグ。訳での出現順で、書かれた綴りのまま。
	Extra []string
}

// Same はタグの構成がそろっているか。
func (d TagDiff) Same() bool {
	return len(d.Missing) == 0 && len(d.Extra) == 0
}

// CompareTags は原文と訳のタグの構成を突き合わせる。
//
// 見るのはタグの多重集合で、並びは見ない。TextMeshPro は入れ違いを直して描く
// ので（[CheckTags] と同じ理由）、<b><i> と <i><b> を違いとして出すと、直す
// ところが無い報告になる。数は見る。原文に <i> が2組あって訳に1組しかない行は、
// 強調がひとつ落ちている。
//
// 突き合わせは正規化した綴りで行い、報告は書かれた綴りのまま返す。正規化は
// 小文字化と、閉じ括弧の直前の空白を落とすことだけである（[canonicalTag]）。
// 値まで含めて数えるので <size=70%> と <size=60%> は別のタグになる。値を
// 無視すると、大きさを書き換えた訳が「そろっている」と出てしまう。書き換えは
// Missing と Extra の両方に出る（原文の <size=70%> が足りず、訳の <size=60%>
// が余る）。
//
// 自分で閉じるタグ（<br/>）も数える。[CheckTags] は開閉の数え上げから外して
// いるが、こちらが見るのは「原文にあったものが訳にもあるか」なので、落ちて
// いれば直す対象である。
func CompareTags(source, translation string) TagDiff {
	src := countTags(source)
	dst := countTags(translation)
	return TagDiff{
		Missing: excessTags(src, dst),
		Extra:   excessTags(dst, src),
	}
}

// Note は結果を注記（[Finding.Note] と [Finding.NoteReason]）にする。
//
// 片方だけのときと両方のときで識別子を分けるのは [TagBalance.Note] と同じ
// 理由で、目録の文面に「無いほうの見出し」を残さないためである。
func (d TagDiff) Note() reason.Reason {
	missing := strings.Join(d.Missing, " ")
	extra := strings.Join(d.Extra, " ")
	switch {
	case len(d.Missing) > 0 && len(d.Extra) > 0:
		return reason.New(reason.NoteTagMissingExtra,
			"原文にあって訳に無いタグ: "+missing+"／訳にあって原文に無いタグ: "+extra,
			"missing", missing, "extra", extra)
	case len(d.Missing) > 0:
		return reason.New(reason.NoteTagMissing, "原文にあって訳に無いタグ: "+missing, "tags", missing)
	case len(d.Extra) > 0:
		return reason.New(reason.NoteTagExtra, "訳にあって原文に無いタグ: "+extra, "tags", extra)
	default:
		return reason.Reason{}
	}
}

// tagCount は1つの文字列に出てきたタグを、正規化した綴りごとに数えたもの。
type tagCount struct {
	// order は正規化した綴りを、初めて出てきた順に並べたもの。
	// 報告の並びを原文（または訳）に出てきた順にするために持つ。
	order []string
	// count は正規化した綴りごとの個数。
	count map[string]int
	// spelled は正規化した綴りごとの、書かれたままの綴り。初出のものを採る。
	// <I> と <i> のように綴りだけが違うものは、先に出たほうで報告する。
	spelled map[string]string
}

// countTags は文字列のタグを数える。
func countTags(text string) tagCount {
	c := tagCount{count: make(map[string]int), spelled: make(map[string]string)}
	for _, m := range tagToken.FindAllString(text, -1) {
		canon := canonicalTag(m)
		if _, seen := c.count[canon]; !seen {
			c.order = append(c.order, canon)
			c.spelled[canon] = m
		}
		c.count[canon]++
	}
	return c
}

// excessTags は a にあって b に足りないぶんの綴りを、a の出現順で並べる。
//
// 足りない数だけ綴りを繰り返す。原文に <i> が2組ある行で1組だけ落ちている
// ことを、1件として出したいためである。
func excessTags(a, b tagCount) []string {
	var out []string
	for _, canon := range a.order {
		for i := a.count[canon] - b.count[canon]; i > 0; i-- {
			out = append(out, a.spelled[canon])
		}
	}
	return out
}

// canonicalTag は綴りの揺れを落として、同じタグかどうかを数えられる形にする。
//
// 落とすのは2つだけ。大文字小文字（[CheckTags] が名前を区別しないのと同じ
// 扱いにする。<gradient="Gold"> と <gradient="gold"> も同じ色を指す）と、
// 閉じ括弧の直前の空白（<i > は手打ちの揺れで、別のタグではない）。
//
// 引数は [tagToken] が拾った綴りであることを前提にする。末尾は必ず ">" で、
// 中に ">" は入らない。
func canonicalTag(token string) string {
	s := strings.ToLower(token)
	return strings.TrimRight(strings.TrimSuffix(s, ">"), " \t") + ">"
}

// tagMismatchFindings は「原文とタグの構成が違う行」を集める。
//
// 見るのは作業コピーだけである。公開ファイルには原文の列が無く、突き合わせる
// 相手がいない。読んでいないときは [Summary.canJudge] が false になり、
// 0 件ではなく理由が出る。CI には作業コピーが無いので、この判定が CI の
// 終了コードを動かすことはない。
//
// 対象は publish が採る行のうち、原文と訳の両方が入っているものだけ。
//
//   - 原文が空の行は、ゲームがその文字をまだ読み込んでいない（移植仕様
//     「作業コピー生成 R7/R8」）。相手がいないので判定できない。
//   - 訳が空の行は「未翻訳」に出ている。訳が無ければ原文のタグは全部
//     「訳に無い」ことになるので、ここでも出すと未翻訳の行がもう一度、
//     しかもタグの話として並ぶ。
//
// 同じキーの行が2つあれば、先に報告した行だけを出す（[tagFindings] と同じ
// 理由で、画面はバッジをキー単位で付ける）。
func tagMismatchFindings(idx *orderIndex, loc Locale) []Finding {
	if !loc.HasWorking {
		return nil
	}
	var out []Finding
	seen := make(map[string]struct{})
	for _, row := range loc.Working {
		k, adopted := PublishKey(row)
		if !adopted || k == "" {
			continue
		}
		if row.SourceEn == "" || row.Translation == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		d := CompareTags(row.SourceEn, row.Translation)
		if d.Same() {
			continue
		}
		seen[k] = struct{}{}
		f := workingFinding(idx, row)
		note := d.Note()
		f.Note, f.NoteReason = note.Text, note
		out = append(out, f)
	}
	return out
}
