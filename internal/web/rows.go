package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"unicode/utf8"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/edit"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
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
	// ID は行（レコード）の ID（[edit.Line.ID]、lineView の id）。書き込む先はこれで
	// 決める。クライアントにパスは書かせない。
	//
	// 物理行の番号では引かない。引用符で囲んだ値に改行があるレコードは複数の
	// 物理行にまたがり、ほかのレコードの値の改行が増えれば、後ろのレコードの行番号が
	// ずれる。ID はセグメントの通し番号で、自分の保存では変わらない
	// （internal/edit の書く前の事後確認で確かめる）。
	ID int `json:"id"`
	// Key はクライアントがその行にあると思っているキー（先頭フィールド）。
	//
	// ID だけで同定すると、409 のあとが危うい。409 を受けた画面は読み直した
	// 内容に自分の編集を載せ直すが、そのあいだによそが行を足したり消したり
	// していると、同じ ID が別のキーの行を指す。訳が別の行へ入り、その行に
	// もとからあった訳が消える。
	//
	// 空なら照合しない。キーを持たない行（キー列が空の作業コピー）があるため
	// で、送られてきたときは必ず照合する。画面は常に送る。
	Key string `json:"key,omitempty"`
	// Translation は差し替える訳。改行を入れてよい（決まったことの 1）。CRLF と単独の
	// CR は internal/edit が LF にそろえて書き、そろえた行の結果には断り（Warning）を付ける。
	// NUL と不正なUTF-8、書くと訳の行がレコードに見える値は internal/edit が拒む。画面は
	// 送る前に改行を LF にそろえ、NUL を落としておく（app.js の sanitize）。
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
	// ID は要求の ID をそのまま返す。
	ID int `json:"id"`
	// Number は、その行の最初の物理行（その行を書き換える前のもの。同じ要求の前の行で
	// 訳の改行が増えたり減ったりしていれば、そのぶんずれている）。同定には使わない。
	// そんな行が無ければ 0。画面は行番号の欄を rowsResponse.Numbers で直す。
	Number int `json:"n"`
	// Saved はこの行が保存されたか。
	Saved bool `json:"saved"`
	// Translation は保存後にモデルから読み直した値。画面はこれで欄を更新する。
	// 書いた値と読み直した値が食い違わないことを、画面で確かめられるようにする。
	Translation string `json:"translation"`
	// Error は保存できなかった理由。Saved が false のときだけ入る。
	Error string `json:"error,omitempty"`
	// Warning は保存はできたが、そのまま受け取ってほしくないときの断り。
	//
	// 1つの欄に連ねる形にしてあるのは、画面がこれを行の下に1行で出すためである
	// （app.js の setRowNote）。欄を増やすたびに画面の描き方を決め直すことになる。
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
	// Numbers は、この保存で行番号（最初と最後の物理行）が変わったデータ行の、いまの
	// 行番号。訳に改行を足したり消したりすると、そのレコードの物理行の数が変わり、後ろの
	// 行の行番号がずれる（ID は変わらない）。画面は ID で行を引いて、行番号の欄と読み上げの
	// 名前を直す。表示のためだけの値で、同定には使わない。変わった行が無ければ省く。
	Numbers []lineNumber `json:"numbers,omitempty"`
	// Counts と Notes は局所更新後のもの。画面はこれで差し替える。
	Counts []countView `json:"counts"`
	Notes  []string    `json:"notes"`
	// Stats は保存後の「数えたもの」。ファイルの物理行の数は、訳の改行で変わる。
	Stats []statView `json:"stats"`
}

// lineNumber は1行の行番号。lineView の n と end と同じ意味で、end は行をまたぐレコード
// だけに入る。
type lineNumber struct {
	ID     int `json:"id"`
	Number int `json:"n"`
	End    int `json:"end,omitempty"`
}

// lineSpans は、データ行の ID ごとの最初と最後の物理行を控える。
func lineSpans(file *edit.File) map[int][2]int {
	out := make(map[int][2]int)
	for _, l := range file.Lines() {
		if l.Kind == edit.KindData {
			out[l.ID] = [2]int{l.Number, l.EndNumber}
		}
	}
	return out
}

// changedNumbers は、before（書き換える前の [lineSpans]）から行番号が変わったデータ行の、
// いまの行番号を、ファイルの順に並べる。
func changedNumbers(before map[int][2]int, file *edit.File) []lineNumber {
	var out []lineNumber
	for _, l := range file.Lines() {
		if l.Kind != edit.KindData || before[l.ID] == [2]int{l.Number, l.EndNumber} {
			continue
		}
		n := lineNumber{ID: l.ID, Number: l.Number}
		if l.EndNumber > l.Number {
			n.End = l.EndNumber
		}
		out = append(out, n)
	}
	return out
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
// 経路はこれ1つで、書き出す先は Target.Input の1つだけである。作業コピーが
// あればそれ、無ければ公開ファイル自身で、publish が入力に選ぶファイルと同じ。
// 新しい訳がコミットする側へ入るのは publish を回したときである。
//
// publish はここでは回さない。保存は「触ったレコードの最終フィールドだけを差し替える」
// であって再生成ではない。再生成すると並びと見出しが作り直され、訳を打ち直す
// 途中の自動保存で「訳が空になった行」が丸ごと消える。
func (s *server) handleRows(w http.ResponseWriter, r *http.Request) {
	cat := s.cat.forRequest(s.opt.UILang, r.Header.Get("Accept-Language"))

	// 本文を先に丸ごと読む。復号しながら流さないのは、復号する前にバイト列が
	// UTF-8 かを見たいからである。
	//
	// [encoding/json] は不正なバイトを U+FFFD へ黙って置き換える。置き換わった
	// あとの文字列はもう「正しい UTF-8」なので、[edit.File.SetTranslation] の
	// UTF-8 の検査（[reason.EditBadUTF8]）はこの経路では1度も立たない。実測
	// （この開発機）では、CP932 の 4 バイト "82 C6 82 EA" を訳に入れて送ると、
	// 断りも警告も出ずに saved で返り、作業コピーには "EF BF BD C6 82 EF BF BD"
	// が書かれた。U+FFFD は正しい UTF-8 なので、この先のどの検査も止めない。
	// publish はそれをコミットする側へ運ぶ。
	//
	// 止めるのは本文のバイト列である。U+FFFD そのものを禁じる形は採らない。
	// 元から U+FFFD を含む訳（壊れた原文をそのまま写した訳など）を拒むことになる。
	// 「送られたバイト列が UTF-8 ではない」は、それ自体がまぎれもない誤りである。
	//
	// ブラウザーは必ず UTF-8 で送るので、画面から使っているかぎりここは立たない。
	// それでも見るのは、この守りが「壊れた値をファイルへ入れない」ために
	// 書かれたものだからである。立たない守りは無い守りと同じである。
	//
	// POST を足すときは、その経路でも同じことをすること。ここに置いてあるのは、
	// 本文の上限（[maxRowsBody]）を持っているのがこの経路だからである。
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRowsBody))
	if err != nil {
		// 誤りの中身は返さない。本文には訳が入っている。
		s.writeError(w, cat, http.StatusBadRequest, "error.bad_request")
		return
	}
	if !utf8.Valid(body) {
		// 何バイト目が壊れていたかも返さない。位置は訳の長さを漏らす。
		s.writeError(w, cat, http.StatusBadRequest, "error.bad_utf8")
		return
	}

	var req rowsRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	// 知らない鍵を拒む。画面と待ち受けが別の版になったとき、送ったつもりの
	// 値が黙って落ちる形にしない。
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		// 誤りの中身は返さない。本文には訳が入っている。
		s.writeError(w, cat, http.StatusBadRequest, "error.bad_request")
		return
	}

	target := s.target(req.Locale)
	if target == nil {
		// 誤りの文面にロケール名を書き戻さない（handleLines と同じ）。
		s.writeError(w, cat, http.StatusNotFound, "error.unknown_locale", "locale", "")
		return
	}
	if req.BaseVersion == "" {
		// 版が無い要求は通さない。通すと「読んだときの状態」を確かめずに
		// 書くことになり、別の窓や publish が書いた内容を消す。
		s.writeError(w, cat, http.StatusBadRequest, "error.version_required")
		return
	}
	if len(req.Edits) == 0 || len(req.Edits) > maxRowsEdits {
		s.writeError(w, cat, http.StatusBadRequest, "error.bad_request")
		return
	}

	out := s.saveRows(cat, target, req)

	// ここから先はファイルに触らない。錠はもう放してある。
	switch {
	case out.conflict:
		s.writeConflict(w, cat, target, out.file)
		return
	case out.errKey != "":
		s.writeErrorResults(w, cat, out.status, out.errKey, out.results)
		return
	}

	// 件数の局所更新。訳が入ったキーを未翻訳から引くだけで、カテゴリの
	// 再判定はしない（13ロケール全部を読み直すことになる）。
	for i := range out.results {
		if !out.results[i].Saved {
			continue
		}
		line, ok := out.file.Line(out.results[i].ID)
		if !ok {
			continue
		}
		s.markFilled(target.Locale, line.Key(), line.Translation() != "")
		// タグの開閉は保存した訳だけで判定し直せる（原文と比べない）。
		// 起動時の判定を、この行についてだけいまの訳で上書きする。
		s.markTags(target.Locale, line.Key(), diff.CheckTags(line.Translation()))
	}

	sum := s.summary(target.Locale)
	filled := s.filledKeys(target.Locale)
	tags := s.tagStates(target.Locale)
	badges := s.badgesByKey(cat, s.findings[target.Locale], filled, tags)
	for i := range out.results {
		if line, ok := out.file.Line(out.results[i].ID); ok {
			out.results[i].Badges = badges[line.Key()]
		}
	}

	noteRequest(w, " locale=%s edits=%d saved=%d", target.Locale, len(req.Edits), out.applied)
	lines := out.file.Lines()
	s.writeJSON(w, rowsResponse{
		Locale:  target.Locale,
		Version: out.file.Version(),
		Results: out.results,
		Numbers: out.numbers,
		// 行数も数え直す。件数だけ返してチップの数を置いていくと、訳を入れた
		// 直後に「32 行」と書いたチップが31行しか出さなくなる。
		Counts: s.buildCounts(cat, target.Locale, sum,
			rowsByCategory(lines, badges)),
		Notes: s.buildNotes(cat, target, sum,
			out.file.Header() != nil && hasSourceColumn(out.file.Header())),
		Stats: s.buildStats(cat, sum, out.file.PhysicalLines(), dataRowCount(lines)),
	})
}

// dataRowCount はデータ行（レコード）の数を数える（linesResponse.Rows と同じ数え方）。
func dataRowCount(lines []edit.Line) int {
	n := 0
	for _, l := range lines {
		if l.Kind == edit.KindData {
			n++
		}
	}
	return n
}

// saveOutcome は [server.saveRows] の結果。
//
// 応答そのものではなく、応答を組み立てるための材料である。錠の中でファイルを
// 読み書きし、錠を放してから応答を組む、という順にするために挟んでいる。
type saveOutcome struct {
	// file は読み込んだファイル。conflict のときは読み直したいまの中身。
	file *edit.File
	// results は行ごとの結果。
	results []rowResult
	// numbers は、保存で行番号が変わったデータ行のいまの行番号（rowsResponse.Numbers）。
	numbers []lineNumber
	// applied は実際にモデルへ入れた行数。
	applied int
	// conflict が true なら 409。ファイルには1バイトも書いていない。
	conflict bool
	// status と errKey は誤りのとき。errKey が空なら成功。
	status int
	errKey string
}

// saveRows は錠の中でファイルを読み、行を差し替えて書く。
//
// 錠はファイルを読んで書き終えるまでで放す。応答の組み立て（1839行ぶんの
// JSON になる）まで抱えると、錠の範囲が「ファイルを守る」よりずっと広く見える。
//
// 保存を直列にするのは、同じファイルへ同時に2つ書くと、片方の版の照合が
// 通ったあとにもう片方が書き終える、という並びが起きうるためである。この錠（saveMu）は
// この待ち受けの中でしか効かない。ほかの dwloc edit や publish との競り合いは、
// [edit.File.Save] が版の照合から rename までを OS の錠で囲んで防ぐ（改善の決定 3）。
//
// 書く先は Target.Input の1つだけである。以前はここで、ゲーム側の作業コピーと
// コミットする側の公開ファイルの両方へ書いていたが、それは成り立たなかった。
// 公開ファイルには未翻訳の行が存在しない（publish が訳の空の行を書かない、
// 移植仕様 R20）ので、翻訳者がいちばんやりたいこと——未訳の行を訳す——は、
// キーで引いても書き戻す先の行が無く、1行も入らない。それでも画面は saved=true を
// 返すので、入ったように見えるぶん黙って落とすより悪い。新しい訳がコミットする側へ
// 入るのは publish を回したときである。
func (s *server) saveRows(cat *Catalog, target *publish.Target, req rowsRequest) saveOutcome {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	file, err := edit.Open(target.Input)
	if err != nil {
		s.logf("open failed locale=%s", target.Locale)
		return saveOutcome{status: http.StatusInternalServerError, errKey: "error.read_failed"}
	}
	if file.Version() != req.BaseVersion {
		// 手前でファイルが変わっている。1バイトも書かずに、いまの中身を返す。
		//
		// 読み取り専用の判定より先に見る（決まったことのそのほか 5）。画面を開いている
		// あいだに、ゲームや表計算ソフトがファイルを書き換えて読み取り専用の形（閉じない
		// 引用符など）になると、先に読み取り専用で断っていたころは、画面が行ごとの理由を
		// 持てずに送り直しを続け、載せ直しも行き先の無い訳への移動も走らなかった。
		// 409 なら、いまの行一覧（読み取り専用の理由つき）を描き直し、載せ直せない訳を
		// 行き先の無い訳として出し続けられる。
		return saveOutcome{file: file, conflict: true}
	}
	if file.ReadOnly() {
		// 読んだときから読み取り専用のファイル（ヘッダーが受理できない、など）。
		// 画面は読み取り専用のファイルでは入力欄を開かないので、ふつうはここへ来ない。
		return saveOutcome{status: http.StatusUnprocessableEntity, errKey: "error.file_readonly"}
	}

	results := make([]rowResult, 0, len(req.Edits))
	applied := 0
	// 書き換える前の行番号。訳の改行で変わった行を、保存のあとに拾う（changedNumbers）。
	before := lineSpans(file)
	for _, e := range req.Edits {
		res := rowResult{ID: e.ID}
		if line, ok := file.Line(e.ID); ok {
			res.Number = line.Number
		}
		if !keyMatches(file, e) {
			// その ID には別のキーの行がある。書くと訳が別の行へ入り、
			// その行にもとからあった訳が消える。書かずに理由を返す。
			res.Error = s.cat.T(cat, "error.row_moved")
			results = append(results, res)
			continue
		}
		switch err := file.SetTranslation(e.ID, e.Translation); {
		case err == nil:
			applied++
			res.Saved = true
			if line, ok := file.Line(e.ID); ok {
				res.Translation = line.Translation()
				if res.Translation != e.Translation {
					// 書いた値が送られた値と違う。いまは、訳の中の CRLF と単独の CR を
					// LF にそろえたときだけ起きる（internal/edit の SetTranslation）。前後の
					// 空白は引用して書くので変わらない。画面の値をこちらで上書きするので、
					// 変えたことを断る（改善の ui-16。画面の出し方は app.js の applyResults）。
					addWarning(&res, s.cat.T(cat, "warn.value_normalized"))
				}
			}
		case errors.Is(err, edit.ErrReadOnly):
			// ファイル全体が読み取り専用。上で弾いているのでここへは来ないが、
			// 来たなら1行ずつ断るのではなくまとめて断る。
			return saveOutcome{status: http.StatusUnprocessableEntity, errKey: "error.file_readonly"}
		default:
			// 編集できない行と、書けない値（NUL・不正なUTF-8・書くと訳の行がレコードに
			// 見える値）。
			// 理由は目録から組み直す。行の中身は含まない。
			res.Error = s.editErrorText(cat, err)
		}
		results = append(results, res)
	}

	if applied == 0 {
		// 1行も書けなかった。書いていないことを状態コードでも示す。
		// 画面はこの結果を見て、その行を「保存できていない行」として残す。
		// ここまででファイルへは1バイトも書いていない（モデルを触っただけ）。
		return saveOutcome{
			results: results,
			status:  http.StatusUnprocessableEntity,
			errKey:  "error.no_row_saved",
		}
	}

	if err := file.Save(); err != nil {
		var recheck *edit.RecheckError
		if errors.As(err, &recheck) {
			// 書く直前にファイル全体を読み直すと、編集モデルと同じに読めなかった
			// （書く前の事後確認の後半）。1バイトも書いていない。
			s.logf("save check failed locale=%s line=%d", target.Locale, recheck.Line)
			return saveOutcome{
				results: s.recheckResults(cat, results, recheck),
				status:  http.StatusUnprocessableEntity,
				errKey:  "error.save_check_failed",
			}
		}
		if errors.Is(err, edit.ErrConflict) {
			// Save は書く直前にもう一度版を照合する。ここで弾かれたときも
			// このファイルには1バイトも書いていない。手元の File はもう古いので
			// 読み直す。
			fresh, err := edit.Open(target.Input)
			if err != nil {
				s.logf("open failed locale=%s", target.Locale)
				return saveOutcome{status: http.StatusInternalServerError, errKey: "error.read_failed"}
			}
			return saveOutcome{file: fresh, conflict: true}
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
		//
		// 端末には理由を書く。応答には出さない（すぐ上の注記）ので、書かないと
		// 「なぜ書けないか」がどこにも出ない。待っても直らない失敗——ゲームの
		// フォルダーへ書けない PC（Program Files の既定の権限、権限を絞った端末、
		// 読み取り専用で繋いだ外付け）——では、画面は開くのに保存だけが落ち
		// 続けるので、権限の話が読めないと直し方に手が届かない。edit は --game を
		// 省いてもゲームのフォルダーを探すので、この道は指定を打っていない人にも
		// 起きる（そのときの逃げ道は --no-game）。
		//
		// 誤りに入るのはパスと OS の文言だけで、行の中身は入らない
		// （[TestGameSaveLogHasNoRowContent] が頁全体で見張っている）。
		s.logf("save failed locale=%s: %v", target.Locale, err)
		return saveOutcome{
			results: results,
			status:  http.StatusServiceUnavailable,
			errKey:  "error.save_failed",
		}
	}

	return saveOutcome{file: file, results: results, numbers: changedNumbers(before, file), applied: applied}
}

// recheckResults は、書く前の事後確認が外れたときの行ごとの結果を作る。
//
// ファイルには1バイトも書いていないので、どの行の Saved も倒す（倒さずに返すと、
// 画面が「保存できた行」と読んで未保存の控えを捨てる）。外れた行（recheck が指す ID）
// にだけ理由を付ける。画面はその行を保存できない行にして送り直しを止め、理由の無い
// ほかの行は未保存のまま送り直す（422 は送り直す状態コード）。次の要求には外れた行が
// 入らないので、ほかの行は書ける。
func (s *server) recheckResults(cat *Catalog, results []rowResult, recheck *edit.RecheckError) []rowResult {
	why := s.cat.T(cat, "error.not_editable",
		"line", itoa(recheck.Line), "reason", s.reasonText(cat, recheck.Cause))
	for i := range results {
		results[i].Saved = false
		results[i].Translation = ""
		results[i].Warning = ""
		if results[i].ID == recheck.ID && results[i].Error == "" {
			results[i].Error = why
		}
	}
	return results
}

// addWarning は行の断りを1つ足す。既にあれば後ろに連ねる。
//
// 上書きしないのは、先に付いた断り（値を正規化した、など）が消えるためである。
// 区切りを空白1つにしてあるのは、画面が1行で出すのに合わせてある。
func addWarning(res *rowResult, text string) {
	if text == "" {
		return
	}
	if res.Warning == "" {
		res.Warning = text
		return
	}
	res.Warning += " " + text
}

// editErrorText は internal/edit が返した誤りを、画面に出す文面にする。
//
// 外枠（「12行目は編集できない: …」）も、その中に入る理由も目録から引く。
// err.Error() をそのまま返すと、英語の画面でその行だけ日本語になる。
//
// 知らない型の誤りは Error() をそのまま返す。訳されていない文が出るほうが、
// 何も出ないよりよい。internal/edit の文面に行の中身は入っていないので、
// そのまま返しても訳や原文が漏れることはない。
func (s *server) editErrorText(cat *Catalog, err error) string {
	var notEditable *edit.NotEditableError
	if errors.As(err, &notEditable) {
		if notEditable.Line == 0 {
			// そんな行が無い。外枠の「N行目は」を書けないので、理由だけを出す。
			return s.reasonText(cat, notEditable.Cause)
		}
		return s.cat.T(cat, "error.not_editable",
			"line", itoa(notEditable.Line), "reason", s.reasonText(cat, notEditable.Cause))
	}
	var invalid *edit.InvalidValueError
	if errors.As(err, &invalid) {
		return s.cat.T(cat, "error.invalid_value",
			"line", itoa(invalid.Line), "reason", s.reasonText(cat, invalid.Cause))
	}
	return err.Error()
}

// keyMatches は、要求の ID が指す行がクライアントの思っているキーの行かを返す。
//
// キーを送ってこない要求（キー列が空の行）は照合しない。行が無いときも通す。
// 行が無いことは [edit.File.SetTranslation] が断るので、理由を2か所で作らない。
func keyMatches(file *edit.File, e rowEdit) bool {
	if e.Key == "" {
		return true
	}
	line, ok := file.Line(e.ID)
	if !ok {
		return true
	}
	return line.Key() == e.Key
}

// writeConflict は 409 といまの行一覧を返す。ファイルには1バイトも書いていない。
func (s *server) writeConflict(w http.ResponseWriter, cat *Catalog, target *publish.Target,
	file *edit.File) {

	sum := s.summary(target.Locale)
	current := s.buildLines(cat, target, file, sum, s.findings[target.Locale])
	noteRequest(w, " locale=%s conflict", target.Locale)
	s.writeJSONStatus(w, http.StatusConflict, conflictResponse{
		Conflict: true,
		Message:  s.cat.T(cat, "error.conflict"),
		Current:  current,
	})
}

// writeError は誤りを JSON で返す。文面は目録から引く。
//
// kv は文面の {name} を埋める組。埋めずに残すと、翻訳者の画面に {locale} の
// ような鍵がそのまま出る。
func (s *server) writeError(w http.ResponseWriter, cat *Catalog, status int, key string, kv ...string) {
	s.writeErrorResults(w, cat, status, key, nil, kv...)
}

// writeErrorResults は誤りと行ごとの理由を返す。
func (s *server) writeErrorResults(w http.ResponseWriter, cat *Catalog, status int, key string, results []rowResult, kv ...string) {
	s.writeJSONStatus(w, status, errorResponse{
		Message: s.cat.T(cat, key, kv...),
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
// 持つのは「訳が入ったキー」と「保存した行のタグの開閉」の2つだけである。
// ほかのカテゴリの再判定はしない。判定し直すには 13ロケール全部を読むことになり、
// 1回の自動保存でやる処理ではない。できないことは画面の断り書き
// （note.counts_local）で正直に伝える。
//
// タグの開閉だけ判定し直すのは、その判定が保存した行の訳だけで決まるからである
// （[diff.CheckTags]。原文とも他のロケールとも比べない）。判定そのものは
// internal/diff のものをそのまま呼ぶので、ここに判断は増えていない。
type editOverlay struct {
	mu sync.Mutex
	// untranslated は起動時に「未翻訳」と判定されたキー。ロケール名で引く。
	untranslated map[string]map[string]struct{}
	// filled は起動後に訳が入ったキー。ロケール名で引く。
	// 訳を消したときはここから外れるので、件数は両方向に動く。
	filled map[string]map[string]struct{}
	// tagged は起動時に「タグの開閉がそろわない行」と判定されたキー。
	// 件数をどちらへ動かすか（直したのか、壊したのか）を決めるために持つ。
	tagged map[string]map[string]struct{}
	// tags は起動後に保存した行の、いまの訳での判定。触った行だけ入る。
	// 起動時の判定をこの行についてだけ上書きするので、直せば外れ、壊せば付く。
	tags map[string]map[string]tagState
}

// tagState は起動後に保存した行の、タグの開閉のいまの判定。
type tagState struct {
	// bad は開閉がそろっていないか。
	bad bool
	// note は bad のときの注記（[diff.TagBalance.Note]）。バッジに添える。
	note reason.Reason
}

func newEditOverlay() *editOverlay {
	return &editOverlay{
		untranslated: make(map[string]map[string]struct{}),
		filled:       make(map[string]map[string]struct{}),
		tagged:       make(map[string]map[string]struct{}),
		tags:         make(map[string]map[string]tagState),
	}
}

// addTagged は起動時の「タグの開閉がそろわない行」を控える。
func (o *editOverlay) addTagged(locale, key string) {
	if key == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.tagged[locale] == nil {
		o.tagged[locale] = make(map[string]struct{})
	}
	o.tagged[locale][key] = struct{}{}
}

// markTags は保存した行のタグの開閉を控える。
//
// そろっている行も控える。起動時に当たっていた行を直したときは、「もう当たら
// ない」ことを覚えていないと起動時のバッジが残る。
func (s *server) markTags(locale, key string, b diff.TagBalance) {
	if key == "" {
		return
	}
	o := s.overlay
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.tags[locale] == nil {
		o.tags[locale] = make(map[string]tagState)
	}
	o.tags[locale][key] = tagState{bad: !b.Balanced(), note: b.Note()}
}

// tagStates は起動後に保存した行のタグの判定の写しを返す。
func (s *server) tagStates(locale string) map[string]tagState {
	o := s.overlay
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make(map[string]tagState, len(o.tags[locale]))
	for key, st := range o.tags[locale] {
		out[key] = st
	}
	return out
}

// tagsTouched は起動後にタグの判定をし直した行の数を返す。
func (s *server) tagsTouched(locale string) int {
	o := s.overlay
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.tags[locale])
}

// tagDelta は「タグの開閉がそろわない行」の件数を、起動時からどれだけ動かすかを返す。
//
// 起動時に当たっていた行を直せば 1 減り、当たっていなかった行を壊せば 1 増える。
// 当たっていた行を別の形で壊したままなら動かない。
func (s *server) tagDelta(locale string) int {
	o := s.overlay
	o.mu.Lock()
	defer o.mu.Unlock()
	n := 0
	for key, st := range o.tags[locale] {
		_, was := o.tagged[locale][key]
		switch {
		case was && !st.bad:
			n--
		case !was && st.bad:
			n++
		}
	}
	return n
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
