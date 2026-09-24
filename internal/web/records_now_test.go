package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

/*
ここの試験は、保存をレコードの単位へ移す作業（全体を解釈する読み手へ移す作業の PR3）で
変わる待ち受けの API の結果を見る。

PR3 の最初のコミットでは、切り替える前の結果を名前の末尾が Now の試験で固定した
（決まったことの 13）。切り替えたコミットで期待値を直し、名前から Now を外した。
Now のまま残っているものは、まだ切り替えていない結果である。

  - 行一覧はレコードで並び、行は id と行番号の範囲（n と end）を持つ。データ行は
    レコードで数え、ファイルの物理行はファイルから数える（以前は物理行で並び、id が
    無く、続きの行もデータ行に数えていた）。
  - 保存の要求は id で行を指し、結果は id といまの最初の物理行（n）を返す（以前は
    物理行の番号 line で指していた）。
  - 読み取り専用のファイルは、版を照合する前に 422 で断る（版が古くても 409 にならない）。

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

// TestReadOnlyIsCheckedBeforeVersionNow は、読み取り専用のファイルを、版を照合する前に
// 422 で断ることを固定する。版が古くても 409 といまの行一覧は返らない。
func TestReadOnlyIsCheckedBeforeVersionNow(t *testing.T) {
	root := newEditRoot(t)
	path := filepath.Join(root, filepath.FromSlash("Translations/_discovered/ja.working.csv"))
	if err := os.WriteFile(path, []byte("a,b,c\n1,2,3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, Options{Root: root, Stdout: io.Discard})
	rec := save(t, s, "ja", strings.Repeat("0", 64), rowEdit{ID: 2, Translation: jaTyped})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("状態コードが %d、422 を期待\n%s", rec.Code, rec.Body.String())
	}
}
