package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// helloKey は入力に置く英文のキー。ハッシュを直書きすると、値を変えたときに
// テストだけが古いまま通ってしまうので、internal/key で計算する。
var helloKey = key.For("Hello")

// scriptOrderCSV は "Hello" を1回だけ再生する最小の再生順。
var scriptOrderCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
	"L01 Ryan,intro,Ryan_1_intro,1,line:a8779ebf," + helloKey + ",Ryan,\n"

// workingCSV は翻訳者の手元にある形。原文の列があり、キーはまだ入っていない。
const workingCSV = "key,source_en,translation\n" +
	",Hello,こんにちは\n"

// publishTree は publish を試すための最小のリポジトリを作る。
func publishTree(t *testing.T, locales ...string) string {
	t.Helper()

	files := map[string]string{"data/script_order.csv": scriptOrderCSV}
	for _, locale := range locales {
		files["Translations/"+locale+"/strings.csv"] = workingCSV
	}
	return makeTree(t, files)
}

func TestRunPublishWritesPublishedFile(t *testing.T) {
	root := publishTree(t, "ja")

	code, stdout, stderr := runCLI("publish", "--root", root)
	if code != exitOK {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
	}

	// 元実装と同じ集計の1行が出ること。パスは絶対ではなくリポジトリ相対。
	checkContains(t, "stdout", stdout, []string{
		"Translations/ja/strings.csv <- Translations/ja/strings.csv",
		"1 converted", "0 already hashed", "0 malformed dropped", "1 in play order", "0 other",
		"1 件を書き出しました。",
	})
	if stderr != "" {
		t.Errorf("標準エラーへ何か出ている:\n%s", stderr)
	}

	got := readFile(t, root, "Translations/ja/strings.csv")
	lines := strings.Split(got, "\n")
	if lines[0] != publish.HeaderLine {
		t.Errorf("1行目 = %q, 期待 %q", lines[0], publish.HeaderLine)
	}
	checkContains(t, "出力", got, []string{helloKey, "こんにちは", "Ryan_1_intro"})
	if strings.Contains(got, "Hello") {
		t.Errorf("公開ファイルに英語の原文が残っている:\n%s", got)
	}
}

func TestRunPublishThenValidate(t *testing.T) {
	// publish の出力が validate を通ること。2つのサブコマンドのつなぎ目の確認で、
	// 元の手順（hash-strings.ps1 のあとに check-translations.py）と同じ並び。
	root := publishTree(t, "ja")

	if code, stdout, stderr := runCLI("publish", "--root", root); code != exitOK {
		t.Fatalf("publish の終了コード = %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	code, stdout, stderr := runCLI("validate", "--root", root)
	if code != exitOK {
		t.Fatalf("validate の終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout, stderr)
	}
	if stdout != "translations OK\n" {
		t.Errorf("stdout = %q, 期待 %q", stdout, "translations OK\n")
	}
}

func TestRunPublishDryRun(t *testing.T) {
	root := publishTree(t, "ja")
	before := readFile(t, root, "Translations/ja/strings.csv")

	// 1回目。まだ変換していないので「変更あり」になり、ファイルは書かれない。
	code, stdout, stderr := runCLI("publish", "--root", root, "--dry-run")
	if code != exitOK {
		t.Fatalf("終了コード = %d, 期待 %d\nstderr:\n%s", code, exitOK, stderr)
	}
	checkContains(t, "stdout", stdout, []string{"[dry-run]", "変更あり", "1 件中 1 件が変わります"})
	if got := readFile(t, root, "Translations/ja/strings.csv"); got != before {
		t.Errorf("--dry-run なのにファイルが書き換わっている:\n%s", got)
	}

	// 本番の実行を挟むと、次の --dry-run は「変更なし」になる。
	if code, _, stderr := runCLI("publish", "--root", root); code != exitOK {
		t.Fatalf("publish の終了コード = %d\nstderr:\n%s", code, stderr)
	}
	published := readFile(t, root, "Translations/ja/strings.csv")

	code, stdout, stderr = runCLI("publish", "--root", root, "--dry-run")
	if code != exitOK {
		t.Fatalf("終了コード = %d, 期待 %d\nstderr:\n%s", code, exitOK, stderr)
	}
	checkContains(t, "stdout", stdout, []string{"変更なし", "1 件中 0 件が変わります"})
	if got := readFile(t, root, "Translations/ja/strings.csv"); got != published {
		t.Errorf("--dry-run でファイルが変わっている")
	}
}

func TestRunPublishLocaleOption(t *testing.T) {
	tests := []struct {
		name string
		// args は publish と --root のあとに置く引数。
		args []string
		// wantWritten は書き換わっているはずのロケール。
		wantWritten []string
		// wantUntouched は入力のままのはずのロケール。
		wantUntouched []string
		wantCode      int
		// containsStderr はエラー時に含まれていてほしい文字列。
		containsStderr []string
	}{
		{
			name:        "指定しなければ全ロケールが対象",
			args:        nil,
			wantWritten: []string{"de", "ja"},
			wantCode:    exitOK,
		},
		{
			name:          "1つだけ指定すると他は触らない",
			args:          []string{"--locale", "ja"},
			wantWritten:   []string{"ja"},
			wantUntouched: []string{"de"},
			wantCode:      exitOK,
		},
		{
			name:        "カンマ区切りで複数を指定できる",
			args:        []string{"--locale", "ja,de"},
			wantWritten: []string{"de", "ja"},
			wantCode:    exitOK,
		},
		{
			name:        "繰り返し指定もできる",
			args:        []string{"--locale", "ja", "--locale", "de"},
			wantWritten: []string{"de", "ja"},
			wantCode:    exitOK,
		},
		{
			name:          "同じロケールを2回指定しても1回だけ処理する",
			args:          []string{"--locale", "ja", "--locale", "ja"},
			wantWritten:   []string{"ja"},
			wantUntouched: []string{"de"},
			wantCode:      exitOK,
		},
		{
			name:          "大文字小文字が違っても拾う",
			args:          []string{"--locale", "JA"},
			wantWritten:   []string{"ja"},
			wantUntouched: []string{"de"},
			wantCode:      exitOK,
		},
		{
			name:           "無いロケールを指定したらエラーにする",
			args:           []string{"--locale", "fr"},
			wantUntouched:  []string{"de", "ja"},
			wantCode:       exitError,
			containsStderr: []string{"--locale", "fr", "de, ja"},
		},
		{
			name:           "1つでも見つからなければ何も書かない",
			args:           []string{"--locale", "ja,fr"},
			wantUntouched:  []string{"de", "ja"},
			wantCode:       exitError,
			containsStderr: []string{"fr"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := publishTree(t, "ja", "de")

			args := append([]string{"publish", "--root", root}, tt.args...)
			code, stdout, stderr := runCLI(args...)
			if code != tt.wantCode {
				t.Errorf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, tt.wantCode, stdout, stderr)
			}
			checkContains(t, "stderr", stderr, tt.containsStderr)

			for _, locale := range tt.wantWritten {
				path := "Translations/" + locale + "/strings.csv"
				if got := readFile(t, root, path); got == workingCSV {
					t.Errorf("%s が書き換わっていない", path)
				}
			}
			for _, locale := range tt.wantUntouched {
				path := "Translations/" + locale + "/strings.csv"
				if got := readFile(t, root, path); got != workingCSV {
					t.Errorf("%s を触らないはずが書き換わっている:\n%s", path, got)
				}
			}

			// 出力の並びはディレクトリ名順。指定した順ではない。
			if tt.wantCode == exitOK && len(tt.wantWritten) == 2 {
				if strings.Index(stdout, "Translations/de/") > strings.Index(stdout, "Translations/ja/") {
					t.Errorf("ログの並びがディレクトリ名順ではない:\n%s", stdout)
				}
			}
		})
	}
}

func TestRunPublishUsesWorkingCopy(t *testing.T) {
	// 作業コピーがあれば入力はそちら。出力は常に公開ファイルで、作業コピーは残る。
	root := makeTree(t, map[string]string{
		"data/script_order.csv":                   scriptOrderCSV,
		"Translations/ja/strings.csv":             "key,section,node,order,speaker,translation\n",
		"Translations/_discovered/ja.working.csv": workingCSV,
	})

	code, stdout, stderr := runCLI("publish", "--root", root)
	if code != exitOK {
		t.Fatalf("終了コード = %d, 期待 %d\nstderr:\n%s", code, exitOK, stderr)
	}
	checkContains(t, "stdout", stdout, []string{
		"Translations/ja/strings.csv <- Translations/_discovered/ja.working.csv",
	})
	checkContains(t, "出力", readFile(t, root, "Translations/ja/strings.csv"), []string{helloKey, "こんにちは"})
	if got := readFile(t, root, "Translations/_discovered/ja.working.csv"); got != workingCSV {
		t.Errorf("作業コピーが書き換わっている:\n%s", got)
	}
}

func TestRunPublishErrors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		// wantCode は期待する終了コード。
		wantCode int
		// containsStderr / containsStdout は含まれていてほしい文字列。
		containsStderr []string
		containsStdout []string
	}{
		{
			name:           "Translations が無ければエラー",
			files:          map[string]string{"data/script_order.csv": scriptOrderCSV},
			wantCode:       exitError,
			containsStderr: []string{"Translations を読めません"},
		},
		{
			name: "対象が1つも無ければエラー",
			files: map[string]string{
				"data/script_order.csv": scriptOrderCSV,
				// ロケールのディレクトリはあるが入力になるファイルが無い。
				"Translations/ja/README.md": "",
			},
			wantCode:       exitError,
			containsStderr: []string{"対象になるロケールがありません", "Translations"},
		},
		{
			name: "再生順の列名が重複していればエラー",
			files: map[string]string{
				"data/script_order.csv":       "key,key\nabc,def\n",
				"Translations/ja/strings.csv": workingCSV,
			},
			wantCode:       exitError,
			containsStderr: []string{"再生順のデータを読めません"},
		},
		{
			name: "再生順が無くても生成はするが警告を出す",
			files: map[string]string{
				"Translations/ja/strings.csv": workingCSV,
			},
			wantCode:       exitOK,
			containsStderr: []string{"警告", "data/script_order.csv"},
			containsStdout: []string{"1 converted", "0 in play order", "1 other"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := makeTree(t, tt.files)

			code, stdout, stderr := runCLI("publish", "--root", root)
			if code != tt.wantCode {
				t.Errorf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, tt.wantCode, stdout, stderr)
			}
			checkContains(t, "stderr", stderr, tt.containsStderr)
			checkContains(t, "stdout", stdout, tt.containsStdout)
		})
	}
}

func TestLocaleListSet(t *testing.T) {
	tests := []struct {
		name string
		// values は --locale に渡す値を順に並べたもの。
		values []string
		want   []string
		// wantErr は Set がエラーを返すことを期待する。
		wantErr bool
	}{
		{name: "1つ", values: []string{"ja"}, want: []string{"ja"}},
		{name: "繰り返し", values: []string{"ja", "de"}, want: []string{"ja", "de"}},
		{name: "カンマ区切り", values: []string{"ja,de"}, want: []string{"ja", "de"}},
		{name: "前後の空白は落とす", values: []string{" ja , de "}, want: []string{"ja", "de"}},
		{name: "大文字はそのまま保つ", values: []string{"pt-BR"}, want: []string{"pt-BR"}},
		{name: "空はエラー", values: []string{""}, wantErr: true},
		{name: "カンマだけもエラー", values: []string{","}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var list localeList
			var err error
			for _, v := range tt.values {
				if err = list.Set(v); err != nil {
					break
				}
			}
			if tt.wantErr {
				if err == nil {
					t.Fatalf("エラーになるはずが %v を受け入れた", tt.values)
				}
				return
			}
			if err != nil {
				t.Fatalf("エラーになった: %v", err)
			}
			if strings.Join(list, ",") != strings.Join(tt.want, ",") {
				t.Errorf("= %v, 期待 %v", []string(list), tt.want)
			}
		})
	}
}

// TestLocaleListString は flag.Value としての表示を確かめる。
//
// flag は既定値を表示するときに、ゼロ値のポインターでも String を呼ぶことがある。
// そこで落ちると、使い方の表示が panic に化ける。
func TestLocaleListString(t *testing.T) {
	var none *localeList
	if got := none.String(); got != "" {
		t.Errorf("nil の表示 = %q, 期待 \"\"", got)
	}
	list := localeList{"ja", "pt-BR"}
	// Set が受けるのと同じカンマ区切りで表示する。表示をそのまま打ち直せる。
	if got, want := list.String(), "ja,pt-BR"; got != want {
		t.Errorf("表示 = %q, 期待 %q", got, want)
	}
}

// TestRunPublishReportsWriteFailure は、書き出しに失敗したときに終了コード2で
// 止まり、元のファイルを壊さないことを見る。
//
// 既定では入力と出力が同じファイルなので、書きかけで残ると原本を失う。
// 「書き出しました」と言ってもいけない。コミットする中身が古いままになる。
func TestRunPublishReportsWriteFailure(t *testing.T) {
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		t.Skip("root は書き込みの権限を無視するので、失敗を作れない")
	}
	root := publishTree(t, "ja")
	dir := filepath.Join(root, "Translations", "ja")
	path := filepath.Join(dir, "strings.csv")
	// Windows は読み取り専用のファイルへの置き換えを断り、Linux と macOS は
	// 書けないディレクトリに一時ファイルを作れない。どちらでも書き出しが落ちる。
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	// 戻さないと t.TempDir が後片付けで消せない。
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
		_ = os.Chmod(path, 0o644)
	})

	code, stdout, stderr := runCLI("publish", "--root", root)
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
	}
	checkContains(t, "stderr", stderr, []string{"Translations/ja/strings.csv を書き出せません"})
	if strings.Contains(stdout, "件を書き出しました") {
		t.Errorf("失敗したのに書き出したと言っている:\n%s", stdout)
	}
	if got := readFile(t, root, "Translations/ja/strings.csv"); got != workingCSV {
		t.Errorf("元のファイルが変わっている:\n%s", got)
	}
	// 書きかけの一時ファイルをリポジトリに残さない。残るとコミットに紛れ込む。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "strings.csv" {
			t.Errorf("書きかけのファイルが残っている: %s", e.Name())
		}
	}
}

func TestSelectLocales(t *testing.T) {
	targets := []publish.Target{
		{Locale: "de"},
		{Locale: "ja"},
		{Locale: "pt-BR"},
	}

	tests := []struct {
		name string
		want []string
		// wantOut は選ばれるロケールを順に並べたもの。
		wantOut []string
		wantErr bool
	}{
		{name: "指定なしは全部", want: nil, wantOut: []string{"de", "ja", "pt-BR"}},
		{name: "完全一致", want: []string{"ja"}, wantOut: []string{"ja"}},
		{name: "並びは指定順ではなく元の順", want: []string{"ja", "de"}, wantOut: []string{"de", "ja"}},
		{name: "重複は1回だけ", want: []string{"ja", "ja"}, wantOut: []string{"ja"}},
		{name: "大小を無視して一致", want: []string{"pt-br"}, wantOut: []string{"pt-BR"}},
		{name: "無いものはエラー", want: []string{"fr"}, wantErr: true},
		{name: "1つでも無ければエラー", want: []string{"ja", "fr"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectLocales(targets, tt.want)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("エラーになるはずが %v を受け入れた", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("エラーになった: %v", err)
			}
			if strings.Join(localeNames(got), ",") != strings.Join(tt.wantOut, ",") {
				t.Errorf("= %v, 期待 %v", localeNames(got), tt.wantOut)
			}
		})
	}
}
