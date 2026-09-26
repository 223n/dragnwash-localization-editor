package edit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestEditWritesTheUpstreamCases は、上流との突き合わせの入力の表
// （testdata/upstream/cases.json）の ml-edit-* の入力が、画面の保存（SetTranslation）が
// 訳に改行を入れて書く形そのものであることを見る。
//
// 表の入力は、上流の tools/hash-strings.ps1（pwsh 7.4.6 と 7.6.6）と dwloc publish に
// 読ませ、公開ファイルがバイト一致することを確かめてある（cmd/dwloc の
// publish_upstream_test）。入力を手で組むと、画面が書く形とずれても気づけないので、
// 訳の空いた（または1行の）ファイルに SetTranslation で書いた結果と比べる。
func TestEditWritesTheUpstreamCases(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "upstream", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []struct {
			Name string `json:"name"`
			Text string `json:"text"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	texts := make(map[string]string)
	for _, c := range table.Cases {
		texts[c.Name] = c.Text
	}

	type write struct {
		id    int
		value string
	}
	tests := []struct {
		name   string
		before string
		writes []write
	}{
		{
			name: "ml-edit-working-crlf",
			before: "key,section,node,order,speaker,source_en,translation\r\n" +
				"7692c3ad3540bb80,L01 Ember,Ember_1_intro,1,Ember,one,\r\n" +
				"3fc4ccfe745870e2,L01 Ember,Ember_1_intro,2,Moss,two,に\r\n" +
				"8b5b9db0c13db242,L01 Ember,Ember_1_outro,3,Ember,three,\r\n" +
				",UI,,,UI,Four.,\r\n",
			// 画面から届く値は LF にそろえてあるが、ここでは CRLF と単独の CR も混ぜて、
			// SetTranslation が LF にそろえて書くことも一緒に見る。
			writes: []write{
				{2, "いち\r\n"},
				{3, "\nに"},
				{4, "「さ\"ん」、\r  よん, ご  \n\"ろく\""},
				{5, "し\n# ご\n\nはち"},
			},
		},
		{
			name: "ml-edit-published-lf",
			before: "key,section,node,order,speaker,translation\n" +
				"7692c3ad3540bb80,L01 Ember,Ember_1_intro,1,Ember,いち\n" +
				"3fc4ccfe745870e2,L01 Ember,Ember_1_intro,2,Moss,に\n",
			writes: []write{
				{2, "いち\nに"},
				{3, "\n\nに,さん\n"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want, ok := texts[tt.name]
			if !ok {
				t.Fatalf("入力の表に %s が無い", tt.name)
			}
			path := writeTemp(t, tt.before)
			f, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.writes {
				if err := f.SetTranslation(w.id, w.value); err != nil {
					t.Fatalf("ID %d に書けない: %v", w.id, err)
				}
			}
			if err := f.Save(); err != nil {
				t.Fatalf("保存に失敗した: %v", err)
			}
			if got := readFile(t, path); got != want {
				t.Errorf("画面が書く形と入力の表が違う\n got %q\nwant %q", got, want)
			}
			// 書いたファイルを読み直しても、どの行も編集できる（読み取り専用の理由に当たらない）。
			for _, l := range Parse([]byte(want)).Lines() {
				if l.Kind == KindData && !l.Editable {
					t.Errorf("ID %d が編集できない: %s", l.ID, l.Reason)
				}
			}
		})
	}
}
