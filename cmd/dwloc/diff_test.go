package main

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// diff のテストで使うキー。ハッシュを直書きせず internal/key で計算するのは、
// 計算の仕様が変わったときにテストだけが古いまま通らないようにするため。
var (
	diffHelloKey = key.For("Hello")   // 再生順にも公開ファイルにもある
	diffByeKey   = key.For("Goodbye") // 再生順にだけある（どのロケールにも訳が無い）
	diffGoneKey  = key.For("Gone")    // 公開ファイルにだけある。section が UI でない
	diffGapKey   = key.For("Gap")     // 公開ファイルにだけある。UI だが話者名が残る
	diffUIKey    = key.For("Setting") // 公開ファイルにだけある。UI 文言と見分けられない
)

// diffOrderCSV は "Hello" と "Goodbye" を1回ずつ再生する最小の再生順。
var diffOrderCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
	"L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf," + diffHelloKey + ",Ryan,\n" +
	"L01 Ryan,intro,Ryan_1_intro,2,line:b0000001," + diffByeKey + ",Ryan,\n"

// diffPublishedJA は要確認のカテゴリを1件ずつ含む公開ファイル。
// 並びは publish が書く形（HeaderLine の6列）と同じ。
var diffPublishedJA = publish.HeaderLine + "\n" +
	diffHelloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
	diffGoneKey + ",L01 Ryan,Ryan_9_gone,3,Ryan,きえたはず\n" +
	diffGapKey + ",UI,,,Conrad,のこった台詞\n" +
	diffUIKey + ",UI,,,UI,設定\n" +
	"line:ffffffff,UI,,,UI,はじめまして\n"

// diffPublishedDE は "Hello" だけを訳した公開ファイル。
// ja が持つ他のハッシュキーを持たないので「他のロケールにあって無い行」が出る。
var diffPublishedDE = publish.HeaderLine + "\n" +
	diffHelloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hallo\n"

// diffCleanJA は再生順とぴったり合っている公開ファイル。要確認が出ない形。
var diffCleanJA = publish.HeaderLine + "\n" +
	diffHelloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n"

// diffTree は diff を試すための最小のリポジトリを作る。
func diffTree(t *testing.T, files map[string]string) string {
	t.Helper()

	all := map[string]string{"data/script_order.csv": diffOrderCSV}
	for rel, content := range files {
		all[rel] = content
	}
	return makeTree(t, all)
}

func TestRunDiffReportsReviewAndExitsOne(t *testing.T) {
	root := diffTree(t, map[string]string{
		"Translations/ja/strings.csv": diffPublishedJA,
		"Translations/de/strings.csv": diffPublishedDE,
	})

	code, stdout, stderr := runCLI("diff", "--root", root, "--locale", "ja")
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	if stderr != "" {
		t.Errorf("標準エラーへ何か出ている:\n%s", stderr)
	}

	checkContains(t, "stdout", stdout, []string{
		// 比較のため、報告しないロケール（de）も読んでいることを伝える1行。
		"2 ロケールを読みました",
		"data/script_order.csv",
		"ja  Translations/ja/strings.csv",
		// 要確認の2カテゴリが1件ずつ。
		"台本から消えた行", "台本に無い台詞ID行",
		diffGoneKey, "きえたはず",
		"要確認が 2 行あります。",
		// 参考は件数と理由だけで、既定では一覧にしない。
		"台本に無い台詞行", "（--all で一覧）",
		// 作業コピーが無いので、0 件ではなく「判定できません」と書く。
		"判定していません（作業コピーがありません）",
	})
	// ja は報告の対象だけ。絞ったロケールの見出しが混ざっていないこと。
	if strings.Contains(stdout, "\nde  ") {
		t.Errorf("--locale ja なのに de の報告が出ている:\n%s", stdout)
	}
}

func TestRunDiffLocaleGapAppearsForOtherLocale(t *testing.T) {
	// de には ja が持つハッシュキーが無い。母集合は常に全ロケールから作るので、
	// --locale de と絞っても ja の存在を根拠にした報告が出る。
	root := diffTree(t, map[string]string{
		"Translations/ja/strings.csv": diffPublishedJA,
		"Translations/de/strings.csv": diffPublishedDE,
	})

	code, stdout, stderr := runCLI("diff", "--root", root, "--locale", "de", "--all")
	if code != exitOK {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
	}
	checkContains(t, "stdout", stdout, []string{"他のロケールにあって無い行", diffGoneKey})
	if !strings.Contains(stdout, "要確認はありません。") {
		t.Errorf("de には要確認が無いはずなのに、そう書かれていない:\n%s", stdout)
	}
}

func TestRunDiffCleanTreeExitsZero(t *testing.T) {
	root := diffTree(t, map[string]string{"Translations/ja/strings.csv": diffCleanJA})

	code, stdout, stderr := runCLI("diff", "--root", root)
	if code != exitOK {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
	}
	checkContains(t, "stdout", stdout, []string{
		"要確認はありません。",
		// 再生順にあってどのロケールにも訳が無い "Goodbye" は参考どまり。
		"どのロケールにも訳が無い行", "1 件",
	})
}

func TestRunDiffWorkingCopy(t *testing.T) {
	// 作業コピーがあると未翻訳を数えられる。既定は 0 のままで、--strict で 1 になる。
	root := diffTree(t, map[string]string{
		"Translations/ja/strings.csv":                 diffCleanJA,
		"Translations/_discovered/ja.working.csv":     "key,source_en,translation\n" + diffHelloKey + ",Hello,こんにちは\n" + diffByeKey + ",Goodbye,\n",
		"Translations/_discovered/.gitkeep-not-a-csv": "",
	})

	code, stdout, stderr := runCLI("diff", "--root", root)
	if code != exitOK {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
	}
	checkContains(t, "stdout", stdout, []string{
		"作業コピー  Translations/_discovered/ja.working.csv",
		"未翻訳", diffByeKey, "Goodbye",
	})
	if strings.Contains(stdout, "判定できません") {
		t.Errorf("作業コピーがあるのに判定できないと書かれている:\n%s", stdout)
	}

	code, _, stderr = runCLI("diff", "--root", root, "--strict")
	if code != exitProblems {
		t.Fatalf("--strict の終了コード = %d, 期待 %d\nstderr:\n%s", code, exitProblems, stderr)
	}

	// --no-working は作業コピーがあっても読まない。未翻訳は判定できなくなる。
	code, stdout, _ = runCLI("diff", "--root", root, "--no-working", "--strict")
	if code != exitOK {
		t.Fatalf("--no-working の終了コード = %d, 期待 %d\nstdout:\n%s", code, exitOK, stdout)
	}
	checkContains(t, "stdout", stdout, []string{
		"読みませんでした",
		"判定していません（作業コピーを読んでいません）",
	})
	if strings.Contains(stdout, "Goodbye") {
		t.Errorf("--no-working なのに作業コピーの原文が出ている:\n%s", stdout)
	}
	// 実在する作業コピーを「ありません」と言ってはいけない。読まなかっただけで、
	// 利用者は既に持っている。ゲーム内で書き出し直させることになる。
	if strings.Contains(stdout, "ありません（Translations/_discovered/ja.working.csv") {
		t.Errorf("実在する作業コピーを「ありません」と書いている:\n%s", stdout)
	}
	if strings.Contains(stdout, "ゲーム内で作業コピーを書き出すと") {
		t.Errorf("既にある作業コピーの書き出しを促している:\n%s", stdout)
	}
}

func TestRunDiffDroppedRowIsReview(t *testing.T) {
	// publish が黙って捨てる行。訳が消える唯一の経路なので、既定で終了コード1。
	root := diffTree(t, map[string]string{
		"Translations/ja/strings.csv":             diffCleanJA,
		"Translations/_discovered/ja.working.csv": "key,source_en,translation\nEnglish text,,訳した文\n",
	})

	code, stdout, stderr := runCLI("diff", "--root", root)
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	checkContains(t, "stdout", stdout, []string{"publish で捨てられる行", "訳した文", "要確認が 1 行あります。"})
}

func TestRunDiffCSVFormat(t *testing.T) {
	root := diffTree(t, map[string]string{"Translations/ja/strings.csv": diffPublishedJA})

	code, stdout, stderr := runCLI("diff", "--root", root, "--format", "csv")
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstderr:\n%s", code, exitProblems, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if lines[0] != "locale,category,status,key,section,node,order,speaker,source_en,translation,note" {
		t.Errorf("1行目 = %q", lines[0])
	}
	checkContains(t, "stdout", stdout, []string{
		"ja,vanished,review," + diffGoneKey,
		"ja,stray_line_id,review,line:ffffffff",
		// 参考のカテゴリも CSV には全件入る（--all は text 形式の指定）。
		"ja,unknown_origin,info," + diffUIKey,
	})
	if strings.Contains(stdout, "要確認") {
		t.Errorf("CSV に人向けの文面が混ざっている:\n%s", stdout)
	}
}

func TestRunDiffArgumentErrors(t *testing.T) {
	root := diffTree(t, map[string]string{"Translations/ja/strings.csv": diffCleanJA})

	tests := []struct {
		name       string
		args       []string
		wantStderr []string
	}{
		{
			name:       "知らないロケールはエラーにする",
			args:       []string{"diff", "--root", root, "--locale", "xx"},
			wantStderr: []string{"--locale に指定したロケールがありません", "xx", "対象にできるのは ja"},
		},
		{
			name:       "知らない形式はエラーにする",
			args:       []string{"diff", "--root", root, "--format", "json"},
			wantStderr: []string{"--format は text か csv です"},
		},
		{
			name:       "負の上限はエラーにする",
			args:       []string{"diff", "--root", root, "--limit", "-1"},
			wantStderr: []string{"--limit は 0 以上です"},
		},
		{
			name:       "余分な引数はエラーにする",
			args:       []string{"diff", "--root", root, "ja"},
			wantStderr: []string{"余分な引数です", "使い方は dwloc diff --help で表示します。"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(tt.args...)
			if code != exitError {
				t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
			}
			checkContains(t, "stderr", stderr, tt.wantStderr)
		})
	}
}

// TestRunDiffWarnsWhenOrderIsUnreadable は、再生順を読めないときに必ず標準エラーで
// 断ることを見る。
//
// 再生順が読めないと「再生順に無い」を根拠にするカテゴリがどれも成り立たない。
// internal/diff はその判定を止めるが、止めたこと自体は csv の本体に出ない。
// 断らないと、csv を読む人には「台本から消えた行は 0 件」に見える。
//
// 終了コードは 0 のまま。判定を止めたぶんを要確認に数えると、公開ファイルの
// 全行が「台本から消えた行」になって CI が赤くなる。
func TestRunDiffWarnsWhenOrderIsUnreadable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{
			name:  "再生順のファイルが無い",
			files: map[string]string{"Translations/ja/strings.csv": diffCleanJA},
		},
		{
			// 行はあるのに key 列を引けない。行数だけ見ていると読めたように見える。
			name: "再生順に key 列が無い",
			files: map[string]string{
				"data/script_order.csv": "section,phase,node,order,line_id,speaker\n" +
					"L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf,Ryan\n",
				"Translations/ja/strings.csv": diffCleanJA,
			},
		},
	} {
		for _, format := range []string{diffFormatText, diffFormatCSV} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				root := makeTree(t, tc.files)

				code, stdout, stderr := runCLI("diff", "--root", root, "--format", format)
				if code != exitOK {
					t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
				}
				// パスはルートからの相対で出す。絶対パスには利用者名が入りうる。
				checkContains(t, "stderr", stderr,
					[]string{"警告: data/script_order.csv から再生順を読めません", "判定しません"})
				if strings.Contains(stderr, filepath.ToSlash(root)) || strings.Contains(stderr, root) {
					t.Errorf("絶対パスが出ている:\n%s", stderr)
				}
				if format == diffFormatCSV && strings.Contains(stdout, "警告") {
					t.Errorf("csv 本体に人向けの文面が混ざっている:\n%s", stdout)
				}
			})
		}
	}
}

// TestRunDiffNamesEmptyLocales は、公開ファイルも作業コピーも無いロケールを
// 名前で伝えることを見る。
//
// publish はそのロケールを対象にしない（書き出す元が無い）。この道具まで黙ると、
// 「訳が1件も無い」という最大の要作業が見えなくなる。csv の本体には書く場所が
// 無いので、標準エラーへ出す。
func TestRunDiffNamesEmptyLocales(t *testing.T) {
	root := diffTree(t, map[string]string{
		"Translations/ja/strings.csv": diffCleanJA,
		"Translations/de/.keep":       "",
	})

	for _, format := range []string{diffFormatText, diffFormatCSV} {
		code, stdout, stderr := runCLI("diff", "--root", root, "--format", format)
		if code == exitError {
			t.Fatalf("%s: 終了コード = %d\nstdout:\n%s\nstderr:\n%s", format, code, stdout, stderr)
		}
		checkContains(t, format+" の stderr", stderr, []string{"訳が1件もないロケールがあります: de"})
	}

	// そのロケールを --locale に書いても「ありません」とは言わない。
	// ディレクトリは実在するので、そう言うと嘘になる。
	code, stdout, stderr := runCLI("diff", "--root", root, "--locale", "de")
	if code == exitError {
		t.Fatalf("--locale de の終了コード = %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if strings.Contains(stderr, "--locale に指定したロケールがありません") {
		t.Errorf("実在するロケールを無いと言っている:\n%s", stderr)
	}
	checkContains(t, "--locale de の stdout", stdout, []string{"訳が1件もないロケール: de"})
}

// TestRunDiffOnlyEmptyLocales は、ロケールのディレクトリはあるのに比べる中身が
// 1つも無いときに、終了コード2で止まることを見る。
//
// 「0 件」と書いて成功で終わると、何も比べていないことに気づけない。
// 止める前に、どのロケールが空なのかも伝える。
func TestRunDiffOnlyEmptyLocales(t *testing.T) {
	root := diffTree(t, map[string]string{"Translations/de/.keep": ""})

	code, stdout, stderr := runCLI("diff", "--root", root)
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
	}
	checkContains(t, "stderr", stderr, []string{
		"訳が1件もないロケールがあります: de",
		"対象になるロケールがありません: Translations",
	})
	if stdout != "" {
		t.Errorf("失敗したのに標準出力へ書いている:\n%s", stdout)
	}
}

// TestRunDiffFileErrorIsRelative は、読めない公開ファイルをルートからの相対パスで
// 伝えることを見る。
//
// internal/diff は表示の基準を知らないので、パスを持ったままエラーを返す。
// そのまま出すと、手元の絶対パス（利用者名が入りうる）が CI のログや不具合報告へ
// 漏れる。
func TestRunDiffFileErrorIsRelative(t *testing.T) {
	root := diffTree(t, map[string]string{
		// 列名が重複したヘッダー。読み始めて初めて分かる壊れ方。
		"Translations/ja/strings.csv": "key,key,node,order,speaker,translation\n" +
			diffHelloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n",
	})

	code, stdout, stderr := runCLI("diff", "--root", root)
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
	}
	checkContains(t, "stderr", stderr, []string{"dwloc: Translations/ja/strings.csv: "})
	if strings.Contains(stderr, filepath.ToSlash(root)) || strings.Contains(stderr, root) {
		t.Errorf("絶対パスが出ている:\n%s", stderr)
	}
}

// failWriter は書き込みを必ず断る io.Writer。閉じたパイプへ書いたときの代わり。
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("パイプが閉じています")
}

// TestRunDiffReportsWriteFailure は、結果を書き出せなかったときに終了コード2を
// 返すことを見る。
//
// ここで報告の終了コード（0 か 1）を返すと、CI は結果を1行も受け取っていないのに
// 「要確認なし」と読む。
func TestRunDiffReportsWriteFailure(t *testing.T) {
	root := diffTree(t, map[string]string{"Translations/ja/strings.csv": diffCleanJA})

	for _, format := range []string{diffFormatText, diffFormatCSV} {
		var errOut bytes.Buffer
		code := run([]string{"diff", "--root", root, "--format", format}, failWriter{}, &errOut)
		if code != exitError {
			t.Errorf("%s: 終了コード = %d, 期待 %d\n%s", format, code, exitError, errOut.String())
		}
		checkContains(t, format+" の stderr", errOut.String(),
			[]string{"結果を書き出せません", "パイプが閉じています"})
	}
}

// TestRunDiffCSVWarnsWhenOldOrderLooksStale は、読めた1つ前の再生順が「いまの版と
// 同じ」に見えるとき、csv では標準エラーで保留を伝えることを見る。
//
// csv は Finding を並べるだけなので、「候補が0件」と「判定していない」が同じ姿
// （carryover の行が無い）になる。黙っていると「移すべき訳は無い」と読まれ、
// 消えた行の訳を捨てる判断に直結する。text 形式は本文に書くので重ねない。
func TestRunDiffCSVWarnsWhenOldOrderLooksStale(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無いので飛ばす")
	}
	// 台本から消えた行（diffGoneKey）がある公開ファイル。旧版と新版の台詞IDとキーの
	// 対応が同じなのに消えた行がある、という辻褄の合わない形を作る。
	root := diffTree(t, map[string]string{"Translations/ja/strings.csv": diffPublishedJA})
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v が失敗したので飛ばす: %v (%s)", args, err, out)
		}
	}
	gitRun("init")
	gitRun("add", ".")
	gitRun(append(gitTestOpts(), "commit", "-m", "first")...)
	// 話者の列だけを変えてコミットする。再生順は変わったが、台詞IDとキーの対応は
	// 同じまま。1つ前の版として読まれるのは first の再生順になる。
	speakerChanged := strings.ReplaceAll(diffOrderCSV, ",Ryan,\n", ",Kobold,\n")
	if err := os.WriteFile(filepath.Join(root, "data", "script_order.csv"),
		[]byte(speakerChanged), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun("add", ".")
	gitRun(append(gitTestOpts(), "commit", "-m", "speaker only")...)

	code, stdout, stderr := runCLI("diff", "--root", root, "--format", "csv")
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	// 報告するのは ja だけなので、ロケール名は添えない（全ロケールが同じ理由）。
	// 台詞IDは読めているので、台本に無い台詞ID行は止めていない。
	checkContains(t, "stderr", stderr, []string{
		"警告: 「引き継ぎ候補」は判定しません（読めた1つ前の再生順が、いまの版と同じ内容に見えます）。",
	})
	// 締めの1行は、ほかに止めたカテゴリ（作業コピーが無いので未翻訳など）と一緒に並べる。
	if !slices.Contains(heldTail(t, stderr), "carryover") {
		t.Errorf("carryover の行が無いことを 0 件と読ませない断りが無い:\n%s", stderr)
	}
	if strings.Contains(stderr, "台本に無い台詞ID行") {
		t.Errorf("判定している台本に無い台詞ID行を止めたと書いている:\n%s", stderr)
	}
	if strings.Contains(stdout, ",carryover,") {
		t.Errorf("判定していないのに carryover の行がある:\n%s", stdout)
	}

	// text 形式は本文に理由を書くので、標準エラーへ重ねない。
	code, stdout, stderr = runCLI("diff", "--root", root)
	if code != exitProblems {
		t.Fatalf("text の終了コード = %d, 期待 %d\nstderr:\n%s", code, exitProblems, stderr)
	}
	if stderr != "" {
		t.Errorf("text 形式で標準エラーへ何か出ている:\n%s", stderr)
	}
	checkContains(t, "stdout", stdout, []string{"判定していません（読めた1つ前の再生順が、いまの版と同じ内容に見えます"})
}

// TestRunDiffCSVWarnsWhenOrderHasNoLineIDs は、いまの再生順に台詞IDが無いせいで
// 引き継ぎ候補を保留したときも、csv では標準エラーで伝えることを見る。
//
// 1つ前の再生順は読めているので、旧再生順の有無だけを見ていると警告が出ない。
// 再生順のキーは読めているので、再生順を読めないという警告も出ない。そのまま
// だと csv には台本から消えた行だけが並び、carryover の行が無いことが
// 「引き継ぎ先は無い」と読まれ、翻訳者は消えた行の訳を捨てる。
func TestRunDiffCSVWarnsWhenOrderHasNoLineIDs(t *testing.T) {
	root := diffCarryTree(t)
	// ゲーム更新のあとの再生順から、台詞IDだけが抜けた形。まだコミットしない。
	noLineIDs := "section,phase,node,order,line_id,key,speaker,condition\n" +
		"L01 Ryan,intro,Ryan_1_intro,1,," + diffHelloKey + ",Ryan,\n" +
		"L01 Ryan,intro,Ryan_1_intro,2,," + diffCarryToKey + ",Ryan,\n"
	if err := os.WriteFile(filepath.Join(root, "data", "script_order.csv"), []byte(noLineIDs), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runCLI("diff", "--root", root, "--format", "csv")
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	// 前提: 台本から消えた行はキーだけで判定できるので、csv に出ている。
	if !strings.Contains(stdout, "ja,vanished,review,"+diffCarryFromKey+",") {
		t.Fatalf("前提が崩れている: 台本から消えた行が無い:\n%s", stdout)
	}
	if strings.Contains(stdout, ",carryover,") {
		t.Errorf("判定していないのに carryover の行がある:\n%s", stdout)
	}
	// 台本に無い台詞ID行も同じ理由で止まるので、同じ1行に入る。
	checkContains(t, "stderr", stderr, []string{
		"警告: 「引き継ぎ候補」と「台本に無い台詞ID行」は判定しません（再生順に台詞ID (line_id) がありません）。",
	})
	for _, id := range []string{"carryover", "stray_line_id"} {
		if !slices.Contains(heldTail(t, stderr), id) {
			t.Errorf("%s の行が無いことを 0 件と読ませない断りが無い:\n%s", id, stderr)
		}
	}
	if n := strings.Count(stderr, "「引き継ぎ候補」"); n != 1 {
		t.Errorf("保留を %d 回書いている:\n%s", n, stderr)
	}
	// csv には text 形式の見出しが無いので、理由の文面だけで言い分ける。キーは
	// 読めていて、台本から消えた行は csv に出ている。「再生順を読めていません」と
	// 書くと、その行まで当てにならないように読める。
	if strings.Contains(stderr, "再生順を読めていません") {
		t.Errorf("キーは読めているのに、再生順を丸ごと読めていないように書いている:\n%s", stderr)
	}

	// text 形式は本文に理由を書くので、標準エラーへ重ねない。
	code, stdout, stderr = runCLI("diff", "--root", root)
	if code != exitProblems {
		t.Fatalf("text の終了コード = %d, 期待 %d\nstderr:\n%s", code, exitProblems, stderr)
	}
	if stderr != "" {
		t.Errorf("text 形式で標準エラーへ何か出ている:\n%s", stderr)
	}
	checkContains(t, "stdout", stdout, []string{"判定していません（再生順に台詞ID (line_id) がありません）"})
	if strings.Contains(stdout, "再生順を読めていません") {
		t.Errorf("キーは読めているのに、本文が再生順を丸ごと読めていないように書いている:\n%s", stdout)
	}
}

// TestRunDiffCSVWarnsWhenStrayLineIDIsHeld は、再生順に台詞IDが無いせいで
// 「台本に無い台詞ID行」を保留したときも、csv では標準エラーで伝えることを見る。
//
// 公開ファイルの line: の行は、再生順の台詞IDと突き合わせて初めて「台本に無い」と
// 言える。台詞IDが1件も無ければ判定を止めるが、csv は Finding を並べるだけなので、
// stray_line_id の行が無いことが「台本に無い台詞ID行は無い」と読まれる。publish は
// その行を末尾のブロックへ回すので、見落とすと置き場所のずれた訳がそのまま出ていく。
// 引き継ぎ候補の保留だけを書いていたころは、ここで何も言わなかった。
//
// 名指しするカテゴリは、text 形式の見出しから拾って照合する。CLI 側で名前を
// 並べて持つと、台詞IDを要るカテゴリが増減したときに警告だけが古いまま残る。
func TestRunDiffCSVWarnsWhenStrayLineIDIsHeld(t *testing.T) {
	// 再生順の line_id 列だけを空にした形。キーは読めるので、再生順を読めないという
	// 警告は出ない。git リポジトリにもしない。台詞IDの有無で決まることだけを見るため。
	noLineIDs := strings.NewReplacer(",line:a8779ebf,", ",,", ",line:b0000001,", ",,").Replace(diffOrderCSV)
	root := diffTree(t, map[string]string{
		"data/script_order.csv": noLineIDs,
		// line:ffffffff は再生順に無い台詞ID。台詞IDを読めていれば stray_line_id に出る。
		"Translations/ja/strings.csv": diffPublishedJA,
	})

	code, stdout, stderr := runCLI("diff", "--root", root)
	if code != exitProblems {
		t.Fatalf("text の終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	// text 形式は本文に書くので、標準エラーへ重ねない。
	if stderr != "" {
		t.Errorf("text 形式で標準エラーへ何か出ている:\n%s", stderr)
	}
	const head, tail = "再生順に台詞ID (line_id) がありません。", "は判定しません。"
	_, rest, ok := strings.Cut(stdout, head)
	names, _, ok2 := strings.Cut(rest, tail)
	if !ok || !ok2 || !strings.Contains(names, "「台本に無い台詞ID行」") {
		t.Fatalf("前提が崩れている: 見出しが台本に無い台詞ID行を名指ししていない:\n%s", stdout)
	}

	code, stdout, stderr = runCLI("diff", "--root", root, "--format", "csv")
	if code != exitProblems {
		t.Fatalf("csv の終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	// 前提: 台本から消えた行はキーだけで判定できるので、csv に出ている。
	if !strings.Contains(stdout, "ja,vanished,review,"+diffGoneKey+",") {
		t.Fatalf("前提が崩れている: 台本から消えた行が無い:\n%s", stdout)
	}
	if strings.Contains(stdout, ",stray_line_id,") {
		t.Errorf("判定していないのに stray_line_id の行がある:\n%s", stdout)
	}
	// 同じ理由で止めたカテゴリは1行にまとめる。報告するのは ja だけなので、
	// ロケール名は添えない。
	checkContains(t, "stderr", stderr, []string{
		"dwloc: 警告: " + names + "は判定しません（再生順に台詞ID (line_id) がありません）。\n",
		"stray_line_id",
	})
	// 作業コピーが無いので、ほかの理由の行（未翻訳など）も並ぶ。台詞IDの理由は1行だけ。
	if n := strings.Count(stderr, "は判定しません（再生順に台詞ID (line_id) がありません）"); n != 1 {
		t.Errorf("台詞IDの保留を %d 行に分けて書いている:\n%s", n, stderr)
	}
	if strings.Contains(stdout, "警告") {
		t.Errorf("csv 本体に人向けの文面が混ざっている:\n%s", stdout)
	}
}

// TestRunDiffCSVWarnsForEveryHeldCategory は、csv 形式の標準エラーが、text 形式の
// 本文が「判定していません」と書くカテゴリを、同じロケール・同じ理由ですべて
// 名指しすることを見る。
//
// csv は Finding を1行ずつ並べるだけなので、「0件だった」と「判定していない」が
// 同じ姿（行が無い）になる。作業コピーの無い CI では、未翻訳も publish で捨てられる
// 行も原文とタグが違う行も判定しないが、台詞IDを要るカテゴリだけを書いていた
// ころは、これを黙っていた。使い方の「無ければ、その判定だけを『判定できません』と
// 伝えます」が text 形式でしか成り立っていなかった。
//
// 照合の相手を text 形式の本文にするのは、名指しを試験に書き写さないためである。
// どのカテゴリが何を要るかを試験の側で並べると、カテゴリや判定の材料が増えたとき、
// 試験だけが古いまま通る。
//
// 再生順を読めないときの「再生順を読めていません」だけは、理由の行の照合から外す。
// runDiff が先に「再生順を読めません。台本から消えた行などは判定しません」と
// 書いていて、同じ理由を2度書かないためである。締めの1行の照合からは外さない。
// runDiff の警告が名指しするのは「台本から消えた行など」までで、表計算で
// not_published や script_gap を絞る人は、締めの識別子を見て初めて「0 件ではない」と
// 分かる。
func TestRunDiffCSVWarnsForEveryHeldCategory(t *testing.T) {
	const layout = "source_en,translation,axis,required_px,available_px,ratio,object_path\n" +
		"Hello,こんにちは,x,240,180,1.33,Canvas/Label\n"
	working := "key,source_en,translation\n" + diffHelloKey + ",Hello,こんにちは\n"

	tests := []struct {
		name  string
		files map[string]string
		// noOrder は data/script_order.csv を置かない。置くと diffTree の再生順が入る。
		noOrder bool
		args    []string
		// mustHold は、csv の標準エラーが必ず名指しするロケールとカテゴリの識別子。
		// 本文との照合は、両方から何も拾えなくても通ってしまうので、拾えることを別に見る。
		mustHold map[string][]string
		// judged は、判定しているので名指ししてはいけないロケールとカテゴリの識別子。
		judged map[string][]string
		// orderHeld は、本文が「再生順を読めていません」で止めたロケールとカテゴリの
		// 識別子。理由の行では名指しせず（runDiff の再生順の警告に任せる）、締めの
		// 1行には並べる。本文との照合は、両方から何も拾えなくても通ってしまうので、
		// 拾えることを別に見る。
		orderHeld map[string][]string
	}{
		{
			// CI で普通の形。作業コピーも、はみ出しの記録も、git の履歴も無い。
			name:  "作業コピーもはみ出しの記録も無い",
			files: map[string]string{"Translations/ja/strings.csv": diffCleanJA},
			mustHold: map[string][]string{"ja": {
				"untranslated", "carry_from", "dropped", "tag_mismatch", "layout_risk", "carryover",
			}},
			judged: map[string][]string{"ja": {"vanished", "stray_line_id", "locale_gap"}},
		},
		{
			// ファイルはあるのに読まない。「ありません」と書くと、既にある作業コピーを
			// ゲーム内で書き出し直させることになる。
			name: "作業コピーとはみ出しの記録を --no-working で読まない",
			files: map[string]string{
				"Translations/ja/strings.csv":               diffCleanJA,
				"Translations/_discovered/ja.working.csv":   working,
				"Translations/_discovered/layout_risks.csv": layout,
			},
			args:     []string{"--no-working"},
			mustHold: map[string][]string{"ja": {"untranslated", "layout_risk"}},
		},
		{
			// 作業コピーは読めたが、再生順に norm 列が無い。止まるのは引き継ぎ元の候補だけ。
			name: "作業コピーはあるが再生順に norm 列が無い",
			files: map[string]string{
				"Translations/ja/strings.csv":             diffCleanJA,
				"Translations/_discovered/ja.working.csv": working,
			},
			mustHold: map[string][]string{"ja": {"carry_from"}},
			judged:   map[string][]string{"ja": {"untranslated", "dropped", "tag_mismatch"}},
		},
		{
			// 作業コピーの有無がロケールで違う。止めたロケールだけを名指しする。
			name: "作業コピーのあるロケールと無いロケールがある",
			files: map[string]string{
				"Translations/ja/strings.csv":             diffCleanJA,
				"Translations/de/strings.csv":             diffPublishedDE,
				"Translations/_discovered/ja.working.csv": working,
			},
			mustHold: map[string][]string{"de": {"untranslated", "dropped", "tag_mismatch"}},
			judged:   map[string][]string{"ja": {"untranslated", "dropped", "tag_mismatch"}},
		},
		{
			// 再生順の警告に入るカテゴリは重ねないが、作業コピーの保留はそこに入らない
			// ので書く。ロケールごと飛ばすと、未翻訳を判定していないことが消える。
			name:     "再生順を読めず作業コピーも無い",
			files:    map[string]string{"Translations/ja/strings.csv": diffCleanJA},
			noOrder:  true,
			mustHold: map[string][]string{"ja": {"untranslated", "dropped", "tag_mismatch"}},
			orderHeld: map[string][]string{"ja": {
				"vanished", "carryover", "stray_line_id", "not_published", "script_gap", "unknown_origin",
			}},
		},
		{
			// 作業コピーはあるので、止まるのは再生順を要るカテゴリだけである。引き継ぎ元の
			// 候補も、norm 列ではなく再生順を読めていないことで止まる（norm はキーのある
			// 行からしか拾わない）。理由の行は1つも無く、再生順の警告と締めだけになる。
			name: "再生順を読めず作業コピーとはみ出しの記録はある",
			files: map[string]string{
				"Translations/ja/strings.csv":               diffCleanJA,
				"Translations/_discovered/ja.working.csv":   working,
				"Translations/_discovered/layout_risks.csv": layout,
			},
			noOrder: true,
			judged:  map[string][]string{"ja": {"untranslated", "dropped", "tag_mismatch", "layout_risk"}},
			orderHeld: map[string][]string{"ja": {
				"vanished", "carryover", "carry_from", "stray_line_id", "not_published", "script_gap", "unknown_origin",
			}},
		},
	}
	idOf := make(map[string]string)
	for c := range diffAllCounts(t) {
		idOf[c.String()] = c.ID()
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var root string
			if tt.noOrder {
				root = makeTree(t, tt.files)
			} else {
				root = diffTree(t, tt.files)
			}
			base := append([]string{"diff", "--root", root}, tt.args...)

			code, text, textErr := runCLI(base...)
			if code == exitError {
				t.Fatalf("text の終了コード = %d\nstdout:\n%s\nstderr:\n%s", code, text, textErr)
			}
			// text 形式は本文に書くので、標準エラーへ重ねない。
			if strings.Contains(textErr, "は判定しません（") {
				t.Errorf("text 形式で保留を標準エラーへ重ねている:\n%s", textErr)
			}
			want := heldInText(text)
			// 締めの1行が並べるのは、本文がどこかのロケールで「判定していません」と
			// 書いたカテゴリ全部である。理由の行から外す分を消す前に拾っておく。
			var wantTail []string
			// orderText は、本文が「再生順を読めていません」で止めたロケールとカテゴリの識別子。
			orderText := make(map[string]map[string]bool)
			for locale, held := range want {
				orderText[locale] = make(map[string]bool)
				for name, why := range held {
					if !slices.Contains(wantTail, idOf[name]) {
						wantTail = append(wantTail, idOf[name])
					}
					if why == "再生順を読めていません" {
						orderText[locale][idOf[name]] = true
						delete(held, name)
					}
				}
				if len(held) == 0 {
					delete(want, locale)
				}
			}

			code, csvOut, stderr := runCLI(append(base, "--format", "csv")...)
			if code == exitError {
				t.Fatalf("csv の終了コード = %d\nstdout:\n%s\nstderr:\n%s", code, csvOut, stderr)
			}
			if strings.Contains(csvOut, "判定しません") {
				t.Errorf("csv 本体に人向けの文面が混ざっている:\n%s", csvOut)
			}
			got := heldInStderr(t, stderr, localesInText(text))
			if !maps.EqualFunc(got, want, maps.Equal) {
				t.Errorf("csv の標準エラーと text の本文で、判定しないカテゴリが違う\n"+
					"--- 標準エラーから ---\n%v\n--- 本文から ---\n%v\n--- 標準エラー ---\n%s--- 本文 ---\n%s",
					got, want, stderr, text)
			}
			if strings.Contains(stderr, "（再生順を読めていません）") {
				t.Errorf("再生順の警告と同じ理由を重ねている:\n%s", stderr)
			}

			heldIDs := make(map[string]map[string]bool)
			for locale, held := range got {
				heldIDs[locale] = make(map[string]bool)
				for name := range held {
					heldIDs[locale][idOf[name]] = true
				}
			}
			for locale, ids := range tt.mustHold {
				for _, id := range ids {
					if !heldIDs[locale][id] {
						t.Errorf("%s の %s を判定しないと書いていない:\n%s", locale, id, stderr)
					}
				}
			}
			for locale, ids := range tt.judged {
				for _, id := range ids {
					if heldIDs[locale][id] {
						t.Errorf("%s の %s は判定しているのに、判定しないと書いている:\n%s", locale, id, stderr)
					}
				}
			}
			// 締めの1行は、どこかで止めたカテゴリの csv の識別子を1度ずつ並べる。
			// 表計算で category 列を絞る人が読むのはこちらである。再生順の警告に
			// 理由を任せたカテゴリも並べる。
			tail := heldTail(t, stderr)
			for locale, ids := range tt.orderHeld {
				for _, id := range ids {
					if !orderText[locale][id] {
						t.Errorf("前提が崩れている: 本文が %s の %s を「再生順を読めていません」で止めていない:\n%s",
							locale, id, text)
					}
					if heldIDs[locale][id] {
						t.Errorf("%s の %s を、再生順の警告と重ねて理由の行で名指ししている:\n%s", locale, id, stderr)
					}
					if !slices.Contains(tail, id) {
						t.Errorf("%s の行が無いことを 0 件と読ませない断りが締めに無い:\n%s", id, stderr)
					}
				}
			}
			slices.Sort(tail)
			slices.Sort(wantTail)
			if !slices.Equal(tail, wantTail) {
				t.Errorf("締めの識別子が %v、本文が判定していないと書いたカテゴリは %v:\n%s", tail, wantTail, stderr)
			}
			if n := strings.Count(stderr, "0 件という意味ではありません"); n != 1 {
				t.Errorf("締めを %d 回書いている:\n%s", n, stderr)
			}
		})
	}
}

// heldInText は text 形式の本文から、ロケールごとに「判定していません（理由）」と
// 書いたカテゴリの名前と理由を拾う。
func heldInText(stdout string) map[string]map[string]string {
	out := make(map[string]map[string]string)
	locale := ""
	for _, line := range strings.Split(stdout, "\n") {
		if name, ok := localeHeading(line); ok {
			locale = name
			continue
		}
		name, why, ok := strings.Cut(strings.TrimSpace(line), "判定していません（")
		if !ok || locale == "" {
			continue
		}
		if out[locale] == nil {
			out[locale] = make(map[string]string)
		}
		out[locale][strings.TrimSpace(name)] = strings.TrimSuffix(why, "）")
	}
	return out
}

// localesInText は text 形式の本文に出た、報告したロケールの名前を順に返す。
func localesInText(stdout string) []string {
	var out []string
	for _, line := range strings.Split(stdout, "\n") {
		if name, ok := localeHeading(line); ok {
			out = append(out, name)
		}
	}
	return out
}

// localeHeading は、text 形式の本文のロケールの見出し
// （「ja  Translations/ja/strings.csv   ハッシュ 1 行 / …」）からロケール名を取る。
func localeHeading(line string) (string, bool) {
	if strings.HasPrefix(line, " ") || !strings.Contains(line, "   ハッシュ ") {
		return "", false
	}
	name, _, ok := strings.Cut(line, "  ")
	return name, ok
}

// heldInStderr は csv 形式の標準エラーから、ロケールごとに「判定しません（理由）」と
// 書いたカテゴリの名前と理由を拾う。ロケール名を添えていない行は、報告した
// ロケール（locales）すべての分として読む。
func heldInStderr(t *testing.T, stderr string, locales []string) map[string]map[string]string {
	t.Helper()

	out := make(map[string]map[string]string)
	for _, line := range strings.Split(stderr, "\n") {
		body, ok := strings.CutPrefix(line, "dwloc: 警告: ")
		if !ok {
			continue
		}
		head, why, ok := strings.Cut(body, "は判定しません（")
		if !ok {
			continue
		}
		why, ok = strings.CutSuffix(why, "）。")
		if !ok {
			t.Errorf("保留の行の終わり方が違う: %q", line)
		}
		targets := locales
		if !strings.HasPrefix(head, "「") {
			names, rest, found := strings.Cut(head, " の「")
			if !found {
				t.Errorf("保留の行からロケールを読めない: %q", line)
				continue
			}
			targets = strings.Split(names, ", ")
			head = "「" + rest
		}
		for _, locale := range targets {
			if out[locale] == nil {
				out[locale] = make(map[string]string)
			}
			for _, name := range quotedNames(head) {
				out[locale][name] = why
			}
		}
	}
	return out
}

// quotedNames は「」で囲まれた名前を、出てきた順に返す。
func quotedNames(s string) []string {
	var out []string
	for {
		_, rest, ok := strings.Cut(s, "「")
		if !ok {
			return out
		}
		name, after, ok := strings.Cut(rest, "」")
		if !ok {
			return out
		}
		out = append(out, name)
		s = after
	}
}

// heldTail は保留の締めの1行（「… の行が無いことは、0 件という意味ではありません。」）が
// 並べた csv の識別子を返す。締めが無ければ空を返す。
func heldTail(t *testing.T, stderr string) []string {
	t.Helper()

	for _, line := range strings.Split(stderr, "\n") {
		body, ok := strings.CutPrefix(line, "dwloc:       ")
		if !ok {
			continue
		}
		ids, ok := strings.CutSuffix(body, " の行が無いことは、0 件という意味ではありません。")
		if !ok {
			continue
		}
		return strings.Split(ids, " と ")
	}
	return nil
}

// diffAllCounts は internal/diff が持つ全カテゴリを、件数 0 の Counts として返す。
//
// Compare は Counts に全カテゴリを入れる約束なので、1ロケールの空の比較から取る。
// 試験の側でカテゴリを手で並べると、カテゴリが増えたときに試験だけが古いまま通る。
func diffAllCounts(t *testing.T) map[diff.Category]int {
	t.Helper()
	rep := diff.Compare(&diff.Repo{Locales: []diff.Locale{{Name: "ja"}}}, nil)
	if len(rep.Locales) != 1 || len(rep.Locales[0].Counts) == 0 {
		t.Fatalf("前提が崩れている: 空の比較から全カテゴリを取れない: %+v", rep.Locales)
	}
	return rep.Locales[0].Counts
}

// TestWarnHeldCategoriesNamesWhatIsHeld は、判定の材料が1つ欠けたときの警告が、
// 実際に保留にしたカテゴリだけを、text 形式の本文と同じ理由で名指しすることを見る。
//
// 名指しするカテゴリは [diff.Summary.CanJudge] から、理由は
// [diff.Summary.JudgeBlockReason] から決まる。見ているのは2つ。保留したカテゴリを
// 名指しすること。落ちると csv でその保留が見えなくなる。判定したカテゴリを
// 名指ししないこと。混ざると止めていないカテゴリを止めたと書く。
// 材料ごとに確かめるので、判定の材料が増えて片方だけが古くなったときもここで落ちる。
//
// 再生順のキーを読めていないときだけは、「再生順を読めていません」で止めた
// カテゴリを理由の行で名指ししない。runDiff が先に書く再生順の警告に入るからである。
// 締めの1行には、そのカテゴリも並べる。
//
// 材料を全部そろえた要約から、1つずつ欠いて確かめる。そろえ方が足りないと
// 別の理由で止まったカテゴリが混ざり、突き合わせが成り立たない。そのため先に、
// そろえた要約で全カテゴリを判定できることを見る。internal/diff が判定の材料を
// 増やしたら、ここの ready と missing に足す。
func TestWarnHeldCategoriesNamesWhatIsHeld(t *testing.T) {
	counts := diffAllCounts(t)
	ready := diff.Summary{
		Locale: "ja", Counts: counts,
		HasWorking: true, OrderKeys: true, OrderLineIDs: true, OrderNorms: true,
		HasLayoutRisks: true, OldOrder: true,
	}
	for c := range counts {
		if !ready.CanJudge(c) {
			t.Fatalf("材料をそろえた要約で %s を判定できない（%s）。判定の材料が増えたなら ready に足す",
				c.ID(), ready.JudgeBlockReason(c))
		}
	}

	missing := []struct {
		name string
		edit func(*diff.Summary)
		// mustName は、この材料が欠けたら必ず名指しするカテゴリ。CanJudge と
		// 突き合わせるだけだと、両方が同じようにずれたときに気づけない。
		mustName []diff.Category
	}{
		{"作業コピーが無い", func(s *diff.Summary) { s.HasWorking = false },
			[]diff.Category{diff.CatUntranslated, diff.CatDropped, diff.CatTagMismatch, diff.CatCarryFrom}},
		{"作業コピーを読んでいない", func(s *diff.Summary) { s.HasWorking, s.WorkingExists = false, true },
			[]diff.Category{diff.CatUntranslated}},
		{"台詞IDが無い", func(s *diff.Summary) { s.OrderLineIDs = false },
			[]diff.Category{diff.CatCarryover, diff.CatStrayLineID}},
		{"norm 列が無い", func(s *diff.Summary) { s.OrderNorms = false },
			[]diff.Category{diff.CatCarryFrom}},
		{"はみ出しの記録が無い", func(s *diff.Summary) { s.HasLayoutRisks = false },
			[]diff.Category{diff.CatLayoutRisk}},
		{"はみ出しの記録を読んでいない", func(s *diff.Summary) { s.HasLayoutRisks, s.LayoutRisksExist = false, true },
			[]diff.Category{diff.CatLayoutRisk}},
		{"1つ前の再生順が無い", func(s *diff.Summary) { s.OldOrder = false },
			[]diff.Category{diff.CatCarryover}},
		{"1つ前の再生順を疑う", func(s *diff.Summary) { s.OldOrderStale = true },
			[]diff.Category{diff.CatCarryover}},
		{"再生順のキーを読めていない", func(s *diff.Summary) { s.OrderKeys, s.OrderLineIDs = false, false }, nil},
	}
	for _, m := range missing {
		t.Run(m.name, func(t *testing.T) {
			sum := ready
			m.edit(&sum)
			var errOut bytes.Buffer
			warnHeldCategories(&diff.Report{Locales: []diff.Summary{sum}}, &errOut)
			got := errOut.String()

			var named []diff.Category
			for c := range counts {
				line := lineNaming(got, c)
				block := sum.JudgeBlockReason(c)
				held := !sum.CanJudge(c) && (sum.OrderKeys || block.ID != reason.JudgeOrderUnreadable)
				if in := line != ""; in != held {
					t.Errorf("%s: 名指しするはず = %v なのに、警告で名指ししたか = %v:\n%s", c.ID(), held, in, got)
					continue
				}
				if !held {
					continue
				}
				named = append(named, c)
				// 理由は本文の「判定していません（理由）」と同じ文面にする。
				if !strings.Contains(line, "（"+block.Text+"）") {
					t.Errorf("%s: 理由が本文と違う。本文は %q:\n%s", c.ID(), block.Text, line)
				}
			}
			for _, want := range m.mustName {
				if !slices.Contains(named, want) {
					t.Errorf("%s を名指ししていない:\n%s", want.ID(), got)
				}
			}
			// キーだけで判定できるカテゴリは、再生順のキーがあるかぎり止まらない。
			if sum.OrderKeys && slices.Contains(named, diff.CatVanished) {
				t.Errorf("キーだけで判定できる %s を名指ししている:\n%s", diff.CatVanished.ID(), got)
			}
			// 締めの1行は、保留したカテゴリを表示順にすべて並べる。理由の行を
			// 再生順の警告に任せたカテゴリも入れる。
			var wantTail []string
			for _, c := range diff.Categories() {
				if !sum.CanJudge(c) {
					wantTail = append(wantTail, c.ID())
				}
			}
			if tail := heldTail(t, got); !slices.Equal(tail, wantTail) {
				t.Errorf("締めの識別子が %v、保留したカテゴリは %v:\n%s", tail, wantTail, got)
			}
		})
	}
}

// lineNaming は警告のうち、カテゴリ c を名指しした行を返す。無ければ空を返す。
func lineNaming(stderr string, c diff.Category) string {
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "dwloc: 警告: ") && strings.Contains(line, "「"+c.String()+"」") {
			return line
		}
	}
	return ""
}

// TestWarnHeldCategories は、csv 形式で判定を保留したカテゴリを標準エラーへ書く
// 書き方を固定する。
//
// 見ているのは6つ。保留は理由ごとに1行だけ書くこと（同じ保留を別の文面で
// 重ねない。作業コピーの無い CI では毎回全ロケールが止まるので、ロケールごとや
// カテゴリごとに書くとうるさい）。理由は text 形式の本文と同じ判断から取ること
// （旧再生順の有無だけを見ると、台詞IDが無くて保留したときに黙る）。台詞IDを要る
// カテゴリに限らず書くこと（台詞IDを要るカテゴリだけを見ていたころは、作業コピーが
// 無いときの未翻訳などを黙っていた）。理由が空でも「（）」と書かないこと（空のまま
// 出すと理由を取り違えたように見える）。再生順のキーを読めていないときは、「再生順を
// 読めていません」の分の理由を runDiff が先に書く再生順の警告に任せること。任せるのは
// 理由の行だけで、作業コピーの保留は書き、締めの1行にはその分のカテゴリも並べること。
func TestWarnHeldCategories(t *testing.T) {
	counts := diffAllCounts(t)
	// ready は判定の材料が全部そろったロケールの要約。各場合はここから欠く。
	ready := func(locale string) diff.Summary {
		return diff.Summary{
			Locale: locale, Counts: counts,
			HasWorking: true, OrderKeys: true, OrderLineIDs: true, OrderNorms: true,
			HasLayoutRisks: true, OldOrder: true,
		}
	}
	const (
		// キーは読めているので、理由は「再生順を読めていません」ではなく、台詞IDが
		// 無いことを言う。csv には text 形式の見出しが無く、この文面だけで読み分ける。
		bothHeld     = "「引き継ぎ候補」と「台本に無い台詞ID行」は判定しません（再生順に台詞ID (line_id) がありません）。\n"
		tailCarry    = "dwloc:       carryover の行が無いことは、0 件という意味ではありません。\n"
		tailBoth     = "dwloc:       carryover と stray_line_id の行が無いことは、0 件という意味ではありません。\n"
		staleWhy     = "読めた1つ前の再生順が、いまの版と同じ内容に見えます"
		bothHeldLine = "dwloc: 警告: " + bothHeld
		// 作業コピーを要る4つ。表示順に並ぶ。
		workingHeld = "「未翻訳」と「引き継ぎ元の候補」と「publish で捨てられる行」と「原文とタグが違う行」は判定しません"
		tailWorking = "dwloc:       untranslated と carry_from と dropped と tag_mismatch の行が無いことは、0 件という意味ではありません。\n"
	)

	tests := []struct {
		name    string
		locales []diff.Summary
		want    string
	}{
		{
			name:    "全部判定できていれば何も書かない",
			locales: []diff.Summary{ready("de"), ready("ja")},
			want:    "",
		},
		{
			name: "1つ前の再生順が無ければ理由を1行だけ書く",
			locales: func() []diff.Summary {
				out := []diff.Summary{ready("de"), ready("ja")}
				for i := range out {
					out[i].OldOrder = false
					out[i].OldOrderReason = diff.ErrNoRepository.Error()
				}
				return out
			}(),
			want: "dwloc: 警告: 「引き継ぎ候補」は判定しません（git リポジトリではないか、コミットがありません）。\n" + tailCarry,
		},
		{
			name: "理由が空でも括弧の中を空にしない",
			locales: func() []diff.Summary {
				s := ready("ja")
				s.OldOrder = false
				return []diff.Summary{s}
			}(),
			want: "dwloc: 警告: 「引き継ぎ候補」は判定しません（1つ前の再生順を読めていません）。\n" + tailCarry,
		},
		{
			name: "いまの再生順に台詞IDが無ければ、旧再生順が読めていても2つを1行にまとめて書く",
			locales: func() []diff.Summary {
				out := []diff.Summary{ready("de"), ready("ja")}
				for i := range out {
					out[i].OrderLineIDs = false
				}
				return out
			}(),
			want: bothHeldLine + tailBoth,
		},
		{
			// text 形式の本文も、台詞IDの理由を先に書く（judgeBlockReason の見る順）。
			// csv だけ旧再生順の理由を足すと、同じ実行の2つの形式で言うことが食い違う。
			name: "台詞IDも1つ前の再生順も無ければ、本文と同じく台詞IDの理由だけを書く",
			locales: func() []diff.Summary {
				s := ready("ja")
				s.OrderLineIDs, s.OldOrder = false, false
				s.OldOrderReason = diff.ErrNoGit.Error()
				return []diff.Summary{s}
			}(),
			want: bothHeldLine + tailBoth,
		},
		{
			name: "一部のロケールだけ保留なら、そのロケールを名指しする",
			locales: func() []diff.Summary {
				stale := ready("ja")
				stale.OldOrderStale = true
				stale.OldOrderReason = staleWhy
				return []diff.Summary{ready("de"), stale}
			}(),
			want: "dwloc: 警告: ja の「引き継ぎ候補」は判定しません（" + staleWhy + "）。\n" + tailCarry,
		},
		{
			// 締めの1行は、どこかで止めたカテゴリを1度だけ並べる。行ごとに
			// 締めを書くと、同じ断りが何度も出る。
			name: "理由の違うロケールは行を分け、締めは1度だけ書く",
			locales: func() []diff.Summary {
				noIDs := ready("de")
				noIDs.OrderLineIDs = false
				stale := ready("ja")
				stale.OldOrderStale = true
				stale.OldOrderReason = staleWhy
				return []diff.Summary{noIDs, stale, ready("ko")}
			}(),
			want: "dwloc: 警告: de の" + bothHeld +
				"dwloc: 警告: ja の「引き継ぎ候補」は判定しません（" + staleWhy + "）。\n" + tailBoth,
		},
		{
			// 理由は runDiff が先に書く再生順の警告（「台本から消えた行などは
			// 判定しません」）に任せ、締めの1行だけを書く。締めはその警告の続きとして
			// 読める字下げで、「など」に入るカテゴリを csv の識別子で並べる。締めまで
			// 省くと、not_published で絞った人が 0 行を「0 件」と読む。
			name: "再生順のキーを読めていなければ、理由は再生順の警告に任せ、締めには並べる",
			locales: func() []diff.Summary {
				s := ready("ja")
				s.OrderKeys, s.OrderLineIDs, s.OldOrder = false, false, false
				s.OldOrderReason = diff.ErrNoGit.Error()
				return []diff.Summary{s}
			}(),
			want: "dwloc:       vanished と carryover と stray_line_id と not_published と script_gap と unknown_origin" +
				" の行が無いことは、0 件という意味ではありません。\n",
		},
		{
			name: "作業コピーが無ければ、止めた4つを1行にまとめて書く",
			locales: func() []diff.Summary {
				out := []diff.Summary{ready("de"), ready("ja")}
				for i := range out {
					out[i].HasWorking = false
				}
				return out
			}(),
			want: "dwloc: 警告: " + workingHeld + "（作業コピーがありません）。\n" + tailWorking,
		},
		{
			// 読まないと指定しただけで、ファイルはある。「ありません」と書くと、
			// 既にある作業コピーをゲーム内で書き出し直させることになる。
			name: "作業コピーとはみ出しの記録を読んでいなければ、読んでいないと書く",
			locales: func() []diff.Summary {
				s := ready("ja")
				s.HasWorking, s.WorkingExists = false, true
				s.HasLayoutRisks, s.LayoutRisksExist = false, true
				return []diff.Summary{s}
			}(),
			want: "dwloc: 警告: " + workingHeld + "（作業コピーを読んでいません）。\n" +
				"dwloc: 警告: 「はみ出しの恐れがある行」は判定しません（はみ出しの記録を読んでいません）。\n" +
				"dwloc:       untranslated と carry_from と dropped と tag_mismatch と layout_risk の行が無いことは、0 件という意味ではありません。\n",
		},
		{
			// CI で普通の形。作業コピーも、はみ出しの記録も、git の履歴も無い。
			// ロケールが何個あっても、理由ごとに1行で済む。
			name: "作業コピーもはみ出しの記録も1つ前の再生順も無ければ、理由ごとに1行ずつ書く",
			locales: func() []diff.Summary {
				out := []diff.Summary{ready("de"), ready("fr"), ready("ja")}
				for i := range out {
					out[i].HasWorking, out[i].HasLayoutRisks, out[i].OldOrder = false, false, false
					out[i].OldOrderReason = diff.ErrNoRepository.Error()
				}
				return out
			}(),
			want: "dwloc: 警告: " + workingHeld + "（作業コピーがありません）。\n" +
				"dwloc: 警告: 「引き継ぎ候補」は判定しません（git リポジトリではないか、コミットがありません）。\n" +
				"dwloc: 警告: 「はみ出しの恐れがある行」は判定しません（ゲーム内で Check translation layout を走らせた記録がありません）。\n" +
				"dwloc:       untranslated と carryover と carry_from と dropped と tag_mismatch と layout_risk の行が無いことは、0 件という意味ではありません。\n",
		},
		{
			name: "作業コピーの無いロケールだけを名指しする",
			locales: func() []diff.Summary {
				noWorking := ready("de")
				noWorking.HasWorking = false
				return []diff.Summary{noWorking, ready("ja")}
			}(),
			want: "dwloc: 警告: de の" + workingHeld + "（作業コピーがありません）。\n" + tailWorking,
		},
		{
			// 引き継ぎ元の候補は作業コピーと norm 列の両方を要る。作業コピーのある
			// ロケールでは norm 列の理由になり、無いロケールの行には入らない。
			name: "同じカテゴリでもロケールで理由が違えば、それぞれの行に入れる",
			locales: func() []diff.Summary {
				out := []diff.Summary{ready("de"), ready("ja")}
				for i := range out {
					out[i].OrderNorms = false
				}
				out[0].HasWorking = false
				return out
			}(),
			want: "dwloc: 警告: de の" + workingHeld + "（作業コピーがありません）。\n" +
				"dwloc: 警告: ja の「引き継ぎ元の候補」は判定しません（再生順に norm 列がありません）。\n" + tailWorking,
		},
		{
			// 再生順の警告に入るのは「再生順を読めていません」の分だけである。
			// ロケールごと飛ばすと、未翻訳を判定していないことが消える。締めには
			// 両方の理由で止めたカテゴリを、表示順に1つの並びで書く。
			name: "再生順のキーを読めていなくても、作業コピーの保留は書く",
			locales: func() []diff.Summary {
				s := ready("ja")
				s.OrderKeys, s.OrderLineIDs, s.OrderNorms, s.OldOrder = false, false, false, false
				s.HasWorking = false
				s.OldOrderReason = diff.ErrNoGit.Error()
				return []diff.Summary{s}
			}(),
			want: "dwloc: 警告: " + workingHeld + "（作業コピーがありません）。\n" +
				"dwloc:       untranslated と vanished と carryover と carry_from と dropped と stray_line_id と tag_mismatch" +
				" と not_published と script_gap と unknown_origin の行が無いことは、0 件という意味ではありません。\n",
		},
		{
			// 作業コピーがあると、引き継ぎ元の候補を止めるのは再生順である。norm 列の
			// 理由で書くと、再生順の警告と別の直し方（norm 列だけを足す）を言うことになる。
			name: "再生順のキーを読めず作業コピーがあれば、引き継ぎ元の候補も再生順の警告に任せる",
			locales: func() []diff.Summary {
				s := ready("ja")
				s.OrderKeys, s.OrderLineIDs, s.OrderNorms, s.OldOrder = false, false, false, false
				s.OldOrderReason = diff.ErrNoGit.Error()
				return []diff.Summary{s}
			}(),
			want: "dwloc:       vanished と carryover と carry_from と stray_line_id と not_published と script_gap" +
				" と unknown_origin の行が無いことは、0 件という意味ではありません。\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var errOut bytes.Buffer
			warnHeldCategories(&diff.Report{Locales: tt.locales}, &errOut)
			if got := errOut.String(); got != tt.want {
				t.Errorf("標準エラーが違う\n--- 得たもの ---\n%s--- 期待 ---\n%s", got, tt.want)
			}
		})
	}
}

func TestRunDiffWithoutTranslations(t *testing.T) {
	// 比較する相手が1つも無い状態。0 件と書いて成功で終わると、何も比べて
	// いないことに気づけないので、publish と同じくエラーにする。
	root := makeTree(t, map[string]string{"data/script_order.csv": diffOrderCSV})

	code, stdout, stderr := runCLI("diff", "--root", root)
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
	}
	checkContains(t, "stderr", stderr, []string{"Translations"})
}

func TestRunDiffHelp(t *testing.T) {
	code, stdout, stderr := runCLI("diff", "--help")
	if code != exitOK {
		t.Fatalf("終了コード = %d, 期待 %d\nstderr:\n%s", code, exitOK, stderr)
	}
	checkContains(t, "stdout", stdout, []string{
		"使い方: dwloc diff", "--no-working", "--strict", "dwloc publish より先に走らせてください",
		// 引き継ぎ候補は既存カテゴリと重なるので、使い方の側で先に断っておく。
		"引き継ぎ候補", "訳は", "書き換えません",
		// 移動と複製のどちらなのかで、旧行の訳を消してよいかが変わる。
		"「移動」", "「複製」",
		// csv では保留が本文に出ないので、どこに出るかを使い方にも書いておく。
		"保留します", "標準エラー",
		// 台詞IDを要るカテゴリに限らず、判定しなかったカテゴリはすべて書く。
		"判定しなかったカテゴリ（作業コピーが無いときの未翻訳など）",
		// 台詞IDが無いときは、引き継ぎ候補のほかにもう1つ止まる。
		"「台本に無い台詞ID行」も保留し",
	})
}

func TestRunDiffAppearsInGlobalUsage(t *testing.T) {
	// 入口の一覧に載っていないサブコマンドは、あっても見つけてもらえない。
	code, stdout, _ := runCLI("help")
	if code != exitOK {
		t.Fatalf("終了コード = %d, 期待 %d", code, exitOK)
	}
	checkContains(t, "stdout", stdout, []string{"diff"})
}

// diffCarryFromKey / diffCarryToKey は「英文が直されてキーが変わった1行」を表す。
// 公開ファイルには旧キーの訳だけがあり、再生順には新キーだけがある。
var (
	diffCarryFromKey = key.For("See you tomorrow")
	diffCarryToKey   = key.For("See you tomorrow!")
)

// diffCarryOrderCSV は Hello のあとに「直されたほうの」台詞が来る再生順。
// 台詞ID line:b0000001 は変わらず、キーだけが変わっている。
var diffCarryOrderCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
	"L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf," + diffHelloKey + ",Ryan,\n" +
	"L01 Ryan,intro,Ryan_1_intro,2,line:b0000001," + diffCarryToKey + ",Ryan,\n"

// diffCarryOldOrderCSV は更新前の再生順。同じ台詞IDに旧キーが載っている。
var diffCarryOldOrderCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
	"L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf," + diffHelloKey + ",Ryan,\n" +
	"L01 Ryan,intro,Ryan_1_intro,2,line:b0000001," + diffCarryFromKey + ",Ryan,\n"

// diffCarryPublishedJA は旧キーの訳を持ったままの公開ファイル。
var diffCarryPublishedJA = publish.HeaderLine + "\n" +
	diffHelloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
	diffCarryFromKey + ",L01 Ryan,Ryan_1_intro,2,Ryan,またあした\n"

// diffCarryTree はゲーム更新の直後を、git リポジトリとして組み立てる。
//
// 手順は翻訳者の実際の流れに合わせてある。更新前の再生順をコミットしておき、
// ゲームが更新されてゲーム内で書き出し直したところまでを作る（まだコミットしない）。
// dwloc は既定で git から1つ前の版を読むので、ここだけは本物の git を通す。
// internal/diff 側は旧再生順を直に渡せるので git を要らないが、入口が既定で
// git を選んでいること自体は、どこかで一度実際に確かめておく必要がある。
func diffCarryTree(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無いので飛ばす")
	}

	root := diffTree(t, map[string]string{
		"data/script_order.csv":       diffCarryOldOrderCSV,
		"Translations/ja/strings.csv": diffCarryPublishedJA,
	})
	for _, args := range [][]string{
		{"init"},
		{"add", "."},
		append(gitTestOpts(), "commit", "-m", "before the game update"),
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v が失敗したので飛ばす: %v (%s)", args, err, out)
		}
	}

	// ゲームが更新され、ゲーム内で再生順を書き出し直したところ。まだコミットしない。
	path := filepath.Join(root, "data", "script_order.csv")
	if err := os.WriteFile(path, []byte(diffCarryOrderCSV), 0o644); err != nil {
		t.Fatalf("再生順を書き換えられない: %v", err)
	}
	return root
}

// TestRunDiffCarryoverIsShown は、ゲーム更新でキーが変わった行の引き継ぎ先が
// 端末の出力から読み取れることを確かめる。
//
// 見ているのは「どの旧キーの訳を、どの新キーへ移せばよいか」が1行で分かること。
// キーと訳と新しい位置が同じ行に並んでいないと、翻訳者は16桁hexを目で突き合わせる
// ことになり、候補が示されても次の一手が決まらない。
func TestRunDiffCarryoverIsShown(t *testing.T) {
	root := diffCarryTree(t)

	code, stdout, stderr := runCLI("diff", "--root", root)
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	if stderr != "" {
		t.Errorf("標準エラーへ何か出ている:\n%s", stderr)
	}

	checkContains(t, "stdout", stdout, []string{
		"引き継ぎ候補",
		// 引き継ぎ元の行。旧キーと、移すことになる訳。
		diffCarryFromKey, "またあした",
		// 引き継ぎ先。新しいキーと、その新しい位置。
		"引き継ぎ先 " + diffCarryToKey, "L01 Ryan / Ryan_1_intro / 2",
		// 訳を書き換える機能ではないことを、一覧の前に必ず書く。
		"訳は書き換えていません",
		// 旧行を消してよいかどうか（移動か複製か）を内訳で伝える。
		"移動 1 件",
		// 同じ行は「台本から消えた行」にも出る。重なることを先に伝える。
		"「台本から消えた行」にも出ます",
		// 同じ一覧を二度読ませないよう、消えた行の側でも重なりを断っておく。
		"うち 1 件には引き継ぎ候補があります",
	})

	// 引き継ぎ元・引き継ぎ先・訳が1行にそろっていること。
	// 別々の行に散らばっていると、目で突き合わせる手間が残る。
	line := ""
	for _, l := range strings.Split(stdout, "\n") {
		if strings.Contains(l, diffCarryFromKey) && strings.Contains(l, "引き継ぎ先") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("引き継ぎ元と引き継ぎ先が同じ行に出ていない:\n%s", stdout)
	}
	checkContains(t, "引き継ぎ候補の行", line, []string{diffCarryToKey, "またあした", "Ryan_1_intro"})

	// 「台本から消えた行」は候補が付いても減らさない。既存の判定は動かさない。
	// 締めは行で数えるので、同じ1行が2カテゴリに出ても 1 行のまま。
	if !strings.Contains(stdout, "要確認が 1 行あります（カテゴリをまたぐ重なりを含めて、のべ 2 件）。") {
		t.Errorf("締めの数え方が違う:\n%s", stdout)
	}
}

// TestRunDiffCarryoverCSVKeepsColumns は、CSV に引き継ぎ先が載ること、かつ
// 列が11のまま変わらないことを確かめる。
//
// 列数は使い方の説明にも書いてある約束で、増やすと表計算に貼る側の手順が変わる。
// 人は note 列で読めるので、列を足さずに済ませてある。
func TestRunDiffCarryoverCSVKeepsColumns(t *testing.T) {
	root := diffCarryTree(t)

	code, stdout, stderr := runCLI("diff", "--root", root, "--format", "csv")
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstderr:\n%s", code, exitProblems, stderr)
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if lines[0] != "locale,category,status,key,section,node,order,speaker,source_en,translation,note" {
		t.Fatalf("列を増やしている: %q", lines[0])
	}
	want := len(strings.Split(lines[0], ","))
	carry := ""
	for _, l := range lines[1:] {
		if strings.HasPrefix(l, "ja,carryover,") {
			carry = l
		}
		if got := len(strings.Split(l, ",")); got != want {
			t.Errorf("列数が違う行がある: %d 列, 期待 %d 列: %q", got, want, l)
		}
	}
	if carry == "" {
		t.Fatalf("carryover の行が無い:\n%s", stdout)
	}
	checkContains(t, "carryover の行", carry, []string{
		"ja,carryover,review," + diffCarryFromKey + ",",
		// 引き継ぎ先は note 列（最終列）に入る。
		"引き継ぎ先 " + diffCarryToKey,
		"またあした",
	})
	// 旧キーの行は「台本から消えた行」にも残っている。
	if !strings.Contains(stdout, "ja,vanished,review,"+diffCarryFromKey+",") {
		t.Errorf("台本から消えた行から外している:\n%s", stdout)
	}
}

// 「複製」になる引き継ぎ候補のキー。移動のほう（diffCarryFromKey = 6cfe…）より
// 大きい値になる英文を選んである。キー順に並べると複製が後ろへ回るので、
// --limit で切り詰めたときに落ちるかどうかを確かめられる。
var (
	diffCarryCopiedFromKey = key.For("Good morning")  // 再生順の別の行で生きたまま
	diffCarryCopiedToKey   = key.For("Good morning!") // 直された側の新しいキー
)

// diffCarryTwoOrderCSV は移動と複製が1件ずつ出る更新後の再生順。
//
// line:b0000001 の英文が直されて移動、line:b0000002 の英文も直されたが、
// 同じ英文が Unused（line:c0000001）にも置かれていて、そちらは直されていない。
// 後者の旧キーはいまも再生順にあるので、旧行の訳を消すと Unused 側が英語に戻る。
var diffCarryTwoOrderCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
	"L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf," + diffHelloKey + ",Ryan,\n" +
	"L01 Ryan,intro,Ryan_1_intro,2,line:b0000001," + diffCarryToKey + ",Ryan,\n" +
	"L01 Ryan,intro,Ryan_1_intro,3,line:b0000002," + diffCarryCopiedToKey + ",Ryan,\n" +
	"Unused,intro,Ryan_9_unused,1,line:c0000001," + diffCarryCopiedFromKey + ",Ryan,\n"

// diffCarryTwoOldOrderCSV はその更新前。どちらの台詞IDにも旧キーが載っている。
var diffCarryTwoOldOrderCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
	"L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf," + diffHelloKey + ",Ryan,\n" +
	"L01 Ryan,intro,Ryan_1_intro,2,line:b0000001," + diffCarryFromKey + ",Ryan,\n" +
	"L01 Ryan,intro,Ryan_1_intro,3,line:b0000002," + diffCarryCopiedFromKey + ",Ryan,\n" +
	"Unused,intro,Ryan_9_unused,1,line:c0000001," + diffCarryCopiedFromKey + ",Ryan,\n"

// diffCarryTwoPublishedJA は両方の旧キーの訳を持ったままの公開ファイル。
var diffCarryTwoPublishedJA = diffCarryPublishedJA +
	diffCarryCopiedFromKey + ",L01 Ryan,Ryan_1_intro,3,Ryan,おはよう\n"

// TestRunDiffCarryoverCopiedIsMarkedOnTheLine は、「複製」であることが一覧の
// 行そのものから読み取れることと、--limit で切り詰めても残ることを確かめる。
//
// 内訳の「複製 1 件」だけでは、どの行が複製かが分からない。実データの1行は
// 200 桁を超えるので、末尾の文面まで読ませる置き方だと見落とす。旧行の訳を
// 消してよいかどうかが決まる情報なので、行の先頭に出し、一覧の先頭へ回す。
func TestRunDiffCarryoverCopiedIsMarkedOnTheLine(t *testing.T) {
	root := diffCarryTree(t)
	writeCarryTree(t, root, diffCarryTwoOrderCSV, diffCarryTwoOldOrderCSV, diffCarryTwoPublishedJA)

	// キー順では移動が先に来ること。この前提が崩れると、--limit の確認が
	// 「たまたま通っている」だけになる。
	if !(diffCarryFromKey < diffCarryCopiedFromKey) {
		t.Fatalf("テストの前提が崩れている: 移動 %s は複製 %s より小さいはず",
			diffCarryFromKey, diffCarryCopiedFromKey)
	}

	code, stdout, stderr := runCLI("diff", "--root", root)
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	checkContains(t, "stdout", stdout, []string{"移動 1 件", "複製 1 件"})

	line := findingLineFor(t, stdout, diffCarryCopiedFromKey)
	if !strings.HasPrefix(strings.TrimSpace(line), "複製") {
		t.Errorf("行の先頭が「複製」ではない: %q", line)
	}
	checkContains(t, "複製の行", line, []string{
		diffCarryCopiedToKey, "おはよう", "残してください",
	})

	// --limit 1 でも複製が残ること。落ちるのは移動のほうで、そちらは同じ行が
	// 「台本から消えた行」にも出る。
	code, stdout, _ = runCLI("diff", "--root", root, "--limit", "1")
	if code != exitProblems {
		t.Fatalf("--limit 1 の終了コード = %d, 期待 %d\nstdout:\n%s", code, exitProblems, stdout)
	}
	carry := carryoverLines(stdout)
	if len(carry) != 1 {
		t.Fatalf("--limit 1 なのに %d 行出ている:\n%s", len(carry), stdout)
	}
	if !strings.Contains(carry[0], diffCarryCopiedFromKey) {
		t.Errorf("--limit で複製が落ちている: %q", carry[0])
	}
}

// TestRunDiffCarryoverMovedIsMarkedOnTheLine は「移動」の側にも同じ印が付くことを
// 確かめる。片方だけに印を付けると、印の無い行が「まだ判定していない行」に見える。
func TestRunDiffCarryoverMovedIsMarkedOnTheLine(t *testing.T) {
	root := diffCarryTree(t)

	code, stdout, stderr := runCLI("diff", "--root", root)
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	line := findingLineFor(t, stdout, diffCarryFromKey)
	if !strings.HasPrefix(strings.TrimSpace(line), "移動") {
		t.Errorf("行の先頭が「移動」ではない: %q", line)
	}
}

// writeCarryTree は再生順の新旧と公開ファイルを置き直す。
//
// 旧版と公開ファイルはコミットして、新版の再生順だけを未コミットで置く。
// diffCarryTree が作る「ゲーム更新の直後」と同じ形にそろえる。
func writeCarryTree(t *testing.T, root, newOrder, oldOrder, published string) {
	t.Helper()
	orderPath := filepath.Join(root, "data", "script_order.csv")
	files := map[string]string{
		orderPath: oldOrder,
		filepath.Join(root, "Translations", "ja", "strings.csv"): published,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("%s を書けない: %v", path, err)
		}
	}
	for _, args := range [][]string{
		{"add", "."},
		append(gitTestOpts(), "commit", "-m", "replace the pre-update state"),
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v が失敗したので飛ばす: %v (%s)", args, err, out)
		}
	}
	if err := os.WriteFile(orderPath, []byte(newOrder), 0o644); err != nil {
		t.Fatalf("更新後の再生順を書けない: %v", err)
	}
}

// carryoverLines は「引き継ぎ候補」の一覧に出ている行だけを返す。
func carryoverLines(stdout string) []string {
	var out []string
	in := false
	for _, l := range strings.Split(stdout, "\n") {
		if strings.Contains(l, "引き継ぎ候補") {
			in = true
			continue
		}
		if !in {
			continue
		}
		if !strings.HasPrefix(l, "        ") {
			// 次のカテゴリの見出し（字下げが浅い）に来たら終わり。
			break
		}
		// カテゴリの説明にも「引き継ぎ先」の語が出るので、note の書き出しで拾う。
		if strings.Contains(l, "（引き継ぎ先 ") {
			out = append(out, l)
		}
	}
	return out
}

// findingLineFor は一覧のうち、引き継ぎ先が書かれた key の行を返す。
func findingLineFor(t *testing.T, stdout, key string) string {
	t.Helper()
	for _, l := range strings.Split(stdout, "\n") {
		if strings.Contains(l, key) && strings.Contains(l, "引き継ぎ先") {
			return l
		}
	}
	t.Fatalf("%s の引き継ぎ候補の行が無い:\n%s", key, stdout)
	return ""
}

// TestRunDiffCarryoverHeldWithoutGit は、1つ前の再生順を取り出せないときに
// 「0 件」と書かず、理由を添えて保留することを確かめる。
//
// csv 形式まで見るのは、そちらに保留を書く場所が無いため。行が1つも無いだけだと
// 「引き継ぎ先は無い」と読まれ、翻訳者は移すべき訳をそのまま捨てる。
func TestRunDiffCarryoverHeldWithoutGit(t *testing.T) {
	// git リポジトリではないので、1つ前の再生順を取り出す手立てが無い。
	root := diffTree(t, map[string]string{
		"data/script_order.csv":       diffCarryOrderCSV,
		"Translations/ja/strings.csv": diffCarryPublishedJA,
	})

	code, stdout, stderr := runCLI("diff", "--root", root)
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	// text 形式は本文に理由を書くので、標準エラーへ重ねて出さない。
	if stderr != "" {
		t.Errorf("text 形式で標準エラーへ何か出ている:\n%s", stderr)
	}
	checkContains(t, "stdout", stdout, []string{
		"引き継ぎ候補", "判定していません（", "git リポジトリではないか、コミットがありません",
	})
	if strings.Contains(stdout, "引き継ぎ先") {
		t.Errorf("旧再生順が無いのに候補を出している:\n%s", stdout)
	}

	code, stdout, stderr = runCLI("diff", "--root", root, "--format", "csv")
	if code != exitProblems {
		t.Fatalf("csv の終了コード = %d, 期待 %d\nstderr:\n%s", code, exitProblems, stderr)
	}
	// csv 本体は表計算に貼る形のままにして、保留は標準エラーへ書く。
	checkContains(t, "stderr", stderr, []string{
		"「引き継ぎ候補」は判定しません", "git リポジトリではないか、コミットがありません",
	})
	if !slices.Contains(heldTail(t, stderr), "carryover") {
		t.Errorf("carryover の行が無いことを 0 件と読ませない断りが無い:\n%s", stderr)
	}
	// 同じ保留を別の文面で2度書かない。以前は runDiff と warnHeldCarryover
	// （いまの warnHeldCategories）の両方が書いていて、しかも後者は理由の
	// 受け皿を通していなかった。
	if n := strings.Count(stderr, "「引き継ぎ候補」は判定しません"); n != 1 {
		t.Errorf("保留を %d 回書いている:\n%s", n, stderr)
	}
	// 台詞IDは読めているので、台本に無い台詞ID行は止めていない。
	if strings.Contains(stderr, "stray_line_id") {
		t.Errorf("判定している stray_line_id を止めたと書いている:\n%s", stderr)
	}
	if strings.Contains(stdout, "carryover") {
		t.Errorf("判定していないのに carryover の行がある:\n%s", stdout)
	}
	if strings.Contains(stdout, "警告") {
		t.Errorf("csv 本体に人向けの文面が混ざっている:\n%s", stdout)
	}
}

// TestRunDiffCSVClosingLineFollowsHeldWarnings は、csv の標準エラーで、締めの1行
// （「… の行が無いことは、0 件という意味ではありません」）が、判定を保留した
// ことを述べる警告のすぐ後に来ることを見る。
//
// 締めの1行は字下げして、直前の警告の続きとして読ませている。再生順が無く、
// 作業コピーとはみ出しの記録はあるときは、保留の理由の行が1つも出ず、再生順の
// 警告に締めが続く。そこへ「訳が1件もないロケール」の行が挟まると、締めがその
// 行の続きに読め、どのカテゴリの話なのかが分からなくなる。
func TestRunDiffCSVClosingLineFollowsHeldWarnings(t *testing.T) {
	root := makeTree(t, map[string]string{
		"Translations/ja/strings.csv": diffCleanJA,
		"Translations/_discovered/ja.working.csv": "key,source_en,translation\n" +
			diffHelloKey + ",Hello,こんにちは\n",
		"Translations/_discovered/layout_risks.csv": "source_en,translation,axis,required_px,available_px,ratio,object_path\n" +
			"Hello,こんにちは,x,240,180,1.33,Canvas/Label\n",
		"Translations/de/.keep": "",
	})

	code, stdout, stderr := runCLI("diff", "--root", root, "--format", "csv")
	if code == exitError {
		t.Fatalf("終了コード = %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
	closing := -1
	for i, line := range lines {
		if strings.Contains(line, "の行が無いことは、0 件という意味ではありません") {
			closing = i
		}
	}
	if closing < 1 {
		t.Fatalf("締めの1行が無い:\n%s", stderr)
	}
	if prev := lines[closing-1]; !strings.HasPrefix(prev, "dwloc: 警告:") {
		t.Errorf("締めの1行の直前が、保留を述べた警告ではない: %q\n--- 標準エラー ---\n%s", prev, stderr)
	}
	// 訳が1件もないロケールも、並びが変わっただけで伝えている。
	checkContains(t, "stderr", stderr, []string{"訳が1件もないロケールがあります: de"})
}
