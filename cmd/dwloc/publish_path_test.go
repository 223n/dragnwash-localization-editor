package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// TestRunPublishPathOption は --path 指定を確かめる。元実装 tools/hash-strings.ps1 の
// -Path にあたり、Translations の走査をせず、指定したファイルを入出力兼用にする
// （移植仕様 R8 前半）。
func TestRunPublishPathOption(t *testing.T) {
	// 渡すのは公開ファイルの形。位置の列が空の行は、再生順から位置を埋め直す。
	const mine = "key,section,node,order,speaker,translation\n"
	root := makeTree(t, map[string]string{
		"data/script_order.csv": scriptOrderCSV,
		"手元/my.csv":             mine + helloKey + ",,,,,こんにちは\n",
		// 走査されたら混ざるはずのロケール。--path のときは触らないこと。
		"Translations/ja/strings.csv": workingCSV,
	})
	target := filepath.Join(root, "手元", "my.csv")

	code, stdout, stderr := runCLI("publish", "--root", root, "--path", target)
	if code != exitOK {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
	}
	checkContains(t, "stdout", stdout, []string{"1 already hashed", "1 in play order", "1 件を書き出しました。"})

	got := readFile(t, root, "手元/my.csv")
	checkContains(t, "出力", got, []string{helloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは"})

	// Translations 側は手つかずのまま。
	if other := readFile(t, root, "Translations/ja/strings.csv"); other != workingCSV {
		t.Errorf("--path なのに Translations が書き換わった:\n%s", other)
	}
}

// publishedHello は publish が書く形（source_en 列の無い6列）の公開ファイル。
// --path に渡すのはこの形のファイルである。
var publishedHello = publish.HeaderLine + "\n" +
	helloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n"

// TestPublishPathRefusesAWorkingCopy は、--path にヘッダーに source_en 列のある
// ファイル（作業コピー）を渡したとき、書かずに止めて、何を渡すかを案内することを
// 見る（改善の決定 11、改善の調査の cli-missed-1）。
//
// --path は入力と書き出し先が同じファイルなので、作業コピーを渡すと、その作業コピーを
// 公開の形に書き換える。source_en 列と訳の無い行が消え、終了コード0で警告も出ない。
// ゲーム側の作業コピー（edit の保存先）を指すと、次の起動から原文の欄と「未翻訳」の
// 判定が消える。上流の tools/hash-strings.ps1 も -Path には公開の strings.csv を渡すよう
// 書いている。列名の照合は publish と同じく大文字小文字を区別しない。
func TestPublishPathRefusesAWorkingCopy(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		extra         []string
	}{
		{"作業コピー", workingCSV, nil},
		{"--dry-run でも", workingCSV, []string{"--dry-run"}},
		{"列名の大文字小文字", "key,Source_EN,translation\n,Hello,こんにちは\n", nil},
		{"ゲームの作業コピーの7列", "key,section,node,order,speaker,source_en,translation\n,L01 Ryan,Ryan_1_intro,1,Ryan,Hello,こんにちは\n,L01 Ryan,Ryan_1_intro,2,Ryan,Bye,\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := makeTree(t, map[string]string{
				"data/script_order.csv":                   scriptOrderCSV,
				"Translations/_discovered/ja.working.csv": tc.content,
			})
			path := filepath.Join(root, "Translations", "_discovered", "ja.working.csv")
			args := append([]string{"publish", "--root", root, "--path", path}, tc.extra...)
			code, stdout, stderr := runCLI(args...)
			if code != exitError {
				t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
			}
			checkContains(t, "stderr", stderr, []string{
				"dwloc: Translations/_discovered/ja.working.csv は作業コピーです（ヘッダーに source_en 列があります）。",
				"--path には公開ファイル（Translations/<ロケール>/strings.csv）を渡してください。",
				"--path を付けずに dwloc publish を実行します",
			})
			if stdout != "" {
				t.Errorf("止めたのに標準出力へ書いている:\n%s", stdout)
			}
			if got := readFile(t, root, "Translations/_discovered/ja.working.csv"); got != tc.content {
				t.Errorf("作業コピーが書き換わった:\n%s", got)
			}
		})
	}
}

// TestPublishPathIsRelativeToTheCurrentDirectory は、--path をカレントディレクトリからの
// 相対で解くことと、見つからないときにそのことを添えることを見る（改善の決定 11、
// 改善の調査の cli-12）。
//
// 上流の tools/hash-strings.ps1 の -Path も PowerShell の現在の場所から解くので、
// --root からにはしない。--root と組み合わせた人が「見つからない」とだけ言われて
// 戸惑わないよう、どこから解いたかを添える。
func TestPublishPathIsRelativeToTheCurrentDirectory(t *testing.T) {
	root := makeTree(t, map[string]string{
		"data/script_order.csv":       scriptOrderCSV,
		"Translations/ja/strings.csv": publishedHello,
	})
	rel := filepath.Join("Translations", "ja", "strings.csv")

	t.Run("別のディレクトリからは見つからない", func(t *testing.T) {
		t.Chdir(t.TempDir())
		code, stdout, stderr := runCLI("publish", "--root", root, "--path", rel, "--dry-run")
		if code != exitError {
			t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
		}
		checkContains(t, "stderr", stderr, []string{
			"見つかりません",
			"dwloc:       --path はカレントディレクトリからの相対です（--root からではありません）。",
		})
	})

	t.Run("ルートへ移れば見つかる", func(t *testing.T) {
		t.Chdir(root)
		code, stdout, stderr := runCLI("publish", "--root", root, "--path", rel, "--dry-run")
		if code != exitOK {
			t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
		}
		checkContains(t, "stdout", stdout, []string{"1 already hashed", "[dry-run] 1 件中"})
	})

	t.Run("使い方に書いてある", func(t *testing.T) {
		code, stdout, _ := runCLI("publish", "--help")
		if code != exitOK {
			t.Fatalf("終了コード = %d", code)
		}
		checkContains(t, "publish --help", stdout, []string{
			"パスはカレントディレクトリからの相対です",
			"ヘッダーに source_en 列のあるファイル（作業コピー）",
		})
	})
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
	// 1行目はキーの列に英文が入っている。キーを決められないので publish はこの行を
	// 捨てる（diff の「publish で捨てられる行」と同じ形）。--path に渡すのは公開
	// ファイルの形なので、原文の列は無い。
	lossy := "key,translation\n" +
		"English text,訳した文\n" +
		helloKey + ",こんにちは\n"
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
