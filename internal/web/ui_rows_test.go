package web

import (
	"strings"
	"testing"
)

// この束は、画面が行を ID で引き、行をまたぐレコードを1行として描いていることを
// 字面で見る。
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

// TestSourceKeepsLineBreaks は、原文の欄が値の中の改行と空行をそのまま描くことを見る。
func TestSourceKeepsLineBreaks(t *testing.T) {
	css := uiSource(t, "ui/app.css")

	if !strings.Contains(cssRule(t, css, ".source"), "white-space: pre-wrap;") {
		t.Error(".source に white-space: pre-wrap が無い。段落を2つ持つ原文が1段に詰まり、空行も消える")
	}
}
