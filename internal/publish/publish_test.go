package publish

import (
	"errors"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/order"
)

// orderHeader は script_order.csv のヘッダー行。テストの表を短く保つために切り出す。
const orderHeader = "section,phase,node,order,line_id,key,speaker,condition\n"

// flowHeader は level_flow.csv のヘッダー行。元ファイルには他にも列があるが、
// 読まれるのはこの5列だけ。
const flowHeader = "level,dragon,weather,set_flags,end_flags\n"

// testOrder は CSV のテキストから order.Data を作る。
func testOrder(t *testing.T, scriptOrderCSV, levelFlowCSV string) *order.Data {
	t.Helper()

	data, err := order.LoadPowerShell([]byte(scriptOrderCSV), []byte(levelFlowCSV))
	if err != nil {
		t.Fatalf("再生順データを読めない: %v", err)
	}
	return data
}

// publishedRows は組み立てた公開CSVを読み返す。見出し行と空行は落ちるので、
// 残るのはデータ行だけになる。エスケープの往復も同時に確かめられる。
func publishedRows(t *testing.T, out []byte) []csvfile.Row {
	t.Helper()

	rows, err := csvfile.ReadPowerShellRows(out)
	if err != nil {
		t.Fatalf("出力を読み返せない: %v", err)
	}
	return rows
}

func TestBuild(t *testing.T) {
	tests := []struct {
		name        string
		scriptOrder string
		levelFlow   string
		input       string
		existing    string
		want        string
		wantStats   Stats
	}{
		{
			name:        "再生順が空なら見出しも UI 表記も出ない",
			scriptOrder: "",
			input: "key,translation\n" +
				"0000000000000001,あ\n" +
				"0000000000000002,い\n",
			want: "key,section,node,order,speaker,translation\n" +
				"0000000000000001,,,,UI,あ\n" +
				"0000000000000002,,,,UI,い\n",
			wantStats: Stats{Kept: 2, Other: 2},
		},
		{
			name: "再生順どおりに見出し付きで並べる",
			scriptOrder: orderHeader +
				"S1,intro,N1,1,line:aa,0000000000000001,Ryan,\n" +
				"S1,intro,N1,2,line:bb,0000000000000002,Kobold,\n",
			input: "key,translation\n" +
				"0000000000000001,あ\n" +
				"0000000000000002,い\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- intro: N1 ---\n" +
				"0000000000000001,S1,N1,1,Ryan,あ\n" +
				"0000000000000002,S1,N1,2,Kobold,い\n",
			wantStats: Stats{Kept: 2, InPlayOrder: 2},
		},
		{
			name: "訳が空の行は出力しないがカウンタには入る",
			scriptOrder: orderHeader +
				"S1,intro,N1,1,,0000000000000001,Ryan,\n" +
				"S1,intro,N1,2,,0000000000000002,Ryan,\n",
			input: "key,translation\n" +
				"0000000000000001,あ\n" +
				"0000000000000002,\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- intro: N1 ---\n" +
				"0000000000000001,S1,N1,1,Ryan,あ\n",
			wantStats: Stats{Kept: 2, InPlayOrder: 1},
		},
		{
			name: "同じキーが2行あれば先勝ち",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,,\n",
			input: "key,translation\n" +
				"0000000000000001,さき\n" +
				"0000000000000001,あと\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- N1 ---\n" +
				"0000000000000001,S1,N1,1,,さき\n",
			wantStats: Stats{Kept: 2, InPlayOrder: 1},
		},
		{
			name: "訳が空の行はスロットを取らないので後の行が採用される",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,,\n",
			input: "key,translation\n" +
				"0000000000000001,\n" +
				"0000000000000001,あと\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- N1 ---\n" +
				"0000000000000001,S1,N1,1,,あと\n",
			wantStats: Stats{Kept: 2, InPlayOrder: 1},
		},
		{
			name: "該当行が無いセクションとノードの見出しは出ない",
			scriptOrder: orderHeader +
				"S1,intro,N1,1,,0000000000000001,Ryan,\n" +
				"S2,intro,N2,1,,0000000000000002,Ryan,\n" +
				"S2,intro,N3,1,,0000000000000003,Ryan,\n",
			input: "key,translation\n" +
				"0000000000000003,う\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S2 =====\n" +
				"# --- intro: N3 ---\n" +
				"0000000000000003,S2,N3,1,Ryan,う\n",
			wantStats: Stats{Kept: 1, InPlayOrder: 1},
		},
		{
			// 元実装の $lastNode 比較を seen 集合に置き換えると落ちるケース。
			// 実データにも同じ形が2件あり、間違えると全ロケールで2行ずれる。
			name: "同一セクション内でノードが再訪されると見出しがもう一度出る",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,,\n" +
				"S1,,N2,2,,0000000000000002,,\n" +
				"S1,,N1,3,,0000000000000003,,\n",
			input: "key,translation\n" +
				"0000000000000001,あ\n" +
				"0000000000000002,い\n" +
				"0000000000000003,う\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- N1 ---\n" +
				"0000000000000001,S1,N1,1,,あ\n" +
				"# --- N2 ---\n" +
				"0000000000000002,S1,N2,2,,い\n" +
				"# --- N1 ---\n" +
				"0000000000000003,S1,N1,3,,う\n",
			wantStats: Stats{Kept: 3, InPlayOrder: 3},
		},
		{
			// 直前に「出力した」行だけが $lastNode を動かす。訳の無いノードを
			// またいでも見出しは増えない。
			name: "1行も出ないノードを挟んでもノード見出しは再出力されない",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,,\n" +
				"S1,,N2,2,,0000000000000002,,\n" +
				"S1,,N1,3,,0000000000000003,,\n",
			input: "key,translation\n" +
				"0000000000000001,あ\n" +
				"0000000000000003,う\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- N1 ---\n" +
				"0000000000000001,S1,N1,1,,あ\n" +
				"0000000000000003,S1,N1,3,,う\n",
			wantStats: Stats{Kept: 2, InPlayOrder: 2},
		},
		{
			name: "セクションが変わると同名ノードでも見出しが出る",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,,\n" +
				"S2,,N1,2,,0000000000000002,,\n",
			input: "key,translation\n" +
				"0000000000000001,あ\n" +
				"0000000000000002,い\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- N1 ---\n" +
				"0000000000000001,S1,N1,1,,あ\n" +
				"\n" +
				"# ===== S2 =====\n" +
				"# --- N1 ---\n" +
				"0000000000000002,S2,N1,2,,い\n",
			wantStats: Stats{Kept: 2, InPlayOrder: 2},
		},
		{
			name: "再生順に同じキーが2度出てもハッシュ行は最初の位置に1回だけ",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,,\n" +
				"S1,,N2,2,,0000000000000001,,\n",
			input: "key,translation\n" +
				"0000000000000001,あ\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- N1 ---\n" +
				"0000000000000001,S1,N1,1,,あ\n",
			wantStats: Stats{Kept: 1, InPlayOrder: 1},
		},
		{
			name: "ハッシュ行と台詞ID行が同じ位置に来るとハッシュ行が先",
			scriptOrder: orderHeader +
				"S1,,N1,1,line:aa,0000000000000001,Ryan,\n",
			input: "key,translation\n" +
				"0000000000000001,あ\n" +
				"line:aa,ラインの訳\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- N1 ---\n" +
				"0000000000000001,S1,N1,1,Ryan,あ\n" +
				"line:aa,S1,N1,1,Ryan,ラインの訳\n",
			wantStats: Stats{Kept: 1, LineKept: 1, InPlayOrder: 1},
		},
		{
			name: "台詞ID行だけでもセクションとノードの見出しは出る",
			scriptOrder: orderHeader +
				"S1,,N1,1,line:aa,0000000000000001,Ryan,\n",
			input: "key,translation\n" +
				"line:aa,ラインの訳\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- N1 ---\n" +
				"line:aa,S1,N1,1,Ryan,ラインの訳\n",
			wantStats: Stats{LineKept: 1, InPlayOrder: 0},
		},
		{
			name: "台詞ID行は訳が空なら捨て、重複は先勝ち",
			scriptOrder: orderHeader +
				"S1,,N1,1,line:aa,0000000000000001,Ryan,\n",
			input: "key,translation\n" +
				"line:aa,\n" +
				"line:aa,あと\n" +
				"line:aa,さらにあと\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- N1 ---\n" +
				"line:aa,S1,N1,1,Ryan,あと\n",
			wantStats: Stats{LineKept: 1},
		},
		{
			name: "再生順に無い台詞ID行は末尾の専用見出しの下に出る",
			scriptOrder: orderHeader +
				"S1,,N1,1,line:aa,0000000000000001,Ryan,\n",
			input: "key,translation\n" +
				"line:zz,みなしご\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== Per-line translations not found in the script order =====\n" +
				"line:zz,,,,,みなしご\n",
			wantStats: Stats{LineKept: 1},
		},
		{
			// R26 の見出しだけは再生順の有無に依存しない。
			name:        "再生順が空でも台詞ID行の見出しは出る",
			scriptOrder: "",
			input: "key,translation\n" +
				"line:zz,みなしご\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== Per-line translations not found in the script order =====\n" +
				"line:zz,,,,,みなしご\n",
			wantStats: Stats{LineKept: 1},
		},
		{
			name: "再生順に無いキーは UI 見出しの下に入力順で出る",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,Ryan,\n",
			input: "key,speaker,translation\n" +
				"0000000000000009,Menu,設定\n" +
				"0000000000000001,,あ\n" +
				"000000000000000a,,決定\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- N1 ---\n" +
				"0000000000000001,S1,N1,1,Ryan,あ\n" +
				"\n" +
				"# ===== UI and other text (not part of the dialogue script) =====\n" +
				"0000000000000009,UI,,,Menu,設定\n" +
				"000000000000000a,UI,,,UI,決定\n",
			wantStats: Stats{Kept: 3, InPlayOrder: 1, Other: 2},
		},
		{
			name: "レベル見出しと固定見出し",
			scriptOrder: orderHeader +
				"L01 Ryan,intro,N1,1,,0000000000000001,,\n" +
				"Cutscene,,N2,2,,0000000000000002,,\n",
			levelFlow: flowHeader +
				"0,Ryan,Sunny,level_1,MedkitCompleted | level_1_complete\n",
			input: "key,translation\n" +
				"0000000000000001,あ\n" +
				"0000000000000002,い\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== Level 1: Ryan (Sunny) | sets level_1 | ends MedkitCompleted, level_1_complete =====\n" +
				"# --- intro: N1 ---\n" +
				"0000000000000001,L01 Ryan,N1,1,,あ\n" +
				"\n" +
				"# ===== Cutscenes (started by game code) =====\n" +
				"# --- N2 ---\n" +
				"0000000000000002,Cutscene,N2,2,,い\n",
			wantStats: Stats{Kept: 2, InPlayOrder: 2},
		},
		{
			name: "condition はノード見出しに後置される",
			scriptOrder: orderHeader +
				"S1,cum,N1,1,,0000000000000001,,$a $b\n",
			input: "key,translation\n" +
				"0000000000000001,あ\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- cum: N1 | if $a $b ---\n" +
				"0000000000000001,S1,N1,1,,あ\n",
			wantStats: Stats{Kept: 1, InPlayOrder: 1},
		},
		{
			// order 列は10進に直さず生の綴りのまま書く。書き戻すと "007" が "7" に化ける。
			name: "order 列は生の綴りのまま書く",
			scriptOrder: orderHeader +
				"S1,,N1,007,,0000000000000001,,\n",
			input: "key,translation\n" +
				"0000000000000001,あ\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- N1 ---\n" +
				"0000000000000001,S1,N1,007,,あ\n",
			wantStats: Stats{Kept: 1, InPlayOrder: 1},
		},
		{
			name: "カンマと引用符を含む値は引用して書く",
			scriptOrder: orderHeader +
				`"S,1",,"N""1",1,,0000000000000001,,` + "\n",
			input: "key,translation\n" +
				`0000000000000001,"あ,""い"""` + "\n",
			want: "key,section,node,order,speaker,translation\n" +
				"\n" +
				"# ===== S,1 =====\n" +
				`# --- N"1 ---` + "\n" +
				`0000000000000001,"S,1","N""1",1,,"あ,""い"""` + "\n",
			wantStats: Stats{Kept: 1, InPlayOrder: 1},
		},
		{
			name: "既存の公開ファイルからヘッダー直下のコメントを引き継ぐ",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,Ryan,\n",
			input: "key,translation\n" +
				"0000000000000001,あ\n",
			existing: "key,section,node,order,speaker,translation\n" +
				"# Language: 日本語 (ja)\n" +
				"# 暫定訳です\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- N1 ---\n" +
				"0000000000000001,S1,N1,1,Ryan,あ\n",
			want: "key,section,node,order,speaker,translation\n" +
				"# Language: 日本語 (ja)\n" +
				"# 暫定訳です\n" +
				"\n" +
				"# ===== S1 =====\n" +
				"# --- N1 ---\n" +
				"0000000000000001,S1,N1,1,Ryan,あ\n",
			wantStats: Stats{Kept: 1, InPlayOrder: 1},
		},
		{
			name:        "入力が空なら固定ヘッダーだけになる",
			scriptOrder: orderHeader + "S1,,N1,1,,0000000000000001,,\n",
			input:       "",
			want:        "key,section,node,order,speaker,translation\n",
			wantStats:   Stats{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := testOrder(t, tt.scriptOrder, tt.levelFlow)

			got, stats, err := Build(data, []byte(tt.input), []byte(tt.existing))
			if err != nil {
				t.Fatalf("Build がエラーを返した: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("出力が違う\n--- got ---\n%s\n--- want ---\n%s", got, tt.want)
			}
			if stats != tt.wantStats {
				t.Errorf("集計が違う\ngot  %+v\nwant %+v", stats, tt.wantStats)
			}
		})
	}
}

// TestBuildHashing は source_en からキーを作る経路（R15〜R17）を確かめる。
// 期待するキーはテスト内で key.For を使って求める。
func TestBuildHashing(t *testing.T) {
	const src = "Hello"
	hashed := key.For(src)

	tests := []struct {
		name      string
		input     string
		wantKeys  []string
		wantStats Stats
	}{
		{
			name:      "source_en だけの行はハッシュをキーにする",
			input:     "source_en,translation\n" + src + ",やあ\n",
			wantKeys:  []string{hashed},
			wantStats: Stats{Converted: 1, Other: 1},
		},
		{
			name:      "key がハッシュと一致する行は通る",
			input:     "key,source_en,translation\n" + hashed + "," + src + ",やあ\n",
			wantKeys:  []string{hashed},
			wantStats: Stats{Converted: 1, Other: 1},
		},
		{
			name:      "key がハッシュと食い違う行は捨てる",
			input:     "key,source_en,translation\n0000000000000001," + src + ",やあ\n",
			wantKeys:  nil,
			wantStats: Stats{Dropped: 1},
		},
		{
			name:      "key が空なら食い違い判定を通さずハッシュを採用する",
			input:     "key,source_en,translation\n," + src + ",やあ\n",
			wantKeys:  []string{hashed},
			wantStats: Stats{Converted: 1, Other: 1},
		},
		{
			name:      "source_en が空で16桁キーなら素通し",
			input:     "key,source_en,translation\n0000000000000001,,あ\n",
			wantKeys:  []string{"0000000000000001"},
			wantStats: Stats{Kept: 1, Other: 1},
		},
		{
			name:      "大文字の16進キーは小文字に直して素通し",
			input:     "key,translation\nAABBCCDDEEFF0011,あ\n",
			wantKeys:  []string{"aabbccddeeff0011"},
			wantStats: Stats{Kept: 1, Other: 1},
		},
		{
			name:      "key の前後の空白はトリムされる",
			input:     "key,translation\n  0000000000000001  ,あ\n",
			wantKeys:  []string{"0000000000000001"},
			wantStats: Stats{Kept: 1, Other: 1},
		},
		{
			name:      "16桁でないキーで source_en も無い行は捨てる",
			input:     "key,translation\nnot-a-key,あ\n",
			wantKeys:  nil,
			wantStats: Stats{Dropped: 1},
		},
		{
			// 大文字の LINE: は台詞IDと見なされない（-cmatch）。16桁16進でもないので捨てる。
			name:      "大文字の LINE: は台詞IDではなく捨てられる",
			input:     "key,translation\nLINE:abc,あ\n",
			wantKeys:  nil,
			wantStats: Stats{Dropped: 1},
		},
		{
			name:      "key 列そのものが無くても source_en があれば通る",
			input:     "source_en,translation\n" + src + ",やあ\n",
			wantKeys:  []string{hashed},
			wantStats: Stats{Converted: 1, Other: 1},
		},
		{
			// 空文字を key.For に渡すと SHA-256("") のキーが生まれてしまう。
			// source_en が空の行はハッシュ経路へ進まないことを固定する。
			name:      "source_en が空の行は空文字のハッシュを作らない",
			input:     "key,source_en,translation\n,,あ\n",
			wantKeys:  nil,
			wantStats: Stats{Dropped: 1},
		},
		{
			// 空白は引用して渡す。引用しない先頭空白は ConvertFrom-Csv 側で
			// 落ちてしまい、ハッシュまで届かない（csvfile.ParsePowerShellRecord）。
			name:      "source_en はトリムしないので前後の空白でキーが変わる",
			input:     "source_en,translation\n" + src + ",やあ\n\" " + src + "\",やあ2\n",
			wantKeys:  []string{hashed, key.For(" " + src)},
			wantStats: Stats{Converted: 2, Other: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, stats, err := Build(nil, []byte(tt.input), nil)
			if err != nil {
				t.Fatalf("Build がエラーを返した: %v", err)
			}
			if stats != tt.wantStats {
				t.Errorf("集計が違う\ngot  %+v\nwant %+v", stats, tt.wantStats)
			}

			rows := publishedRows(t, got)
			var keys []string
			for _, row := range rows {
				keys = append(keys, row.Get("key"))
			}
			if strings.Join(keys, ",") != strings.Join(tt.wantKeys, ",") {
				t.Errorf("出力されたキーが違う\ngot  %v\nwant %v", keys, tt.wantKeys)
			}
			if got := string(got); strings.Contains(got, key.HashOfEmpty) && len(tt.wantKeys) == 0 {
				t.Errorf("空文字のハッシュが紛れ込んでいる:\n%s", got)
			}
		})
	}
}

// TestBuildSpeaker は speaker 列の決定順（R24e）を確かめる。
func TestBuildSpeaker(t *testing.T) {
	tests := []struct {
		name        string
		scriptOrder string
		input       string
		want        string
	}{
		{
			name: "再生順に話者がいればそれを使う",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,Ryan,\n",
			input: "key,speaker,translation\n0000000000000001,入力の話者,あ\n",
			want:  "Ryan",
		},
		{
			name: "同じキーに複数の話者がいればスラッシュで連結する",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,Ryan,\n" +
				"S1,,N2,2,,0000000000000001,Alexander,\n",
			input: "key,speaker,translation\n0000000000000001,入力の話者,あ\n",
			want:  "Ryan/Alexander",
		},
		{
			name: "同じ話者が何度出ても1回だけ数える",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,Ryan,\n" +
				"S1,,N2,2,,0000000000000001,Ryan,\n",
			input: "key,speaker,translation\n0000000000000001,,あ\n",
			want:  "Ryan",
		},
		{
			name: "再生順に話者がいなければ入力の話者を使う",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,,\n",
			input: "key,speaker,translation\n0000000000000001,入力の話者,あ\n",
			want:  "入力の話者",
		},
		{
			name: "どちらも空ならその行の話者（＝空）になる",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,,\n",
			input: "key,speaker,translation\n0000000000000001,,あ\n",
			want:  "",
		},
		{
			// 話者表は「そのキーのどこか1行でも話者が非空なら」載る。
			// 載っていれば、いま見ている行の話者が空でも表の値が勝つ。
			name: "別の行に話者があればその行の話者が空でも表の値が勝つ",
			scriptOrder: orderHeader +
				"S1,,N1,1,,0000000000000001,,\n" +
				"S1,,N2,2,,0000000000000001,Ryan,\n",
			input: "key,speaker,translation\n0000000000000001,入力の話者,あ\n",
			want:  "Ryan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := testOrder(t, tt.scriptOrder, "")

			got, _, err := Build(data, []byte(tt.input), nil)
			if err != nil {
				t.Fatalf("Build がエラーを返した: %v", err)
			}
			rows := publishedRows(t, got)
			if len(rows) != 1 {
				t.Fatalf("出力行が1行でない: %d行\n%s", len(rows), got)
			}
			if speaker := rows[0].Get("speaker"); speaker != tt.want {
				t.Errorf("speaker が違う: got %q, want %q", speaker, tt.want)
			}
		})
	}
}

// TestBuildLineIDSpeaker は台詞ID行の speaker が入力ではなく再生順の値になることを
// 確かめる（R24g）。
func TestBuildLineIDSpeaker(t *testing.T) {
	data := testOrder(t, orderHeader+
		"S1,,N1,1,line:aa,0000000000000001,Ryan,\n"+
		"S1,,N2,2,line:bb,0000000000000001,Alexander,\n", "")

	input := "key,speaker,translation\n" +
		"0000000000000001,入力の話者,共通の訳\n" +
		"line:aa,入力の話者,Ryan の訳\n" +
		"line:bb,入力の話者,Alexander の訳\n"

	got, stats, err := Build(data, []byte(input), nil)
	if err != nil {
		t.Fatalf("Build がエラーを返した: %v", err)
	}

	want := "key,section,node,order,speaker,translation\n" +
		"\n" +
		"# ===== S1 =====\n" +
		"# --- N1 ---\n" +
		"0000000000000001,S1,N1,1,Ryan/Alexander,共通の訳\n" +
		"line:aa,S1,N1,1,Ryan,Ryan の訳\n" +
		"# --- N2 ---\n" +
		"line:bb,S1,N2,2,Alexander,Alexander の訳\n"
	if string(got) != want {
		t.Errorf("出力が違う\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if wantStats := (Stats{Kept: 1, LineKept: 2, InPlayOrder: 1}); stats != wantStats {
		t.Errorf("集計が違う\ngot  %+v\nwant %+v", stats, wantStats)
	}
}

// TestBuildLineIDCaseInsensitive は台詞IDの照合が大文字小文字を区別しないことを
// 確かめる（[ordered]@{} の既定）。接頭辞は小文字に固定されているので、
// 差が出るのはコロンより後ろだけ。
func TestBuildLineIDCaseInsensitive(t *testing.T) {
	data := testOrder(t, orderHeader+"S1,,N1,1,line:AA,0000000000000001,Ryan,\n", "")

	got, stats, err := Build(data, []byte("key,translation\nline:aa,訳\n"), nil)
	if err != nil {
		t.Fatalf("Build がエラーを返した: %v", err)
	}

	// 出力される台詞IDは再生順側の綴り。
	want := "key,section,node,order,speaker,translation\n" +
		"\n" +
		"# ===== S1 =====\n" +
		"# --- N1 ---\n" +
		"line:AA,S1,N1,1,Ryan,訳\n"
	if string(got) != want {
		t.Errorf("出力が違う\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if stats.LineKept != 1 {
		t.Errorf("LineKept が違う: got %d, want 1", stats.LineKept)
	}
}

// TestBuildDuplicateColumn は入力のヘッダー列名が重複しているとエラーになることを
// 確かめる。元実装では ConvertFrom-Csv の例外で処理全体が止まる。
func TestBuildDuplicateColumn(t *testing.T) {
	_, _, err := Build(nil, []byte("key,Key\n0000000000000001,x\n"), nil)
	if err == nil {
		t.Fatal("重複列でエラーにならなかった")
	}
	var dup *csvfile.DuplicateColumnError
	if !errors.As(err, &dup) {
		t.Fatalf("DuplicateColumnError でない: %v", err)
	}
}

// TestBuildIdempotent は「出力をそのまま入力に戻すと同じ出力になる」ことを確かめる。
// 公開ファイルの再生成が冪等であることが移植の到達目標なので、合成データでも固定する。
func TestBuildIdempotent(t *testing.T) {
	data := testOrder(t, orderHeader+
		"L01 Ryan,intro,N1,1,line:aa,0000000000000001,Ryan,\n"+
		"L01 Ryan,intro,N1,2,line:bb,0000000000000002,Kobold,\n"+
		"Cutscene,,N2,1,line:cc,0000000000000003,,\n",
		flowHeader+"0,Ryan,Sunny,level_1,level_1_complete\n")

	input := "key,section,node,order,speaker,translation\n" +
		"# Language: 日本語 (ja)\n" +
		"0000000000000001,L01 Ryan,N1,1,Ryan,あ\n" +
		"line:aa,L01 Ryan,N1,1,Ryan,ラインの訳\n" +
		"0000000000000002,L01 Ryan,N1,2,Kobold,い\n" +
		"0000000000000003,Cutscene,N2,1,,う\n" +
		"0000000000000009,UI,,,UI,設定\n" +
		"line:zz,,,,,みなしご\n"

	first, _, err := Build(data, []byte(input), []byte(input))
	if err != nil {
		t.Fatalf("1回目の Build がエラーを返した: %v", err)
	}
	second, _, err := Build(data, first, first)
	if err != nil {
		t.Fatalf("2回目の Build がエラーを返した: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("再生成で結果が変わった\n--- 1回目 ---\n%s\n--- 2回目 ---\n%s", first, second)
	}
}

func TestHeaderComments(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		want     []string
	}{
		{name: "空", existing: "", want: nil},
		{name: "ヘッダーだけ", existing: "key,section\n", want: nil},
		{
			name:     "2行目が空行なら何も引き継がない",
			existing: "key,section\n\n# ===== S1 =====\n",
			want:     nil,
		},
		{
			name: "コメントを空行まで引き継ぐ",
			existing: "key,section\n" +
				"# Language: Deutsch (de)\n" +
				"# provisional\n" +
				"\n" +
				"# ===== S1 =====\n",
			want: []string{"# Language: Deutsch (de)", "# provisional"},
		},
		{
			name: "セクション見出しで打ち切る",
			existing: "key,section\n" +
				"# Language: ja\n" +
				"# ===== S1 =====\n" +
				"# あとのコメント\n",
			want: []string{"# Language: ja"},
		},
		{
			name: "ノード見出しで打ち切る",
			existing: "key,section\n" +
				"# Language: ja\n" +
				"# --- N1 ---\n",
			want: []string{"# Language: ja"},
		},
		{
			name: "コメントでない行で打ち切る",
			existing: "key,section\n" +
				"# Language: ja\n" +
				"0000000000000001,S1\n" +
				"# あとのコメント\n",
			want: []string{"# Language: ja"},
		},
		{
			// 判定はトリムしないので、先頭に空白があるとコメントと見なされず打ち切る。
			name:     "行頭に空白があるコメントは引き継がない",
			existing: "key,section\n # Language: ja\n",
			want:     nil,
		},
		{
			name:     "BOM 付きでもヘッダーを正しく飛ばす",
			existing: "\xef\xbb\xbfkey,section\n# Language: ja\n\n",
			want:     []string{"# Language: ja"},
		},
		{
			name:     "CRLF 終端でも改行は含まない",
			existing: "key,section\r\n# Language: ja\r\n\r\n",
			want:     []string{"# Language: ja"},
		},
		{
			name:     "末尾に改行が無くても読める",
			existing: "key,section\n# Language: ja",
			want:     []string{"# Language: ja"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HeaderComments([]byte(tt.existing))
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("引き継いだコメントが違う\ngot  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestStatsLogLine(t *testing.T) {
	s := Stats{Converted: 1, Kept: 2, LineKept: 3, Dropped: 4, InPlayOrder: 5, Other: 6}
	want := "out.csv <- in.csv: 1 converted, 2 already hashed, 3 per-line, 4 malformed dropped, 5 in play order, 6 other"
	if got := s.LogLine("out.csv", "in.csv"); got != want {
		t.Errorf("ログ行が違う\ngot  %s\nwant %s", got, want)
	}
}

func TestNodeTitle(t *testing.T) {
	tests := []struct {
		name  string
		entry order.Entry
		want  string
	}{
		{name: "ノード名だけ", entry: order.Entry{Node: "N1"}, want: "N1"},
		{name: "phase 付き", entry: order.Entry{Phase: "intro", Node: "N1"}, want: "intro: N1"},
		{name: "condition 付き", entry: order.Entry{Node: "N1", Condition: "$a"}, want: "N1 | if $a"},
		{
			name:  "phase と condition の両方",
			entry: order.Entry{Phase: "cum", Node: "N1", Condition: "$a $b"},
			want:  "cum: N1 | if $a $b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nodeTitle(tt.entry); got != tt.want {
				t.Errorf("ノード見出しが違う: got %q, want %q", got, tt.want)
			}
		})
	}
}
