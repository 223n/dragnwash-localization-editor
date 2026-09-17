// Command dwloc は Drag'n Wash の翻訳リポジトリを扱うコマンドです。
//
// サブコマンドは5つあります。
//
//	dwloc validate   公開ファイルを検証する（tools/check-translations.py の移植）
//	dwloc diff       公開ファイルと再生順を突き合わせ、次にやることを並べる
//	dwloc edit       ブラウザーで訳を書き換える（サブコマンドを省くとこれになる）
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

	"github.com/223n/dragnwash-localization-editor/internal/gamedir"
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
  dwloc                      サブコマンドを省くと edit を始めます

サブコマンド:
  validate   公開ファイル（Translations/<ロケール>/strings.csv）を検証する
  publish    公開用CSVを生成し直す
  diff       公開ファイルと再生順を突き合わせ、次にやることを並べる
  edit       手元だけで待ち受けを始め、ブラウザーで訳を書き換える
  version    版を表示する

共通のオプション:
  --root <ディレクトリ>
        翻訳リポジトリのルート（既定: カレントディレクトリ）。
        サブコマンドの前後どちらに置いてもかまいません。
  --game <フォルダー>|auto
        作業コピーを探すゲームのプラグインフォルダー（diff / edit）。
        auto と書くと Steam のライブラリから探します。
        指定しないとゲームのフォルダーは見に行きません。
        publish は受け付けません（指定があると何も書かずに終わります）。
        validate は受け取りますが使いません。

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
	game := global.String("game", "", gameFlagUsage)

	if code, ok := parseFlags(global, args, usageText, stdout, stderr); !ok {
		return code
	}

	rest := global.Args()
	if len(rest) == 0 {
		return runDefault(args, *root, *game, stdout, stderr)
	}

	switch name := rest[0]; name {
	case "validate":
		return runValidate(rest[1:], *root, stdout, stderr)
	case "publish":
		return runPublish(rest[1:], *root, *game, stdout, stderr)
	case "diff":
		return runDiff(rest[1:], *root, *game, stdout, stderr)
	case "edit":
		return runEdit(rest[1:], *root, *game, stdout, stderr)
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

// stdin は Enter を待つときの読み取り先。テストが差し替えられるように変数にしてある。
var stdin io.Reader = os.Stdin

// startEdit は画面を始める関数。ここも変数にしてあるのは、引数なしの経路を
// 試すときに実際の待ち受けとブラウザーを起こさないためである。
var startEdit = runEdit

// runDefault はサブコマンドを省いて起動されたときの入口です。
//
// 翻訳者はコマンドプロンプトに慣れていないことが多いので、翻訳リポジトリの
// フォルダーへ dwloc を置いてダブルクリックするだけで画面が開くようにしてあります。
// ダブルクリックで開いた窓の作業ディレクトリは実行ファイルのある場所になるので、
// --root の既定（カレントディレクトリ）がそのまま効きます。
//
// 使い方の表示をここから外したのは、翻訳者にとって最初の1回がいちばん脱落しやすい
// ためです。使い方は dwloc help と dwloc --help で今までどおり出ます。
func runDefault(args []string, root, game string, stdout, stderr io.Writer) int {
	if !looksLikeRepo(root) {
		where, err := filepath.Abs(root)
		if err != nil {
			where = root
		}
		fmt.Fprintf(stderr, notARepoText, where)
		// ダブルクリックで開いた窓は、終わると同時に閉じる。理由を読む間も無く
		// 消えるので、引数を1つも受け取っていないときだけ Enter を待つ。
		// 端末から素の dwloc を打った場合もここを通るが、Enter を1回押すだけで済む。
		if len(args) == 0 {
			waitForEnter(stderr)
		}
		return exitError
	}
	// 画面を始める前に、ほかのこともできると伝える。使い方を出さなくなったぶん、
	// ここが唯一の手掛かりになる。
	fmt.Fprintln(stdout, "サブコマンドを指定すると、検証や公開もできます（dwloc help）。")
	return startEdit(nil, root, game, stdout, stderr)
}

// notARepoText は、翻訳リポジトリではない場所で起動されたときの案内です。
//
// 「見つかりません」だけで終わらせず、どこへ置けばよいかを図で示します。
// ここで詰まると、翻訳者は道具そのものを諦めます。
const notARepoText = `dwloc: ここは翻訳リポジトリではないようです。
  探した場所: %s

dwloc は、翻訳リポジトリのフォルダーに置いて実行します。
Translations フォルダーと同じ場所へ dwloc を移してから、もう一度開いてください。

  <翻訳リポジトリ>/
    Translations/    ← これと同じ場所に
    data/
    dwloc            ← これを置く

置き場所を変えずに使うときは、--root でフォルダーを指定します。
  dwloc edit --root <翻訳リポジトリのパス>

ほかの使い方は dwloc help で表示します。
`

// gameFlagUsage は --game の1行説明。共通の入口とサブコマンドで同じ文を使います。
// 何か所も言い回しが割れると、同じ指定が別のものに見えます。
const gameFlagUsage = "作業コピーを探すゲームのプラグインフォルダー（auto で自動検出）"

// gameFoundText は、どのフォルダーを探し先にするかを伝える文です。
//
// 「使います」と書かないのは、決めた時点ではまだ1バイトも読んでいないからです。
// リポジトリ側に作業コピーがあるロケールでは、そちらが先に当たるのでゲーム側は
// 読みません。そのロケールの作業コピーがゲーム側に無いときも読みません。
// 実際に読んだファイルは、ロケールごとに別の場所で出します（dwloc diff の
// 「作業コピー」の行、画面の「ファイル」の欄）。
//
// 標準エラーへ書きます。標準出力にしないのは dwloc diff --format csv のためで、
// あちらの標準出力は表計算へそのまま貼る前提なので、1行足すと列がずれます。
// 標準エラーなら、端末には出て、リダイレクトしたCSVには入りません。
const gameFoundText = `dwloc: ゲームのフォルダーを作業コピーの探し先にします: %s
dwloc:       実際に読むのは、そのロケールの作業コピーがここにあるときだけです。再生順は翻訳リポジトリのままです。
`

// gameNotFoundText は自動検出が空振りしたときの案内です。
const gameNotFoundText = `dwloc: ゲームのフォルダーが見つかりません。
dwloc:       Steam のライブラリを探しましたが、Translations/_discovered を持つプラグインがありませんでした。
dwloc:       ゲームを1度起動して、ゲーム内で Export game flow を実行してください。
dwloc:       場所が分かっているときは --game <フォルダー> で直に指定できます。
`

// gameNotPluginText は --game に指定された場所が外れていたときの案内です。
const gameNotPluginText = `dwloc: 指定された場所に Translations/_discovered がありません: %s
dwloc:       ゲームのフォルダーか、その中の BepInEx/plugins/<プラグイン> を指定してください。
dwloc:       そのフォルダーは、ゲーム内で Export game flow を1度実行するとできます。
`

// gameAmbiguousText は候補が複数あったときの案内です。
//
// どれかを選んで進めません。選んだ根拠は翻訳者から見えないので、黙って別の
// ゲームのファイルを直させることになります。
const gameAmbiguousText = `dwloc: ゲームのフォルダーが%d個見つかりました。どれを使うかを --game <フォルダー> で指定してください。
`

// resolveGame は --game の値からゲームのプラグインフォルダーを決めます。
//
// 値が空なら何もしません。自動検出が走るのは --game auto と書かれたときだけです。
// 既定で走らせないことにしたのは、2つの理由によります。
//
//  1. 指定が無いときの出力を、この変更の前後で1バイトも変えないためです。
//     作業コピーが見つかるかどうかで原文の欄と「未翻訳」の件数が変わるので、
//     自動検出が既定で効くと、同じコマンドの結果が実行する PC ごとに変わります。
//     CI と手元で違う答えが出る道具は、根拠として使えません。
//  2. 黙って別のフォルダーを読み始めないためです。ゲーム側の作業コピーは
//     コミットしないファイルで、リポジトリの中だけを見ているときと画面の
//     見た目が変わりません。読む先が増えたことに気づかないまま直していると、
//     直したものがどこへ入ったのかを追えなくなります。
//
// 決まったときは必ず1行書きます。書かないと、上の2つ目が守れません。
func resolveGame(value string, stderr io.Writer) (string, bool) {
	if value == "" {
		return "", true
	}

	plugin, err := gamedir.Resolve(value)
	if err != nil {
		writeGameError(err, stderr)
		return "", false
	}
	fmt.Fprintf(stderr, gameFoundText, filepath.ToSlash(plugin.Path))
	return plugin.Path, true
}

// writeGameError は、ゲームのフォルダーを決められなかった理由を書きます。
//
// 理由ごとに次にやることが違うので、1つの文面にまとめません。自動検出が
// 空振りしたときは書き出しがまだ、指定が外れているときは場所の取り違え、
// 候補が複数あるときは人が選ぶ番、と別のことをしてもらう必要があります。
func writeGameError(err error, stderr io.Writer) {
	var ambiguous *gamedir.AmbiguousError
	if errors.As(err, &ambiguous) {
		fmt.Fprintf(stderr, gameAmbiguousText, len(ambiguous.Candidates))
		for _, c := range ambiguous.Candidates {
			fmt.Fprintf(stderr, "dwloc:         %s\n", filepath.ToSlash(c.Path))
		}
		return
	}
	var notPlugin *gamedir.NotPluginError
	if errors.As(err, &notPlugin) {
		fmt.Fprintf(stderr, gameNotPluginText, filepath.ToSlash(notPlugin.Path))
		return
	}
	if errors.Is(err, gamedir.ErrNotFound) {
		fmt.Fprint(stderr, gameNotFoundText)
		return
	}
	// ここへ来るのは internal/gamedir が新しい誤りを増やしたとき。
	// 名前の付いていない理由でも、黙って終わらせない。
	fmt.Fprintf(stderr, "dwloc: ゲームのフォルダーを決められません: %v\n", err)
}

// looksLikeRepo は、そこが翻訳リポジトリらしいかを返します。
//
// 見るのは Translations ディレクトリの有無だけです。中身まで確かめないのは、
// ロケールの数え方を publish.DiscoverTargets と2か所に持たないためです。
// Translations はあるがロケールが1つも無い、という場合は待ち受け側が断ります。
func looksLikeRepo(root string) bool {
	info, err := os.Stat(filepath.Join(root, "Translations"))
	return err == nil && info.IsDir()
}

// enterPrompt は Enter を待つときに出す文。試験が同じ文で見張る。
const enterPrompt = "Enter キーを押すと閉じます。"

// waitForEnter は Enter が押されるまで待ちます。
//
// 読めない（標準入力が閉じている）ときはすぐ戻ります。待ち続けると、
// 入力の無い場所から呼ばれたときに止まったままになります。
func waitForEnter(stdout io.Writer) {
	fmt.Fprintln(stdout, "\nEnter キーを押すと閉じます。")
	buf := make([]byte, 1)
	for {
		n, err := stdin.Read(buf)
		if err != nil {
			return
		}
		if n > 0 && buf[0] == '\n' {
			return
		}
	}
}

// versionUsage は version の説明。
const versionUsage = `使い方: dwloc version

dwloc の版を1行で表示します。ビルド時に版を埋め込んでいなければ dev と出ます。
`

// runVersion は版を表示します。
func runVersion(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("dwloc version", stderr)
	// --root と --game は使わないが、共通オプションのつもりで打たれても止まらない
	// ように受ける。版の表示に根拠となるフォルダーは要らないので、値は読まない。
	fs.String("root", "", "（version では使いません）")
	fs.String("game", "", "（version では使いません）")
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
