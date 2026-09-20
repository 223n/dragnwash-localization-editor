package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/edit"
)

// uiView は画面へ渡す目録。
type uiView struct {
	Lang string `json:"lang"`
	// Dir は組む向き。画面は <html dir> にそのまま写す。UI 自体を右から左に
	// 組む言語が来ても、骨組みを直さずに済ませるための1つ。
	Dir      string            `json:"dir"`
	Name     string            `json:"name"`
	Messages map[string]string `json:"messages"`
}

// bootstrapResponse は GET /api/bootstrap の応答。
type bootstrapResponse struct {
	UI uiView `json:"ui"`
	// Locales は選べるロケール名。クライアントが /api/lines に載せてよいのは
	// この一覧にある名前だけで、載せたとしても照合を通らなければ届かない。
	Locales []string `json:"locales"`
	// Selected は最初に出すロケール。--locale が無ければ空。
	Selected string `json:"selected"`
	// Game は作業コピーを探しているゲーム側のプラグインフォルダー。
	// --game が無ければ空。
	//
	// 「使っているフォルダー」ではない。ロケールによっては、そこに作業コピーが
	// 無くて1バイトも読まない。実際に読み書きするファイルはロケールごとに決まり、
	// linesResponse.Path に出る。
	//
	// それでも探し先を出すのは、いまどのファイルを直しているのかを取り違えさせない
	// ため。リポジトリの中だけを見ているときと、ゲームのフォルダーを読んでいるときで、
	// 出てくる行の見た目は変わらない。黙って別の場所を読み始めると、翻訳者は
	// 直したものがどこへ入ったのかを追えなくなる。
	//
	// 出すのはパスだけである。ゲーム側の作業コピーには原文（英語の台本）が
	// 入っているが、その中身は行の一覧と同じ道でしか出さない。
	Game string `json:"game,omitempty"`
	// CanEdit は訳を書き換えて保存できるか。画面はこれを見て入力欄を出す。
	//
	// ファイル単位で編集できない場合（ヘッダーが受理できない）は、行を読む
	// ときに linesResponse.ReadOnlyReason で分かる。ここは待ち受け全体の話。
	CanEdit bool `json:"canEdit"`
	// AutosaveDelayMs は入力が止まってから自動保存するまでの待ち時間。
	//
	// 画面に持たせず待ち受けから渡すのは、値を変えたときに直す場所を1つに
	// するため。画面はこの値をそのまま使う。
	AutosaveDelayMs int `json:"autosaveDelayMs"`
}

// handleIndex は画面そのものを返す。
func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// "GET /" は前方一致なので、/ 以外もここへ来る。要求のパスからファイル名を
	// 組み立てる場所を作らないため、完全一致だけを通す。
	if r.URL.Path != "/" {
		s.notFound(w)
		return
	}
	s.writeAsset(w, s.assets["/"])
}

// handleAsset は埋め込んだ資産を返す。経路ごとに1つずつ結ぶ。
func (s *server) handleAsset(route string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, ok := s.assets[route]
		if !ok {
			s.notFound(w)
			return
		}
		s.writeAsset(w, a)
	}
}

// writeAsset は資産を1つ書き出す。
func (s *server) writeAsset(w http.ResponseWriter, a asset) {
	w.Header().Set("Content-Type", a.contentType)
	w.Header().Set("Content-Length", itoa(len(a.body)))
	_, _ = w.Write(a.body)
}

// handleBootstrap は画面の初期値を返す。
func (s *server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	cat := s.cat.forRequest(s.opt.UILang, r.Header.Get("Accept-Language"))
	// 目録の選び方が Accept-Language を見るので、その旨を明示する。
	// no-store なので効く場面は無いが、選び方を応答に書いておく。
	w.Header().Set("Vary", "Accept-Language")

	s.writeJSON(w, bootstrapResponse{
		UI: uiView{
			Lang:     cat.Lang,
			Dir:      cat.Dir,
			Name:     cat.Name,
			Messages: cat.Messages,
		},
		Locales:         localeNames(s.targets),
		Selected:        s.opt.Locale,
		Game:            gamePath(s.opt.Game),
		CanEdit:         true,
		AutosaveDelayMs: int(autosaveDelay.Milliseconds()),
	})
}

// handleLines は1ロケール分の行を返す。
//
// クライアントが指定できるのはロケール名だけで、それも起動時に列挙した一覧との
// 完全一致を通る。パスの組み立ては常にこちら側で行い、要求の値が
// ファイル名の一部になる場所はどこにも無い。
func (s *server) handleLines(w http.ResponseWriter, r *http.Request) {
	cat := s.cat.forRequest(s.opt.UILang, r.Header.Get("Accept-Language"))

	locale := r.URL.Query().Get("locale")
	if locale == "" {
		http.Error(w, s.cat.T(cat, "error.locale_required"), http.StatusBadRequest)
		return
	}
	target := s.target(locale)
	if target == nil {
		// 誤りの文面にロケール名を返さない。返してもよい値ではあるが、
		// 応答へ要求の値を書き戻す癖を作らないほうがよい。
		http.Error(w, s.cat.T(cat, "error.unknown_locale", "locale", ""), http.StatusNotFound)
		return
	}

	file, err := edit.Open(target.Input)
	if err != nil {
		// 誤りの中身（パスを含む）は返さない。記録にも中身は書かない。
		s.logf("open failed locale=%s", target.Locale)
		http.Error(w, s.cat.T(cat, "error.read_failed"), http.StatusInternalServerError)
		return
	}

	sum, ok := s.summaries[target.Locale]
	if !ok {
		// 起動時の突き合わせにそのロケールが無かった場合。ここへ来るのは、
		// publish.DiscoverTargets と internal/diff の見え方が食い違ったとき
		// （起動後にファイルが増えた、など）に限る。件数は空のまま出し、
		// 数字を作らない。作れば、それが画面の独自判断になる。
		sum = diff.Summary{Locale: target.Locale}
	}

	resp := s.buildLines(cat, target, file, sum, s.findings[target.Locale])
	noteRequest(w, " locale=%s lines=%d", target.Locale, len(resp.Lines))
	s.writeJSON(w, resp)
}

// gamePath は画面へ渡すゲームのフォルダーを整える。指定が無ければ空。
//
// [server.displayPath] を通さないのは、ゲームのフォルダーがルートの外にあるのが
// ふつうだからである。displayPath はルートの外を絶対パスのまま返すので、
// 結果は同じになるが、「相対にできるかもしれない」という読み方を残さない。
func gamePath(game string) string {
	if game == "" {
		return ""
	}
	return filepath.ToSlash(game)
}

// autosaveDelay は入力が止まってから自動保存するまでの待ち時間。
//
// 1.5 秒にしてあるのは、打っている途中の1文字ごとに書かないため。作業コピーへ
// 書くとゲームが約2秒でホットリロードするので、打鍵ごとに保存するとゲームの
// 画面が打ちかけの訳で何度も書き換わる。欄から離れたときは待たずに保存する。
const autosaveDelay = 1500 * time.Millisecond

// writeJSON は応答を JSON で書く。状態コードは 200。
func (s *server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		// 書き出しの途中で切れた場合。状態コードはもう送ってあるので
		// 書き換えられない。記録にも中身は書かない。
		s.logf("write failed")
	}
}

// writeJSONStatus は状態コードを指定して JSON を書く。
//
// Content-Type を WriteHeader より先に置く。逆にすると、その応答だけ
// Content-Type が付かない。
func (s *server) writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		s.logf("write failed")
	}
}

// noteRequest は記録に足す1言を入れる。記録していないときは何もしない。
//
// 足してよいのはロケール名と件数だけ。行の中身はここを通らない。
func noteRequest(w http.ResponseWriter, format string, args ...any) {
	sw, ok := w.(*statusWriter)
	if !ok {
		return
	}
	sw.note = fmt.Sprintf(format, args...)
}
