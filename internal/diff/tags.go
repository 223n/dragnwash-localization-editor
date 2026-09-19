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
