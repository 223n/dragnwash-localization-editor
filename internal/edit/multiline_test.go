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
（csvfile.SplitSegments）で行の種類を決め、複数の物理行にまたがるレコードと閉じない
引用符のファイルを編集させないことを固定する（決まったことの 3。design の phases[2]）。

全体を解釈する読み手へ移す作業（docs/port-spec.md）の PR2 の最初のコミットでは、
同じ入力で、物理行を1行ずつ見ていたときの結果を固定していた（名前の末尾が
LineBasedNow の試験。決まったことの 13）。後半のコミットで、次のように期待値を直した。

  - 訳が行をまたぐレコードの1行目: 編集できる行で訳は「いち」 → 編集できない
    （理由は行をまたぐレコード）。書くと続きの行が残り、publish の読み方では壊れた
    レコードになるため。
  - 原文が行をまたぐレコードの1行目: 列数が合わないので編集できない → 行をまたぐ
    レコードなので編集できない。キーと原文の全体を持つ。
  - 続きの行: 列数が合わない行 → レコードとして解釈しない生の行。
  - 閉じない引用符: その物理行の終わりで閉じたものとして、どの行も編集できる →
    ファイル全体を読み取り専用にし、引用符が開いた行から後ろを生の行のまま並べる。

保存の単位は物理行のまま（PR3 でレコードへ移す）。見本の英文と訳はどれも架空の文である。
*/

// mlWorking は、実物と同じく CRLF で区切った作業コピー。訳が行をまたぐレコード
// （2〜3行目）と、原文が空行を挟んで行をまたぐ訳の空のレコード（4〜6行目）と、
// 訳の中に '#' で始まる行があるレコード（8〜9行目）を持つ。
var mlWorking = "key,section,node,order,speaker,source_en,translation\r\n" +
	key.For("one") + ",UI,,,UI,one,\"いち\nに\"\r\n" +
	key.For("para1\n\npara2") + ",UI,,,UI,\"para1\n\npara2\",\r\n" +
	key.For("two") + ",UI,,,UI,two,さん\r\n" +
	key.For("three") + ",UI,,,UI,three,\"よん\n# ご\"\r\n"

// TestParseMultilineRecords は、行をまたぐレコードのどの物理行も、理由を付けて編集
// させないことを固定する。1行目はキーと原文の全体を持ち、続きの行はレコードとして
// 解釈しない生の行になる。値の中の '#' で始まる行は見出しにしない。
func TestParseMultilineRecords(t *testing.T) {
	f := Parse([]byte(mlWorking))
	if f.ReadOnly() {
		t.Fatalf("ファイル全体が読み取り専用になっている: %s", f.ReadOnlyReason())
	}

	type want struct {
		kind        Kind
		editable    bool
		key         string
		source      string
		translation string
		// span は行をまたぐレコードの理由の置換（"2-3" の形）。空なら理由が無いか、
		// その理由ではない。
		span string
	}
	wants := []want{
		{kind: KindHeader},
		{kind: KindData, key: key.For("one"), source: "one", span: "2-3"},
		{kind: KindData, span: "2-3"},
		{kind: KindData, key: key.For("para1\n\npara2"), source: "para1\n\npara2", span: "4-6"},
		{kind: KindBlank, span: "4-6"},
		{kind: KindData, span: "4-6"},
		{kind: KindData, editable: true, key: key.For("two"), source: "two", translation: "さん"},
		{kind: KindData, key: key.For("three"), source: "three", span: "8-9"},
		{kind: KindData, span: "8-9"},
	}
	lines := f.Lines()
	if len(lines) != len(wants) {
		t.Fatalf("物理行ごとに %d 行のはずが %d 行", len(wants), len(lines))
	}
	for i, w := range wants {
		l := lines[i]
		source := ""
		if l.Kind == KindData && len(l.Fields) == 7 {
			source = l.Fields[5]
		}
		span := ""
		if l.Cause.ID == reason.EditMultiline {
			span = argOf(l.Cause, "line") + "-" + argOf(l.Cause, "end")
		}
		got := want{l.Kind, l.Editable, l.Key(), source, l.Translation(), span}
		if got != w {
			t.Errorf("%d行目 = %+v\n       want %+v", l.Number, got, w)
		}
	}

	// どの物理行にも書かせない。理由は行をまたぐレコード。
	for _, n := range []int{2, 3, 4, 6, 8, 9} {
		var notEditable *NotEditableError
		if err := f.SetTranslation(n, "訳"); !errors.As(err, &notEditable) || notEditable.Cause.ID != reason.EditMultiline {
			t.Errorf("%d行目に書けてしまう、または理由が違う: %v", n, err)
		}
	}
	// 1物理行に収まるレコードは、いままでどおり書ける。触っていない行は1バイトも変えない。
	if err := f.SetTranslation(7, "さんさん"); err != nil {
		t.Fatalf("7行目に書けない: %v", err)
	}
	if want := strings.Replace(mlWorking, ",two,さん\r\n", ",two,さんさん\r\n", 1); string(f.Bytes()) != want {
		t.Errorf("書いた結果が違う\n got %q\nwant %q", f.Bytes(), want)
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
			for _, l := range lines {
				if err := f.SetTranslation(l.Number, "訳"); !errors.Is(err, ErrReadOnly) {
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
