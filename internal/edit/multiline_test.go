package edit

import (
	"errors"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

/*
ここの試験は、編集モデルが、publish と同じ全体を解釈する読み方の区切り
（csvfile.SplitSegments）のセグメントを1つずつ行にし、複数の物理行にまたがる
レコードを1つの行として扱うことと、閉じない引用符のファイルを編集させないことを
固定する（決まったことの 2・3。design の phases[3]）。

全体を解釈する読み手へ移す作業（docs/port-spec.md）の PR2 では、保存が物理行の
単位だったので、行をまたぐレコードのどの物理行も編集させず、続きの行を生の行として
1行ずつ並べていた。PR3 で、次のように期待値を直した。

  - 行をまたぐレコードは、途中の物理行も含めて1つの行になる。ID はセグメントの
    通し番号で、行番号は最初と最後の物理行（Number と EndNumber）。
  - 原文が行をまたぐ訳の空いたレコード: 編集できない → 訳が1行に収まるので書ける。
  - 訳が行をまたぐレコード: 行をまたぐレコードの理由 → 訳に改行がある理由
    （訳への改行の入力は PR4 で足す）。
  - 閉じない引用符: 変わらない（ファイル全体を読み取り専用にし、引用符が開いた行から
    後ろを生の行のまま並べる）。

見本の英文と訳はどれも架空の文である。
*/

// mlWorking は、実物と同じく CRLF で区切った作業コピー。訳が行をまたぐレコード
// （2〜3行目）と、原文が空行を挟んで行をまたぐ訳の空のレコード（4〜6行目）と、
// 訳の中に '#' で始まる行があるレコード（8〜9行目）を持つ。
var mlWorking = "key,section,node,order,speaker,source_en,translation\r\n" +
	key.For("one") + ",UI,,,UI,one,\"いち\nに\"\r\n" +
	key.For("para1\n\npara2") + ",UI,,,UI,\"para1\n\npara2\",\r\n" +
	key.For("two") + ",UI,,,UI,two,さん\r\n" +
	key.For("three") + ",UI,,,UI,three,\"よん\n# ご\"\r\n"

// TestParseMultilineRecords は、行をまたぐレコードを1つの行にし、訳に改行があるときは
// 理由を付けて編集させないことを固定する。原文が行をまたぐレコードは、訳が1行に
// 収まるかぎり書け、変わるのはそのレコードの最終フィールドだけである。
func TestParseMultilineRecords(t *testing.T) {
	f := Parse([]byte(mlWorking))
	if f.ReadOnly() {
		t.Fatalf("ファイル全体が読み取り専用になっている: %s", f.ReadOnlyReason())
	}

	type want struct {
		id, number, end int
		kind            Kind
		editable        bool
		key             string
		source          string
		translation     string
		cause           string
	}
	wants := []want{
		{id: 1, number: 1, end: 1, kind: KindHeader},
		{id: 2, number: 2, end: 3, kind: KindData, key: key.For("one"), source: "one", cause: reason.EditMultilineTranslation},
		{id: 3, number: 4, end: 6, kind: KindData, editable: true, key: key.For("para1\n\npara2"), source: "para1\n\npara2"},
		{id: 4, number: 7, end: 7, kind: KindData, editable: true, key: key.For("two"), source: "two", translation: "さん"},
		{id: 5, number: 8, end: 9, kind: KindData, key: key.For("three"), source: "three", cause: reason.EditMultilineTranslation},
	}
	lines := f.Lines()
	if len(lines) != len(wants) {
		t.Fatalf("セグメントごとに %d 行のはずが %d 行", len(wants), len(lines))
	}
	for i, w := range wants {
		l := lines[i]
		source := ""
		if l.Kind == KindData && len(l.Fields) == 7 {
			source = l.Fields[5]
		}
		got := want{l.ID, l.Number, l.EndNumber, l.Kind, l.Editable, l.Key(), source, l.Translation(), l.Cause.ID}
		if got != w {
			t.Errorf("%d番目 = %+v\n       want %+v", i, got, w)
		}
	}
	if f.PhysicalLines() != 9 {
		t.Errorf("物理行の数 = %d、9 を期待", f.PhysicalLines())
	}

	// 訳に改行があるレコードには書かせない。
	for _, id := range []int{2, 5} {
		var notEditable *NotEditableError
		if err := f.SetTranslation(id, "訳"); !errors.As(err, &notEditable) ||
			notEditable.Cause.ID != reason.EditMultilineTranslation {
			t.Errorf("ID %d に書けてしまう、または理由が違う: %v", id, err)
		}
	}
	// 原文が行をまたぐレコードと、1物理行に収まるレコードには書ける。触っていない
	// レコードは1バイトも変えず、レコードの終端（CRLF）も残す。
	if err := f.SetTranslation(3, "段落"); err != nil {
		t.Fatalf("ID 3 に書けない: %v", err)
	}
	if err := f.SetTranslation(4, "さんさん"); err != nil {
		t.Fatalf("ID 4 に書けない: %v", err)
	}
	written := strings.Replace(mlWorking, "para2\",\r\n", "para2\",段落\r\n", 1)
	written = strings.Replace(written, ",two,さん\r\n", ",two,さんさん\r\n", 1)
	if string(f.Bytes()) != written {
		t.Errorf("書いた結果が違う\n got %q\nwant %q", f.Bytes(), written)
	}
	// 読み直しても同じ ID の同じレコードになる。
	again := Parse(f.Bytes())
	if l, ok := again.Line(3); !ok || l.Number != 4 || l.EndNumber != 6 || l.Translation() != "段落" {
		t.Errorf("読み直した ID 3 = %+v", l)
	}
	// 訳を空に戻すと、元のバイト列に戻る。
	for _, id := range []int{3, 4} {
		orig, _ := Parse([]byte(mlWorking)).Line(id)
		if err := f.SetTranslation(id, orig.Translation()); err != nil {
			t.Fatalf("ID %d を戻せない: %v", id, err)
		}
	}
	if string(f.Bytes()) != mlWorking {
		t.Errorf("元に戻らない\n got %q\nwant %q", f.Bytes(), mlWorking)
	}
}

// TestParseUnclosedQuote は、閉じない引用符のあるファイルを全体で読み取り専用にし、
// 引用符が開いたレコードから後ろを生の行のまま並べることを固定する（決まったことの 3）。
//
// 全体を解釈して読むと、引用符が開いた行からファイルの終わりまでが1つの値になる。
// そこを訳として書くと、publish は（形の確かめで止めなければ）後ろの行を丸ごと訳として
// 公開する。
func TestParseUnclosedQuote(t *testing.T) {
	type line struct {
		kind Kind
		key  string
	}
	tests := []struct {
		name   string
		data   string
		opened string
		// header は受理したヘッダー。ヘッダーの中で引用符が開けば nil。
		header []string
		lines  []line
	}{
		{
			// 引用符が開いた行より前のレコードは読めるが、ファイル全体を編集させない。
			name:   "データの途中で開く",
			data:   "key,translation\n" + key.For("zero") + ",ok\n" + key.For("one") + ",\"訳\n" + key.For("two") + ",に\n\n# 見出し\n",
			opened: "3",
			header: []string{"key", "translation"},
			lines: []line{
				{KindHeader, ""}, {KindData, key.For("zero")},
				// 3行目から後ろは生の行。'#' の行も見出しにしない（値の中の行である）。
				{KindData, ""}, {KindData, ""}, {KindBlank, ""}, {KindData, ""},
			},
		},
		{
			// ヘッダーで開いたときも、受理されないヘッダーより引用符のほうを言う。
			name:   "ヘッダーで開く",
			data:   "key,\"translation\n" + key.For("one") + ",いち\n",
			opened: "1",
			lines:  []line{{KindData, ""}, {KindData, ""}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Parse([]byte(tt.data))
			if !f.ReadOnly() || f.ReadOnlyCause().ID != reason.EditUnclosedQuote || argOf(f.ReadOnlyCause(), "line") != tt.opened {
				t.Fatalf("読み取り専用の理由 = %v %+v、%s行目の閉じない引用符を期待", f.ReadOnly(), f.ReadOnlyCause(), tt.opened)
			}
			if got := f.Header(); strings.Join(got, ",") != strings.Join(tt.header, ",") {
				t.Errorf("ヘッダー = %v、%v を期待", got, tt.header)
			}
			lines := f.Lines()
			if len(lines) != len(tt.lines) {
				t.Fatalf("物理行ごとに %d 行のはずが %d 行", len(tt.lines), len(lines))
			}
			for i, w := range tt.lines {
				l := lines[i]
				if l.Kind != w.kind || l.Key() != w.key || l.Editable {
					t.Errorf("%d行目 = {%v %q %v}、{%v %q false} を期待", l.Number, l.Kind, l.Key(), l.Editable, w.kind, w.key)
				}
				if l.Kind == KindData && l.Cause.ID != reason.EditUnclosedQuote {
					t.Errorf("%d行目の理由 = %s、閉じない引用符を期待", l.Number, l.Cause.ID)
				}
			}
			for i, l := range lines {
				if l.ID != lines[0].ID+i || l.Number != l.EndNumber {
					t.Errorf("%d番目の ID と行番号 = %d・%d〜%d", i, l.ID, l.Number, l.EndNumber)
				}
				if err := f.SetTranslation(l.ID, "訳"); !errors.Is(err, ErrReadOnly) {
					t.Errorf("%d行目に書けてしまう、または読み取り専用の誤りでない: %v", l.Number, err)
				}
			}
			if string(f.Bytes()) != tt.data {
				t.Error("読み取り専用のファイルのバイトが変わっている")
			}
		})
	}
}

// TestParseChoosesTheHeaderLikePublish は、どの行をヘッダーにするかを publish と同じ
// 区切りの関数で決めることを固定する（決まったことのそのほか 8）。受理はいままでどおり
// 生テキストの完全一致で見る（[matchHeader]）。
//
// 物理行を1行ずつ見ていたときは、"," の行を空行相当として飛ばし、全角空白だけの行を
// ヘッダーにしていた。publish（と上流）は "," の行をヘッダーにし（ヘッダーに key 列が
// 無いので形の確かめ (a) で止まる）、全角空白だけの行を空行として落とす。
func TestParseChoosesTheHeaderLikePublish(t *testing.T) {
	const body = "key,translation\n" + "0123456789abcdef,v\n"
	tests := []struct {
		name     string
		data     string
		readOnly string
	}{
		{"ヘッダーの前の \",\" の行はヘッダーになる", ",\n" + body, reason.EditBadHeader},
		{"ヘッダーの前の全角空白だけの行は空行", "　\n" + body, ""},
		{"ヘッダーの前の NO-BREAK SPACE だけの行は空行", " \n" + body, ""},
		{"ヘッダーの後ろの \",\" の行は空行相当", body + ",\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Parse([]byte(tt.data))
			if got := f.ReadOnlyCause().ID; got != tt.readOnly {
				t.Fatalf("読み取り専用の理由 = %q、%q を期待（%s）", got, tt.readOnly, f.ReadOnlyReason())
			}
			if tt.readOnly != "" {
				return
			}
			for _, l := range f.Lines() {
				if l.Kind == KindData && (!l.Editable || l.Translation() != "v") {
					t.Errorf("%d行目 = %+v、訳 v の編集できる行を期待", l.Number, l)
				}
			}
		})
	}
}

// argOf は理由の置換から name の値を引く。
func argOf(why reason.Reason, name string) string {
	for i := 0; i+1 < len(why.Args); i += 2 {
		if why.Args[i] == name {
			return why.Args[i+1]
		}
	}
	return ""
}
