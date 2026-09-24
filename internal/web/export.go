package web

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

/*
書き出し。いま開いているロケールのCSVを、翻訳者が選んだ場所へ保存させる。

置き場を決めるのはブラウザーである。この待ち受けは中身を1本の応答として返し、
Content-Disposition: attachment を付けるだけで、どこへ書くかは知らない。
画面からパスを受け取る経路は作らない。クライアントが指定できるのはロケール名と
形の2つだけで、どちらも数えあげた値との完全一致を通る。パスの組み立ては
今までどおりこちら側で行う（internal/web のパッケージコメント「外へ出さない」）。

保存先を打ち込ませる作りにしなかったのは、この待ち受けが「手元の1人が使う道具」
だからではない。ブラウザーから来た文字列でファイルを書く経路を1つでも開けると、
そこが待ち受けの中でいちばん弱い場所になる。ブラウザーの保存ダイアログなら、
書く先を決めるのはブラウザーで、待ち受けは1バイトも書かない。

出せる形は2つある。どちらも「いま画面に出ているロケール」のものである。

	published   dwloc publish が作るのと同じCSV。キーがハッシュになり、行の
	            並びが台本の順になる。そのままリポジトリへ入れられる形。
	            publish が書く前に見る守り（形、土台の食い違い、失われる訳）を、
	            同じ順で同じだけ通る。
	working     いま書き込んでいるファイルをそのまま写したもの。作業コピーが
	            あればそれ、無ければ公開ファイル自身になる。
*/

const (
	// exportFormPublished と exportFormWorking は form に受け付ける2つの値。
	exportFormPublished = "published"
	exportFormWorking   = "working"

	// exportFallbackName は名前を作れなかったときに使う名前。
	exportFallbackName = "strings.csv"
)

// handleExport は1ロケール分のCSVを、ブラウザーに保存させる形で返す。
func (s *server) handleExport(w http.ResponseWriter, r *http.Request) {
	cat := s.cat.forRequest(s.opt.UILang, r.Header.Get("Accept-Language"))

	locale := r.URL.Query().Get("locale")
	if locale == "" {
		http.Error(w, s.cat.T(cat, "error.locale_required"), http.StatusBadRequest)
		return
	}
	target := s.target(locale)
	if target == nil {
		// 誤りの文面にロケール名を返さない。handleLines と同じ扱いにする。
		http.Error(w, s.cat.T(cat, "error.unknown_locale", "locale", ""), http.StatusNotFound)
		return
	}

	form := r.URL.Query().Get("form")
	var body []byte
	var name string
	var err error
	switch form {
	case exportFormPublished:
		body, name, err = s.exportPublished(w, cat, target)
		if body == nil && err == nil {
			// 応答はもう書いてある（守りに引っかかった）。
			return
		}
	case exportFormWorking:
		body, name, err = s.exportWorking(target)
	default:
		http.Error(w, s.cat.T(cat, "error.unknown_form"), http.StatusBadRequest)
		return
	}
	if err != nil {
		// 誤りの中身（パスを含む）は返さない。記録にも中身は書かない。
		s.logf("export failed locale=%s form=%s", target.Locale, form)
		http.Error(w, s.cat.T(cat, "error.export_failed"), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	noteRequest(w, " locale=%s form=%s bytes=%d", target.Locale, form, len(body))
	if _, err := w.Write(body); err != nil {
		s.logf("write failed")
	}
}

/*
exportPublished は publish が作るのと同じCSVを組み立てる。

書き出す前に、いまコミットされている公開ファイルの訳が1つも失われないことを
確かめる。ここを通さないと、publish が「書くと訳が失われる」と止めるはずの
中身を、翻訳者が自分の手でリポジトリへ写せてしまう。止める場所が画面に無いと、
守りは publish を回した人にしか効かない。

返り値が (nil, "", nil) のときは、応答をこの中で書き終えている。
*/
func (s *server) exportPublished(w http.ResponseWriter, cat *Catalog, target *publish.Target) ([]byte, string, error) {
	/*
		再生順のデータ（data/script_order.csv と data/level_flow.csv）の形を、読む前に
		見る。publish と同じ順（cmd/dwloc の runPublish は再生順のデータの形を最初に
		見る）。閉じない引用符は読み込みの誤り（500）ではなく形の崩れとして断り、
		見出しの行へそのまま書く値の改行は、書き出すと公開ファイルの見出しが壊れる
		ので断る。どちらも画面の指定では通せない。
	*/
	orderHazards, err := publish.CheckOrderShape(s.opt.Root)
	if err != nil {
		return nil, "", err
	}
	if len(orderHazards) > 0 {
		http.Error(w, s.cat.T(cat, "error.export_unsafe_shape",
			"count", strconv.Itoa(len(orderHazards))), http.StatusConflict)
		return nil, "", nil
	}
	data, err := publish.LoadOrder(s.opt.Root)
	if err != nil {
		return nil, "", err
	}

	/*
		入力・いまの公開ファイル・ゲーム側の公開ファイルが、読むと訳や原文を
		取り違える形になっていないかを最初に見る。publish と同じ順（cmd/dwloc の
		runPublish は、形 → 組み立て → 土台の食い違い → 失われる訳、の順で見る）。

		ここを通さないと、publish が止める中身（英語の原文を飲み込んだ訳や、
		黙って落ちた行）を画面からは書き出せる。組み立てより前に見るのは、
		組み立てが読み方の誤り（閉じない引用符）で止まると、形の崩れとして
		伝えられず、書き出しの失敗（500）になるためである。画面には、確かめた
		うえで通す指定（dwloc publish --accept-multiline）が無い。正しい複数行の
		値で止まったときは、文面で dwloc publish を案内する。
	*/
	hazards, err := publish.CheckTargetShape(*target)
	if err != nil {
		return nil, "", err
	}
	if len(hazards) > 0 {
		// 件数だけを返す。どのファイルの何行目かはパスを含むので画面へ出さない。
		// 内訳と直し方は dwloc publish が出す。
		http.Error(w, s.cat.T(cat, "error.export_unsafe_shape",
			"count", strconv.Itoa(len(hazards))), http.StatusConflict)
		return nil, "", nil
	}
	out, _, err := publish.BuildTarget(data, *target)
	if err != nil {
		return nil, "", err
	}

	/*
		ゲーム側の作業コピーを入力にしたときは、その作業コピーが建っている土台が
		コミット済みとそろっているかを先に見る。

		ずれていると、訳は消えないまま古い版へ巻き戻る。下の [publish.CheckLoss] は
		これを捕まえられない。訳は消えておらず、書き換わっただけだからである。

		順番も publish と同じにする（cmd/dwloc の runPublish は、形 → 組み立て →
		土台の食い違い → 失われる訳、の順で見る）。順番が違うと、同じ状態に
		対して画面と publish が別の理由を出す。

		コミット済みの公開ファイルがまだ無いロケール（新しい言語の最初の書き出し）は
		確かめずに通す。巻き戻る先が無いからである。publish の reportBaseDrift も
		同じ場面を通しており、下の [publish.CheckTargetLoss] も出力先の無いときを
		「失うものが無い」と扱う。ここで止めると、ゲームで入れた新しい言語の訳を
		画面からは1行も書き出せず、同じ状態で publish だけが通る。
	*/
	if target.GameBase != "" {
		current, err := os.ReadFile(target.Output)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// コミット済みがまだ無い。巻き戻る先が無いので確かめない。
		case err != nil:
			return nil, "", err
		default:
			drift, err := publish.CheckBase(*target, current)
			if err != nil {
				return nil, "", err
			}
			if drift.Count > 0 {
				// 件数だけを返す。どの行かは訳そのものなので画面へ出さない。
				http.Error(w, s.cat.T(cat, "error.export_would_roll_back",
					"count", strconv.Itoa(drift.Count)), http.StatusConflict)
				return nil, "", nil
			}
		}
	}

	losses, err := publish.CheckTargetLoss(*target, out)
	if err != nil {
		return nil, "", err
	}
	if len(losses) > 0 {
		// 件数だけを返す。どの行かは訳そのものなので画面へ出さない。
		// 内訳は dwloc publish が出す。
		http.Error(w, s.cat.T(cat, "error.export_would_lose",
			"count", strconv.Itoa(len(losses))), http.StatusConflict)
		return nil, "", nil
	}

	// 名前は公開ファイルのものにそろえる。そのままリポジトリへ置ける名前である。
	return out, exportName(target.Output), nil
}

// exportWorking は、いま書き込んでいるファイルをそのまま返す。
//
// 組み立て直さない。「画面で直したものが、そのままの形で欲しい」という求めに
// 対して、別の形を返すのは答えになっていない。
func (s *server) exportWorking(target *publish.Target) ([]byte, string, error) {
	body, err := os.ReadFile(target.Input)
	if err != nil {
		return nil, "", err
	}
	return body, exportName(target.Input), nil
}

/*
exportName は Content-Disposition に載せる名前を作る。

元にするのはこちら側が持っているパスだけで、要求の値は1文字も混ざらない。
それでも通す字を数えあげるのは、ロケール名がディレクトリ名から来ており、
そこに引用符や改行を入れたディレクトリを作れば、ヘッダーを割れるためである。
作れるのはこの待ち受けを動かしている本人だが、守りの範囲を「誰が作ったか」で
決めない。

通らない字が1つでもあれば、名前ごと [exportFallbackName] に落とす。1字ずつ
落として作り直すと、元の名前と見分けの付かない別の名前になりうる。
*/
func exportName(path string) string {
	base := filepath.Base(path)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return exportFallbackName
	}
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return exportFallbackName
		}
	}
	// 先頭のドットだけの名前（".csv"）や、上へ登る名前は使わない。
	if strings.HasPrefix(base, ".") || strings.Contains(base, "..") {
		return exportFallbackName
	}
	return base
}
