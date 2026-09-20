package web

import (
	"strings"
	"testing"
)

// ここは差し込む入力欄の形についての試験。
//
// 入力欄は長いあいだ input type="text" だった。欄の幅に収まらない訳が横へ流れ、
// 打っている場所の前後しか見えないという声が上がっている（issue #41）。
// 出ている側の欄（.row .translation）は white-space: pre-wrap で折り返して
// いるので、input のままだと打ち始めた瞬間に見え方が変わることにもなっていた。

// TestEditorIsTextarea は、入力欄が折り返す欄であることを見る。
//
// input へ戻すと、長い訳が1行へ流れて元に戻る。戻せない理由は改行が入ることでは
// ない（Enter は行送りに使っており、貼り付けも空白へ置き換える）。
func TestEditorIsTextarea(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	if !strings.Contains(js, `var editor = document.createElement("textarea")`) {
		t.Error("入力欄が textarea でない。長い訳が1行へ流れる")
	}
	if strings.Contains(js, `editor.type = "text"`) {
		t.Error("input のときの指定が残っている")
	}

	css := uiSource(t, "ui/app.css")
	at := strings.Index(css, ".editor {")
	if at < 0 {
		t.Fatal("app.css に .editor が無い")
	}
	end := strings.Index(css[at:], "\n}")
	if end < 0 {
		t.Fatal(".editor の終わりが分からない")
	}
	block := css[at : at+end]
	// 折り返すこと。出ている側の欄と同じ見え方にそろえる。
	if !strings.Contains(block, "white-space: pre-wrap") {
		t.Error(".editor が折り返さない")
	}
	// 隅を引いて伸び縮みさせられると、fitEditor の測り直しと食い違う。
	if !strings.Contains(block, "resize: none") {
		t.Error(".editor で resize を切っていない")
	}
	// 高さが合っていない場面で字を切らない。送りの棒が出るほうがよい。
	if strings.Contains(block, "overflow: hidden") || strings.Contains(block, "overflow-y: hidden") {
		t.Error(".editor で overflow を hidden にしている。高さが合わないと字が切れる")
	}
	// 打ち始めた瞬間に行が詰まらないよう、出ている側の欄と同じ下限を敷く。
	if !strings.Contains(block, "min-height: 2em") {
		t.Error(".editor に min-height が無い。空の行で高さが変わる")
	}
	// font: inherit をこの欄まで届かせること。textarea は既定でこの頁の字を継がない。
	if !strings.Contains(css, "textarea {\n  font: inherit;") {
		t.Error("textarea に font: inherit が当たっていない")
	}
}

// TestEditorHeightFollowsContent は、入力欄の高さを中身に合わせていることを見る。
//
// textarea は rows で決まった高さのままなので、折り返して増えたぶんは自分で
// 伸ばすしかない。伸ばさなければ input と同じで、1行しか見えない。
func TestEditorHeightFollowsContent(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	at := strings.Index(js, "function fitEditor() {")
	if at < 0 {
		t.Fatal("fitEditor が無い。入力欄の高さが中身に追わない")
	}
	end := strings.Index(js[at:], "\n  }")
	if end < 0 {
		t.Fatal("fitEditor の終わりが分からない")
	}
	block := js[at : at+end]

	// いったん auto へ戻してから測ること。戻さないと前の高さが下限として残り、
	// 字を消しても縮まない。
	autoAt := strings.Index(block, `editor.style.height = "auto"`)
	measureAt := strings.Index(block, "editor.scrollHeight")
	if autoAt < 0 {
		t.Error("測る前に高さを auto へ戻していない。字を消しても縮まない")
	}
	if measureAt < 0 {
		t.Fatal("scrollHeight で測っていない")
	}
	if autoAt > measureAt {
		t.Error("auto へ戻すのが測るより後ろにある")
	}
	// 枠線のぶんを足すこと。この頁は box-sizing: border-box なので、
	// scrollHeight をそのまま入れると内側が枠線のぶんだけ足りない。
	//
	// 実際に起きた: 4行に折り返した訳で clientHeight 92 に対して scrollHeight 94 に
	// なり、2pxの送りの棒が出た。
	if !strings.Contains(block, "borderTopWidth") || !strings.Contains(block, "borderBottomWidth") {
		t.Error("枠線のぶんを足していない。折り返した訳に送りの棒が出る")
	}

	// 開くときは、焦点を移すより先に高さを決めること。focus は欄を画面へ
	// 入れようとするので、あとから伸ばすと送った先が実際の位置とずれる。
	open := strings.Index(js, "entry.row.insertBefore(editor, entry.value)")
	if open < 0 {
		t.Fatal("入力欄を差し込む場所が無い")
	}
	tail := js[open:]
	fit := strings.Index(tail, "fitEditor()")
	focus := strings.Index(tail, "editor.focus()")
	if fit < 0 {
		t.Error("開くときに高さを決めていない")
	}
	if focus < 0 {
		t.Fatal("開くときに焦点を移していない")
	}
	if fit > focus {
		t.Error("高さを決めるのが焦点より後ろにある。送った先が欄の位置とずれる")
	}

	// 打つたびに測り直すこと。折り返しが増えても高さが追わなければ意味がない。
	input := strings.Index(js, "function onInput() {")
	if input < 0 {
		t.Fatal("onInput が無い")
	}
	inputEnd := strings.Index(js[input:], "\n  }")
	if inputEnd < 0 {
		t.Fatal("onInput の終わりが分からない")
	}
	if !strings.Contains(js[input:input+inputEnd], "fitEditor()") {
		t.Error("打つたびに高さを測り直していない")
	}
}

// TestEditorStillRejectsNewlines は、折り返す欄にしても値に改行が入らないことを見る。
//
// internal/edit は CR / LF を含む値を拒む。公開ファイルの読み手が1物理行=1レコードで
// 読むためで、textarea にしたのは折り返しのためだけである。
func TestEditorStillRejectsNewlines(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	// Enter は行送り。改行は入れない。
	start := strings.Index(js, `editor.addEventListener("keydown"`)
	if start < 0 {
		t.Fatal("入力欄の keydown が無い")
	}
	block := js[start:]
	enter := strings.Index(block, `if (e.key === "Enter") {`)
	if enter < 0 {
		t.Fatal("Enter の分かれ道が無い")
	}
	if !strings.Contains(block[enter:enter+1200], "e.preventDefault()") {
		t.Error("Enter で preventDefault を通していない。textarea では改行が入る")
	}

	// 貼り付けと入力は空白へ置き換える。
	if !strings.Contains(js, `replace(/[\r\n]+/g, " ")`) {
		t.Error("改行を空白へ置き換えていない")
	}
}
