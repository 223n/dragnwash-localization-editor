package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
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
			wantStderr: []string{"余分な引数です", "使い方: dwloc diff"},
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
	checkContains(t, "stderr", stderr,
		[]string{"いまの版と同じ内容に見えます。引き継ぎ候補は判定しません: ja"})
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

// TestOldOrderReasonTextFallback は、理由が空でも「（）」と書かないことを見る。
//
// internal/diff は理由を必ず埋める約束だが、空のまま出すと理由を取り違えたように
// 見える。最後の受け皿の文面が出ること。
func TestOldOrderReasonTextFallback(t *testing.T) {
	if got := oldOrderReasonText(&diff.Repo{OldOrderReason: "git を実行できません"}); got != "git を実行できません" {
		t.Errorf("理由がそのまま出ていない: %q", got)
	}
	if got := oldOrderReasonText(&diff.Repo{}); got != "1つ前の再生順を読めません" {
		t.Errorf("受け皿の文面 = %q", got)
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
		"引き継ぎ候補は判定しません", "git リポジトリではないか、コミットがありません",
		"引き継ぎ先が無いという意味ではありません",
	})
	if strings.Contains(stdout, "carryover") {
		t.Errorf("判定していないのに carryover の行がある:\n%s", stdout)
	}
	if strings.Contains(stdout, "警告") {
		t.Errorf("csv 本体に人向けの文面が混ざっている:\n%s", stdout)
	}
}
