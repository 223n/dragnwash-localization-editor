package diff

import (
	"errors"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
)

/*
ここの試験は、diff が全体を解釈する読み方（csvfile.ReadPowerShell）で、行をまたぐ
レコードと閉じない引用符をどう読むかを固定する。

全体を解釈する読み手へ移す作業（docs/port-spec.md）の PR2 の最初のコミットでは、同じ
入力で、行単位で読んでいたときの結果（行をまたぐレコードが物理行ごとの行に割れる）を
固定していた（名前の末尾が LineBasedNow の試験。決まったことの 13）。読み手を
切り替えたコミットで、次のように期待値を直した。

  - ReadRows: 5行 → 3行（行をまたぐレコードが1行になる）
  - 実物と同じ形の作業コピー: 捨てられる行 2 → 0、未翻訳 1 → 2
  - 閉じない引用符: 行の終わりで閉じて読む → 読み込みの誤り
  - layout_risks: 3件 → 2件（原文が行をまたいでも1件）
  - CSV の読み戻し: 4行 → 2行（報告の件数と同じ）

見本の英文と訳はどれも架空の文である。
*/

// lbMultiSource は行をまたぐ原文。ゲーム側の作業コピーの実物にある形（LF が2つ、
// 間に空行）をまねた架空の文。
const lbMultiSource = "para1\n\npara2"

// lbWorkingCRLF は、ゲームが書く実物と同じく CRLF で区切った作業コピーのヘッダー。
const lbWorkingCRLF = "key,section,node,order,speaker,source_en,translation\r\n"

// TestReadRowsReadsMultilineRecords は、行をまたぐレコードを1つの行として読む
// ことを固定する。値の中の改行は読んだとおり（LF のまま）残る。
func TestReadRowsReadsMultilineRecords(t *testing.T) {
	keyMulti := key.For(lbMultiSource)
	csv := lbWorkingCRLF +
		keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",\"いち\nに\"\r\n" +
		keyMulti + ",UI,,,UI,\"" + lbMultiSource + "\",\r\n" +
		keyBye + ",L01 Ryan,Ryan_1_intro,2,Ryan," + srcBye + ",さん\r\n"

	rows, err := ReadRows([]byte(csv))
	if err != nil {
		t.Fatalf("ReadRows: %v", err)
	}
	want := []struct {
		key                   string
		kind                  Kind
		sourceEn, translation string
	}{
		{keyHello, KindHash, srcHello, "いち\nに"},
		{keyMulti, KindHash, lbMultiSource, ""},
		{keyBye, KindHash, srcBye, "さん"},
	}
	if len(rows) != len(want) {
		t.Fatalf("%d 行のはずが %d 行: %+v", len(want), len(rows), rows)
	}
	for i, w := range want {
		r := rows[i]
		if r.Key != w.key || r.Kind != w.kind || r.SourceEn != w.sourceEn || r.Translation != w.translation {
			t.Errorf("%d 行目 = {%q %v %q %q}, want {%q %v %q %q}",
				i+1, r.Key, r.Kind, r.SourceEn, r.Translation, w.key, w.kind, w.sourceEn, w.translation)
		}
	}
}

// TestCompareMultilineWorkingCopyCounts は、原文が行をまたぐ訳の空のレコードを持つ
// 作業コピー（実物と同じ形）の件数を固定する。
//
// そのレコードは原文のハッシュがキーと合う訳の空の行として読まれ、未翻訳に数える。
// 行単位で読んでいたときは、1行目（原文が para1 に切れてハッシュがキーと合わない）と
// 続きの行（キーの形でない）が「publish で捨てられる行」の2件になっていた。実物の
// ja 作業コピーでも、捨てられる行が 2 → 0、未翻訳が1件増える。
func TestCompareMultilineWorkingCopyCounts(t *testing.T) {
	repo := newRepo(t, map[string]string{
		"data/script_order.csv": orderTwo,
		"Translations/ja/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
		"Translations/_discovered/ja.working.csv": lbWorkingCRLF +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\r\n" +
			keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold," + srcBye + ",\r\n" +
			key.For(lbMultiSource) + ",UI,,,UI,\"" + lbMultiSource + "\",\r\n",
	}, true)
	rep := Compare(repo, nil)

	got := counts(t, rep, "ja")
	if got[CatDropped] != 0 {
		t.Errorf("publish で捨てられる行 = %d、0 を期待", got[CatDropped])
	}
	if got[CatUntranslated] != 2 {
		t.Errorf("未翻訳 = %d、2 を期待（行をまたぐレコードも数える）", got[CatUntranslated])
	}
}

// TestLoadUnclosedQuoteIsAnErrorForNow は、閉じない引用符のある公開ファイルを読むと、
// いまは読み込み全体が誤りになることを固定する。
//
// 全体を解釈する読み手は、閉じない引用符を型付きの誤り（csvfile.UnclosedQuoteError）に
// する。行単位で読んでいたときは、その物理行の終わりで閉じたものとして何事も無く
// 読んでいた。決まったことの 3 では、diff はそのファイルに依る判定を「判定して
// いません（N行目の引用符が閉じない）」にして続け、終了コードを1にする
// （そのほか 6）。それを入れるまでの途中の振る舞いで、入れたらこの試験を直す。
func TestLoadUnclosedQuoteIsAnErrorForNow(t *testing.T) {
	_, err := LoadWith(writeTree(t, map[string]string{
		"data/script_order.csv": orderTwo,
		"Translations/ja/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"こんにちは\n" +
			keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold,さようなら\n",
	}), Options{Working: true})
	var unclosed *csvfile.UnclosedQuoteError
	var fileErr *FileError
	if !errors.As(err, &unclosed) || unclosed.Line != 2 || !errors.As(err, &fileErr) {
		t.Fatalf("閉じない引用符の誤りをファイルつきで返していない: %v", err)
	}
	if !strings.HasSuffix(fileErr.Path, "strings.csv") {
		t.Errorf("どのファイルかが違う: %s", fileErr.Path)
	}
}

// TestParseLayoutRisksReadsMultilineSource は、原文が行をまたぐはみ出しの記録を
// 1件として読むことを固定する。原文が本物の原文になるので、作業コピーの行と同じ
// キーで結び付く。行単位で読んでいたときは、物理行ごとの2件に割れ、どちらの原文も
// 本物の原文のハッシュにならなかった。
func TestParseLayoutRisksReadsMultilineSource(t *testing.T) {
	data := []byte(layoutRisksHeader +
		"\"" + lbMultiSource + "\",訳,x,240,180,1.33,Canvas/Label\n" +
		layoutRow(srcHello, "こんにちは", "y", "50", "40", "1.25", "Canvas/Label2"))

	risks, err := ParseLayoutRisks(data)
	if err != nil {
		t.Fatalf("ParseLayoutRisks: %v", err)
	}
	if len(risks) != 2 {
		t.Fatalf("2 件のはずが %d 件: %+v", len(risks), risks)
	}
	if risks[0].SourceEn != lbMultiSource || risks[0].Translation != "訳" || risks[0].RatioText != "1.33" {
		t.Errorf("1件目 = %+v", risks[0])
	}
	if risks[1].SourceEn != srcHello {
		t.Errorf("2件目 = %+v", risks[1])
	}
}

// TestWriteCSVReadBackMultiline は、値に改行を含む報告を CSV に書いて読み戻すと、
// 報告と同じ件数と値に戻ることを固定する。書く側（csvfile.EscapeField）は改行を
// 含む値を引用して書き、読む側は全体を解釈する。
func TestWriteCSVReadBackMultiline(t *testing.T) {
	rep := &Report{Findings: []Finding{
		{Locale: "ja", Category: CatUntranslated, Key: key.For(lbMultiSource), SourceEn: lbMultiSource},
		{Locale: "ja", Category: CatVanished, Key: keyBye, Translation: "いち\nに"},
	}}
	var b strings.Builder
	if err := rep.WriteCSV(&b); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	if !strings.Contains(b.String(), "\""+lbMultiSource+"\"") {
		t.Fatalf("改行を含む値を引用して書いていない:\n%s", b.String())
	}

	rows, err := ReadRows([]byte(b.String()))
	if err != nil {
		t.Fatalf("読み戻せない: %v", err)
	}
	if len(rows) != len(rep.Findings) {
		t.Fatalf("読み戻した行 %d、報告は %d 件: %+v", len(rows), len(rep.Findings), rows)
	}
	if rows[0].SourceEn != lbMultiSource || rows[1].Translation != "いち\nに" {
		t.Errorf("値が戻っていない: %+v", rows)
	}
}
