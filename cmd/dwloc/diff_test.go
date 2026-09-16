package main

import (
	"strings"
	"testing"

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
		"要確認が 2 件あります。",
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
	checkContains(t, "stdout", stdout, []string{"publish で捨てられる行", "訳した文", "要確認が 1 件あります。"})
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
