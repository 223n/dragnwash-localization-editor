package diff

import (
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
)

/*
ここの試験は、全体を解釈する読み手へ移す作業（docs/port-spec.md）の PR2 で、diff が
行単位の読み方（csvfile.ReadPowerShellRows）から全体を解釈する読み方
（csvfile.ReadPowerShell）へ移る前の結果を固定する（決まったことの 13）。

どれも名前の末尾が LineBasedNow で、「行単位で読むいまの結果」を固定するだけで、
正しいとはしない。行単位の読み方は、引用符で囲んだ値が行をまたぐと、物理行ごとに
別の行へ割る。読み手を切り替えるコミットで期待値が変わるので、そこで直す
（どのコミットで何が変わったかは PR の説明に書く）。

見本の英文と訳はどれも架空の文である。
*/

// lbMultiSource は行をまたぐ原文。ゲーム側の作業コピーの実物にある形（LF が2つ、
// 間に空行）をまねた架空の文。
const lbMultiSource = "para1\n\npara2"

// lbWorkingCRLF は、ゲームが書く実物と同じく CRLF で区切った作業コピーのヘッダー。
const lbWorkingCRLF = "key,section,node,order,speaker,source_en,translation\r\n"

// TestReadRowsSplitsMultilineRecordsLineBasedNow は、行をまたぐレコードを物理行ごとの
// 行に割るいまの読み方を固定する。
//
// 訳が行をまたぐレコード（2〜3行目）と、原文が空行を挟んで行をまたぐレコード
// （4〜6行目）がある。全体を解釈すると3件だが、行単位では5件になる。
// 続きの行（`に"` と `para2"`）は、それだけで壊れたキーの行として読まれる。
func TestReadRowsSplitsMultilineRecordsLineBasedNow(t *testing.T) {
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
		{keyHello, KindHash, srcHello, "いち"},
		{`に"`, KindBroken, "", ""},
		{keyMulti, KindHash, "para1", ""},
		{`para2"`, KindBroken, "", ""},
		{keyBye, KindHash, srcBye, "さん"},
	}
	if len(rows) != len(want) {
		t.Fatalf("行単位では %d 行に割れるはずが %d 行: %+v", len(want), len(rows), rows)
	}
	for i, w := range want {
		r := rows[i]
		if r.Key != w.key || r.Kind != w.kind || r.SourceEn != w.sourceEn || r.Translation != w.translation {
			t.Errorf("%d 行目 = {%q %v %q %q}, want {%q %v %q %q}",
				i+1, r.Key, r.Kind, r.SourceEn, r.Translation, w.key, w.kind, w.sourceEn, w.translation)
		}
	}
}

// TestCompareMultilineWorkingCopyCountsLineBasedNow は、原文が行をまたぐ訳の空の
// レコードを持つ作業コピー（実物と同じ形）の件数を固定する。
//
// 行単位では、そのレコードの1行目（原文が para1 に切れてハッシュがキーと合わない）と
// 続きの行（キーの形でない）が「publish で捨てられる行」の2件になり、未翻訳には
// 数えない。実物の ja 作業コピーでも、捨てられる行が2件になっている。
func TestCompareMultilineWorkingCopyCountsLineBasedNow(t *testing.T) {
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
	if got[CatDropped] != 2 {
		t.Errorf("publish で捨てられる行 = %d、行単位では 2", got[CatDropped])
	}
	if got[CatUntranslated] != 1 {
		t.Errorf("未翻訳 = %d、行単位では 1（行をまたぐレコードは数えない）", got[CatUntranslated])
	}
}

// TestLoadUnclosedQuoteLineBasedNow は、閉じない引用符のある作業コピーと公開
// ファイルを、行単位で読んで何事も無く読み込むいまの振る舞いを固定する。
//
// 行単位の読み方は、閉じない引用符をその物理行の終わりで閉じる。全体を解釈すると
// ファイルの終わりまでが1つの値になるので、読み手は型付きの誤り
// （csvfile.UnclosedQuoteError）を返す（決まったことの 3。diff はそのファイルに依る
// 判定を「判定していません」にして続ける）。
func TestLoadUnclosedQuoteLineBasedNow(t *testing.T) {
	repo := newRepo(t, map[string]string{
		"data/script_order.csv": orderTwo,
		"Translations/ja/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"こんにちは\n" +
			keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold,さようなら\n",
		"Translations/_discovered/ja.working.csv": workingHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",\"こんにちは\n" +
			keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold," + srcBye + ",\n",
	}, true)

	loc := repo.Locales[0]
	if len(loc.Published) != 2 || loc.Published[0].Translation != "こんにちは" {
		t.Errorf("公開ファイルを行単位で2行に読むはず: %+v", loc.Published)
	}
	if !loc.HasWorking || len(loc.Working) != 2 || loc.Working[0].Translation != "こんにちは" {
		t.Errorf("作業コピーを行単位で2行に読むはず: %+v", loc.Working)
	}
	if got := counts(t, Compare(repo, nil), "ja"); got[CatUntranslated] != 1 {
		t.Errorf("未翻訳 = %d、行単位では 1", got[CatUntranslated])
	}
}

// TestParseLayoutRisksSplitsMultilineSourceLineBasedNow は、原文が行をまたぐ
// はみ出しの記録を、物理行ごとの2件に割るいまの読み方を固定する。
//
// 1件目は原文が para1 に切れ、ほかの列は空になる。2件目は続きの行で、原文が
// `para2"` になり、訳と大きさの列はこちらに入る。どちらの原文も本物の原文の
// ハッシュにならないので、作業コピーの行と結び付かない。
func TestParseLayoutRisksSplitsMultilineSourceLineBasedNow(t *testing.T) {
	data := []byte(layoutRisksHeader +
		"\"" + lbMultiSource + "\",訳,x,240,180,1.33,Canvas/Label\n" +
		layoutRow(srcHello, "こんにちは", "y", "50", "40", "1.25", "Canvas/Label2"))

	risks, err := ParseLayoutRisks(data)
	if err != nil {
		t.Fatalf("ParseLayoutRisks: %v", err)
	}
	if len(risks) != 3 {
		t.Fatalf("行単位では 3 件に割れるはずが %d 件: %+v", len(risks), risks)
	}
	if risks[0].SourceEn != "para1" || risks[0].Translation != "" || risks[0].RatioText != "" {
		t.Errorf("1件目 = %+v、原文だけが para1 に切れるはず", risks[0])
	}
	if risks[1].SourceEn != `para2"` || risks[1].Translation != "訳" || risks[1].RatioText != "1.33" {
		t.Errorf("2件目 = %+v、続きの行が原文 para2\" として読まれるはず", risks[1])
	}
	if risks[2].SourceEn != srcHello {
		t.Errorf("3件目 = %+v", risks[2])
	}
}

// TestWriteCSVReadBackMultilineLineBasedNow は、値に改行を含む報告を CSV に書いて
// 読み戻すと、行単位の読み方では行が割れることを固定する。
//
// 書く側（csvfile.EscapeField）は改行を含む値を引用して書くので、書いたものは
// 正しい CSV である。割れるのは読み戻す側の読み方のせいである。
func TestWriteCSVReadBackMultilineLineBasedNow(t *testing.T) {
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
	// 1件目は "para1 の行と para2" の行に、2件目は "いち の行と に" の行に割れる。
	// 間の空行は落ちる。
	if len(rows) != 4 {
		t.Fatalf("行単位では 4 行に割れるはずが %d 行（報告は %d 件）: %+v", len(rows), len(rep.Findings), rows)
	}
}
