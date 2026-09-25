package edit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
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

どちらも、正しく組み立てたバイト列では外れない。外れるのは、組み立て（編集モデル）に
誤りがあるときである。そのため、ここではモデルを試験の中で壊して、確かめが誤りを
見つけることを見る。ただし後半のうち、ゲームの読み方で読んだ値が保存の前後で変わらない
ことの確かめ（File.gameChange）は、正しく組み立てても外れることがあるので、壊さずに
見る。見本の英文と訳はどれも架空の文である。
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

// gameShiftWorking は、キーも原文も空のレコード（ID 3）の訳を書き換えると、ゲームの
// 読み方（CsvReader）で後ろのレコード（ID 4）が見つからなくなる2列の作業コピー。
//
// ID 2 の訳の途中の '"' からゲームは引用を始め、ID 3 の訳の '"' で閉じる。ID 3 の訳を
// '"' の無い値にすると、引用が閉じずにファイルの終わりまで続き、ID 4 を飲み込む。
// ID 2 はゲームの読み方と割れるので編集できないが、ID 3 はゲームが引かない（鍵が無い）
// ので食い違いの判定に入らず、編集できる。
const gameShiftWorking = "key,translation\r\n" +
	"aaaaaaaaaaaaaaaa,x\"y\r\n" +
	",p\"q\r\n" +
	"bbbbbbbbbbbbbbbb,ok\r\n"

// TestSaveRefusesWhatChangesHowTheGameReadsOtherRecords は、書き換えたレコードのほかで、
// ゲームの読み方の値が変わる保存を、書かずに断ることを見る（PR3 の検証の指摘）。
//
// publish の読み方ではどのレコードも変わらないので、ほかの確かめでは見つからない。
// 書くと、ホットリロードのあと、ゲームは ID 4 の訳を出さなくなる。
func TestSaveRefusesWhatChangesHowTheGameReadsOtherRecords(t *testing.T) {
	path := writeTemp(t, gameShiftWorking)
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if line, _ := f.Line(3); !line.Editable || line.Key() != "" {
		t.Fatalf("前提が崩れている。ID 3 はキーの空いた編集できる行のはず: %+v", line)
	}
	if line, _ := f.Line(4); !line.Editable {
		t.Fatalf("前提が崩れている。ID 4 は編集できる行のはず: %+v", line)
	}
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
func TestSaveBlamesTheRecordThatFailsAlone(t *testing.T) {
	tests := []struct {
		name string
		body string
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
			edits: []recheckEdit{{3, "訳"}, {4, "架空の訳"}},
			blame: 3, alone: 4,
			want: strings.Replace(gameShiftWorking, "bbbbbbbbbbbbbbbb,ok", "bbbbbbbbbbbbbbbb,架空の訳", 1),
		},
		{
			name:  "書き換える順が逆",
			body:  gameShiftWorking,
			edits: []recheckEdit{{4, "架空の訳"}, {3, "訳"}},
			blame: 3, alone: 4,
			want: strings.Replace(gameShiftWorking, "bbbbbbbbbbbbbbbb,ok", "bbbbbbbbbbbbbbbb,架空の訳", 1),
		},
		{
			// 前の ID 2 は原因でなく、後ろの ID 4（キーの空いた行）の訳を消すと、ゲームの
			// 引用が閉じなくなる。訳を消した ID 4 は空行相当に変わるので、ID 2 だけを
			// 確かめ直すとき、ID 4 は書き換える前の種類（データ行）で見る。外れたところ
			// （ゲームの読み方の値が変わる ID 3）から決めると、その前の ID 2 を指していた。
			name: "後ろのレコードの訳を消して外れる",
			body: "key,translation\r\n" +
				"cccccccccccccccc,old\r\n" +
				"aaaaaaaaaaaaaaaa,x\"y\r\n" +
				",p\"q\r\n" +
				"bbbbbbbbbbbbbbbb,ok\r\n",
			edits: []recheckEdit{{2, "架空の訳"}, {4, ""}},
			blame: 4, alone: 2,
			want: "key,translation\r\n" +
				"cccccccccccccccc,架空の訳\r\n" +
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
