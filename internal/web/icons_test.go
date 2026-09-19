package web

import (
	"regexp"
	"strings"
	"testing"
)

// この束は、画面のアイコンが「頁に埋めた図形だけを使い、読み上げの邪魔をしない」
// ことを見る。
//
// アイコンは index.html の先頭の svg に <symbol id="i-…"> として埋めてあり、
// HTML の <use href="#i-…"> と app.js の icon("…") がそれを指す。名前を打ち
// 間違えても、ブラウザーは何も描かないだけで誤りを出さない。ボタンからアイコンが
// 黙って消える。

// spriteSymbols は index.html に埋めた図形の名前を返す。
func spriteSymbols(t *testing.T) map[string]struct{} {
	t.Helper()
	html := uiSource(t, "ui/index.html")
	out := make(map[string]struct{})
	for _, m := range regexp.MustCompile(`<symbol id="i-([a-z0-9-]+)"`).FindAllStringSubmatch(html, -1) {
		out[m[1]] = struct{}{}
	}
	if len(out) == 0 {
		t.Fatal("index.html に図形（<symbol id=\"i-…\">）が1つも無い")
	}
	return out
}

// TestIconsExistInSprite は、使っているアイコンが全部埋めてあり、埋めてある
// アイコンが全部使われていることを見る。
//
// 後者も見るのは、使わなくなった図形が残ると、帰属表示（THIRD_PARTY_NOTICES.md）
// の一覧と食い違うからである。
func TestIconsExistInSprite(t *testing.T) {
	symbols := spriteSymbols(t)
	html := uiSource(t, "ui/index.html")
	js := uiSource(t, "ui/app.js")

	used := make(map[string]struct{})
	for _, m := range regexp.MustCompile(`<use href="#i-([a-z0-9-]+)"`).FindAllStringSubmatch(html, -1) {
		used[m[1]] = struct{}{}
	}
	for _, m := range regexp.MustCompile(`icon\("([a-z0-9-]+)"\)`).FindAllStringSubmatch(js, -1) {
		used[m[1]] = struct{}{}
	}
	// 畳みの見出しは foldTitle(summary, 名前, 文言) で組む。
	for _, m := range regexp.MustCompile(`foldTitle\([^,]+, "([a-z0-9-]+)"`).FindAllStringSubmatch(js, -1) {
		used[m[1]] = struct{}{}
	}
	// 保存の状態のアイコンは表（saveIcons）で引く。
	for _, m := range regexp.MustCompile(`(?:clean|saving|pending|failed|conflict): "([a-z0-9-]+)"`).FindAllStringSubmatch(js, -1) {
		used[m[1]] = struct{}{}
	}
	// 編集できない行と、それ以外の1言。三項で選んでいる。
	for _, m := range regexp.MustCompile(`icon\(entry\.editable \? "([a-z0-9-]+)" : "([a-z0-9-]+)"\)`).FindAllStringSubmatch(js, -1) {
		used[m[1]] = struct{}{}
		used[m[2]] = struct{}{}
	}

	for name := range used {
		if _, ok := symbols[name]; !ok {
			t.Errorf("アイコン %q が index.html に埋まっていない。何も描かれない", name)
		}
	}
	for name := range symbols {
		if _, ok := used[name]; !ok {
			t.Errorf("図形 %q は埋めてあるが使っていない。帰属表示の一覧と食い違う", name)
		}
	}
}

// TestIconsAreHiddenFromAssistiveTech は、アイコンが読み上げに出ないことを見る。
//
// 隣の文字が名前になる。svg そのものが読まれると、名前の無い図形が文字の前に
// 1つずつ挟まる。アイコンだけのボタン（左の列の開閉、閉じる）は app.js が
// 目録から aria-label を入れる。
func TestIconsAreHiddenFromAssistiveTech(t *testing.T) {
	html := uiSource(t, "ui/index.html")
	for _, m := range regexp.MustCompile(`<svg[^>]*>`).FindAllString(html, -1) {
		if !strings.Contains(m, `aria-hidden="true"`) {
			t.Errorf("aria-hidden の無い svg がある: %s", m)
		}
	}
	js := uiSource(t, "ui/app.js")
	start := strings.Index(js, "function icon(")
	if start < 0 {
		t.Fatal("app.js に icon() が無い")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("icon() の終わりが分からない")
	}
	if !strings.Contains(js[start:start+end], `setAttribute("aria-hidden", "true")`) {
		t.Error("icon() が aria-hidden を付けていない")
	}
	for _, id := range []string{"menu", "sidebar-close"} {
		if !strings.Contains(js, `el.`+lowerCamel(id)+`.setAttribute("aria-label"`) {
			t.Errorf("アイコンだけのボタン #%s に名前を入れていない", id)
		}
	}
}

// lowerCamel は id（menu、sidebar-close）を el の鍵（menu、sidebarClose）にする。
func lowerCamel(id string) string {
	parts := strings.Split(id, "-")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

// TestSpriteCarriesNoNamespaceURL は、埋めた図形に URL が残っていないことを見る。
//
// 元の SVG は xmlns とライセンスのコメントに URL を持つ。どちらも落としてある。
// TestAssetsHaveNoExternalReference も引っかけるが、ここでは理由を名指しする。
func TestSpriteCarriesNoNamespaceURL(t *testing.T) {
	html := uiSource(t, "ui/index.html")
	if strings.Contains(html, "xmlns=") {
		t.Error("index.html に xmlns 属性が残っている。HTML の中の svg に名前空間の宣言は要らない")
	}
	js := uiSource(t, "ui/app.js")
	if strings.Contains(js, "w3.org") {
		t.Error("app.js に名前空間の URL が書いてある。図形の svg から namespaceURI を読むこと")
	}
}
