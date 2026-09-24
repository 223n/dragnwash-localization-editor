package web

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testPort はテストで使うポート番号。実際には束ねないので、Host の照合が
// 通るかどうかだけに使う。
const testPort = "54321"

// 固定の値。テストの意図をその場で読めるように、ここへ集めておく。
const (
	// keyKept は再生順にも公開ファイルにもあるキー。
	keyKept = "0da72197e898ebe1"
	// keyKept2 は同上（2行目）。ja にだけあり、he には無い。
	keyKept2 = "334d016f755cd6dc"
	// keyVanished は公開ファイルにあるが再生順に無く、section が 'UI' でない。
	// internal/diff は「台本から消えた行」（要確認）と見る。
	keyVanished = "aaaaaaaaaaaaaaaa"
	// keyUI は公開ファイルにあるが再生順に無く、section も speaker も 'UI'。
	// internal/diff は「由来を判定できない行」（参考）と見る。
	keyUI = "ffffffffffffffff"

	// jaVanished は keyVanished の訳。記録に出ていないことを確かめるために使う。
	jaVanished = "きえたぎょうのやく"
)

// newTestRoot は小さな翻訳リポジトリを作る。
//
// 実データを読ませないのは、テストが遅くなるからではなく、失敗したときに
// 「どの行のせいか」を目で追えるようにするため。実データでの確かめは
// 別に（手で起動して）行う。
func newTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("data/script_order.csv", strings.Join([]string{
		"section,phase,node,order,line_id,key,speaker,condition",
		"L01 Ryan,intro,Ryan_1_intro,1,line:aaaaaaaa," + keyKept + ",Ryan,",
		"L01 Ryan,intro,Ryan_1_intro,2,line:bbbbbbbb," + keyKept2 + ",Kobold,",
		"",
	}, "\n"))

	write("Translations/ja/strings.csv", strings.Join([]string{
		"key,section,node,order,speaker,translation",
		"",
		"# ===== Level 1: Ryan (Sunny) =====",
		"# --- intro: Ryan_1_intro ---",
		keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？",
		keyKept2 + ",L01 Ryan,Ryan_1_intro,2,Kobold,こんにちは！",
		keyVanished + ",L01 Ryan,Ryan_1_intro,3,Ryan," + jaVanished,
		"",
		"# ===== UI =====",
		keyUI + ",UI,,,UI,設定",
		"",
	}, "\n"))

	write("Translations/he/strings.csv", strings.Join([]string{
		"key,section,node,order,speaker,translation",
		"",
		"# --- intro: Ryan_1_intro ---",
		keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,שלום",
		"",
	}, "\n"))

	return root
}

// newTestServer は待ち受けを組み立てる。実際には束ねない。
func newTestServer(t *testing.T, opt Options) *server {
	t.Helper()
	if opt.Root == "" {
		opt.Root = newTestRoot(t)
	}
	if opt.Stdout == nil {
		opt.Stdout = io.Discard
	}
	if opt.Stderr == nil {
		opt.Stderr = io.Discard
	}
	opt.openBrowser = func(string) error { return nil }

	s, err := newServer(opt)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	// run が実際のポートから作る照合表と Cookie の名前を、テストでは決め打ちで入れる。
	s.usePort(testPort)
	return s
}

// do は1回の要求を送る。cookie が true なら通ったあとの状態を作る。
func do(t *testing.T, s *server, method, target string, cookie bool, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Host = "127.0.0.1:" + testPort
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
			continue
		}
		req.Header.Set(k, v)
	}
	if cookie {
		req.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
	}
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	return rec
}

func TestCookieMissingIs404(t *testing.T) {
	// 401 ではなく 404 を返す。401 は「そこに何かがある」と教えてしまう。
	s := newTestServer(t, Options{})
	for _, target := range []string{"/", "/app.js", "/app.css", "/api/bootstrap", "/api/lines?locale=ja"} {
		rec := do(t, s, http.MethodGet, target, false, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: 状態コードが %d、404 を期待", target, rec.Code)
		}
		if body := rec.Body.String(); strings.Contains(body, "token") || strings.Contains(body, "cookie") {
			t.Errorf("%s: 応答が理由を漏らしている: %q", target, body)
		}
	}
}

func TestTokenIsMovedToCookie(t *testing.T) {
	s := newTestServer(t, Options{})
	rec := do(t, s, http.MethodGet, "/?"+tokenParam+"="+s.token, false, nil)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("状態コードが %d、303 を期待", rec.Code)
	}
	// 素の / へ送り返す。トークンをアドレス欄にも履歴にも残さないため。
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("Location が %q、\"/\" を期待", loc)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Cookie が %d 個、1個を期待", len(cookies))
	}
	c := cookies[0]
	switch {
	case c.Name != s.cookieName:
		t.Errorf("Cookie の名前が %q", c.Name)
	case c.Value != s.token:
		t.Errorf("Cookie の値がトークンと違う")
	case !c.HttpOnly:
		t.Errorf("HttpOnly が付いていない")
	case c.SameSite != http.SameSiteStrictMode:
		t.Errorf("SameSite が Strict でない: %v", c.SameSite)
	case c.Path != "/":
		t.Errorf("Path が %q", c.Path)
	}
}

func TestWrongTokenIs404(t *testing.T) {
	s := newTestServer(t, Options{})
	for _, token := range []string{"", "x", s.token + "x", s.token[:len(s.token)-1]} {
		rec := do(t, s, http.MethodGet, "/?"+tokenParam+"="+token, false, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("トークン %q: 状態コードが %d、404 を期待", token, rec.Code)
		}
	}
}

func TestTokenOnlyWorksOnRoot(t *testing.T) {
	// 資産や API にトークンを載せても通らない。受け取り口は1つだけにする。
	s := newTestServer(t, Options{})
	for _, target := range []string{"/app.js", "/api/bootstrap", "/api/lines"} {
		rec := do(t, s, http.MethodGet, target+"?"+tokenParam+"="+s.token, false, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: 状態コードが %d、404 を期待", target, rec.Code)
		}
	}
}

func TestHostMustMatchExactly(t *testing.T) {
	s := newTestServer(t, Options{})
	cases := []struct {
		host string
		want int
	}{
		{"127.0.0.1:" + testPort, http.StatusOK},
		{"localhost:" + testPort, http.StatusOK},
		{"[::1]:" + testPort, http.StatusOK},
		// DNS リバインディング。名前が 127.0.0.1 を指していても Host は名前のまま。
		{"dwloc.example:" + testPort, http.StatusNotFound},
		{"127.0.0.1.nip.io:" + testPort, http.StatusNotFound},
		// ポートが違えば別の待ち受け宛て。
		{"127.0.0.1:1", http.StatusNotFound},
		{"127.0.0.1", http.StatusNotFound},
		{"", http.StatusNotFound},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
		req.Host = tc.host
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("Host %q: 状態コードが %d、%d を期待", tc.host, rec.Code, tc.want)
		}
	}
}

func TestSecFetchSite(t *testing.T) {
	s := newTestServer(t, Options{})
	cases := []struct {
		name   string
		site   string
		target string
		want   int
	}{
		// 頁の中の fetch。
		{"同一生成元のAPI", "same-origin", "/api/bootstrap", http.StatusOK},
		// アドレス欄からの移動と、ブラウザーの自動起動。最初の1回がこれになる。
		{"アドレス欄からの移動", "none", "/", http.StatusOK},
		// none を全部通すと、他の経路も素通りしてしまう。
		{"noneのAPI", "none", "/api/bootstrap", http.StatusNotFound},
		{"他サイトからの移動", "cross-site", "/", http.StatusNotFound},
		{"他サイトからのAPI", "cross-site", "/api/bootstrap", http.StatusNotFound},
		{"同一サイト", "same-site", "/api/bootstrap", http.StatusNotFound},
		// 送ってこないブラウザーもある。Host とトークンで守る。
		{"ヘッダー無し", "", "/api/bootstrap", http.StatusOK},
	}
	for _, tc := range cases {
		rec := do(t, s, http.MethodGet, tc.target, true, map[string]string{"Sec-Fetch-Site": tc.site})
		if rec.Code != tc.want {
			t.Errorf("%s: 状態コードが %d、%d を期待", tc.name, rec.Code, tc.want)
		}
	}
}

func TestNoCORSHeaders(t *testing.T) {
	s := newTestServer(t, Options{})
	rec := do(t, s, http.MethodGet, "/api/bootstrap", true, nil)
	for _, name := range []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Credentials",
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
	} {
		if v := rec.Header().Get(name); v != "" {
			t.Errorf("%s が %q。CORS のヘッダーは1つも返さない", name, v)
		}
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	s := newTestServer(t, Options{})
	want := map[string]string{
		"Content-Security-Policy":      contentSecurityPolicy,
		"X-Content-Type-Options":       "nosniff",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Resource-Policy": "same-origin",
		// ディスクキャッシュに原文と訳を残さない。
		"Cache-Control": "no-store",
	}
	cases := []struct {
		name   string
		cookie bool
		target string
	}{
		{"画面", true, "/"},
		{"スクリプト", true, "/app.js"},
		{"様式", true, "/app.css"},
		{"初期値", true, "/api/bootstrap"},
		{"行", true, "/api/lines?locale=ja"},
		{"404", false, "/api/lines?locale=ja"},
		{"知らない経路", true, "/nowhere"},
	}
	for _, tc := range cases {
		rec := do(t, s, http.MethodGet, tc.target, tc.cookie, nil)
		for name, value := range want {
			if got := rec.Header().Get(name); got != value {
				t.Errorf("%s: %s が %q、%q を期待", tc.name, name, got, value)
			}
		}
	}
}

func TestCSPContents(t *testing.T) {
	// 方針の中身を1つずつ確かめる。文字列の一致だけだと、書き換えたときに
	// 何が緩んだのかが分からない。
	for _, want := range []string{
		"default-src 'none'",
		"script-src 'self'",
		"style-src 'self'",
		"img-src 'self' data:",
		"connect-src 'self'",
		"form-action 'none'",
		"base-uri 'none'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(contentSecurityPolicy, want) {
			t.Errorf("CSP に %q が無い", want)
		}
	}
	for _, bad := range []string{"unsafe-inline", "unsafe-eval", "*", "http:", "https:"} {
		if strings.Contains(contentSecurityPolicy, bad) {
			t.Errorf("CSP に %q が入っている", bad)
		}
	}
}

func TestUnknownPathIs404(t *testing.T) {
	// "GET /" は前方一致なので、完全一致の検査が効いているかを見る。
	s := newTestServer(t, Options{})
	for _, target := range []string{"/nowhere", "/ui/index.html", "/api", "/api/lines/ja"} {
		rec := do(t, s, http.MethodGet, target, true, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: 状態コードが %d、404 を期待", target, rec.Code)
		}
	}
}

func TestWriteMethodsAreNotRouted(t *testing.T) {
	// この段は読み取り専用。書く経路は1つも無い。
	s := newTestServer(t, Options{})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := do(t, s, method, "/api/lines?locale=ja", true, nil)
		if rec.Code == http.StatusOK {
			t.Errorf("%s /api/lines が 200 を返した", method)
		}
	}
}

func TestIdleTrackerTouch(t *testing.T) {
	tr := newIdleTracker()
	tr.last = time.Now().Add(-time.Hour)
	if tr.since() < time.Hour {
		t.Fatalf("経過が %v", tr.since())
	}
	tr.touch()
	if tr.since() > time.Minute {
		t.Errorf("touch のあとも経過が %v", tr.since())
	}
}

func TestRequestTouchesIdleTracker(t *testing.T) {
	s := newTestServer(t, Options{})
	s.idle.last = time.Now().Add(-time.Hour)
	do(t, s, http.MethodGet, "/api/bootstrap", true, nil)
	if s.idle.since() > time.Minute {
		t.Errorf("要求を通しても経過が %v のまま", s.idle.since())
	}

	// 通らなかった要求では時計を進めない。外から叩き続けるだけで待ち受けが
	// 居座り続ける形にしないため。
	s.idle.last = time.Now().Add(-time.Hour)
	do(t, s, http.MethodGet, "/api/bootstrap", false, nil)
	if s.idle.since() < time.Hour {
		t.Errorf("404 の要求で経過が %v に戻った", s.idle.since())
	}
}

func TestUnknownLocaleAtStartup(t *testing.T) {
	root := newTestRoot(t)
	_, err := newServer(Options{Root: root, Locale: "nope", Stdout: io.Discard, Stderr: io.Discard})
	if err == nil {
		t.Fatal("知らないロケールでも起動してしまった")
	}
}

func TestNoLocaleAtStartup(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Translations"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := newServer(Options{Root: root, Stdout: io.Discard, Stderr: io.Discard})
	if err == nil {
		t.Fatal("対象が無くても起動してしまった")
	}
}

func TestStartupFailures(t *testing.T) {
	// 読み込みで失敗したら待ち受けを始めない。画面を出してから「バッジが出せません」と
	// 言われても、翻訳者には直しようがない。
	cases := []struct {
		name string
		root func(t *testing.T) string
	}{
		{"翻訳のフォルダーが無い", func(t *testing.T) string {
			return t.TempDir()
		}},
		{"再生順を読めない", func(t *testing.T) string {
			root := newTestRoot(t)
			// 同じ名前のディレクトリにする。あるのに読めない、という形になる。
			order := filepath.Join(root, "data", "script_order.csv")
			if err := os.Remove(order); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(order, 0o755); err != nil {
				t.Fatal(err)
			}
			return root
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hidden := false
			_, err := newServer(Options{
				Root: tc.root(t), Stdout: io.Discard, Stderr: io.Discard,
				HideFromRecord: func(string) { hidden = true },
			})
			if err == nil {
				t.Fatal("読めないのに起動してしまった")
			}
			// トークンを作る前に止まっている。止まった起動のトークンを伏せさせる理由は無い。
			if hidden {
				t.Error("起動しないのにトークンを伏せさせた")
			}
		})
	}
}

func TestNewServerFallsBackToTheConsole(t *testing.T) {
	// Stdout と Stderr を渡さなければ標準出力と標準エラーへ書く（Options の doc）。
	// nil のまま持つと、最初の1行（URL）を書くところで落ちる。
	s, err := newServer(Options{Root: newTestRoot(t)})
	if err != nil {
		t.Fatal(err)
	}
	if s.stdout != os.Stdout {
		t.Error("Stdout を省いたのに標準出力へ向いていない")
	}
	if s.stderr != os.Stderr {
		t.Error("Stderr を省いたのに標準エラーへ向いていない")
	}
	if s.opt.openBrowser == nil {
		t.Error("ブラウザーを開く関数が入っていない")
	}
}

func TestDisplayPathOnAnotherDrive(t *testing.T) {
	// Steam のライブラリーを別のドライブに置くのはよくある形で、そのときゲームの
	// フォルダーはルートからの相対にできない（filepath.Rel が誤りを返す）。
	// 相対にできないパスは、ルートの外のパスと同じくそのまま出す。空にしたり
	// 途中で切ったりすると、翻訳者はどのファイルを直しているのか分からない。
	if runtime.GOOS != "windows" {
		t.Skip("ドライブ文字があるのは Windows だけ")
	}
	s := newTestServer(t, Options{})
	s.opt.Root = `C:\dwloc\repo`
	const game = `D:\SteamLibrary\steamapps\common\DragNWash\BepInEx\ja.working.csv`
	if got, want := s.displayPath(game), filepath.ToSlash(game); got != want {
		t.Errorf("displayPath が %q、%q を期待", got, want)
	}
}

func TestTokensAreLongAndURLSafe(t *testing.T) {
	// 32バイトを、記号の増えない形（base64url、埋め草なし）で出す。URL に載せても
	// 書き換わらず、起動のたびに別の値になる。
	seen := make(map[string]bool)
	for range 16 {
		token, err := newToken()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || len(raw) != tokenBytes {
			t.Errorf("%q が %d バイトの base64url ではない（%d バイト、%v）", token, tokenBytes, len(raw), err)
		}
		if escaped := url.QueryEscape(token); escaped != token {
			t.Errorf("URL に載せると %q が %q に書き換わる", token, escaped)
		}
		if seen[token] {
			t.Errorf("同じトークンが2度出た: %q", token)
		}
		seen[token] = true
	}
}

func TestPanicBecomes500WithoutContent(t *testing.T) {
	// panic しても待ち受けは落とさず 500 を返す。記録するのはパスだけで、panic の
	// 値も問い合わせ文字列も書かない。値には行の中身が混ざりうるし、問い合わせ
	// 文字列にはトークンが載りうる。守りのヘッダーは 500 にも付いている。
	var log strings.Builder
	s := newTestServer(t, Options{UILang: "ja", Stderr: &log})
	h := s.securityHeaders(s.recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("訳: " + jaVanished)
	})))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/lines?locale=ja&t=secret-token", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状態コードが %d、500 を期待", rec.Code)
	}
	if want := s.t("error.internal"); !strings.Contains(rec.Body.String(), want) {
		t.Errorf("応答が %q、%q を期待", rec.Body.String(), want)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Errorf("500 に守りのヘッダーが付いていない: %q", got)
	}
	if !strings.Contains(log.String(), `dwloc edit: panic "/api/lines"`) {
		t.Errorf("記録にパスが無い: %q", log.String())
	}
	for _, secret := range []string{jaVanished, "secret-token", "locale=ja"} {
		if strings.Contains(rec.Body.String(), secret) || strings.Contains(log.String(), secret) {
			t.Errorf("応答か記録に %q が出ている\n応答: %q\n記録: %q", secret, rec.Body.String(), log.String())
		}
	}
}

func TestLoggerRecordsAnUnwrittenResponseAs200(t *testing.T) {
	// ハンドラーが何も書かずに戻ったときも、送られるのは 200 である。記録にも
	// 200 と書く。0 と書くと、記録を読んだ人は応答が無かったのかと思う。
	var file strings.Builder
	s := newTestServer(t, Options{Record: &file})
	h := s.logger(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/quiet?t=secret-token", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d", rec.Code)
	}
	if !strings.HasPrefix(file.String(), `dwloc edit: GET "/quiet" 200 `) {
		t.Errorf("記録が %q", file.String())
	}
	if strings.Contains(file.String(), "secret-token") {
		t.Errorf("記録に問い合わせ文字列が出ている: %q", file.String())
	}
}
