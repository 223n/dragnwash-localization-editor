package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRunPublishPathOption は --path 指定を確かめる。元実装 tools/hash-strings.ps1 の
// -Path にあたり、Translations の走査をせず、指定したファイルを入出力兼用にする
// （移植仕様 R8 前半）。
func TestRunPublishPathOption(t *testing.T) {
	root := makeTree(t, map[string]string{
		"data/script_order.csv": scriptOrderCSV,
		"手元/my.csv":             workingCSV,
		// 走査されたら混ざるはずのロケール。--path のときは触らないこと。
		"Translations/ja/strings.csv": workingCSV,
	})
	target := filepath.Join(root, "手元", "my.csv")

	code, stdout, stderr := runCLI("publish", "--root", root, "--path", target)
	if code != exitOK {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
	}
	checkContains(t, "stdout", stdout, []string{"1 converted", "1 件を書き出しました。"})

	got := readFile(t, root, "手元/my.csv")
	checkContains(t, "出力", got, []string{helloKey, "こんにちは"})
	if strings.Contains(got, "Hello") {
		t.Errorf("原文が残っている:\n%s", got)
	}

	// Translations 側は手つかずのまま。
	if other := readFile(t, root, "Translations/ja/strings.csv"); other != workingCSV {
		t.Errorf("--path なのに Translations が書き換わった:\n%s", other)
	}
}

// TestRunPublishPathWithLocaleIsRejected は --path と --locale の同時指定を
// エラーにすることを確かめる。片方を黙って無視すると、絞ったつもりの指定が
// 効かないまま書き換わる。
func TestRunPublishPathWithLocaleIsRejected(t *testing.T) {
	root := publishTree(t, "ja")

	code, stdout, stderr := runCLI("publish", "--root", root,
		"--path", filepath.Join(root, "Translations", "ja", "strings.csv"), "--locale", "ja")
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
	}
	checkContains(t, "stderr", stderr, []string{"--path と --locale は同時に指定できません"})
}

// TestRunPublishWritesNothingWhenOneTargetFails は、1件でも組み立てに失敗したら
// 何も書かないことを確かめる。
//
// 列名の重複は読み始めて初めて分かる失敗なので、事前の検査では防げない。
// 1件ずつ書きながら進むと、先頭のロケールだけ新しい内容、残りは古い内容という
// 半端な状態で終わる。
func TestRunPublishWritesNothingWhenOneTargetFails(t *testing.T) {
	const brokenCSV = "key,translation,TRANSLATION\n" + "0000000000000001,あ,い\n"

	root := makeTree(t, map[string]string{
		"data/script_order.csv":       scriptOrderCSV,
		"Translations/aa/strings.csv": workingCSV,
		"Translations/zz/strings.csv": brokenCSV,
	})

	code, stdout, stderr := runCLI("publish", "--root", root)
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
	}
	checkContains(t, "stderr", stderr, []string{"Translations/zz/strings.csv を変換できません"})

	// 先に並ぶ aa も書き換わっていないこと。
	if got := readFile(t, root, "Translations/aa/strings.csv"); got != workingCSV {
		t.Errorf("失敗したのに aa が書き換わった:\n%s", got)
	}
	if got := readFile(t, root, "Translations/zz/strings.csv"); got != brokenCSV {
		t.Errorf("失敗したのに zz が書き換わった:\n%s", got)
	}
}

// TestPathListSet は --path の値の取り込みを確かめる。カンマでは分けない。
func TestPathListSet(t *testing.T) {
	var l pathList
	if err := l.Set("a,b.csv"); err != nil {
		t.Fatalf("Set が失敗した: %v", err)
	}
	if err := l.Set("c.csv"); err != nil {
		t.Fatalf("Set が失敗した: %v", err)
	}
	if len(l) != 2 || l[0] != "a,b.csv" || l[1] != "c.csv" {
		t.Errorf("pathList = %q, want [a,b.csv c.csv]", []string(l))
	}
	if err := l.Set("   "); err == nil {
		t.Error("空白だけの指定がエラーにならなかった")
	}
}

// TestPathListString は flag.Value としての表示を確かめる。
//
// 区切りはカンマではなく OS のリスト区切り（PATH と同じ）にしてある。
// パス名にはカンマを入れられるので、カンマで並べると1件と2件が見分けられない。
// ゼロ値のポインターでも落ちないことも見る（[TestLocaleListString] と同じ理由）。
func TestPathListString(t *testing.T) {
	var none *pathList
	if got := none.String(); got != "" {
		t.Errorf("nil の表示 = %q, 期待 \"\"", got)
	}
	list := pathList{"a,b.csv", "c.csv"}
	want := "a,b.csv" + string(filepath.ListSeparator) + "c.csv"
	if got := list.String(); got != want {
		t.Errorf("表示 = %q, 期待 %q", got, want)
	}
}

// TestRunPublishPathStopsWhenTranslationsWouldBeLost は、--path でも訳が失われる
// なら書かずに止まることを見る。
//
// --path は入力と出力が同じファイルなので、止めずに書けば、消える訳を持っていた
// 原本そのものが上書きされる。取り戻す先が無い。
//
// 報告の頭にはロケール名を付けない。--path ではロケールが決まらないので、
// 付けるとすれば当て推量になる。
func TestRunPublishPathStopsWhenTranslationsWouldBeLost(t *testing.T) {
	// 1行目はキーの列に英文が入っていて原文の列が空。キーを決められないので
	// publish はこの行を捨てる（diff の「publish で捨てられる行」と同じ形）。
	const lossy = "key,source_en,translation\n" +
		"English text,,訳した文\n" +
		",Hello,こんにちは\n"
	root := makeTree(t, map[string]string{
		"data/script_order.csv": scriptOrderCSV,
		"手元/my.csv":             lossy,
	})
	target := filepath.Join(root, "手元", "my.csv")

	code, stdout, stderr := runCLI("publish", "--root", root, "--path", target)
	if code != exitProblems {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitProblems, stdout, stderr)
	}
	checkContains(t, "stderr", stderr, []string{
		"訳が失われるので、1バイトも書きませんでした",
		// ロケール名の無い見出し。パスはルートからの相対。
		"dwloc:   手元/my.csv（1 件）",
		"2行目 English text 「訳した文」",
		"失われる訳が 1 件あります",
	})
	if got := readFile(t, root, "手元/my.csv"); got != lossy {
		t.Errorf("止めたのにファイルが変わっている:\n%s", got)
	}
	if strings.Contains(stdout, "書き出しました") {
		t.Errorf("書き出したと出ている:\n%s", stdout)
	}
}
