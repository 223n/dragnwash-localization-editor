// Command dwloc は Drag'n Wash の翻訳リポジトリを扱うコマンドです。
//
// サブコマンドは5つあります。
//
//	dwloc validate   公開ファイルを検証する（tools/check-translations.py の移植）
//	dwloc diff       公開ファイルと再生順を突き合わせ、次にやることを並べる
//	dwloc edit       ブラウザーで行を読む（いまは読み取り専用）
//	dwloc publish    公開用CSVを生成する（tools/hash-strings.ps1 の移植）
//	dwloc version    版を表示する
//
// 終了コードは 0 が成功、1 が「実行はできたが、人が見るべきものが残っている」、
// 2 が実行時のエラーです。1 を返すのは validate が問題を見つけたときと、
// diff が要確認を見つけたとき（--strict なら要作業も）です。
// この3段に分けているのは、CIが「検証に落ちた」と「そもそも実行できなかった」を
// 区別できるようにするためです。元実装の check-translations.py は前者だけを1で返し、
// 後者はトレースバックで落ちていました（移植仕様「形式検証 / 未決の点」）。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// version は表示する版。既定は "dev" で、リリース時は次のように差し替えます。
//
//	go build -ldflags "-X main.version=1.2.3" ./cmd/dwloc
//
// var にしているのは -ldflags -X が const を書き換えられないためです。
var version = "dev"

// 終了コード。値はこのコマンドの約束なので、増やすときも既存の意味を変えないこと。
const (
	// exitOK は成功。
	exitOK = 0
	// exitProblems は validate が問題を見つけたときと、diff が要確認を
	// 見つけたとき。実行そのものは成功している。
	exitProblems = 1
	// exitError は実行できなかったとき。引数の誤り、ファイルが読めない、などが入る。
	exitError = 2
)

// usageText は引数なしや help のときに出す説明。
//
// 使い方の表示は日本語で書く。翻訳者が使うコマンドで、対象の利用者が
// 日本語話者だという前提による。
const usageText = `dwloc は Drag'n Wash の翻訳リポジトリを扱うコマンドです。

使い方:
  dwloc <サブコマンド> [オプション]

サブコマンド:
  validate   公開ファイル（Translations/<ロケール>/strings.csv）を検証する
  publish    公開用CSVを生成し直す
  diff       公開ファイルと再生順を突き合わせ、次にやることを並べる
  edit       手元だけで待ち受けを始め、1ロケールの全行を1画面に出す（読み取り専用）
  version    版を表示する

共通のオプション:
  --root <ディレクトリ>
        翻訳リポジトリのルート（既定: カレントディレクトリ）。
        サブコマンドの前後どちらに置いてもかまいません。

終了コード:
  0   成功
  1   validate が問題を見つけた、または diff が要確認を見つけた
      （diff --strict では要作業でも 1 になります）
  2   実行時のエラー（引数の誤り、ファイルが読めない、など）

サブコマンドごとの説明は dwloc <サブコマンド> --help で表示します。
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run は引数を解釈してサブコマンドへ渡し、終了コードを返します。
//
// main から os.Exit を切り離しているのはテストのためです。標準出力と標準エラーも
// 引数で受け取るので、テストはバッファを渡して出力をそのまま確かめられます。
func run(args []string, stdout, stderr io.Writer) int {
	// 共通オプションは、サブコマンドの前にも置けるようにここで受ける。
	// flag パッケージは最初の非フラグ引数で解釈を止めるので、
	// "dwloc --root X validate --locale ja" のような並びが素直に通る。
	global := newFlagSet("dwloc", stderr)
	root := global.String("root", ".", "翻訳リポジトリのルート")

	if code, ok := parseFlags(global, args, usageText, stdout, stderr); !ok {
		return code
	}

	rest := global.Args()
	if len(rest) == 0 {
		fmt.Fprint(stderr, usageText)
		return exitError
	}

	switch name := rest[0]; name {
	case "validate":
		return runValidate(rest[1:], *root, stdout, stderr)
	case "publish":
		return runPublish(rest[1:], *root, stdout, stderr)
	case "diff":
		return runDiff(rest[1:], *root, stdout, stderr)
	case "edit":
		return runEdit(rest[1:], *root, stdout, stderr)
	case "version":
		return runVersion(rest[1:], stdout, stderr)
	case "help":
		fmt.Fprint(stdout, usageText)
		return exitOK
	default:
		fmt.Fprintf(stderr, "dwloc: 知らないサブコマンドです: %s\n\n", name)
		fmt.Fprint(stderr, usageText)
		return exitError
	}
}

// versionUsage は version の説明。
const versionUsage = `使い方: dwloc version

dwloc の版を1行で表示します。ビルド時に版を埋め込んでいなければ dev と出ます。
`

// runVersion は版を表示します。
func runVersion(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("dwloc version", stderr)
	// --root は使わないが、共通オプションのつもりで打たれても止まらないように受ける。
	// 版の表示に根拠となるディレクトリは要らないので、値は読まない。
	fs.String("root", "", "（version では使いません）")
	if code, ok := parseFlags(fs, args, versionUsage, stdout, stderr); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return unexpectedArg(fs.Arg(0), versionUsage, stderr)
	}

	fmt.Fprintf(stdout, "dwloc %s\n", version)
	return exitOK
}

// newFlagSet は FlagSet を作ります。エラー文の行き先は標準エラーです。
//
// Usage を空の関数にしているのは、説明を出す先を自分で決めるためです。
// flag は -h でも解釈の失敗でも Usage を呼びますが、前者は求められて出す説明
// （標準出力）、後者は失敗の報告（標準エラー）で、行き先が違います。
// flag に任せると両方が標準エラーへ出て、-h のときに二重に出ます。
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {}
	return fs
}

// parseFlags は共通の後始末つきで Parse を呼びます。
// 第2戻り値が false のとき、第1戻り値をそのまま終了コードにします。
func parseFlags(fs *flag.FlagSet, args []string, usage string, stdout, stderr io.Writer) (int, bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usage)
			return exitOK, false
		}
		// 解釈に失敗したときは flag 自身が理由の1行を標準エラーへ書いている。
		// その後ろに1行あけて使い方を足す。
		fmt.Fprintln(stderr)
		fmt.Fprint(stderr, usage)
		return exitError, false
	}
	return exitOK, true
}

// unexpectedArg は余分な引数を受け取ったときの報告です。
//
// 黙って無視しないのは、"dwloc publish ja" のような打ち間違いを
// 「全ロケールを書き出す」に化けさせないためです。
func unexpectedArg(arg, usage string, stderr io.Writer) int {
	fmt.Fprintf(stderr, "dwloc: 余分な引数です: %s\n\n", arg)
	fmt.Fprint(stderr, usage)
	return exitError
}

// displayPath は報告に出すパスを root からの相対にしてスラッシュ区切りで返します。
//
// 元実装の hash-strings.ps1 はログに絶対パスを出しますが、ここでは相対にします。
// 手元の絶対パスには利用者名が入ることがあり、CIのログや不具合報告へ貼られると
// そのまま漏れるためです。ルートの外にあるパスはそのまま返します。
func displayPath(root, path string) string {
	absRoot, rootErr := filepath.Abs(root)
	absPath, pathErr := filepath.Abs(path)
	if rootErr != nil || pathErr != nil {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return filepath.ToSlash(path)
	}
	// filepath.Rel は ".." を含む結果も成功で返すので、外へ出たものは自分で弾く。
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
