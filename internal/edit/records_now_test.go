package edit

import (
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

/*
ここの試験は、保存をレコードの単位へ移す作業（全体を解釈する読み手へ移す作業の PR3）で
変わる編集モデルの結果を見る。

PR3 の最初のコミットでは、切り替える前の結果を名前の末尾が Now の試験で固定した
（決まったことの 13）。切り替えたコミットで期待値を直し、名前から Now を外した。
Now のまま残っているものは、まだ切り替えていない結果である。

  - 行はセグメント（レコード）で並び、保存は ID で引く。行をまたぐレコードは1つの行に
    なり、訳が1行に収まるかぎり書ける（以前は物理行で並び、行をまたぐレコードの
    どの物理行も編集できなかった）。
  - カンマだけの行（",,,,,,"）は、キーの空いた編集できる行になる（改善の ui-15）。
  - 行の区切りが CR だけのファイルも、ゲームの読み方と値が割れる行も編集できる。
  - 飲み込みの疑いのあるレコードは、訳に改行がある理由で編集できない。

見本の英文と訳はどれも架空の文である。
*/

// nowPara は、実物の作業コピーにある形（原文が空行を挟んで3物理行にまたがり、訳の
// 空いたレコード）をまねた架空の原文。続きの行には '#' で始まる行と、カンマの多い
// 行を入れる。カンマの数はヘッダーの列数より少なく、飲み込みの疑いには当たらない。
const nowPara = "Rinse the plates, cups, and bowls.\n\n# Then dry, stack, and sort them."

// nowWorking は、実物と同じ形の作業コピー（レコードの区切りは CRLF、値の中は LF）。
//
// 物理行:
//
//	1 ヘッダー / 2 空行 / 3・4 見出し / 5 訳あり / 6 訳が空 / 7 空行 / 8 見出し /
//	9〜11 原文が行をまたぐレコード（10 は値の中の空行、11 は値の中の '#' の行）/
//	12 訳あり
var nowWorking = "key,section,node,order,speaker,source_en,translation\r\n" +
	"\r\n" +
	"# ===== Level 1: Fern (Sunny) | sets level_1 =====\r\n" +
	"# --- intro: Fern_1_intro ---\r\n" +
	key.For("Hello?") + ",L01 Fern,Fern_1_intro,1,Fern,Hello?,もしもし？\r\n" +
	key.For("Bye.") + ",L01 Fern,Fern_1_intro,2,Fern,Bye.,\r\n" +
	"\r\n" +
	"# ===== UI and other text (not part of the dialogue script) =====\r\n" +
	key.For(nowPara) + ",UI,,,UI,\"" + nowPara + "\",\r\n" +
	key.For("Start") + ",UI,,,UI,Start,はじめる\r\n"

// TestEditListsRecords は、行がセグメント（レコード）で並び、保存を ID で引くことを
// 見る。行をまたぐレコードは1つの行になり、訳が1行に収まるかぎり書ける。書いても
// 変わるのはそのレコードの最終フィールドだけで、訳を空に戻すと元のバイト列に戻る。
//
// PR3 の最初のコミットでは TestEditListsPhysicalLinesNow として、行が物理行で並び、
// 続きの行も1行ずつ並んでどれも編集できず、保存を物理行の番号で引くことを固定していた。
func TestEditListsRecords(t *testing.T) {
	f := Parse([]byte(nowWorking))
	if f.ReadOnly() {
		t.Fatalf("読み取り専用になった: %s", f.ReadOnlyReason())
	}
	lines := f.Lines()
	if len(lines) != 10 || f.PhysicalLines() != 12 {
		t.Fatalf("行が %d 行（物理行 %d 行）、10 行（物理行 12 行）を期待", len(lines), f.PhysicalLines())
	}
	wantKinds := []Kind{KindHeader, KindBlank, KindComment, KindComment, KindData, KindData,
		KindBlank, KindComment, KindData, KindData}
	wantNumbers := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 12}
	for i, l := range lines {
		end := wantNumbers[i]
		if i == 8 {
			end = 11
		}
		if l.ID != i+1 || l.Number != wantNumbers[i] || l.EndNumber != end || l.Kind != wantKinds[i] {
			t.Errorf("%d番目 = {ID %d, %d〜%d, %v}、{ID %d, %d〜%d, %v} を期待",
				i, l.ID, l.Number, l.EndNumber, l.Kind, i+1, wantNumbers[i], end, wantKinds[i])
		}
	}
	para := lines[8]
	if !para.Editable || para.Key() != key.For(nowPara) || para.Fields[5] != nowPara {
		t.Errorf("原文が行をまたぐレコード = %+v", para)
	}

	// 原文が行をまたぐレコード（ID 9）と、その後ろのレコード（ID 10。12行目）に書く。
	if err := f.SetTranslation(9, "すすぐ"); err != nil {
		t.Fatalf("ID 9 に書けない: %v", err)
	}
	if err := f.SetTranslation(10, "スタート"); err != nil {
		t.Fatalf("ID 10 に書けない: %v", err)
	}
	want := strings.Replace(nowWorking, "sort them.\",\r\n", "sort them.\",すすぐ\r\n", 1)
	want = strings.Replace(want, ",Start,はじめる\r\n", ",Start,スタート\r\n", 1)
	if got := string(f.Bytes()); got != want {
		t.Errorf("書いた結果が違う\n got %q\nwant %q", got, want)
	}
	if err := f.SetTranslation(9, ""); err != nil {
		t.Fatalf("ID 9 を空に戻せない: %v", err)
	}
	if err := f.SetTranslation(10, "はじめる"); err != nil {
		t.Fatalf("ID 10 を戻せない: %v", err)
	}
	if got := string(f.Bytes()); got != nowWorking {
		t.Errorf("元のバイト列に戻らない\n got %q\nwant %q", got, nowWorking)
	}
}

// TestEditCommaOnlyRowIsEditableNow は、カンマだけの行が、キーの空いた編集できる行に
// なることを固定する（改善の ui-15）。publish はキーも原文も空の行を捨てるので、
// ここへ打った訳は黙って落ちる。
func TestEditCommaOnlyRowIsEditableNow(t *testing.T) {
	const data = "key,section,node,order,speaker,source_en,translation\n" + ",,,,,,\n"
	f := Parse([]byte(data))
	l, ok := f.Line(2)
	if !ok || l.Kind != KindData || !l.Editable || l.Key() != "" {
		t.Fatalf("2行目 = %+v、キーの空いた編集できる行を期待", l)
	}
	if err := f.SetTranslation(2, "訳"); err != nil {
		t.Fatalf("書けない: %v", err)
	}
	if got := string(f.Bytes()); got != "key,section,node,order,speaker,source_en,translation\n,,,,,,訳\n" {
		t.Errorf("書いた結果 = %q", got)
	}
}

// TestEditCROnlyFileIsEditableNow は、行の区切りが CR だけのファイルも編集できることを
// 固定する。ゲームの読み方（CsvReader）は引用の外の CR を捨てるので、このファイルを
// 1行と読み、どのレコードも値が割れる。
func TestEditCROnlyFileIsEditableNow(t *testing.T) {
	const data = "key,translation\r" + "0123456789abcdef,いち\r" + "fedcba9876543210,に\r"
	f := Parse([]byte(data))
	if f.ReadOnly() {
		t.Fatalf("読み取り専用になった: %s", f.ReadOnlyReason())
	}
	if got := csvfile.CSharpDisagreements(csvfile.ReadPowerShellMarked([]byte(data))); len(got) != 2 {
		t.Fatalf("前提が崩れた: ゲームの読み方と割れるレコードが %d 件（2件のはず）", len(got))
	}
	for _, n := range []int{2, 3} {
		if l, _ := f.Line(n); !l.Editable {
			t.Errorf("%d行目が編集できない: %+v", n, l)
		}
	}
}

// TestEditGameReadsDifferentlyIsEditableNow は、ゲームの読み方と値が割れるレコードも
// 編集できることを固定する。フィールドの途中の '"' は、主の読み手ではただの文字、
// ゲームの読み方では引用の始まりになる（移植仕様「CSVとキー生成 R6」）。
func TestEditGameReadsDifferentlyIsEditableNow(t *testing.T) {
	const data = "key,speaker,translation\n" + "0123456789abcdef,Fern,い\"ろ\"は\n"
	if got := csvfile.CSharpDisagreements(csvfile.ReadPowerShellMarked([]byte(data))); len(got) != 1 || got[0].Column != "translation" {
		t.Fatalf("前提が崩れた: ゲームの読み方との食い違い = %+v", got)
	}
	f := Parse([]byte(data))
	if l, _ := f.Line(2); !l.Editable || l.Translation() != "い\"ろ\"は" {
		t.Errorf("2行目 = %+v、訳 い\"ろ\"は の編集できる行を期待", l)
	}
}

// TestEditSwallowSuspectIsMultilineNow は、飲み込みの疑いのあるレコードが、訳に改行が
// ある理由で編集できないことを固定する。
//
// PR3 の最初のコミットでは、どの物理行も行をまたぐレコードの理由で編集できないことを
// 固定していた。保存をレコードの単位へ移したコミットで、レコード1つの行になり、理由が
// 訳の改行に変わった。
func TestEditSwallowSuspectIsMultilineNow(t *testing.T) {
	data := "key,section,node,order,speaker,source_en,translation\r\n" +
		key.For("one") + ",UI,,,UI,one,\"いち\r\n" +
		key.For("two") + ",UI,,,UI,two,\r\n" +
		key.For("three") + ",UI,,,UI,three,さん\"\r\n"
	if got := csvfile.FindSwallows(csvfile.SplitSegments([]byte(data))); len(got) != 2 {
		t.Fatalf("前提が崩れた: 飲み込みの疑い = %+v", got)
	}
	f := Parse([]byte(data))
	if l, _ := f.Line(2); l.Number != 2 || l.EndNumber != 4 || l.Editable || l.Cause.ID != reason.EditMultilineTranslation {
		t.Errorf("ID 2 = %+v、訳に改行がある理由の2〜4行目のレコードを期待", l)
	}
}
