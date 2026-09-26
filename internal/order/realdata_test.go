package order

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/sourcerepo"
)

// 実データの試験は上流 main に追従する（改善の決定 31）。件数や行の値は決め打ちにせず、
// 入力の物理行（sourcerepo.ContentLines）をカンマで分けたものから求める。
// data/ の2ファイルには引用符・コメント行・空行が1つも無い（TestRealDataLoadersAgree の
// 前提）ので、カンマで分けた値が、そのまま読み手の読むべき値になる。
//
// 以前は上流 003ed1e に固有の値（1839 行、15 レベル、1602 種のキー、先頭と末尾の行、
// 8 列で norm・fp・nlen の無い形）を決め打ちにしていた。いまの main は norm・fp・nlen の
// 3列を足した 11 列で、先頭と末尾の行の Norm と FP が空でないので落ちていた。
// 8 列の形を読むことは TestLoadPowerShellOlderShapes（合成の見本）が見る。

// uiSectionTitle は公開CSVの最後に出る見出し。セクション名から作られるものではなく、
// 出力側が書く固定文字列なので、このパッケージの対象外。
const uiSectionTitle = "UI and other text (not part of the dialogue script)"

// sourceRepo は元実装のリポジトリの場所を返す。環境変数（sourcerepo.Env）で指定した
// 場所に無ければ落とし、指定していなくて見つからなければ飛ばす（sourcerepo.Find）。
// CI には元リポジトリが無いので、飛ばせることが必須。
func sourceRepo(t *testing.T) string {
	t.Helper()
	return sourcerepo.Find(t, "data", "script_order.csv")
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

// plainTable は、引用符の無い CSV の data を、列名から値を引ける行の並びにする。
// 読み手（internal/csvfile）を通さずに期待値を作るためにある。引用符のある行が
// あれば落とす（data/ の2ファイルには無い）。
func plainTable(t *testing.T, data []byte) (lines []sourcerepo.Line, rows []map[string]string) {
	t.Helper()
	content := sourcerepo.ContentLines(data)
	if len(content) < 2 {
		t.Fatalf("ヘッダーとデータの行が無い（%d 行）", len(content))
	}
	header, ok := sourcerepo.PlainFields(content[0].Text)
	if !ok {
		t.Fatalf("ヘッダーに引用符がある: %s", content[0].Text)
	}
	for _, line := range content[1:] {
		fields, ok := sourcerepo.PlainFields(line.Text)
		if !ok {
			t.Fatalf("%d行目に引用符がある。カンマで分けて期待値を作れない", line.Number)
		}
		row := make(map[string]string, len(header))
		for i, name := range header {
			if i < len(fields) {
				row[strings.ToLower(name)] = fields[i]
			}
		}
		lines = append(lines, line)
		rows = append(rows, row)
	}
	return lines, rows
}

// TestRealDataLoad は実データの件数と、すべての行の中身を、入力の物理行をカンマで
// 分けた値と突き合わせる。
func TestRealDataLoad(t *testing.T) {
	data := loadRealData(t)
	lines, rows := plainTable(t, readSourceFile(t, "data", "script_order.csv"))
	levelLines, _ := plainTable(t, readSourceFile(t, "data", "level_flow.csv"))

	if len(data.Entries) != len(rows) {
		t.Fatalf("Entries = %d件, want %d（script_order.csv のデータ行）", len(data.Entries), len(rows))
	}
	// level_flow.csv の level はどの行も整数なので、捨てられる行は無い。
	if len(data.Levels) != len(levelLines) {
		t.Errorf("Levels = %d件, want %d（level_flow.csv のデータ行）", len(data.Levels), len(levelLines))
	}

	for i, e := range data.Entries {
		row := rows[i]
		want := Entry{
			Section: row["section"], HasSection: true, Phase: row["phase"], Node: row["node"], HasNode: true,
			Order: parseOrder(row["order"]), OrderText: row["order"], LineID: row["line_id"],
			Key: strings.ToLower(strings.TrimSpace(row["key"])), Speaker: row["speaker"], Condition: row["condition"],
			Norm: row["norm"], FP: row["fp"], NLen: parseNLen(row["nlen"]),
		}
		if e != want {
			// 値は出さない。行番号と、どの列かだけにする。
			t.Errorf("%d行目の Entry が、カンマで分けた値と違う", lines[i].Number)
		}
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
func TestRealDataSectionTitles(t *testing.T) {
	data := loadRealData(t)

	// script_order.csv のセクションを初出順に集める。公開CSVの見出しは
	// セクションが変わるたびに出るので、セクションが飛び飛びに再登場しない
	// かぎりこの並びと一致する（実データでは再登場しない）。
	var sections []string
	for _, e := range data.Entries {
		if !slices.Contains(sections, e.Section) {
			sections = append(sections, e.Section)
		}
	}

	got := make([]string, 0, len(sections))
	for _, section := range sections {
		got = append(got, data.SectionTitle(section))
	}

	want := publishedSectionTitles(t)
	if len(want) == 0 {
		t.Fatal("公開ファイルに見出しの行が1つも無い")
	}
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

// TestRealDataLevelHeaders は、level_flow.csv のどのレベルも、そのセクション名で
// 見出しを引けることを確かめる。
//
// 見出しの書き方（天気だけ・sets だけ・ends だけ・全部そろい）は TestLevelMetaHeader が
// 合成の見本で見る。ここは、実データのセクション名から LevelFor で引き当てられる
// ことを見る。見出しの文言そのものは TestRealDataSectionTitles が公開ファイルと
// 突き合わせる。
func TestRealDataLevelHeaders(t *testing.T) {
	data := loadRealData(t)
	if len(data.Levels) == 0 {
		t.Fatal("レベルが1つも無い")
	}
	for _, level := range data.Levels {
		section := level.Section()
		meta, ok := data.LevelFor(section)
		if !ok {
			t.Errorf("LevelFor(%q) が引けない", section)
			continue
		}
		if meta.Section() != section {
			t.Errorf("Section() = %q, want %q", meta.Section(), section)
		}
		if got := data.SectionTitle(section); got != meta.Header() {
			t.Errorf("SectionTitle(%q) = %q, want %q", section, got, meta.Header())
		}
	}
}

// TestRealDataSpeakers は話者表を実データで確かめる。期待値は、script_order.csv の
// 行をカンマで分けて、キーごとに初出順で重ねずに並べたもの（元実装の BuildSpeakers と
// 同じ作り方）である。
func TestRealDataSpeakers(t *testing.T) {
	data := loadRealData(t)
	_, rows := plainTable(t, readSourceFile(t, "data", "script_order.csv"))

	want := make(map[string][]string)
	var keys []string
	for _, row := range rows {
		k := strings.ToLower(strings.TrimSpace(row["key"]))
		if _, seen := want[k]; !seen {
			keys = append(keys, k)
			want[k] = nil
		}
		if s := row["speaker"]; s != "" && !slices.Contains(want[k], s) {
			want[k] = append(want[k], s)
		}
	}
	if len(keys) == 0 {
		t.Fatal("キーが1つも無い")
	}
	shared := 0
	for _, k := range keys {
		if got := data.SpeakersFor(k); got != strings.Join(want[k], SpeakerSeparator) {
			t.Errorf("SpeakersFor(%q) = %q, want %q", k, got, strings.Join(want[k], SpeakerSeparator))
		}
		if got := data.IsShared(k); got != (len(want[k]) > 1) {
			t.Errorf("IsShared(%q) = %t, want %t", k, got, len(want[k]) > 1)
		}
		if len(want[k]) > 1 {
			shared++
		}
	}
	// 知らないキーは空。
	unknown := strings.Repeat("f", key.Length)
	if _, ok := want[unknown]; !ok {
		if got := data.SpeakersFor(unknown); got != "" || data.IsShared(unknown) {
			t.Errorf("知らないキーの SpeakersFor = %q, IsShared = %t", got, data.IsShared(unknown))
		}
	}
	t.Logf("キー %d 種、うち2人以上が話すキー %d 種", len(keys), shared)
}

// TestRealDataSpeakerColumn は公開ずみ Translations/ja/strings.csv の speaker 列と
// SpeakersFor の結果を突き合わせる。元実装のハッシュ行は
// `$speakers[$k] -join '/'` をそのまま speaker 列に書いているので、
// 話者表の作り方が違えばここで落ちる。
//
// 照合する行の数は、公開ファイルの物理行から数える（キーが16桁で section が UI でない
// 行）。キーと section の列には引用符が入らないので、頭の2つのカンマで分ければ足りる。
func TestRealDataSpeakerColumn(t *testing.T) {
	data := loadRealData(t)
	raw := readSourceFile(t, "Translations", "ja", "strings.csv")

	wantChecked := 0
	for _, line := range sourcerepo.ContentLines(raw)[1:] {
		parts := strings.SplitN(line.Text, ",", 3)
		if len(parts) == 3 && key.LooksLike(parts[0]) && parts[1] != "UI" {
			wantChecked++
		}
	}
	if wantChecked == 0 {
		t.Fatal("照合できる行が公開ファイルに1つも無い")
	}

	f, err := csvfile.ReadPowerShell(raw)
	if err != nil {
		t.Fatalf("ja/strings.csv が読めない: %v", err)
	}
	checked := 0
	for _, row := range f.Rows() {
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
	if checked != wantChecked {
		t.Errorf("照合した行 = %d, want %d（公開ファイルの物理行から数えた数）", checked, wantChecked)
	}
}
