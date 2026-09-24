package order

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
)

// testLevelsCSV は実データの level_flow.csv を縮めたもの。列の並びも実ファイルに合わせてある。
const testLevelsCSV = "flow_asset,level,dragon,spawn_flag,set_flags,end_flags,weather\n" +
	"LevelFlow,0,Ryan,,level_1,level_1_complete,Sunny\n" +
	"LevelFlow,1,Conrad,,,,Sunny\n" +
	"LevelFlow,4,Conrad,,level_5,MedkitCompleted | level_5_complete,Rainy\n"

func TestSectionTitle(t *testing.T) {
	data := &Data{Levels: ParseLevels(rowsFromCSV(testLevelsCSV))}

	tests := []struct {
		name    string
		section string
		want    string
	}{
		{"Cutscene は固定文言", "Cutscene", "Cutscenes (started by game code)"},
		{"Reaction は固定文言", "Reaction", "Dragon reactions (started by game code)"},
		{"Unused は固定文言", "Unused", "Unused nodes (not reachable in the current game)"},
		{"固定文言の判定は大小を無視する", "CUTSCENE", "Cutscenes (started by game code)"},
		{"対応表にあれば見出しを返す", "L01 Ryan", "Level 1: Ryan (Sunny) | sets level_1 | ends level_1_complete"},
		{"対応表（天気だけ）", "L02 Conrad", "Level 2: Conrad (Sunny)"},
		{"対応表（level 4 = L05）", "L05 Conrad", "Level 5: Conrad (Rainy) | sets level_5 | ends MedkitCompleted, level_5_complete"},
		{"対応表は大小がずれても引ける", "l05 conrad", "Level 5: Conrad (Rainy) | sets level_5 | ends MedkitCompleted, level_5_complete"},
		{"対応表に無ければセクション名そのまま", "L03 Alexander", "L03 Alexander"},
		{"空白のずれは吸収しない", "L05  Conrad", "L05  Conrad"},
		{"未知のセクションはそのまま", "なにか", "なにか"},
		{"空文字は空文字", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := data.SectionTitle(tt.section); got != tt.want {
				t.Errorf("SectionTitle(%q) = %q, want %q", tt.section, got, tt.want)
			}
		})
	}
}

// TestSectionTitleWithoutLevels は level_flow.csv が無いときの挙動を確かめる。
// 固定文言だけが残り、レベルのセクションはセクション名そのままになる
// （移植仕様「スクリプト順 R7」）。
func TestSectionTitleWithoutLevels(t *testing.T) {
	data := &Data{}
	tests := []struct {
		section string
		want    string
	}{
		{"Cutscene", "Cutscenes (started by game code)"},
		{"L01 Ryan", "L01 Ryan"},
	}
	for _, tt := range tests {
		t.Run(tt.section, func(t *testing.T) {
			if got := data.SectionTitle(tt.section); got != tt.want {
				t.Errorf("SectionTitle(%q) = %q, want %q", tt.section, got, tt.want)
			}
		})
	}
}

func TestFixedSectionTitle(t *testing.T) {
	tests := []struct {
		section string
		want    string
		wantOK  bool
	}{
		{"Cutscene", CutsceneTitle, true},
		{"Reaction", ReactionTitle, true},
		{"Unused", UnusedTitle, true},
		{"unused", UnusedTitle, true},
		{"L01 Ryan", "", false},
		{"Cutscenes", "", false},
		{" Cutscene", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.section, func(t *testing.T) {
			got, ok := FixedSectionTitle(tt.section)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("FixedSectionTitle(%q) = (%q, %t), want (%q, %t)", tt.section, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// TestLevelForLastWins は同じセクション名の行が複数あるとき後の行が勝つことを確かめる。
func TestLevelForLastWins(t *testing.T) {
	csv := "level,dragon,weather\n" +
		"0,Ryan,Sunny\n" +
		"0,Ryan,Night\n"
	data := &Data{Levels: ParseLevels(rowsFromCSV(csv))}

	meta, ok := data.LevelFor("L01 Ryan")
	if !ok {
		t.Fatal("L01 Ryan が引けない")
	}
	if meta.Weather != "Night" {
		t.Errorf("Weather = %q, want Night（後の行が勝つ）", meta.Weather)
	}
	if _, ok := data.LevelFor("L01 Conrad"); ok {
		t.Error("L01 Conrad が引けてしまった")
	}
}

// speakersCSV は話者表の組み立てを試すための並び。ファイル順が話者の順になる。
const speakersCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
	"L01 Ryan,intro,N1,1,line:0001,aaaaaaaaaaaaaaaa,Phone,\n" +
	"L01 Ryan,intro,N1,2,line:0002,bbbbbbbbbbbbbbbb,Ryan,\n" +
	"L01 Ryan,intro,N1,3,line:0003,aaaaaaaaaaaaaaaa,Ryan,\n" +
	"L02 Conrad,intro,N2,1,line:0004,aaaaaaaaaaaaaaaa,Phone,\n" +
	"L02 Conrad,intro,N2,2,line:0005,bbbbbbbbbbbbbbbb,Ryan,\n" +
	"L02 Conrad,intro,N2,3,line:0006,cccccccccccccccc,,\n" +
	"L02 Conrad,intro,N2,4,line:0007,dddddddddddddddd,ryan,\n" +
	"L02 Conrad,intro,N2,5,line:0008,dddddddddddddddd,Ryan,\n"

func TestSpeakers(t *testing.T) {
	data := &Data{Entries: ParseEntries(rowsFromCSV(speakersCSV))}

	tests := []struct {
		name     string
		key      string
		want     []string
		wantJoin string
		wantShar bool
	}{
		{
			name: "出現順で重複なく並ぶ", key: "aaaaaaaaaaaaaaaa",
			want: []string{"Phone", "Ryan"}, wantJoin: "Phone/Ryan", wantShar: true,
		},
		{
			name: "同じ話者が何度出ても1人", key: "bbbbbbbbbbbbbbbb",
			want: []string{"Ryan"}, wantJoin: "Ryan", wantShar: false,
		},
		{
			name: "話者が空の行は数えない", key: "cccccccccccccccc",
			want: nil, wantJoin: "", wantShar: false,
		},
		{
			name: "話者名の大小は区別する（別人として並ぶ）", key: "dddddddddddddddd",
			want: []string{"ryan", "Ryan"}, wantJoin: "ryan/Ryan", wantShar: true,
		},
		{
			name: "未登録のキー", key: "eeeeeeeeeeeeeeee",
			want: nil, wantJoin: "", wantShar: false,
		},
		{
			name: "引く側のキーは正規化される", key: "  AAAAAAAAAAAAAAAA  ",
			want: []string{"Phone", "Ryan"}, wantJoin: "Phone/Ryan", wantShar: true,
		},
		{
			name: "空のキー", key: "",
			want: nil, wantJoin: "", wantShar: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := data.Speakers(tt.key); !slices.Equal(got, tt.want) {
				t.Errorf("Speakers(%q) = %q, want %q", tt.key, got, tt.want)
			}
			if got := data.SpeakersFor(tt.key); got != tt.wantJoin {
				t.Errorf("SpeakersFor(%q) = %q, want %q", tt.key, got, tt.wantJoin)
			}
			if got := data.IsShared(tt.key); got != tt.wantShar {
				t.Errorf("IsShared(%q) = %t, want %t", tt.key, got, tt.wantShar)
			}
		})
	}
}

// TestSpeakersReturnsCopy は返り値を書き換えても内部の表が壊れないことを確かめる。
func TestSpeakersReturnsCopy(t *testing.T) {
	data := &Data{Entries: ParseEntries(rowsFromCSV(speakersCSV))}

	got := data.Speakers("aaaaaaaaaaaaaaaa")
	got[0] = "書き換え"
	if again := data.SpeakersFor("aaaaaaaaaaaaaaaa"); again != "Phone/Ryan" {
		t.Errorf("SpeakersFor = %q, want Phone/Ryan", again)
	}
}

// TestSpeakersBuiltOnce は話者表が一度だけ作られることを確かめる。元実装も
// 遅延構築で作り直さないので、Entries を後から差し替えても結果は変わらない。
func TestSpeakersBuiltOnce(t *testing.T) {
	data := &Data{Entries: ParseEntries(rowsFromCSV(speakersCSV))}
	if got := data.SpeakersFor("aaaaaaaaaaaaaaaa"); got != "Phone/Ryan" {
		t.Fatalf("SpeakersFor = %q, want Phone/Ryan", got)
	}

	data.Entries = nil
	if got := data.SpeakersFor("aaaaaaaaaaaaaaaa"); got != "Phone/Ryan" {
		t.Errorf("Entries を消したあとの SpeakersFor = %q, want Phone/Ryan（作り直さない）", got)
	}
}

// TestDataConcurrentLookup は遅延構築の索引を同時に引いても壊れないことを確かめる。
// 元実装の静的キャッシュはロックが無いが、こちらは sync.Once で構築を1回にしてある。
//
// 競合を実際に検出するには -race が要る（cgo が要るので、gcc の無い環境では
// 動かせない）。-race 無しでも、索引が nil のまま読まれる類の事故は拾える。
func TestDataConcurrentLookup(t *testing.T) {
	data := &Data{
		Entries: ParseEntries(rowsFromCSV(speakersCSV)),
		Levels:  ParseLevels(rowsFromCSV(testLevelsCSV)),
	}

	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			if got := data.SpeakersFor("aaaaaaaaaaaaaaaa"); got != "Phone/Ryan" {
				t.Errorf("SpeakersFor = %q, want Phone/Ryan", got)
			}
			if got := data.SectionTitle("L01 Ryan"); !strings.HasPrefix(got, "Level 1: Ryan") {
				t.Errorf("SectionTitle = %q", got)
			}
		}()
	}
	wg.Wait()
}

func TestLoadCSharp(t *testing.T) {
	const orderCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
		"L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf,0da72197e898ebe1,Ryan,\n"

	data := LoadCSharp([]byte(orderCSV), []byte(testLevelsCSV))
	if len(data.Entries) != 1 {
		t.Fatalf("Entries = %d件, want 1", len(data.Entries))
	}
	if len(data.Levels) != 3 {
		t.Fatalf("Levels = %d件, want 3", len(data.Levels))
	}
	if got := data.SectionTitle(data.Entries[0].Section); got != "Level 1: Ryan (Sunny) | sets level_1 | ends level_1_complete" {
		t.Errorf("SectionTitle = %q", got)
	}
	if got := data.SpeakersFor("0da72197e898ebe1"); got != "Ryan" {
		t.Errorf("SpeakersFor = %q, want Ryan", got)
	}
}

// TestLoadWithoutLevelFlow は level_flow.csv が無い（nil）場合を確かめる。
func TestLoadWithoutLevelFlow(t *testing.T) {
	const orderCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
		"L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf,0da72197e898ebe1,Ryan,\n"

	t.Run("C#方式", func(t *testing.T) {
		data := LoadCSharp([]byte(orderCSV), nil)
		if len(data.Levels) != 0 {
			t.Fatalf("Levels = %d件, want 0", len(data.Levels))
		}
		if got := data.SectionTitle("L01 Ryan"); got != "L01 Ryan" {
			t.Errorf("SectionTitle = %q, want L01 Ryan", got)
		}
	})

	t.Run("PowerShell方式", func(t *testing.T) {
		data, err := LoadPowerShell([]byte(orderCSV), nil)
		if err != nil {
			t.Fatalf("LoadPowerShell が失敗した: %v", err)
		}
		if len(data.Levels) != 0 {
			t.Fatalf("Levels = %d件, want 0", len(data.Levels))
		}
	})
}

// TestLoadPowerShellDuplicateColumn はヘッダーの列名が重複していると
// エラーになることを確かめる。元実装では処理全体が止まる箇所。
func TestLoadPowerShellDuplicateColumn(t *testing.T) {
	tests := []struct {
		name      string
		orderCSV  string
		levelsCSV string
		wantIn    string
	}{
		{
			name:      "script_order 側",
			orderCSV:  "key,key\naaaaaaaaaaaaaaaa,bbbbbbbbbbbbbbbb\n",
			levelsCSV: testLevelsCSV,
			wantIn:    "script_order.csv",
		},
		{
			name:      "level_flow 側",
			orderCSV:  "section,key\nL01 Ryan,aaaaaaaaaaaaaaaa\n",
			levelsCSV: "level,LEVEL\n0,1\n",
			wantIn:    "level_flow.csv",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadPowerShell([]byte(tt.orderCSV), []byte(tt.levelsCSV))
			if err == nil {
				t.Fatal("エラーにならなかった")
			}
			var dup *csvfile.DuplicateColumnError
			if !errors.As(err, &dup) {
				t.Errorf("err = %v, want DuplicateColumnError", err)
			}
			if !strings.Contains(err.Error(), tt.wantIn) {
				t.Errorf("err = %v, want %q を含む", err, tt.wantIn)
			}
		})
	}
}

// TestLoadReadersDifferOnMultilineFieldLineBasedNow は2つの読み方の違いが出る入力を
// 確かめる。実データにはこの形は無いが、どちらを選んだかで結果が変わることを残しておく。
//
// PowerShell 方式の期待値は、行単位で読むいまの結果（csvfile.ReadPowerShellRows）を
// 固定したもので、正しいとはしない。全体を解釈する読み手へ移す作業
// （docs/port-spec.md）の PR2 で、order が全体を解釈する読み方（上流 main と同じ）へ
// 移ると、引用フィールドの中の改行は値に残り、C# 方式と同じ1件になる。そのコミットで
// 期待値を直す（決まったことの 13）。
func TestLoadReadersDifferOnMultilineFieldLineBasedNow(t *testing.T) {
	// 行頭の '#' はどちらも落とすが、PowerShell 方式は引用の中を見ないので
	// 引用フィールド内の改行で行が割れる。
	const orderCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
		"# これはコメント\n" +
		"\"L01\nRyan\",intro,N1,1,line:0001,aaaaaaaaaaaaaaaa,Ryan,\n"

	sharp := LoadCSharp([]byte(orderCSV), nil)
	if len(sharp.Entries) != 1 {
		t.Fatalf("C#方式の Entries = %d件, want 1", len(sharp.Entries))
	}
	if sharp.Entries[0].Section != "L01\nRyan" {
		t.Errorf("C#方式の Section = %q, want %q", sharp.Entries[0].Section, "L01\nRyan")
	}

	shell, err := LoadPowerShell([]byte(orderCSV), nil)
	if err != nil {
		t.Fatalf("LoadPowerShell が失敗した: %v", err)
	}
	// 2つの物理行がそれぞれ別レコードになる。前半（"L01）は key が空、
	// 後半（Ryan",intro,...）が key を持つ。件数が2件なのは pwsh 7.6.6 の
	// ConvertFrom-Csv と同じで、key が空の行を捨てないため（[ParseAllEntries]）。
	if len(shell.Entries) != 2 {
		t.Fatalf("PowerShell方式の Entries = %d件, want 2", len(shell.Entries))
	}
	if shell.Entries[0].Section == "L01\nRyan" {
		t.Error("PowerShell方式で複数行フィールドが読めてしまった")
	}
	if got, want := shell.Entries[0].Section, "L01"; got != want {
		t.Errorf("前半の Section = %q, want %q", got, want)
	}
	if got := shell.Entries[0].Key; got != "" {
		t.Errorf("前半の Key = %q, want 空文字", got)
	}
	if got, want := shell.Entries[1].Section, `Ryan"`; got != want {
		t.Errorf("後半の Section = %q, want %q", got, want)
	}
	if got, want := shell.Entries[1].Key, "aaaaaaaaaaaaaaaa"; got != want {
		t.Errorf("後半の Key = %q, want %q", got, want)
	}
}

// TestLoadPowerShellUnclosedQuoteLineBasedNow は、閉じない引用符のある再生順を、
// 行単位で読んで何事も無く読み込むいまの振る舞いを固定する。
//
// 行単位の読み方は、閉じない引用符をその物理行の終わりで閉じる。全体を解釈すると
// ファイルの終わりまでが1つの値になるので、全体を解釈する読み手へ移る PR2 では
// 誤り（csvfile.UnclosedQuoteError）になる。そのコミットで期待値を直す
// （決まったことの 13）。
func TestLoadPowerShellUnclosedQuoteLineBasedNow(t *testing.T) {
	const orderCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
		"L01 Ryan,intro,N1,1,line:0001,aaaaaaaaaaaaaaaa,\"Ryan,\n" +
		"L01 Ryan,intro,N1,2,line:0002,bbbbbbbbbbbbbbbb,Kobold,\n"

	data, err := LoadPowerShell([]byte(orderCSV), nil)
	if err != nil {
		t.Fatalf("行単位では誤りにならないはず: %v", err)
	}
	if len(data.Entries) != 2 {
		t.Fatalf("Entries = %d件、行単位では 2", len(data.Entries))
	}
	if got, want := data.Entries[0].Speaker, "Ryan,"; got != want {
		t.Errorf("1件目の Speaker = %q、行単位では %q（引用符が行の終わりで閉じる）", got, want)
	}
	if got, want := data.Entries[1].Key, "bbbbbbbbbbbbbbbb"; got != want {
		t.Errorf("2件目の Key = %q, want %q", got, want)
	}
}
