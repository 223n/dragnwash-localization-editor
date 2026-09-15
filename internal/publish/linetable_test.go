package publish

import (
	"strings"
	"testing"
)

// op は [lineTable] への1操作。テストの表を読みやすくするために使う。
type op struct {
	kind        string // "add" か "remove"
	id          string
	translation string
}

func TestLineTable(t *testing.T) {
	tests := []struct {
		name string
		ops  []op
		// wantRemaining は残っている項目を "ID=訳" の形で挿入順に並べたもの。
		wantRemaining []string
		// wantLookup は Lookup の期待値。値が空文字なら「見つからない」を意味する。
		wantLookup map[string]string
	}{
		{
			name: "挿入順を保つ",
			ops: []op{
				{kind: "add", id: "line:c", translation: "3"},
				{kind: "add", id: "line:a", translation: "1"},
				{kind: "add", id: "line:b", translation: "2"},
			},
			wantRemaining: []string{"line:c=3", "line:a=1", "line:b=2"},
		},
		{
			name: "同じIDは先勝ち",
			ops: []op{
				{kind: "add", id: "line:a", translation: "さき"},
				{kind: "add", id: "line:a", translation: "あと"},
			},
			wantRemaining: []string{"line:a=さき"},
			wantLookup:    map[string]string{"line:a": "さき"},
		},
		{
			name: "先勝ちの判定は大文字小文字を区別しない",
			ops: []op{
				{kind: "add", id: "line:AA", translation: "さき"},
				{kind: "add", id: "line:aa", translation: "あと"},
			},
			wantRemaining: []string{"line:AA=さき"},
			wantLookup:    map[string]string{"line:aa": "さき", "line:AA": "さき"},
		},
		{
			name: "Remove すると残らず引けなくなる",
			ops: []op{
				{kind: "add", id: "line:a", translation: "1"},
				{kind: "add", id: "line:b", translation: "2"},
				{kind: "remove", id: "line:a"},
			},
			wantRemaining: []string{"line:b=2"},
			wantLookup:    map[string]string{"line:a": "", "line:b": "2"},
		},
		{
			name: "Remove も大文字小文字を区別しない",
			ops: []op{
				{kind: "add", id: "line:AA", translation: "1"},
				{kind: "remove", id: "line:aa"},
			},
			wantRemaining: nil,
			wantLookup:    map[string]string{"line:AA": ""},
		},
		{
			name: "無いIDを Remove しても何も起きない",
			ops: []op{
				{kind: "add", id: "line:a", translation: "1"},
				{kind: "remove", id: "line:zz"},
				{kind: "remove", id: "line:a"},
				{kind: "remove", id: "line:a"},
			},
			wantRemaining: nil,
		},
		{
			name:          "空の表",
			ops:           nil,
			wantRemaining: nil,
			wantLookup:    map[string]string{"line:a": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := newLineTable()
			for _, o := range tt.ops {
				switch o.kind {
				case "add":
					table.Add(o.id, o.translation)
				case "remove":
					table.Remove(o.id)
				default:
					t.Fatalf("知らない操作: %q", o.kind)
				}
			}

			var got []string
			for _, e := range table.Remaining() {
				got = append(got, e.id+"="+e.translation)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantRemaining, ",") {
				t.Errorf("残りが違う\ngot  %v\nwant %v", got, tt.wantRemaining)
			}
			if table.RemainingCount() != len(tt.wantRemaining) {
				t.Errorf("RemainingCount が違う: got %d, want %d", table.RemainingCount(), len(tt.wantRemaining))
			}
			for id, want := range tt.wantLookup {
				value, ok := table.Lookup(id)
				if want == "" {
					if ok {
						t.Errorf("Lookup(%q) が見つかってしまった: %q", id, value)
					}
					continue
				}
				if !ok || value != want {
					t.Errorf("Lookup(%q) が違う: got %q(%v), want %q", id, value, ok, want)
				}
			}
		})
	}
}
