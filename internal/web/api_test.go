package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// decode は応答を読む。
func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("応答を読めません: %v\n%s", err, body)
	}
	return v
}

func TestBootstrap(t *testing.T) {
	s := newTestServer(t, Options{Locale: "ja", UILang: "ja"})
	rec := do(t, s, http.MethodGet, "/api/bootstrap", true, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d", rec.Code)
	}
	got := decode[bootstrapResponse](t, rec.Body.Bytes())

	if got.Selected != "ja" {
		t.Errorf("Selected が %q", got.Selected)
	}
	if strings.Join(got.Locales, ",") != "he,ja" {
		// publish.DiscoverTargets はディレクトリ名順で返す。
		t.Errorf("Locales が %v", got.Locales)
	}
	if !got.ReadOnly {
		t.Error("ReadOnly が false。この段は読み取り専用")
	}
	if got.UI.Lang != "ja" || got.UI.Dir != "ltr" {
		t.Errorf("UI が %+v", got.UI)
	}
	if got.UI.Messages["ui.reload"] == "" {
		t.Error("目録が空")
	}
}

func TestBootstrapLanguageChoice(t *testing.T) {
	// 選び方は --ui-lang > Accept-Language > en。
	cases := []struct {
		name   string
		uiLang string
		accept string
		want   string
	}{
		{"指定がいちばん強い", "en", "ja", "en"},
		{"指定がいちばん強い（逆）", "ja", "en-US", "ja"},
		{"Accept-Language を見る", "", "ja-JP,ja;q=0.9", "ja"},
		{"qの大きい順", "", "de;q=0.2,ja;q=0.8", "ja"},
		{"当たらなければ en", "", "de,fr", "en"},
		{"無ければ en", "", "", "en"},
		{"q=0 は当たりにしない", "", "ja;q=0,de", "en"},
	}
	for _, tc := range cases {
		s := newTestServer(t, Options{UILang: tc.uiLang})
		rec := do(t, s, http.MethodGet, "/api/bootstrap", true,
			map[string]string{"Accept-Language": tc.accept})
		got := decode[bootstrapResponse](t, rec.Body.Bytes())
		if got.UI.Lang != tc.want {
			t.Errorf("%s: 言語が %q、%q を期待", tc.name, got.UI.Lang, tc.want)
		}
	}
}

func TestLinesLocaleIsCheckedAgainstTheList(t *testing.T) {
	// クライアントが書けるのはロケール名だけで、それも起動時に列挙した一覧との
	// 完全一致を通る。パスの組み立てはこちら側でしか行わない。
	s := newTestServer(t, Options{})
	bad := []string{
		"../../etc/passwd",
		"..\\..\\windows\\win.ini",
		"ja/../he",
		"_discovered",
		"ja ",
		"JA",
		"ignore.txt",
		"/etc/passwd",
		"ja\x00",
	}
	for _, locale := range bad {
		rec := do(t, s, http.MethodGet, "/api/lines?locale="+urlEscape(locale), true, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("locale=%q: 状態コードが %d、404 を期待", locale, rec.Code)
		}
		if body := rec.Body.String(); strings.Contains(body, locale) {
			t.Errorf("locale=%q: 応答に要求の値が書き戻されている: %q", locale, body)
		}
	}

	rec := do(t, s, http.MethodGet, "/api/lines", true, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("locale 無し: 状態コードが %d、400 を期待", rec.Code)
	}
}

// urlEscape は問い合わせ文字列に載せる値を包む。
func urlEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
			b.WriteByte(c)
		default:
			b.WriteString("%")
			const hex = "0123456789ABCDEF"
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}

// getLines は1ロケール分の応答を取る。
func getLines(t *testing.T, s *server, locale string) linesResponse {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/lines?locale="+locale, true, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d", rec.Code)
	}
	return decode[linesResponse](t, rec.Body.Bytes())
}

func TestLinesFollowTheFile(t *testing.T) {
	s := newTestServer(t, Options{UILang: "ja"})
	got := getLines(t, s, "ja")

	// 列名はファイルのヘッダーそのもの。訳さずに出す。
	if strings.Join(got.Columns, ",") != "key,section,node,order,speaker,translation" {
		t.Errorf("Columns が %v", got.Columns)
	}
	if got.SourceColumn {
		t.Error("公開ファイルに source_en 列は無い")
	}

	// 並びはファイルの並び。見出しはファイルのコメント行そのまま。
	var shape []string
	for _, line := range got.Lines {
		if line.Kind == lineKindHeading {
			shape = append(shape, line.Heading+":"+line.Text)
			continue
		}
		shape = append(shape, "data:"+line.Key)
	}
	want := []string{
		"section:# ===== Level 1: Ryan (Sunny) =====",
		"node:# --- intro: Ryan_1_intro ---",
		"data:" + keyKept,
		"data:" + keyKept2,
		"data:" + keyVanished,
		"section:# ===== UI =====",
		"data:" + keyUI,
	}
	if strings.Join(shape, "\n") != strings.Join(want, "\n") {
		t.Errorf("並びが違う:\n得た:\n%s\n期待:\n%s",
			strings.Join(shape, "\n"), strings.Join(want, "\n"))
	}

	// 物理行番号はファイルのまま。空行もヘッダー行も番号を消費している。
	// 先頭が 3 なのは、1行目がヘッダー、2行目が空行だから。
	if got.Lines[0].Number != 3 {
		t.Errorf("最初の行の番号が %d、3 を期待", got.Lines[0].Number)
	}
	for _, line := range got.Lines {
		if line.Number <= 0 {
			t.Errorf("行番号が %d", line.Number)
		}
	}

	// 行数は待ち受けが数えて渡す。画面に数えさせない。
	if got.Rows != 4 {
		t.Errorf("Rows が %d、4 を期待", got.Rows)
	}
}

func TestBadgesComeFromDiff(t *testing.T) {
	s := newTestServer(t, Options{UILang: "ja"})
	got := getLines(t, s, "ja")

	badges := map[string][]string{}
	for _, line := range got.Lines {
		if line.Kind != lineKindData {
			continue
		}
		for _, b := range line.Badges {
			badges[line.Key] = append(badges[line.Key], b.Category+"/"+b.Status)
		}
	}

	// 再生順にある行には何も付かない。
	if len(badges[keyKept]) != 0 {
		t.Errorf("%s に %v が付いた", keyKept, badges[keyKept])
	}
	// 再生順に無く section が 'UI' でない行は「台本から消えた行」（要確認）。
	if strings.Join(badges[keyVanished], ",") != "vanished/review" {
		t.Errorf("%s のバッジが %v", keyVanished, badges[keyVanished])
	}
	// 再生順に無く section も speaker も 'UI' の行は「由来を判定できない行」（参考）。
	if strings.Join(badges[keyUI], ",") != "unknown_origin/info" {
		t.Errorf("%s のバッジが %v", keyUI, badges[keyUI])
	}

	// 他のロケールにあって無い行は、その無いほうのロケールに付く。
	he := getLines(t, s, "he")
	found := false
	for _, line := range he.Lines {
		for _, b := range line.Badges {
			if b.Category == "locale_gap" {
				found = true
			}
		}
	}
	if found {
		t.Error("he の公開ファイルに無い行が、he の一覧に出るはずがない")
	}
}

func TestCountsSayNotJudgedInsteadOfZero(t *testing.T) {
	// 判定できていないカテゴリを 0 件と書くと「もう何も残っていない」と読まれる。
	s := newTestServer(t, Options{UILang: "ja"})
	got := getLines(t, s, "ja")

	byID := map[string]countView{}
	for _, c := range got.Counts {
		byID[c.Category] = c
	}

	// 作業コピーが無いので、未翻訳と「publish で捨てられる行」は判定できない。
	for _, id := range []string{"untranslated", "dropped"} {
		c, ok := byID[id]
		if !ok {
			t.Fatalf("%s が件数に無い", id)
		}
		if c.Judged {
			t.Errorf("%s が判定済みになっている", id)
		}
		if c.Reason == "" {
			t.Errorf("%s に理由が無い", id)
		}
	}
	// git の履歴が無いので、引き継ぎ候補も判定できない。
	if c := byID["carryover"]; c.Judged || c.Reason == "" {
		t.Errorf("carryover が %+v", c)
	}
	// 判定できるものは数が出る。
	if c := byID["vanished"]; !c.Judged || c.Count != 1 {
		t.Errorf("vanished が %+v", c)
	}

	// 並びは 要作業 → 要確認 → 参考。internal/diff の text 出力と同じ。
	var order []string
	for _, c := range got.Counts {
		if len(order) == 0 || order[len(order)-1] != c.Status {
			order = append(order, c.Status)
		}
	}
	if strings.Join(order, ",") != "todo,review,info" {
		t.Errorf("並びが %v", order)
	}
}

func TestNotesSayWhatWasRead(t *testing.T) {
	s := newTestServer(t, Options{UILang: "ja"})
	got := getLines(t, s, "ja")
	joined := strings.Join(got.Notes, "\n")
	if !strings.Contains(joined, "作業コピー") {
		t.Errorf("作業コピーのことが書かれていない: %q", joined)
	}
	if !strings.Contains(joined, "引き継ぎ") {
		t.Errorf("引き継ぎ候補の保留が書かれていない: %q", joined)
	}
	// パスはルートからの相対。絶対パスには利用者名が入ることがある。
	if strings.Contains(joined, ":\\") || strings.Contains(joined, "/tmp/") {
		t.Errorf("絶対パスが出ている: %q", joined)
	}
	if !strings.HasPrefix(got.Path, "Translations/") {
		t.Errorf("Path が %q", got.Path)
	}
}

func TestVerboseLogHasNoRowContent(t *testing.T) {
	// いちばん大事な性質。原文と訳は既定でも --verbose でも記録に出さない。
	var log bytes.Buffer
	s := newTestServer(t, Options{Verbose: true, Stderr: &log, UILang: "ja"})
	do(t, s, http.MethodGet, "/?"+tokenParam+"="+s.token, false, nil)
	do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	do(t, s, http.MethodGet, "/api/lines?locale=he", true, nil)

	text := log.String()
	if text == "" {
		t.Fatal("--verbose なのに記録が空")
	}
	for _, secret := range []string{jaVanished, "もしもし", "こんにちは", "שלום", "設定", keyVanished, keyKept} {
		if strings.Contains(text, secret) {
			t.Errorf("記録に行の中身が出ている: %q\n%s", secret, text)
		}
	}
	// トークンも出さない。最初の1回の URL に載っているので、問い合わせ文字列ごと落とす。
	if strings.Contains(text, s.token) {
		t.Errorf("記録にトークンが出ている:\n%s", text)
	}
	// 出てよいものは出ている。
	for _, want := range []string{"GET /api/lines 200", "locale=ja", "lines="} {
		if !strings.Contains(text, want) {
			t.Errorf("記録に %q が無い:\n%s", want, text)
		}
	}
}

func TestQuietByDefault(t *testing.T) {
	var log bytes.Buffer
	s := newTestServer(t, Options{Stderr: &log})
	do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	if log.Len() != 0 {
		t.Errorf("既定で記録が出ている:\n%s", log.String())
	}
}

func TestErrorBodiesHaveNoRowContent(t *testing.T) {
	s := newTestServer(t, Options{UILang: "ja"})
	for _, target := range []string{"/api/lines", "/api/lines?locale=nope", "/nowhere"} {
		rec := do(t, s, http.MethodGet, target, true, nil)
		body := rec.Body.String()
		for _, secret := range []string{jaVanished, "もしもし", "שלום"} {
			if strings.Contains(body, secret) {
				t.Errorf("%s の応答に行の中身が出ている: %q", target, body)
			}
		}
	}
}
