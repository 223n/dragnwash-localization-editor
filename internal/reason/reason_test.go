package reason

import (
	"sort"
	"strings"
	"testing"
)

// TestAllIsUnique は識別子が重複していないことを見る。
//
// 識別子は平らな1つの名前空間にある（目録の鍵は "reason." + ID）。2つの理由が
// 同じ名前を持つと、片方の文面がもう片方の場所に出る。日本語が残るより悪い。
func TestAllIsUnique(t *testing.T) {
	seen := make(map[string]struct{}, len(all))
	var dup []string
	for _, id := range All() {
		if _, ok := seen[id]; ok {
			dup = append(dup, id)
		}
		seen[id] = struct{}{}
	}
	sort.Strings(dup)
	if len(dup) > 0 {
		t.Errorf("同じ識別子が2度ある: %v", dup)
	}
}

// TestAllIsASCII は識別子が ASCII の小文字と数字と下線だけであることを見る。
//
// 目録の鍵になる文字列なので、表示名を変えても対応が切れない形にしておく。
// [Category.ID] と [Status.ID] と同じ付け方にそろえてある。
func TestAllIsASCII(t *testing.T) {
	for _, id := range All() {
		if id == "" {
			t.Error("空の識別子がある")
			continue
		}
		for _, r := range id {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
				continue
			}
			t.Errorf("%q に使えない字がある: %q", id, r)
			break
		}
		if strings.HasPrefix(id, "_") || strings.HasSuffix(id, "_") {
			t.Errorf("%q の端が下線になっている", id)
		}
	}
}

// TestAllIsACopy は [All] が内部の並びを外へ渡していないことを見る。
//
// 渡すと、呼び出し側の並べ替えがこのパッケージの並びを変える。
func TestAllIsACopy(t *testing.T) {
	first := All()
	if len(first) == 0 {
		t.Fatal("識別子が1つも無い")
	}
	first[0] = "tampered"
	if All()[0] == "tampered" {
		t.Error("内部の並びが書き換わった")
	}
}

func TestNewAndString(t *testing.T) {
	why := New(NoteLocaleGap, "他の 3 ロケールにあります", "count", "3")
	if why.ID != NoteLocaleGap {
		t.Errorf("識別子が %q", why.ID)
	}
	if got := why.String(); got != "他の 3 ロケールにあります" {
		t.Errorf("文面が %q", got)
	}
	if len(why.Args) != 2 || why.Args[0] != "count" || why.Args[1] != "3" {
		t.Errorf("置換が %v", why.Args)
	}
	if why.Empty() {
		t.Error("空だと言われた")
	}
}

// TestEmpty は、識別子も文面も無いときだけ空と見なすことを見る。
//
// 識別子を持たない理由（[diff.OldOrderSource] を差し替えた呼び出し側が作る
// 誤り）は、文面だけでも出さなければならない。空と見なすと、その理由が
// 画面から消える。
func TestEmpty(t *testing.T) {
	cases := []struct {
		why  Reason
		want bool
	}{
		{Reason{}, true},
		{New("", "文面だけ"), false},
		{New(EditNoNUL, ""), false},
		{New(EditNoNUL, "訳に NUL は入れられない"), false},
	}
	for _, tc := range cases {
		if got := tc.why.Empty(); got != tc.want {
			t.Errorf("Empty(%+v) = %v、%v を期待", tc.why, got, tc.want)
		}
	}
}
