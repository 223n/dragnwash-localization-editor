package main

import (
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// formulaTree は、表計算が式と読む値を訳に持つリポジトリを作る。
//
// 訳は再生順に無い行（台本から消えた行）に置き、diff の csv に必ず出るようにする。
// 作業コピーの無いロケールでは、ほかの人が Pull Request で入れた公開ファイルの訳が
// そのまま csv に出る。
func formulaTree(t *testing.T, translation string) string {
	t.Helper()

	gone := key.For("A line that was removed")
	return makeTree(t, map[string]string{
		"data/script_order.csv": diffOrderCSV,
		"Translations/ja/strings.csv": publish.HeaderLine + "\n" +
			diffHelloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
			gone + ",L01 Ryan,Ryan_9_gone,3,Ryan," + translation + "\n",
	})
}

// csvRowWith は csv の出力から key を含む行を返す。無ければ試験を落とす。
func csvRowWith(t *testing.T, stdout, needle string) string {
	t.Helper()

	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("%q を含む行が無い:\n%s", needle, stdout)
	return ""
}

// TestRunDiffCSVDefusesFormulas は、diff の csv で、表計算が式と読む値の頭に ' を
// 付けることを見る。
//
// 使い方は csv を「表計算にそのまま貼れます」と案内している。先頭が = + - @ の値や、
// タブと CR で始まる値は、表計算に貼ると式として評価される。訳は公開ファイルから
// 来るので、ほかの人が Pull Request で入れた訳が、開いた人の画面で式になる。
// 悪意が無くても、台詞のダッシュで始まる訳は #NAME? などに化ける。
func TestRunDiffCSVDefusesFormulas(t *testing.T) {
	tests := []struct {
		name        string
		translation string
		// want は csv の行に出てほしい値の書き方（引用が要るものは引用込み）。
		want string
	}{
		{name: "等号", translation: "=1+1", want: ",'=1+1,"},
		{name: "正符号", translation: "+81 の番号", want: ",'+81 の番号,"},
		{name: "負符号", translation: "-では、また", want: ",'-では、また,"},
		{name: "単価記号", translation: "@みんな", want: ",'@みんな,"},
		// タブと CR で始まる値は、公開ファイルを読む段で前後の空白として落ちるので、
		// ここでは作れない。internal/diff の TestDefuseFormula で見る。
		{
			// 引用が要る値でも、' は引用の内側に入る。外側に付けると CSV が壊れる。
			name:        "引用が要る式",
			translation: `"=HYPERLINK(""https://example.invalid/"",""open"")"`,
			want:        `,"'=HYPERLINK(""https://example.invalid/"",""open"")",`,
		},
		{name: "ふつうの訳は変えない", translation: "こんにちは、また", want: ",こんにちは、また,"},
		{name: "途中の等号は変えない", translation: "1+1=2", want: ",1+1=2,"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := formulaTree(t, tt.translation)
			_, stdout, stderr := runCLI("diff", "--root", root, "--no-game", "--format", "csv")
			row := csvRowWith(t, stdout, "Ryan_9_gone")
			if !strings.Contains(row, tt.want) {
				t.Errorf("行 = %q、%q を含むことを期待\nstderr:\n%s", row, tt.want, stderr)
			}
		})
	}
}

// TestRunDiffRawCSVKeepsValues は、--raw-csv を付けると値に手を加えないことを見る。
//
// 機械と突き合わせる使い方では、元の値そのものが要る。頭に ' が付くと、公開
// ファイルの訳と一致しなくなる。
func TestRunDiffRawCSVKeepsValues(t *testing.T) {
	root := formulaTree(t, "=1+1")
	code, stdout, stderr := runCLI("diff", "--root", root, "--no-game", "--format", "csv", "--raw-csv")
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\n%s", code, exitProblems, stderr)
	}
	if row := csvRowWith(t, stdout, "Ryan_9_gone"); !strings.Contains(row, ",=1+1,") {
		t.Errorf("--raw-csv なのに値が変わった: %q", row)
	}
}

// TestRunDiffRawCSVNeedsCSV は、--raw-csv を text 形式と一緒に打つと止まることを見る。
// 効かない指定を黙って受けると、付けたつもりで効いていない事故になる。
func TestRunDiffRawCSVNeedsCSV(t *testing.T) {
	root := formulaTree(t, "=1+1")
	code, stdout, stderr := runCLI("diff", "--root", root, "--no-game", "--raw-csv")
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\n%s", code, exitError, stderr)
	}
	if stdout != "" {
		t.Errorf("止めたのに標準出力へ書いている:\n%s", stdout)
	}
	checkContains(t, "標準エラー", stderr, []string{"--raw-csv", "--format csv"})
}
