package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ここは /api/export の試験。
//
// 書き出しは、翻訳者が選んだ場所へCSVを保存させる道である（issue #42）。
// 場所を決めるのはブラウザーで、待ち受けは中身を返すだけ。画面からパスを
// 受け取る経路は1つも作らない、というのがこの機能の要である。

// exportGet は書き出しを1回取りにいく。
func exportGet(t *testing.T, s *server, query string) *http.Response {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/export?"+query, true, nil)
	return rec.Result()
}

func TestExportPublishedIsThePublishForm(t *testing.T) {
	// publish が作るのと同じ形を返す。見出しのコメントが入るのがその印で、
	// いま編集しているファイル（見出しを持たない作業コピー）とは別物になる。
	s := newTestServer(t, Options{UILang: "ja"})
	rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form=published", true, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d、200 を期待: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "key,section,node,order,speaker,translation") {
		t.Errorf("公開ファイルの見出し行で始まっていない:\n%s", body)
	}
	if !strings.Contains(body, "# ") {
		t.Error("見出しのコメントが入っていない。publish の形になっていない")
	}
	if !strings.Contains(body, keyKept) {
		t.Error("訳のある行が出ていない")
	}
}

func TestExportWorkingIsTheFileAsItIs(t *testing.T) {
	// 組み立て直さない。「画面で直したものが、そのままの形で欲しい」という
	// 求めに別の形を返すのは答えになっていない。
	root := newTestRoot(t)
	s := newTestServer(t, Options{Root: root, UILang: "ja"})
	rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form=working", true, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d、200 を期待: %s", rec.Code, rec.Body.String())
	}
	want, err := os.ReadFile(filepath.Join(root, "Translations", "ja", "strings.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != string(want) {
		t.Errorf("ファイルの中身と違う\n got %q\nwant %q", rec.Body.String(), string(want))
	}
}

func TestExportSaysItIsAnAttachment(t *testing.T) {
	// これが無いと、ブラウザーは保存ではなく表示へ回す。
	s := newTestServer(t, Options{UILang: "ja"})
	for _, form := range []string{"published", "working"} {
		rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form="+form, true, nil)
		if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="strings.csv"` {
			t.Errorf("%s: Content-Disposition が %q", form, got)
		}
		if got := rec.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
			t.Errorf("%s: Content-Type が %q", form, got)
		}
	}
}

func TestExportRejectsUnknownForm(t *testing.T) {
	s := newTestServer(t, Options{UILang: "ja"})
	for _, query := range []string{"locale=ja", "locale=ja&form=", "locale=ja&form=nope", "locale=ja&form=PUBLISHED"} {
		rec := do(t, s, http.MethodGet, "/api/export?"+query, true, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: 状態コードが %d、400 を期待", query, rec.Code)
		}
	}
}

func TestExportRejectsUnknownLocale(t *testing.T) {
	s := newTestServer(t, Options{UILang: "ja"})

	rec := do(t, s, http.MethodGet, "/api/export?form=published", true, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("ロケールを省いたのに %d", rec.Code)
	}

	// 当たらない名前は 404。誤りの文面に要求の値を書き戻さない。
	rec = do(t, s, http.MethodGet, "/api/export?locale=nope&form=published", true, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("状態コードが %d、404 を期待", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "nope") {
		t.Errorf("誤りの文面に要求の値が出ている: %q", rec.Body.String())
	}
}

func TestExportNeedsTheCookie(t *testing.T) {
	// ほかの経路と同じ守りを通ること。通らないと、この待ち受けが開いている
	// あいだ、ブラウザーの中の別の頁からCSVを丸ごと取れる道が1つできる。
	s := newTestServer(t, Options{UILang: "ja"})
	rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form=published", false, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("Cookie 無しで %d。404 を期待", rec.Code)
	}
}

func TestExportRefusesToLoseTranslations(t *testing.T) {
	// いちばん大事な性質。publish が「書くと訳が失われる」と止める中身を、
	// 書き出しからは出せてしまうと、守りは publish を回した人にしか効かない。
	// 翻訳者は落としたファイルを自分の手でリポジトリへ写せる。
	root := newTestRoot(t)
	// 作業コピーを、公開ファイルより1行少ない形で置く。
	working := filepath.Join(root, "Translations", "_discovered", "ja.working.csv")
	if err := os.MkdirAll(filepath.Dir(working), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"key,section,node,order,speaker,translation",
		keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？",
		"",
	}, "\n")
	if err := os.WriteFile(working, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, Options{Root: root, UILang: "ja"})
	rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form=published", true, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("状態コードが %d、409 を期待:\n%s", rec.Code, rec.Body.String())
	}
	// 失われる行の訳そのものは画面へ出さない。内訳は dwloc publish が出す。
	if strings.Contains(rec.Body.String(), jaVanished) {
		t.Errorf("誤りの文面に行の中身が出ている: %q", rec.Body.String())
	}

	// 同じ状態でも、いま編集しているファイルはそのまま出せること。
	// 止めるのは「publish の形にすると失われる」ときだけで、書き出し全部ではない。
	rec = do(t, s, http.MethodGet, "/api/export?locale=ja&form=working", true, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("編集中のファイルの書き出しまで止めている: %d", rec.Code)
	}
}

func TestExportNameIsSafeForTheHeader(t *testing.T) {
	// 名前はこちら側のパスから作るが、ロケール名はディレクトリ名から来る。
	// 引用符や改行を入れたディレクトリを作れば、ヘッダーを割れる。作れるのは
	// この待ち受けを動かしている本人だが、守りの範囲を「誰が作ったか」で決めない。
	for _, tc := range []struct {
		path string
		want string
	}{
		{filepath.Join("a", "strings.csv"), "strings.csv"},
		{filepath.Join("a", "ja.working.csv"), "ja.working.csv"},
		{filepath.Join("a", "pt-BR.working.csv"), "pt-BR.working.csv"},
		{filepath.Join("a", `bad".csv`), "strings.csv"},
		{filepath.Join("a", "bad\n.csv"), "strings.csv"},
		{filepath.Join("a", "日本語.csv"), "strings.csv"},
		{filepath.Join("a", ".csv"), "strings.csv"},
		{filepath.Join("a", "..csv"), "strings.csv"},
		{"", "strings.csv"},
	} {
		if got := exportName(tc.path); got != tc.want {
			t.Errorf("exportName(%q) が %q、%q を期待", tc.path, got, tc.want)
		}
	}
}

func TestExportIsRecorded(t *testing.T) {
	// 記録には形と大きさだけを残す。中身は残さない。
	var file strings.Builder
	s := newTestServer(t, Options{Record: &file, UILang: "ja"})
	do(t, s, http.MethodGet, "/api/export?locale=ja&form=published", true, nil)

	text := file.String()
	for _, want := range []string{"GET /api/export 200", "locale=ja", "form=published", "bytes="} {
		if !strings.Contains(text, want) {
			t.Errorf("記録に %q が無い:\n%s", want, text)
		}
	}
	for _, secret := range []string{jaVanished, "もしもし", "こんにちは", "設定"} {
		if strings.Contains(text, secret) {
			t.Errorf("記録に行の中身が出ている: %q\n%s", secret, text)
		}
	}
}
