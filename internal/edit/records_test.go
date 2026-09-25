package edit

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
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
    行だった。改善の ui-15）。publish がキーを決められないレコード（原文が空で key 列が
    16桁のキーでも台詞ID でもない。",UI,,,UI,,訳" や2列の "hello,訳" など）も、理由を
    付けて編集させない（以前は編集できた。決まったことの 24）。
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
	// 訳だけが入った行（",,,,,,古い"）は、値がどれも空ではないので空行相当にはならず、
	// publish がキーを決められない理由で編集させない（TestEditKeylessRowIsReadOnly）。
}

// 行の形の見本に使うヘッダー（受理される4種）。
const (
	headerWorking   = "key,section,node,order,speaker,source_en,translation\n"
	headerPublished = "key,section,node,order,speaker,translation\n"
	headerSpeaker   = "key,speaker,translation\n"
	headerPair      = "key,translation\n"
)

// TestEditKeylessRowIsReadOnly は、publish がキーを決められないレコード（原文が空で、
// key 列が16桁のキーでも台詞ID でもない）を、理由を付けて編集させないことを見る
// （決まったことの 24）。publish はこのレコードを捨てる（移植仕様 R17）ので、書いた訳は
// 公開されない。値がどれも空のレコード（改善の ui-15）と同じ理由である。
//
// PR3 のはじめは、訳だけが入った行はいままでどおり書けて、訳を消すと空行相当になった
// （消すことは断らなかった。そのころは TestEditCommaOnlyRowIsBlank の後半と
// TestRefreshRecomputesKind が見ていた）。次に key 列も原文も空のレコードだけを編集
// させないようにし（そのころの名前は TestEditRowWithoutKeyOrSourceIsReadOnly）、publish
// の規則にそろえて、key 列に16桁のキーでない値があるレコードまで広げた。
func TestEditKeylessRowIsReadOnly(t *testing.T) {
	tests := []struct {
		name, data string
	}{
		// key 列も原文も空。
		{"作業コピーで訳だけ", headerWorking + ",UI,,,UI,,訳\n"},
		{"作業コピーで訳も空", headerWorking + ",UI,,,UI,,\n"},
		{"作業コピーで値がみな空で訳だけ", headerWorking + ",,,,,,古い\n"},
		{"key 列が空白だけ", headerWorking + "\"  \",UI,,,UI,,訳\n"},
		{"2列で訳だけ", headerPair + ",訳\n"},
		{"3列で訳だけ", headerSpeaker + ",Fern,訳\n"},
		{"公開ファイルの6列で訳だけ", headerPublished + ",UI,,,UI,訳\n"},
		// key 列に値があるが、16桁のキーでも台詞ID でもなく、原文が空。
		{"2列の公開ファイルで key が16桁のキーでない", headerPair + "hello,訳\n"},
		{"3列で key が16桁のキーでない", headerSpeaker + "hello,Fern,訳\n"},
		{"6列で key が16桁のキーでない", headerPublished + "abc,UI,,,UI,訳\n"},
		{"7列で key が16桁のキーでなく原文が空", headerWorking + "hello,UI,,,UI,,訳\n"},
		{"key が15桁", headerPair + "0123456789abcde,訳\n"},
		{"key が17桁", headerPair + "0123456789abcdef0,訳\n"},
		{"key に16進でない字がある", headerPair + "0123456789abcdeg,訳\n"},
		// 台詞ID の接頭辞は大文字小文字を区別する（key.LooksLikeLineID）。
		{"台詞ID の接頭辞が大文字", headerPair + "LINE:intro_1,訳\n"},
		{"台詞ID の接頭辞だけ", headerPair + "line:,訳\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Parse([]byte(tt.data))
			l, ok := f.Line(2)
			if !ok || l.Kind != KindData || l.Editable || l.Cause.ID != reason.EditNoKeyOrSource || l.Text == "" {
				t.Fatalf("ID 2 = %+v、publish がキーを決められない理由を期待", l)
			}
			var notEditable *NotEditableError
			if err := f.SetTranslation(2, "新しい訳"); !errors.As(err, &notEditable) || notEditable.Cause.ID != reason.EditNoKeyOrSource {
				t.Errorf("書けてしまう、または理由が違う: %v", err)
			}
			if err := f.SetTranslation(2, ""); !errors.As(err, &notEditable) {
				t.Errorf("訳を消せてしまう: %v", err)
			}
			if f.Dirty() || string(f.Bytes()) != tt.data {
				t.Error("断ったのにモデルが変わった")
			}
		})
	}

	// 書ける形。原文があれば publish は原文からキーを作る（移植仕様 R14）。原文が空でも、
	// key 列が台詞ID（R12）か、前後の空白を除き小文字にして16桁のキー（R11・R13・R16）
	// なら、そのキーで引く。原文があって key 列と合わないレコード（R15）も publish は
	// 捨てるが、ここには当てない（diff が「publish で捨てられる行」として知らせる）。
	// 原文はトリムしないので、空白だけの原文も原文である。
	writable := []struct {
		name, data string
	}{
		{"原文があって key 列が空", headerWorking + ",UI,,,UI,Start,はじめる\n"},
		{"原文と key 列が合う", headerWorking + key.For("Start") + ",UI,,,UI,Start,はじめる\n"},
		{"原文があって key 列が合わない", headerWorking + "hello,UI,,,UI,Start,はじめる\n"},
		{"原文があって key 列が別の16桁のキー", headerWorking + "0123456789abcdef,UI,,,UI,Start,はじめる\n"},
		{"原文が空白だけ", headerWorking + ",UI,,,UI,\" \",訳\n"},
		{"原文が空で key 列が16桁のキー", headerWorking + "0123456789abcdef,UI,,,UI,,おわる\n"},
		{"2列で16桁のキー", headerPair + "0123456789abcdef,訳\n"},
		{"2列で前後に空白のある16桁のキー", headerPair + "\"  0123456789abcdef  \",訳\n"},
		{"2列で大文字の16桁のキー", headerPair + "0123456789ABCDEF,訳\n"},
		{"2列で台詞ID", headerPair + "line:intro_1,訳\n"},
		{"3列で台詞ID", headerSpeaker + "line:intro_1,Fern,訳\n"},
		{"6列で16桁のキー", headerPublished + "0123456789abcdef,UI,,,UI,訳\n"},
	}
	for _, tt := range writable {
		t.Run(tt.name, func(t *testing.T) {
			f := Parse([]byte(tt.data))
			if l, _ := f.Line(2); !l.Editable {
				t.Fatalf("ID 2 = %+v、編集できる行を期待", l)
			}
			if err := f.SetTranslation(2, "新しい訳"); err != nil {
				t.Errorf("書けない: %v", err)
			}
		})
	}
}

// TestEditSpacesAroundKeyColumn は、key 列の16桁のキーや台詞ID の前後に空白があるとき、
// publish はキーとして読む（キーを決められない理由には当たらない）が、引用符で囲まない
// 空白はゲームの読み方と食い違うことがあり、そのときは食い違いの理由で編集させないことを
// 見る（README の「書き換えられない行」）。
//
// 引用符で囲まない値は、publish の読み方（ConvertFrom-Csv）では先頭の空白とタブを削り、
// 末尾の空白を1つだけ残す（csvfile の trimPowerShellTrailing）。ゲームの読み方は削らない
// ので、先頭の空白と、2つ以上続く末尾の空白で値が割れる。引用した値の空白は、どちらの
// 読み方でも値に残る。
func TestEditSpacesAroundKeyColumn(t *testing.T) {
	const hex = "0123456789abcdef"
	tests := []struct {
		name, key string
		editable  bool
	}{
		{"先頭の空白", " " + hex, false},
		{"先頭のタブ", "\t" + hex, false},
		{"前後の空白", " " + hex + " ", false},
		{"末尾に2つの空白", hex + "  ", false},
		{"台詞ID の先頭の空白", " line:intro_1", false},
		{"台詞ID の末尾に2つの空白", "line:intro_1  ", false},
		{"末尾に1つの空白", hex + " ", true},
		{"末尾に1つのタブ", hex + "\t", true},
		{"引用した前後の空白", `"  ` + hex + `  "`, true},
		{"引用した台詞ID の前後の空白", `" line:intro_1 "`, true},
		{"台詞ID の末尾に1つの空白", "line:intro_1 ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := headerPair + tt.key + ",訳\n"
			if _, stats, err := publish.Build(nil, []byte(data), nil); err != nil || stats.Dropped != 0 {
				t.Fatalf("前提が崩れた: publish がこの行を捨てる（%v、%s）", err, stats.LogLine("out", "in"))
			}
			f := Parse([]byte(data))
			l, _ := f.Line(2)
			if tt.editable {
				if !l.Editable {
					t.Fatalf("ID 2 = %+v、編集できる行を期待", l)
				}
				if err := f.SetTranslation(2, "新しい訳"); err != nil {
					t.Errorf("書けない: %v", err)
				}
				return
			}
			if l.Editable || l.Cause.ID != reason.EditGameDisagrees || argOf(l.Cause, "column") != "key" {
				t.Fatalf("ID 2 = %+v、key 列の食い違いの理由を期待", l)
			}
			var notEditable *NotEditableError
			if err := f.SetTranslation(2, "新しい訳"); !errors.As(err, &notEditable) || notEditable.Cause.ID != reason.EditGameDisagrees {
				t.Errorf("書けてしまう、または理由が違う: %v", err)
			}
		})
	}
}

// TestKeylessAgreesWithPublish は、publish がキーを決められない理由で編集させない行が、
// 同じ入力で publish が捨てて集計の malformed dropped に数える行と食い違わないことを見る
// （決まったことの 24）。判定は publish と同じ関数（publish.Keyless）を呼ぶが、呼び方
// （ヘッダーと値の組み方）がずれても食い違うので、実際に publish.Build を走らせて比べる。
//
// 原文があって key 列と合わない行（R15）は、publish が捨てても編集させる（diff が
// 「publish で捨てられる行」として知らせる）。そのため、比べるのは原文が空の行だけで、
// 原文のある行は publish が捨てるかどうかにかかわらず編集できることを見る。
func TestKeylessAgreesWithPublish(t *testing.T) {
	var keyless, dropped, kept int
	for _, header := range []string{headerWorking, headerPublished, headerSpeaker, headerPair} {
		columns := strings.Split(strings.TrimSuffix(header, "\n"), ",")
		sources := []string{""}
		if slices.Contains(columns, "source_en") {
			sources = append(sources, "Start", " ")
		}
		for _, k := range keyColumnSamples {
			for _, src := range sources {
				data := header + sampleRecord(columns, k, src, "訳")
				_, stats, err := publish.Build(nil, []byte(data), nil)
				if err != nil {
					t.Fatalf("%q: publish.Build: %v", data, err)
				}
				l, _ := Parse([]byte(data)).Line(2)
				isKeyless := !l.Editable && l.Cause.ID == reason.EditNoKeyOrSource
				switch {
				case src == "" && isKeyless != (stats.Dropped == 1):
					t.Errorf("%q: 編集させない %v、publish が捨てた行 %d（%s）", data, isKeyless, stats.Dropped, stats.LogLine("out", "in"))
				case src != "" && !l.Editable:
					t.Errorf("%q: 原文のある行が編集できない（%+v）", data, l)
				case src == "" && !isKeyless && !l.Editable:
					t.Errorf("%q: ほかの理由で編集できない（%+v）", data, l)
				}
				if isKeyless {
					keyless++
					if !strings.Contains(stats.LogLine("out", "in"), "1 malformed dropped") {
						t.Errorf("%q: 集計に malformed dropped として出ない: %s", data, stats.LogLine("out", "in"))
					}
				}
				if src != "" && stats.Dropped == 1 {
					dropped++ // R15。publish は捨てるが編集できる
				}
				if src == "" && !isKeyless {
					kept++
				}
			}
		}
	}
	// 見本が痩せていないこと（どの組み合わせも一度は通る）。
	if keyless == 0 || dropped == 0 || kept == 0 {
		t.Errorf("見本が偏っている: 編集させない %d、原文があって捨てられる %d、原文が空で書ける %d", keyless, dropped, kept)
	}
}

// keyColumnSamples は、publish がキーを決められるかの境目を突く key 列の値。キーを
// 決められない形（空、空白だけ、16桁のキーの形でない値、大文字の接頭辞や接頭辞だけの
// 台詞ID、15桁、16進でない字）と、決められる形（台詞ID、16桁のキー、前後に空白のある
// 16桁のキー、大文字の16桁のキー）を混ぜる。
var keyColumnSamples = []string{
	"", "  ", "hello", "abc", "LINE:intro_1", "line:", "line:intro_1",
	"0123456789abcdef", "  0123456789abcdef  ", "0123456789ABCDEF",
	"0123456789abcde", "0123456789abcdeg", key.For("Start"),
}

// sampleRecord は、列 columns のヘッダーのもとで、key 列が k、原文が src、訳が tr の
// レコードを1行（改行で終わる）にする。ほかの列は UI の行の値で埋める。
func sampleRecord(columns []string, k, src, tr string) string {
	values := map[string]string{"section": "UI", "node": "", "order": "", "speaker": "UI"}
	fields := make([]string, len(columns))
	for i, col := range columns {
		switch col {
		case "key":
			fields[i] = csvField(k)
		case "source_en":
			fields[i] = csvField(src)
		case "translation":
			fields[i] = csvField(tr)
		default:
			fields[i] = values[col]
		}
	}
	return strings.Join(fields, ",") + "\n"
}

// TestKeylessRowInPublishedFileStopsPublish は、publish がキーを決められない理由で編集
// させない行に訳が入っているとき、publish がどうなるかを、開いているファイルごとに見る
// （README の「書き換えられない行」と移植仕様の edit_no_key_or_source）。
//
//   - 作業コピーの無いロケールで公開ファイル自身を開いているとき（publish の入力と
//     書き出し先が同じ）は、失われる訳の確かめ（publish.CheckLoss）がその行を拾い、
//     publish は止まる。画面はその行の訳を空にする書き換えも断るので、止まった状態は
//     画面からはほどけない。編集できる行は、同じ形で置いても止めない。
//   - 作業コピーを開いているときは、publish はその行を捨てるだけで止まらない（いまの
//     公開ファイルにその行は無いので、失われる訳に数えない）。
func TestKeylessRowInPublishedFileStopsPublish(t *testing.T) {
	var stopped, passed int
	for _, header := range []string{headerPublished, headerSpeaker, headerPair} {
		columns := strings.Split(strings.TrimSuffix(header, "\n"), ",")
		for _, k := range keyColumnSamples {
			// 公開ファイル自身が入力で、同じファイルへ書き出す。
			current := header + sampleRecord(columns, "fedcba9876543210", "", "既訳") +
				sampleRecord(columns, k, "", "訳")
			out, _, err := publish.Build(nil, []byte(current), nil)
			if err != nil {
				t.Fatalf("%q: publish.Build: %v", current, err)
			}
			losses, err := publish.CheckLoss("zz", []byte(current), out)
			if err != nil {
				t.Fatalf("%q: publish.CheckLoss: %v", current, err)
			}
			f := Parse([]byte(current))
			l, _ := f.Line(3)
			if l.Editable {
				passed++
				if len(losses) != 0 {
					t.Errorf("%q: 編集できる行なのに publish が止まる: %+v", current, losses)
				}
				continue
			}
			if l.Cause.ID != reason.EditNoKeyOrSource {
				t.Fatalf("%q: ID 3 = %+v、キーを決められない理由を期待", current, l)
			}
			stopped++
			if len(losses) != 1 || losses[0].Line != 3 || losses[0].Why.ID != reason.PublishRowGone {
				t.Errorf("%q: 失われる訳 = %+v、3行目の1件（%s）を期待", current, losses, reason.PublishRowGone)
			}
			var notEditable *NotEditableError
			if err := f.SetTranslation(3, ""); !errors.As(err, &notEditable) {
				t.Errorf("%q: 画面から訳を空にできてしまう: %v", current, err)
			}
		}
	}
	if stopped == 0 || passed == 0 {
		t.Errorf("見本が偏っている: 止まる %d、止まらない %d", stopped, passed)
	}

	// 作業コピーが入力のとき。書き出し先の公開ファイルには、キーのある行だけがある。
	published := headerPublished + "fedcba9876543210,UI,,,UI,既訳\n"
	columns := strings.Split(strings.TrimSuffix(headerWorking, "\n"), ",")
	for _, k := range []string{"", "hello"} {
		working := headerWorking + sampleRecord(columns, "fedcba9876543210", "", "既訳") +
			sampleRecord(columns, k, "", "訳")
		if l, _ := Parse([]byte(working)).Line(3); l.Editable || l.Cause.ID != reason.EditNoKeyOrSource {
			t.Fatalf("%q: ID 3 = %+v、キーを決められない理由を期待", working, l)
		}
		out, stats, err := publish.Build(nil, []byte(working), nil)
		if err != nil {
			t.Fatalf("%q: publish.Build: %v", working, err)
		}
		losses, err := publish.CheckLoss("zz", []byte(published), out)
		if err != nil {
			t.Fatalf("%q: publish.CheckLoss: %v", working, err)
		}
		if stats.Dropped != 1 || len(losses) != 0 {
			t.Errorf("%q: 捨てた行 %d、失われる訳 %+v。捨てて止まらないことを期待", working, stats.Dropped, losses)
		}
	}
}

// csvField は、値 v を CSV のフィールドに書く形にする。前後の空白、カンマ、二重引用符が
// あれば引用する（引用しない前後の空白は、ゲームの読み方と割れる別の理由に当たる）。
func csvField(v string) string {
	if v != strings.TrimSpace(v) || strings.ContainsAny(v, ",\"") {
		return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
	}
	return v
}

// TestEditReasonOrderForKeylessRow は、publish がキーを決められないレコードがほかの理由にも
// 当たるとき、直す先を指す順（列の数、飲み込み、ゲームの読み方との食い違い、publish が
// キーを決められない、訳の改行）で理由を1つだけ付けることを見る（[File.judge] の注記）。
func TestEditReasonOrderForKeylessRow(t *testing.T) {
	const working = headerWorking
	tests := []struct {
		name, data, want string
		args             []string
	}{
		{
			// 列の数が合わない行は、key 列や原文の列に見えている値が、その列の値とは限らない。
			name: "列の数が合わない",
			data: "key,translation\n,訳,余り\n",
			want: reason.EditFieldCount, args: []string{"header", "2", "row", "3"},
		},
		{
			// 訳の開き引用符が閉じず、次のキーの形の行を飲み込む。先に引用符を直す。
			name: "飲み込みの疑い",
			data: working + ",UI,,,UI,,\"訳\n" + key.For("two") + ",UI,,,UI,two,に\"\n",
			want: reason.EditSwallow, args: []string{"line", "3"},
		},
		{
			// 値の途中の '"' はゲームの読み方と割れる形だが、食い違いの判定は key 列か
			// 原文のあるレコードだけを見るので、key 列も原文も空なら同時には当たらない。
			name: "key 列も原文も空でゲームの読み方と割れる形",
			data: working + ",UI,,,UI,,い\"ろ\"は\n",
			want: reason.EditNoKeyOrSource,
		},
		{
			// key 列に値があれば食い違いの判定に入るので、同時に当たる。食い違いは
			// ファイルの形を直す理由なので先に付ける。
			name: "key 列が16桁のキーでなくゲームの読み方と割れる形",
			data: headerPair + "hello,い\"ろ\"は\n",
			want: reason.EditGameDisagrees, args: []string{"column", "translation"},
		},
		{
			// 訳の改行は改行の入力を足すまで（PR4）の制限で、キーを決められないことは
			// そのあとも残る。
			name: "訳の改行",
			data: working + ",UI,,,UI,,\"い\nち\"\n",
			want: reason.EditNoKeyOrSource,
		},
		{
			name: "key 列が16桁のキーでなく訳の改行",
			data: headerPair + "hello,\"い\nち\"\n",
			want: reason.EditNoKeyOrSource,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Parse([]byte(tt.data))
			l, _ := f.Line(2)
			if l.Editable || l.Cause.ID != tt.want || !slices.Equal(l.Cause.Args, tt.args) {
				t.Errorf("ID 2 = %+v、理由 %s %q を期待", l, tt.want, tt.args)
			}
		})
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
