package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

const (
	// DefaultIdleTimeout は --idle-timeout の既定値。
	//
	// 操作が絶えたら自分で終わる。翻訳者の PC に待ち受けを残さないためで、
	// 残せば「起動しっぱなしのローカルサーバー」が1つ増える。トークンは
	// 起動のたびに変わるので、残った待ち受けは誰も開けないまま居座ることになる。
	DefaultIdleTimeout = 30 * time.Minute

	// readHeaderTimeout はヘッダーを読み終えるまでの上限。
	// 手元の1人が使う道具なので短くてよい。開いたまま黙る接続を残さない。
	readHeaderTimeout = 10 * time.Second

	// shutdownTimeout は終わるときに進行中の要求を待つ上限。
	shutdownTimeout = 5 * time.Second

	// idleCheckInterval は暇かどうかを見にいく間隔。
	idleCheckInterval = 15 * time.Second

	// listenHost は束ねる先。固定である。--port を指定しても変わらない。
	//
	// 0.0.0.0 も :: も受け付けない。手元の1人が使う道具で、同じ LAN の誰かが
	// 開ける必要はまったく無い。開ける形にできるようにしておくと、いつか
	// 誰かが開ける。
	listenHost = "127.0.0.1"
)

// Options は [Run] の指定。
type Options struct {
	// Root は翻訳リポジトリのルート。
	Root string
	// Game はゲーム側のプラグインフォルダー。空なら見に行かない。
	//
	// ここが入っていると、作業コピーの探し先にゲーム側が加わり、そちらが先に
	// 当たる。原文の欄が埋まり、「未翻訳」を判定できるようになる。画面が並べる
	// のも保存するのもその作業コピーで、コミットする側へ入るのは publish を
	// 回したときである。
	//
	// 値は internal/gamedir で目印まで確かめ終わったパスであること。確かめは
	// 呼び出し側（cmd/dwloc）が済ませる。--game を省かれたときに探しに行くか
	// どうかも呼び出し側の判断で、edit は探し、publish と diff は探さない。
	// ここではどちらの経路で決まった値も同じに扱う。
	Game string
	// Locale は最初に出すロケール。空なら選ばせる。
	//
	// ここへ渡すのは publish.DiscoverTargets が返した名前そのものであること。
	// 表記ゆれの吸収は呼び出し側（cmd/dwloc）が済ませる。
	Locale string
	// Port は待ち受けるポート。0 なら空きを取る。束ねる先は常に 127.0.0.1。
	Port int
	// UILang は画面の言語タグ。空なら Accept-Language を見る。
	UILang string
	// IdleTimeout は操作が絶えてから終わるまで。0 で終わらない。
	IdleTimeout time.Duration
	// NoBrowser が true ならブラウザーを開かない。
	NoBrowser bool
	// Verbose が true なら要求を1行ずつ記録する。
	//
	// 記録するのはメソッド・パス・状態コード・所要時間・ロケール名・件数だけ。
	// 原文と訳は既定でも --verbose でも書かない。
	Verbose bool

	// Stdout は URL とトークンの行き先。nil なら os.Stdout。
	Stdout io.Writer
	// Stderr は記録の行き先。nil なら os.Stderr。
	Stderr io.Writer

	// oldOrder は1つ前の版の再生順を返す関数。nil なら [diff.GitOldOrder]。
	//
	// 試験で引き継ぎ候補を出すための穴である。固定にすると、引き継ぎ候補が
	// バッジとして画面に載るところまでを見るのに git リポジトリが要る。
	// 見本が git でない以上、その経路は端から端まで一度も通らない。
	// internal/diff が同じ理由で Options.OldOrder を開けているのに合わせる。
	oldOrder diff.OldOrderSource

	// openBrowser はブラウザーを開く関数。nil なら [openBrowser]。
	// テストで差し替えるための穴で、外からは触れない。
	openBrowser func(url string) error
}

// asset は埋め込んだ資産1つ。
type asset struct {
	body        []byte
	contentType string
}

// server は待ち受けの状態。
type server struct {
	opt Options

	cat       *catalogs
	serverCat *Catalog

	// targets はロケールの一覧。クライアントが指定できるのはこの名前だけで、
	// パスの組み立ては常にこちら側で行う。
	targets []publish.Target
	// summaries と findings はロケール名で引く、起動時のスナップショット。
	summaries map[string]diff.Summary
	findings  map[string][]diff.Finding

	// overlay は起動後の局所更新（訳が入ったキー）。件数とバッジをここで引く。
	overlay *editOverlay
	// saveMu は保存を直列にする。同じファイルへ同時に2つ書かせない。
	saveMu sync.Mutex

	token string
	hosts map[string]struct{}
	// cookieName は Cookie の名前。待ち受けているポートを含む（[cookieNameFor]）。
	cookieName string
	assets     map[string]asset

	idle *idleTracker

	stdout io.Writer
	stderr io.Writer

	// stop は待ち受けを終えるための合図。暇で終わるときに閉じる。
	stop     chan struct{}
	stopOnce sync.Once
	// stopReason は終わった理由の文言。
	stopReason string
}

// Run は待ち受けを始め、終わるまで戻らない。
//
// 終わるのは3つの場合。Ctrl+C を受け取ったとき、--idle-timeout のあいだ操作が
// 絶えたとき、待ち受け自体が失敗したとき。
func Run(opt Options) error {
	s, err := newServer(opt)
	if err != nil {
		return err
	}
	return s.run()
}

// newServer は起動前の読み込みを全部済ませる。
//
// 読み込みで失敗したら待ち受けを始めない。画面を出してから「バッジが出せません」と
// 言われても、翻訳者には直しようがない。
func newServer(opt Options) (*server, error) {
	if opt.Stdout == nil {
		opt.Stdout = os.Stdout
	}
	if opt.Stderr == nil {
		opt.Stderr = os.Stderr
	}
	if opt.openBrowser == nil {
		opt.openBrowser = openBrowser
	}

	cat, err := loadCatalogs()
	if err != nil {
		return nil, err
	}
	s := &server{
		opt:       opt,
		cat:       cat,
		serverCat: cat.forServer(opt.UILang),
		summaries: make(map[string]diff.Summary),
		findings:  make(map[string][]diff.Finding),
		overlay:   newEditOverlay(),
		idle:      newIdleTracker(),
		stdout:    opt.Stdout,
		stderr:    opt.Stderr,
		stop:      make(chan struct{}),
	}

	// ロケールの列挙は publish.DiscoverTargetsWithGame に委ねる。走査の規則を
	// 2か所に持つと、publish が対象にするロケールと、この画面が開くロケールが
	// 食い違う。publish も同じ関数を通るので、画面で直したファイルが publish の
	// 入力になることが、呼び先の一致で保たれる。
	targets, err := publish.DiscoverTargetsWithGame(opt.Root, opt.Game)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, errors.New(s.t("error.no_locales", "path",
			s.displayPath(filepath.Join(opt.Root, publish.TranslationsDir))))
	}
	s.targets = targets

	if opt.Locale != "" && s.target(opt.Locale) == nil {
		return nil, errors.New(s.t("error.unknown_locale", "locale", opt.Locale))
	}

	// 状態バッジと件数のスナップショットを1回だけ作る。画面を読み直しても
	// この値は変わらない。ファイルの中身は開くたびに読み直すが、突き合わせは
	// 起動時のもので、両者がずれうることは承知のうえで固定してある
	// （比較は13ロケール全部を読むので、1行開くたびにやり直す種類の処理ではない）。
	repo, err := diff.LoadWith(opt.Root, diff.Options{
		Working: true, Game: opt.Game, OldOrder: opt.oldOrder,
	})
	if err != nil {
		return nil, err
	}
	report := diff.Compare(repo, nil)
	for _, sum := range report.Locales {
		s.summaries[sum.Locale] = sum
	}
	for _, f := range report.Findings {
		s.findings[f.Locale] = append(s.findings[f.Locale], f)
		if f.Category == diff.CatUntranslated {
			// 「未翻訳」のキーだけ控えておく。保存で訳が入ったら、この集合との
			// 積のぶんだけ件数から引く。引くだけで、カテゴリの再判定はしない。
			s.overlay.addUntranslated(f.Locale, f.Key)
		}
		if f.Category == diff.CatTagUnbalanced {
			// 「タグの開閉がそろわない行」のキーも控えておく。こちらは保存した
			// 行だけ判定し直す（訳だけで判定できるので、他のロケールを読まない）。
			// 起動時の集合は、件数をどちらへ動かすかを決めるために持つ。
			s.overlay.addTagged(f.Locale, f.Key)
		}
	}

	if err := s.loadAssets(); err != nil {
		return nil, err
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	s.token = token
	return s, nil
}

// loadAssets は埋め込んだ資産を読み出す。
//
// http.FileServer を使わないのは、クライアントにパスを書かせないため。経路は
// 下の3つだけで、要求のパスからファイル名を組み立てる場所がどこにも無い。
func (s *server) loadAssets() error {
	files := map[string]string{
		"/":        "ui/index.html",
		"/app.css": "ui/app.css",
		"/app.js":  "ui/app.js",
	}
	types := map[string]string{
		"/":        "text/html; charset=utf-8",
		"/app.css": "text/css; charset=utf-8",
		"/app.js":  "text/javascript; charset=utf-8",
	}
	s.assets = make(map[string]asset, len(files))
	for route, name := range files {
		body, err := fs.ReadFile(uiFS, name)
		if err != nil {
			return fmt.Errorf("%s を読めません: %w", name, err)
		}
		s.assets[route] = asset{body: body, contentType: types[route]}
	}
	return nil
}

// run は待ち受けを始める。
func (s *server) run() error {
	addr := net.JoinHostPort(listenHost, strconv.Itoa(s.opt.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	// Host ヘッダーの照合表と Cookie の名前は、実際に取れたポートから作る。
	// --port 0 のときはここで初めてポートが決まる。
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	s.usePort(port)

	url := "http://" + listenHost + ":" + port + "/?" + tokenParam + "=" + s.token
	s.announce(url)

	httpSrv := &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          nil,
	}

	// Ctrl+C と暇の両方で終える。どちらも「終わる」1本の道へ合流させる。
	ctx, cancelSignal := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancelSignal()

	done := make(chan error, 1)
	go func() { done <- httpSrv.Serve(ln) }()

	s.idle.touch()
	go s.watchIdle()

	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		s.stopReason = s.t("server.interrupted")
	case <-s.stop:
	}

	if s.stopReason != "" {
		fmt.Fprintln(s.stdout, s.stopReason)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	fmt.Fprintln(s.stdout, s.t("server.stopped"))
	return nil
}

// usePort は取れたポートから、Host の照合表と Cookie の名前を作る。
//
// 2つを同じ場所で作るのは、片方だけ変えて食い違うのを避けるためである。
// どちらも「この待ち受けはこのポートのもの」という同じ1つの根拠から出ている。
func (s *server) usePort(port string) {
	s.hosts = allowedHosts(port)
	s.cookieName = cookieNameFor(port)
}

// announce は開く先を標準出力へ書く。
//
// URL を必ず標準出力へ出すのは、ブラウザーが開かない環境があるため（WSL、
// Steam Deck など）。自動で開けたかどうかに関わらず出す。
//
// トークンはここにしか出さない。ファイルには書かない。書けば、待ち受けが
// 終わったあとも読める形で残る。
func (s *server) announce(url string) {
	fmt.Fprintln(s.stdout, s.t("server.listening"))
	fmt.Fprintln(s.stdout, s.t("server.url", "url", url))
	fmt.Fprintln(s.stdout, s.t("server.editing"))
	fmt.Fprintln(s.stdout, s.t("server.save_target"))
	if s.hasWorkingCopy() {
		// 作業コピーを開いているロケールが1つでもあれば、そこへの保存だけでは
		// コミットする側へ入らないことを端末にも出す。ロケールごとの正確な
		// 行き先は画面の断り書きに出るが、そこまで読まずに打ち始める人がいる。
		//
		// 開いているのがゲーム側の作業コピーなら、要るのは publish ではなく
		// publish --game である。ここを言い分けるのは、--game の要る人へ
		// 要らない字を渡すと、そのとおり打った publish が「0 件変わりません」と
		// 言って終わるからである。訳は失われないので守り（publish.CheckLoss）も
		// 止めず、入らなかったことに気づく手がかりが出ない。
		fmt.Fprintln(s.stdout, s.t("server.publish_hint"))
	}
	fmt.Fprintln(s.stdout, s.t("server.locales", "locales", strings.Join(localeNames(s.targets), ", ")))
	if s.opt.IdleTimeout > 0 {
		fmt.Fprintln(s.stdout, s.t("server.idle_hint", "duration", s.opt.IdleTimeout.String()))
	} else {
		fmt.Fprintln(s.stdout, s.t("server.idle_off"))
	}
	fmt.Fprintln(s.stdout, s.t("server.stop_hint"))

	if s.opt.NoBrowser {
		fmt.Fprintln(s.stdout, s.t("server.browser_skipped"))
		return
	}
	fmt.Fprintln(s.stdout, s.t("server.browser_opening"))
	if err := s.opt.openBrowser(url); err != nil {
		// 失敗しても待ち受けは続ける。URL は既に出してある。
		// 誤りの中身は書かない（開く手立てに翻訳者は関われない）。
		fmt.Fprintln(s.stdout, s.t("server.browser_failed"))
	}
}

// watchIdle は操作が絶えたら待ち受けを終える。
func (s *server) watchIdle() {
	if s.opt.IdleTimeout <= 0 {
		return
	}
	interval := idleCheckInterval
	if s.opt.IdleTimeout < interval {
		interval = s.opt.IdleTimeout
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			if s.idle.since() >= s.opt.IdleTimeout {
				s.stopOnce.Do(func() {
					s.stopReason = s.t("server.idle_stopped",
						"duration", s.opt.IdleTimeout.String())
					close(s.stop)
				})
				return
			}
		}
	}
}

// t は Go 側の文言を返す。
func (s *server) t(key string, kv ...string) string {
	return s.cat.T(s.serverCat, key, kv...)
}

// target はロケール名から対象を引く。当たらなければ nil。
//
// 照合は完全一致だけにする。クライアントが送ってくるのは、この待ち受け自身が
// 一覧として渡した名前しかない。表記ゆれを吸収する必要があるのはコマンドラインの
// 指定（cmd/dwloc の --locale）だけで、そちらは呼び出し側で済ませてある。
func (s *server) target(locale string) *publish.Target {
	for i := range s.targets {
		if s.targets[i].Locale == locale {
			return &s.targets[i]
		}
	}
	return nil
}

// displayPath は表示に出すパスをルートからの相対にしてスラッシュ区切りで返す。
//
// 絶対パスをそのまま出さないのは、手元の絶対パスに利用者名が入ることがあるため。
// 画面の内容は不具合報告に貼られる。cmd/dwloc の displayPath と同じ考え方だが、
// あちらは package main にあるので取り込めない。
func (s *server) displayPath(path string) string {
	absRoot, rootErr := filepath.Abs(s.opt.Root)
	absPath, pathErr := filepath.Abs(path)
	if rootErr != nil || pathErr != nil {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return filepath.ToSlash(path)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// hasWorkingCopy は、作業コピーを開くロケールが1つでもあるかを返す。
//
// 作業コピーがあるかはロケールごとに決まるが、起動時の1行はロケールを選ぶ前に
// 出すので、1つでもあれば出す。ロケールごとの正確な行き先は画面に出る
// （「ファイル」の欄と note.via_publish の断り書き）。
func (s *server) hasWorkingCopy() bool {
	for _, t := range s.targets {
		if t.Input != t.Output {
			return true
		}
	}
	return false
}

// localeNames は対象のロケール名を並び順のまま取り出す。
func localeNames(targets []publish.Target) []string {
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		names = append(names, t.Locale)
	}
	return names
}

// idleTracker は最後に要求を受けた時刻を持つ。
type idleTracker struct {
	mu   sync.Mutex
	last time.Time
}

func newIdleTracker() *idleTracker {
	return &idleTracker{last: time.Now()}
}

// touch は「いま操作があった」と記録する。
func (t *idleTracker) touch() {
	t.mu.Lock()
	t.last = time.Now()
	t.mu.Unlock()
}

// since は最後の操作からの経過を返す。
func (t *idleTracker) since() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return time.Since(t.last)
}
