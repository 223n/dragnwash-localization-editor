package order

import (
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
)

// TestParseAllEntriesKeepsEmptyKey は、公開CSV生成の経路が key 空行を残すことを
// 確かめる。元実装 tools/hash-strings.ps1 の `foreach ($e in $order)` には
// key 空行のスキップが無く、件数がそのまま $order.Count になる。
func TestParseAllEntriesKeepsEmptyKey(t *testing.T) {
	const csv = "section,phase,node,order,line_id,key,speaker,condition\n" +
		"Reaction,,N9,1,line:bbb1,,Ryan,\n" +
		"Reaction,,N9,2,line:bbb2,aaaaaaaaaaaaaaaa,Ryan,\n"

	rows, err := csvfile.ReadPowerShellRows([]byte(csv))
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}

	all := ParseAllEntries(rows)
	if len(all) != 2 {
		t.Fatalf("ParseAllEntries = %d件, want 2", len(all))
	}
	if all[0].Key != "" {
		t.Errorf("1件目の Key = %q, want 空文字", all[0].Key)
	}
	if all[0].LineID != "line:bbb1" {
		t.Errorf("1件目の LineID = %q", all[0].LineID)
	}

	// C# 側の規則では捨てられる。両方の読み方を1つにまとめてはいけない。
	if sharp := ParseEntries(rows); len(sharp) != 1 {
		t.Errorf("ParseEntries = %d件, want 1", len(sharp))
	}
}

// TestParseEntriesColumnPresence は、列の欠損と値が空文字であることを
// 区別して持つことを確かめる。見出しを出すかどうかがこの区別で決まる。
func TestParseEntriesColumnPresence(t *testing.T) {
	tests := []struct {
		name           string
		csv            string
		wantHasSection bool
		wantHasNode    bool
	}{
		{
			name:           "列があって値が空",
			csv:            "key,section,node,order\naaaaaaaaaaaaaaaa,,,1\n",
			wantHasSection: true,
			wantHasNode:    true,
		},
		{
			name:           "section と node の列そのものが無い",
			csv:            "key,order,line_id,speaker\naaaaaaaaaaaaaaaa,1,line:ccc,Ryan\n",
			wantHasSection: false,
			wantHasNode:    false,
		},
		{
			name:           "node の列だけ無い",
			csv:            "key,section,order\naaaaaaaaaaaaaaaa,Cutscene,1\n",
			wantHasSection: true,
			wantHasNode:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := csvfile.ReadPowerShellRows([]byte(tt.csv))
			if err != nil {
				t.Fatalf("読み込みに失敗した: %v", err)
			}
			entries := ParseAllEntries(rows)
			if len(entries) != 1 {
				t.Fatalf("Entries = %d件, want 1", len(entries))
			}
			if entries[0].HasSection != tt.wantHasSection {
				t.Errorf("HasSection = %v, want %v", entries[0].HasSection, tt.wantHasSection)
			}
			if entries[0].HasNode != tt.wantHasNode {
				t.Errorf("HasNode = %v, want %v", entries[0].HasNode, tt.wantHasNode)
			}
		})
	}
}
