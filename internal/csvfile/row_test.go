package csvfile

import (
	"slices"
	"testing"
)

func TestRowLookup(t *testing.T) {
	header := []string{"key", "Source_EN", "translation"}
	record := []string{"0da72197e898ebe1", "Hello?", "もしもし？"}
	row := NewRow(header, record)

	tests := []struct {
		name      string
		column    string
		wantValue string
		wantFound bool
	}{
		{"綴りどおり", "key", "0da72197e898ebe1", true},
		{"小文字で引く", "source_en", "Hello?", true},
		{"大文字で引く", "TRANSLATION", "もしもし？", true},
		{"混在で引く", "Key", "0da72197e898ebe1", true},
		{"無い列", "speaker", "", false},
		{"空の列名", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := row.Lookup(tt.column)
			if got != tt.wantValue || ok != tt.wantFound {
				t.Errorf("Lookup(%q) = (%q, %v), want (%q, %v)", tt.column, got, ok, tt.wantValue, tt.wantFound)
			}
			if got := row.Get(tt.column); got != tt.wantValue {
				t.Errorf("Get(%q) = %q, want %q", tt.column, got, tt.wantValue)
			}
			if got := row.Has(tt.column); got != tt.wantFound {
				t.Errorf("Has(%q) = %v, want %v", tt.column, got, tt.wantFound)
			}
		})
	}
}

// TestRowFieldCount は、ヘッダーより短い行と長い行の扱いを固定する（移植仕様 R13）。
// 返る Row の列はいつでもヘッダーぶんだけで、行の長さには依存しない。
func TestRowFieldCount(t *testing.T) {
	header := []string{"key", "speaker", "translation"}

	t.Run("列が足りない行は空文字で埋める", func(t *testing.T) {
		row := NewRow(header, []string{"k"})
		if got, ok := row.Lookup("translation"); got != "" || !ok {
			t.Errorf("Lookup(translation) = (%q, %v), want (\"\", true)", got, ok)
		}
		if row.Len() != 3 {
			t.Errorf("Len() = %d, want 3", row.Len())
		}
	})

	t.Run("余った列は捨てる", func(t *testing.T) {
		row := NewRow(header, []string{"k", "s", "t", "余り", "さらに余り"})
		if row.Len() != 3 {
			t.Errorf("Len() = %d, want 3", row.Len())
		}
		if got := row.Columns(); !slices.Equal(got, header) {
			t.Errorf("Columns() = %q, want %q", got, header)
		}
	})
}

// TestRowDuplicateColumns は、同名の列（大文字小文字違いを含む）があるときの
// 挙動を固定する。値は後の列が勝ち、綴りは初出が残る。
// 元実装 CsvReader.cs:35 のインデクサ代入に由来する。
func TestRowDuplicateColumns(t *testing.T) {
	row := NewRow([]string{"key", "KEY", "translation"}, []string{"first", "second", "訳"})

	if got := row.Get("key"); got != "second" {
		t.Errorf("Get(key) = %q, want %q（後の列が勝つ）", got, "second")
	}
	if got, want := row.Columns(), []string{"key", "translation"}; !slices.Equal(got, want) {
		t.Errorf("Columns() = %q, want %q（綴りは初出が残る）", got, want)
	}
}

// TestRowEmptyColumnName は、末尾カンマ付きのヘッダーが作る空の列名を確かめる。
// 元実装も dict[""] を作るので、列名の検証はしない。
func TestRowEmptyColumnName(t *testing.T) {
	row := NewRow([]string{"key", ""}, []string{"k", "v"})
	if got, ok := row.Lookup(""); got != "v" || !ok {
		t.Errorf("Lookup(\"\") = (%q, %v), want (%q, true)", got, ok, "v")
	}
}

// TestRowColumnsIsCopy は、Columns の戻り値を書き換えても Row が壊れないことを確かめる。
func TestRowColumnsIsCopy(t *testing.T) {
	row := NewRow([]string{"key", "translation"}, []string{"k", "t"})
	cols := row.Columns()
	cols[0] = "書き換え"
	if got := row.Columns()[0]; got != "key" {
		t.Errorf("Columns()[0] = %q, want %q", got, "key")
	}
}
