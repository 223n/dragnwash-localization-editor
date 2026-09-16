package order

import "testing"

func TestLevelMetaSection(t *testing.T) {
	tests := []struct {
		name string
		meta LevelMeta
		want string
	}{
		{"level 0 は L01", LevelMeta{Index: 0, Dragon: "Ryan"}, "L01 Ryan"},
		{"level 9 は L10", LevelMeta{Index: 9, Dragon: "Ryan"}, "L10 Ryan"},
		{"level 14 は L15", LevelMeta{Index: 14, Dragon: "Alexander"}, "L15 Alexander"},
		{"3桁はゼロ埋めしない", LevelMeta{Index: 99, Dragon: "Ryan"}, "L100 Ryan"},
		{"dragon が空でも空白は入る", LevelMeta{Index: 0}, "L01 "},
		{"level -1 は L00", LevelMeta{Index: -1, Dragon: "Ryan"}, "L00 Ryan"},
		{"負値は符号の後ろをゼロ埋めする（.NET の書式 00 に合わせる）", LevelMeta{Index: -2, Dragon: "Ryan"}, "L-01 Ryan"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.meta.Section(); got != tt.want {
				t.Errorf("Section() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLevelMetaHeader(t *testing.T) {
	tests := []struct {
		name string
		meta LevelMeta
		want string
	}{
		{
			name: "全部そろっている（実データ level 4）",
			meta: LevelMeta{Index: 4, Dragon: "Conrad", Weather: "Rainy", SetFlags: "level_5", EndFlags: "MedkitCompleted, level_5_complete"},
			want: "Level 5: Conrad (Rainy) | sets level_5 | ends MedkitCompleted, level_5_complete",
		},
		{
			name: "天気だけ（実データ level 8）",
			meta: LevelMeta{Index: 8, Dragon: "Alexander", Weather: "Sunny"},
			want: "Level 9: Alexander (Sunny)",
		},
		{
			name: "ends だけ（実データ level 9）",
			meta: LevelMeta{Index: 9, Dragon: "Ryan", Weather: "Sunny", EndFlags: "PicnicCompleted"},
			want: "Level 10: Ryan (Sunny) | ends PicnicCompleted",
		},
		{
			name: "sets だけ（実データ level 7）",
			meta: LevelMeta{Index: 7, Dragon: "Conrad", Weather: "Sunny", SetFlags: "DeliveredMountFrame"},
			want: "Level 8: Conrad (Sunny) | sets DeliveredMountFrame",
		},
		{
			name: "見出しの番号はゼロ埋めしない",
			meta: LevelMeta{Index: 0, Dragon: "Ryan"},
			want: "Level 1: Ryan",
		},
		{
			name: "天気が空なら空の括弧も出さない",
			meta: LevelMeta{Index: 1, Dragon: "Conrad", SetFlags: "a", EndFlags: "b"},
			want: "Level 2: Conrad | sets a | ends b",
		},
		{
			name: "後置は天気→sets→ends の順",
			meta: LevelMeta{Index: 1, Dragon: "Conrad", Weather: "Night", SetFlags: "a", EndFlags: "b"},
			want: "Level 2: Conrad (Night) | sets a | ends b",
		},
		{
			name: "負値はゼロ埋めなしでそのまま出る",
			meta: LevelMeta{Index: -2, Dragon: "Ryan"},
			want: "Level -1: Ryan",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.meta.Header(); got != tt.want {
				t.Errorf("Header() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseLevels(t *testing.T) {
	const header = "flow_asset,level,dragon,set_flags,end_flags,weather\n"

	tests := []struct {
		name string
		csv  string
		want []LevelMeta
	}{
		{
			name: "実データと同じ形の1行",
			csv:  header + "LevelFlow,0,Ryan,level_1,level_1_complete,Sunny\n",
			want: []LevelMeta{{Index: 0, Dragon: "Ryan", Weather: "Sunny", SetFlags: "level_1", EndFlags: "level_1_complete"}},
		},
		{
			name: "複数値の区切り「 | 」は「, 」になる（R9）",
			csv:  header + "LevelFlow,4,Conrad,level_5,MedkitCompleted | level_5_complete,Rainy\n",
			want: []LevelMeta{{Index: 4, Dragon: "Conrad", Weather: "Rainy", SetFlags: "level_5", EndFlags: "MedkitCompleted, level_5_complete"}},
		},
		{
			name: "3値以上でも全部置換する",
			csv:  header + "LevelFlow,0,Ryan,a | b | c,,\n",
			want: []LevelMeta{{Index: 0, Dragon: "Ryan", SetFlags: "a, b, c"}},
		},
		{
			name: "空白の無いパイプは置換しない",
			csv:  header + "LevelFlow,0,Ryan,a|b,a |b,\n",
			want: []LevelMeta{{Index: 0, Dragon: "Ryan", SetFlags: "a|b", EndFlags: "a |b"}},
		},
		{
			// 前後に余分な空白があっても、内側の3文字がそろっていれば置換される。
			// 置換は単純な部分文字列の置き換えで、区切りの前後を見ない。
			name: "空白が2つある場合は内側だけ置換される",
			csv:  header + "LevelFlow,0,Ryan,a  |  b,,\n",
			want: []LevelMeta{{Index: 0, Dragon: "Ryan", SetFlags: "a ,  b"}},
		},
		{
			name: "weather と dragon は無加工",
			csv:  header + "LevelFlow,0, Ryan , a | b , , Sunny \n",
			want: []LevelMeta{{Index: 0, Dragon: " Ryan ", Weather: " Sunny ", SetFlags: " a, b ", EndFlags: " "}},
		},
		{
			name: "level が非数値の行は捨てる（R8）",
			csv: header +
				"LevelFlow,x,Ryan,,,Sunny\n" +
				"LevelFlow,1,Conrad,,,Sunny\n",
			want: []LevelMeta{{Index: 1, Dragon: "Conrad", Weather: "Sunny"}},
		},
		{
			// pwsh 7.6.6 の [int]'' は 0 を返す。捨てるとレベル1の見出しが変わる。
			name: "level が空欄の行は Index 0 として残す",
			csv:  header + "LevelFlow,,Ryan,,,Sunny\n",
			want: []LevelMeta{{Index: 0, Dragon: "Ryan", Weather: "Sunny"}},
		},
		{
			// 列が無いときのプロパティは $null で、[int]$null も 0 になる。
			name: "level 列そのものが無くても Index 0 として残す",
			csv:  "flow_asset,dragon,weather\nLevelFlow,Ryan,Sunny\n",
			want: []LevelMeta{{Index: 0, Dragon: "Ryan", Weather: "Sunny"}},
		},
		{
			name: "level が整数として読めない行は捨てる",
			csv:  header + "LevelFlow,abc,Ryan,,,Sunny\n",
			want: []LevelMeta{},
		},
		{
			name: "列名の大文字小文字は無視される",
			csv:  "Flow_Asset,LEVEL,Dragon,Set_Flags,End_Flags,Weather\nLevelFlow,2,Alexander,,,Sunny\n",
			want: []LevelMeta{{Index: 2, Dragon: "Alexander", Weather: "Sunny"}},
		},
		{
			name: "ファイル順を保つ",
			csv: header +
				"LevelFlow,2,Alexander,,,Sunny\n" +
				"LevelFlow,0,Ryan,,,Sunny\n",
			want: []LevelMeta{
				{Index: 2, Dragon: "Alexander", Weather: "Sunny"},
				{Index: 0, Dragon: "Ryan", Weather: "Sunny"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLevels(rowsFromCSV(tt.csv))
			if len(got) != len(tt.want) {
				t.Fatalf("件数 = %d, want %d（%+v）", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPad2(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "00"},
		{1, "01"},
		{9, "09"},
		{10, "10"},
		{99, "99"},
		{100, "100"},
		{-1, "-01"},
		{-10, "-10"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := pad2(tt.n); got != tt.want {
				t.Errorf("pad2(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}
