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
)

/*
ここの試験は、保存をレコードの単位へ移す前（全体を解釈する読み手へ移す作業の PR3 の
最初のコミット）の待ち受けの API の結果を固定する（決まったことの 13）。名前の末尾は
どれも Now で、切り替えるコミットで期待値を直す。

固定するのは PR3 で変わるものである。

  - 行一覧は物理行で並び、行に id は無い。行をまたぐレコードの続きの行も1行として並び、
    データ行の数に入る。ファイルの物理行の数は、並べた行（空行とヘッダーも含む）の数。
  - 保存の要求は物理行の番号（line）で行を指し、結果も line で返る。
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

// TestLinesArePhysicalLinesNow は、行一覧が物理行で並び、行に id が無いことと、
// 数えたものの数え方を固定する。
func TestLinesArePhysicalLinesNow(t *testing.T) {
	root := newEditRoot(t)
	writeNowWorking(t, root)
	s := newTestServer(t, Options{Root: root, UILang: "ja"})
	rec := do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"id":`) {
		t.Errorf("行に id がある: %.300s", rec.Body.String())
	}
	lines := decode[linesResponse](t, rec.Body.Bytes())

	var numbers []int
	for _, l := range lines.Lines {
		numbers = append(numbers, l.Number)
	}
	// 4行目は値の中の空行なので並べない。5行目は値の中の '#' の行で、生の行として並ぶ。
	if got := fmt.Sprint(numbers); got != "[2 3 5 6]" {
		t.Errorf("並べた行 = %s、[2 3 5 6] を期待", got)
	}
	if lines.Rows != 4 {
		t.Errorf("データ行の数 = %d、続きの行も数えて 4 を期待", lines.Rows)
	}
	ja := s.cat.lookup("ja")
	stats := make(map[string]int)
	for _, st := range lines.Stats {
		stats[st.Label] = st.Value
	}
	if got := stats[s.cat.T(ja, "stats.file_lines")]; got != 6 {
		t.Errorf("ファイルの物理行 = %d、6 を期待", got)
	}
	if got := stats[s.cat.T(ja, "stats.data_lines")]; got != 4 {
		t.Errorf("データ行 = %d、4 を期待", got)
	}
}

// TestSaveAddressesPhysicalLinesNow は、保存の要求が物理行の番号で行を指し、結果も
// 物理行の番号で返ることを固定する。
func TestSaveAddressesPhysicalLinesNow(t *testing.T) {
	root := newEditRoot(t)
	path := writeNowWorking(t, root)
	s := newTestServer(t, Options{Root: root})
	lines := getLines(t, s, "ja")

	body := `{"locale":"ja","baseVersion":"` + lines.Version +
		`","edits":[{"line":6,"key":"` + key.For(srcBye) + `","translation":"` + jaTyped + `"}]}`
	rec := doPost(t, s, "/api/rows", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"line":6,`) {
		t.Errorf("結果に物理行の番号が無い: %.300s", rec.Body.String())
	}
	if got := readFile(t, path); !strings.HasSuffix(got, ","+srcBye+","+jaTyped+"\r\n") {
		t.Errorf("6行目に入っていない: %q", got)
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
	rec := save(t, s, "ja", strings.Repeat("0", 64), rowEdit{Line: 2, Translation: jaTyped})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("状態コードが %d、422 を期待\n%s", rec.Code, rec.Body.String())
	}
}
