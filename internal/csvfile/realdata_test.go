package csvfile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/sourcerepo"
)

// 実データの試験は上流 main に追従する（改善の決定 31）。行数は決め打ちにせず、
// コメントと空行を除いた物理行（sourcerepo.ContentLines）から数え、読んだ値は
// 引用符の無い行をカンマで分けたもの（sourcerepo.PlainFields）と突き合わせる。
// 以前は上流 003ed1e に固有の値（ja/strings.csv の 1721 行、BOM 付きの level_flow.csv、
// 先頭の行の値）を決め打ちにしていたので、いまの main（ja は 1738 行、level_flow.csv に
// BOM が無い）を渡すと落ちていた。BOM を剥がすことは TestReadPowerShellRows の
// 「BOMを剥がす」が、合成の見本で見る。

// sourceRepo は元実装のリポジトリの場所を返す。環境変数（sourcerepo.Env）で指定した
// 場所に無ければ落とし、指定していなくて見つからなければ飛ばす（sourcerepo.Find）。
// CI には元リポジトリが無いので、飛ばせることが必須。
func sourceRepo(t *testing.T) string {
	t.Helper()
	return sourcerepo.Find(t, "Translations")
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

// contentLines は data のコメントと空行を除いた物理行を、ヘッダーとデータに分けて返す。
func contentLines(t *testing.T, data []byte) (header sourcerepo.Line, rows []sourcerepo.Line) {
	t.Helper()
	lines := sourcerepo.ContentLines(data)
	if len(lines) < 2 {
		t.Fatalf("ヘッダーとデータの行が無い（%d 行）", len(lines))
	}
	return lines[0], lines[1:]
}

// checkPlainRow は、読み手が読んだ行 row の値を、元の物理行 line をカンマで分けた値と
// 突き合わせる。引用符のある行は分け方が読み手と同じになる保証が無いので比べない。
// 比べたら true を返す。落ちたときに出すのは行番号と列名だけで、値は出さない。
func checkPlainRow(t *testing.T, columns []string, row interface{ Get(string) string }, line sourcerepo.Line) bool {
	t.Helper()
	fields, ok := sourcerepo.PlainFields(line.Text)
	if !ok {
		return false
	}
	for c, name := range columns {
		want := ""
		if c < len(fields) {
			want = fields[c]
		}
		if row.Get(name) != want {
			t.Errorf("%d行目の %s が、カンマで分けた値と違う", line.Number, name)
		}
	}
	return true
}

// TestRealDataJaStrings は公開ファイル Translations/ja/strings.csv を3方式すべてで読み、
// 同じ行数・同じ内容になることを確かめる。
func TestRealDataJaStrings(t *testing.T) {
	data := readSourceFile(t, "Translations", "ja", "strings.csv")
	columns := []string{"key", "section", "node", "order", "speaker", "translation"}
	header, lines := contentLines(t, data)
	if header.Text != strings.Join(columns, ",") {
		t.Fatalf("ヘッダーの行 = %q, want %q", header.Text, strings.Join(columns, ","))
	}

	t.Run("C#方式", func(t *testing.T) {
		rows := ReadCSharpRows(data)
		if len(rows) != len(lines) {
			t.Fatalf("行数 = %d, want %d（コメントと空行を除いた物理行）", len(rows), len(lines))
		}
		if got := rows[0].Columns(); !slices.Equal(got, columns) {
			t.Errorf("ヘッダー = %q, want %q", got, columns)
		}
		compared := 0
		for i, row := range rows {
			if checkPlainRow(t, columns, row, lines[i]) {
				compared++
			}
		}
		if compared == 0 {
			t.Error("カンマで分けて比べられた行が1つも無い")
		}
	})

	t.Run("PowerShell方式", func(t *testing.T) {
		rows, err := ReadPowerShellRows(data)
		if err != nil {
			t.Fatalf("ReadPowerShellRows が失敗した: %v", err)
		}
		if len(rows) != len(lines) {
			t.Fatalf("行数 = %d, want %d", len(rows), len(lines))
		}
	})

	t.Run("Python方式", func(t *testing.T) {
		records, err := ReadPythonRecords(data)
		if err != nil {
			t.Fatalf("ReadPythonRecords が失敗した: %v", err)
		}
		// 空白だけの行は無いので、コメントと空行を捨てたレコードは内容行と同じ数になる。
		if len(records) != len(lines)+1 {
			t.Fatalf("レコード数 = %d, want %d（ヘッダーを含む）", len(records), len(lines)+1)
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
		for i, r := range records[1:] {
			if r.Number != lines[i].Number {
				t.Fatalf("%d件目の行番号 = %d, want %d", i+1, r.Number, lines[i].Number)
			}
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
	// 実データで裏付ける。引用符付きで書き出される行の数は、元の物理行のうち
	// 引用符を含む行の数と同じになる（引用符が要るのは訳の列だけ）。
	t.Run("読み書きの往復で元の行に戻る", func(t *testing.T) {
		records, err := ReadPythonRecords(data)
		if err != nil {
			t.Fatalf("ReadPythonRecords が失敗した: %v", err)
		}
		// 各レコードの元の物理行。実データの値は1行に収まっているので、
		// レコードの先頭行がそのままその行の全体になる。
		physical := SplitPythonLines(data)
		recLines := make([]Line, 0, len(records))
		for _, r := range records {
			recLines = append(recLines, physical[r.Number-1])
		}
		rows := ReadCSharpRows(data)
		if len(recLines) != len(rows)+1 {
			t.Fatalf("物理行とレコードの数が揃わない: %d と %d", len(recLines), len(rows))
		}

		quoted, wantQuoted := 0, 0
		for _, l := range lines {
			if strings.Contains(l.Text, `"`) {
				wantQuoted++
			}
		}
		for i, row := range rows {
			fields := make([]string, 0, len(columns))
			for _, name := range columns {
				fields = append(fields, row.Get(name))
			}
			got := string(AppendLine(nil, fields...))
			want := recLines[i+1].Text
			if got != want {
				t.Fatalf("%d行目の書き戻しが元と違う\n got  %q\n want %q", recLines[i+1].Number, got, want)
			}
			if len(fields[5]) != len(EscapeField(fields[5])) {
				quoted++
			}
		}
		if quoted != wantQuoted {
			t.Errorf("引用符付きで書き出される訳文の行数 = %d, want %d（引用符を含む物理行の数）", quoted, wantQuoted)
		}
	})
}

// TestRealDataLevelFlow は data/level_flow.csv を読む。
//
// 上流の版によって BOM の有無が違う（003ed1e は BOM 付き、main は BOM 無し）。
// どちらでも、最初の列を含むすべての列を引けることを見る。BOM が残ると、最初の列
// だけが引けなくなる。
func TestRealDataLevelFlow(t *testing.T) {
	data := readSourceFile(t, "data", "level_flow.csv")
	header, lines := contentLines(t, data)
	columns, ok := sourcerepo.PlainFields(header.Text)
	if !ok {
		t.Fatalf("ヘッダーに引用符がある: %s", header.Text)
	}

	rows, err := ReadPowerShellRows(data)
	if err != nil {
		t.Fatalf("ReadPowerShellRows が失敗した: %v", err)
	}
	if len(rows) != len(lines) {
		t.Fatalf("行数 = %d, want %d（コメントと空行を除いた物理行）", len(rows), len(lines))
	}
	for i, row := range rows {
		// 列があることも見る。値が空の列（末尾カンマ）でも、列そのものは引ける。
		for _, name := range columns {
			if _, ok := row.Lookup(name); !ok {
				t.Errorf("%d行目で %s 列を引けない", lines[i].Number, name)
			}
		}
		checkPlainRow(t, columns, row, lines[i])
	}

	// C#方式でも同じ件数になる。
	if got := len(ReadCSharpRows(data)); got != len(lines) {
		t.Errorf("C#方式の行数 = %d, want %d", got, len(lines))
	}
}

// TestRealDataScriptOrder は data/script_order.csv を読む。出力順の権威になるファイル。
//
// section にはスペースが含まれる。引用符なしフィールドの空白の扱いを間違えると、
// "L01" や "L01 " に化けて、カンマで分けた値と食い違う。
func TestRealDataScriptOrder(t *testing.T) {
	data := readSourceFile(t, "data", "script_order.csv")
	header, lines := contentLines(t, data)
	columns, ok := sourcerepo.PlainFields(header.Text)
	if !ok {
		t.Fatalf("ヘッダーに引用符がある: %s", header.Text)
	}
	for _, name := range []string{"section", "phase", "node", "order", "line_id", "key", "speaker", "condition"} {
		if !slices.Contains(columns, name) {
			t.Errorf("ヘッダーに %s 列が無い", name)
		}
	}

	rows, err := ReadPowerShellRows(data)
	if err != nil {
		t.Fatalf("ReadPowerShellRows が失敗した: %v", err)
	}
	if len(rows) != len(lines) {
		t.Fatalf("行数 = %d, want %d（コメントと空行を除いた物理行）", len(rows), len(lines))
	}
	for i, row := range rows {
		checkPlainRow(t, columns, row, lines[i])
	}
	if got := len(ReadCSharpRows(data)); got != len(lines) {
		t.Errorf("C#方式の行数 = %d, want %d", got, len(lines))
	}
}

// TestRealDataWholeReaderAgrees は、元リポジトリの全ロケールの公開ファイルと
// 再生順の2ファイルを、主の読み手と行単位の読み手で読み、同じ行を返すことを
// 確かめる。
//
// 実データには行をまたぐレコードが無い。そのため、PR2 で publish・diff・order を
// 主の読み手へ切り替えても、これらのファイルでは結果が変わらない（上流 main の
// 16ロケールを dwloc publish に通すと、コミット済みのファイルとバイト一致する）。
// あわせて、飲み込み・単独の CR・改行・ゲームの読み方との食い違いの検出が、
// 実データで1件も当たらないことを見る。当たれば、正当なファイルの publish が塞がる。
//
// 落ちたときに出すのは件数と物理行の番号とキーと列名だけにする。値は出さない。
func TestRealDataWholeReaderAgrees(t *testing.T) {
	root := sourceRepo(t)
	files := [][]string{{"data", "script_order.csv"}, {"data", "level_flow.csv"}}
	// ロケールは入力から数える。少なすぎれば（Translations を読めていなければ）
	// sourcerepo.Locales が落とす。0 どうしで一致してしまうのを防ぐ。
	for _, locale := range sourcerepo.Locales(t, root) {
		files = append(files, []string{"Translations", locale, "strings.csv"})
	}
	for _, parts := range files {
		t.Run(strings.Join(parts, "/"), func(t *testing.T) {
			data := readSourceFile(t, parts...)
			f, err := ReadPowerShell(data)
			if err != nil {
				t.Fatalf("主の読み手が失敗した: %v", err)
			}
			rows, err := ReadPowerShellRows(data)
			if err != nil {
				t.Fatalf("行単位の読み手が失敗した: %v", err)
			}
			if len(f.Records) != len(rows) {
				t.Fatalf("件数: 主の読み手 %d、行単位 %d", len(f.Records), len(rows))
			}
			for i, r := range f.Records {
				if r.MultiLine() {
					t.Errorf("行をまたぐレコードがある: %d〜%d行目", r.Line, r.EndLine)
				}
				for _, col := range r.Columns() {
					if r.Get(col) != rows[i].Get(col) {
						t.Errorf("%d行目（key %s）の %s が行単位の読み手と違う", r.Line, r.Get("key"), col)
					}
				}
			}
			checkSegmentInvariants(t, string(data), f.Segments)
			if got := FindSwallows(f.Segments); got != nil {
				t.Errorf("飲み込みと見なされた: %+v", got)
			}
			if got := FindCRCuts(f.Segments); got != nil {
				t.Errorf("単独の CR で切れた値と見なされた: %+v", got)
			}
			if got := LoneCRValues(f); got != nil {
				t.Errorf("単独の CR を含む値がある: %+v", got)
			}
			if got := LineBreakValues(f); got != nil {
				t.Errorf("改行を含む値がある: %+v", got)
			}
			if got := CSharpDisagreements(f); got != nil {
				t.Errorf("ゲームの読み方と割れる: %+v", got)
			}
			t.Logf("%d 件", len(f.Records))
		})
	}
}

// workingCopyEnv は、実物の作業コピー（ゲーム側の
// Translations/_discovered/<ロケール>.working.csv）を渡す環境変数。
const workingCopyEnv = "DWLOC_WORKING_COPY"

// TestRealWorkingCopy は、実物の作業コピーを全体を解釈する読み手で読み、区切りの
// 関数と検出の関数が約束どおりに振る舞うかを確かめる。
//
// 作業コピーはゲームのフォルダーにしか無く（元リポジトリでは .gitignore で外して
// ある）、ゲームの英語の原文を含む。CI には無いので、DWLOC_WORKING_COPY で渡した
// ときだけ走る。ファイルは読むだけで、出すのは件数・物理行の番号・キーだけにする。
//
//	DWLOC_WORKING_COPY=<ゲーム>/BepInEx/plugins/DragNWashLocalization/Translations/_discovered/ja.working.csv \
//	  go test ./internal/csvfile -run RealWorkingCopy -v
//
// 実物には、原文が行をまたぐ（空行で段落を分けた）レコードがある。行単位の読み方では
// そのレコードが2件の行に割れるので、行単位の件数は1件多くなる。
func TestRealWorkingCopy(t *testing.T) {
	path := os.Getenv(workingCopyEnv)
	if path == "" {
		t.Skipf("%s が無いので、実物の作業コピーの確かめは飛ばす", workingCopyEnv)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s が読めない: %v", path, err)
	}
	f, err := ReadPowerShell(data)
	if err != nil {
		t.Fatalf("主の読み手が失敗した: %v", err)
	}
	checkSegmentInvariants(t, string(data), f.Segments)

	multi := 0
	for _, r := range f.Records {
		if r.MultiLine() {
			multi++
			t.Logf("行をまたぐレコード: %d〜%d行目（key %s、ID %d）", r.Line, r.EndLine, r.Get("key"), r.ID)
		}
	}
	if got := FindSwallows(f.Segments); got != nil {
		t.Errorf("飲み込みと見なされた: %+v", got)
	}
	if got := FindCRCuts(f.Segments); got != nil {
		t.Errorf("単独の CR で切れた値と見なされた: %+v", got)
	}
	if got := LoneCRValues(f); got != nil {
		t.Errorf("単独の CR を含む値がある: %+v", got)
	}
	if got := CSharpDisagreements(f); got != nil {
		t.Errorf("ゲームの読み方と割れる: %+v", got)
	}
	if game := ReadCSharpRows(data); len(game) != len(f.Records) {
		t.Errorf("件数: 主の読み手 %d、ゲームの読み方 %d", len(f.Records), len(game))
	}
	rows, err := ReadPowerShellRows(data)
	if err != nil {
		t.Fatalf("行単位の読み手が失敗した: %v", err)
	}
	t.Logf("物理行 %d、セグメント %d、レコード %d（行をまたぐもの %d）、行単位の読み手の行 %d",
		f.Segments.Lines, len(f.Segments.List), len(f.Records), multi, len(rows))
}
