package csvfile

import (
	"slices"
	"testing"
)

func TestFieldOffsets(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []int
	}{
		{"空文字", "", []int{0}},
		{"1フィールド", "abc", []int{0}},
		{"2フィールド", "a,b", []int{0, 2}},
		{"3フィールド", "a,b,c", []int{0, 2, 4}},
		// この段の要。ParsePowerShellRecord は末尾の空フィールドを落とすが、
		// ここでは落とさない。作業コピーの未訳行がこの形。
		{"末尾が空", "a,b,", []int{0, 2, 4}},
		{"末尾が空を2つ", "a,,", []int{0, 2, 3}},
		{"カンマだけ", ",", []int{0, 1}},
		{"カンマ2つ", ",,", []int{0, 1, 2}},
		{"引用フィールド", `a,"b",c`, []int{0, 2, 6}},
		{"引用の中のカンマは区切りでない", `a,"b,c",d`, []int{0, 2, 8}},
		{"引用の中の二重引用符", `a,"b""c",d`, []int{0, 2, 9}},
		{"閉じ引用符の後ろに文字", `a,"b"x,c`, []int{0, 2, 7}},
		{"閉じていない引用符", `a,"b,c`, []int{0, 2}},
		{"引用符1つだけ", `"`, []int{0}},
		{"空の引用フィールド", `""`, []int{0}},
		{"先頭の空白は区切りに影響しない", ` a , b `, []int{0, 4}},
		{"タブは区切りでない", "a\tb,c", []int{0, 4}},
		// 位置はバイト単位。'鍵' は UTF-8 で3バイトなので、2つ目は4から始まる。
		{"非ASCII", "鍵,訳,です", []int{0, 4, 8}},
		{"引用の中の非ASCII", `a,"訳,続き",c`, []int{0, 2, 15}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FieldOffsets(tt.line)
			if !slices.Equal(got, tt.want) {
				t.Errorf("FieldOffsets(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

// TestFieldOffsetsContractVsParseRecord は ParsePowerShellRecord との契約の差を
// 固定する。末尾の空フィールドを落とすかどうかが唯一の差で、
// 落とすのは最大1つ、必ず末尾。この前提が崩れると internal/edit の
// 「フィールドを空文字で埋める」処理が別の列を埋めてしまう。
func TestFieldOffsetsContractVsParseRecord(t *testing.T) {
	lines := []string{
		"", " ", ",", ",,", `""`, `"`,
		"a", "a,b", "a,b,", "a,,", "a,b,c",
		`a,"b",`, `a,"b,c",`, `a,"b"`, `a,"`, `a,"x`,
		" a , b , ", "a,b, ", "鍵,,訳",
	}
	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			offsets := FieldOffsets(line)
			// 比較の相手は parsePowerShellFields。ParsePowerShellRecord は
			// 空行相当の行で0個を返すので、構造の比較には向かない
			// （internal/edit は空行相当をデータ行として扱わない）。
			fields := parsePowerShellFields(line)

			if len(offsets) == 0 {
				t.Fatalf("FieldOffsets(%q) が空", line)
			}
			if offsets[0] != 0 {
				t.Errorf("先頭が0でない: %v", offsets)
			}
			for i := 1; i < len(offsets); i++ {
				if offsets[i] <= offsets[i-1] {
					t.Errorf("昇順でない: %v", offsets)
				}
			}
			if last := offsets[len(offsets)-1]; last > len(line) {
				t.Errorf("行の長さを超える位置 %d: %v", last, offsets)
			}
			// 落ちるのは最大1つ、しかも末尾だけ。
			if d := len(offsets) - len(fields); d < 0 || d > 1 {
				t.Errorf("フィールド数の差が想定外: offsets=%d fields=%d", len(offsets), len(fields))
			}
			// 各位置から次のカンマ（または行末）までが、そのフィールドの生の範囲。
			// 最終フィールドの範囲を切り出して差し替えるのが internal/edit の保存。
			for i, off := range offsets {
				if i+1 < len(offsets) && line[offsets[i+1]-1] != ',' {
					t.Errorf("位置 %d の直前が区切りのカンマでない", offsets[i+1])
				}
				_ = off
			}
		})
	}
}

// TestFieldOffsetsLastFieldReplacement は「最終フィールドを差し替える」操作が
// 期待どおりの範囲を書き換えることを固定する。internal/edit の保存そのもの。
func TestFieldOffsetsLastFieldReplacement(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		value string
		want  string
	}{
		{"未訳行に訳を入れる", "abc,L01 Ryan,n,1,Ryan,", "こんにちは", "abc,L01 Ryan,n,1,Ryan,こんにちは"},
		{"訳を差し替える", "abc,L01 Ryan,n,1,Ryan,古い", "新しい", "abc,L01 Ryan,n,1,Ryan,新しい"},
		{"引用された訳を差し替える", `abc,,,,,"a,b"`, "c", "abc,,,,,c"},
		{"カンマを含む訳", "abc,,,,,x", "a,b", `abc,,,,,"a,b"`},
		{"引用符を含む訳", "abc,,,,,x", `a"b`, `abc,,,,,"a""b"`},
		{"訳を空にする", "abc,,,,,x", "", "abc,,,,,"},
		{"前半に引用フィールドがあっても触らない", `abc,"L01, Ryan",n,1,Ryan,x`, "y", `abc,"L01, Ryan",n,1,Ryan,y`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			offsets := FieldOffsets(tt.line)
			start := offsets[len(offsets)-1]
			got := tt.line[:start] + EscapeField(tt.value)
			if got != tt.want {
				t.Errorf("差し替え結果が違う:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}
