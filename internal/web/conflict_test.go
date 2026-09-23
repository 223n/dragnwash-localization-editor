package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// TestSaveRejectsMovedRow は、行番号の指す行が思っているキーの行でないときに
// 書かないことを確かめる。
//
// 409 を受けた画面は、読み直した内容に自分の編集を載せ直す。そのあいだに
// よそが行を足したり消したりしていると、同じ行番号が別のキーの行を指す。
// 書いてしまうと、訳が別の行へ入り、その行にもとからあった訳が消える。
// どちらも黙って起きるので、翻訳者は気づけない。
func TestSaveRejectsMovedRow(t *testing.T) {
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	path := inputPath(t, s, "ja")
	before := readFile(t, path)

	lines := getLines(t, s, "ja")
	// 6行目にあるのは srcBye のキー。srcHello のキーだと思って送る。
	rec := save(t, s, "ja", lines.Version, rowEdit{
		Line:        6,
		Key:         key.For(srcHello),
		Translation: jaTyped,
	})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("状態コードが %d、422 を期待\n%s", rec.Code, rec.Body.String())
	}
	got := decode[errorResponse](t, rec.Body.Bytes())
	if len(got.Results) != 1 {
		t.Fatalf("結果が %+v", got.Results)
	}
	if got.Results[0].Saved {
		t.Error("保存できたことになっている")
	}
	if got.Results[0].Error == "" {
		t.Error("理由を返していない")
	}
	if after := readFile(t, path); after != before {
		t.Errorf("ファイルが変わった\n前: %q\n後: %q", before, after)
	}
}

// TestSaveAcceptsMatchingKey は、キーが合っていれば普通に書けることを確かめる。
//
// 照合そのものが「常に断る」形になっていないことを見る。断る側だけを試すと、
// 何も保存できなくなった状態でも試験は通る。
func TestSaveAcceptsMatchingKey(t *testing.T) {
	s := newTestServer(t, Options{Root: newEditRoot(t)})

	lines := getLines(t, s, "ja")
	rec := save(t, s, "ja", lines.Version, rowEdit{
		Line:        6,
		Key:         key.For(srcBye),
		Translation: jaTyped,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	got := decode[rowsResponse](t, rec.Body.Bytes())
	if len(got.Results) != 1 || !got.Results[0].Saved {
		t.Fatalf("結果が %+v", got.Results)
	}
	if body := readFile(t, inputPath(t, s, "ja")); !strings.Contains(body, jaTyped) {
		t.Error("訳がファイルに入っていない")
	}
}

// TestSaveMixesMovedAndGoodRows は、ずれた行が混ざっていても残りは書くことを
// 確かめる。1行の食い違いで残り全部を巻き添えにすると、直せない1行のせいで
// 他の訳が永久に保存できなくなる。
func TestSaveMixesMovedAndGoodRows(t *testing.T) {
	s := newTestServer(t, Options{Root: newEditRoot(t)})

	lines := getLines(t, s, "ja")
	rec := save(t, s, "ja", lines.Version,
		rowEdit{Line: 5, Key: "0000000000000000", Translation: "ずれている"},
		rowEdit{Line: 6, Key: key.For(srcBye), Translation: jaTyped},
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	got := decode[rowsResponse](t, rec.Body.Bytes())
	if len(got.Results) != 2 {
		t.Fatalf("結果が %+v", got.Results)
	}
	if got.Results[0].Saved {
		t.Error("ずれた行を保存してしまっている")
	}
	if !got.Results[1].Saved {
		t.Error("合っている行を保存していない")
	}
	body := readFile(t, inputPath(t, s, "ja"))
	if strings.Contains(body, "ずれている") {
		t.Error("ずれた行の訳がファイルに入っている")
	}
	if !strings.Contains(body, jaTyped) {
		t.Error("合っている行の訳がファイルに入っていない")
	}
}

// TestUnknownLocaleMessageHasNoPlaceholder は、知らないロケールへの誤りの文面に
// 置き換えられていない {locale} が残らないことを確かめる。
//
// 残ると、翻訳者の画面に鍵がそのまま出る。GET /api/lines は潰しているので、
// POST だけが残っていた。
func TestUnknownLocaleMessageHasNoPlaceholder(t *testing.T) {
	s := newTestServer(t, Options{Root: newEditRoot(t)})

	for _, locale := range []string{"../he", "ja/../he", "JA", "_discovered", "xx"} {
		rec := save(t, s, locale, strings.Repeat("0", 64), rowEdit{Line: 6, Translation: jaTyped})
		if rec.Code != http.StatusNotFound {
			t.Errorf("%q: 状態コードが %d、404 を期待", locale, rec.Code)
		}
		if body := rec.Body.String(); strings.Contains(body, "{locale}") {
			t.Errorf("%q: 応答に置き換えていない鍵が残っている: %s", locale, body)
		}
	}
}

// TestSessionCookieIsPerPort は、Cookie の名前が待ち受けごとに違うことを確かめる。
//
// Cookie はポートで分かれない。名前を固定にすると、dwloc edit をもう1つ動かした
// だけで後から置いた Cookie が先のトークンを上書きし、先に開いていた画面は
// すべての要求が 404 になる。保存が入ってからは、打った訳をファイルへ入れる
// 手立てが無くなる、という結果になる。
func TestSessionCookieIsPerPort(t *testing.T) {
	root := newEditRoot(t)
	a := newTestServer(t, Options{Root: root})
	b := newTestServer(t, Options{Root: root})
	b.usePort("18999")

	if a.cookieName == b.cookieName {
		t.Fatalf("2つの待ち受けが同じ Cookie の名前を使っている: %q", a.cookieName)
	}
	if !strings.Contains(a.cookieName, testPort) {
		t.Errorf("Cookie の名前にポートが入っていない: %q", a.cookieName)
	}

	ask := func(c *http.Cookie) int {
		req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
		req.Host = "127.0.0.1:" + testPort
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.AddCookie(c)
		rec := httptest.NewRecorder()
		a.handler().ServeHTTP(rec, req)
		return rec.Code
	}

	// もう片方の Cookie では通らないこと。ブラウザーは両方を送るので、
	// 名前が同じだと後から置いたほうが前の値を消す。
	if code := ask(&http.Cookie{Name: b.cookieName, Value: b.token}); code != http.StatusNotFound {
		t.Errorf("別の待ち受けの Cookie で %d が返った、404 を期待", code)
	}
	// 自分の Cookie では通ること。
	if code := ask(&http.Cookie{Name: a.cookieName, Value: a.token}); code != http.StatusOK {
		t.Errorf("自分の Cookie で %d が返った、200 を期待", code)
	}
}

// TestSaveToMissingRowSaysTheRowIsMissing は、キーを添えて無い行番号へ送ったときに
// 「行が無い」と言うことを確かめる。
//
// キーの照合は、行が無ければ素通しして internal/edit に断らせる。照合の側で
// 「行がずれた」と言うと、読み直せば直る話に見えてしまい、画面はいつまでも
// 載せ直しを続ける。行が無いことの理由は1か所（internal/edit）でだけ作る。
func TestSaveToMissingRowSaysTheRowIsMissing(t *testing.T) {
	s := newTestServer(t, Options{Root: newEditRoot(t), UILang: "ja"})
	ja := s.cat.lookup("ja")
	path := inputPath(t, s, "ja")
	before := readFile(t, path)

	lines := getLines(t, s, "ja")
	rec := save(t, s, "ja", lines.Version, rowEdit{Line: 999, Key: key.For(srcBye), Translation: jaTyped})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("状態コードが %d、422 を期待\n%s", rec.Code, rec.Body.String())
	}
	got := decode[errorResponse](t, rec.Body.Bytes())
	if len(got.Results) != 1 {
		t.Fatalf("結果が %+v", got.Results)
	}
	why := reason.New(reason.EditNoSuchLine, "そんな行番号は無い")
	want := s.cat.T(ja, "error.not_editable", "line", "999", "reason", s.reasonText(ja, why))
	if got.Results[0].Error != want {
		t.Errorf("理由が %q、%q を期待", got.Results[0].Error, want)
	}
	if got.Results[0].Error == s.cat.T(ja, "error.row_moved") {
		t.Error("無い行を「ずれた」と言っている")
	}
	if after := readFile(t, path); after != before {
		t.Errorf("ファイルが変わった\n前: %q\n後: %q", before, after)
	}
}
