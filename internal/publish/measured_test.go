package publish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildAgainstMeasuredPowerShell は、pwsh 7.6.6 で tools/hash-strings.ps1 を
// 実際に走らせて得た出力を期待値として固定する。
//
// どの入力も現行の実データには現れないが、いずれも元実装との食い違いが実測で
// 確認できた形で、素直に書くと逆の結果になる。改行は元実装が Environment.NewLine を
// 使うため Windows では CRLF になる。ここでは LF に直したものを置いてある
// （移植仕様 R27 と csvfile.LineTerminator の選択）。
func TestBuildAgainstMeasuredPowerShell(t *testing.T) {
	tests := []struct {
		name        string
		scriptOrder string
		levelFlow   string
		input       string
		want        string
		wantStats   Stats
	}{
		{
			// 再生順の key 列が空でも行は捨てない。捨てると台詞ID訳が
			// 末尾の孤児ブロックへ落ち、section/node/order/speaker が空になる。
			name:        "key が空で line_id を持つ再生順の行にも台詞ID訳を置く",
			scriptOrder: orderHeader + "Reaction,,N9,1,line:bbb1,,Ryan,\n",
			input:       "key,translation\nline:bbb1,emptykey-line\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== Dragon reactions (started by game code) =====\n" +
				"# --- N9 ---\n" +
				"line:bbb1,Reaction,N9,1,Ryan,emptykey-line\n",
			wantStats: Stats{LineKept: 1},
		},
		{
			// UI 見出しと section='UI' は「再生順にレコードが1件以上あるか」に
			// かかる。key が全部空でもレコードはあるので、どちらも出る。
			name:        "再生順の key が全て空でも UI 見出しは出る",
			scriptOrder: orderHeader + "Reaction,,N9,1,line:zzz,,Ryan,\n",
			input:       "key,translation\n9999999999999999,ui-only\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== UI and other text (not part of the dialogue script) =====\n" +
				"9999999999999999,UI,,,UI,ui-only\n",
			wantStats: Stats{Kept: 1, Other: 1},
		},
		{
			// section / node 列そのものが無いと、プロパティは $null になる。
			// $lastSection の初期値も $null なので、先頭行でも見出しが出ない。
			name:        "section と node の列が無い再生順では見出しが1行も出ない",
			scriptOrder: "key,order,line_id,speaker\n9999999999999999,1,line:ccc,Ryan\n",
			input:       "key,translation\n9999999999999999,has-row\n",
			want: "key,section,node,order,speaker,translation\n" +
				"9999999999999999,,,1,Ryan,has-row\n",
			wantStats: Stats{Kept: 1, InPlayOrder: 1},
		},
		{
			// [int]'' は 0。捨てるとレベル1の見出しが Sunny 側で残ってしまう。
			name:        "level が空欄の行も Index 0 として後勝ちする",
			scriptOrder: orderHeader + "L01 Ryan,intro,N1,1,line:eee,9999999999999999,Ryan,\n",
			levelFlow:   flowHeader + "0,Ryan,Sunny,,\n,Ryan,Rainy,,\n",
			input:       "key,translation\n9999999999999999,lvl\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== Level 1: Ryan (Rainy) =====\n" +
				"# --- intro: N1 ---\n" +
				"9999999999999999,L01 Ryan,N1,1,Ryan,lvl\n",
			wantStats: Stats{Kept: 1, InPlayOrder: 1},
		},
		{
			// 1行目が空白だけでも、ヘッダーは次の行になる。上流の hash-strings.ps1 は
			// 55d2e09 / c8fda90（Remove-NonRecords）でヘッダーより上の空白だけの行を
			// 落とすようになった。上流 main で同じ形の入力（上流の報告 #8、
			// p-ws-above-header）を走らせると、行は残って「1 already hashed」になる。
			//
			// 003ed1e の ConvertFrom-Csv はこの行をヘッダーにし、列が0個になるので
			// 後続の2行が malformed 扱いで捨てられていた（公開ファイルがヘッダーだけに
			// なる）。この移植も以前はそれを写していた。
			name:        "入力の1行目が空白だけでもヘッダーは次の行になる",
			scriptOrder: orderHeader + "Reaction,,N9,1,line:ddd,9999999999999999,Ryan,\n",
			input:       "   \nkey,translation\n9999999999999999,blank-first\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== Dragon reactions (started by game code) =====\n" +
				"# --- N9 ---\n" +
				"9999999999999999,Reaction,N9,1,Ryan,blank-first\n",
			wantStats: Stats{Kept: 1, InPlayOrder: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := testOrder(t, tt.scriptOrder, tt.levelFlow)
			got, stats, err := Build(data, []byte(tt.input), nil)
			if err != nil {
				t.Fatalf("Build が失敗した: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("出力が違う\ngot:\n%s\nwant:\n%s", string(got), tt.want)
			}
			if stats != tt.wantStats {
				t.Errorf("stats = %+v, want %+v", stats, tt.wantStats)
			}
		})
	}
}

// TestWriteBytesIsAtomic は書き出しが一時ファイル経由であることを確かめる。
// 失敗しても書きかけのファイルを残さないための仕掛けで、既定では入力と出力が
// 同じファイルなので、ここが壊れると原本を失う。
func TestWriteBytesIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "strings.csv")
	writeFile(t, path, "古い内容\n")

	if err := WriteBytes(path, []byte("新しい内容\n")); err != nil {
		t.Fatalf("WriteBytes が失敗した: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("読み返せない: %v", err)
	}
	if string(got) != "新しい内容\n" {
		t.Errorf("中身 = %q, want %q", got, "新しい内容\n")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ディレクトリを読めない: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("一時ファイルが残っている: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("ファイル数 = %d, want 1", len(entries))
	}
}

// TestWriteBytesKeepsOriginalOnFailure は、書けないときに元のファイルを
// 壊さないことを確かめる。出力先がディレクトリなら rename は必ず失敗する。
func TestWriteBytesKeepsOriginalOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "strings.csv")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("準備に失敗した: %v", err)
	}
	inner := filepath.Join(target, "keep.txt")
	writeFile(t, inner, "残っていてほしい\n")

	if err := WriteBytes(target, []byte("新しい内容\n")); err == nil {
		t.Fatal("エラーにならなかった")
	}

	got, err := os.ReadFile(inner)
	if err != nil {
		t.Fatalf("中のファイルが失われた: %v", err)
	}
	if string(got) != "残っていてほしい\n" {
		t.Errorf("中身 = %q", got)
	}
}
