package web

import (
	"io/fs"
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
