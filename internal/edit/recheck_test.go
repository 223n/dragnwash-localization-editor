package edit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

/*
ここの試験は、書く前の事後確認（決まったことのそのほか 4）を見る。

  - 前半: SetTranslation は、差し替えたレコードだけを読み直して確かめ、外れたら
    書き換えずに NotEditableError（reason.EditRecheckFailed）を返す（recheckRecord）。
  - 後半: Save は、書く直前にファイル全体を読み直して確かめ、外れたら1バイトも書かずに、
    外れたところに最も近い書き換えたレコードを指す RecheckError を返す（File.verify）。

どちらも、正しく組み立てたバイト列では外れない。外れるのは、組み立て（編集モデル）に
誤りがあるときである。そのため、ここではモデルを試験の中で壊して、確かめが誤りを
見つけることを見る。見本の英文と訳はどれも架空の文である。
*/

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
	}{
		{"1物理行のレコード", one, prefixOne + "新しい\r\n", "新しい", true},
		{"原文が行をまたぐレコード", two, prefixTwo + "段落\r\n", "段落", true},
		{"引用が要る訳", one, prefixOne + "\"a,b\"\r\n", "a,b", true},
		{"後ろにレコードが増える", one, prefixOne + "x\r\nk,y\r\n", "x", false},
		{"引用符が閉じない", one, prefixOne + "\"x\r\n", "\"x", false},
		{"終端が変わる", one, prefixOne + "x\n", "x", false},
		{"物理行の数が変わる", two, prefixTwo + "\"x\ny\"\r\n", "x\ny", false},
		{"区切りの数が変わる", one, prefixOne + "x,y\r\n", "x,y", false},
		{"訳より前の値が変わる", one, "k,UI,,,UI,one,x\r\n", "x", false},
		{"訳が value と違う", one, prefixOne + "x\r\n", "y", false},
		{"ゲームの読み方と割れる", one, prefixOne + "a\"b\"c\r\n", "a\"b\"c", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields, ok := recheckRecord(tt.orig, tt.text, tt.value)
			if ok != tt.ok {
				t.Fatalf("recheckRecord = %v、%v を期待", ok, tt.ok)
			}
			if ok && fields[len(fields)-1] != tt.value {
				t.Errorf("読み直した訳 = %q", fields[len(fields)-1])
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
		// breakModel は、ID 2（添字 1）を書き換えたあとのモデルを壊す。
		breakModel func(f *File)
		// blame は誤りが指すレコードの ID。
		blame int
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
			// 触っていない後ろの行が1バイト変わる形。指すのは、その前で最も近い書き換え。
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, recheckWorking)
			f, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.SetTranslation(2, "新しい"); err != nil {
				t.Fatal(err)
			}
			tt.breakModel(f)

			err = f.Save()
			var recheck *RecheckError
			if !errors.As(err, &recheck) || !errors.Is(err, ErrRecheck) {
				t.Fatalf("Save = %v、*RecheckError を期待", err)
			}
			if recheck.ID != tt.blame || recheck.Cause.ID != reason.EditRecheckFailed || recheck.Line != 2 {
				t.Errorf("誤り = %+v、ID %d を期待", recheck, tt.blame)
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
