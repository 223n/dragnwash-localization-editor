package csvfile

import (
	"testing"
)

// TestReadPowerShellRowsHeaderSelection は、ヘッダーに選ばれる行の規則を固定する。
//
// ConvertFrom-Csv が読み飛ばすのは完全な空行だけで、空白だけの行や "," も
// ヘッダーになる（pwsh 7.6.6 で実測）。データ行に使う空行相当の判定をここへ
// 持ち込むと、先頭に空白だけの行が1本あるだけで本物のヘッダーがデータ行へずれ、
// 公開CSVの全行が黙って捨てられる。
func TestReadPowerShellRowsHeaderSelection(t *testing.T) {
	tests := []struct {
		name    string
		csv     string
		wantLen int
		// wantKey は1行目の key 列。列が引けない場合は wantHasKey が false。
		wantHasKey bool
		wantKey    string
	}{
		{
			name:       "空行は飛ばして次の行がヘッダーになる",
			csv:        "\nkey,translation\nabc,あ\n",
			wantLen:    1,
			wantHasKey: true,
			wantKey:    "abc",
		},
		{
			name:       "空行が続いても飛ばす",
			csv:        "\n\nkey,translation\nabc,あ\n",
			wantLen:    1,
			wantHasKey: true,
			wantKey:    "abc",
		},
		{
			// 列が0個のヘッダーになるので、以降の行はどの列も引けない。
			name:       "空白だけの行はヘッダーになる",
			csv:        "   \nkey,translation\nabc,あ\n",
			wantLen:    2,
			wantHasKey: false,
		},
		{
			name:       "タブだけの行もヘッダーになる",
			csv:        "\t\nkey,translation\nabc,あ\n",
			wantLen:    2,
			wantHasKey: false,
		},
		{
			// 列が1個（名前は空文字）のヘッダー。元実装では H1 という既定名が
			// 付くが、名前で引くかぎりどちらも key を引けない点は変わらない。
			name:       "カンマだけの行もヘッダーになる",
			csv:        ",\nkey,translation\nabc,あ\n",
			wantLen:    2,
			wantHasKey: false,
		},
		{
			name:       "空の引用フィールドだけの行もヘッダーになる",
			csv:        `""` + "\nkey,translation\nabc,あ\n",
			wantLen:    2,
			wantHasKey: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := ReadPowerShellRows([]byte(tt.csv))
			if err != nil {
				t.Fatalf("ReadPowerShellRows が失敗した: %v", err)
			}
			if len(rows) != tt.wantLen {
				t.Fatalf("行数 = %d, want %d", len(rows), tt.wantLen)
			}
			if len(rows) == 0 {
				return
			}
			got, ok := rows[0].Lookup("key")
			if ok != tt.wantHasKey {
				t.Fatalf("key 列の有無 = %v, want %v", ok, tt.wantHasKey)
			}
			if ok && got != tt.wantKey {
				t.Errorf("key = %q, want %q", got, tt.wantKey)
			}
		})
	}
}

// TestParsePowerShellRecordTrailingQuote は、行末の引用フィールドの扱いを固定する。
//
// 捨てるのは「値が空、かつ引用符が閉じていない」末尾フィールドだけ。
// 「引用符で始まったか」で判定すると `b,""` まで残り方が変わる。
// 値はすべて pwsh 7.6.6 の ConvertFrom-Csv で実測した。
func TestParsePowerShellRecordTrailingQuote(t *testing.T) {
	tests := []struct {
		line   string
		want   []string
		wantOK bool
	}{
		{line: `b,"`, want: []string{"b"}, wantOK: true},
		{line: `b,""`, want: []string{"b", ""}, wantOK: true},
		{line: `b,"x`, want: []string{"b", "x"}, wantOK: true},
		{line: "b,", want: []string{"b"}, wantOK: true},
		{line: `,"`, want: nil, wantOK: false},
		{line: `,""`, want: []string{"", ""}, wantOK: true},
		{line: `"`, want: nil, wantOK: false},
		{line: `"  `, want: []string{"  "}, wantOK: true},
		{line: `b,"  `, want: []string{"b", "  "}, wantOK: true},
		{line: `""`, want: nil, wantOK: false},
		{line: `""""`, want: []string{`"`}, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got, ok := ParsePowerShellRecord(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("レコード判定 = %v, want %v（fields=%q）", ok, tt.wantOK, got)
			}
			if !ok {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("fields = %q, want %q", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("fields[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestIsPythonBlankBoundary は、行を落とすかどうかを決める空白判定の境界を固定する。
//
// この関数は internal/validate の「訳が空か」の判定にも使われる。別々に持つと、
// 片方で落ちて片方で残る文字が生まれ、報告される行番号がずれる。
func TestIsPythonBlankBoundary(t *testing.T) {
	blank := []string{"", " ", "\t", "\r", "\n", "\f", "\v", "\x1c", "\x1d", "\x1e", "\x1f", "\u0085", " ", " \t\x1c"}
	notBlank := []string{"a", " a ", "\x00", "\x1b", "\u200b"}

	for _, s := range blank {
		if !IsPythonBlank(s) {
			t.Errorf("IsPythonBlank(%q) = false, want true", s)
		}
	}
	for _, s := range notBlank {
		if IsPythonBlank(s) {
			t.Errorf("IsPythonBlank(%q) = true, want false", s)
		}
	}
}
