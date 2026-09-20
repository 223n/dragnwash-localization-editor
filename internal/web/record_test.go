package web

import (
	"bytes"
	"net/http"
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
	for _, want := range []string{"GET /api/lines 200", "locale=ja", "lines="} {
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

	if !strings.Contains(screen.String(), "GET /api/lines 200") {
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
