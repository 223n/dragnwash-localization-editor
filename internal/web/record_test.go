package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// ここは Options.Record の試験。--verbose を付けなくても、要求の記録を
// ログファイルへ残せることと、残すものが --verbose のときと変わらないことを見る。

func TestRecordWithoutVerbose(t *testing.T) {
	// --verbose を付けなくても Record があればそこへ1行ずつ残る。画面は静かなまま。
	// ログファイルは、画面をうるさくせずに手がかりを増やすためにある。
	var screen, file bytes.Buffer
	s := newTestServer(t, Options{Stderr: &screen, Record: &file, UILang: "ja"})
	do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)

	if screen.Len() != 0 {
		t.Errorf("--verbose でないのに画面へ記録が出ている:\n%s", screen.String())
	}
	text := file.String()
	for _, want := range []string{`GET "/api/lines" 200`, "locale=ja", "lines="} {
		if !strings.Contains(text, want) {
			t.Errorf("ログファイルに %q が無い:\n%s", want, text)
		}
	}
}

func TestRecordIsNotDoubledWithVerbose(t *testing.T) {
	// --verbose のときは標準エラーへだけ書く。呼び出し側がそこをログファイルへも
	// 束ねているので、Record へも書くと同じ行がファイルに2度入る。
	var screen, file bytes.Buffer
	s := newTestServer(t, Options{Verbose: true, Stderr: &screen, Record: &file, UILang: "ja"})
	do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)

	if !strings.Contains(screen.String(), `GET "/api/lines" 200`) {
		t.Errorf("--verbose なのに画面へ記録が出ていない:\n%s", screen.String())
	}
	if file.Len() != 0 {
		t.Errorf("--verbose のときに Record へも書いている（ファイルに二重に入る）:\n%s", file.String())
	}
}

func TestRecordHasNoRowContent(t *testing.T) {
	// いちばん大事な性質。原文と訳を書かない約束は、行き先がファイルになっても
	// 変わらない。ログファイルはそのまま不具合の報告に貼れるものでなければならない。
	var file bytes.Buffer
	s := newTestServer(t, Options{Record: &file, UILang: "ja"})
	do(t, s, http.MethodGet, "/?"+tokenParam+"="+s.token, false, nil)
	do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	do(t, s, http.MethodGet, "/api/lines?locale=he", true, nil)

	text := file.String()
	if text == "" {
		t.Fatal("Record を渡したのに記録が空")
	}
	for _, secret := range []string{jaVanished, "もしもし", "こんにちは", "שלום", "設定", keyVanished, keyKept} {
		if strings.Contains(text, secret) {
			t.Errorf("ログファイルに行の中身が出ている: %q\n%s", secret, text)
		}
	}
	// トークンも出さない。最初の1回の URL に載っているので、問い合わせ文字列ごと落とす。
	if strings.Contains(text, s.token) {
		t.Errorf("ログファイルにトークンが出ている:\n%s", text)
	}
}

// TestRecordQuotesAndClipsThePath は、要求の経路を %q で書き、長さを切ることを見る。
//
// 記録は認証より外側で取るので、トークンを持たない相手の要求も記録に残る
// （起動し直したあとに古いタブが出す 404 を、あとから辿れるようにするため）。
// 経路をそのまま書くと、そうした相手が改行を入れて本物と見分けの付かない行を
// 足したり、1回の要求で記録を 1MB 近く太らせたりできる。メソッドも同じく切る。
func TestRecordQuotesAndClipsThePath(t *testing.T) {
	var file bytes.Buffer
	s := newTestServer(t, Options{Record: &file})
	h := s.handler()

	// トークンも Cookie も無く、Host も合わない要求。経路に改行（%0A）を入れて、
	// 時刻から始まる偽の1行を足そうとする。
	forged := "/x%0A20:00:00%20dwloc%20edit:%20POST%20/api/rows%20200%2012ms%20locale=ja%20edits=1%20saved=1"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, forged, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状態コードが %d、404 を期待", rec.Code)
	}
	// 通らなかった要求も記録には残す。
	lines := strings.Split(strings.TrimSuffix(file.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("要求1回で記録が %d 行になった（偽の行を足せる）:\n%s", len(lines), file.String())
	}
	if want := `dwloc edit: GET "/x\n20:00:00 dwloc edit: POST /api/rows 200 12ms locale=ja edits=1 saved=1" 404 `; !strings.HasPrefix(lines[0], want) {
		t.Errorf("記録が %q、%q で始まることを期待", lines[0], want)
	}

	// 長い経路は切る。どこで切ったか、元が何バイトだったかは残す。
	file.Reset()
	long := "/" + strings.Repeat("a", 5000)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, long, nil))
	got := file.String()
	if len(got) > 2*recordPathMax {
		t.Errorf("長い経路を切らずに書いている（%d バイト）", len(got))
	}
	if want := `"/` + strings.Repeat("a", recordPathMax-1) + `"…(5001 bytes) 404 `; !strings.Contains(got, want) {
		t.Errorf("記録が %q、%q を含むことを期待", got, want)
	}

	// 長いメソッドも切る。メソッドも、トークンを持たない相手が決められる。
	// net/http はヘッダーの上限（既定で 1MB）まで通すので、切らなければ経路と
	// 同じく、1回の要求で記録を太らされる。
	file.Reset()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(strings.Repeat("A", 5000), "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状態コードが %d、404 を期待", rec.Code)
	}
	got = file.String()
	if len(got) > 2*recordPathMax {
		t.Errorf("長いメソッドを切らずに書いている（%d バイト）", len(got))
	}
	if want := `dwloc edit: "` + strings.Repeat("A", recordMethodMax) + `"…(5000 bytes) "/" 404 `; !strings.HasPrefix(got, want) {
		t.Errorf("記録が %q、%q で始まることを期待", got, want)
	}
}

// TestRecordMethod は、記録に書くメソッドの形を見る。
//
// ふだんの GET と POST は囲まずに書き、行の形を README の例のまま保つ。
// 名前の無いメソッドは経路と同じく %q で囲み、長ければ切る。
func TestRecordMethod(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{method: http.MethodGet, want: "GET"},
		{method: http.MethodPost, want: "POST"},
		{method: http.MethodOptions, want: "OPTIONS"},
		{method: "PROPFIND", want: `"PROPFIND"`},
		// net/http を通らない呼び出しでも、改行で行を割らせない。
		{method: "A\nB", want: `"A\nB"`},
		{method: "", want: `""`},
		{method: strings.Repeat("A", recordMethodMax), want: `"` + strings.Repeat("A", recordMethodMax) + `"`},
		{
			method: strings.Repeat("A", recordMethodMax+1),
			want:   `"` + strings.Repeat("A", recordMethodMax) + `"…(` + strconv.Itoa(recordMethodMax+1) + " bytes)",
		},
	}
	for _, tt := range tests {
		if got := recordMethod(tt.method); got != tt.want {
			t.Errorf("recordMethod(%q) = %q、%q を期待", tt.method, got, tt.want)
		}
	}
}

// TestRecordClipsThePathOnARuneBoundary は、経路を切る位置が文字の途中に
// ならないことを見る。途中で切ると、%q が壊れた1バイトを \x の形で書き、
// 記録を読んだ人には元に無い文字が見える。
func TestRecordClipsThePathOnARuneBoundary(t *testing.T) {
	path := "/" + strings.Repeat("a", recordPathMax-2) + "あ"
	got := recordPath(path)
	if want := `"/` + strings.Repeat("a", recordPathMax-2) + `"…(` + strconv.Itoa(len(path)) + " bytes)"; got != want {
		t.Errorf("recordPath = %q、%q を期待", got, want)
	}
	if got, want := recordPath("/api/rows"), `"/api/rows"`; got != want {
		t.Errorf("recordPath = %q、%q を期待", got, want)
	}
}

func TestTokenIsHiddenFromRecord(t *testing.T) {
	// トークンが決まった直後に、伏せる相手として渡す。URL を画面に出すより前で
	// なければ間に合わない。画面はログファイルへも束ねられている。
	var hidden []string
	s := newTestServer(t, Options{HideFromRecord: func(secret string) {
		hidden = append(hidden, secret)
	}})

	if len(hidden) != 1 {
		t.Fatalf("伏せさせた回数が %d、1 を期待: %q", len(hidden), hidden)
	}
	if hidden[0] != s.token {
		t.Errorf("伏せさせたのがトークンではない")
	}
}
