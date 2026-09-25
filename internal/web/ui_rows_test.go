package web

import (
	"strings"
	"testing"
)

// この束は、画面が行を ID で引き、行をまたぐレコードを1行として描き、読み上げに行の
// 文脈を渡していることを字面で見る。
//
// 振る舞いは E2E（e2e/specs/multiline.spec.mjs と render.spec.mjs）が見ている。ここに
// あるのは、実際に踏んで決めた形が書き換えで消えないための見張りである。

// cssRule は app.css の中の selector で始まる規則の中身（次の "\n}" まで）を返す。
func cssRule(t *testing.T, css, selector string) string {
	t.Helper()
	at := strings.Index(css, "\n"+selector+" {")
	if at < 0 {
		t.Fatalf("app.css に %s が無い", selector)
	}
	end := strings.Index(css[at:], "\n}")
	if end < 0 {
		t.Fatalf("%s の終わりが分からない", selector)
	}
	return css[at : at+end]
}

// TestRowsAreAddressedByID は、画面が行を ID（lineView の id）で引いていることを見る。
//
// 行番号で引いていたころは、引用符で囲んだ値に改行があるレコードで行番号とレコードが
// 1対1にならなかった。待ち受けは行番号の要求を知らない鍵として 400 で断る
// （rowsRequest の DisallowUnknownFields）ので、画面が line を送ると1行も保存できない。
func TestRowsAreAddressedByID(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	flush := functionBody(t, js, "flush")
	if !strings.Contains(flush, "id: id,") {
		t.Error("保存の要求が行を ID で指していない")
	}
	if strings.Contains(flush, "line: line") {
		t.Error("保存の要求がまだ行番号（line）を送っている。待ち受けは 400 で断る")
	}
	for _, name := range []string{"applyResults", "applyRowErrors"} {
		if !strings.Contains(functionBody(t, js, name), "state.rows.get(r.id)") {
			t.Errorf("%s が結果を ID で引いていない", name)
		}
	}

	row := functionBody(t, js, "rowNode")
	for _, want := range []string{
		"row.dataset.id = String(line.id);",
		"value.dataset.id = String(line.id);",
		"state.rows.set(line.id, entry);",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("rowNode に %q が無い", want)
		}
	}
	if strings.Contains(js, "dataset.line") {
		t.Error("app.js に dataset.line が残っている。行は data-id で引く")
	}
	// 行そのものも data-id を持つので、訳の欄かどうかを class でも見ること。見ないと、
	// 行の余白を押しただけで入力欄が開く。
	if !strings.Contains(functionBody(t, js, "idOf"), `target.classList.contains("translation")`) {
		t.Error("idOf が訳の欄かどうかを見ていない。行の余白を押しても入力欄が開く")
	}
}

// TestRowNumberShowsTheRange は、行番号の欄に最初の物理行を出し、行をまたぐレコード
// だけ最後の物理行を添えることを見る（決まったことのそのほか 3）。区切りの字も目録から
// 引く。画面に「〜」を直に書くと、英語の画面にも残る。
func TestRowNumberShowsTheRange(t *testing.T) {
	js := uiSource(t, "ui/app.js")
	css := uiSource(t, "ui/app.css")

	row := functionBody(t, js, "rowNode")
	for _, want := range []string{
		`num.appendChild(span("num-start", line.n));`,
		"if (line.end) {",
		`t("ui.line_end", { line: line.end })`,
	} {
		if !strings.Contains(row, want) {
			t.Errorf("rowNode に %q が無い", want)
		}
	}
	if !strings.Contains(cssRule(t, css, ".num-start,\n.num-end"), "display: block;") {
		t.Error("行番号の範囲が2段に分かれていない。6ch の欄で「1865〜」と「1867」に割れる")
	}
}

// TestSourceKeepsLineBreaks は、原文の欄が値の中の改行と空行をそのまま描き、英語として
// 読ませることを見る（改善の決定 ui-9）。
func TestSourceKeepsLineBreaks(t *testing.T) {
	js := uiSource(t, "ui/app.js")
	css := uiSource(t, "ui/app.css")

	if !strings.Contains(cssRule(t, css, ".source"), "white-space: pre-wrap;") {
		t.Error(".source に white-space: pre-wrap が無い。段落を2つ持つ原文が1段に詰まり、空行も消える")
	}
	if !strings.Contains(functionBody(t, js, "rowNode"), `source.lang = "en";`) {
		t.Error("原文の欄に lang=\"en\" が無い。日本語の画面では英文が日本語の声で読まれる")
	}
}

// TestEditorNamesItsRow は、入力欄の名前に行番号を入れ、説明に原文と行の1言を結ぶことを
// 見る（改善の決定 20）。名前が「訳」だけだと、Enter で次の行へ進むたびに「訳、編集」と
// だけ読まれ、どの原文を訳しているかが分からない。
func TestEditorNamesItsRow(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	open := functionBody(t, js, "openEditor")
	for _, want := range []string{
		`editor.setAttribute("aria-label", t("ui.edit_label_line", { line: entry.n }));`,
		`editor.setAttribute("aria-describedby", entry.source.id + " " + entry.note.id);`,
	} {
		if !strings.Contains(open, want) {
			t.Errorf("openEditor に %q が無い", want)
		}
	}
	row := functionBody(t, js, "rowNode")
	for _, want := range []string{`source.id = "source-" + line.id;`, `note.id = "note-" + line.id;`} {
		if !strings.Contains(row, want) {
			t.Errorf("rowNode に %q が無い。入力欄の説明から指せない", want)
		}
	}
}

// TestSectionHeadingsAreHeadings は、節と節点の見出しを読み上げの見出しにし、メモは
// 見出しにしないことを見る（改善の決定 ui-10）。
func TestSectionHeadingsAreHeadings(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	head := functionBody(t, js, "headingNode")
	for _, want := range []string{
		`if (level !== "other") {`,
		`e.setAttribute("role", "heading");`,
		`e.setAttribute("aria-level", level === "section" ? "2" : "3");`,
	} {
		if !strings.Contains(head, want) {
			t.Errorf("headingNode に %q が無い", want)
		}
	}
}

// TestOffscreenRowsAreNotDrawn は、画面の外の行を描かず、入力欄を差し込んだ行だけは
// いつも描くことと、見出しには描かない指定を付けないことを見る（改善の決定 21）。
//
// 見出しに付けると、Chromium の読み上げの木で、画面の外の見出しの名前が空になり、
// 読み上げソフトの見出しの一覧と見出しへ移る操作が使えなくなる（改善の ui-10）。
func TestOffscreenRowsAreNotDrawn(t *testing.T) {
	css := uiSource(t, "ui/app.css")

	rule := cssRule(t, css, ".list > .row")
	if !strings.Contains(rule, "content-visibility: auto;") {
		t.Error("行に content-visibility: auto が無い。全行を描き直すたびに画面が止まる")
	}
	if !strings.Contains(rule, "contain-intrinsic-block-size: auto ") {
		t.Error("行に高さの見積もり（contain-intrinsic-block-size: auto …）が無い")
	}
	if !strings.Contains(cssRule(t, css, ".list > .row:has(> .editor)"), "content-visibility: visible;") {
		t.Error("入力欄を差し込んだ行を描かないままにしている。入力欄の高さを測れない")
	}
	if strings.Contains(css, "\n.list > .heading {") || strings.Contains(cssRule(t, css, ".heading"), "content-visibility") {
		t.Error("見出しに描かない指定がある。画面の外の見出しが読み上げの木で名前を失う")
	}
}

// TestRemapDoesNotPlaceOnRowsThatCannotBeEdited は、409 のあとの載せ直しで、編集
// できない行には載せないことを見る（決まったことのそのほか 5）。
//
// 開いているあいだにファイルが読み取り専用の形に書き換わると、待ち受けは 409 と読み
// 取り専用の理由つきの一覧を返す。編集できない行に載せると、訳は生の行の裏に隠れ、
// 保存は読み取り専用で断られ続ける。行き先の無い訳として出す。キーの無い行も、同じ
// ID にいまキーのある行が来ていたら載せない。
func TestRemapDoesNotPlaceOnRowsThatCannotBeEdited(t *testing.T) {
	js := uiSource(t, "ui/app.js")

	remap := functionBody(t, js, "remap")
	for _, want := range []string{
		"if (line.editable) {",
		`if (open.get(id) === "") {`,
		"if (to === null || moved.has(to) || !open.has(to)) {",
	} {
		if !strings.Contains(remap, want) {
			t.Errorf("remap に %q が無い", want)
		}
	}
}
