package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/edit"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

/*
ここの試験は、保存をレコードの単位へ移す作業（全体を解釈する読み手へ移す作業の PR3）で
変わった待ち受けの API の結果を見る。

PR3 の最初のコミットでは、切り替える前の結果を名前の末尾が Now の試験で固定した
（決まったことの 13。ファイル名も records_now_test.go だった）。切り替えたコミットで
期待値を直し、名前から Now を外した。

  - 行一覧はレコードで並び、行は id と行番号の範囲（n と end）を持つ。データ行は
    レコードで数え、ファイルの物理行はファイルから数える（以前は物理行で並び、id が
    無く、続きの行もデータ行に数えていた）。
  - 保存の要求は id で行を指し、結果は id といまの最初の物理行（n）を返す（以前は
    物理行の番号 line で指していた）。
  - 版の照合を、ファイル全体の読み取り専用の判定より先に置く（以前は、読み取り専用の
    ファイルを、版が古くても 409 ではなく 422 で断っていた）。

見本の英文と訳はどれも架空の文である。
*/

// nowPara は、原文が空行を挟んで3物理行にまたがる訳の空いたレコードの原文（架空）。
const nowPara = "Rinse the plates, cups, and bowls.\n\n# Then dry, stack, and sort them."

// writeNowWorking は、newEditRoot の作業コピーを、行をまたぐレコードのある形に替える。
//
// 物理行（CRLF 区切り、値の中は LF）:
//
//	1 ヘッダー / 2 訳あり / 3〜5 原文が行をまたぐレコード（4 は値の中の空行、
//	5 は値の中の '#' の行）/ 6 訳が空
func writeNowWorking(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash("Translations/_discovered/ja.working.csv"))
	body := "key,section,node,order,speaker,source_en,translation\r\n" +
		key.For(srcHello) + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + "," + jaHello + "\r\n" +
		key.For(nowPara) + ",UI,,,UI,\"" + nowPara + "\",\r\n" +
		key.For(srcBye) + ",L01 Ryan,Ryan_1_intro,2,Ryan," + srcBye + ",\r\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLinesAreRecords は、行一覧がレコード（セグメント）で並び、行が id と行番号の
// 範囲を持つことと、数えたものの数え方を見る。
//
// PR3 の最初のコミットでは TestLinesArePhysicalLinesNow として、行が物理行で並び、
// id が無く、続きの行もデータ行に数えることを固定していた。
func TestLinesAreRecords(t *testing.T) {
	root := newEditRoot(t)
	writeNowWorking(t, root)
	s := newTestServer(t, Options{Root: root, UILang: "ja"})
	lines := getLines(t, s, "ja")

	var got []string
	for _, l := range lines.Lines {
		got = append(got, fmt.Sprintf("%d:%d-%d", l.ID, l.Number, l.End))
	}
	// 3〜5行目のレコードは1行として並び、値の中の空行と '#' の行は並ばない。
	if fmt.Sprint(got) != "[2:2-0 3:3-5 4:6-0]" {
		t.Errorf("並べた行（id:n-end）= %v、[2:2-0 3:3-5 4:6-0] を期待", got)
	}
	if lines.Rows != 3 {
		t.Errorf("データ行の数 = %d、レコードで数えて 3 を期待", lines.Rows)
	}
	if l := lines.Lines[1]; l.Source != nowPara || !l.Editable || l.Key != key.For(nowPara) {
		t.Errorf("原文が行をまたぐレコード = %+v", l)
	}
	ja := s.cat.lookup("ja")
	stats := make(map[string]int)
	for _, st := range lines.Stats {
		stats[st.Label] = st.Value
	}
	if got := stats[s.cat.T(ja, "stats.file_lines")]; got != 6 {
		t.Errorf("ファイルの物理行 = %d、6 を期待", got)
	}
	if got := stats[s.cat.T(ja, "stats.data_lines")]; got != 3 {
		t.Errorf("データ行 = %d、3 を期待", got)
	}
}

// TestSaveAddressesRecordsByID は、保存の要求が ID で行を指し、結果が ID といまの
// 最初の物理行を返すことを見る。行をまたぐレコードの後ろでは、ID と物理行の番号が
// ずれる。
//
// PR3 の最初のコミットでは TestSaveAddressesPhysicalLinesNow として、要求と結果が
// 物理行の番号（line）で行を指すことを固定していた。いまは line の鍵を知らない鍵と
// して断る（画面と待ち受けの版が食い違ったとき、送った値が黙って落ちないように）。
func TestSaveAddressesRecordsByID(t *testing.T) {
	root := newEditRoot(t)
	path := writeNowWorking(t, root)
	s := newTestServer(t, Options{Root: root})
	lines := getLines(t, s, "ja")

	old := `{"locale":"ja","baseVersion":"` + lines.Version +
		`","edits":[{"line":6,"key":"` + key.For(srcBye) + `","translation":"` + jaTyped + `"}]}`
	if rec := doPost(t, s, "/api/rows", old, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("line で指す要求が %d、400 を期待", rec.Code)
	}

	body := `{"locale":"ja","baseVersion":"` + lines.Version +
		`","edits":[{"id":4,"key":"` + key.For(srcBye) + `","translation":"` + jaTyped + `"}]}`
	rec := doPost(t, s, "/api/rows", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":4,"n":6,`) {
		t.Errorf("結果に ID といまの最初の物理行が無い: %.300s", rec.Body.String())
	}
	if got := readFile(t, path); !strings.HasSuffix(got, ","+srcBye+","+jaTyped+"\r\n") {
		t.Errorf("6行目に入っていない: %q", got)
	}
}

// TestOneRequestWritesEachEditToItsRecord は、1回の保存の要求に入った複数の編集が、
// それぞれ ID の指すレコードの最終フィールドに入り、ほかは1バイトも変わらないことを、
// ファイル全体のバイト列で見る。見本は実物と同じ形（区切りは CRLF、値の中は LF、
// 空行・'#' の行・カンマの多い続きの行を含む）で、行をまたぐレコードの後ろでは ID と
// 行番号がずれる。
func TestOneRequestWritesEachEditToItsRecord(t *testing.T) {
	const para = "Rinse the plates, cups, and bowls.\n\n# Then dry, stack, and sort them."
	root := newEditRoot(t)
	path := filepath.Join(root, filepath.FromSlash("Translations/_discovered/ja.working.csv"))
	head := "key,section,node,order,speaker,source_en,translation\r\n" +
		"\r\n" +
		"# ===== Level 1: Ryan (Sunny) =====\r\n" +
		"# --- intro: Ryan_1_intro ---\r\n"
	hello := key.For(srcHello) + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ","
	paraRow := key.For(para) + ",UI,,,UI,\"" + para + "\","
	bye := key.For(srcBye) + ",L01 Ryan,Ryan_1_intro,2,Ryan," + srcBye + ","
	body := head + hello + jaHello + "\r\n" + "\r\n" + paraRow + "\r\n" + bye + "\r\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, Options{Root: root})
	lines := getLines(t, s, "ja")
	ids := make(map[string]int)
	for _, l := range lines.Lines {
		ids[l.Key] = l.ID
	}
	// 物理行: 5 hello / 6 空行 / 7〜9 para / 10 bye。ID: 5 / 6 / 7 / 8。
	if ids[key.For(srcHello)] != 5 || ids[key.For(para)] != 7 || ids[key.For(srcBye)] != 8 {
		t.Fatalf("ID = %v", ids)
	}

	rec := save(t, s, "ja", lines.Version,
		rowEdit{ID: 8, Key: key.For(srcBye), Translation: "さようなら, また"},
		rowEdit{ID: 5, Key: key.For(srcHello), Translation: ""},
		rowEdit{ID: 7, Key: key.For(para), Translation: `すすいで "ふく"`})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	got := decode[rowsResponse](t, rec.Body.Bytes())
	for i, want := range []struct{ id, n int }{{8, 10}, {5, 5}, {7, 7}} {
		if r := got.Results[i]; !r.Saved || r.ID != want.id || r.Number != want.n {
			t.Errorf("%d番目の結果 = %+v", i, r)
		}
	}
	want := head + hello + "\r\n" + "\r\n" + paraRow + `"すすいで ""ふく"""` + "\r\n" + bye + `"さようなら, また"` + "\r\n"
	if after := readFile(t, path); after != want {
		t.Errorf("書いた結果が違う\n got %q\nwant %q", after, want)
	}

	// 訳を元に戻すと、元のバイト列に戻る。
	rec = save(t, s, "ja", got.Version,
		rowEdit{ID: 5, Key: key.For(srcHello), Translation: jaHello},
		rowEdit{ID: 7, Key: key.For(para), Translation: ""},
		rowEdit{ID: 8, Key: key.For(srcBye), Translation: ""})
	if rec.Code != http.StatusOK {
		t.Fatalf("戻す保存の状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	if after := readFile(t, path); after != body {
		t.Errorf("元のバイト列に戻らない\n got %q\nwant %q", after, body)
	}
}

// TestLineBreaksInValuesAreSentAsLF は、値の中の改行を LF にそろえて渡すことを見る
// （model.go の lfLineBreaks）。表計算ソフトなどで保存し直すと、値の中の改行が CRLF に
// なることがある。そろえるのは描くための値だけで、ファイルは変えない。
func TestLineBreaksInValuesAreSentAsLF(t *testing.T) {
	root := newEditRoot(t)
	path := filepath.Join(root, filepath.FromSlash("Translations/_discovered/ja.working.csv"))
	body := "key,section,node,order,speaker,source_en,translation\r\n" +
		key.For("para1\r\n\r\npara2") + ",UI,,,UI,\"para1\r\n\r\npara2\",\r\n" +
		key.For("two") + ",UI,,,UI,two,\"に\r\nさん\"\r\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, Options{Root: root, UILang: "ja"})
	lines := getLines(t, s, "ja")
	if len(lines.Lines) != 2 {
		t.Fatalf("並べた行 = %+v", lines.Lines)
	}
	if l := lines.Lines[0]; l.Source != "para1\n\npara2" || !l.Editable || l.End != 4 {
		t.Errorf("原文が行をまたぐレコード = %+v", l)
	}
	if l := lines.Lines[1]; l.Editable || l.Text != key.For("two")+",UI,,,UI,two,\"に\nさん\"" {
		t.Errorf("訳が行をまたぐレコード = %+v", l)
	}
	if readFile(t, path) != body {
		t.Error("ファイルが変わった")
	}
}

// TestRowsThatCannotBeWrittenSafelyAreReadOnly は、publish やゲームが別の値に読む
// レコードを、理由（目録の文面）を付けて編集させず、値がどれも空のレコードを並べない
// ことを見る。
//
//   - 飲み込みの疑い（csvfile.FindSwallows）: 引用符の閉じ誤りで後ろの行を値に
//     飲み込んでいる疑い。原文の側で飲み込み、訳は1行に収まる形。
//   - ゲームの読み方との食い違い（csvfile.CSharpDisagreements）: フィールドの途中の '"'。
//   - ",,,,,," の行（改善の ui-15）: 空行相当として並べない。
//
// 行の区切りが CR だけのファイルは、ファイル全体を読み取り専用にする。
func TestRowsThatCannotBeWrittenSafelyAreReadOnly(t *testing.T) {
	root := newEditRoot(t)
	path := filepath.Join(root, filepath.FromSlash("Translations/_discovered/ja.working.csv"))
	body := "key,section,node,order,speaker,source_en,translation\n" +
		key.For(srcHello) + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"" + srcHello + "\n" +
		key.For(srcBye) + ",L01 Ryan,Ryan_1_intro,2,Ryan," + srcBye + "\"," + jaHello + "\n" +
		",,,,,,\n" +
		key.For("Start") + ",UI,,,UI,Start,は\"じ\"める\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, Options{Root: root, UILang: "ja"})
	ja := s.cat.lookup("ja")
	lines := getLines(t, s, "ja")
	if len(lines.Lines) != 2 || lines.Rows != 2 {
		t.Fatalf("並べた行 = %+v、カンマだけの行を除いた2行を期待", lines.Lines)
	}
	wants := []struct {
		id     int
		reason string
	}{
		{2, s.cat.T(ja, "reason."+reason.EditSwallow, "line", "3")},
		{4, s.cat.T(ja, "reason."+reason.EditGameDisagrees, "column", "translation")},
	}
	for i, w := range wants {
		l := lines.Lines[i]
		if l.ID != w.id || l.Editable || l.Reason != w.reason || l.Text == "" {
			t.Errorf("ID %d = %+v、理由 %q を期待", w.id, l, w.reason)
		}
	}
	before := readFile(t, path)
	for _, id := range []int{2, 3, 4} {
		if rec := save(t, s, "ja", lines.Version, rowEdit{ID: id, Translation: jaTyped}); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("ID %d: 状態コードが %d、422 を期待", id, rec.Code)
		}
	}
	if readFile(t, path) != before {
		t.Error("断ったのにファイルが変わった")
	}

	// 行の区切りが CR だけのファイル。
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(body, "\n", "\r")), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := getLines(t, s, "ja").ReadOnlyReason; got != s.cat.T(ja, "reason."+reason.EditCROnly) {
		t.Errorf("読み取り専用の理由 = %q", got)
	}
}

// TestSaveCheckFailureNamesTheRow は、書く直前のファイル全体の確かめ（書く前の事後確認の
// 後半）が外れたときの行ごとの結果を見る。どの行の saved も倒し、外れた行にだけ理由を
// 付ける。画面はその行を保存できない行にして送り直しを止め、理由の無い行は送り直す。
//
// 確かめは正しく組み立てたファイルでは外れないので、誤りは試験の中で作る（外れること
// そのものは internal/edit の TestSaveRechecksTheWholeFile が見ている）。
func TestSaveCheckFailureNamesTheRow(t *testing.T) {
	s := newTestServer(t, Options{UILang: "en"})
	en := s.cat.lookup("en")
	why := reason.New(reason.EditRecheckFailed, "読み直すと合わない", "line", "7")
	results := []rowResult{
		{ID: 3, Number: 3, Saved: true, Translation: "a", Warning: "w"},
		{ID: 5, Number: 7, Saved: true, Translation: "b"},
		{ID: 6, Number: 8, Error: "もとからの理由"},
	}
	got := s.recheckResults(en, results, &edit.RecheckError{ID: 5, Line: 7, Cause: why})
	want := s.cat.T(en, "error.not_editable", "line", "7", "reason", s.reasonText(en, why))
	if hasJapanese(want) {
		t.Errorf("英語の画面に日本語が出る: %q", want)
	}
	for i, r := range got {
		if r.Saved || r.Translation != "" || r.Warning != "" {
			t.Errorf("%d番目が保存したことになっている: %+v", i, r)
		}
	}
	if got[0].Error != "" || got[1].Error != want || got[2].Error != "もとからの理由" {
		t.Errorf("理由 = %q / %q / %q", got[0].Error, got[1].Error, got[2].Error)
	}
}

// TestConflictLetsTheScreenRemapByKey は、409 のあと、いまの行一覧のキーで編集を
// 載せ直して保存できることを見る（画面の onConflict と remap がする手順を、要求で
// なぞる）。
//
// 画面を開いているあいだに、よそが手前に行を足すと、ID は1つずつ後ろへずれる。
// 古い ID のまま送ると、版が違うので 409 になり、いまの行一覧が返る。そこから同じ
// キーの行を探して、新しい ID と新しい版で送り直せば書ける。版だけ新しくして古い ID で
// 送ると、その ID には別のキーの行があるので書かずに断る（row_moved）。
func TestConflictLetsTheScreenRemapByKey(t *testing.T) {
	root := newEditRoot(t)
	s := newTestServer(t, Options{Root: root, UILang: "ja"})
	path := inputPath(t, s, "ja")
	lines := getLines(t, s, "ja")

	// よそが見出しの下に、行をまたぐレコードを1つ足す。
	added := key.For("new\nline") + ",L01 Ryan,Ryan_1_intro,0,Ryan,\"new\nline\",\n"
	outside := strings.Replace(readFile(t, path), "# --- intro: Ryan_1_intro ---\n",
		"# --- intro: Ryan_1_intro ---\n"+added, 1)
	if err := os.WriteFile(path, []byte(outside), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := save(t, s, "ja", lines.Version, rowEdit{ID: 6, Key: key.For(srcBye), Translation: jaTyped})
	if rec.Code != http.StatusConflict {
		t.Fatalf("状態コードが %d、409 を期待", rec.Code)
	}
	current := decode[conflictResponse](t, rec.Body.Bytes()).Current
	to := 0
	for _, l := range current.Lines {
		if l.Kind == lineKindData && l.Key == key.For(srcBye) {
			to = l.ID
			if l.Number != 8 {
				t.Errorf("載せ直す先の行番号 = %d、8 を期待", l.Number)
			}
		}
	}
	if to != 7 {
		t.Fatalf("キーで引いた ID = %d、7 を期待", to)
	}

	// 版だけ新しくして古い ID で送ると、別のキーの行なので断る。
	rec = save(t, s, "ja", current.Version, rowEdit{ID: 6, Key: key.For(srcBye), Translation: jaTyped})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("古い ID の状態コードが %d、422 を期待", rec.Code)
	}
	if got := decode[errorResponse](t, rec.Body.Bytes()); got.Results[0].Error != s.cat.T(s.cat.lookup("ja"), "error.row_moved") {
		t.Errorf("理由 = %q", got.Results[0].Error)
	}
	if readFile(t, path) != outside {
		t.Fatal("断ったのにファイルが変わった")
	}

	// 載せ直した ID で送ると書ける。変わるのはその行の訳だけ。
	rec = save(t, s, "ja", current.Version, rowEdit{ID: to, Key: key.For(srcBye), Translation: jaTyped})
	if rec.Code != http.StatusOK {
		t.Fatalf("載せ直した ID の状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	if want := strings.Replace(outside, ","+srcBye+",\n", ","+srcBye+","+jaTyped+"\n", 1); readFile(t, path) != want {
		t.Errorf("書いた結果が違う\n got %q\nwant %q", readFile(t, path), want)
	}
}

// TestVersionIsCheckedBeforeReadOnly は、版の照合を、ファイル全体の読み取り専用の判定
// より先に置くことを見る（決まったことのそのほか 5・17）。
//
// 画面を開いているあいだに、ゲームや表計算ソフトがファイルを書き換えて読み取り専用の
// 形（閉じない引用符など）になったとき、版が違えば 409 といまの行一覧（読み取り専用の
// 理由つき）を返す。画面はそれを描き直し、載せ直せない訳を行き先の無い訳として出し
// 続けられる。版が合っている（読んだときから読み取り専用の）ファイルは、いままでどおり
// 422 で断る。
//
// PR3 の最初のコミットでは TestReadOnlyIsCheckedBeforeVersionNow として、版が古くても
// 422 で断る（409 といまの行一覧を返さない）ことを固定していた。
func TestVersionIsCheckedBeforeReadOnly(t *testing.T) {
	root := newEditRoot(t)
	s := newTestServer(t, Options{Root: root, Stdout: io.Discard, UILang: "ja"})
	ja := s.cat.lookup("ja")
	path := inputPath(t, s, "ja")
	lines := getLines(t, s, "ja")

	// 開いているあいだに、6行目の訳の引用符が閉じない形に書き換わる。
	broken := strings.Replace(readFile(t, path), ","+srcBye+",", ","+srcBye+",\"よそ", 1)
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := save(t, s, "ja", lines.Version, rowEdit{ID: 6, Key: key.For(srcBye), Translation: jaTyped})
	if rec.Code != http.StatusConflict {
		t.Fatalf("状態コードが %d、409 を期待\n%s", rec.Code, rec.Body.String())
	}
	got := decode[conflictResponse](t, rec.Body.Bytes())
	why := s.cat.T(ja, "reason."+reason.EditUnclosedQuote, "line", "6")
	if got.Current == nil || got.Current.ReadOnlyReason != why || len(got.Current.Lines) == 0 {
		t.Fatalf("いまの行一覧 = %+v、読み取り専用の理由 %q を期待", got.Current, why)
	}
	for _, l := range got.Current.Lines {
		if l.Kind == lineKindData && (l.Editable || l.Reason != why) {
			t.Errorf("ID %d = %+v、理由つきの読み取り専用を期待", l.ID, l)
		}
	}
	if readFile(t, path) != broken {
		t.Error("409 なのにファイルが変わった")
	}

	// 読んだときから読み取り専用のファイル（版が合う）は 422。
	rec = save(t, s, "ja", got.Current.Version, rowEdit{ID: 6, Translation: jaTyped})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("状態コードが %d、422 を期待\n%s", rec.Code, rec.Body.String())
	}
	if readFile(t, path) != broken {
		t.Error("読み取り専用のファイルが書き換わった")
	}
}
