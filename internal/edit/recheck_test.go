package edit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

/*
ここの試験は、書く前の事後確認（決まったことのそのほか 4）を見る。

  - 前半: SetTranslation は、差し替えたレコードだけを読み直して確かめ、外れたら
    書き換えずに NotEditableError（reason.EditRecheckFailed）を返す（recheckRecord）。
  - 後半: Save は、書く直前にファイル全体を読み直して確かめ、外れたら1バイトも書かずに、
    原因の書き換えたレコードを指す RecheckError を返す（File.verify と File.culprit）。

前半と、後半のうち組み立ての確かめ（セグメント・ID・バイト列・値）は、正しく組み立てた
バイト列では外れない。外れるのは、組み立て（編集モデル）に誤りがあるときである。
そのため、ここではモデルを試験の中で壊して、確かめが誤りを見つけることを見る。

後半のうち形の確かめ（飲み込みの疑いとゲームの読み方との食い違い）は、正しく組み立てても
外れうる。値が改行で終わる列を持つレコードに、カンマで始まる訳を書くと、続きの物理行を
単独で読んだときにヘッダーと同じ列の数に見える。訳の改行の後ろの行も同じ形になりうる。
この形は、SetTranslation が差し替えたレコードをヘッダーと並べて先に断る
（TestSetTranslationRefusesLinesThatLookLikeRecords）ので、Save の確かめはモデルを差し替えて
見る（TestSaveRechecksTheShapesOfTouchedRecords）。

ゲームの読み方で読んだ値が保存の前後で変わらないことの確かめ（File.gameChange）は、
key 列も原文も空のレコードの訳を書き換えたときに外れていた。いまはそのレコードを編集
させない（決まったことの 24）ので、編集できるレコードだけを書き換えて外れる形は見つけて
いない（乱数で作った架空のファイル 30 万個で探した）。ここでは判定を飛ばしてそのレコードを
書き換え（forceEditable）、判定をすり抜けた形も確かめが捕まえることを見る。

見本の英文と訳はどれも架空の文である。
*/

// forceEditable は、ID id のデータ行を、編集可否の判定を飛ばして編集できる行にする。
// 書く直前の確かめが、判定をすり抜けた書き換えも捕まえることを見るために使う。
func forceEditable(t *testing.T, f *File, id int) {
	t.Helper()
	i, ok := f.indexOf(id)
	if !ok || f.lines[i].Kind != KindData {
		t.Fatalf("ID %d はデータ行ではない", id)
	}
	f.lines[i].Editable = true
	f.lines[i].setReason(reason.Reason{})
}

// recheckWorking は、確かめの試験に使う作業コピー。ID 2 は1物理行、ID 3 は原文が
// 行をまたぐレコード（3〜4行目）、ID 4 は1物理行。
var recheckWorking = "key,section,node,order,speaker,source_en,translation\r\n" +
	key.For("one") + ",UI,,,UI,one,いち\r\n" +
	key.For("two\nlines") + ",UI,,,UI,\"two\nlines\",\r\n" +
	key.For("three") + ",UI,,,UI,three,さん\r\n"

// TestRecheckRecord は、差し替えたレコードの確かめが、書いてよい形だけを通すことを見る。
func TestRecheckRecord(t *testing.T) {
	f := Parse([]byte(recheckWorking))
	one, _ := f.Line(2)
	two, _ := f.Line(3)
	prefixOne := one.body()[:one.last]
	prefixTwo := two.body()[:two.last]

	tests := []struct {
		name  string
		orig  Line
		text  string
		value string
		ok    bool
		// span は、通るときに返るはずの物理行の数から1を引いたもの。0 なら元のレコードと同じ。
		span int
	}{
		{"1物理行のレコード", one, prefixOne + "新しい\r\n", "新しい", true, 0},
		{"原文が行をまたぐレコード", two, prefixTwo + "段落\r\n", "段落", true, 0},
		{"引用が要る訳", one, prefixOne + "\"a,b\"\r\n", "a,b", true, 0},
		{"後ろにレコードが増える", one, prefixOne + "x\r\nk,y\r\n", "x", false, 0},
		{"引用符が閉じない", one, prefixOne + "\"x\r\n", "\"x", false, 0},
		{"終端が変わる", one, prefixOne + "x\n", "x", false, 0},
		// 訳への改行の入力（決まったことの 1）。物理行の数は、訳の改行のぶんだけ変わってよい。
		{"訳の改行で物理行の数が増える", two, prefixTwo + "\"x\ny\"\r\n", "x\ny", true, 2},
		{"訳が改行で終わる", one, prefixOne + "\"x\n\"\r\n", "x\n", true, 1},
		{"改行を引用せずに書くと後ろにレコードが増える", one, prefixOne + "x\ny\r\n", "x\ny", false, 0},
		{"区切りの数が変わる", one, prefixOne + "x,y\r\n", "x,y", false, 0},
		{"訳より前の値が変わる", one, "k,UI,,,UI,one,x\r\n", "x", false, 0},
		{"訳が value と違う", one, prefixOne + "x\r\n", "y", false, 0},
		{"訳の改行が value と違う", one, prefixOne + "\"x\r\ny\"\r\n", "x\ny", false, 0},
		{"ゲームの読み方と割れる", one, prefixOne + "a\"b\"c\r\n", "a\"b\"c", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields, span, ok := recheckRecord(tt.orig, tt.text, tt.value)
			if ok != tt.ok {
				t.Fatalf("recheckRecord = %v、%v を期待", ok, tt.ok)
			}
			if !ok {
				return
			}
			if fields[len(fields)-1] != tt.value {
				t.Errorf("読み直した訳 = %q", fields[len(fields)-1])
			}
			// 物理行の数から1を引いたもの。省いた見本では、元のレコードと同じ。
			want := tt.span
			if want == 0 {
				want = tt.orig.EndNumber - tt.orig.Number
			}
			if span != want {
				t.Errorf("物理行の数 = %d+1、%d+1 を期待", span, want)
			}
		})
	}
}

// TestSetTranslationRefusesWhatDoesNotReadBack は、差し替えたレコードを読み直すと
// 合わないとき、書き換えずに理由を付けて断ることを見る。モデルの最終フィールドの
// 位置を壊して、差し替える範囲を誤らせる。
func TestSetTranslationRefusesWhatDoesNotReadBack(t *testing.T) {
	f := Parse([]byte(recheckWorking))
	f.lines[1].last -= 3 // 最終フィールドの開始位置を、原文の途中へずらす。

	var notEditable *NotEditableError
	err := f.SetTranslation(2, "新しい")
	if !errors.As(err, &notEditable) || notEditable.Cause.ID != reason.EditRecheckFailed ||
		notEditable.ID != 2 || notEditable.Line != 2 || argOf(notEditable.Cause, "line") != "2" {
		t.Fatalf("SetTranslation = %v、2行目の書く前の確かめの理由を期待", err)
	}
	if f.Dirty() || string(f.Bytes()) != recheckWorking {
		t.Error("断ったのにモデルが変わった")
	}
}

// TestSaveRechecksTheWholeFile は、書く直前のファイル全体の確かめが、外れたときに
// 1バイトも書かず、行を特定できる誤りを返すことを見る。
func TestSaveRechecksTheWholeFile(t *testing.T) {
	tests := []struct {
		name string
		// edits は書き換えるレコードの ID。空なら ID 2 だけ。
		edits []int
		// breakModel は、書き換えたあとのモデルを壊す。
		breakModel func(f *File)
		// blame は誤りが指すレコードの ID、line はその最初の物理行（0 なら 2）。
		blame, line int
	}{
		{
			// 書き換えたレコードの終わりの改行を消し、後ろのレコードと1つになる形。
			name: "書き換えたレコードが後ろを飲み込む",
			breakModel: func(f *File) {
				f.lines[1].Text = f.lines[1].Text[:len(f.lines[1].Text)-2]
			},
			blame: 2,
		},
		{
			// 触っていない後ろの行が1バイト変わる形。指すのは、その前で最も近い書き換え
			// （ここでは書き換えが1つだけ）。
			name: "触っていない行が変わる",
			breakModel: func(f *File) {
				f.lines[3].Text = f.lines[3].Text[:len(f.lines[3].Text)-2] + "!\r\n"
			},
			blame: 2,
		},
		{
			// モデルの値だけがずれる形。
			name: "書き換えたレコードの値がモデルと違う",
			breakModel: func(f *File) {
				f.lines[1].Fields[len(f.lines[1].Fields)-1] = "別の訳"
			},
			blame: 2,
		},
		{
			// レコードが1つ消える形。セグメントの数と ID が変わる。
			name: "レコードが消える",
			breakModel: func(f *File) {
				f.lines[3].Text = ""
			},
			blame: 2,
		},
		{
			// 最後に触っていないレコードが1つ増える形。セグメントの数だけが変わる。
			name: "レコードが増える",
			breakModel: func(f *File) {
				f.lines[3].Text += key.For("four") + ",UI,,,UI,four,よん\r\n"
			},
			blame: 2,
		},
		{
			// 書き換えより前（ヘッダー）が変わる形。その前に書き換えが無いので、最初の
			// 書き換えを指す。
			name: "書き換えより前が変わる",
			breakModel: func(f *File) {
				f.lines[0].Text = "#" + f.lines[0].Text
			},
			blame: 2,
		},
		{
			// 書き換えたレコードの物理行の数が変わる形。モデルの値もそろえてあるので、
			// バイト列と値の比べでは見つからない。後ろのレコードの行番号がずれる。
			name: "書き換えたレコードの物理行の数が変わる",
			breakModel: func(f *File) {
				line := &f.lines[1]
				line.Text = line.body()[:line.last] + "\"新\nしい\"" + line.term
				line.Fields[len(line.Fields)-1] = "新\nしい"
			},
			blame: 2,
		},
		{
			// 触っていない後ろの行の行番号だけがモデルでずれる形。バイト列と値は同じ。
			// 訳の改行で行番号をずらすとき（File.shift）の誤りを、書く前に見つける。
			name: "モデルの行番号だけがずれる",
			breakModel: func(f *File) {
				f.lines[3].Number++
				f.lines[3].EndNumber++
			},
			blame: 2,
		},
		{
			// 触っていない行の種類だけがモデルで変わる形。バイト列は同じ。
			name: "種類だけが違う",
			breakModel: func(f *File) {
				f.lines[3].Kind = KindComment
			},
			blame: 2,
		},
		{
			// 2つのレコードを書き換え、後ろ（ID 4）の側で外れる形。1つずつ確かめ直すと
			// ID 4 だけが外れるので、指すのは ID 4 で、最初の書き換えの ID 2 ではない。
			name:  "後ろの書き換えで外れる",
			edits: []int{2, 4},
			breakModel: func(f *File) {
				f.lines[3].Fields[len(f.lines[3].Fields)-1] = "別の訳"
			},
			blame: 4, line: 5,
		},
		{
			// 2つのレコードを書き換え、その後ろの触っていない行が変わる形。どちらも
			// 1つずつ確かめ直すと外れない（触っていない行は読み込んだときのバイト列で
			// 組む）ので、外れたところより前で最も近い書き換えの ID 3 を指す。
			name:  "2つ書き換えて触っていない行が変わる",
			edits: []int{2, 3},
			breakModel: func(f *File) {
				f.lines[3].Text = f.lines[3].Text[:len(f.lines[3].Text)-2] + "!\r\n"
			},
			blame: 3, line: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, recheckWorking)
			f, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			edits, line := tt.edits, tt.line
			if edits == nil {
				edits = []int{2}
			}
			if line == 0 {
				line = 2
			}
			for _, id := range edits {
				if err := f.SetTranslation(id, "新しい"); err != nil {
					t.Fatal(err)
				}
			}
			tt.breakModel(f)

			err = f.Save()
			var recheck *RecheckError
			if !errors.As(err, &recheck) || !errors.Is(err, ErrRecheck) {
				t.Fatalf("Save = %v、*RecheckError を期待", err)
			}
			if recheck.ID != tt.blame || recheck.Cause.ID != reason.EditRecheckFailed || recheck.Line != line {
				t.Errorf("誤り = %+v、ID %d（%d行目）を期待", recheck, tt.blame, line)
			}
			if want := "保存しない: " + recheck.Cause.Text; err.Error() != want {
				t.Errorf("Error() = %q、%q を期待", err.Error(), want)
			}
			if got := readFile(t, path); got != recheckWorking {
				t.Errorf("外れたのに書いた: %q", got)
			}
		})
	}
}

// TestSaveRechecksTheShapesOfTouchedRecords は、書き換えたレコードが、ファイル全体で
// 読むと飲み込みの疑いやゲームの読み方との食い違いに当たるとき、書かないことを見る。
//
// モデルを「書き換えたあとのバイト列を読んだもの」に差し替えて、区切りと値はそろって
// いるが、形の検出に当たる状態を作る。
func TestSaveRechecksTheShapesOfTouchedRecords(t *testing.T) {
	const header = "key,section,node,order,speaker,source_en,translation\n"
	tests := []struct {
		name  string
		after string
	}{
		{
			name: "飲み込みの疑い",
			after: header + key.For("one") + ",UI,,,UI,one,\"いち\n" +
				key.For("two") + ",UI,,,UI,two,に\"\n",
		},
		{
			name:  "ゲームの読み方と割れる",
			after: header + key.For("one") + ",UI,,,UI,one,い\"ち\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := header + key.For("one") + ",UI,,,UI,one,いち\n"
			path := writeTemp(t, before)
			f, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			f.lines = Parse([]byte(tt.after)).lines
			f.touched = map[int]bool{1: true}
			f.dirty = true

			var recheck *RecheckError
			if err := f.Save(); !errors.As(err, &recheck) || recheck.ID != 2 {
				t.Fatalf("Save = %v、ID 2 を指す *RecheckError を期待", err)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != before {
				t.Errorf("外れたのに書いた: %q", got)
			}
		})
	}
}

// swallowOnWrite は、訳を書くと飲み込みの疑いに当たる3列の作業コピー。ID 2 の speaker の
// 値は改行で終わり、続きの物理行（3行目）は閉じ引用符で始まる。そこへカンマで始まる訳を
// 書くと、3行目を単独で読んだとき、閉じ引用符が開き引用符に見え、訳のカンマが区切りに
// なって、ヘッダーと同じ3列に見える（csvfile.FindSwallows の same_columns）。
// ID 3 は1物理行のレコード。
const swallowOnWrite = "key,speaker,translation\r\n" +
	"aaaaaaaaaaaaaaaa,\"Fern\r\n\",\r\n" +
	"bbbbbbbbbbbbbbbb,Kobold,ok\r\n"

// TestSetTranslationRefusesLinesThatLookLikeRecords は、書こうとした訳の行が、その行だけで
// 読むとレコードに見える（飲み込みの疑いに当たる）とき、書き換えずに、訳の何行目かを
// 添えて断ることを見る。publish は同じ関数（csvfile.FindSwallows）でそのレコードを止める。
//
// PR3 では、この形は書く直前のファイル全体の確かめ（Save）で外れ、理由は書く前の事後確認の
// 一般の文（reason.EditRecheckFailed）だった。訳に改行を入れられるようになると、訳の2行目
// 以降がこの形になりうるので、差し替えたレコードをヘッダーと並べて確かめ、直す先の分かる
// 理由で先に断る（File.swallowedLine）。
func TestSetTranslationRefusesLinesThatLookLikeRecords(t *testing.T) {
	tests := []struct {
		name  string
		id    int
		value string
		// line は、レコードに見える行が訳の何行目か。0 なら書ける。
		line int
	}{
		// 原文（speaker）が改行で終わるので、訳の1行目が続きの物理行に入る。
		{"カンマで始まる訳が3列に見える", 2, ",訳,", 1},
		{"カンマで始まらない訳は書ける", 2, "訳,あり", 0},
		// 訳の改行の後ろの行。
		{"改行の後ろの行がキーの形で始まる", 3, "いち\nfedcba9876543210,x", 2},
		{"改行の後ろの行が3列に見える", 3, "いち\nに\na,b,c", 3},
		{"改行の後ろの行が台詞IDで始まる", 3, "いち\nline:0a0b0c01,x", 2},
		{"改行の後ろの行の列が少ない", 3, "いち\nに,さん", 0},
		{"改行の後ろの行が '#' で始まる", 3, "いち\n# a,b,c", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, swallowOnWrite)
			f, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if line, _ := f.Line(tt.id); !line.Editable || line.Key() == "" {
				t.Fatalf("前提が崩れている。ID %d はキーのある編集できる行のはず: %+v", tt.id, line)
			}
			err = f.SetTranslation(tt.id, tt.value)
			if tt.line == 0 {
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Save(); err != nil {
					t.Fatalf("保存に失敗した: %v", err)
				}
				return
			}
			var invalid *InvalidValueError
			if !errors.As(err, &invalid) || invalid.Cause.ID != reason.EditLineLooksLikeRecord ||
				invalid.ID != tt.id || argOf(invalid.Cause, "line") != strconv.Itoa(tt.line) {
				t.Fatalf("SetTranslation = %v、訳の%d行目がレコードに見える理由を期待", err, tt.line)
			}
			if f.Dirty() || string(f.Bytes()) != swallowOnWrite {
				t.Error("断ったのにモデルが変わった")
			}
			if l, _ := f.Line(3); l.Number != 4 || l.EndNumber != 4 || f.PhysicalLines() != 4 {
				t.Errorf("断ったのに行番号が変わった: %d〜%d、物理行 %d", l.Number, l.EndNumber, f.PhysicalLines())
			}
		})
	}
}

// TestSetTranslationCountsColumnsLikeTheHeader は、訳の2行目以降がレコードに見えるかを、
// カンマで区切った列の数がヘッダーの列の数と同じかで決めることを、7列の作業コピー（実物と
// 同じ形）で見る。カンマの数では、ヘッダーの列の数より1つ少ない6個で断り、5個と7個は書く。
// README（ja・en）の「7列の作業コピーならカンマ6個」と同じ数え方である（検証の指摘。以前の
// README は「カンマの数がヘッダーの列と同じ」と書いていた）。
func TestSetTranslationCountsColumnsLikeTheHeader(t *testing.T) {
	for commas, refused := range map[int]bool{5: false, 6: true, 7: false} {
		value := "いち\n" + strings.Repeat("x,", commas) + "x"
		f := Parse([]byte(mlWorking))
		err := f.SetTranslation(4, value)
		var invalid *InvalidValueError
		switch {
		case refused && (!errors.As(err, &invalid) || invalid.Cause.ID != reason.EditLineLooksLikeRecord ||
			argOf(invalid.Cause, "line") != "2"):
			t.Errorf("カンマ %d 個: SetTranslation = %v、訳の2行目がレコードに見える理由を期待", commas, err)
		case !refused && err != nil:
			t.Errorf("カンマ %d 個: 書けない: %v", commas, err)
		}
	}
}

// gameShiftWorking は、キーも原文も空のレコード（ID 3）の訳を書き換えると、ゲームの
// 読み方（CsvReader）で後ろのレコード（ID 4）が見つからなくなる2列の作業コピー。
//
// ID 2 の訳の途中の '"' からゲームは引用を始め、ID 3 の訳の '"' で閉じる。ID 3 の訳を
// '"' の無い値にすると、引用が閉じずにファイルの終わりまで続き、ID 4 を飲み込む。
// ID 2 はゲームの読み方と割れるので編集できない。ID 3 はゲームが引かない（鍵が無い）
// ので食い違いの判定に入らず、PR3 のはじめは編集できた。いまは key 列も原文も空なので
// 編集させない（決まったことの 24）。試験では判定を飛ばして書き換える（forceEditable）。
const gameShiftWorking = "key,translation\r\n" +
	"aaaaaaaaaaaaaaaa,x\"y\r\n" +
	",p\"q\r\n" +
	"bbbbbbbbbbbbbbbb,ok\r\n"

// TestSaveRefusesWhatChangesHowTheGameReadsOtherRecords は、書き換えたレコードのほかで、
// ゲームの読み方の値が変わる保存を、書かずに断ることを見る（PR3 の検証の指摘）。
//
// publish の読み方ではどのレコードも変わらないので、ほかの確かめでは見つからない。
// 書くと、ホットリロードのあと、ゲームは ID 4 の訳を出さなくなる。見つけたときの形
// （key 列も原文も空の ID 3 の書き換え）は、いまは編集させないことで先に断る。この確かめは、
// 判定をすり抜けた書き換えを捕まえる守りとして残す。
func TestSaveRefusesWhatChangesHowTheGameReadsOtherRecords(t *testing.T) {
	path := writeTemp(t, gameShiftWorking)
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var notEditable *NotEditableError
	if err := f.SetTranslation(3, "訳"); !errors.As(err, &notEditable) || notEditable.Cause.ID != reason.EditNoKeyOrSource {
		t.Fatalf("ID 3 は key 列も原文も空の理由で断るはず: %v", err)
	}
	if line, _ := f.Line(4); !line.Editable {
		t.Fatalf("前提が崩れている。ID 4 は編集できる行のはず: %+v", line)
	}
	forceEditable(t, f, 3)
	if err := f.SetTranslation(3, "訳"); err != nil {
		t.Fatalf("書き換えそのものは通るはず: %v", err)
	}

	var recheck *RecheckError
	if err := f.Save(); !errors.As(err, &recheck) || recheck.ID != 3 || recheck.Line != 3 {
		t.Fatalf("Save = %v、ID 3 を指す *RecheckError を期待", err)
	}
	if got := readFile(t, path); got != gameShiftWorking {
		t.Errorf("外れたのに書いた: %q", got)
	}
}

// recheckEdit は、試験で書き換えるレコードの ID と訳。
type recheckEdit struct {
	id    int
	value string
}

// TestSaveBlamesTheRecordThatFailsAlone は、1回の保存で2つ以上のレコードを書き換えて
// 確かめが外れたとき、それだけを書き換えても外れるレコードを指し、指さなかったレコードは
// 単独なら保存できることを見る（PR3 の検証の指摘）。
//
// 画面は、指されたレコードの送り直しを止め、ほかのレコードを次の要求で送り直す。原因で
// ないレコードを指すと、単独なら書ける訳を打ち直すまで保存せず、原因のレコードを送り直す。
//
// 見本の key 列も原文も空のレコードは、いまは編集させない（決まったことの 24）ので、
// 判定を飛ばして書き換える（forceEditable）。
func TestSaveBlamesTheRecordThatFailsAlone(t *testing.T) {
	tests := []struct {
		name string
		body string
		// force は、判定を飛ばして編集できる行にする ID。
		force int
		// edits は書き換えるレコードの ID と訳（書き換える順）。
		edits []recheckEdit
		// blame は指すレコードの ID（最初の物理行も同じ）、alone は単独なら保存できる
		// レコードの ID、want はそれを保存したあとの中身。
		blame, alone int
		want         string
	}{
		{
			// gameShiftWorking の ID 3（キーの空いた行）と、その後ろの ID 4 を一緒に
			// 書き換える。外れるのは ID 4 の食い違い（ゲームが ID 4 を見つけられない）と
			// してで、そこから決めると ID 4 を指していた。
			name:  "後ろのレコードの食い違いとして外れる",
			body:  gameShiftWorking,
			force: 3,
			edits: []recheckEdit{{3, "訳"}, {4, "架空の訳"}},
			blame: 3, alone: 4,
			want: strings.Replace(gameShiftWorking, "bbbbbbbbbbbbbbbb,ok", "bbbbbbbbbbbbbbbb,架空の訳", 1),
		},
		{
			name:  "書き換える順が逆",
			body:  gameShiftWorking,
			force: 3,
			edits: []recheckEdit{{4, "架空の訳"}, {3, "訳"}},
			blame: 3, alone: 4,
			want: strings.Replace(gameShiftWorking, "bbbbbbbbbbbbbbbb,ok", "bbbbbbbbbbbbbbbb,架空の訳", 1),
		},
		{
			// 前の ID 2 は原因でなく、後ろの ID 4（キーの空いた行）の訳を消すと、ID 4 は
			// "," になって読み直すと空行相当になり、ゲームの引用も閉じなくなる。指すのは
			// ID 4 で、ID 2 だけなら保存できる。
			name: "後ろのレコードの訳を消して外れる",
			body: "key,translation\r\n" +
				"cccccccccccccccc,old\r\n" +
				"aaaaaaaaaaaaaaaa,x\"y\r\n" +
				",p\"q\r\n" +
				"bbbbbbbbbbbbbbbb,ok\r\n",
			force: 4,
			edits: []recheckEdit{{2, "架空の訳"}, {4, ""}},
			blame: 4, alone: 2,
			want: "key,translation\r\n" +
				"cccccccccccccccc,架空の訳\r\n" +
				"aaaaaaaaaaaaaaaa,x\"y\r\n" +
				",p\"q\r\n" +
				"bbbbbbbbbbbbbbbb,ok\r\n",
		},
		{
			// 上と同じ形で、後ろの ID 4 の訳に改行を入れて外れる。ID 4 の物理行が1つ増える
			// ので、ID 2 だけを書き換えたバイト列では、ID 5 の行番号がモデル（どちらの書き換えも
			// 入れたもの）と1つずれる。1つずつ確かめ直すときは、入れた書き換えのぶんだけで
			// 行番号を見積もる（File.numbers）。モデルの行番号と比べると、原因でない ID 2 を
			// 指してしまう。
			name: "後ろのレコードの訳に改行を入れて外れる",
			body: "key,translation\r\n" +
				"cccccccccccccccc,old\r\n" +
				"aaaaaaaaaaaaaaaa,x\"y\r\n" +
				",p\"q\r\n" +
				"bbbbbbbbbbbbbbbb,ok\r\n",
			force: 4,
			edits: []recheckEdit{{2, "架空の訳"}, {4, "訳\nです"}},
			blame: 4, alone: 2,
			want: "key,translation\r\n" +
				"cccccccccccccccc,架空の訳\r\n" +
				"aaaaaaaaaaaaaaaa,x\"y\r\n" +
				",p\"q\r\n" +
				"bbbbbbbbbbbbbbbb,ok\r\n",
		},
		{
			// 前の ID 2 の訳に改行を入れ（原因ではない）、後ろの ID 4 で外れる。ID 4 だけを
			// 書き換えたバイト列では、ID 3 から後ろの行番号がモデルと1つずれる。
			name: "前のレコードの訳に改行を入れ、後ろのレコードで外れる",
			body: "key,translation\r\n" +
				"cccccccccccccccc,old\r\n" +
				"aaaaaaaaaaaaaaaa,x\"y\r\n" +
				",p\"q\r\n" +
				"bbbbbbbbbbbbbbbb,ok\r\n",
			force: 4,
			edits: []recheckEdit{{2, "架空の\n訳"}, {4, "訳"}},
			blame: 4, alone: 2,
			want: "key,translation\r\n" +
				"cccccccccccccccc,\"架空の\n訳\"\r\n" +
				"aaaaaaaaaaaaaaaa,x\"y\r\n" +
				",p\"q\r\n" +
				"bbbbbbbbbbbbbbbb,ok\r\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, tt.body)
			f, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			forceEditable(t, f, tt.force)
			var value string
			for _, e := range tt.edits {
				if err := f.SetTranslation(e.id, e.value); err != nil {
					t.Fatalf("ID %d の書き換えそのものは通るはず: %v", e.id, err)
				}
				if e.id == tt.alone {
					value = e.value
				}
			}

			var recheck *RecheckError
			if err := f.Save(); !errors.As(err, &recheck) || recheck.ID != tt.blame || recheck.Line != tt.blame {
				t.Fatalf("Save = %v、ID %d を指す *RecheckError を期待", err, tt.blame)
			}
			if got := readFile(t, path); got != tt.body {
				t.Fatalf("外れたのに書いた: %q", got)
			}

			again, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := again.SetTranslation(tt.alone, value); err != nil {
				t.Fatal(err)
			}
			if err := again.Save(); err != nil {
				t.Fatalf("指さなかった ID %d が単独でも保存できない: %v", tt.alone, err)
			}
			if got := readFile(t, path); got != tt.want {
				t.Errorf("保存後の中身 = %q", got)
			}
		})
	}
}

// TestGameChangePointsAtTheFirstChangedRecord は、ゲームの読み方の値が変わったところの
// うち、ファイルの前にあるものを返すことと、publish の読み方に無い鍵の行がゲームにだけ
// 増えたときはファイルの終わりを返すことを見る。
func TestGameChangePointsAtTheFirstChangedRecord(t *testing.T) {
	t.Run("前にあるほう", func(t *testing.T) {
		f := Parse([]byte(gameShiftWorking))
		forceEditable(t, f, 3)
		if err := f.SetTranslation(3, "訳"); err != nil {
			t.Fatal(err)
		}
		out := f.Bytes()
		// ID 2（添字 1）はゲームの読み方の値が変わり、ID 4（添字 3）は見つからなくなる。
		if got := f.gameChange(csvfile.ReadPowerShellMarked(out), out, f.touched); got != 1 {
			t.Errorf("gameChange = %d、1 を期待", got)
		}
	})
	t.Run("ゲームから消えた鍵", func(t *testing.T) {
		// 書いたあとのバイト列から、鍵のある行が1つ消える形。消えた鍵のレコードは
		// 書いたあとの publish の読み方にも無いので、ファイルの終わりを返す。
		const orig = "key,translation\n" +
			"aaaaaaaaaaaaaaaa,a\n" +
			"bbbbbbbbbbbbbbbb,b\n"
		f := Parse([]byte(orig))
		out := []byte("key,translation\naaaaaaaaaaaaaaaa,a\n")
		if got := f.gameChange(csvfile.ReadPowerShellMarked(out), out, f.touched); got != len(f.lines) {
			t.Errorf("gameChange = %d、ファイルの終わり（%d）を期待", got, len(f.lines))
		}
	})
	t.Run("ゲームにだけある鍵", func(t *testing.T) {
		const orig = "key,section,node,order,speaker,source_en,translation\n" +
			"0123456789abcdef,UI,,,UI,one,いち\n"
		f := Parse([]byte(orig))
		// 引用符で囲まない値の先頭の空白は、publish の読み方では削られ、ゲームの読み方
		// では残る。ゲームの鍵（source_en の値）は publish の読み方のどのレコードにも無い。
		out := []byte(orig + ",UI,,,UI, two,に\n")
		if got := f.gameChange(csvfile.ReadPowerShellMarked(out), out, f.touched); got != len(f.lines) {
			t.Errorf("gameChange = %d、ファイルの終わり（%d）を期待", got, len(f.lines))
		}
	})
	t.Run("変わらない", func(t *testing.T) {
		// ゲームの読み方と割れるレコード（ID 2。値の先頭の空白）があっても、ほかの
		// レコードの訳を書き換えるだけなら、ゲームの読み方の値は変わらない。
		const orig = "key,translation\n" +
			"aaaaaaaaaaaaaaaa, x\n" +
			"bbbbbbbbbbbbbbbb,ok\n"
		path := writeTemp(t, orig)
		f, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if line, _ := f.Line(2); line.Editable {
			t.Fatalf("前提が崩れている。ID 2 はゲームの読み方と割れるはず: %+v", line)
		}
		if err := f.SetTranslation(3, "よし"); err != nil {
			t.Fatal(err)
		}
		if err := f.Save(); err != nil {
			t.Fatalf("保存に失敗した: %v", err)
		}
		if got := readFile(t, path); got != "key,translation\naaaaaaaaaaaaaaaa, x\nbbbbbbbbbbbbbbbb,よし\n" {
			t.Errorf("保存後の中身 = %q", got)
		}
	})
}

// TestRecheckReasonMatchesTheCatalog は、書く前の事後確認の理由の文面が、画面の目録
// （ja）の文面と同じことを見る。
//
// ほかの理由は、画面の試験（internal/web の TestJapaneseCatalogMatchesTheSourceText）が
// 実際の判定から見本を取って突き合わせる。この理由は正しく組み立てたファイルでは
// 立たないので、あちらでは見本を取れない。ここで目録のファイルを直接読んで比べる。
func TestRecheckReasonMatchesTheCatalog(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "web", "ui", "i18n", "ja.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Messages map[string]string `json:"messages"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	why := recheckReason(12)
	text, ok := catalog.Messages["reason."+why.ID]
	if !ok {
		t.Fatalf("目録に reason.%s が無い", why.ID)
	}
	if !slices.Equal(why.Args, []string{"line", "12"}) {
		t.Errorf("置換 = %q", why.Args)
	}
	if got := strings.ReplaceAll(text, "{line}", "12"); got != why.Text {
		t.Errorf("目録は %q、元の文面は %q", got, why.Text)
	}
}

// TestSaveAgainOnTheSameFile は、同じ File で別々のレコードを続けて保存できることを見る。
//
// 書く直前の確かめは、触っていない行を「読み込んだとき（または最後に保存したとき）の
// バイト列」と比べる。保存したあとにその基準を新しくしないと、2回目の保存で、1回目に
// 書いた行を「触っていない行が変わった」として断る。画面の待ち受けは要求のたびに
// ファイルを開き直すので表には出ないが、このパッケージを直に使う側では起きる。
func TestSaveAgainOnTheSameFile(t *testing.T) {
	path := writeTemp(t, recheckWorking)
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{2, 4, 3} {
		if err := f.SetTranslation(id, "訳"+string(rune('0'+id))); err != nil {
			t.Fatalf("ID %d: %v", id, err)
		}
		if err := f.Save(); err != nil {
			t.Fatalf("ID %d の保存に失敗した: %v", id, err)
		}
	}
	want := "key,section,node,order,speaker,source_en,translation\r\n" +
		key.For("one") + ",UI,,,UI,one,訳2\r\n" +
		key.For("two\nlines") + ",UI,,,UI,\"two\nlines\",訳3\r\n" +
		key.For("three") + ",UI,,,UI,three,訳4\r\n"
	if got := readFile(t, path); got != want {
		t.Errorf("保存後の中身 = %q", got)
	}
}

// TestSaveAgainAfterLineBreaks は、同じ File で、訳に改行を足して保存したあと、後ろの
// レコードを書いて保存し、さらに改行を減らして保存できることを見る（Save の「続けてもう
// 一度呼んでよい」）。
//
// 書く直前の確かめは、触っていないレコードが「読み込んだとき（または最後に保存したとき）」の
// 物理行の数を占めるとして、行番号を見積もる（[File.numbers] の origSpan）。保存のあとに
// その数を覚え直さないと、2回目の保存で、1回目に改行を足したレコードを1物理行と見積もり、
// 後ろのレコードの行番号が1つ合わず、RecheckError で1バイトも書けなくなる（検証の指摘。
// 画面の待ち受けは要求のたびにファイルを開き直すので表には出ない）。
func TestSaveAgainAfterLineBreaks(t *testing.T) {
	path := writeTemp(t, recheckWorking)
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	one := key.For("one") + ",UI,,,UI,one,"
	three := key.For("three") + ",UI,,,UI,three,"
	steps := []struct {
		name  string
		id    int
		value string
		// from と to は、その保存でファイルの中で置き換わるもの。
		from, to string
		// numbers は、保存のあとの ID ごとの最初と最後の物理行。
		numbers [][3]int
	}{
		{"ID 2 に改行を足す", 2, "い\nち", one + "いち\r\n", one + "\"い\nち\"\r\n",
			[][3]int{{1, 1, 1}, {2, 2, 3}, {3, 4, 5}, {4, 6, 6}}},
		{"後ろの ID 4 を書く", 4, "さん。", three + "さん\r\n", three + "さん。\r\n",
			[][3]int{{1, 1, 1}, {2, 2, 3}, {3, 4, 5}, {4, 6, 6}}},
		{"ID 2 の改行を減らす", 2, "いち。", one + "\"い\nち\"\r\n", one + "いち。\r\n",
			[][3]int{{1, 1, 1}, {2, 2, 2}, {3, 3, 4}, {4, 5, 5}}},
	}
	want := recheckWorking
	for _, step := range steps {
		if err := f.SetTranslation(step.id, step.value); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if err := f.Save(); err != nil {
			t.Fatalf("%s: 保存に失敗した: %v", step.name, err)
		}
		want = strings.Replace(want, step.from, step.to, 1)
		if got := readFile(t, path); got != want {
			t.Fatalf("%s: 保存した中身 = %q、%q を期待", step.name, got, want)
		}
		again, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := lineNumbers(f); !slices.Equal(got, step.numbers) || !slices.Equal(lineNumbers(again), step.numbers) {
			t.Errorf("%s: 行番号 = %v、読み直すと %v、%v を期待", step.name, got, lineNumbers(again), step.numbers)
		}
		if f.PhysicalLines() != again.PhysicalLines() {
			t.Errorf("%s: 物理行の数 = %d、読み直すと %d", step.name, f.PhysicalLines(), again.PhysicalLines())
		}
	}
}

// TestSaveKeepsIDsAcrossSaves は、保存の前後で ID が変わらないことを、実物と同じ形の
// 作業コピーで見る。書いたあとに読み直しても、どの ID も同じレコード（同じ行番号の
// 範囲）を指す。
func TestSaveKeepsIDsAcrossSaves(t *testing.T) {
	path := writeTemp(t, recheckWorking)
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	before := f.Lines()
	for _, id := range []int{2, 3, 4} {
		if err := f.SetTranslation(id, "訳"+string(rune('0'+id))); err != nil {
			t.Fatalf("ID %d: %v", id, err)
		}
	}
	if err := f.Save(); err != nil {
		t.Fatalf("保存に失敗した: %v", err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	after := again.Lines()
	if len(after) != len(before) {
		t.Fatalf("行が %d から %d に変わった", len(before), len(after))
	}
	for i := range before {
		if before[i].ID != after[i].ID || before[i].Number != after[i].Number || before[i].EndNumber != after[i].EndNumber {
			t.Errorf("%d番目: 前 {%d %d〜%d} 後 {%d %d〜%d}", i,
				before[i].ID, before[i].Number, before[i].EndNumber, after[i].ID, after[i].Number, after[i].EndNumber)
		}
	}
	if f.touched != nil {
		t.Error("保存したのに書き換えた印が残っている")
	}
}
