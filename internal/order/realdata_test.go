package order

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
)

// sourceRepoEnv は元実装のリポジトリの場所を上書きする環境変数。
const sourceRepoEnv = "DRAGNWASH_SOURCE_REPO"

// sourceRepoCandidates は環境変数が無いときに探す場所。
var sourceRepoCandidates = []string{
	`C:\dev\223n\dragnwash-localization\.claude\worktrees\translator-editor-research-fdf740`,
	`C:\dev\223n\dragnwash-localization`,
}

// 実データの件数。いずれも元ファイルを数えて得た値。
const (
	// data/script_order.csv は1840物理行 = ヘッダ1 + データ1839。key が空の行は無い。
	scriptOrderEntries = 1839
	// data/level_flow.csv は16物理行 = ヘッダ1 + データ15（level 0..14）。BOM付き。
	levelFlowLevels = 15
	// script_order.csv の key は1602種。speaker が空の行が無いので話者表も1602件。
	distinctKeys = 1602
	// うち話者が2人以上のキーは29種。
	sharedKeys = 29
	// セクションは18種。公開CSVの "# =====" 行（UI 見出しを除く）と同数。
	distinctSections = 18
)

// uiSectionTitle は公開CSVの最後に出る見出し。セクション名から作られるものではなく、
// 出力側が書く固定文字列なので、このパッケージの対象外。
const uiSectionTitle = "UI and other text (not part of the dialogue script)"

// sourceRepo は元実装のリポジトリの場所を返す。見つからなければテストを飛ばす。
// CI には元リポジトリが無いので、飛ばせることが必須。
func sourceRepo(t *testing.T) string {
	t.Helper()

	candidates := sourceRepoCandidates
	if env := os.Getenv(sourceRepoEnv); env != "" {
		candidates = []string{env}
	}
	for _, root := range candidates {
		if _, err := os.Stat(filepath.Join(root, "data", "script_order.csv")); err == nil {
			return root
		}
	}
	t.Skipf("元実装のリポジトリが見つからないので飛ばす（%s で場所を指定できる）", sourceRepoEnv)
	return ""
}

func readSourceFile(t *testing.T, parts ...string) []byte {
	t.Helper()
	path := filepath.Join(append([]string{sourceRepo(t)}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s が読めない: %v", path, err)
	}
	return data
}

// loadRealData は実データを読む。読み方は公開CSV生成と同じ PowerShell 方式。
func loadRealData(t *testing.T) *Data {
	t.Helper()
	data, err := LoadPowerShell(
		readSourceFile(t, "data", "script_order.csv"),
		readSourceFile(t, "data", "level_flow.csv"),
	)
	if err != nil {
		t.Fatalf("LoadPowerShell が失敗した: %v", err)
	}
	return data
}

// TestRealDataLoad は実データの件数と、先頭・末尾の行の中身を確かめる。
func TestRealDataLoad(t *testing.T) {
	data := loadRealData(t)

	if len(data.Entries) != scriptOrderEntries {
		t.Errorf("Entries = %d件, want %d", len(data.Entries), scriptOrderEntries)
	}
	if len(data.Levels) != levelFlowLevels {
		t.Errorf("Levels = %d件, want %d", len(data.Levels), levelFlowLevels)
	}

	first := Entry{
		Section: "L01 Ryan", HasSection: true, Phase: "intro", Node: "Ryan_1_intro", HasNode: true,
		Order: 1, OrderText: "1", LineID: "line:a8779ebf",
		Key: "0da72197e898ebe1", Speaker: "Ryan",
	}
	if data.Entries[0] != first {
		t.Errorf("先頭 = %+v, want %+v", data.Entries[0], first)
	}

	last := Entry{
		Section: "Unused", HasSection: true, Node: "Start", HasNode: true,
		Order: 24, OrderText: "24", LineID: "line:1772a124",
		Key: "add98a1ef99b1290", Speaker: "Start",
	}
	if got := data.Entries[len(data.Entries)-1]; got != last {
		t.Errorf("末尾 = %+v, want %+v", got, last)
	}
}

// TestRealDataLoadersAgree は2つの読み方が実データでは同じ結果になることを確かめる。
// data/ の2ファイルには引用符・コメント行・空行が1つも無いので一致するはずで、
// 片方が壊れたときにここで気づける。
func TestRealDataLoadersAgree(t *testing.T) {
	shell := loadRealData(t)
	sharp := LoadCSharp(
		readSourceFile(t, "data", "script_order.csv"),
		readSourceFile(t, "data", "level_flow.csv"),
	)

	if !slices.Equal(shell.Entries, sharp.Entries) {
		t.Error("Entries が2方式で食い違う")
	}
	if !slices.Equal(shell.Levels, sharp.Levels) {
		t.Error("Levels が2方式で食い違う")
	}
}

// TestRealDataSectionTitles は実データから作った見出し文言が、公開ずみの
// Translations/ja/strings.csv の "# ===== ... =====" 行と一致することを確かめる。
// 仕様書の実出力例（Level 5 / Level 9 / Level 10）もここに含まれる。
func TestRealDataSectionTitles(t *testing.T) {
	data := loadRealData(t)

	// script_order.csv のセクションを初出順に集める。公開CSVの見出しは
	// セクションが変わるたびに出るので、セクションが飛び飛びに再登場しない
	// かぎりこの並びと一致する（実データでは再登場しない＝18種18見出し）。
	var sections []string
	for _, e := range data.Entries {
		if !slices.Contains(sections, e.Section) {
			sections = append(sections, e.Section)
		}
	}
	if len(sections) != distinctSections {
		t.Fatalf("セクション = %d種, want %d", len(sections), distinctSections)
	}

	got := make([]string, 0, len(sections))
	for _, section := range sections {
		got = append(got, data.SectionTitle(section))
	}

	want := publishedSectionTitles(t)
	if !slices.Equal(got, want) {
		t.Errorf("見出し文言が公開ファイルと食い違う\n got = %q\nwant = %q", got, want)
	}
}

// publishedSectionTitles は Translations/ja/strings.csv の見出し行から文言を取り出す。
// 最後の UI 見出しは出力側が書く固定文字列なので落とす。
func publishedSectionTitles(t *testing.T) []string {
	t.Helper()

	const prefix = "# ===== "
	const suffix = " ====="

	var titles []string
	for _, line := range csvfile.SplitNetLines(csvfile.TrimBOMString(string(readSourceFile(t, "Translations", "ja", "strings.csv")))) {
		if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, suffix) {
			continue
		}
		title := strings.TrimSuffix(strings.TrimPrefix(line, prefix), suffix)
		if title == uiSectionTitle {
			continue
		}
		titles = append(titles, title)
	}
	return titles
}

// TestRealDataLevelHeaders はレベル15件の見出しを1つずつ確かめる。
// 天気だけ・sets だけ・ends だけ・全部そろい、の4通りが実データに揃っている。
func TestRealDataLevelHeaders(t *testing.T) {
	data := loadRealData(t)

	tests := []struct {
		section string
		want    string
	}{
		{"L01 Ryan", "Level 1: Ryan (Sunny) | sets level_1 | ends level_1_complete"},
		{"L02 Conrad", "Level 2: Conrad (Sunny)"},
		{"L03 Alexander", "Level 3: Alexander (Sunny)"},
		{"L04 Ryan", "Level 4: Ryan (Sunny)"},
		{"L05 Conrad", "Level 5: Conrad (Rainy) | sets level_5 | ends MedkitCompleted, level_5_complete"},
		{"L06 Alexander", "Level 6: Alexander (Night) | sets level_6_started"},
		{"L07 Ryan", "Level 7: Ryan (Sunny)"},
		{"L08 Conrad", "Level 8: Conrad (Sunny) | sets DeliveredMountFrame"},
		{"L09 Alexander", "Level 9: Alexander (Sunny)"},
		{"L10 Ryan", "Level 10: Ryan (Sunny) | ends PicnicCompleted"},
		{"L11 Conrad", "Level 11: Conrad (Sunny)"},
		{"L12 Alexander", "Level 12: Alexander (Rainy)"},
		{"L13 Ryan", "Level 13: Ryan (Sunny)"},
		{"L14 Conrad", "Level 14: Conrad (Night)"},
		{"L15 Alexander", "Level 15: Alexander (Sunny)"},
	}
	for _, tt := range tests {
		t.Run(tt.section, func(t *testing.T) {
			if got := data.SectionTitle(tt.section); got != tt.want {
				t.Errorf("SectionTitle(%q) = %q, want %q", tt.section, got, tt.want)
			}
			meta, ok := data.LevelFor(tt.section)
			if !ok {
				t.Fatalf("LevelFor(%q) が引けない", tt.section)
			}
			if meta.Section() != tt.section {
				t.Errorf("Section() = %q, want %q", meta.Section(), tt.section)
			}
		})
	}
}

// TestRealDataSpeakers は話者表を実データで確かめる。
func TestRealDataSpeakers(t *testing.T) {
	data := loadRealData(t)

	tests := []struct {
		name     string
		key      string
		want     string
		isShared bool
	}{
		{"1人だけ", "0da72197e898ebe1", "Ryan", false},
		{"最多の29回出るキー", "ab5df625bc76dbd4", "Phone/Ryan/Alexander/Conrad/Kobold", true},
		{"知らないキー", strings.Repeat("f", key.Length), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := data.SpeakersFor(tt.key); got != tt.want {
				t.Errorf("SpeakersFor(%q) = %q, want %q", tt.key, got, tt.want)
			}
			if got := data.IsShared(tt.key); got != tt.isShared {
				t.Errorf("IsShared(%q) = %t, want %t", tt.key, got, tt.isShared)
			}
		})
	}

	var keys, shared int
	seen := make(map[string]bool)
	for _, e := range data.Entries {
		if seen[e.Key] {
			continue
		}
		seen[e.Key] = true
		keys++
		if data.IsShared(e.Key) {
			shared++
		}
	}
	if keys != distinctKeys {
		t.Errorf("キー = %d種, want %d", keys, distinctKeys)
	}
	if shared != sharedKeys {
		t.Errorf("2人以上が話すキー = %d種, want %d", shared, sharedKeys)
	}
}

// TestRealDataSpeakerColumn は公開ずみ Translations/ja/strings.csv の speaker 列と
// SpeakersFor の結果を突き合わせる。元実装のハッシュ行は
// `$speakers[$k] -join '/'` をそのまま speaker 列に書いているので、
// 話者表の作り方が違えばここで落ちる（1570行を照合する）。
func TestRealDataSpeakerColumn(t *testing.T) {
	data := loadRealData(t)

	rows, err := csvfile.ReadPowerShellRows(readSourceFile(t, "Translations", "ja", "strings.csv"))
	if err != nil {
		t.Fatalf("ja/strings.csv が読めない: %v", err)
	}

	checked := 0
	for _, row := range rows {
		k := row.Get("key")
		// 台詞ID行（line:...）の speaker 列はその出現の話者1人なので対象外。
		// UI 行は script_order.csv に無いキーなので対象外。
		if !key.LooksLike(k) || row.Get("section") == "UI" {
			continue
		}
		checked++
		if got := data.SpeakersFor(k); got != row.Get("speaker") {
			t.Errorf("key %s の speaker = %q, want %q", k, got, row.Get("speaker"))
		}
	}
	if checked != 1570 {
		t.Errorf("照合した行 = %d, want 1570", checked)
	}
}
