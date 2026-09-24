package edit

import (
	"errors"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

/*
ここの試験は、全体を解釈する読み手へ移す作業（docs/port-spec.md）の PR2 で、edit が
複数行のレコードと閉じない引用符を扱う前の結果を固定する（決まったことの 13）。

どれも名前の末尾が LineBasedNow で、「物理行を1行ずつ見るいまの結果」を固定するだけで、
正しいとはしない。いまの編集モデルは物理行を1行ずつ読むので、行をまたぐレコードは
物理行ごとに割れ、閉じない引用符はその物理行の終わりで閉じたものとして読む。PR2 では、
複数行のレコードに属する物理行を編集させず、閉じない引用符のあるファイルは全体を
編集させない（design の phases[2]）。そこで期待値が変わるので、そのコミットで直す。

見本の英文と訳はどれも架空の文である。
*/

// lbWorking は、実物と同じく CRLF で区切った作業コピー。訳が行をまたぐレコード
// （2〜3行目）と、原文が空行を挟んで行をまたぐ訳の空のレコード（4〜6行目）を持つ。
var lbWorking = "key,section,node,order,speaker,source_en,translation\r\n" +
	key.For("one") + ",UI,,,UI,one,\"いち\nに\"\r\n" +
	key.For("para1\n\npara2") + ",UI,,,UI,\"para1\n\npara2\",\r\n" +
	key.For("two") + ",UI,,,UI,two,さん\r\n"

// TestParseMultilineRecordsLineBasedNow は、行をまたぐレコードを物理行ごとの行に
// 割るいまの読み方を固定する。
//
// 訳が行をまたぐレコードの1行目は、区切りの数がヘッダーと同じ7つなので編集できる
// 行に見え、訳は「いち」までに切れて見える。ここへ書くと続きの行（`に"`）が残り、
// 全体を解釈する読み方では壊れたレコードになる。原文が行をまたぐレコードの1行目と
// 続きの行は、列数がヘッダーと合わないので編集できない。
func TestParseMultilineRecordsLineBasedNow(t *testing.T) {
	f := Parse([]byte(lbWorking))
	if f.ReadOnly() {
		t.Fatalf("ファイル全体が読み取り専用になっている: %s", f.ReadOnlyReason())
	}

	want := []struct {
		kind        Kind
		editable    bool
		translation string
	}{
		{KindHeader, false, ""},
		{KindData, true, "いち"},
		{KindData, false, ""},
		{KindData, false, ""},
		{KindBlank, false, ""},
		{KindData, false, ""},
		{KindData, true, "さん"},
	}
	lines := f.Lines()
	if len(lines) != len(want) {
		t.Fatalf("物理行ごとに %d 行のはずが %d 行", len(want), len(lines))
	}
	for i, w := range want {
		l := lines[i]
		if l.Kind != w.kind || l.Editable != w.editable || l.Translation() != w.translation {
			t.Errorf("%d行目 = {%v %v %q}, want {%v %v %q}",
				l.Number, l.Kind, l.Editable, l.Translation(), w.kind, w.editable, w.translation)
		}
	}

	// 原文が行をまたぐレコードの1行目は、列数が合わないので書かせない。
	var notEditable *NotEditableError
	if err := f.SetTranslation(4, "訳"); !errors.As(err, &notEditable) || notEditable.Cause.ID != reason.EditFieldCount {
		t.Errorf("4行目に書けてしまう、または理由が違う: %v", err)
	}
	// 訳が行をまたぐレコードの1行目は、いまは書けてしまう。
	if err := f.SetTranslation(2, "いちに"); err != nil {
		t.Errorf("2行目は行単位では編集できる行に見えるはず: %v", err)
	}
}

// TestParseUnclosedQuoteLineBasedNow は、閉じない引用符のあるファイルを、その
// 物理行の終わりで閉じたものとして読むいまの振る舞いを固定する。
//
// 全体を解釈する読み方では、引用符が開いた2行目からファイルの終わりまでが1つの値に
// なる。いまは2行目も3行目も編集できる行として並び、2行目の訳は「訳」に見える。
func TestParseUnclosedQuoteLineBasedNow(t *testing.T) {
	f := Parse([]byte("key,translation\n" + key.For("one") + ",\"訳\n" + key.For("two") + ",に\n"))
	if f.ReadOnly() {
		t.Fatalf("ファイル全体が読み取り専用になっている: %s", f.ReadOnlyReason())
	}
	lines := f.Lines()
	if len(lines) != 3 {
		t.Fatalf("行が %d 行、3 行を期待", len(lines))
	}
	for i, want := range []string{"訳", "に"} {
		l := lines[i+1]
		if l.Kind != KindData || !l.Editable || l.Translation() != want {
			t.Errorf("%d行目 = {%v %v %q}, want {data true %q}", l.Number, l.Kind, l.Editable, l.Translation(), want)
		}
	}
	if err := f.SetTranslation(3, "さん"); err != nil {
		t.Errorf("3行目は行単位では編集できるはず: %v", err)
	}
}
