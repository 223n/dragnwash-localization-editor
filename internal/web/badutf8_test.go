package web

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestSaveRejectsABodyThatIsNotUTF8 は、本文のバイト列が UTF-8 でなければ
// 1バイトも書かないことを見る。
//
// この試験は実機で見つけた抜け道をそのまま写したものである。
// [encoding/json] は不正なバイトを U+FFFD へ黙って置き換えるので、置き換わった
// あとの文字列はもう正しい UTF-8 であり、[edit.File.SetTranslation] の UTF-8 の
// 検査（reason.EditBadUTF8）はこの経路では1度も立たない。
//
// 実測（この開発機、実データの ja.working.csv）では、CP932 の "82 C6 82 EA" を
// 訳に入れて送ると断りも警告も出ずに saved で返り、ファイルには
// "EF BF BD C6 82 EF BF BD" が書かれた。U+FFFD は正しい UTF-8 なので、
// この先のどの検査も止めない。publish はそれをコミットする側へ運ぶ。
func TestSaveRejectsABodyThatIsNotUTF8(t *testing.T) {
	// CP932 の「とれ」。UTF-8 としては不正なバイト列である。
	const cp932 = "\x82\xc6\x82\xea"
	if utf8.ValidString(cp932) {
		t.Fatal("見本が正しい UTF-8 になっている。壊れたバイト列で試していない")
	}

	s := newTestServer(t, Options{Root: newEditRoot(t)})
	path := inputPath(t, s, "ja")
	before := readFile(t, path)
	lines := getLines(t, s, "ja")

	// saveBody は json.Marshal を通るので使えない（あちらは通る前に U+FFFD へ
	// 直してしまう）。壊れたバイト列をそのまま本文に載せるため、手で組む。
	body := fmt.Sprintf(
		`{"locale":"ja","baseVersion":%q,"edits":[{"line":6,"translation":"%s"}]}`,
		lines.Version, cp932)
	rec := doPost(t, s, "/api/rows", body, nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状態コードが %d、%d を期待\n%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if after := readFile(t, path); after != before {
		t.Errorf("拒んだのにファイルが変わっている\n前: %q\n後: %q", before, after)
	}
	// 置き換わった U+FFFD がファイルへ入っていないこと。状態コードだけを見ると、
	// 「拒んだうえで書いていた」を見逃す。
	if strings.ContainsRune(readFile(t, path), utf8.RuneError) {
		t.Errorf("ファイルに U+FFFD が入っている:\n%s", readFile(t, path))
	}
	// 位置は返さない。何バイト目で壊れていたかは訳の長さを漏らす。
	if strings.Contains(rec.Body.String(), "82") || strings.Contains(rec.Body.String(), "byte") {
		t.Errorf("誤りの応答が壊れた位置を返している:\n%s", rec.Body.String())
	}
}

// TestSaveKeepsAReplacementCharacterThatWasSentProperly は、正しい UTF-8 として
// 送られた U+FFFD は通すことを見る。
//
// U+FFFD そのものを禁じる形は採っていない。壊れた原文をそのまま写した訳など、
// 元から U+FFFD を含む訳がありうる。止めたいのは「送られたバイト列が UTF-8
// ではない」ことだけである。ここが落ちるようになったら、守りが広がりすぎている。
func TestSaveKeepsAReplacementCharacterThatWasSentProperly(t *testing.T) {
	s := newTestServer(t, Options{Root: newEditRoot(t)})
	lines := getLines(t, s, "ja")

	want := "こわれた�もじ"
	rec := save(t, s, "ja", lines.Version, rowEdit{Line: 6, Translation: want})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	got := decode[rowsResponse](t, rec.Body.Bytes())
	if len(got.Results) != 1 || !got.Results[0].Saved {
		t.Fatalf("保存できていない: %+v", got.Results)
	}
	if got.Results[0].Translation != want {
		t.Errorf("読み直した値が %q、%q を期待", got.Results[0].Translation, want)
	}
}
