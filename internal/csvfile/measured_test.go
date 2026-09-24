package csvfile

import (
	"errors"
	"testing"
)

// TestReadPowerShellRowsHeaderSelection は、ヘッダーに選ばれる行の規則を固定する。
//
// ヘッダーの前で飛ばすのは、空行と空白だけの行である。上流の hash-strings.ps1
// （55d2e09 / c8fda90 の Remove-NonRecords）がヘッダーを選ぶ前にこれらを落とす
// ので、それに合わせてある。003ed1e の ConvertFrom-Csv は完全な空行しか飛ばさず、
// 空白だけの行が列0個のヘッダーになって、公開CSVの全行が黙って捨てられていた。
//
// "," と '""' の行は上流でも落ちず、ヘッダーになる。データ行に使う空行相当の判定を
// ここへ持ち込まないことも、あわせて固定する。
func TestReadPowerShellRowsHeaderSelection(t *testing.T) {
	tests := []struct {
		name    string
		csv     string
		wantLen int
		// wantKey は1行目の key 列。列が引けない場合は wantHasKey が false。
		wantHasKey bool
		wantKey    string
		// wantHeaderLine はヘッダーに選んだ行の番号（[ReadPowerShellTable]）。
		wantHeaderLine int
	}{
		{
			name:           "空行は飛ばして次の行がヘッダーになる",
			csv:            "\nkey,translation\nabc,あ\n",
			wantLen:        1,
			wantHasKey:     true,
			wantKey:        "abc",
			wantHeaderLine: 2,
		},
		{
			name:           "空行が続いても飛ばす",
			csv:            "\n\nkey,translation\nabc,あ\n",
			wantLen:        1,
			wantHasKey:     true,
			wantKey:        "abc",
			wantHeaderLine: 3,
		},
		{
			// 上流の報告 #8 の入力（p-ws-above-header）と同じ形。
			name:           "空白だけの行は飛ばす",
			csv:            "   \nkey,translation\nabc,あ\n",
			wantLen:        1,
			wantHasKey:     true,
			wantKey:        "abc",
			wantHeaderLine: 2,
		},
		{
			name:           "タブだけの行も飛ばす",
			csv:            "\t\nkey,translation\nabc,あ\n",
			wantLen:        1,
			wantHasKey:     true,
			wantKey:        "abc",
			wantHeaderLine: 2,
		},
		{
			// .NET の Trim が落とす文字は、半角スペースとタブだけではない。
			name:           "全角空白や改行でない空白文字だけの行も飛ばす",
			csv:            "　\n \t\v\f\nkey,translation\nabc,あ\n",
			wantLen:        1,
			wantHasKey:     true,
			wantKey:        "abc",
			wantHeaderLine: 3,
		},
		{
			// 上流の報告 #8 で、上流と一致していた入力（p-comment-blank-header）。
			name:           "コメントと空行と空白だけの行が混ざっても飛ばす",
			csv:            "# note\n\n  \n# more\nkey,translation\nabc,あ\n",
			wantLen:        1,
			wantHasKey:     true,
			wantKey:        "abc",
			wantHeaderLine: 5,
		},
		{
			// 空白だけの行を落とすのはヘッダーの前だけの話ではないが、データ行では
			// もともと [ParsePowerShellRecord] がレコードにしない。
			name:           "ヘッダーの後ろの空白だけの行はレコードにならない",
			csv:            "key,translation\n   \nabc,あ\n",
			wantLen:        1,
			wantHasKey:     true,
			wantKey:        "abc",
			wantHeaderLine: 1,
		},
		{
			// 列が1個（名前は空文字）のヘッダー。元実装では H1 という既定名が
			// 付くが、名前で引くかぎりどちらも key を引けない点は変わらない。
			name:           "カンマだけの行はヘッダーになる",
			csv:            ",\nkey,translation\nabc,あ\n",
			wantLen:        2,
			wantHasKey:     false,
			wantHeaderLine: 1,
		},
		{
			name:           "空の引用フィールドだけの行もヘッダーになる",
			csv:            `""` + "\nkey,translation\nabc,あ\n",
			wantLen:        2,
			wantHasKey:     false,
			wantHeaderLine: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := ReadPowerShellRows([]byte(tt.csv))
			if err != nil {
				t.Fatalf("ReadPowerShellRows が失敗した: %v", err)
			}
			table, err := ReadPowerShellTable([]byte(tt.csv))
			if err != nil {
				t.Fatalf("ReadPowerShellTable が失敗した: %v", err)
			}
			if table.HeaderLine != tt.wantHeaderLine {
				t.Errorf("ヘッダーの行 = %d, want %d", table.HeaderLine, tt.wantHeaderLine)
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

// TestReadPowerShellRowsEmptyColumnNames は、空の列名が重複の判定に入らないことを
// 固定する。
//
// ConvertFrom-Csv は空の列名に H1, H2 ... の既定名を振り、警告を出すだけで
// 止まらない（pwsh 7.6.6 で、移植元の Read-Csv を $ErrorActionPreference = 'Stop'
// のまま実測）。表計算ソフトで保存すると "key,translation,,," のように空の列が
// 末尾に付くことがある。空の名前どうしを重複と数えると、元実装なら公開できる
// ファイルでこちらだけが止まる。空白の列名は空ではないので、重複すれば止まる。
func TestReadPowerShellRowsEmptyColumnNames(t *testing.T) {
	tests := []struct {
		name string
		csv  string
		// wantDup は重複として返るべき列名。空なら成功を期待する。
		wantDup string
	}{
		{
			name: "末尾に空の列が並ぶ",
			csv:  "key,translation,,,\nabc,あ,,,\n",
		},
		{
			name: "先頭に空の列が並ぶ",
			csv:  ",,key,translation\nx,y,abc,あ\n",
		},
		{
			name: "空の引用フィールドが並ぶ",
			csv:  `"","",key,translation` + "\nx,y,abc,あ\n",
		},
		{
			name:    "空白の列名は重複になる",
			csv:     `" "," ",key,translation` + "\nx,y,abc,あ\n",
			wantDup: " ",
		},
		{
			name:    "空の列をはさんでも同名の列は重複になる",
			csv:     "key,,KEY,translation\nabc,x,abc,あ\n",
			wantDup: "KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := ReadPowerShellRows([]byte(tt.csv))
			if tt.wantDup != "" {
				var dup *DuplicateColumnError
				if !errors.As(err, &dup) {
					t.Fatalf("err = %v, want *DuplicateColumnError", err)
				}
				if dup.Name != tt.wantDup {
					t.Errorf("重複した列名 = %q, want %q", dup.Name, tt.wantDup)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadPowerShellRows が失敗した: %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("行数 = %d, want 1", len(rows))
			}
			if got := rows[0].Get("key"); got != "abc" {
				t.Errorf("Get(key) = %q, want %q", got, "abc")
			}
			if got := rows[0].Get("translation"); got != "あ" {
				t.Errorf("Get(translation) = %q, want %q", got, "あ")
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
