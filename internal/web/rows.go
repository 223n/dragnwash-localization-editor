package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/edit"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// maxRowsBody は POST /api/rows の本文の上限。
//
// 1839行のファイル全部を1回で送っても届く大きさにしてある。上限を置くのは、
// 手が滑って巨大な本文を送ったときに待ち受けが記憶を食い尽くさないため。
const maxRowsBody = 4 << 20

// maxRowsEdits は1回の要求で受ける行数の上限。
//
// 実データで最も行数の多いロケールより十分多くしてある。1回の要求が
// ファイル全体を書き換えることはありうる（引き継ぎのあとの一括入力など）ので、
// 小さく絞らない。
const maxRowsEdits = 20000

// rowEdit は1行ぶんの書き換え要求。
type rowEdit struct {
	// Line は1始まりの物理行番号。行の同定はこれだけで行う。
	// クライアントにパスもキーも書かせない。
	Line int `json:"line"`
	// Translation は差し替える訳。CR / LF / NUL / 不正なUTF-8 は
	// internal/edit が拒む。画面側は送る前に置き換えておくこと。
	Translation string `json:"translation"`
}

// rowsRequest は POST /api/rows の本文。
type rowsRequest struct {
	Locale string `json:"locale"`
	// BaseVersion は画面が読んだときのファイルの版。合わなければ1バイトも書かない。
	BaseVersion string    `json:"baseVersion"`
	Edits       []rowEdit `json:"edits"`
}

// rowResult は1行ぶんの結果。
//
// 行ごとに返すのは、1行の失敗で残り全部を巻き添えにしないため。失敗した行は
// 画面が「保存できていない行」として残し、翻訳者の入力を捨てない。
type rowResult struct {
	Line int `json:"line"`
	// Saved はこの行が保存されたか。
	Saved bool `json:"saved"`
	// Translation は保存後にモデルから読み直した値。画面はこれで欄を更新する。
	// 書いた値と読み直した値が食い違わないことを、画面で確かめられるようにする。
	Translation string `json:"translation"`
	// Error は保存できなかった理由。Saved が false のときだけ入る。
	Error string `json:"error,omitempty"`
	// Warning は保存はできたが、書いた値と読み直した値が違うときの断り。
	Warning string `json:"warning,omitempty"`
	// Badges は局所更新後の状態バッジ。訳が入った行から「未翻訳」が消える。
	Badges []badgeView `json:"badges"`
}

// rowsResponse は POST /api/rows の応答（成功時）。
type rowsResponse struct {
	Locale string `json:"locale"`
	// Version は保存後のファイルの版。画面は次の保存要求にこれを載せる。
	Version string      `json:"version"`
	Results []rowResult `json:"results"`
	// Counts と Notes は局所更新後のもの。画面はこれで差し替える。
	Counts []countView `json:"counts"`
	Notes  []string    `json:"notes"`
}

// conflictResponse は 409 の応答。
//
// 現在の行一覧を載せるのは、画面に読み直させるためである。画面はこれを描いた
// うえで、未保存の編集をどう扱うかを人に選ばせる。黙って上書きも、黙って破棄も
// させない。
type conflictResponse struct {
	Conflict bool   `json:"conflict"`
	Message  string `json:"message"`
	// Current はいまのファイルの中身。Version も入っている。
	Current *linesResponse `json:"current"`
}

// errorResponse は保存できなかったときの応答。
//
// 行ごとの理由が要る場面（編集できない行への要求など）があるので、
// http.Error の平文ではなく JSON で返す。
type errorResponse struct {
	Message string      `json:"message"`
	Results []rowResult `json:"results,omitempty"`
}

// handleRows は訳を保存する。
//
// 経路はこれ1つで、書き出す先は publish.DiscoverTargets が返した Target.Input。
// 作業コピーがあればそれ、無ければ公開ファイル自身。画面が編集するファイルと
// publish が入力に選ぶファイルを同じにしておくと、画面の内容と公開結果が
// 食い違う余地が無い。
//
// publish は回さない。保存は「触った行の最終フィールドだけを差し替える」であって
// 再生成ではない。再生成すると並びと見出しが作り直され、訳を打ち直す途中の
// 自動保存で「訳が空になった行」が丸ごと消える。
func (s *server) handleRows(w http.ResponseWriter, r *http.Request) {
	cat := s.cat.forRequest(s.opt.UILang, r.Header.Get("Accept-Language"))

	var req rowsRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRowsBody))
	// 知らない鍵を拒む。画面と待ち受けが別の版になったとき、送ったつもりの
	// 値が黙って落ちる形にしない。
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		// 誤りの中身は返さない。本文には訳が入っている。
		s.writeError(w, cat, http.StatusBadRequest, "error.bad_request", nil)
		return
	}

	target := s.target(req.Locale)
	if target == nil {
		// 誤りの文面にロケール名を書き戻さない（handleLines と同じ）。
		s.writeError(w, cat, http.StatusNotFound, "error.unknown_locale", nil)
		return
	}
	if req.BaseVersion == "" {
		// 版が無い要求は通さない。通すと「読んだときの状態」を確かめずに
		// 書くことになり、別の窓や publish が書いた内容を消す。
		s.writeError(w, cat, http.StatusBadRequest, "error.version_required", nil)
		return
	}
	if len(req.Edits) == 0 || len(req.Edits) > maxRowsEdits {
		s.writeError(w, cat, http.StatusBadRequest, "error.bad_request", nil)
		return
	}

	// 保存は直列にする。同じファイルへ同時に2つ書くと、片方の版の照合が
	// 通ったあとにもう片方が書き終える、という並びが起きうる。
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	s.applyRows(w, cat, target, req)
}

// applyRows は版を照合し、行を差し替えて保存する。
func (s *server) applyRows(w http.ResponseWriter, cat *Catalog, target *publish.Target, req rowsRequest) {
	file, err := edit.Open(target.Input)
	if err != nil {
		s.logf("open failed locale=%s", target.Locale)
		s.writeError(w, cat, http.StatusInternalServerError, "error.read_failed", nil)
		return
	}
	if file.ReadOnly() {
		// ヘッダーが受理できないファイル。理由は internal/edit の文面をそのまま出す。
		s.writeError(w, cat, http.StatusUnprocessableEntity, "error.file_readonly", nil)
		return
	}
	if file.Version() != req.BaseVersion {
		// 手前でファイルが変わっている。1バイトも書かずに、いまの中身を返す。
		s.writeConflict(w, cat, target, file)
		return
	}

	results := make([]rowResult, 0, len(req.Edits))
	applied := 0
	for _, e := range req.Edits {
		res := rowResult{Line: e.Line}
		switch err := file.SetTranslation(e.Line, e.Translation); {
		case err == nil:
			applied++
			res.Saved = true
			if line, ok := file.Line(e.Line); ok {
				res.Translation = line.Translation()
				if res.Translation != e.Translation {
					// CSV として書き戻して読み直すと値が変わる場合
					// （internal/edit の doc.go が挙げている前後の空白など）。
					// 画面の値をこちらで上書きするので、変えたことを断る。
					res.Warning = s.cat.T(cat, "warn.value_normalized")
				}
			}
		case errors.Is(err, edit.ErrReadOnly):
			// ファイル全体が読み取り専用。上で弾いているのでここへは来ないが、
			// 来たなら1行ずつ断るのではなくまとめて断る。
			s.writeError(w, cat, http.StatusUnprocessableEntity, "error.file_readonly", nil)
			return
		default:
			// 編集できない行と、書けない値（改行・NUL・不正なUTF-8）。
			// 理由は internal/edit が持つ文面をそのまま出す。行の中身は含まない。
			res.Error = err.Error()
		}
		results = append(results, res)
	}

	if applied == 0 {
		// 1行も書けなかった。書いていないことを状態コードでも示す。
		// 画面はこの結果を見て、その行を「保存できていない行」として残す。
		s.writeErrorResults(w, cat, http.StatusUnprocessableEntity, "error.no_row_saved", results)
		return
	}

	if err := file.Save(); err != nil {
		if errors.Is(err, edit.ErrConflict) {
			// Save は書く直前にもう一度版を照合する。ここで弾かれたときも
			// 1バイトも書いていない。読み直して返す。
			s.writeConflictReload(w, cat, target)
			return
		}
		// 書けなかった。誤りの中身（パスを含む）は返さない。
		//
		// Saved を全部倒してから返す。ここまでの Saved は「モデルが受け付けた」
		// という意味でしかなく、ファイルには1バイトも入っていない。倒さずに返すと、
		// 画面が「保存できた行」と読んで未保存の控えを捨てる。訳が消える。
		for i := range results {
			results[i].Saved = false
			results[i].Translation = ""
		}
		// 503 にするのは、この失敗のほとんどが待てば直るものだからである。
		// Windows では、ゲームがホットリロードでファイルを開いている最中の
		// rename が共有違反で失敗する（実測で、読み手がいると数パーセント）。
		// 画面はこれを見て少し待ってからもう一度送る。
		s.logf("save failed locale=%s", target.Locale)
		s.writeErrorResults(w, cat, http.StatusServiceUnavailable, "error.save_failed", results)
		return
	}

	// 件数の局所更新。訳が入ったキーを未翻訳から引くだけで、カテゴリの
	// 再判定はしない（13ロケール全部を読み直すことになる）。
	for i := range results {
		if !results[i].Saved {
			continue
		}
		line, ok := file.Line(results[i].Line)
		if !ok {
			continue
		}
		s.markFilled(target.Locale, line.Key(), line.Translation() != "")
	}

	sum := s.summary(target.Locale)
	filled := s.filledKeys(target.Locale)
	badges := s.badgesByKey(cat, s.findings[target.Locale], filled)
	for i := range results {
		if line, ok := file.Line(results[i].Line); ok {
			results[i].Badges = badges[line.Key()]
		}
	}

	noteRequest(w, " locale=%s edits=%d saved=%d", target.Locale, len(req.Edits), applied)
	s.writeJSON(w, rowsResponse{
		Locale:  target.Locale,
		Version: file.Version(),
		Results: results,
		Counts:  s.buildCounts(cat, target.Locale, sum),
		Notes:   s.buildNotes(cat, target.Locale, sum, file.Header() != nil && hasSourceColumn(file.Header())),
	})
}

// writeConflict は 409 といまの行一覧を返す。1バイトも書いていない。
func (s *server) writeConflict(w http.ResponseWriter, cat *Catalog, target *publish.Target, file *edit.File) {
	sum := s.summary(target.Locale)
	current := s.buildLines(cat, target.Locale, s.displayPath(target.Input), file,
		sum, s.findings[target.Locale])
	noteRequest(w, " locale=%s conflict", target.Locale)
	s.writeJSONStatus(w, http.StatusConflict, conflictResponse{
		Conflict: true,
		Message:  s.cat.T(cat, "error.conflict"),
		Current:  current,
	})
}

// writeConflictReload はファイルを読み直してから 409 を返す。
//
// [edit.File.Save] が書く直前の照合で弾いたとき、手元の File はもう古い。
func (s *server) writeConflictReload(w http.ResponseWriter, cat *Catalog, target *publish.Target) {
	file, err := edit.Open(target.Input)
	if err != nil {
		s.logf("open failed locale=%s", target.Locale)
		s.writeError(w, cat, http.StatusInternalServerError, "error.read_failed", nil)
		return
	}
	s.writeConflict(w, cat, target, file)
}

// writeError は誤りを JSON で返す。文面は目録から引く。
func (s *server) writeError(w http.ResponseWriter, cat *Catalog, status int, key string, results []rowResult) {
	s.writeErrorResults(w, cat, status, key, results)
}

// writeErrorResults は誤りと行ごとの理由を返す。
func (s *server) writeErrorResults(w http.ResponseWriter, cat *Catalog, status int, key string, results []rowResult) {
	s.writeJSONStatus(w, status, errorResponse{
		Message: s.cat.T(cat, key),
		Results: results,
	})
}

// summary は起動時のスナップショットを引く。無ければ空を返す。
//
// 空を返すのは handleLines と同じ扱い。数字を作らない。
func (s *server) summary(locale string) diff.Summary {
	if sum, ok := s.summaries[locale]; ok {
		return sum
	}
	return diff.Summary{Locale: locale}
}

// hasSourceColumn はヘッダーに source_en 列があるかを返す。
func hasSourceColumn(header []string) bool {
	return columnIndex(header).source >= 0
}

// editOverlay は起動後の局所更新を持つ。
//
// 持つのは「訳が入ったキー」だけである。カテゴリの再判定はしない。判定し直すには
// 13ロケール全部を読むことになり、1回の自動保存でやる処理ではない。できないことは
// 画面の断り書き（note.counts_local）で正直に伝える。
type editOverlay struct {
	mu sync.Mutex
	// untranslated は起動時に「未翻訳」と判定されたキー。ロケール名で引く。
	untranslated map[string]map[string]struct{}
	// filled は起動後に訳が入ったキー。ロケール名で引く。
	// 訳を消したときはここから外れるので、件数は両方向に動く。
	filled map[string]map[string]struct{}
}

func newEditOverlay() *editOverlay {
	return &editOverlay{
		untranslated: make(map[string]map[string]struct{}),
		filled:       make(map[string]map[string]struct{}),
	}
}

// addUntranslated は起動時の「未翻訳」を控える。
func (o *editOverlay) addUntranslated(locale, key string) {
	if key == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.untranslated[locale] == nil {
		o.untranslated[locale] = make(map[string]struct{})
	}
	o.untranslated[locale][key] = struct{}{}
}

// markFilled は訳の有無を控える。
func (s *server) markFilled(locale, key string, has bool) {
	if key == "" {
		return
	}
	o := s.overlay
	o.mu.Lock()
	defer o.mu.Unlock()
	if !has {
		delete(o.filled[locale], key)
		return
	}
	if o.filled[locale] == nil {
		o.filled[locale] = make(map[string]struct{})
	}
	o.filled[locale][key] = struct{}{}
}

// filledKeys は起動後に訳が入ったキーの写しを返す。
func (s *server) filledKeys(locale string) map[string]struct{} {
	o := s.overlay
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make(map[string]struct{}, len(o.filled[locale]))
	for key := range o.filled[locale] {
		out[key] = struct{}{}
	}
	return out
}

// untranslatedFilled は起動時に「未翻訳」だった行のうち、訳が入った数を返す。
//
// 未翻訳の件数からこれを引く。引くだけで、カテゴリの割り当ては変えない。
func (s *server) untranslatedFilled(locale string) int {
	o := s.overlay
	o.mu.Lock()
	defer o.mu.Unlock()
	n := 0
	for key := range o.filled[locale] {
		if _, ok := o.untranslated[locale][key]; ok {
			n++
		}
	}
	return n
}
