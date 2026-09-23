package web

import (
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

// この束は、画面側の「訳を失わない」仕掛けが消えていないことを見る。
//
// どれも字面の試験である。Go から DOM を動かせないので、振る舞いそのものは
// 保証できない。それでも書いてあるのは、ここに挙げたものが、実際に動かして
// 見つかった「訳が消える／訳が別の行に入る／打鍵がどこにも入らない」経路の
// 直しそのものだからである。
// 書き方を変えるときは、変えたあとの形でブラウザーから確かめること。
//
// 振る舞いのほうは、保証できるぶんを待ち受け側へ寄せてある。行がずれたときに
// 書かないことは [TestSaveRejectsMovedRow] が実際のファイルで見ている。

// uiSource は埋め込んだ画面の資産を読む。
func uiSource(t *testing.T, name string) string {
	t.Helper()
	body, err := fs.ReadFile(uiFS, name)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// TestConflictKeepsEditsTypedWhileChoosing は、競合を選んでいるあいだに打った訳を
// 捨てていないことを見る。
//
// 実際に起きた: keepMine が state.pending を state.mine で丸ごと置き換えていた。
// 競合パネルが出ているあいだに打った訳は、「自分の訳を上に載せる」を押した瞬間に
// 黙って消え、状態表示は「保存済み」になった。設計で最優先に置いている
// 「黙って破棄もしない」が、まさに競合の場面で破れていた。
func TestConflictKeepsEditsTypedWhileChoosing(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	if strings.Contains(js, "state.pending = new Map(state.mine)") {
		t.Error("keepMine が pending を丸ごと置き換えている。選んでいるあいだに打った訳が消える")
	}
	// 片方にだけ入れる形（打ったばかりのほうを残す）になっていること。
	if !strings.Contains(js, "if (!state.pending.has(line))") {
		t.Error("keepMine が pending を残していない。打ったばかりの訳のほうが新しい")
	}
}

// TestTakeFileResumesAutosave は、「ファイルの訳を採る」のあとに自動保存が
// 動き直すことを見る。
//
// 実際に起きた: takeFile が flush も schedule も呼ばず、競合のあいだ止めていた
// 自動保存がそのまま止まったままになった。選んでいるあいだに打った訳は、
// 別の行を触るまで送られなかった。
func TestTakeFileResumesAutosave(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, "function takeFile()")
	if start < 0 {
		t.Fatal("takeFile が無い")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("takeFile の終わりが分からない")
	}
	if !strings.Contains(js[start:start+end], "flush()") {
		t.Error("takeFile が flush を呼んでいない。競合のあと自動保存が止まったままになる")
	}
}

// TestRowsAreIdentifiedByKey は、行の同定にキーを使っていることを見る。
//
// 実際に起きた: 409 のあとの載せ直しが物理行番号だけだったため、よそが行を
// 挿入していると、打った訳が別のキーの行へ入り、その行にもとからあった訳が
// 消えた。キーが変わったことは画面のどこにも出なかった。
func TestRowsAreIdentifiedByKey(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	for _, want := range []string{
		// 保存の要求にキーを載せる（待ち受けが食い違いを見つけて断れる）。
		"key: entry && entry.key ? entry.key : \"\"",
		// 読み直した内容への載せ直しをキーで行う。
		"function remap(",
		// 載せる先が決められなかった訳を捨てずに出す。
		"function addOrphans(",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js に %q が無い。行の同定は行番号だけに頼らない", want)
		}
	}
}

// TestSaveRetryDoesNotGiveUp は、保存の送り直しを途中で諦めていないことを見る。
//
// 実際に起きた: 再試行の回数が 200 応答でしか戻らず、一度使い切ると、原因
// （ゲームがファイルを開いている）が消えたあともその訳は二度と送られなかった。
// 翻訳者が別の行を触るまで、訳はブラウザーの中だけに残り続けた。
func TestSaveRetryDoesNotGiveUp(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	if strings.Contains(js, "state.retry >= retryDelays.length") {
		t.Error("送り直しを回数で打ち切っている。原因が消えてもその訳は送られない")
	}
	if !strings.Contains(js, "i = retryDelays.length - 1") {
		t.Error("最後の間隔で送り続ける形になっていない")
	}
}

// topRegion は index.html の `<div class="top">` から、それに対応する閉じ div
// までを返す。
//
// 入れ子の div を数える。数えずに最初の `</div>` で切ると、.top の中に div を
// 1つ置いた瞬間に範囲が途中で終わり、message / conflict / orphans が
// 「一帯の外にある」という、事実と違う理由で落ちる。前の段は index.html に
// 「ここに div を置くな」というコメントを足して避けていたが、それは試験の
// 都合を骨組みに持ち込んでいる。
func topRegion(t *testing.T, html string) string {
	t.Helper()
	const open = `<div class="top">`
	start := strings.Index(html, open)
	if start < 0 {
		t.Fatal("index.html に貼り付ける一帯（.top）が無い")
	}
	depth := 0
	for i := start; i < len(html); i++ {
		rest := html[i:]
		switch {
		case strings.HasPrefix(rest, "<div"):
			depth++
		case strings.HasPrefix(rest, "</div>"):
			depth--
			if depth == 0 {
				return html[start : i+len("</div>")]
			}
		}
	}
	t.Fatal(".top の閉じ div が見つからない")
	return ""
}

// TestAlertsStayOnScreen は、競合の引き止めと失敗の理由が画面に残ることを見る。
//
// 実際に起きた: 1681行の途中（scrollY 55546）で 409 が起きると、決断用の
// ボタンは画面の約55,000px 上にあり、そのあいだ自動保存は止まったままだった。
//
// 貼り付いているだけでは足りないことも見る。.top は max-height: 50vh で
// 止めてあるので、中身が増えると引き止めのボタンは帯の中でスクロールアウトし、
// 貼り付いたまま押せなくなる（800x600 で実際に起きた）。だから、絞り込みの
// 一帯をここへ戻していないことも合わせて見る。
func TestAlertsStayOnScreen(t *testing.T) {
	html := uiSource(t, "ui/index.html")
	css := uiSource(t, "ui/app.css")

	inside := topRegion(t, html)
	for _, want := range []string{`id="message"`, `id="conflict"`, `id="orphans"`} {
		if !strings.Contains(inside, want) {
			t.Errorf("%s が貼り付ける一帯の外にある。行の途中では見えなくなる", want)
		}
	}
	if strings.Contains(inside, `class="finder"`) {
		t.Error("絞り込みの一帯が貼り付ける一帯の中にある。引き止めのボタンが帯の中で押し出される")
	}

	block := strings.Index(css, ".top {")
	if block < 0 {
		t.Fatal("app.css に .top が無い")
	}
	if !strings.Contains(css[block:block+200], "position: sticky") {
		t.Error(".top が貼り付いていない")
	}
}

// TestScrollMarginIsOnTheFocusedElement は、画面へ入れるときの余白が、焦点の
// 入る要素に付いていることを見る。
//
// 実際に起きた: 余白を .row に付けていたが、1度も使われなかった。ブラウザーが
// 画面へ入れようとするのは焦点の入った要素（.translation と、差し込んだ
// .editor）であって、その親の .row ではない。getComputedStyle(editor)
// .scrollMarginTop は "0px" のままで、Shift+Tab で上の行へ戻ると、開いた
// 入力欄が貼り付く帯の下へ完全に潜った。
func TestScrollMarginIsOnTheFocusedElement(t *testing.T) {
	css := uiSource(t, "ui/app.css")

	// 余白は帯の実測の高さから作ること。固定値では、帯が伸びた場面で足りない。
	//
	// 実際に起きた: 8em（112px）の固定値にしていたが、.top は競合と行き先の無い
	// 訳が出ると max-height: 50vh まで伸びる。800x600 では、行き先の無い訳が
	// 1件出ているだけで .top が 150.6px、両方出ると 300px になり、Shift+Tab で
	// 開いた入力欄も、スラッシュで移った検索の欄も帯の裏に入った
	// （elementFromPoint はどちらも帯の中身を返した）。
	for _, sel := range []string{
		".row .translation[tabindex] {",
		".editor {",
		".finder {",
	} {
		at := strings.Index(css, sel)
		if at < 0 {
			t.Fatalf("app.css に %s が無い", sel)
		}
		end := strings.Index(css[at:], "\n}")
		if end < 0 {
			t.Fatalf("%s の終わりが分からない", sel)
		}
		block := css[at : at+end]
		if !strings.Contains(block, "scroll-margin-top") {
			t.Errorf("%s に scroll-margin-top が無い。焦点の入る要素に付けないと効かない", sel)
		}
		if !strings.Contains(block, "var(--top-height,") {
			t.Errorf("%s の余白が帯の実測の高さから作られていない。帯が伸びると足りなくなる", sel)
		}
	}

	// 測る側。ResizeObserver が無いときは既定値のままになること。
	js := uiSource(t, "ui/app.js")
	start := strings.Index(js, "function watchTopHeight()")
	if start < 0 {
		t.Fatal("app.js に watchTopHeight が無い。帯の高さを測っていない")
	}
	tail := strings.Index(js[start:], "\n  }")
	if tail < 0 {
		t.Fatal("watchTopHeight の終わりが分からない")
	}
	body := js[start : start+tail]
	for _, want := range []string{
		`setProperty("--top-height"`,
		`typeof ResizeObserver !== "function"`,
		// 見張りの参照を残すこと。捨てると1度も呼ばれないまま回収されることが
		// ある（実測: 窓を 0x0 から 800x600 へ変えても --top-height は 0px の
		// ままで、帯が 48px から 49.4px に伸びたときも動かなかった）。
		"state.topWatch = new ResizeObserver(apply)",
		"state.topWatch.observe(el.top)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("watchTopHeight に %q が無い", want)
		}
	}

	// .row 側には残さない。残すと、効かない指定が2か所にあることになる。
	row := strings.Index(css, "\n.row {")
	if row < 0 {
		t.Fatal("app.css に .row が無い")
	}
	end := strings.Index(css[row:], "\n}")
	if end < 0 {
		t.Fatal(".row の終わりが分からない")
	}
	if strings.Contains(css[row:row+end], "scroll-margin") {
		t.Error(".row に scroll-margin が残っている。焦点はここに入らないので効かない")
	}
}

// TestTopHeightDefaultIsTheSameEverywhere は、--top-height の既定値（CSS の var() の
// 第2引数）が app.css のどこでも同じで、それを説明する3か所（app.css の注記、app.js の
// watchTopHeight の注記、doc.go）も同じ値を言っていることを見る。
//
// 実際に起きた: app.css は 10em なのに、doc.go は「第2引数（8em）」と書き、帯の 49.4px に
// 合わせたと説明していた。既定値は ResizeObserver が無い環境で実際に効く値なので、
// 説明が違うと、直す人が間違った前提で値を動かす。
func TestTopHeightDefaultIsTheSameEverywhere(t *testing.T) {
	css := uiSource(t, "ui/app.css")
	values := make(map[string]bool)
	for _, m := range regexp.MustCompile(`var\(--top-height, ([0-9.]+[a-z]+)\)`).FindAllStringSubmatch(css, -1) {
		values[m[1]] = true
	}
	if len(values) != 1 {
		t.Fatalf("app.css の --top-height の既定値が1つにそろっていない: %v", values)
	}
	var def string
	for v := range values {
		def = v
	}

	doc, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatal(err)
	}
	said := regexp.MustCompile(`第2引数（(?:既定値の )?([0-9.]+[a-z]+)`)
	for name, text := range map[string]string{
		"app.css の注記": css,
		"app.js":      uiSource(t, "ui/app.js"),
		"doc.go":      string(doc),
	} {
		found := said.FindAllStringSubmatch(text, -1)
		if len(found) == 0 {
			t.Errorf("%s が --top-height の既定値を書いていない", name)
		}
		for _, m := range found {
			if m[1] != def {
				t.Errorf("%s が既定値を %s と書いている。app.css は %s", name, m[1], def)
			}
		}
	}
}

// TestDiscardAsksAfterSending は、読み直しと切り替えが、送れるものを送り終えてから
// 尋ね、受けたあとは捨てると答えた訳を送らないことを見る。
//
// 実際に起きた: 押した時点で未保存があるかを見て尋ねていたので、打った直後に押すと、
// 欄から離れたときの保存（blur）が返る前か後かで尋ねたり尋ねなかったりした。尋ねた文は
// 「その訳は消えます」なのに、その訳は送られてファイルに入った。受けたあとも、読み込みが
// 返るまでに送り直しの時計が切れると、捨てると答えた訳が送られた。振る舞いは E2E の
// boot.spec.mjs が見ている。
func TestDiscardAsksAfterSending(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, "function askDiscard(")
	if start < 0 {
		t.Fatal("askDiscard が無い")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("askDiscard の終わりが分からない")
	}
	body := js[start : start+end]
	settle := strings.Index(body, "settle().then(")
	confirm := strings.Index(body, "window.confirm(")
	if settle < 0 || confirm < 0 || confirm < settle {
		t.Error("askDiscard が送り終えるのを待たずに尋ねている")
	}
	if !strings.Contains(body, "state.discarding = {}") {
		t.Error("受けたあとに、捨てると答えた訳を送らない印を立てていない")
	}

	// 送り終わりそのものを待てること（送っている最中の flush は早く戻る）。
	if !strings.Contains(js, "state.sending = postJSON(") {
		t.Error("flush が送り終わりを控えていない。送っている最中の保存を待てない")
	}
	// 捨てると答えたあとは送らない。印を見るのは、送る要求を組むより前であること。
	fl := strings.Index(js, "function flush()")
	if fl < 0 {
		t.Fatal("flush が無い")
	}
	hold := strings.Index(js[fl:], "if (state.discarding) {")
	post := strings.Index(js[fl:], "state.sending = postJSON(")
	if hold < 0 || post < 0 || hold > post {
		t.Error("flush が、捨てると答えた訳を送る前に止めていない")
	}
	// 読めなかったら送り直しへ戻すこと（諦めない）。
	ld := strings.Index(js, "function load(locale, resetFinder)")
	if ld < 0 {
		t.Fatal("load が無い")
	}
	fail := strings.Index(js[ld:], ".catch(function ()")
	if fail < 0 || !strings.Contains(js[ld+fail:ld+fail+800], "stopHolding(holding)") {
		t.Error("読み込みに失敗したときに、止めていた送り直しを戻していない")
	}
}

// TestExportStopsWhileTranslationsAreOutsideTheFile は、ファイルに入っていない訳
// （競合・保存できない行・行き先の無い訳）が残っているあいだ、書き出しが中身を
// 取りにいかないことを見る。
//
// 実際に起きた: 書き出しは未保存（state.pending）と送信中だけを見ていたので、保存
// できない行や行き先の無い訳が残っていても、その訳の入らない中身を「書き出しました」と
// 渡した。hasUnsaved が未保存に数えるものは、書き出しでも止める。
func TestExportStopsWhileTranslationsAreOutsideTheFile(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, "function exportCsv(")
	if start < 0 {
		t.Fatal("exportCsv が無い")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("exportCsv の終わりが分からない")
	}
	body := js[start : start+end]
	fetch := strings.Index(body, "return fetchCsv(form)")
	if fetch < 0 {
		t.Fatal("exportCsv が中身を取りにいっていない")
	}
	for _, want := range []string{"if (state.mine) {", "if (state.failed.size) {", "if (state.orphans.length) {"} {
		at := strings.Index(body, want)
		if at < 0 || at > fetch {
			t.Errorf("exportCsv が取りにいく前に %q を見ていない", want)
		}
	}
	for _, key := range []string{"ui.export_conflict", "ui.export_row_failed", "ui.export_orphans"} {
		if !strings.Contains(body, `t("`+key+`")`) {
			t.Errorf("exportCsv が止めた理由（%s）を出していない", key)
		}
	}
}

// TestEmptyTranslationIsClickable は、訳が空の行にも的があることを見る。
//
// 実際に起きた: 幅900px以下（flex に変わる側）で、訳が空の欄の高さが枠線ぶんの
// 2px しか無かった。いちばん打ちたい未訳の行が、マウスでは掴めなかった。
func TestEmptyTranslationIsClickable(t *testing.T) {
	css := uiSource(t, "ui/app.css")

	block := strings.Index(css, ".row .translation[tabindex] {")
	if block < 0 {
		t.Fatal("app.css に訳の欄の指定が無い")
	}
	if !strings.Contains(css[block:block+200], "min-height") {
		t.Error("訳の欄に高さが敷かれていない。空の行がマウスで掴めない")
	}
}

// TestFilterKeepsUnsavedRowsVisible は、絞り込みと検索が「未保存の訳がある行」と
// 「保存できなかった行」を隠さないことを見る。
//
// 隠すと、直すべき行が画面から消える。翻訳者は、消えたことにも気づけない。
// 自動保存があるので未保存が残るのは保存できなかったときと競合中だけだが、
// その2つはまさに人が見て直さなければならない状態である。
func TestFilterKeepsUnsavedRowsVisible(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	if !strings.Contains(js, "function keepAlways(") {
		t.Fatal("app.js に keepAlways が無い。条件に当たらなくても出す行が要る")
	}
	// 3つとも見ていること。競合で抱えているぶん（state.mine）も未保存である。
	start := strings.Index(js, "function keepAlways(")
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("keepAlways の終わりが分からない")
	}
	body := js[start : start+end]
	for _, want := range []string{"state.pending.has(n)", "state.failed.has(n)", "state.mine"} {
		if !strings.Contains(body, want) {
			t.Errorf("keepAlways が %s を見ていない", want)
		}
	}
	// 出すか決めるところで、条件より先に keepAlways を見ていること。決め方は
	// shouldShow にまとめてあり、applyView も reviewClosed もそこを通る。
	show := strings.Index(js, "function shouldShow(entry, q)")
	if show < 0 {
		t.Fatal("shouldShow が無い")
	}
	tail := strings.Index(js[show:], "\n  }")
	if tail < 0 {
		t.Fatal("shouldShow の終わりが分からない")
	}
	if !strings.Contains(js[show:show+tail], "if (keepAlways(entry.line)) {") {
		t.Error("出す行の決め方が keepAlways を通っていない")
	}
	if !strings.Contains(js, "var show = shouldShow(entry, q);") {
		t.Error("applyView が shouldShow を通っていない")
	}
}

// TestOpenRowIsNeverHidden は、いま入力欄が開いている行を絞り込みが隠さない
// ことを見る。
//
// 前の段は「隠れるなら先に保存してから閉じる」形だった。それが打鍵を捨てた。
// 検索欄に打ってから 120ms 以内に行の訳欄をクリックすると、入力欄が開いた直後に
// applyView が走り、まだ1字も打っていない（＝ keepAlways に当たらない）その行を
// commitEditor で閉じて隠した。焦点は body へ落ち、以後打った字はどこにも入らず、
// 保存の欄は「保存済み」のまま、警告は1つも出なかった（実際に起きた）。
//
// 直した形では、触っている行を隠さない。隠す道が無くなるので、applyView の中で
// 入力欄を閉じる処理そのものが要らない。閉じる処理を戻すときは、この経路を
// もう一度ブラウザーで踏んでから戻すこと。
func TestOpenRowIsNeverHidden(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, "function keepAlways(")
	if start < 0 {
		t.Fatal("app.js に keepAlways が無い")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("keepAlways の終わりが分からない")
	}
	if !strings.Contains(js[start:start+end], "state.editing === n") {
		t.Error("keepAlways が「いま入力欄が開いている行」を見ていない。開いた直後に隠れて打鍵が落ちる")
	}

	at := strings.Index(js, "function applyView()")
	if at < 0 {
		t.Fatal("applyView が無い")
	}
	tail := strings.Index(js[at:], "\n  }")
	if tail < 0 {
		t.Fatal("applyView の終わりが分からない")
	}
	if strings.Contains(js[at:at+tail], "commitEditor()") {
		t.Error("applyView がまた入力欄を閉じている。開いた直後の入力欄を閉じて打鍵を捨てる")
	}

	// commitEditor 自体は残す（欄から離れたときなどに通る）。閉じる前に保存へ回すこと。
	commit := strings.Index(js, "function commitEditor()")
	if commit < 0 {
		t.Fatal("commitEditor が無い")
	}
	if !strings.Contains(js[commit:commit+160], "flush()") {
		t.Error("commitEditor が flush を呼んでいない")
	}
}

// TestClosedRowIsReviewedButOnlyWhenItLeft は、入力欄を閉じたあとの当て直しが、
// 「閉じた行がもう出す行でないとき」だけ走ることを見る。
//
// 入力欄が開いているあいだ、その行は keepAlways が条件を無視して出している。
// 閉じたあともそのままにすると、条件に当たらない行が残り、「表示中 N 行」
// もその行を数えたままになる。だから閉じたときに照らし直す。
//
// ただし、閉じるたびに当て直すのは行きすぎだった。上から順に Enter で打って
// いくとき、閉じる時点のその行はまだ未保存なので当て直しは走らない。走らせると、
// 数行前に打ち終わって保存の済んだ行が条件から外れて次々に消え、一覧が1行ぶん
// ずつせり上がる。「保存のたびには当て直さない」（訳し終えた行が目の前で
// 消えないようにする）と決めたことを、1行遅れでやり直すだけになる。
//
// 呼ぶ順番も実測で決めた。Enter の行送りで当て直しを先に回すと、そのとき
// state.editing は空なので、これから開く行が条件に当たらなければその場で隠れ、
// そのあと入力欄が隠れた行へ差し込まれる。実測では、「未保存」で絞った状態で
// 保存済みの行から Enter を押すと、入力欄は行番号 41 の行に入ったのに、その行は
// hidden、入力欄の高さは 0、一覧は「条件に合う行がありません」になった。
// 焦点は入力欄にあるので、打った字はどこにも見えないまま入る。
func TestClosedRowIsReviewedButOnlyWhenItLeft(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, "function reviewClosed(")
	if start < 0 {
		t.Fatal("app.js に reviewClosed が無い。閉じた行が条件に当たらないまま残る")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("reviewClosed の終わりが分からない")
	}
	body := js[start : start+end]
	// まだ出す行なら何も動かさないこと。
	if !strings.Contains(body, "if (shouldShow(entry, el.search.value.toLowerCase())) {") {
		t.Error("reviewClosed が、まだ出す行かどうかを見ていない。打ち終わった行が次々に消える")
	}
	if !strings.Contains(body, "applyView();") {
		t.Error("reviewClosed が当て直していない")
	}

	// 出すか出さないかの決め方は1か所（shouldShow）にまとめること。
	if !strings.Contains(js, "function shouldShow(entry, q)") {
		t.Fatal("shouldShow が無い。applyView と reviewClosed で決め方がずれる")
	}
	if !strings.Contains(js, "var show = shouldShow(entry, q);") {
		t.Error("applyView が shouldShow を通っていない")
	}

	// Enter の行送りは、次の行を開いてから照らし直すこと。
	at := strings.Index(js, "var from = state.editing;")
	if at < 0 {
		t.Fatal("Enter の行送りが from を控えていない")
	}
	open := strings.Index(js[at:], "openEditor(next);")
	review := strings.Index(js[at:], "reviewClosed(from);")
	if open < 0 || review < 0 {
		t.Fatal("Enter の行送りに openEditor / reviewClosed が無い")
	}
	if review < open {
		t.Error("次の行を開く前に照らし直している。隠れた行へ入力欄が差し込まれる")
	}
}

// TestFailedRowKeepsItsValue は、保存できなかった行が値まで控えられていることを
// 見る。
//
// 控えないと、その訳は欄の字としてしか残らない。入力欄を開き直す・競合を解く・
// 読み直すのどれでも行は描き直され、そのとき出るのは古い保存値になる。1字打った
// 瞬間に、保存できなかった訳が黙って上書きされる。
func TestFailedRowKeepsItsValue(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, "function shownValue(")
	if start < 0 {
		t.Fatal("shownValue が無い")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("shownValue の終わりが分からない")
	}
	if !strings.Contains(js[start:start+end], "state.failed.get(n)") {
		t.Error("shownValue が保存できなかった値を見ていない。開き直すと古い保存値が出る")
	}

	// 控えるほうも、理由だけでなく値を持つこと。
	at := strings.Index(js, "function markFailed(")
	if at < 0 {
		t.Fatal("markFailed が無い")
	}
	tail := strings.Index(js[at:], "\n  }")
	if tail < 0 {
		t.Fatal("markFailed の終わりが分からない")
	}
	if !strings.Contains(js[at:at+tail], "value:") {
		t.Error("markFailed が値を控えていない")
	}
	// 理由だけを入れる古い形が残っていないこと。
	for _, bad := range []string{
		"state.failed.set(r.line, r.error)",
		`state.failed.set(r.line, r.error ? r.error : "")`,
	} {
		if strings.Contains(js, bad) {
			t.Errorf("app.js に %q がある。理由だけを控えると値が消える", bad)
		}
	}
}

// TestConflictRowIsNotEditableUntilChosen は、競合している行が、どちらを残すか
// 選ぶまで開かないことを見る。
//
// この試験は主張がひっくり返っている。前は「競合中の行を開いたら入力欄に入るのは
// 自分の訳であること」を見ていた。そちらは、競合中の行も編集できるという前提の
// うえで「起点をどちらにするか」を決めていたが、前提そのものが穴だった。
//
// 実際に起きた（どちらも実測で再現した）。
//
//   - 起点をファイルの訳にすると、1字打った瞬間に自分の版が pending を上書きし、
//     自分の訳は画面のどこにも出なくなった。
//   - 起点を自分の訳にすると、いったん離れて開き直したときに、そのあいだ打ち
//     直した訳（pending）が捨てられた。さらにその状態で「ファイルの訳を採る」を
//     押すと、takeFile は pending を残すので、ファイルの訳ではなく自分の競合版が
//     ファイルへ書かれた。押したボタンと逆の結果になった。
//
// 根っこは「どちらを残すか決まっていない行を編集できること」である。決まって
// いない行への入力は keepMine の意味にも takeFile の意味にも取れる。だから
// 曖昧さを別の側へ動かすのではなく、曖昧な状態そのものを作らないことにした。
// 競合していない行は、引き止めが出ているあいだも編集できる。
func TestConflictRowIsNotEditableUntilChosen(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, "function openEditor(")
	if start < 0 {
		t.Fatal("openEditor が無い")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("openEditor の終わりが分からない")
	}
	body := js[start : start+end]
	if !strings.Contains(body, "if (isLocked(n)) {") {
		t.Error("openEditor が競合中の行を開いてしまう。押したボタンと逆の結果になる道が残る")
	}
	// 起点の選び分けは消えていること。残っていると、開ける道がどこかにある。
	if strings.Contains(body, "editor.value = state.mine.get(n)") {
		t.Error("openEditor に競合中の起点が残っている。競合中の行は開かないことにした")
	}

	// isLocked は state.mine を引くだけであること。新しい判断を持たせない。
	lock := strings.Index(js, "function isLocked(")
	if lock < 0 {
		t.Fatal("isLocked が無い")
	}
	tail := strings.Index(js[lock:], "\n  }")
	if tail < 0 {
		t.Fatal("isLocked の終わりが分からない")
	}
	if !strings.Contains(js[lock:lock+tail], "state.mine && state.mine.has(n)") {
		t.Error("isLocked が state.mine を見ていない")
	}

	// 行き先にもしないこと。行き先にすると、Enter で移った先が開かず焦点が落ちる。
	next := strings.Index(js, "function nextEditable(")
	if next < 0 {
		t.Fatal("nextEditable が無い")
	}
	nend := strings.Index(js[next:], "\n  }")
	if nend < 0 {
		t.Fatal("nextEditable の終わりが分からない")
	}
	if !strings.Contains(js[next:next+nend], "isLocked(line)") {
		t.Error("nextEditable が競合中の行を飛ばしていない。Enter で移ると焦点が body へ落ちる")
	}

	// 押しても開かない理由を、行に出していること。
	if !strings.Contains(js, `t("ui.conflict_locked")`) {
		t.Error("競合中の行に断りが出ていない。押しても開かない行が黙って1つある形になる")
	}
	for _, name := range []string{"ja", "en"} {
		cat := uiSource(t, "ui/i18n/"+name+".json")
		if !strings.Contains(cat, `"ui.conflict_locked"`) {
			t.Errorf("%s.json に ui.conflict_locked が無い", name)
		}
	}

	// 1字打っても競合の1言を消さないこと。openEditor が開かないので本来は
	// 通らないが、二重の鍵として残してある。
	at := strings.Index(js, "function onInput()")
	if at < 0 {
		t.Fatal("onInput が無い")
	}
	otail := strings.Index(js[at:], "\n  }")
	if otail < 0 {
		t.Fatal("onInput の終わりが分からない")
	}
	if !strings.Contains(js[at:at+otail], "if (!(state.mine && state.mine.has(n)))") {
		t.Error("onInput が競合中の行の1言まで消している。自分の訳が画面から消える")
	}
}

// TestComposedEnterDoesNotAdvance は、変換を確定した Enter で行が飛ばないことを
// 見る。
//
// 実際に起きた: compositionend が確定の keydown より先に届く並びだと、
// state.composing も e.isComposing も false、keyCode は 13 になり、3つの見張りを
// 全部すり抜けて次の行が開いた。翻訳者から見ると、変換を確定しただけで行が飛ぶ。
// ja / ko / zh-Hans / zh-Hant のためにこの入力方式を選んでいるので、ここが
// 崩れると選定の根拠が失われる。
//
// 時刻は performance.now で測ること。Date.now（壁時計）で測ると、確定と Enter の
// あいだに OS の時計が後ろへ動いたとき（NTP の段差、休止からの復帰）、差が負のまま
// 猶予を超えるまで Enter の行送りが効かなくなる。時計を1時間戻せば1時間効かない。
func TestComposedEnterDoesNotAdvance(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	if !strings.Contains(js, "state.composedAt = performance.now()") {
		t.Error("compositionend の時刻を performance.now で控えていない")
	}
	if strings.Contains(js, "state.composedAt = Date.now()") ||
		strings.Contains(js, "Date.now() - state.composedAt") {
		t.Error("確定の時刻を Date.now で測っている。壁時計が戻ると Enter が長く効かなくなる")
	}
	start := strings.Index(js, `editor.addEventListener("keydown"`)
	if start < 0 {
		t.Fatal("入力欄の keydown が無い")
	}
	block := js[start:]
	guard := strings.Index(block, "performance.now() - state.composedAt < composedGrace")
	if guard < 0 {
		t.Fatal("確定直後の Enter を見分けていない")
	}
	next := strings.Index(block, "nextEditable(from)")
	if next < 0 || guard > next {
		t.Error("確定直後の見分けが行送りより後ろにある。変換の確定で行が飛ぶ")
	}
	// 改行が入らないよう preventDefault は先に済ませること。
	prevent := strings.Index(block, "e.preventDefault()")
	if prevent < 0 || prevent > guard {
		t.Error("確定直後の Enter で preventDefault を通していない。訳に改行が入る")
	}
}

// TestLastRowEnterKeepsFocus は、出ている最後の行で Enter を押しても焦点を
// 失わないことを見る。
//
// 実際に起きた: 入力欄が閉じて焦点が body へ落ち、終わりまで来たという合図も
// 出なかった。キーボードだけで打っている人は、そこから先どこにいるのか
// 分からなくなる。
func TestLastRowEnterKeepsFocus(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, `editor.addEventListener("keydown"`)
	if start < 0 {
		t.Fatal("入力欄の keydown が無い")
	}
	block := js[start:]
	at := strings.Index(block, "if (next === null) {")
	if at < 0 {
		t.Fatal("次の行が無いときの分かれ道が無い")
	}
	// この分かれ道の中だけを見る。閉じ括弧はこの深さ（6桁の字下げ）にある。
	end := strings.Index(block[at:], "\n      }")
	if end < 0 {
		t.Fatal("次の行が無いときの分かれ道の終わりが分からない")
	}
	arm := block[at : at+end]
	if !strings.Contains(arm, "flush()") {
		t.Error("次が無いときに保存が走っていない")
	}
	if strings.Contains(arm, "commitEditor()") {
		t.Error("次が無いのに入力欄を閉じている。焦点が body へ落ちる")
	}
}

// TestAlertsAreAnnounced は、画面の手応えが読み上げにも出ることを見る。
//
// 実際に起きた: この頁には aria-live も role=status も1つも無かった。絞り込みの
// 唯一の手応えである「表示中 N 行」も、黙って失敗しないという約束（保存の状態と
// 失敗の理由）も、目で見ている人だけのものになっていた。絞り込みの群は素の
// span で、文脈の無いチェックボックスが9個並んでいるようにしか読み上げられない。
func TestAlertsAreAnnounced(t *testing.T) {
	html := uiSource(t, "ui/index.html")

	for _, want := range []string{
		`id="shown" class="shown" aria-live="polite"`,
		`id="save-state" class="save-state" aria-live="polite"`,
		`id="message" class="notice error" role="status"`,
		`id="empty" class="notice empty" role="status"`,
		`id="filters" class="filters" role="group" aria-labelledby="filter-label"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html に %q が無い。手応えが読み上げに出ない", want)
		}
	}

	// 見張り（role="status"）は hidden で出し入れしない。hidden の要素は支援
	// 技術の木から外れるので、文字が変わる瞬間にその要素が木に居らず、告知
	// しない実装があり得る。出し入れは中身の入れ替えで行い、空のときは
	// app.css の :empty が余白と下線を落とす。
	//
	// 断っておくと、ここで確かめているのは属性と形までである。読み上げソフトで
	// 実際に告知されたかどうかは、この環境では確かめられていない。
	for _, bad := range []string{
		`class="notice error" role="status" hidden`,
		`class="notice empty" role="status" hidden`,
	} {
		if strings.Contains(html, bad) {
			t.Errorf("index.html の %q が hidden で出し入れされている。木から外れると告知されない", bad)
		}
	}
	js := uiSource(t, "ui/app.js")
	for _, bad := range []string{"el.message.hidden", "el.empty.hidden"} {
		if strings.Contains(js, bad) {
			t.Errorf("app.js が %s を触っている。中身の入れ替えで出し入れすること", bad)
		}
	}
	css := uiSource(t, "ui/app.css")
	if !strings.Contains(css, ".notice:empty {") {
		t.Error("app.css に .notice:empty が無い。空の断り書きが場所を取ったままになる")
	}
}

// TestChipsNameTheirWeight は、条件のチップが重さを名前でも出すことを見る。
//
// 縁と文字の色だけで重さを伝えていた。--todo #8a4b00 と --review #9a1c1c は
// 色覚によっては近く、app.css の冒頭に書いた「色だけで意味を伝えない」に
// 反していた。件数の欄のほうは最初から名前を出している。
func TestChipsNameTheirWeight(t *testing.T) {
	js := uiSource(t, "ui/app.js")
	css := uiSource(t, "ui/app.css")

	if !strings.Contains(js, `span("chip-status", statusLabel)`) {
		t.Error("チップに重さの名前を添えていない")
	}
	// 名前は待ち受けが返したものを使うこと（画面で決めない）。
	if !strings.Contains(js, "c.statusLabel") {
		t.Error("重さの名前を待ち受けの件数から取っていない")
	}
	if !strings.Contains(css, ".chip-status {") {
		t.Error("app.css に重さの名前の指定が無い")
	}
}

// TestLocaleChangeClearsTheFinder は、ロケールを切り替えたら条件と検索語が
// 外れることを見る。あわせて、1行も出なかったときの文言があることも見る。
//
// 実際に起きた: ja で「もしもし」を打ったまま ko へ移ると、ヘッダーが
// 「行: 1713」と出ているのに一覧が空になった。手がかりは隅の「表示中 0 行」
// だけで、壊れたのか条件に当たっていないのかが読み取れなかった。
func TestLocaleChangeClearsTheFinder(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, `el.locale.addEventListener("change"`)
	if start < 0 {
		t.Fatal("ロケールの切り替えが無い")
	}
	end := strings.Index(js[start:], "\n        });")
	if end < 0 {
		t.Fatal("ロケールの切り替えの終わりが分からない")
	}
	// 読むのは change の時点で控えた値（chosen）。尋ねるのは送り終えてからなので、
	// そのあいだに欄の値は変わりうる（app.js の load が欄を描いたロケールへそろえる）。
	if !strings.Contains(js[start:start+end], "load(chosen, true)") {
		t.Error("ロケールを切り替えても条件と検索語が残る。前のロケールの条件を持ち越す")
	}
	// 外すのは切り替えの手前ではなく、読めたときだけ。
	//
	// 実際に起きた: 切り替えの handler が clearFinder() を load() より先に
	// 呼んでいたので、読み込みに失敗すると state.filter と検索欄だけが空になり、
	// チップの checked と一覧は前のまま残った（「チップ2つが選ばれたまま、
	// 一覧も表示中の行数も前のロケールのまま」を実測した）。
	if strings.Contains(js[start:start+end], "clearFinder()") {
		t.Error("切り替えの手前で条件を外している。読み込みに失敗すると条件・検索欄・一覧が食い違う")
	}
	load := strings.Index(js, "function load(locale, resetFinder)")
	if load < 0 {
		t.Fatal("load が resetFinder を受けていない")
	}
	fail := strings.Index(js[load:], ".catch(function ()")
	ok := strings.Index(js[load:], "clearFinder();")
	if ok < 0 {
		t.Fatal("load が clearFinder を呼んでいない")
	}
	if fail >= 0 && ok > fail {
		t.Error("clearFinder が読み込みの失敗側にある。成功したときだけ外すこと")
	}
	// 読み直し（同じロケール）では外さないこと。
	at := strings.Index(js, `el.reload.addEventListener("click"`)
	if at < 0 {
		t.Fatal("読み直しが無い")
	}
	tail := strings.Index(js[at:], "\n        });")
	if tail < 0 {
		t.Fatal("読み直しの終わりが分からない")
	}
	if strings.Contains(js[at:at+tail], "clearFinder()") {
		t.Error("読み直しで条件まで外している。同じロケールを見続けている")
	}
	// 0行のときの文言。
	if !strings.Contains(js, `t("ui.no_rows")`) {
		t.Error("1行も出なかったときの文言が無い")
	}
}

// TestHeadingsAreDroppedWithTheirNode は、見出しの持ち回りが節点でも切れる
// ことを見る。
//
// 切らないと、# ===== でも # --- でもないコメント行（翻訳者が書いた1行メモ
// など）が、その下の行が1つも出ていないのに、後ろの節点の行の見出しとして
// 出続ける。internal/edit は先頭が # の行をすべてコメントとして扱うので、
// メモを1行書いた時点で起きる。
func TestHeadingsAreDroppedWithTheirNode(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, "function applyView()")
	if start < 0 {
		t.Fatal("applyView が無い")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("applyView の終わりが分からない")
	}
	body := js[start : start+end]
	if !strings.Contains(body, `} else if (item.level === "node") {`) {
		t.Error("節点が変わったときに見出しを捨てていない")
	}
	// heads.other を捨てるのは2か所。節が変わったとき（node も一緒に捨てる）と、
	// 節点が変わったときである。
	if n := strings.Count(body, "heads.other = null;"); n != 2 {
		t.Errorf("heads.other を捨てるのが %d か所。節と節点の2か所であること", n)
	}
}

// TestSearchFollowsTheLocale は、検索の欄が開いているロケールに合わせて
// 向きと言語を持つことを見る。
//
// 訳の入力欄は最初から dir="auto" と lang=ロケール名 を持っていたが、検索の欄は
// 持っていなかった。he や ar の翻訳者が検索語を打つと、キャレットと並びが
// 左から右のままになる。当てる先（原文・訳）はそのロケールの字である。
func TestSearchFollowsTheLocale(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	for _, want := range []string{`el.search.dir = "auto"`, "el.search.lang = data.locale"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js に %q が無い。検索の欄がロケールに合わない", want)
		}
	}
}

// TestEnterOpensTheNextRow は、Enter が確定して次の行を開くことを見る。
//
// 翻訳作業のいちばん太い道である。上から順に打っていけないと、1721行の
// ファイルでは1行ごとにマウスへ手が戻る。
//
// 同時に、変換中の Enter を横取りしていないことも見る。横取りすると
// ja / ko / zh-Hans / zh-Hant で変換の確定ができなくなる。
func TestEnterOpensTheNextRow(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, `editor.addEventListener("keydown"`)
	if start < 0 {
		t.Fatal("入力欄の keydown が無い")
	}
	block := js[start:]
	guard := strings.Index(block, "if (state.composing || e.isComposing || e.keyCode === 229)")
	enter := strings.Index(block, `if (e.key === "Enter")`)
	if guard < 0 || enter < 0 {
		t.Fatal("変換中の見張りか Enter の扱いが無い")
	}
	if guard > enter {
		t.Error("変換中の見張りが Enter より後ろにある。変換の確定ができなくなる")
	}
	for _, want := range []string{
		"var from = state.editing;",
		"nextEditable(from)",
		"openEditor(next)",
	} {
		if !strings.Contains(block[enter:], want) {
			t.Errorf("Enter が %s を通っていない。次の行が開かない", want)
		}
	}
	// 次の行を決めてから閉じること。閉じると state.editing が空になる。
	next := strings.Index(block[enter:], "nextEditable(from)")
	closed := strings.Index(block[enter:], "closeEditor();")
	if closed < 0 || next > closed {
		t.Error("閉じてから次の行を決めている。state.editing はもう空である")
	}
	// 隠れている行は飛ばすこと。
	body := strings.Index(js, "function nextEditable(")
	if body < 0 || !strings.Contains(js[body:body+700], "entry.row.hidden") {
		t.Error("nextEditable が隠れている行を飛ばしていない")
	}
}

// TestShortcutsAreOffWhileTyping は、一文字の近道が入力欄で効かないことを見る。
//
// 効くと、その字が訳にも検索語にも打てなくなる。変換中にも横取りしない。
func TestShortcutsAreOffWhileTyping(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, `document.addEventListener("keydown"`)
	if start < 0 {
		t.Fatal("頁全体の keydown が無い")
	}
	end := strings.Index(js[start:], "\n  });")
	if end < 0 {
		t.Fatal("頁全体の keydown の終わりが分からない")
	}
	block := js[start : start+end]
	for _, want := range []string{
		"state.composing || e.isComposing || e.keyCode === 229",
		"isTyping(e.target)",
		"e.ctrlKey || e.metaKey || e.altKey",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("頁全体の keydown に %s が無い", want)
		}
	}
	// 見張りは、キーを見分ける前に置くこと。
	if key := strings.Index(block, `e.key === "/"`); key < strings.Index(block, "isTyping(e.target)") {
		t.Error("入力欄の見張りがキーの見分けより後ろにある")
	}

	// ただし、チェックボックスとラジオは「打っている」に数えない。
	//
	// 実際に起きた: INPUT を type を見ずに弾いていたので、絞り込みの条件に焦点が
	// あるあいだスラッシュが効かず、「条件を選んだ直後にスラッシュで検索へ移る」
	// という、いちばんありそうな流れで黙って何も起きなかった。
	at := strings.Index(js, "function isTyping(")
	if at < 0 {
		t.Fatal("isTyping が無い")
	}
	tail := strings.Index(js[at:], "\n  }")
	if tail < 0 {
		t.Fatal("isTyping の終わりが分からない")
	}
	fn := js[at : at+tail]
	if strings.Contains(fn, `tag === "INPUT" || tag === "TEXTAREA"`) {
		t.Error("INPUT を type を見ずに弾いている。条件に焦点があるとスラッシュが効かない")
	}
	for _, want := range []string{`type !== "checkbox"`, `type !== "radio"`} {
		if !strings.Contains(fn, want) {
			t.Errorf("isTyping が %s を見ていない", want)
		}
	}

	// スラッシュは、貼り付けるのをやめた絞り込みの一帯を画面へ送ること。
	focus := strings.Index(block, "el.search.focus()")
	scroll := strings.Index(block, "el.finder.scrollIntoView(")
	if scroll < 0 {
		t.Error("スラッシュが絞り込みの一帯を画面へ送っていない。欄が見えないまま字が入る")
	} else if focus < 0 || focus > scroll {
		// 送ってから焦点を移すと、そのあとの focus がブラウザーの既定の送り方
		// （入力欄そのものを画面へ入れる。一帯の余白は使わない）で上書きし、
		// 検索の欄が貼り付く帯の下へ潜る。
		t.Error("一帯を送ってから焦点を移している。focus が既定の送り方で上書きする")
	}
}

// TestUIDoesNotNameCategories は、画面がカテゴリを名前で持っていないことを見る。
//
// 絞り込みの一覧は、待ち受けが返した件数（countView）から組む。画面に
// カテゴリ名を書くと、そこが2つ目の定義になる。internal/diff にカテゴリが
// 増えたとき、画面だけが古いままになる。
func TestUIDoesNotNameCategories(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	s := newTestServer(t, Options{})
	rec := do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	got := decode[linesResponse](t, rec.Body.Bytes())
	if len(got.Counts) == 0 {
		t.Fatal("件数が空")
	}
	for _, c := range got.Counts {
		if strings.Contains(js, c.Category) {
			t.Errorf("app.js に %q がある。カテゴリは待ち受けが返したものだけを使う", c.Category)
		}
	}
	// 組み立てが件数から来ていること。
	if !strings.Contains(js, `filterChip("cat:" + c.category`) {
		t.Error("絞り込みの一覧が件数から組まれていない")
	}
}

// TestConflictHoldsOnlyRowsTheOtherSideChanged は、409 のときに「よそが本当に
// 書き換えた行」だけを選ばせる対象にしていることを見る。
//
// 実際に起きた: 409 のたびに未保存の訳を丸ごと state.mine へ移していたため、
// よそが触ったのが別の行でも、こちらの訳が全部「競合したもの」になった。
// そのまま 409 がもう一度起きると、選んでいるあいだに打った訳まで移り、
// 「ファイルの訳を採る」で一緒に捨てられた。ファイルにも画面にも残らず、
// 行き先の無い訳の一覧にも入らず、beforeunload の引き止めも効かなかった。
func TestConflictHoldsOnlyRowsTheOtherSideChanged(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	if strings.Contains(js, "state.mine = carried.edits") {
		t.Error("409 で未保存の訳を丸ごと競合したものへ移している")
	}
	for _, want := range []string{
		// 最後に見たファイルの値と読み直した値を比べる。
		"function fileChanged(",
		// 変わった行と、変わっていない行に分ける。
		"var clashed = new Map();",
		"var untouched = new Map();",
		// 変わっていない行は未保存のまま残す。
		"state.pending = keep.edits;",
		// 選ばせる対象が無いなら引き止めを出さない。
		"state.mine = mine.edits.size ? mine.edits : null;",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js に %q が無い。選ばせる対象は実際に競合した行だけにする", want)
		}
	}
}

// TestConflictButtonsComeFirst は、競合の決断ボタンが貼り付く帯の先頭に出ることを見る。
//
// 実際に起きた: 600x500 と 320x600 では、bar が何段にも折り返したぶん、その下の
// 引き止めが max-height: 50vh の外へ沈み、「ファイルの訳を採る」に
// elementFromPoint が当たらず、実際に押しても何も起きなかった。引き止めが
// 出ているあいだ自動保存は止まっているので、ボタンに手が届かないことは
// 「打ち続けられるのに1バイトも保存されない」状態が続くことを意味する。
func TestConflictButtonsComeFirst(t *testing.T) {
	css := uiSource(t, "ui/app.css")

	block := strings.Index(css, ".top {")
	if block < 0 {
		t.Fatal("app.css に .top が無い")
	}
	if !strings.Contains(css[block:block+400], "flex-direction: column") {
		t.Error(".top が縦の flex になっていない。order が効かない")
	}
	at := strings.Index(css, "#conflict {")
	if at < 0 {
		t.Fatal("app.css に #conflict の並びの指定が無い")
	}
	if !strings.Contains(css[at:at+120], "order: -1") {
		t.Error("競合の引き止めが帯の先頭に出ない")
	}
}

// TestOrphansAreNotCalledSaved は、行き先の見つからない訳が残っているときに
// 「保存済み」と出さないことを見る。
//
// その訳はファイルに1つも入っていない。画面のその欄が、その訳が残っている
// 最後の場所である。「保存済み」と言うと、翻訳者は安心して閉じにいき、
// beforeunload の引き止めで初めて何かが残っていると知ることになる。
func TestOrphansAreNotCalledSaved(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	at := strings.Index(js, "function updateStatus()")
	if at < 0 {
		t.Fatal("updateStatus が無い")
	}
	end := strings.Index(js[at:], "\n  }")
	if end < 0 {
		t.Fatal("updateStatus の終わりが分からない")
	}
	if !strings.Contains(js[at:at+end], "state.orphans.length") {
		t.Error("updateStatus が行き先の無い訳を見ていない")
	}
	if !strings.Contains(js, `t("ui.save_orphans"`) {
		t.Error("行き先の無い訳のための文言を使っていない")
	}
}

// TestOpenEditorReviewsTheRowItLeft は、マウスで行から行へ移ったときにも
// 離れた行を条件へ照らし直すことを見る。
//
// 実際に起きた: openEditor が素の closeEditor を呼ぶだけだったので、この道だけが
// 照らし直しを通らなかった。closeEditor が state.editing を空にしたあとに blur が
// 走るため、blur 側の commitEditor は null を受け取って何もしない。条件に
// 当たらなくなった行が一覧に残り、「表示中 N 行」も減らないままになった。
func TestOpenEditorReviewsTheRowItLeft(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	at := strings.Index(js, "function openEditor(")
	if at < 0 {
		t.Fatal("openEditor が無い")
	}
	end := strings.Index(js[at:], "\n  }")
	if end < 0 {
		t.Fatal("openEditor の終わりが分からない")
	}
	body := js[at : at+end]
	if !strings.Contains(body, "var leaving = state.editing;") {
		t.Error("離れる行を控えていない")
	}
	if !strings.Contains(body, "reviewClosed(leaving)") {
		t.Error("離れた行を条件へ照らし直していない")
	}
	// 照らし直しは、新しい行を開き終えてからでなければならない。逆にすると、
	// これから開く行が条件に当たらない場合その場で隠れ、隠れた行へ入力欄が
	// 差し込まれる（焦点は入っているのに欄が見えない）。
	if strings.Index(body, "editor.focus()") > strings.Index(body, "reviewClosed(leaving)") {
		t.Error("照らし直しが、行を開く前に走っている")
	}
}

// TestScreenElementsExist は、app.js が探す id が index.html にあることを見る。
//
// getElementById は無い id に null を返すだけで、その場では落ちない。落ちるのは
// そこへ書き込む段になってからで、画面のどこか1か所が黙って出なくなる。
// ゲームのフォルダーを出す行（game-path）のように、ふだんは隠れている要素ほど
// 気づけない。
func TestScreenElementsExist(t *testing.T) {
	html := uiSource(t, "ui/index.html")
	js := uiSource(t, "ui/app.js")

	for _, id := range regexp.MustCompile(`getElementById\("([a-z0-9-]+)"\)`).
		FindAllStringSubmatch(js, -1) {
		if !strings.Contains(html, `id="`+id[1]+`"`) {
			t.Errorf("app.js が探す id が index.html に無い: %s", id[1])
		}
	}
}
