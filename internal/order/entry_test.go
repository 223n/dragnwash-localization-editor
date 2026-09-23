package order

import (
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
)

// rowsFromCSV はテスト用にCSVテキストを行へ分ける。読み方の違いが主題ではない
// テストではこちらを使う。
func rowsFromCSV(text string) []csvfile.Row {
	return csvfile.ReadCSharpRows([]byte(text))
}

func TestNormalizeKey(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"正規化済みはそのまま", "0da72197e898ebe1", "0da72197e898ebe1"},
		{"大文字は小文字になる", "0DA72197E898EBE1", "0da72197e898ebe1"},
		{"前後の空白は落ちる", "  0da72197e898ebe1\t", "0da72197e898ebe1"},
		{"改行も空白として落ちる", "\r\n0da72197e898ebe1\n", "0da72197e898ebe1"},
		{"ノーブレークスペースも落ちる", " 0da72197e898ebe1 ", "0da72197e898ebe1"},
		{"途中の空白は残る", "0da7 2197", "0da7 2197"},
		{"台詞IDも同じ規則で正規化する", " LINE:A8779EBF ", "line:a8779ebf"},
		{"空文字は空文字", "", ""},
		{"空白だけなら空文字", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeKey(tt.raw); got != tt.want {
				t.Errorf("NormalizeKey(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseEntries(t *testing.T) {
	const header = "section,phase,node,order,line_id,key,speaker,condition\n"

	tests := []struct {
		name string
		csv  string
		want []Entry
	}{
		{
			name: "実データと同じ形の1行",
			csv:  header + "L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf,0da72197e898ebe1,Ryan,\n",
			want: []Entry{{
				Section: "L01 Ryan", HasSection: true, Phase: "intro", Node: "Ryan_1_intro", HasNode: true,
				Order: 1, OrderText: "1", LineID: "line:a8779ebf",
				Key: "0da72197e898ebe1", Speaker: "Ryan", Condition: "",
			}},
		},
		{
			name: "condition 付きの行",
			csv:  header + "L08 Conrad,cum,Conrad_Outro_3,1,line:d376acb9,6f64e0cc9b162541,Conrad,$conrad_jerked_off_3 $conrad_used_mount_3\n",
			want: []Entry{{
				Section: "L08 Conrad", HasSection: true, Phase: "cum", Node: "Conrad_Outro_3", HasNode: true,
				Order: 1, OrderText: "1", LineID: "line:d376acb9",
				Key: "6f64e0cc9b162541", Speaker: "Conrad",
				Condition: "$conrad_jerked_off_3 $conrad_used_mount_3",
			}},
		},
		{
			name: "ファイル順を保つ（order 列では並べ替えない）",
			csv: header +
				"Unused,,Start,24,line:1772a124,cccccccccccccccc,Start,\n" +
				"Unused,,Start,7,line:1772a125,bbbbbbbbbbbbbbbb,Start,\n" +
				"Unused,,Start,11,line:1772a126,aaaaaaaaaaaaaaaa,Start,\n",
			want: []Entry{
				{Section: "Unused", HasSection: true, Node: "Start", HasNode: true, Order: 24, OrderText: "24", LineID: "line:1772a124", Key: "cccccccccccccccc", Speaker: "Start"},
				{Section: "Unused", HasSection: true, Node: "Start", HasNode: true, Order: 7, OrderText: "7", LineID: "line:1772a125", Key: "bbbbbbbbbbbbbbbb", Speaker: "Start"},
				{Section: "Unused", HasSection: true, Node: "Start", HasNode: true, Order: 11, OrderText: "11", LineID: "line:1772a126", Key: "aaaaaaaaaaaaaaaa", Speaker: "Start"},
			},
		},
		{
			name: "key が空の行は捨てる（R3）",
			csv: header +
				"L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf,,Ryan,\n" +
				"L01 Ryan,intro,Ryan_1_intro,2,line:c3d3da41,334d016f755cd6dc,Kobold,\n",
			want: []Entry{{
				Section: "L01 Ryan", HasSection: true, Phase: "intro", Node: "Ryan_1_intro", HasNode: true,
				Order: 2, OrderText: "2", LineID: "line:c3d3da41",
				Key: "334d016f755cd6dc", Speaker: "Kobold",
			}},
		},
		{
			name: "空白だけの key は捨てず Key が空文字になる（R3の Trim 前判定）",
			csv:  header + "L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf,   ,Ryan,\n",
			want: []Entry{{
				Section: "L01 Ryan", HasSection: true, Phase: "intro", Node: "Ryan_1_intro", HasNode: true,
				Order: 1, OrderText: "1", LineID: "line:a8779ebf",
				Key: "", Speaker: "Ryan",
			}},
		},
		{
			name: "key 列そのものが無ければ全行捨てる",
			csv:  "section,phase,node,order,line_id,speaker,condition\nL01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf,Ryan,\n",
			want: []Entry{},
		},
		{
			name: "key は Trim と小文字化を受ける（R4）",
			csv:  header + "L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf,  0DA72197E898EBE1 ,Ryan,\n",
			want: []Entry{{
				Section: "L01 Ryan", HasSection: true, Phase: "intro", Node: "Ryan_1_intro", HasNode: true,
				Order: 1, OrderText: "1", LineID: "line:a8779ebf",
				Key: "0da72197e898ebe1", Speaker: "Ryan",
			}},
		},
		{
			name: "key 以外は正規化しない（R4）",
			csv:  header + " L01 Ryan , INTRO , Ryan_1_intro ,1, LINE:A8779EBF ,0da72197e898ebe1, RYAN , $A \n",
			want: []Entry{{
				Section: " L01 Ryan ", HasSection: true, Phase: " INTRO ", Node: " Ryan_1_intro ", HasNode: true,
				Order: 1, OrderText: "1", LineID: " LINE:A8779EBF ",
				Key: "0da72197e898ebe1", Speaker: " RYAN ", Condition: " $A ",
			}},
		},
		{
			name: "列名の大文字小文字は無視される",
			csv:  "Section,Phase,Node,Order,Line_ID,KEY,Speaker,Condition\nL01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf,0da72197e898ebe1,Ryan,\n",
			want: []Entry{{
				Section: "L01 Ryan", HasSection: true, Phase: "intro", Node: "Ryan_1_intro", HasNode: true,
				Order: 1, OrderText: "1", LineID: "line:a8779ebf",
				Key: "0da72197e898ebe1", Speaker: "Ryan",
			}},
		},
		{
			name: "列が足りない行は空文字で埋まる",
			csv:  header + "Cutscene,,Intro_Cutscene,3,line:aaaa1111,0da72197e898ebe1\n",
			want: []Entry{{
				Section: "Cutscene", HasSection: true, Node: "Intro_Cutscene", HasNode: true,
				Order: 3, OrderText: "3", LineID: "line:aaaa1111",
				Key: "0da72197e898ebe1",
			}},
		},
		{
			name: "余った列は捨てる",
			csv:  header + "Reaction,,React,1,line:aaaa1111,0da72197e898ebe1,Ryan,,余り,もっと余り\n",
			want: []Entry{{
				Section: "Reaction", HasSection: true, Node: "React", HasNode: true,
				Order: 1, OrderText: "1", LineID: "line:aaaa1111",
				Key: "0da72197e898ebe1", Speaker: "Ryan",
			}},
		},
		{
			name: "データ行が無ければ0件",
			csv:  header,
			want: []Entry{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseEntries(rowsFromCSV(tt.csv))
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

// TestParseEntriesOrder は order 列の解析だけを取り出して確かめる。
// 元実装は int.TryParse の戻り値を捨てているので、読めない値は 0 になり、
// 行そのものは残る（移植仕様「スクリプト順 R5」と敵対検証[medium]）。
func TestParseEntriesOrder(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantOrder int
	}{
		{"通常の値", "12", 12},
		{"1始まりの先頭", "1", 1},
		{"空欄は0", "", 0},
		{"非数値は0", "abc", 0},
		{"小数は0", "1.5", 0},
		{"前後の空白は許す", "  7\t", 7},
		{"先頭の+符号は許す", "+7", 7},
		{"負値も読む", "-7", -7},
		{"int32を超える値は0（Atoiの巨大値を入れない）", "3000000000", 0},
		{"int64を超える値も0", "99999999999999999999", 0},
		{"16進表記は読まない", "0x10", 0},
		{"アンダースコア区切りは読まない", "1_0", 0},
		{"桁区切りのカンマは列が割れるので0", "\"1,000\"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			csv := "section,phase,node,order,line_id,key,speaker,condition\n" +
				"L01 Ryan,intro,Ryan_1_intro," + tt.raw + ",line:a8779ebf,0da72197e898ebe1,Ryan,\n"
			got := ParseEntries(rowsFromCSV(csv))
			if len(got) != 1 {
				t.Fatalf("件数 = %d, want 1", len(got))
			}
			if got[0].Order != tt.wantOrder {
				t.Errorf("Order = %d, want %d", got[0].Order, tt.wantOrder)
			}
			// OrderText は解析結果に関わらず生の値のまま。
			if got[0].Key != "0da72197e898ebe1" {
				t.Errorf("Key = %q, want 0da72197e898ebe1（行が捨てられていないこと）", got[0].Key)
			}
		})
	}
}

// TestParseEntriesOrderTextIsRaw は OrderText が生の値であることを確かめる。
// 公開CSVの order 列はこの値をそのまま書くため、10進へ直すと元実装と
// バイト一致しなくなる。
func TestParseEntriesOrderTextIsRaw(t *testing.T) {
	csv := "section,phase,node,order,line_id,key,speaker,condition\n" +
		"L01 Ryan,intro,Ryan_1_intro, 007 ,line:a8779ebf,0da72197e898ebe1,Ryan,\n"
	got := ParseEntries(rowsFromCSV(csv))
	if len(got) != 1 {
		t.Fatalf("件数 = %d, want 1", len(got))
	}
	if got[0].OrderText != " 007 " {
		t.Errorf("OrderText = %q, want %q", got[0].OrderText, " 007 ")
	}
	if got[0].Order != 7 {
		t.Errorf("Order = %d, want 7", got[0].Order)
	}
}

// TestParseEntriesLineKeyColumns は norm / fp / nlen 列の読み方を確かめる。
//
// internal/diff は nlen が linekey.MinFuzzyLength 以上の行だけを指紋で突き合わせる。
// 読める nlen を 0 に落とすと、言い回しが変わった台詞の訳が何も言わずに引き継がれ
// なくなる。読めない nlen を 0 にせず巨大値にすると、短い台詞まで突き合わせに入る。
// 値は internal/linekey の見本 "Wash the dragon, then rinse!" のもの。
func TestParseEntriesLineKeyColumns(t *testing.T) {
	const (
		header = "section,phase,node,order,line_id,key,speaker,condition,norm,fp,nlen\n"
		prefix = "L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf,0da72197e898ebe1,Ryan,,"
		norm   = "2b0163d969143127"
		fp     = "c291a81964e2e55a"
	)
	tests := []struct {
		name     string
		csv      string
		wantNorm string
		wantFP   string
		wantNLen int
	}{
		{"3列とも読む", header + prefix + norm + "," + fp + ",26\n", norm, fp, 26},
		{"nlen の前後の空白は許す", header + prefix + norm + "," + fp + ", 26\t\n", norm, fp, 26},
		{"nlen が空なら0", header + prefix + norm + "," + fp + ",\n", norm, fp, 0},
		{"nlen が数でなければ0", header + prefix + norm + "," + fp + ",abc\n", norm, fp, 0},
		{"nlen が小数なら0", header + prefix + norm + "," + fp + ",26.0\n", norm, fp, 0},
		{"nlen がint32を超えれば0", header + prefix + norm + "," + fp + ",3000000000\n", norm, fp, 0},
		{
			name:     "3列が無い版では空と0",
			csv:      "section,phase,node,order,line_id,key,speaker,condition\n" + prefix + "\n",
			wantNorm: "", wantFP: "", wantNLen: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseEntries(rowsFromCSV(tt.csv))
			if len(got) != 1 {
				t.Fatalf("件数 = %d, want 1（nlen が読めなくても行は残る）", len(got))
			}
			e := got[0]
			if e.Norm != tt.wantNorm || e.FP != tt.wantFP {
				t.Errorf("Norm, FP = %q, %q, want %q, %q", e.Norm, e.FP, tt.wantNorm, tt.wantFP)
			}
			if e.NLen != tt.wantNLen {
				t.Errorf("NLen = %d, want %d", e.NLen, tt.wantNLen)
			}
		})
	}
}
