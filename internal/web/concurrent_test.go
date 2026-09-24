package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
)

// TestTwoEditorsDoNotLoseEachOthersSaves は、同じファイルを開いた2つの待ち受け
// （dwloc edit を2つ動かした形）が、別々の行を並行して保存し続けても、200 で返った
// 訳が消えないことを見る（改善の決定 3。改善の調査 security-2 の筋書き）。
//
// 待ち受けの中の錠（saveMu）は1つの待ち受けの中でしか効かない。以前は、片方の版の
// 照合が通ってから rename するまでのあいだに、もう片方が古い版から組んだ中身で
// 置き換えることがあり、200 で返った訳が1つ前の値か元の訳に戻った（調査では 319 件の
// うち 16 件）。いまは照合から rename までを OS の錠で囲むので、あとから書く側は、
// 先に書いた側の中身で版の照合に外れて 409 になる。
//
// それぞれの待ち受けは、保存の前にいまの行一覧を読み、自分の行の訳が、自分が最後に
// 200 を受け取った値のままかを確かめる。自分の行を書くのは自分だけなので、違って
// いれば、もう片方が古い中身で上書きした（訳が消えた）ことになる。
func TestTwoEditorsDoNotLoseEachOthersSaves(t *testing.T) {
	root := newEditRoot(t)
	a := newTestServer(t, Options{Root: root})
	b := newTestServer(t, Options{Root: root})

	const rounds = 40
	var wg sync.WaitGroup
	lost := make(chan string, 2*rounds)
	run := func(s *server, id int, k, first, prefix string) {
		defer wg.Done()
		last := first
		saved := 0
		for i := range rounds {
			lines, ok := linesNow(s)
			if !ok {
				// Windows では、もう片方が置き換えている最中に読めないことがある（500）。
				continue
			}
			if got := translationOf(lines, id); got != last {
				lost <- fmt.Sprintf("ID %d: 200 で返った %q が %q に戻った", id, last, got)
				last = got
			}
			value := fmt.Sprintf("%s%d", prefix, i)
			rec := doPost(t, s, "/api/rows", rowsBody(lines.Version, id, k, value), nil)
			switch rec.Code {
			case http.StatusOK:
				last = value
				saved++
			case http.StatusConflict, http.StatusServiceUnavailable:
				// 409 はもう片方が先に書いたとき。503 は Windows で置き換えの最中に
				// 書けなかったとき。どちらも書いていないので、次の回で読み直す。
			default:
				lost <- fmt.Sprintf("ID %d: 状態コード %d: %s", id, rec.Code, rec.Body.String())
			}
		}
		if lines, ok := linesNow(s); ok {
			if got := translationOf(lines, id); got != last {
				lost <- fmt.Sprintf("ID %d: 最後に 200 で返った %q が %q に戻った", id, last, got)
			}
		}
		if saved == 0 {
			lost <- fmt.Sprintf("ID %d: 1度も保存できなかった", id)
		}
	}
	wg.Add(2)
	go run(a, 5, key.For(srcHello), jaHello, "あ")
	go run(b, 6, key.For(srcBye), "", "い")
	wg.Wait()
	close(lost)
	for msg := range lost {
		t.Error(msg)
	}
}

// linesNow は、いまの行一覧を読む。読めなければ false。並行に呼ぶので t を使わない。
func linesNow(s *server) (linesResponse, bool) {
	req := httptest.NewRequest(http.MethodGet, "/api/lines?locale=ja", nil)
	req.Host = "127.0.0.1:" + testPort
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return linesResponse{}, false
	}
	var lines linesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &lines); err != nil {
		return linesResponse{}, false
	}
	return lines, true
}

// rowsBody は1行ぶんの保存の本文を組む。並行に呼ぶので t を使わない。
func rowsBody(version string, id int, k, value string) string {
	body, _ := json.Marshal(rowsRequest{Locale: "ja", BaseVersion: version,
		Edits: []rowEdit{{ID: id, Key: k, Translation: value}}})
	return string(body)
}

// translationOf は、行一覧から ID の行の訳を引く。
func translationOf(lines linesResponse, id int) string {
	for _, l := range lines.Lines {
		if l.ID == id {
			return l.Translation
		}
	}
	return "（行が無い）"
}
