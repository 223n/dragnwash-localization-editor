package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/edit"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// 保存の試験で使う原文。キーはこの原文から計算するので、突き合わせが
// 実データと同じ経路を通る（手で書いた16桁を置くと、publish で捨てられる行に化ける）。
const (
	srcHello = "Hello?"
	srcBye   = "Goodbye."

	// jaHello は作業コピーに最初から入っている訳。
	jaHello = "もしもし？"
	// jaTyped は試験で打ち込む訳。記録に出ていないことの確認にも使う。
	jaTyped = "さようなら。"
)

// newEditRoot は保存を試すための小さな翻訳リポジトリを作る。
//
// newTestRoot と分けてあるのは、作業コピーを置くためである。作業コピーが無いと
// internal/diff は「未翻訳」を判定できず（0 件ではなく「判定していません」になる）、
// 件数の局所更新を確かめられない。
//
// 行番号（作業コピー）:
//
//	1 ヘッダー / 2 空行 / 3 見出し / 4 見出し / 5 訳あり / 6 訳が空 / 7 空行
func newEditRoot(t *testing.T) string {
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
		"L01 Ryan,intro,Ryan_1_intro,1,line:aaaaaaaa," + key.For(srcHello) + ",Ryan,",
		"L01 Ryan,intro,Ryan_1_intro,2,line:bbbbbbbb," + key.For(srcBye) + ",Ryan,",
		"",
	}, "\n"))

	write("Translations/_discovered/ja.working.csv", strings.Join([]string{
		"key,section,node,order,speaker,source_en,translation",
		"",
		"# ===== Level 1: Ryan (Sunny) =====",
		"# --- intro: Ryan_1_intro ---",
		key.For(srcHello) + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + "," + jaHello,
		key.For(srcBye) + ",L01 Ryan,Ryan_1_intro,2,Ryan," + srcBye + ",",
		"",
	}, "\n"))

	// 公開ファイルも置く。実データと同じ形（作業コピーが入力、公開ファイルが出力）に
	// しておかないと、突き合わせが通らない経路を試すことになる。
	write("Translations/ja/strings.csv", strings.Join([]string{
		"key,section,node,order,speaker,translation",
		"",
		"# --- intro: Ryan_1_intro ---",
		key.For(srcHello) + ",L01 Ryan,Ryan_1_intro,1,Ryan," + jaHello,
		"",
	}, "\n"))

	return root
}

// inputPath は待ち受けが書き出す先（Target.Input）を返す。
func inputPath(t *testing.T, s *server, locale string) string {
	t.Helper()
	target := s.target(locale)
	if target == nil {
		t.Fatalf("%s が対象に無い", locale)
	}
	return target.Input
}

// readFile はファイルをそのまま読む。
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// doPost は本文付きの要求を送る。
//
// headers に "" を渡した鍵は消す。Origin と Content-Type を落とした要求を
// 作るために要る。
func doPost(t *testing.T, s *server, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Host = "127.0.0.1:" + testPort
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", "http://127.0.0.1:"+testPort)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
			continue
		}
		req.Header.Set(k, v)
	}
	req.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	return rec
}

// saveBody は保存の本文を組む。
func saveBody(t *testing.T, locale, version string, edits ...rowEdit) string {
	t.Helper()
	body, err := json.Marshal(rowsRequest{Locale: locale, BaseVersion: version, Edits: edits})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// save は1行だけ保存する。応答をそのまま返す。
func save(t *testing.T, s *server, locale, version string, edits ...rowEdit) *httptest.ResponseRecorder {
	t.Helper()
	return doPost(t, s, "/api/rows", saveBody(t, locale, version, edits...), nil)
}

func TestSaveChangesOnlyTheTouchedLine(t *testing.T) {
	// 保存は「触った行の最終フィールドだけを差し替える」。触っていない行が
	// 1バイトでも変わると、この道具は publish の出力と食い違い始める。
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	path := inputPath(t, s, "ja")
	before := readFile(t, path)

	lines := getLines(t, s, "ja")
	rec := save(t, s, "ja", lines.Version, rowEdit{ID: 6, Translation: jaTyped})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	got := decode[rowsResponse](t, rec.Body.Bytes())
	if len(got.Results) != 1 || !got.Results[0].Saved {
		t.Fatalf("結果が %+v", got.Results)
	}
	if got.Results[0].Translation != jaTyped {
		t.Errorf("読み直した値が %q", got.Results[0].Translation)
	}
	if got.Version == "" || got.Version == lines.Version {
		t.Errorf("版が %q（保存で変わるはず）", got.Version)
	}

	after := readFile(t, path)
	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")
	if len(beforeLines) != len(afterLines) {
		t.Fatalf("行数が %d から %d に変わった", len(beforeLines), len(afterLines))
	}
	for i := range beforeLines {
		number := i + 1
		if number == 6 {
			continue
		}
		if beforeLines[i] != afterLines[i] {
			t.Errorf("%d行目が変わった\n前: %q\n後: %q", number, beforeLines[i], afterLines[i])
		}
	}
	if want := key.For(srcBye) + ",L01 Ryan,Ryan_1_intro,2,Ryan," + srcBye + "," + jaTyped; afterLines[5] != want {
		t.Errorf("6行目が %q、%q を期待", afterLines[5], want)
	}

	// 版は保存後のファイルと合っていること。合っていないと、次の保存が
	// 自分の書いた内容を「手前で変わった」と見て 409 を返し続ける。
	again := save(t, s, "ja", got.Version, rowEdit{ID: 6, Translation: jaTyped + "！"})
	if again.Code != http.StatusOK {
		t.Errorf("続けての保存が %d\n%s", again.Code, again.Body.String())
	}
}

func TestSaveRejectsStaleVersion(t *testing.T) {
	// 版が合わなければ1バイトも書かない。書くと、別の窓や publish の再生成が
	// 書いた内容を黙って消すことになる。
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	path := inputPath(t, s, "ja")
	before := readFile(t, path)

	stale := strings.Repeat("0", 64)
	rec := save(t, s, "ja", stale, rowEdit{ID: 6, Translation: jaTyped})
	if rec.Code != http.StatusConflict {
		t.Fatalf("状態コードが %d、409 を期待\n%s", rec.Code, rec.Body.String())
	}
	got := decode[conflictResponse](t, rec.Body.Bytes())
	if !got.Conflict || got.Current == nil {
		t.Fatalf("応答が %+v", got)
	}
	if got.Current.Version == "" || got.Current.Version == stale {
		t.Errorf("いまの版が %q", got.Current.Version)
	}
	if len(got.Current.Lines) == 0 {
		t.Error("いまの行一覧が空。読み直せない")
	}
	if after := readFile(t, path); after != before {
		t.Error("409 なのにファイルが変わった")
	}
}

func TestSaveConflictKeepsWhatTheOtherWriterWrote(t *testing.T) {
	// 手前で誰かが書いたあとに保存しても、その内容を消さない。
	// 409 のあとに画面がどちらを載せるかを選ぶので、待ち受けは黙って上書きしない。
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	path := inputPath(t, s, "ja")
	lines := getLines(t, s, "ja")

	outside := strings.ReplaceAll(readFile(t, path), srcBye+",", srcBye+",よそからの訳")
	if err := os.WriteFile(path, []byte(outside), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := save(t, s, "ja", lines.Version, rowEdit{ID: 6, Translation: jaTyped})
	if rec.Code != http.StatusConflict {
		t.Fatalf("状態コードが %d、409 を期待", rec.Code)
	}
	if after := readFile(t, path); after != outside {
		t.Error("よそが書いた内容が消えた")
	}
	got := decode[conflictResponse](t, rec.Body.Bytes())
	found := false
	for _, line := range got.Current.Lines {
		if line.Number == 6 && line.Translation == "よそからの訳" {
			found = true
		}
	}
	if !found {
		t.Error("409 の応答が、いまファイルにある訳を返していない")
	}
}

func TestWriteNeedsOriginAndJSON(t *testing.T) {
	// 素のフォーム送信では Origin も Content-Type も満たせない。この2つを
	// 要求することで、他の頁から書き込ませない。
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	path := inputPath(t, s, "ja")
	before := readFile(t, path)
	lines := getLines(t, s, "ja")
	body := saveBody(t, "ja", lines.Version, rowEdit{ID: 6, Translation: jaTyped})

	cases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"Origin が無い", map[string]string{"Origin": ""}, http.StatusNotFound},
		{"よその Origin", map[string]string{"Origin": "http://evil.example"}, http.StatusNotFound},
		{"綴りが違う Origin", map[string]string{"Origin": "https://127.0.0.1:" + testPort}, http.StatusNotFound},
		{"別ポートの Origin", map[string]string{"Origin": "http://127.0.0.1:1"}, http.StatusNotFound},
		{"Content-Type が無い", map[string]string{"Content-Type": ""}, http.StatusUnsupportedMediaType},
		{"フォーム送信", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, http.StatusUnsupportedMediaType},
		{"text/plain", map[string]string{"Content-Type": "text/plain"}, http.StatusUnsupportedMediaType},
		{"multipart", map[string]string{"Content-Type": "multipart/form-data; boundary=x"}, http.StatusUnsupportedMediaType},
	}
	for _, tc := range cases {
		rec := doPost(t, s, "/api/rows", body, tc.headers)
		if rec.Code != tc.want {
			t.Errorf("%s: 状態コードが %d、%d を期待", tc.name, rec.Code, tc.want)
		}
		if after := readFile(t, path); after != before {
			t.Fatalf("%s: 書き込みの守りを抜けてファイルが変わった", tc.name)
		}
	}

	// charset 付きは通す。fetch が付けることがある。
	rec := doPost(t, s, "/api/rows", body,
		map[string]string{"Content-Type": "application/json; charset=utf-8"})
	if rec.Code != http.StatusOK {
		t.Errorf("charset 付きが %d\n%s", rec.Code, rec.Body.String())
	}
}

func TestWriteNeedsTheCookie(t *testing.T) {
	// Cookie が無い要求は 404。書く経路でも読む経路と同じ扱いにする。
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	path := inputPath(t, s, "ja")
	before := readFile(t, path)
	lines := getLines(t, s, "ja")

	req := httptest.NewRequest(http.MethodPost, "/api/rows",
		strings.NewReader(saveBody(t, "ja", lines.Version, rowEdit{ID: 6, Translation: jaTyped})))
	req.Host = "127.0.0.1:" + testPort
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", "http://127.0.0.1:"+testPort)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("状態コードが %d、404 を期待", rec.Code)
	}
	if after := readFile(t, path); after != before {
		t.Error("Cookie 無しで書けてしまった")
	}
}

func TestSaveRejectsValuesTheFileCannotHold(t *testing.T) {
	// 改行と NUL は internal/edit が拒む。拒まれた行は保存されず、
	// 1バイトも書かれない（この要求には他の行が無いため）。
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	path := inputPath(t, s, "ja")
	before := readFile(t, path)
	lines := getLines(t, s, "ja")

	cases := []struct {
		name  string
		value string
	}{
		{"LF", "上\n下"},
		{"CR", "上\r下"},
		{"CRLF", "上\r\n下"},
		{"NUL", "あ\x00い"},
	}
	for _, tc := range cases {
		rec := save(t, s, "ja", lines.Version, rowEdit{ID: 6, Translation: tc.value})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: 状態コードが %d、422 を期待\n%s", tc.name, rec.Code, rec.Body.String())
			continue
		}
		got := decode[errorResponse](t, rec.Body.Bytes())
		if len(got.Results) != 1 || got.Results[0].Saved || got.Results[0].Error == "" {
			t.Errorf("%s: 結果が %+v", tc.name, got.Results)
		}
		if after := readFile(t, path); after != before {
			t.Fatalf("%s: 拒んだのにファイルが変わった", tc.name)
		}
	}
}

func TestSaveWithInvalidUTF8(t *testing.T) {
	// 不正なUTF-8は JSON の解釈で U+FFFD に置き換わるので、[edit.File.SetTranslation]
	// の検査までは届かない。届かないこと自体は害にならない（置き換わった時点で
	// 正しいUTF-8になる）が、「JSON を通した時点で値が変わる」ことは記録しておく。
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	path := inputPath(t, s, "ja")
	lines := getLines(t, s, "ja")

	// 対になっていないサロゲート。JSON の \uXXXX として書く。
	body := `{"locale":"ja","baseVersion":"` + lines.Version +
		`","edits":[{"id":6,"translation":"a\ud800b"}]}`
	rec := doPost(t, s, "/api/rows", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	if !utf8.ValidString(readFile(t, path)) {
		t.Error("ファイルに不正なUTF-8が書かれた")
	}
	got := decode[rowsResponse](t, rec.Body.Bytes())
	if !utf8.ValidString(got.Results[0].Translation) {
		t.Error("応答に不正なUTF-8が入っている")
	}
}

func TestSaveRejectsLinesThatCannotBeEdited(t *testing.T) {
	// 編集できない行への要求は、理由を添えて断る。画面は入力欄を出さないので
	// ここへは来ないはずだが、来たときに黙って別の行を書き換えないこと。
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	path := inputPath(t, s, "ja")
	before := readFile(t, path)
	lines := getLines(t, s, "ja")

	for _, line := range []int{1, 2, 3, 4, 7, 0, -1, 999} {
		rec := save(t, s, "ja", lines.Version, rowEdit{ID: line, Translation: jaTyped})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%d行目: 状態コードが %d、422 を期待", line, rec.Code)
			continue
		}
		got := decode[errorResponse](t, rec.Body.Bytes())
		if len(got.Results) != 1 || got.Results[0].Error == "" {
			t.Errorf("%d行目: 理由が返っていない: %+v", line, got.Results)
		}
		if after := readFile(t, path); after != before {
			t.Fatalf("%d行目: 断ったのにファイルが変わった", line)
		}
	}
}

func TestSaveAppliesTheRowsItCan(t *testing.T) {
	// 1行の失敗で残り全部を巻き添えにしない。書けた行は書き、書けない行は
	// 理由を返す。画面はそれを見て「保存できていない行」として残す。
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	lines := getLines(t, s, "ja")

	rec := save(t, s, "ja", lines.Version,
		rowEdit{ID: 6, Translation: jaTyped},
		rowEdit{ID: 3, Translation: "見出しは編集できない"})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	got := decode[rowsResponse](t, rec.Body.Bytes())
	if len(got.Results) != 2 {
		t.Fatalf("結果が %+v", got.Results)
	}
	if !got.Results[0].Saved || got.Results[0].Error != "" {
		t.Errorf("6行目が保存されていない: %+v", got.Results[0])
	}
	if got.Results[1].Saved || got.Results[1].Error == "" {
		t.Errorf("3行目が保存された: %+v", got.Results[1])
	}
	if after := readFile(t, inputPath(t, s, "ja")); !strings.Contains(after, jaTyped) {
		t.Error("書けるはずの行が書かれていない")
	}
}

func TestSaveUpdatesUntranslatedCountLocally(t *testing.T) {
	// 件数の局所更新。訳が入ったキーを未翻訳から引くだけで、カテゴリの
	// 再判定はしない。訳を消せば戻る。
	//
	// --ui-lang を日本語に固定するのは、断り書きの文面で確かめるため。
	// 目録を指定しないと受け皿の en になる。
	s := newTestServer(t, Options{Root: newEditRoot(t), UILang: "ja"})
	lines := getLines(t, s, "ja")

	base, ok := countOf(lines.Counts, "untranslated")
	if !ok || !base.Judged {
		t.Fatalf("未翻訳が判定されていない: %+v", base)
	}
	if base.Count != 1 {
		t.Fatalf("最初の未翻訳が %d 件", base.Count)
	}

	rec := save(t, s, "ja", lines.Version, rowEdit{ID: 6, Translation: jaTyped})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	got := decode[rowsResponse](t, rec.Body.Bytes())
	after, _ := countOf(got.Counts, "untranslated")
	if after.Count != 0 {
		t.Errorf("保存後の未翻訳が %d 件、0 を期待", after.Count)
	}
	for _, b := range got.Results[0].Badges {
		if b.Category == "untranslated" {
			t.Error("訳を入れた行に未翻訳のバッジが残っている")
		}
	}
	// できないことを画面に伝えているか。
	joined := strings.Join(got.Notes, "\n")
	if !strings.Contains(joined, "判定し直していません") {
		t.Errorf("局所更新であることの断りが無い: %v", got.Notes)
	}

	// 読み直しても引いたままであること。起動時のスナップショットへ戻ると、
	// 訳を入れた行が未翻訳に数え直される。
	reloaded := getLines(t, s, "ja")
	again, _ := countOf(reloaded.Counts, "untranslated")
	if again.Count != 0 {
		t.Errorf("読み直したら未翻訳が %d 件に戻った", again.Count)
	}

	// 訳を消したら戻る。両方向に動かないと、打ち間違いを消したときに
	// 「未翻訳 0 件」のまま残る。
	rec = save(t, s, "ja", got.Version, rowEdit{ID: 6, Translation: ""})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d", rec.Code)
	}
	cleared := decode[rowsResponse](t, rec.Body.Bytes())
	back, _ := countOf(cleared.Counts, "untranslated")
	if back.Count != 1 {
		t.Errorf("訳を消したあとの未翻訳が %d 件、1 を期待", back.Count)
	}
}

// countOf は識別子で件数を引く。
func countOf(counts []countView, id string) (countView, bool) {
	for _, c := range counts {
		if c.Category == id {
			return c, true
		}
	}
	return countView{}, false
}

func TestSaveRequestsThatAreRefusedBeforeReadingTheFile(t *testing.T) {
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	lines := getLines(t, s, "ja")

	cases := []struct {
		name string
		body string
		want int
	}{
		{"JSON でない", `{`, http.StatusBadRequest},
		{"知らない鍵", `{"locale":"ja","baseVersion":"x","edits":[],"publish":true}`, http.StatusBadRequest},
		{"版が無い", saveBody(t, "ja", "", rowEdit{ID: 6, Translation: jaTyped}), http.StatusBadRequest},
		{"行が無い", saveBody(t, "ja", lines.Version), http.StatusBadRequest},
		{"知らないロケール", saveBody(t, "../../etc", lines.Version, rowEdit{ID: 6}), http.StatusNotFound},
		{"ロケールが無い", saveBody(t, "", lines.Version, rowEdit{ID: 6}), http.StatusNotFound},
	}
	for _, tc := range cases {
		rec := doPost(t, s, "/api/rows", tc.body, nil)
		if rec.Code != tc.want {
			t.Errorf("%s: 状態コードが %d、%d を期待\n%s", tc.name, rec.Code, tc.want, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "etc") {
			t.Errorf("%s: 応答に要求の値が書き戻されている: %s", tc.name, rec.Body.String())
		}
	}
}

func TestRepeatedSaves(t *testing.T) {
	// 短い間隔で続けて保存する。Windows では rename が共有違反で失敗しうるので、
	// 自動保存が前提のこの道具では「続けて保存できること」が要件になる。
	// 失敗したら、その回の応答を残して落とす。
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	version := getLines(t, s, "ja").Version

	const rounds = 50
	for i := range rounds {
		rec := save(t, s, "ja", version,
			rowEdit{ID: 6, Translation: jaTyped + strings.Repeat("！", i%5)})
		if rec.Code != http.StatusOK {
			t.Fatalf("%d 回目が %d\n%s", i+1, rec.Code, rec.Body.String())
		}
		got := decode[rowsResponse](t, rec.Body.Bytes())
		if !got.Results[0].Saved {
			t.Fatalf("%d 回目が保存されていない: %+v", i+1, got.Results[0])
		}
		version = got.Version
	}
	if after := readFile(t, inputPath(t, s, "ja")); !strings.Contains(after, jaTyped) {
		t.Error("最後の訳が残っていない")
	}
}

func TestSaveLogHasNoRowContent(t *testing.T) {
	// --verbose でも原文と訳は記録に書かない。記録は不具合報告に貼られる。
	var log strings.Builder
	s := newTestServer(t, Options{Root: newEditRoot(t), Verbose: true, Stderr: &log})
	lines := getLines(t, s, "ja")

	save(t, s, "ja", lines.Version, rowEdit{ID: 6, Translation: jaTyped})
	// 拒まれる要求も、競合も記録に通す。
	save(t, s, "ja", strings.Repeat("0", 64), rowEdit{ID: 6, Translation: jaTyped})
	save(t, s, "ja", lines.Version, rowEdit{ID: 3, Translation: jaTyped})

	out := log.String()
	for _, secret := range []string{jaTyped, jaHello, srcHello, srcBye} {
		if strings.Contains(out, secret) {
			t.Errorf("記録に行の中身が出ている（%q）:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, `POST "/api/rows"`) {
		t.Errorf("記録に要求が出ていない:\n%s", out)
	}
	// 件数の項目名は README の記録の例と同じにする（edits は送られた訳の数、
	// saved は実際に保存した数）。例と食い違うと、記録を添えて知らせる人が戸惑う。
	if !strings.Contains(out, " locale=ja edits=1 saved=1\n") {
		t.Errorf("記録の件数が README の例の形になっていない:\n%s", out)
	}
}

func TestSaveQuietByDefault(t *testing.T) {
	var log strings.Builder
	s := newTestServer(t, Options{Root: newEditRoot(t), Stderr: &log})
	lines := getLines(t, s, "ja")
	save(t, s, "ja", lines.Version, rowEdit{ID: 6, Translation: jaTyped})
	if log.Len() != 0 {
		t.Errorf("既定で記録が出ている:\n%s", log.String())
	}
}

func TestSaveResponsesAreNotCached(t *testing.T) {
	// 保存の応答にも訳が入っている。ディスクキャッシュに残さない。
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	lines := getLines(t, s, "ja")
	rec := save(t, s, "ja", lines.Version, rowEdit{ID: 6, Translation: jaTyped})
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control が %q", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type が %q", got)
	}
}

// TestReadOnlyFileCannotBeSaved はヘッダーが受理できないファイルを断ることを見る。
func TestReadOnlyFileCannotBeSaved(t *testing.T) {
	root := newEditRoot(t)
	path := filepath.Join(root, filepath.FromSlash("Translations/_discovered/ja.working.csv"))
	if err := os.WriteFile(path, []byte("a,b,c\n1,2,3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, Options{Root: root, Stdout: io.Discard})
	lines := getLines(t, s, "ja")
	if lines.ReadOnlyReason == "" {
		t.Fatal("読み取り専用の理由が出ていない")
	}
	before := readFile(t, path)
	rec := save(t, s, "ja", lines.Version, rowEdit{ID: 2, Translation: jaTyped})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("状態コードが %d、422 を期待\n%s", rec.Code, rec.Body.String())
	}
	if after := readFile(t, path); after != before {
		t.Error("読み取り専用のファイルが書き換わった")
	}
}

// TestMultilineRecordIsOneRow は、複数の物理行にまたがるレコードを1行として並べ、
// 訳が1行に収まるかぎり書けることを見る。
//
// PR2 までは保存が物理行の単位だったので、行をまたぐレコードのどの物理行も編集
// させていなかった（TestMultilineRecordIsReadOnly）。いまはレコードの最終フィールドを
// 差し替えるので、原文が行をまたぐ訳の空いたレコード（実物の作業コピーにある形）も
// 訳せる。訳に改行があるレコードは、改行の入力を足すまで（PR4）読み取り専用にする。
//
// 行は ID で指し、行番号（n と end）は表示のためだけに持つ。値の中の '#' で始まる行は
// 見出しにせず、値の中の空行も並べない。
func TestMultilineRecordIsOneRow(t *testing.T) {
	const multi = "para1\n\n# para2"
	root := newEditRoot(t)
	path := filepath.Join(root, filepath.FromSlash("Translations/_discovered/ja.working.csv"))
	body := strings.Join([]string{
		"key,section,node,order,speaker,source_en,translation",
		key.For(srcHello) + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + "," + jaHello,
		key.For(multi) + ",UI,,,UI,\"" + multi + "\",",
		key.For(srcBye) + ",L01 Ryan,Ryan_1_intro,2,Ryan," + srcBye + ",\"さよう\n# なら\"",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, Options{Root: root, UILang: "ja"})
	ja := s.cat.lookup("ja")
	lines := getLines(t, s, "ja")
	if lines.ReadOnlyReason != "" {
		t.Fatalf("ファイル全体を読み取り専用にしている: %s", lines.ReadOnlyReason)
	}
	if len(lines.Lines) != 3 || lines.Rows != 3 {
		t.Fatalf("並べた行 = %d 行（データ %d 行）、3 行を期待: %+v", len(lines.Lines), lines.Rows, lines.Lines)
	}

	// 1物理行のレコードは end を持たない。
	if l := lines.Lines[0]; l.ID != 2 || l.Number != 2 || l.End != 0 || !l.Editable {
		t.Errorf("1物理行のレコード = %+v", l)
	}
	para := lines.Lines[1]
	if para.ID != 3 || para.Number != 3 || para.End != 5 || para.Kind != lineKindData ||
		!para.Editable || para.Key != key.For(multi) || para.Source != multi || para.Text != "" {
		t.Errorf("原文が行をまたぐレコード = %+v", para)
	}
	if !slices.ContainsFunc(para.Badges, func(b badgeView) bool { return b.Category == "untranslated" }) {
		t.Errorf("未翻訳のバッジが無い: %+v", para.Badges)
	}
	why := s.cat.T(ja, "reason."+reason.EditMultilineTranslation)
	bye := lines.Lines[2]
	if bye.ID != 4 || bye.Number != 6 || bye.End != 7 || bye.Editable || bye.Reason != why ||
		bye.Translation != "" || bye.Text != key.For(srcBye)+",L01 Ryan,Ryan_1_intro,2,Ryan,"+srcBye+",\"さよう\n# なら\"" {
		t.Errorf("訳が行をまたぐレコード = %+v", bye)
	}

	// 訳が行をまたぐレコードには書かせない。
	before := readFile(t, path)
	rec := save(t, s, "ja", lines.Version, rowEdit{ID: 4, Key: key.For(srcBye), Translation: jaTyped})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("状態コードが %d、422 を期待", rec.Code)
	}
	if got := decode[errorResponse](t, rec.Body.Bytes()); len(got.Results) != 1 ||
		got.Results[0].ID != 4 || got.Results[0].Number != 6 || !strings.Contains(got.Results[0].Error, why) {
		t.Errorf("理由が違う: %+v", got.Results)
	}
	if after := readFile(t, path); after != before {
		t.Error("断ったのにファイルが変わった")
	}

	// 原文が行をまたぐレコードには書ける。変わるのは5行目の最終フィールドだけ。
	rec = save(t, s, "ja", lines.Version, rowEdit{ID: 3, Key: key.For(multi), Translation: jaTyped})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	got := decode[rowsResponse](t, rec.Body.Bytes())
	if len(got.Results) != 1 || !got.Results[0].Saved || got.Results[0].ID != 3 || got.Results[0].Number != 3 ||
		got.Results[0].Translation != jaTyped {
		t.Errorf("結果 = %+v", got.Results)
	}
	if want := strings.Replace(before, "# para2\",\n", "# para2\","+jaTyped+"\n", 1); readFile(t, path) != want {
		t.Errorf("書いた結果が違う\n got %q\nwant %q", readFile(t, path), want)
	}
	for _, b := range got.Results[0].Badges {
		if b.Category == "untranslated" {
			t.Error("訳を入れたレコードに未翻訳のバッジが残っている")
		}
	}
}

// TestUnclosedQuoteFileIsReadOnly は、閉じない引用符のあるファイルを全体で読み取り
// 専用にし、引用符が開いた行から後ろを生の行のまま並べることを見る（決まったことの 3）。
func TestUnclosedQuoteFileIsReadOnly(t *testing.T) {
	root := newEditRoot(t)
	path := filepath.Join(root, filepath.FromSlash("Translations/_discovered/ja.working.csv"))
	body := strings.Join([]string{
		"key,section,node,order,speaker,source_en,translation",
		key.For(srcHello) + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + "," + jaHello,
		key.For(srcBye) + ",L01 Ryan,Ryan_1_intro,2,Ryan," + srcBye + ",\"さ",
		"# --- 見出しに見える行 ---",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, Options{Root: root, UILang: "ja"})
	ja := s.cat.lookup("ja")
	lines := getLines(t, s, "ja")
	why := s.cat.T(ja, "reason."+reason.EditUnclosedQuote, "line", "3")
	if lines.ReadOnlyReason != why {
		t.Fatalf("読み取り専用の理由 = %q、%q を期待", lines.ReadOnlyReason, why)
	}
	for _, l := range lines.Lines {
		if l.Editable || l.Kind != lineKindData || l.Reason != why {
			t.Errorf("%d行目 = %+v、理由つきの読み取り専用のデータ行を期待", l.Number, l)
		}
		// 引用符が開いた行から後ろは、レコードとして解釈しない。
		if l.Number >= 3 && l.Key != "" {
			t.Errorf("%d行目をレコードとして読んでいる: %+v", l.Number, l)
		}
	}
	if len(lines.Lines) != 3 {
		t.Errorf("並べた行が %d 行、3 行（2〜4行目）を期待: %+v", len(lines.Lines), lines.Lines)
	}

	before := readFile(t, path)
	rec := save(t, s, "ja", lines.Version, rowEdit{ID: 2, Translation: jaTyped})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("状態コードが %d、422 を期待\n%s", rec.Code, rec.Body.String())
	}
	if after := readFile(t, path); after != before {
		t.Error("読み取り専用のファイルが書き換わった")
	}
}

func TestSaveBodyLimit(t *testing.T) {
	// 本文はちょうど上限までは受け、1バイトでも超えたら1バイトも書かずに断る。
	// 上限はファイル全部を1回で送っても届く大きさにしてあるので、それより
	// 大きい本文は手が滑ったものである。待ち受けに記憶を食い尽くさせない。
	cases := []struct {
		name string
		size int
		want int
	}{
		{"ちょうど上限", maxRowsBody, http.StatusOK},
		{"上限を1バイト超える", maxRowsBody + 1, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t, Options{Root: newEditRoot(t), UILang: "ja"})
			path := inputPath(t, s, "ja")
			before := readFile(t, path)

			// JSON の後ろの空白は読み飛ばされるので、中身を変えずに大きさだけ変えられる。
			body := saveBody(t, "ja", getLines(t, s, "ja").Version, rowEdit{ID: 6, Translation: jaTyped})
			body += strings.Repeat(" ", tc.size-len(body))

			rec := doPost(t, s, "/api/rows", body, nil)
			if rec.Code != tc.want {
				t.Fatalf("状態コードが %d、%d を期待\n%.200s", rec.Code, tc.want, rec.Body.String())
			}
			after := readFile(t, path)
			if tc.want != http.StatusOK {
				if after != before {
					t.Error("断ったのにファイルが変わった")
				}
				got := decode[errorResponse](t, rec.Body.Bytes())
				if want := s.cat.T(s.cat.lookup("ja"), "error.bad_request"); got.Message != want {
					t.Errorf("文面が %q、%q を期待", got.Message, want)
				}
				return
			}
			if !strings.Contains(after, jaTyped) {
				t.Error("上限ちょうどの本文が保存されていない")
			}
		})
	}
}

func TestSaveEditsLimit(t *testing.T) {
	// 1回で受ける行数もちょうど上限までにする。超えたら1行も書かない。
	// 途中まで書いて残りを落とすと、どこまで入ったのかを画面が知るすべが無い。
	cases := []struct {
		name  string
		edits int
		want  int
	}{
		{"ちょうど上限", maxRowsEdits, http.StatusOK},
		{"上限を1行超える", maxRowsEdits + 1, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t, Options{Root: newEditRoot(t)})
			path := inputPath(t, s, "ja")
			before := readFile(t, path)

			edits := make([]rowEdit, tc.edits)
			for i := range edits {
				edits[i] = rowEdit{ID: 6, Translation: jaTyped}
			}
			rec := save(t, s, "ja", getLines(t, s, "ja").Version, edits...)
			if rec.Code != tc.want {
				t.Fatalf("状態コードが %d、%d を期待\n%.200s", rec.Code, tc.want, rec.Body.String())
			}
			after := readFile(t, path)
			if tc.want != http.StatusOK {
				if after != before {
					t.Error("断ったのにファイルが変わった")
				}
				return
			}
			if got := decode[rowsResponse](t, rec.Body.Bytes()); len(got.Results) != tc.edits {
				t.Errorf("結果が %d 行、%d 行を期待", len(got.Results), tc.edits)
			}
			if !strings.Contains(after, jaTyped) {
				t.Error("上限ちょうどの要求が保存されていない")
			}
		})
	}
}

func TestSaveWhenTheFileHasGone(t *testing.T) {
	// 保存しようとしたらファイルが無くなっていた（手で消した、フォルダーごと動かした）。
	// 500 を返し、ファイルを作り直さない。画面が読んだときの版を確かめられない
	// ので、書いてよい根拠が無い。作り直せば、画面に出ている行だけのファイルが
	// 黙って置かれる。
	var log strings.Builder
	s := newTestServer(t, Options{Root: newEditRoot(t), UILang: "ja", Stderr: &log})
	path := inputPath(t, s, "ja")
	version := getLines(t, s, "ja").Version
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	rec := save(t, s, "ja", version, rowEdit{ID: 6, Translation: jaTyped})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状態コードが %d、500 を期待\n%s", rec.Code, rec.Body.String())
	}
	got := decode[errorResponse](t, rec.Body.Bytes())
	if want := s.cat.T(s.cat.lookup("ja"), "error.read_failed"); got.Message != want {
		t.Errorf("文面が %q、%q を期待", got.Message, want)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("無くなったファイルを作り直した（%v）", err)
	}
	if !strings.Contains(log.String(), "open failed locale=ja") {
		t.Errorf("記録に失敗が出ていない: %q", log.String())
	}
	if strings.Contains(log.String(), jaTyped) || strings.Contains(log.String(), "working.csv") {
		t.Errorf("記録に訳かパスが出ている: %q", log.String())
	}
}

func TestAddWarning(t *testing.T) {
	// 行の断りは上書きせずに連ねる。先に付いた断り（値を整えた、など）を消すと、
	// 翻訳者は自分の訳が書き換わったことに気づけない。区切りは空白1つで、
	// 画面はこれを行の下に1行で出す。
	cases := []struct {
		name   string
		before string
		add    string
		want   string
	}{
		{"最初の断り", "", "値を整えました。", "値を整えました。"},
		{"先の断りに連ねる", "値を整えました。", "もう1つ。", "値を整えました。 もう1つ。"},
		{"空の断りは足さない", "値を整えました。", "", "値を整えました。"},
		{"空に空を足しても空", "", "", ""},
	}
	for _, tc := range cases {
		res := rowResult{Warning: tc.before}
		addWarning(&res, tc.add)
		if res.Warning != tc.want {
			t.Errorf("%s: %q、%q を期待", tc.name, res.Warning, tc.want)
		}
	}
}

func TestEditErrorTextKeepsUnknownErrors(t *testing.T) {
	// internal/edit の2つの型でない誤りは Error() をそのまま返す
	// （doc.go「直していない制限」）。訳されていない文が出るほうが、何も出ない
	// よりよい。2つの型は包まれていても見分け、外枠を目録から組み直す。
	s := newTestServer(t, Options{UILang: "en"})
	en := s.cat.lookup("en")

	unknown := errors.New("unexpected: disk full")
	if got := s.editErrorText(en, unknown); got != unknown.Error() {
		t.Errorf("知らない誤りが %q、%q を期待", got, unknown.Error())
	}

	why := reason.New(reason.EditNoSuchLine, "そんな行番号は無い")
	wrapped := fmt.Errorf("save: %w", &edit.NotEditableError{Line: 3, Reason: why.Text, Cause: why})
	want := s.cat.T(en, "error.not_editable", "line", "3", "reason", s.reasonText(en, why))
	if got := s.editErrorText(en, wrapped); got != want {
		t.Errorf("包まれた NotEditableError が %q、%q を期待", got, want)
	}
	if hasJapanese(want) {
		t.Errorf("英語の画面に日本語が出る: %q", want)
	}
}

func TestRowsWithoutKeyAreNotTracked(t *testing.T) {
	// キー列が空の行（作業コピーにはありうる）は、局所更新の控えに入れない。
	// キーの無い行は、控えから行を引き当てられない（バッジも付かない。
	// [server.badgesByKey]）。入れると "" という1つのキーにまとまり、
	// キーの無い行を1行訳しただけで件数が動く。
	s := newTestServer(t, Options{})
	s.overlay.addUntranslated("ja", "")
	s.overlay.addTagged("ja", "")
	s.markFilled("ja", "", true)
	s.markTags("ja", "", diff.CheckTags("<b>閉じていない"))

	if n := len(s.overlay.untranslated["ja"]); n != 0 {
		t.Errorf("未翻訳の控えに %d 件入った", n)
	}
	if n := len(s.overlay.tagged["ja"]); n != 0 {
		t.Errorf("タグの控えに %d 件入った", n)
	}
	if n := len(s.filledKeys("ja")); n != 0 {
		t.Errorf("訳が入ったキーに %d 件入った", n)
	}
	if n := s.tagsTouched("ja"); n != 0 {
		t.Errorf("タグを判定し直した行に %d 件入った", n)
	}
	if n := s.tagDelta("ja"); n != 0 {
		t.Errorf("タグの件数が %d 動いた", n)
	}
}
