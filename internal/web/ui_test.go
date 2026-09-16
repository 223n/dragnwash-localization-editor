package web

import (
	"io/fs"
	"net/http"
	"strings"
	"testing"
)

// この束は、画面側の「訳を失わない」仕掛けが消えていないことを見る。
//
// どれも字面の試験である。Go から DOM を動かせないので、振る舞いそのものは
// 保証できない。それでも書いてあるのは、ここに挙げた5つが、実際に動かして
// 見つかった「訳が消える／訳が別の行に入る」経路の直しそのものだからである。
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

// TestAlertsStayOnScreen は、競合の引き止めと失敗の理由が画面に残ることを見る。
//
// 実際に起きた: 1681行の途中（scrollY 55546）で 409 が起きると、決断用の
// ボタンは画面の約55,000px 上にあり、そのあいだ自動保存は止まったままだった。
func TestAlertsStayOnScreen(t *testing.T) {
	html := uiSource(t, "ui/index.html")
	css := uiSource(t, "ui/app.css")

	top := strings.Index(html, `<div class="top">`)
	if top < 0 {
		t.Fatal("index.html に貼り付ける一帯（.top）が無い")
	}
	end := strings.Index(html[top:], "</div>")
	if end < 0 {
		t.Fatal(".top の終わりが分からない")
	}
	inside := html[top : top+end]
	for _, want := range []string{`id="message"`, `id="conflict"`, `id="orphans"`} {
		if !strings.Contains(inside, want) {
			t.Errorf("%s が貼り付ける一帯の外にある。行の途中では見えなくなる", want)
		}
	}

	block := strings.Index(css, ".top {")
	if block < 0 {
		t.Fatal("app.css に .top が無い")
	}
	if !strings.Contains(css[block:block+200], "position: sticky") {
		t.Error(".top が貼り付いていない")
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
	// 出すか決めるところで、条件より先に keepAlways を見ていること。
	if !strings.Contains(js, "var show = keepAlways(entry.line) ||") {
		t.Error("出す行の決め方が keepAlways を通っていない")
	}
}

// TestHidingAnOpenRowSavesFirst は、絞り込みで行が消えるとき、その行の入力欄が
// 開いていたら先に保存してから閉じることを見る。
//
// 入力欄を差し込んだまま行ごと隠すと、打った訳が画面からも消える。未保存の行は
// そもそも隠さないので通る道は狭いが、打ち始める前（まだ未保存でない）の行は
// ここを通る。
func TestHidingAnOpenRowSavesFirst(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	start := strings.Index(js, "function applyView()")
	if start < 0 {
		t.Fatal("applyView が無い")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("applyView の終わりが分からない")
	}
	if !strings.Contains(js[start:start+end], "commitEditor()") {
		t.Error("applyView が入力欄を保存してから閉じていない")
	}
	// commitEditor は閉じる前に保存へ回すこと。
	at := strings.Index(js, "function commitEditor()")
	if at < 0 {
		t.Fatal("commitEditor が無い")
	}
	if !strings.Contains(js[at:at+120], "flush()") {
		t.Error("commitEditor が flush を呼んでいない")
	}
}

// TestEnterOpensTheNextRow は、Enter が確定して次の行を開くことを見る。
//
// 翻訳作業のいちばん太い道である。上から順に打っていけないと、1839行の
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
	for _, want := range []string{"nextEditable(state.editing)", "openEditor(next)"} {
		if !strings.Contains(block[enter:], want) {
			t.Errorf("Enter が %s を通っていない。次の行が開かない", want)
		}
	}
	// 次の行を決めてから閉じること。閉じると state.editing が空になる。
	next := strings.Index(block[enter:], "nextEditable(state.editing)")
	commit := strings.Index(block[enter:], "commitEditor()")
	if commit < 0 || next > commit {
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
