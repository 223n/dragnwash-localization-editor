package web

import (
	"regexp"
	"strings"
	"testing"
)

// この束は、色が :root の変数（トークン）にだけ書かれていて、暗い配色の切り替えが
// その変数を差し替えるだけで済むことを見る。
//
// 規則の外に色を1つ書くと、その場所だけ明るい配色のまま暗い画面に残る
// （白い地に薄い赤の行、など）。暗い配色は OS の設定（prefers-color-scheme）に
// 従うだけで、画面の中に切り替えは置かない。この道具は画面の状態をどこにも
// 残さないと決めてあり、切り替えを置くと覚える場所が要る。

// colorLiteral は色の直書きに当たる。#rrggbb、rgb()/rgba()、関数記法
// （hsl()、oklch()、color-mix() など）、名前の色（white、black など）。
//
// 名前の色は宣言の値の側にだけ当てる。プロパティ名（white-space）に当てると誤る。
// transparent と currentColor は色ではなく「色を持たない」「親の色」の指定なので
// 数えない。
var colorLiteral = regexp.MustCompile(
	`#[0-9a-fA-F]{3,8}\b|\b(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch|color|color-mix)\(` +
		`|(?i)\b(?:white|black|red|green|blue|gray|grey|yellow|orange|silver|navy|teal|maroon|purple)\b`)

// TestColorsLiveInTokens は、色の直書きが :root の変数の定義行にしか無いことを見る。
func TestColorsLiveInTokens(t *testing.T) {
	// 注記の中の値（計測の記録や、以前の値の言及）は数えない。この CSS の注記は
	// 行頭に * を付けない書き方なので、行ごとではなく注記ごと外す。
	css := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(uiSource(t, "ui/app.css"), "")

	// 変数の定義を許すのは :root の中（明るい側と暗い側）だけ。ほかの場所で
	// --x: #fff と書くと、暗い配色が差し替えられない色になる。
	depth := 0
	inRoot := false
	for _, line := range strings.Split(css, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ":root {") {
			inRoot = true
		}
		value := trimmed
		if at := strings.Index(trimmed, ":"); at >= 0 && !strings.HasPrefix(trimmed, ":") {
			value = trimmed[at+1:]
		}
		if colorLiteral.MatchString(value) && !(inRoot && strings.HasPrefix(trimmed, "--")) {
			t.Errorf("app.css に色の直書きがある。:root の変数に移すこと: %s", trimmed)
		}
		depth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
		if inRoot && strings.Contains(trimmed, "}") {
			inRoot = false
		}
	}
	if depth != 0 {
		t.Errorf("app.css の括弧の対応が取れていない（%d）", depth)
	}
}

// TestDarkSchemeRedefinesEveryToken は、暗い配色が明るい配色の色の変数を1つ残らず
// 差し替えていることを見る。
//
// 1つでも抜けると、その色だけ明るい配色の値が暗い画面に残る。抜けは目で見ても
// 気づきにくい（薄い地色の差など）ので、字面で数える。
func TestDarkSchemeRedefinesEveryToken(t *testing.T) {
	css := uiSource(t, "ui/app.css")

	dark := strings.Index(css, "@media (prefers-color-scheme: dark)")
	if dark < 0 {
		t.Fatal("app.css に暗い配色（prefers-color-scheme: dark）が無い")
	}
	if !strings.Contains(css, "color-scheme: light dark") {
		t.Error("color-scheme が無い。選択欄やスクロールバーが明るいまま残る")
	}

	light := between(t, css, ":root {", "\n}")
	darkBlock := between(t, css[dark:], ":root {", "\n}")

	token := regexp.MustCompile(`(?m)^\s*(--[a-z0-9-]+):\s*(.+);`)
	lightTokens := make(map[string]string)
	for _, m := range token.FindAllStringSubmatch(light, -1) {
		if colorLiteral.MatchString(m[2]) {
			lightTokens[m[1]] = m[2]
		}
	}
	darkTokens := make(map[string]string)
	for _, m := range token.FindAllStringSubmatch(darkBlock, -1) {
		darkTokens[m[1]] = m[2]
	}
	if len(lightTokens) == 0 {
		t.Fatal(":root に色の変数が1つも無い")
	}
	// 同じ値でよい変数（--on-primary など）もあるので、値の違いまでは見ない。
	// 見るのは「暗い配色の側にも定義があること」だけである。
	for name, value := range lightTokens {
		if _, ok := darkTokens[name]; !ok {
			t.Errorf("暗い配色に %s が無い。明るい配色の %s が暗い画面に残る", name, value)
		}
	}
	for name := range darkTokens {
		if _, ok := lightTokens[name]; !ok {
			t.Errorf("暗い配色にだけ %s がある。明るい配色にも定義すること", name)
		}
	}
}
