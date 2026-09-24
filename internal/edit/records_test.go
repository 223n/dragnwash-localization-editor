package edit

import (
	"errors"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

/*
ここの試験は、保存をレコードの単位へ移す作業（全体を解釈する読み手へ移す作業の PR3）で
変わった編集モデルの結果を見る。

PR3 の最初のコミットでは、切り替える前の結果を名前の末尾が Now の試験で固定した
（決まったことの 13。ファイル名も records_now_test.go だった）。切り替えたコミットで
期待値を直し、名前から Now を外した。

  - 行はセグメント（レコード）で並び、保存は ID で引く。行をまたぐレコードは1つの行に
    なり、訳が1行に収まるかぎり書ける（以前は物理行で並び、行をまたぐレコードの
    どの物理行も編集できなかった）。
  - カンマだけの行（",,,,,,"）は空行相当で、編集させない（以前はキーの空いた編集できる
    行だった。改善の ui-15）。
  - 行の区切りが CR だけのファイルは全体を読み取り専用にし、ゲームの読み方と値が割れる
    レコードは編集させない（以前はどちらも編集できた）。
  - 飲み込みの疑いのあるレコードは、飲み込まれたと疑う物理行を添えて編集させない
    （以前は行をまたぐレコードの理由だった）。

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

// TestSavedRecordsAgreeAcrossReaders は、実物と同じ形の作業コピーで、原文が行をまたぐ
// レコードを含む複数のレコードに書いたあと、3つの読み手（ゲーム内Mod、publish、
// validate）がどのレコードのどの列も同じ値に読むことを見る。
func TestSavedRecordsAgreeAcrossReaders(t *testing.T) {
	f := Parse([]byte(nowWorking))
	writes := map[int]string{
		5:  "カンマ, と \"引用符\"",
		6:  " 前後に空白 ",
		9:  "すすいで、ふく",
		10: "#で始まる訳",
	}
	for id, v := range writes {
		if err := f.SetTranslation(id, v); err != nil {
			t.Fatalf("ID %d: %v", id, err)
		}
	}
	out := f.Bytes()

	game := csvfile.ReadCSharpRows(out)
	ps, err := csvfile.ReadPowerShell(out)
	if err != nil {
		t.Fatalf("主の読み手で読めない: %v", err)
	}
	py, err := csvfile.ReadPythonRecords(out)
	if err != nil {
		t.Fatalf("validate の読み手で読めない: %v", err)
	}
	if len(game) != 4 || len(ps.Records) != 4 || len(py) != 5 {
		t.Fatalf("レコードの数: ゲーム内Mod %d、主の読み手 %d、validate %d（ヘッダーを含む）", len(game), len(ps.Records), len(py))
	}
	for i, r := range ps.Records {
		for c, col := range workingHeader {
			if p, g, v := r.Get(col), game[i].Get(col), py[i+1].Fields[c]; p != g || p != v {
				t.Errorf("ID %d の %s が食い違う: 主の読み手 %q、ゲーム内Mod %q、validate %q", r.ID, col, p, g, v)
			}
		}
		if want, ok := writes[r.ID]; ok && r.Get("translation") != want {
			t.Errorf("ID %d の訳 = %q、%q を期待", r.ID, r.Get("translation"), want)
		}
	}
	if csvfile.FindSwallows(ps.Segments) != nil || csvfile.CSharpDisagreements(ps) != nil {
		t.Error("書いたあとのファイルが、飲み込みの疑いか読み方の食い違いに当たる")
	}
}

// TestEditCommaOnlyRowIsBlank は、値がどれも空になるレコード（",,,,,," の行）を
// 空行相当にし、編集させないことを見る（改善の ui-15）。キーも原文も空なので publish は
// この行を捨て（移植仕様 R17）、ここへ打った訳は黙って落ちる。
//
// PR3 の最初のコミットでは TestEditCommaOnlyRowIsEditableNow として、キーの空いた
// 編集できる行になることを固定していた。
func TestEditCommaOnlyRowIsBlank(t *testing.T) {
	const header = "key,section,node,order,speaker,source_en,translation\n"
	for _, row := range []string{",,,,,,", `"","","","","","",""`, " , ,,,,,"} {
		t.Run(row, func(t *testing.T) {
			f := Parse([]byte(header + row + "\n"))
			l, ok := f.Line(2)
			if !ok || l.Kind != KindBlank || l.Editable || l.Key() != "" {
				t.Fatalf("ID 2 = %+v、空行相当の行を期待", l)
			}
			var notEditable *NotEditableError
			if err := f.SetTranslation(2, "訳"); !errors.As(err, &notEditable) || notEditable.Cause.ID != reason.EditNotDataLine {
				t.Errorf("書けてしまう、または理由が違う: %v", err)
			}
		})
	}

	// 訳だけが入った行は、いままでどおり書ける（publish は捨てるが、訳は失われない）。
	// 訳を消すと空行相当になる。消すことは断らない。
	f := Parse([]byte(header + ",,,,,,古い\n"))
	if l, _ := f.Line(2); l.Kind != KindData || !l.Editable {
		t.Fatalf("訳だけの行 = %+v、編集できる行を期待", l)
	}
	if err := f.SetTranslation(2, ""); err != nil {
		t.Fatalf("訳を消せない: %v", err)
	}
	if l, _ := f.Line(2); l.Kind != KindBlank || l.Editable || l.Cause.ID != reason.EditNotRecord {
		t.Errorf("訳を消したあと = %+v、空行相当を期待", l)
	}
	if again, _ := Parse(f.Bytes()).Line(2); again.Kind != KindBlank {
		t.Errorf("読み直すと %v、空行相当を期待", again.Kind)
	}
}

// TestEditCROnlyFileIsReadOnly は、行の区切りが CR だけのファイルを全体で読み取り
// 専用にすることを見る。ゲームの読み方（CsvReader）は引用の外の CR を捨てるので、
// このファイルを1行と読み、どのレコードも値が割れる。
//
// PR3 の最初のコミットでは TestEditCROnlyFileIsEditableNow として、どのレコードも
// 編集できることを固定していた。
func TestEditCROnlyFileIsReadOnly(t *testing.T) {
	const data = "key,translation\r" + "0123456789abcdef,いち\r" + "fedcba9876543210,に\r"
	if got := csvfile.CSharpDisagreements(csvfile.ReadPowerShellMarked([]byte(data))); len(got) != 2 {
		t.Fatalf("前提が崩れた: ゲームの読み方と割れるレコードが %d 件（2件のはず）", len(got))
	}
	f := Parse([]byte(data))
	if !f.ReadOnly() || f.ReadOnlyCause().ID != reason.EditCROnly {
		t.Fatalf("読み取り専用の理由 = %v %+v、CR だけの改行を期待", f.ReadOnly(), f.ReadOnlyCause())
	}
	for _, id := range []int{2, 3} {
		if l, _ := f.Line(id); l.Editable || l.Cause.ID != reason.EditCROnly {
			t.Errorf("ID %d = %+v", id, l)
		}
		if err := f.SetTranslation(id, "訳"); !errors.Is(err, ErrReadOnly) {
			t.Errorf("ID %d に書けてしまう: %v", id, err)
		}
	}

	// LF や CRLF の区切りが1つでもあれば、ファイル全体は読み取り専用にしない。行末の
	// 単独の CR のせいでゲームの読み方と割れるレコードだけを、行ごとに断る。
	mixed := Parse([]byte("key,translation\n" + "0123456789abcdef,いち\r" + "fedcba9876543210,に\n" + "1111111111111111,さん\n"))
	if mixed.ReadOnly() {
		t.Fatalf("混ざったファイルが読み取り専用: %s", mixed.ReadOnlyReason())
	}
	want := []string{reason.EditGameDisagrees, reason.EditGameMissesRecord, ""}
	for i, id := range []int{2, 3, 4} {
		if l, _ := mixed.Line(id); l.Cause.ID != want[i] || l.Editable != (want[i] == "") {
			t.Errorf("ID %d = %+v、理由 %q を期待", id, l, want[i])
		}
	}
}

// TestEditGameReadsDifferentlyIsReadOnly は、ゲームの読み方と値が割れるレコードを
// 編集させないことを見る。フィールドの途中の '"' は、主の読み手ではただの文字、
// ゲームの読み方では引用の始まりになる（移植仕様「CSVとキー生成 R6」）。書くと、
// 翻訳者が見ている値とゲームが表示する値が食い違う。
//
// PR3 の最初のコミットでは TestEditGameReadsDifferentlyIsEditableNow として、編集
// できることを固定していた。
func TestEditGameReadsDifferentlyIsReadOnly(t *testing.T) {
	const data = "key,speaker,translation\n" + "0123456789abcdef,Fern,い\"ろ\"は\n" + "fedcba9876543210,Fern,に\n"
	f := Parse([]byte(data))
	l, _ := f.Line(2)
	if l.Editable || l.Cause.ID != reason.EditGameDisagrees || argOf(l.Cause, "column") != "translation" {
		t.Errorf("ID 2 = %+v、translation 列の食い違いを期待", l)
	}
	var notEditable *NotEditableError
	if err := f.SetTranslation(2, "いろは"); !errors.As(err, &notEditable) || notEditable.Cause.ID != reason.EditGameDisagrees {
		t.Errorf("書けてしまう、または理由が違う: %v", err)
	}
	if l, _ := f.Line(3); !l.Editable {
		t.Errorf("割れないレコードまで編集できない: %+v", l)
	}
}

// TestEditSwallowSuspectIsReadOnly は、飲み込みの疑い（csvfile.FindSwallows）のある
// レコードを、飲み込まれたと疑う物理行を添えて編集させないことを見る。publish の
// 形の確かめ (f) と同じ関数で見る。
//
// PR3 の最初のコミットでは TestEditSwallowSuspectIsMultilineNow として、行をまたぐ
// レコードと同じ理由で編集できないことを固定していた（保存をレコードの単位へ移した
// コミットでは、理由が訳の改行になっていた）。
func TestEditSwallowSuspectIsReadOnly(t *testing.T) {
	tests := []struct {
		name string
		data string
		line string
	}{
		{
			// 訳の開き引用符が閉じず、後ろの2行を飲み込む。
			name: "訳が飲み込む",
			data: "key,section,node,order,speaker,source_en,translation\r\n" +
				key.For("one") + ",UI,,,UI,one,\"いち\r\n" +
				key.For("two") + ",UI,,,UI,two,\r\n" +
				key.For("three") + ",UI,,,UI,three,さん\"\r\n",
			line: "3",
		},
		{
			// 原文の開き引用符が次の行で閉じ、訳は1行に収まる。訳の改行では断れない形。
			name: "原文が飲み込む",
			data: "key,section,node,order,speaker,source_en,translation\n" +
				"0123456789abcdef,UI,,,UI,\"one\n" +
				"fedcba9876543210,UI,,,UI,two\",いち\n",
			line: "3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := csvfile.FindSwallows(csvfile.SplitSegments([]byte(tt.data))); len(got) == 0 {
				t.Fatalf("前提が崩れた: 飲み込みの疑いが無い")
			}
			f := Parse([]byte(tt.data))
			l, _ := f.Line(2)
			if l.Editable || l.Cause.ID != reason.EditSwallow || argOf(l.Cause, "line") != tt.line {
				t.Errorf("ID 2 = %+v、%s行目の飲み込みの疑いを期待", l, tt.line)
			}
			if err := f.SetTranslation(2, "訳"); err == nil {
				t.Error("書けてしまう")
			}
		})
	}
}
