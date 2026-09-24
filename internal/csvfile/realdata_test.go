package csvfile

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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
	// ja/strings.csv は1943物理行（コメント202、空行19）。
	// 残る1722行のうち先頭がヘッダーなので、データ行は1721。
	jaContentLines = 1722
	jaDataRows     = jaContentLines - 1
	// data/level_flow.csv は16物理行 = ヘッダ1 + データ15（level 0..14）。BOM付き。
	levelFlowRows = 15
	// data/script_order.csv は1840物理行 = ヘッダ1 + データ1839。
	scriptOrderRows = 1839
)

// sourceRepo は元実装のリポジトリの場所を返す。見つからなければテストを飛ばす。
// CI には元リポジトリが無いので、飛ばせることが必須。
func sourceRepo(t *testing.T) string {
	t.Helper()

	candidates := sourceRepoCandidates
	if env := os.Getenv(sourceRepoEnv); env != "" {
		candidates = []string{env}
	}
	for _, root := range candidates {
		if _, err := os.Stat(filepath.Join(root, "Translations")); err == nil {
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

// TestRealDataJaStrings は公開ファイル Translations/ja/strings.csv を3方式すべてで読み、
// 同じ行数・同じ内容になることを確かめる。
func TestRealDataJaStrings(t *testing.T) {
	data := readSourceFile(t, "Translations", "ja", "strings.csv")
	columns := []string{"key", "section", "node", "order", "speaker", "translation"}

	t.Run("C#方式", func(t *testing.T) {
		rows := ReadCSharpRows(data)
		if len(rows) != jaDataRows {
			t.Fatalf("行数 = %d, want %d", len(rows), jaDataRows)
		}
		if got := rows[0].Columns(); !slices.Equal(got, columns) {
			t.Errorf("ヘッダー = %q, want %q", got, columns)
		}
		want := []string{"0da72197e898ebe1", "L01 Ryan", "Ryan_1_intro", "1", "Ryan", "もしもし？"}
		for i, name := range columns {
			if got := rows[0].Get(name); got != want[i] {
				t.Errorf("1行目の %s = %q, want %q", name, got, want[i])
			}
		}
	})

	t.Run("PowerShell方式", func(t *testing.T) {
		rows, err := ReadPowerShellRows(data)
		if err != nil {
			t.Fatalf("ReadPowerShellRows が失敗した: %v", err)
		}
		if len(rows) != jaDataRows {
			t.Fatalf("行数 = %d, want %d", len(rows), jaDataRows)
		}
	})

	t.Run("Python方式", func(t *testing.T) {
		records, err := ReadPythonRecords(data)
		if err != nil {
			t.Fatalf("ReadPythonRecords が失敗した: %v", err)
		}
		// 空白だけの行は無いので、コメントと空行を捨てたレコードは内容行と同じ数になる。
		if len(records) != jaContentLines {
			t.Fatalf("レコード数 = %d, want %d（ヘッダーを含む）", len(records), jaContentLines)
		}
		if got := records[0].Fields; !slices.Equal(got, columns) {
			t.Errorf("ヘッダー = %q, want %q", got, columns)
		}
		// 公開ファイルは6列ちょうどでなければならない。実データはそうなっている。
		for _, r := range records {
			if len(r.Fields) != len(columns) {
				t.Errorf("%d行目: フィールド数 = %d, want %d", r.Number, len(r.Fields), len(columns))
			}
		}
		// 報告用の行番号が物理行番号と一致していること。
		if records[1].Number != 5 {
			t.Errorf("最初のデータ行の行番号 = %d, want 5", records[1].Number)
		}
	})

	t.Run("3方式で内容が一致する", func(t *testing.T) {
		csharp := ReadCSharpRows(data)
		powershell, err := ReadPowerShellRows(data)
		if err != nil {
			t.Fatalf("ReadPowerShellRows が失敗した: %v", err)
		}
		python, err := ReadPythonRecords(data)
		if err != nil {
			t.Fatalf("ReadPythonRecords が失敗した: %v", err)
		}
		if len(csharp) != len(powershell) || len(csharp) != len(python)-1 {
			t.Fatalf("行数が揃わない: C#=%d PowerShell=%d Python=%d（Pythonはヘッダー込み）",
				len(csharp), len(powershell), len(python))
		}
		for i := range csharp {
			for c, name := range columns {
				a, b, p := csharp[i].Get(name), powershell[i].Get(name), python[i+1].Fields[c]
				if a != b || a != p {
					t.Fatalf("%d行目の %s が食い違う: C#=%q PowerShell=%q Python=%q", i, name, a, b, p)
				}
			}
		}
	})

	// 読んだ値を書き戻すと元の行に戻る。エスケープ規則（移植仕様 R3 / R14）を
	// 実データで裏付ける。ja/strings.csv には引用符を含む行が8行ある。
	t.Run("読み書きの往復で元の行に戻る", func(t *testing.T) {
		records, err := ReadPythonRecords(data)
		if err != nil {
			t.Fatalf("ReadPythonRecords が失敗した: %v", err)
		}
		// 各レコードの元の物理行。実データの値は1行に収まっているので、
		// レコードの先頭行がそのままその行の全体になる。
		physical := SplitPythonLines(data)
		lines := make([]Line, 0, len(records))
		for _, r := range records {
			lines = append(lines, physical[r.Number-1])
		}
		rows := ReadCSharpRows(data)
		if len(lines) != len(rows)+1 {
			t.Fatalf("物理行とレコードの数が揃わない: %d と %d", len(lines), len(rows))
		}

		quoted := 0
		for i, row := range rows {
			fields := make([]string, 0, len(columns))
			for _, name := range columns {
				fields = append(fields, row.Get(name))
			}
			got := string(AppendLine(nil, fields...))
			want := lines[i+1].Text
			if got != want {
				t.Fatalf("%d行目の書き戻しが元と違う\n got  %q\n want %q", lines[i+1].Number, got, want)
			}
			if len(fields[5]) != len(EscapeField(fields[5])) {
				quoted++
			}
		}
		if quoted != 8 {
			t.Errorf("引用符付きで書き出される訳文の行数 = %d, want 8", quoted)
		}
	})
}

// TestRealDataLevelFlow は data/level_flow.csv を読む。このファイルだけ BOM 付きなので、
// BOM を剥がせているかがそのまま列引きの成否になる。
func TestRealDataLevelFlow(t *testing.T) {
	data := readSourceFile(t, "data", "level_flow.csv")
	if len(data) < 3 || string(data[:3]) != "\xef\xbb\xbf" {
		t.Fatalf("このファイルはBOM付きのはずだが先頭が %x になっている", data[:min(3, len(data))])
	}

	rows, err := ReadPowerShellRows(data)
	if err != nil {
		t.Fatalf("ReadPowerShellRows が失敗した: %v", err)
	}
	if len(rows) != levelFlowRows {
		t.Fatalf("行数 = %d, want %d", len(rows), levelFlowRows)
	}

	// BOM が残っていると flow_asset だけが引けなくなる。
	if got, ok := rows[0].Lookup("flow_asset"); !ok || got != "LevelFlow" {
		t.Errorf("Lookup(flow_asset) = (%q, %v), want (\"LevelFlow\", true)", got, ok)
	}
	for i, want := range []struct{ level, dragon, weather string }{
		{"0", "Ryan", "Sunny"},
		{"14", "Alexander", "Sunny"},
	} {
		row := rows[0]
		if i == 1 {
			row = rows[len(rows)-1]
		}
		if got := row.Get("level"); got != want.level {
			t.Errorf("level = %q, want %q", got, want.level)
		}
		if got := row.Get("dragon"); got != want.dragon {
			t.Errorf("dragon = %q, want %q", got, want.dragon)
		}
		if got := row.Get("weather"); got != want.weather {
			t.Errorf("weather = %q, want %q", got, want.weather)
		}
	}
	// 最終行の player_spawn は空（末尾カンマ）。列はあるが値が空、という区別を確かめる。
	last := rows[len(rows)-1]
	if got, ok := last.Lookup("player_spawn"); !ok || got != "" {
		t.Errorf("Lookup(player_spawn) = (%q, %v), want (\"\", true)", got, ok)
	}

	// C#方式でも同じ件数になる。
	if got := len(ReadCSharpRows(data)); got != levelFlowRows {
		t.Errorf("C#方式の行数 = %d, want %d", got, levelFlowRows)
	}
}

// TestRealDataScriptOrder は data/script_order.csv を読む。出力順の権威になるファイル。
func TestRealDataScriptOrder(t *testing.T) {
	data := readSourceFile(t, "data", "script_order.csv")

	rows, err := ReadPowerShellRows(data)
	if err != nil {
		t.Fatalf("ReadPowerShellRows が失敗した: %v", err)
	}
	if len(rows) != scriptOrderRows {
		t.Fatalf("行数 = %d, want %d", len(rows), scriptOrderRows)
	}

	want := map[string]string{
		"section":   "L01 Ryan",
		"phase":     "intro",
		"node":      "Ryan_1_intro",
		"order":     "1",
		"line_id":   "line:a8779ebf",
		"key":       "0da72197e898ebe1",
		"speaker":   "Ryan",
		"condition": "",
	}
	for name, value := range want {
		got, ok := rows[0].Lookup(name)
		if !ok {
			t.Errorf("1行目に %s 列が無い", name)
			continue
		}
		if got != value {
			t.Errorf("1行目の %s = %q, want %q", name, got, value)
		}
	}

	// section にはスペースが含まれる。引用符なしフィールドの空白の扱いを
	// 間違えると、ここで "L01" や "L01 " に化ける。
	for _, row := range rows {
		if section := row.Get("section"); section == "" {
			t.Fatal("section が空の行がある")
		}
	}
	if got := len(ReadCSharpRows(data)); got != scriptOrderRows {
		t.Errorf("C#方式の行数 = %d, want %d", got, scriptOrderRows)
	}
}
