package edit

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// TestConflictErrorMessage は、保存を止めたときの文面が「なぜ書かなかったか」を
// 取り違えずに伝えることを見る。
//
// 自動保存が止まったことを翻訳者が知る手がかりはこの文面だけである。
// 「変わっている」と「読めない」は直し方が違う（読み直すか、ファイルを探すか）。
func TestConflictErrorMessage(t *testing.T) {
	t.Run("読み込み後に変わっている", func(t *testing.T) {
		path := writeTemp(t, sampleWorking)
		f, err := Open(path)
		if err != nil {
			t.Fatalf("開けない: %v", err)
		}
		if err := f.SetTranslation(6, "こんにちは"); err != nil {
			t.Fatalf("書き換えに失敗した: %v", err)
		}
		const outside = "key,translation\n0da72197e898ebe1,別の中身\n"
		if err := os.WriteFile(path, []byte(outside), 0o644); err != nil {
			t.Fatal(err)
		}

		var conflict *ConflictError
		if err := f.Save(); !errors.As(err, &conflict) {
			t.Fatalf("Save = %v, want *ConflictError", err)
		}
		want := fmt.Sprintf("%s が読み込み後に変わっている（読んだ版 %s、いまの版 %s）",
			path, conflict.Want[:12], conflict.Got[:12])
		if got := conflict.Error(); got != want {
			t.Errorf("文面が違う\n got %q\nwant %q", got, want)
		}
		// 版は画面向けに短く切る。照合は全桁で行っているので、文面に全桁は要らない。
		if strings.Contains(conflict.Error(), conflict.Want) {
			t.Errorf("版を全桁のまま出している: %q", conflict.Error())
		}
	})

	t.Run("読み直せない", func(t *testing.T) {
		path := writeTemp(t, sampleWorking)
		f, err := Open(path)
		if err != nil {
			t.Fatalf("開けない: %v", err)
		}
		if err := f.SetTranslation(6, "こんにちは"); err != nil {
			t.Fatalf("書き換えに失敗した: %v", err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}

		var conflict *ConflictError
		if err := f.Save(); !errors.As(err, &conflict) {
			t.Fatalf("Save = %v, want *ConflictError", err)
		}
		if conflict.Err == nil {
			t.Fatal("読めなかった原因が入っていない")
		}
		want := fmt.Sprintf("%s を読み直せないので保存しない: %v", path, conflict.Err)
		if got := conflict.Error(); got != want {
			t.Errorf("文面が違う\n got %q\nwant %q", got, want)
		}
		// 照合まで行っていないので、版の話をしてはいけない。
		if strings.Contains(conflict.Error(), "変わっている") {
			t.Errorf("読めなかったのに「変わっている」と言っている: %q", conflict.Error())
		}
	})
}

// TestNotEditableErrorCarriesOneReason は、編集できない理由を文面と識別子の
// 両方で、同じものとして持つことを見る。
//
// CLI は Error() の文面を、画面は Cause から目録で組み直した文面を出す。
// 片方だけ入っていたり中身がずれていたりすると、同じ行について CLI と画面が
// 別の理由を言う。
func TestNotEditableErrorCarriesOneReason(t *testing.T) {
	const header = "key,section,node,order,speaker,translation\n"
	tests := []struct {
		name     string
		content  string
		line     int
		wantID   string
		wantArgs []string
	}{
		{
			name:    "無い行番号",
			content: header,
			line:    5,
			wantID:  reason.EditNoSuchLine,
		},
		{
			name:     "コメント行",
			content:  header + "# 見出し\n",
			line:     2,
			wantID:   reason.EditNotDataLine,
			wantArgs: []string{"kind", "comment"},
		},
		{
			name:     "列が多い行",
			content:  header + "abc,,,,,,余分\n",
			line:     2,
			wantID:   reason.EditFieldCount,
			wantArgs: []string{"header", "6", "row", "7"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Parse([]byte(tt.content))
			var e *NotEditableError
			if err := f.SetTranslation(tt.line, "x"); !errors.As(err, &e) {
				t.Fatalf("err = %v, want *NotEditableError", err)
			}
			if e.Cause.ID != tt.wantID {
				t.Errorf("識別子が %q、期待 %q", e.Cause.ID, tt.wantID)
			}
			if !slices.Equal(e.Cause.Args, tt.wantArgs) {
				t.Errorf("置換が %q、期待 %q", e.Cause.Args, tt.wantArgs)
			}
			if e.Cause.Text != e.Reason {
				t.Errorf("文面が2つに割れている: Reason %q / Cause.Text %q", e.Reason, e.Cause.Text)
			}
			if want := fmt.Sprintf("%d行目は編集できない: %s", tt.line, e.Reason); e.Error() != want {
				t.Errorf("Error() = %q、期待 %q", e.Error(), want)
			}
			// 行が持っている理由と、書き込みが返す理由は同じもの。画面は行を
			// 並べるときに前者を、保存を拒まれたときに後者を出す。
			if line, ok := f.Line(tt.line); ok && line.Kind == KindData && line.Cause.ID != e.Cause.ID {
				t.Errorf("行の理由 %q と書き込みの理由 %q が違う", line.Cause.ID, e.Cause.ID)
			}
		})
	}
}

// TestInvalidValueErrorCarriesOneReason は、書けない値の理由を文面と識別子の
// 両方で、同じものとして持つことを見る。意味は [TestNotEditableErrorCarriesOneReason] と同じ。
func TestInvalidValueErrorCarriesOneReason(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		wantID string
	}{
		{"改行", "前\n後", reason.EditNoNewline},
		{"NUL", "前\x00後", reason.EditNoNUL},
		{"不正なUTF-8", "\xff\xfe壊れた", reason.EditBadUTF8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Parse([]byte("key,translation\nabc,古い\n"))
			var e *InvalidValueError
			if err := f.SetTranslation(2, tt.value); !errors.As(err, &e) {
				t.Fatalf("err = %v, want *InvalidValueError", err)
			}
			if e.Line != 2 {
				t.Errorf("行番号が %d", e.Line)
			}
			if e.Cause.ID != tt.wantID {
				t.Errorf("識別子が %q、期待 %q", e.Cause.ID, tt.wantID)
			}
			if e.Cause.Text != e.Reason {
				t.Errorf("文面が2つに割れている: Reason %q / Cause.Text %q", e.Reason, e.Cause.Text)
			}
			if want := "2行目に書けない値: " + e.Reason; e.Error() != want {
				t.Errorf("Error() = %q、期待 %q", e.Error(), want)
			}
		})
	}
}

// TestShort は版の切り詰めの境目を固定する。
//
// 読み直しに失敗したときの版は空なので、短いものも落とさずそのまま返す。
func TestShort(t *testing.T) {
	full := hashBytes([]byte("key,translation\n"))
	tests := []struct {
		name, in, want string
	}{
		{"空", "", ""},
		{"短い", "abc", "abc"},
		{"ちょうど12桁", full[:12], full[:12]},
		{"13桁", full[:13], full[:12]},
		{"全桁", full, full[:12]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := short(tt.in); got != tt.want {
				t.Errorf("short(%q) = %q、期待 %q", tt.in, got, tt.want)
			}
		})
	}
}
