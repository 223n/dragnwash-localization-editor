package web

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ここは待ち受けを開いてから終えるまで（Run と、その中の announce / watchIdle）の試験。
//
// 待ち受けは本当に 127.0.0.1 で開く。外へは1本も繋がず、要求を送るのも
// 自分が開いた loopback の口だけである。ブラウザーは開かない。
// Options.openBrowser を差し替えて、開くはずだった URL を受け取るだけにする。

// runWait は待ち受けが開く・終わるのを待つ上限。
//
// これを超えたら「開かない」「終わらない」とみなす。暇で終わるまでの時間
// （[runIdleTimeout]）より十分長くしてある。
const runWait = 30 * time.Second

// runIdleTimeout は Run の試験で使う --idle-timeout。
//
// 短すぎると、試験が最初の要求を送る前に暇で終わってしまう。要求は開いた直後に
// 送るので、1秒あれば余裕がある。長くすると試験がそのぶん遅くなる。
const runIdleTimeout = time.Second

// startRun は s の待ち受けを別の goroutine で始める。
//
// 返すのは、開くはずだった URL（トークン付き）と、run が戻ったときの誤りを
// 受け取る口である。URL が出てくるのは、待ち受けを束ね終えたあとになる。
func startRun(t *testing.T, s *server) (string, <-chan error) {
	t.Helper()
	urls := make(chan string, 1)
	s.opt.openBrowser = func(u string) error {
		urls <- u
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- s.run() }()
	return waitURL(t, urls, done), done
}

// waitURL は開くはずだった URL が出てくるのを待つ。
func waitURL(t *testing.T, urls <-chan string, done <-chan error) string {
	t.Helper()
	select {
	case u := <-urls:
		return u
	case err := <-done:
		t.Fatalf("URL を出す前に終わった: %v", err)
	case <-time.After(runWait):
		t.Fatal("URL が出てこない")
	}
	return ""
}

// waitRun は待ち受けが終わるのを待ち、返した誤りを返す。
func waitRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(runWait):
		t.Fatal("待ち受けが終わらない")
	}
	return nil
}

// loopbackClient は待ち受けへ要求を送る道具。
//
// 303 を追わない。トークンを Cookie へ移すところを見たいので、送り返された先へ
// 勝手に進まれると困る。プロキシも通さない（環境変数に何が入っていても loopback へ
// 直に行く）。
func loopbackClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: runWait,
	}
}

// enterWithToken は開くはずだった URL を実際に開き、受け取った Cookie を返す。
//
// ブラウザーが最初にすることと同じで、トークンを渡して Cookie を受け取る。
func enterWithToken(t *testing.T, client *http.Client, openURL string) *http.Cookie {
	t.Helper()
	resp, err := client.Get(openURL)
	if err != nil {
		t.Fatalf("出した URL を開けない: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("出した URL が %d を返した。303 を期待", resp.StatusCode)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Cookie が %d 個、1個を期待", len(cookies))
	}
	return cookies[0]
}

func TestRunStopsWhenIdle(t *testing.T) {
	// 操作が絶えたら自分で終わる。誤りは返さない（dwloc edit は 0 で終わる）。
	// 翻訳者の PC に待ち受けを残さないための仕組みで、終わったあとはポートも放す。
	var out strings.Builder
	urls := make(chan string, 1)
	opt := Options{
		Root:        newTestRoot(t),
		UILang:      "ja",
		IdleTimeout: runIdleTimeout,
		Stdout:      &out,
		Stderr:      io.Discard,
		openBrowser: func(u string) error {
			urls <- u
			return nil
		},
	}
	done := make(chan error, 1)
	go func() { done <- Run(opt) }()
	openURL := waitURL(t, urls, done)

	// 出した URL で本当に入れること。Host の照合表と Cookie の名前は、実際に
	// 取れたポートから作る（--port 0 のときはここで初めて決まる）。試験の他の
	// ところは決め打ちのポートを入れているので、ここでしか通らない道である。
	parsed, err := url.Parse(openURL)
	if err != nil {
		t.Fatal(err)
	}
	client := loopbackClient()
	cookie := enterWithToken(t, client, openURL)
	if want := cookieNameFor(parsed.Port()); cookie.Name != want {
		t.Errorf("Cookie の名前が %q、%q を期待", cookie.Name, want)
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+parsed.Host+"/api/bootstrap", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("API を叩けない: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("受け取った Cookie で API が %d を返した", resp.StatusCode)
	}
	// こちらから先に接続を閉じておく。待ち受けの側に閉じかけの接続が残ると、
	// 下でポートを取り直せるかを見るときに邪魔になる。
	client.CloseIdleConnections()

	if err := waitRun(t, done); err != nil {
		t.Fatalf("暇で終わったのに誤りを返した: %v", err)
	}

	// 終わった理由を端末に出す。黙って消えると、翻訳者は落ちたのかと思う。
	c, err := loadCatalogs()
	if err != nil {
		t.Fatal(err)
	}
	ja := c.forServer("ja")
	for _, want := range []string{
		c.T(ja, "server.idle_stopped", "duration", runIdleTimeout.String()),
		c.T(ja, "server.stopped"),
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("標準出力に %q が無い:\n%s", want, out.String())
		}
	}

	// ポートを放している。同じ --port で開き直せる。
	ln, err := net.Listen("tcp", parsed.Host)
	if err != nil {
		t.Errorf("終わったあとも %s を放していない: %v", parsed.Host, err)
	} else {
		_ = ln.Close()
	}
}

func TestRunStopsOnInterrupt(t *testing.T) {
	// Ctrl+C で終えたときも誤りは返さず、終わった理由を端末に出す。
	//
	// 暇の見張りもそこで止める。止めないと見張りは回り続け、暇になった時点で
	// 終わった理由を「暇で終えた」へ書き換えにくる。その書き換えは Ctrl+C の側が
	// 理由を書いたのと錠を挟まずにぶつかる（データ競合）。dwloc edit は Run が
	// 戻るとすぐ終わるので画面には出ないが、Run を2度呼ぶ試験ではそのまま残る。
	if runtime.GOOS == "windows" {
		t.Skip("Windows では自分のプロセスへ os.Interrupt を送れない")
	}
	var out strings.Builder
	// 暇で終わる前に Ctrl+C が届くよう、暇の時間は長く取る。
	s := newTestServer(t, Options{UILang: "ja", IdleTimeout: time.Hour, Stdout: &out})
	openURL, done := startRun(t, s)

	// 応答が返ってくれば、Ctrl+C の受け口はもう開いている。run は受け口を
	// 開いてから要求を受け始める。
	client := loopbackClient()
	enterWithToken(t, client, openURL)
	client.CloseIdleConnections()

	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := self.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Ctrl+C で終えたのに誤りを返した: %v", err)
	}

	for _, want := range []string{s.t("server.interrupted"), s.t("server.stopped")} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("標準出力に %q が無い:\n%s", want, out.String())
		}
	}
	// 見張りに「止まれ」が伝わっている。
	select {
	case <-s.stop:
	default:
		t.Error("Ctrl+C で終えたのに、暇の見張りが回り続けている")
	}
	if s.stopReason != s.t("server.interrupted") {
		t.Errorf("終わった理由が %q", s.stopReason)
	}
}

func TestRunRefusesToStart(t *testing.T) {
	// 開けないときは誤りを返す（dwloc edit は 0 以外で終わる）。そのとき URL も
	// トークンも出さず、ブラウザーも開かない。開いていない待ち受けの URL を
	// 渡されても、翻訳者は開けないページを前に何が起きたのか分からない。
	taken, err := net.Listen("tcp", net.JoinHostPort(listenHost, "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	takenPort := taken.Addr().(*net.TCPAddr).Port

	cases := []struct {
		name string
		opt  Options
	}{
		// 読み込みで失敗する（newServer が止める）。
		{"翻訳のフォルダーが無い", Options{Root: t.TempDir()}},
		// 待ち受けそのものが失敗する（run が止める）。
		{"ポートがふさがっている", Options{Root: newTestRoot(t), Port: takenPort}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			opened := false
			opt := tc.opt
			opt.Stdout, opt.Stderr = &out, io.Discard
			opt.IdleTimeout = runIdleTimeout
			opt.openBrowser = func(string) error {
				opened = true
				return nil
			}

			if err := Run(opt); err == nil {
				t.Fatal("開けないのに誤りを返さない")
			}
			if out.Len() != 0 {
				t.Errorf("開けないのに標準出力へ書いた:\n%s", out.String())
			}
			if opened {
				t.Error("開けないのにブラウザーを開こうとした")
			}
		})
	}
}

func TestAnnounceTellsHowToOpen(t *testing.T) {
	// URL は、ブラウザーを開けたかどうかに関わらず必ず出す（WSL や Steam Deck では
	// 開けないことがある）。開けなかったときは開けなかったと言い、誤りの中身は
	// 書かない。翻訳者は開く手立てに関われない。
	const openURL = "http://127.0.0.1:54321/?t=token"
	failure := errors.New(`exec: "xdg-open": executable file not found in $PATH`)

	cases := []struct {
		name       string
		opt        Options
		openErr    error
		wantOpened bool
		want       []string
		notWant    []string
	}{
		{
			name:       "ブラウザーを開く",
			opt:        Options{IdleTimeout: 30 * time.Minute},
			wantOpened: true,
			want:       []string{"server.browser_opening"},
			notWant:    []string{"server.browser_failed", "server.browser_skipped"},
		},
		{
			name:       "開けなくても URL は出してある",
			opt:        Options{IdleTimeout: 30 * time.Minute},
			openErr:    failure,
			wantOpened: true,
			want:       []string{"server.browser_opening", "server.browser_failed"},
			notWant:    []string{"server.browser_skipped"},
		},
		{
			name:    "--no-browser",
			opt:     Options{IdleTimeout: 30 * time.Minute, NoBrowser: true},
			want:    []string{"server.browser_skipped"},
			notWant: []string{"server.browser_opening", "server.browser_failed"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			opt := tc.opt
			opt.UILang = "ja"
			s := newTestServer(t, opt)
			s.stdout = &out
			opened := false
			s.opt.openBrowser = func(u string) error {
				opened = true
				if u != openURL {
					t.Errorf("開こうとした URL が %q", u)
				}
				return tc.openErr
			}

			s.announce(openURL)

			text := out.String()
			if want := s.t("server.url", "url", openURL); !strings.Contains(text, want) {
				t.Errorf("URL が出ていない\n--- 期待 ---\n%s\n--- 標準出力 ---\n%s", want, text)
			}
			if opened != tc.wantOpened {
				t.Errorf("ブラウザーを開こうとしたか: %v、%v を期待", opened, tc.wantOpened)
			}
			for _, key := range tc.want {
				if !strings.Contains(text, s.t(key)) {
					t.Errorf("%s が出ていない:\n%s", key, text)
				}
			}
			for _, key := range tc.notWant {
				if strings.Contains(text, s.t(key)) {
					t.Errorf("%s が出ている:\n%s", key, text)
				}
			}
			if strings.Contains(text, "xdg-open") {
				t.Errorf("開けなかった理由の中身を出している:\n%s", text)
			}
		})
	}
}

func TestAnnounceTellsWhenItWillStop(t *testing.T) {
	// 暇で終わるなら何分で終わるかを、終わらないなら終わらないことを言う。
	// 黙って終わると、翻訳者は落ちたのかと思う。
	cases := []struct {
		name    string
		idle    time.Duration
		want    func(s *server) string
		notWant string
	}{
		{"暇で終わる", 30 * time.Minute,
			func(s *server) string { return s.t("server.idle_hint", "duration", "30m0s") }, "server.idle_off"},
		{"--idle-timeout 0", 0,
			func(s *server) string { return s.t("server.idle_off") }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			s := newTestServer(t, Options{UILang: "ja", IdleTimeout: tc.idle, NoBrowser: true})
			s.stdout = &out
			s.announce("http://127.0.0.1:54321/?t=token")

			if want := tc.want(s); !strings.Contains(out.String(), want) {
				t.Errorf("%q が出ていない:\n%s", want, out.String())
			}
			if tc.notWant != "" && strings.Contains(out.String(), s.t(tc.notWant)) {
				t.Errorf("%s が出ている:\n%s", tc.notWant, out.String())
			}
		})
	}
}

func TestAnnounceHasNoPublishHintWithoutWorkingCopy(t *testing.T) {
	// 作業コピーが1つも無ければ、画面が直すのは公開ファイルそのものである。
	// 「publish を回すまでコミットする側へ入らない」と言うのは誤りになる。
	// 出す側（作業コピーがあるとき）は [TestPublishNoteIsTheSameOnBothSides] が見ている。
	var out strings.Builder
	s := newTestServer(t, Options{UILang: "ja", NoBrowser: true})
	s.stdout = &out
	s.announce("http://127.0.0.1:54321/?t=token")
	if strings.Contains(out.String(), s.t("server.publish_hint")) {
		t.Errorf("作業コピーが無いのに publish の案内が出ている:\n%s", out.String())
	}
}

func TestWatchIdle(t *testing.T) {
	t.Run("0 なら見張らない", func(t *testing.T) {
		// --idle-timeout 0 は「終わらない」。どれだけ暇でも止めない。
		s := newTestServer(t, Options{IdleTimeout: 0})
		s.idle.last = time.Now().Add(-24 * time.Hour)
		s.watchIdle()
		select {
		case <-s.stop:
			t.Error("--idle-timeout 0 なのに止めた")
		default:
		}
	})

	t.Run("見張りの間隔より短くても、その時間で終わる", func(t *testing.T) {
		// 見に行く間隔（15秒）より短い --idle-timeout では、間隔のほうを縮める。
		// 縮めないと、1秒を指定しても15秒まで居座る。
		const idle = 20 * time.Millisecond
		s := newTestServer(t, Options{IdleTimeout: idle, UILang: "ja"})
		s.idle.last = time.Now().Add(-time.Hour)

		start := time.Now()
		s.watchIdle()
		if elapsed := time.Since(start); elapsed >= idleCheckInterval {
			t.Errorf("止めるまでに %v かかった。%v を指定している", elapsed, idle)
		}
		select {
		case <-s.stop:
		default:
			t.Fatal("暇なのに止めていない")
		}
		if want := s.t("server.idle_stopped", "duration", idle.String()); s.stopReason != want {
			t.Errorf("終わった理由が %q、%q を期待", s.stopReason, want)
		}
	})
}
