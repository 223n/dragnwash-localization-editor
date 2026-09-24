package web

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// contentSecurityPolicy は全ての応答に付ける方針。
//
// default-src 'none' から始めて、要るものだけを1つずつ開ける。開けていないものは
// 取りにいけない。外部の CDN もフォントも参照しないので、'self' より先は要らない。
//
//   - script-src 'self'   埋め込んだ app.js だけ。行内スクリプトも通らない。
//   - style-src 'self'    埋め込んだ app.css だけ。
//   - img-src 'self' data:  data: は頁の favicon（空の data: URL）のため。
//     これを開けないと、ブラウザーが /favicon.ico を取りにきて毎回 404 になる。
//   - connect-src 'self'  fetch の行き先を自分自身に限る。原文を外へ出さない
//     という約束を、ブラウザー側でも縛るための1行。
//   - form-action 'none'  この画面に form は無い。出す予定も無い。
//   - base-uri 'none'     <base> で相対の行き先を差し替えられないようにする。
//   - frame-ancestors 'none'  他の頁に埋め込ませない。
const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; connect-src 'self'; form-action 'none'; base-uri 'none'; " +
	"frame-ancestors 'none'"

// handler は経路と検査を組み立てる。
//
// 外から内へ、次の順に通す。
//
//  1. 守りのヘッダー   どの応答にも付ける。404 にも付ける。
//  2. panic からの復帰 行の中身を漏らさずに 500 を返す。
//  3. 記録            --verbose かログファイルがあるときだけ。中身は書かない。
//  4. Host の検査      通す綴りの完全一致だけ。
//  5. Sec-Fetch の検査 同一生成元だけ。
//  6. トークンと Cookie  無ければ 404。
//  7. 書き込みの守り   POST の Origin と Content-Type を検査する。
//  8. 経路            ここまで通ったものだけが届く。
func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /app.css", s.handleAsset("/app.css"))
	mux.HandleFunc("GET /app.js", s.handleAsset("/app.js"))
	mux.HandleFunc("GET /api/bootstrap", s.handleBootstrap)
	mux.HandleFunc("GET /api/lines", s.handleLines)
	mux.HandleFunc("GET /api/export", s.handleExport)
	mux.HandleFunc("POST /api/rows", s.handleRows)

	return s.securityHeaders(s.recoverer(s.logger(s.checkHost(s.checkFetchSite(
		s.authenticate(s.checkWrite(mux)))))))
}

// securityHeaders は守りのヘッダーを付ける。
//
// 応答を書く前に付けるので、この先で 404 を返しても 500 を返しても付いている。
// 付け忘れる経路を作らないために、いちばん外側に置いてある。
func (s *server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		// ディスクキャッシュに残さない。残せば、待ち受けが終わったあとも
		// 原文と訳がブラウザーのキャッシュから読める形で残る。
		// API だけでなく画面にも付けるのは、残してよい応答が1つも無いため。
		h.Set("Cache-Control", "no-store")
		// CORS のヘッダーは1つも返さない。返さないことが、他の生成元から
		// 中身を読めないことの根拠になる。
		next.ServeHTTP(w, r)
	})
}

// recoverer は panic を受け止めて 500 を返す。
//
// 記録するのはパスだけで、panic の値は書かない。値には行の中身が混ざりうる
// （フィールドを含む文字列がそのまま入ることがある）。手元の待ち受けの記録に
// 原文が残ると、それを貼った不具合報告から台本が出ていく。
// 原因を追いにくくなるのは承知のうえで、こちらを採る。
func (s *server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logf("panic %s", recordPath(r.URL.Path))
				http.Error(w, s.t("error.internal"), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusWriter は状態コードを覚える http.ResponseWriter。
type statusWriter struct {
	http.ResponseWriter
	status int
	// note は記録に足す1言（ロケール名と件数）。ハンドラーが入れる。
	note string
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// logger は要求を1行ずつ記録する。--verbose かログファイルがあるときだけ。
//
// 書くのはメソッド・パス・状態コード・所要時間と、ハンドラーが足した1言
// （ロケール名と件数）だけ。
//
// パスは r.URL.Path で、問い合わせ文字列は書かない。最初の1回の URL には
// トークンが載っているので、そのまま書くと記録からトークンが読める。
// 原文と訳は既定でも --verbose でもログファイルでも書かない。
//
// 認証より外側に置いてあるので、通らなかった要求（404）も記録に残る。起動し
// 直したあとに古いタブが出す 404 を、あとから辿れるようにするためである。
// その代わり、メソッドとパスはトークンを持たない相手が決めた文字列になる。
// そのまま書かずに [recordMethod] と [recordPath] を通す。
func (s *server) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := s.recordTo()
		if out == nil {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		fmt.Fprintf(out, "dwloc edit: %s %s %d %s%s\n", recordMethod(r.Method), recordPath(r.URL.Path), sw.status,
			time.Since(start).Round(time.Millisecond), sw.note)
	})
}

// recordMethodMax は、記録に書くメソッドの上限（バイト）。
//
// 登録されたメソッド（WebDAV の VERSION-CONTROL や UPDATEREDIRECTREF など、
// どれも20バイトに満たない）が切れずに収まり、1行が長くなりすぎない長さにしてある。
const recordMethodMax = 32

// recordMethod は、記録に書くためのメソッドを返す。
//
// メソッドも、パスと同じくトークンを持たない相手が決められる。net/http はトークンの
// 文字しか通さないので改行は入らないが、長さはヘッダーの上限（既定で 1MB）まで
// 通る。そのまま書くと、パスを切っても、1回の要求で記録を 1MB 近く太らされる。
//
// net/http が名前を持つ9つは、囲まずにそのまま書く。この待ち受けが受けるのは
// GET と POST だけで、ふだんの行の形を変えないため。それ以外は [recordPath] と
// 同じく %q で囲み、長ければ切る。囲んであれば、見慣れないメソッドだと一目で分かる。
func recordMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	}
	return recordClip(method, recordMethodMax)
}

// recordPathMax は、記録に書くパスの上限（バイト）。
//
// 実際の経路はどれも20バイトに満たない。上限はそれより十分に長く、1行が端末の
// 幅を大きく超えない長さにしてある。
const recordPathMax = 200

// recordPath は、記録に書くためにパスを %q で囲み、長ければ切る。
//
// 記録は認証の外側で取るので、パスはトークンを持たない相手でも決められる。
// そのまま書くと、改行を入れて本物と見分けの付かない行を足されたり、1回の
// 要求で記録を 1MB 近く太らされたりする（http.Server の既定のヘッダーの上限）。
// %q なら改行や制御文字は \n の形になり、1要求は必ず1行に収まる。囲む引用符で、
// パスの終わりもはっきりする。
//
// 切るときは文字の途中で切らない。途中で切ると %q が壊れた1バイトを \x の形で
// 書き、元に無い文字が見える。切ったことと元の長さは、後ろに添える。
func recordPath(path string) string {
	return recordClip(path, recordPathMax)
}

// recordClip は s を %q で囲み、limit バイトを超える分を切って返す（[recordPath]）。
func recordClip(s string, limit int) string {
	if len(s) <= limit {
		return strconv.Quote(s)
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strconv.Quote(s[:cut]) + "…(" + strconv.Itoa(len(s)) + " bytes)"
}

// recordTo は要求の記録の行き先を返す。記録しないときは nil。
//
// --verbose のときは画面（標準エラー）へ書く。呼び出し側がそこをログファイルへも
// 束ねているので、1回書けば画面とファイルの両方に入る。--verbose でないときは
// ログファイルだけへ書く。行き先を2つに分けてどちらへも書くと、--verbose の
// ときにファイルへ同じ行が2度入る。
func (s *server) recordTo() io.Writer {
	if s.opt.Verbose {
		return s.stderr
	}
	return s.record
}

// logf は記録を標準エラーへ1行書く。
func (s *server) logf(format string, args ...any) {
	fmt.Fprintf(s.stderr, "dwloc edit: "+format+"\n", args...)
}

// checkHost は Host ヘッダーを完全一致で検査する。
//
// DNS リバインディングへの対策。攻撃者の持つ名前を 127.0.0.1 へ向け直されると、
// ブラウザーから見れば同一生成元のまま、この待ち受けへ要求が届く。そのときの
// Host はその名前なので、Host を数えあげて通す側で弾く。
func (s *server) checkHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.hosts[r.Host]; !ok {
			s.notFound(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// checkFetchSite は Sec-Fetch-Site を検査する。
//
// 同一生成元からの要求だけを通す。頁の中の fetch には必ず same-origin が付く。
//
// none を通すのは、アドレス欄に貼った移動と、ブラウザーの自動起動がそれになる
// ため。最初の1回がこれで、ここを塞ぐと画面が開かない。通すのは GET の "/" に
// 限る。他の生成元の頁からの移動は cross-site になるので、これは混ざらない。
//
// ヘッダーが無いとき（古いブラウザー）は通す。代わりに Host とトークンと
// SameSite=Strict の Cookie が効いている。ここだけで守っているわけではない。
func (s *server) checkFetchSite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch site := r.Header.Get("Sec-Fetch-Site"); {
		case site == "":
		case site == "same-origin":
		case site == "none" && r.Method == http.MethodGet && r.URL.Path == "/":
		default:
			s.notFound(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authenticate はトークンと Cookie を見る。
//
// 流れは1本しかない。
//
//	GET /?t=<トークン>  → Cookie を置いて、素の / へ 303 で送り返す
//	Cookie あり          → 通す
//	それ以外             → 404
//
// トークンを URL から Cookie へ移すのは、URL がブラウザーの履歴に残るため。
// 303 で素の / へ送り返せば、アドレス欄にも履歴にもトークンは残らない。
//
// 404 を返すのは、401 だと「そこに何かがある」と教えてしまうため。他の頁から
// 手探りされたとき、返す答えが「ありません」なら、この待ち受けが動いていること
// 自体が分からない。
func (s *server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(s.cookieName); err == nil && sameToken(c.Value, s.token) {
			s.idle.touch()
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/" &&
			sameToken(r.URL.Query().Get(tokenParam), s.token) {
			s.idle.touch()
			http.SetCookie(w, sessionCookie(s.cookieName, s.token))
			// 303 にするのは、送り先を必ず GET で取りにいかせるため。
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		s.notFound(w)
	})
}

// checkWrite は書き込む要求の入口を絞る。
//
// 見るのは2つ。どちらも「素のフォーム送信では満たせない」ことが根拠になる。
//
//	Origin        同じ生成元の綴りと完全一致すること。ブラウザーは POST に必ず
//	              付けるうえ、頁の JavaScript から偽れない。他の頁から送られた
//	              要求はここで落ちる。
//	Content-Type  application/json であること。<form> が送れるのは
//	              application/x-www-form-urlencoded と multipart/form-data と
//	              text/plain の3つだけなので、この要求を満たせない。
//
// Cookie が SameSite=Strict なので他の生成元からの要求にはそもそも載らないが、
// 守りを1本だけにしない。SameSite の扱いはブラウザーごとに差があり、
// 将来どれかが緩む可能性を、この2つが引き受ける。
//
// Origin が無い、または合わない要求は 404 にする。認証の失敗と同じ扱いで、
// 「そこに何かがある」と教えない。Content-Type だけは 415 を返す。ここまで
// 通っているのは Cookie も Origin も揃った要求、つまりこの画面自身なので、
// 直せる相手に理由を返すほうがよい。
func (s *server) checkWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		if !s.sameOrigin(r.Header.Get("Origin")) {
			s.notFound(w)
			return
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			http.Error(w, s.t("error.not_json"), http.StatusUnsupportedMediaType)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin は Origin がこの待ち受け自身かを返す。
//
// 通す綴りは [allowedHosts] から作る。Host の照合表と根拠を1つにしておくと、
// 片方だけ緩めて食い違う、ということが起きない。http:// しか剥がさないので、
// https:// で来た要求は（同じ host:port でも）通らない。
func (s *server) sameOrigin(origin string) bool {
	host, ok := strings.CutPrefix(origin, "http://")
	if !ok {
		return false
	}
	_, ok = s.hosts[host]
	return ok
}

// notFound は「ありません」を返す。中身は増やさない。
func (s *server) notFound(w http.ResponseWriter) {
	http.Error(w, "404 page not found", http.StatusNotFound)
}
