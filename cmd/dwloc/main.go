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
// 2 が実行時のエラーです。1 を返すのは validate が問題を見つけたとき、
// diff が要確認を見つけたとき（--strict なら要作業も）、そして publish が
// 「書くと訳が失われる」と判断して1バイトも書かずに止まったときです。
// この3段に分けているのは、CIが「検証に落ちた」と「そもそも実行できなかった」を
// 区別できるようにするためです。元実装の check-translations.py は前者だけを1で返し、
// 後者はトレースバックで落ちていました（移植仕様「形式検証 / 未決の点」）。
//
// 画面に出したものは logs/dwloc_<日付>.log にも残します（internal/logfile）。
// 翻訳者にコマンドの出力を貼り直してもらうより、その日のファイルを添えてもらう
// ほうが確実だからです。原文と訳は書きません。diff の本文と publish の訳の断片は
// 画面にだけ出し、記録には件数・キー・行番号・理由・見出しと、省いた行の数だけを
// 残します（record.go）。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/gamedir"
	"github.com/223n/dragnwash-localization-editor/internal/logfile"
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
        作業コピーを探すゲームのプラグインフォルダー（publish / diff / edit）。
        auto と書くと Steam のライブラリから探します。
        指定しなくても、この3つは Steam のライブラリを探します。
        見つからなければ、ゲームを見ずにリポジトリの中だけで続けます。
  --no-game
        ゲームのフォルダーを探しも読みもしません（publish / diff / edit）。
        同じ答えが要るとき（機械との突き合わせ、コミットする中身を固定したい
        とき）に使います。--game と同時には指定できません。
        validate は --game を受け取りますが使いません。

終了コード:
  0   成功
  1   validate が問題を見つけた、diff が要確認を見つけた、または publish が
      「書くと訳が失われる」「ゲームに入っている翻訳が古い」と判断して止まった
      （diff --strict では要作業でも 1 になります）
  2   実行時のエラー（引数の誤り、ファイルが読めない、など）

記録:
  画面に出したものを logs/dwloc_<日付>.log にも残します。1日1ファイルで、
  同じ日の実行は追記します。古いファイルは消しません。不具合を知らせるときは
  その日のファイルを添えてください。原文と訳は書きません。
  diff の一覧と csv、publish が見せる訳の先頭は画面にだけ出し、記録には
  件数・キー・行番号・理由と、省いた行の数だけを残します。
  画面に出た diff の出力は原文と訳を含むので、公開の場へ貼らないでください。
  0.10.0 までの版の記録には、原文と訳が入っていることがあります。

サブコマンドごとの説明は dwloc <サブコマンド> --help で表示します。
`

func main() {
	os.Exit(mainWithRecord())
}

// mainWithRecord は記録をファイルへ残しながら run を呼び、終了コードを返します。
//
// main から切り離してあるのは os.Exit のためです。os.Exit は defer を走らせない
// ので、ファイルを閉じる場所が要ります。閉じ忘れると、改行で終わっていない
// 最後の1行が落ちます。
func mainWithRecord() int {
	w, err := logfile.Open(logfile.Dir)
	if err != nil {
		// 記録を始められなくても本体は動かします。翻訳の作業は記録が無くても
		// 進められる一方、ここで止めると「logs を作れない場所では使えない道具」
		// になります。読み取り専用の場所へ置かれることは十分あります。
		fmt.Fprintf(os.Stderr, "dwloc: 記録を残せません（%v）。このまま続けます。\n", err)
		return run(os.Args[1:], os.Stdout, os.Stderr)
	}

	// 記録にだけ、利用者のホームのパスを ~ に置き換えます。ホームのパスには
	// 利用者名が入り、記録は不具合の報告に添えて手元の外へ出るためです。
	// 画面には全文を出します。書けないファイルの案内などは、権限の話が読めないと
	// 直し方に手が届かないためです。見出しに書く引数（--root や --game）にも効きます。
	if home, err := os.UserHomeDir(); err == nil {
		w.ShortenPath(home, "~")
	}

	// 実行の区切りはファイルにだけ入れます。1日分を追記していくので、
	// どこからが今回の実行かが読めるようにします。画面には出しません。
	fmt.Fprintf(w, "=== dwloc %s %s（%s/%s）===\n",
		version, strings.Join(os.Args[1:], " "), runtime.GOOS, runtime.GOARCH)

	// 要求の記録は、--verbose を付けていなくてもファイルにだけ残します。
	// 画面をうるさくせずに、不具合の報告から辿れる手がかりを増やします。
	record = w
	hideFromRecord = w.Hide

	// 画面が先、記録が後。記録が書けなくなっても画面には出ます（teeWriter）。
	// 原文や訳を含む報告の本文は、サブコマンドが画面にだけ書きます（unrecorded）。
	code := run(os.Args[1:], &teeWriter{screen: os.Stdout, record: w}, &teeWriter{screen: os.Stderr, record: w})

	if err := w.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "dwloc: 記録を書けませんでした（%v）\n", err)
	}
	return code
}

// record は要求の記録だけを受け取る行き先です。nil なら記録しません。
//
// 画面には出さず、ログファイルにだけ残すためにあります。変数にしてあるのは
// run のシグネチャを変えないためで、stdin と startEdit が同じ理由で変数です。
// 試験では nil のままなので、記録の経路は試験の出力に混ざりません。
var record io.Writer

// hideFromRecord は記録から伏せたい文字列を渡す先です。nil なら何もしません。
//
// record と別の変数にしてあるのは、型付きの nil を避けるためです。*logfile.Writer を
// そのまま io.Writer の変数に入れると、nil でも「nil でない io.Writer」になります。
var hideFromRecord func(secret string)

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
//
// 実際に読んだファイルは、ロケールごとに別の場所で出します（dwloc diff の
// 「作業コピー」の行、dwloc publish の "<出力> <- <入力>" の行、画面の
// 「ファイル」の欄）。
const gameFoundText = `dwloc: ゲームのフォルダーを作業コピーの探し先にします: %s
dwloc:       実際に読むのは、そのロケールの作業コピーがここにあるときだけです。再生順は翻訳リポジトリのままです。
`

// gameNotFoundText は自動検出が空振りしたときの案内です。
//
// 押すボタンの名前は Export working copy です。Export game flow ではありません。
// あちらはレベルと会話グラフ（data/script_order.csv の元）を書き出す別のボタンで、
// 作業コピー Translations/_discovered/<ロケール>.working.csv は作りません
// （翻訳リポジトリの src/DragNWashLocalization/Plugin.ImGui.cs:426 と 438）。
// 間違ったほうを案内していたので、押しても作業コピーができず、この案内がもう1度出る、
// という行き止まりになっていました。押す場所（F1 → Translation）も添えます。
const gameNotFoundText = `dwloc: ゲームのフォルダーが見つかりません。
dwloc:       Steam のライブラリを探しましたが、Translations/_discovered を持つプラグインがありませんでした。
dwloc:       ゲームを1度起動して、ゲーム内で F1 → Translation → Export working copy を押してください。
dwloc:       場所が分かっているときは --game <フォルダー> で直に指定できます。
`

// gameNotFoundMacText は、macOS で自動検出が空振りしたときの案内です。
//
// 文面を分けるのは、macOS では「ゲームを1度起動して Export working copy を押して
// ください」が実行できない案内だからです。Drag'n Wash Localization は macOS で
// 動きません。BepInEx 5.4.23.5 が macOS で使う Doorstop が Unity 6.3 のゲームに
// 割り込めず、Mod が読み込まれないためです（元リポジトリの README による）。
// Mod が動かない以上、プラグインのフォルダーも作業コピーも作られません。
//
// dwloc 自身は macOS でも動きます。動かないのはゲームと繋がる部分だけなので、
// 何ができるかも並べて、道具ごと諦めさせないようにします。
const gameNotFoundMacText = `dwloc: ゲームのフォルダーが見つかりません。
dwloc:       macOS では Drag'n Wash Localization（ゲーム内のMod）がまだ動きません。
dwloc:       BepInEx 5.4.23.5 が macOS で使う Doorstop が Unity 6.3 のゲームに割り込めないためです。
dwloc:       Mod が動かないので、プラグインのフォルダーも作業コピーも作られません。
dwloc:       dwloc 自身は動きます。publish / validate / diff と、原文の欄が空のままの edit は使えます。
dwloc:       ほかのPCで書き出した作業コピーがあるときは --game <フォルダー> で指定できます。
`

// gameNotPluginText は --game に指定された場所が外れていたときの案内です。
const gameNotPluginText = `dwloc: 指定された場所に Translations/_discovered がありません: %s
dwloc:       ゲームのフォルダーか、その中の BepInEx/plugins/<プラグイン> を指定してください。
dwloc:       そのフォルダーは、ゲーム内で F1 → Translation → Export working copy を1度押すとできます。
`

// gameAmbiguousText は候補が複数あったときの案内です。
//
// どれかを選んで進めません。選んだ根拠は翻訳者から見えないので、黙って別の
// ゲームのファイルを直させることになります。
const gameAmbiguousText = `dwloc: ゲームのフォルダーが%d個見つかりました。どれを使うかを --game <フォルダー> で指定してください。
`

// resolveGame は、打たれた --game の値からゲームのプラグインフォルダーを決めます。
//
// 値が空なら何もしません。指定のあるときだけを扱う部品で、コマンドから直に
// 呼ぶものではありません。入口は [resolveGameAuto] で、publish / diff / edit の
// 3つともあちらを通ります。
//
// 決まったときは必ず1行書きます。ゲーム側の作業コピーはコミットしないファイルで、
// リポジトリの中だけを見ているときと画面の見た目が変わりません。読む先が増えた
// ことに気づかないまま直していると、直したものがどこへ入ったのかを追えなくなります。
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

// findGame は、指定が無いときにゲームのフォルダーを探す関数です。
//
// 変数にしてあるのは試験のためです。ここを実機の Steam のまま試すと、ゲームを
// 入れていない PC では当たらず、入れている PC でだけ通る試験になります。
// どちらの PC でも同じ答えが出ない試験は、根拠として使えません。
var findGame = gamedir.Find

// gameAutoNotFoundText は、--game を省いた edit の自動検出が空振りしたときの案内です。
//
// --game auto のときの案内（[gameNotFoundText]）より短くします。あちらは
// 「探せ」と打った人への返事なので、次にやること（ゲーム内の Export working copy）
// まで書きます。こちらは打っていない人へこちらの都合で出す字なので、
// 何を探して何が無かったか、そのために画面が何を出せないかだけ言って退きます。
const gameAutoNotFoundText = `dwloc: ゲームのフォルダーを探しましたが、見つかりませんでした。
dwloc:       原文の欄は空のままで、「未翻訳」は判定しません。公開ファイルの手直しはできます。
dwloc:       場所が分かっているときは --game <フォルダー> で指定してください。
`

// gameAutoAmbiguousText は、--game を省いたときの自動検出で候補が複数出たときの案内です。
//
// edit だけでなく publish と diff でも出ます。edit の画面にしか無い言葉
// （原文の欄）は使いません。
//
// --game auto のときは止めますが（[gameAmbiguousText]）、ここでは止めません。
// 打っていない指定のせいで画面が開かないのは、道具として通りません。
// どれかを選ぶこともしません。選んだ根拠は翻訳者から見えないので、黙って別の
// ゲームのファイルを直させることになります。
const gameAutoAmbiguousText = `dwloc: ゲームのフォルダーが%d個見つかりました。どれかを選ばないので、作業コピー無しで始めます。
dwloc:       ゲーム側の作業コピーを読ませるには --game <フォルダー> でどれかを指定してください。
`

// gameBothText は --game と --no-game を一緒に書かれたときの案内です。
//
// どちらかを勝たせません。「ここを読め」と「ゲームは見るな」のどちらを
// 打ち間違えたのかが、こちらからは決められません。勝ち負けの決まりを作ると、
// 打ち間違えた人は自分が何を指定したつもりだったかを思い出せないまま、
// 別の場所を直すことになります。
const gameBothText = `dwloc: --game と --no-game は一緒に指定できません。
dwloc:       --no-game はゲームのフォルダーを探しも読みもしない指定で、--game は読む場所を決める指定です。
dwloc:       どちらか片方だけを書いてください。
`

// resolveGameAuto はゲームのプラグインフォルダーを決めます。publish / diff / edit の
// 3つともここを通ります。
//
// 指定があるときは [resolveGame] と同じです。空のときだけ、ここが自動検出を
// 走らせます。
//
// はじめは edit だけが探す形にしていました。publish の入力が実行する PC で
// 変わるのを避けたかったためです。それは誤りでした。既定の edit はゲーム側の
// 作業コピーへ保存するので、既定の publish がそれを読まないと、訳が1行も
// コミットする側へ届きません。しかも publish は終了コード0で「書き出しました」と
// 言うので、届かなかったことが画面のどこにも出ません。同じ引数で diff が
// 「作業コピーがありません」、edit が「未翻訳32件」と逆のことを言う形にも
// なっていました。片方だけ探すのは、静かに壊れる道でした。
//
// 3つそろえた代わりに、--game を省いたときの出力は実行する PC とゲームの有無で
// 変わります。同じ答えが要るとき（機械との突き合わせ、コミットする中身を
// 固定したいとき）は --no-game を打ちます。develop との出力の突き合わせも
// --no-game で行います。
//
// 原文の欄と「未翻訳」の判定は、作業コピーを読めるかどうかで決まります。その
// 作業コピーは、ふつうゲームのフォルダーにしかありません（リポジトリの
// Translations/_discovered は .gitignore で外してあり、ディレクトリ自体が
// ありません）。--game を打たなかった人に、原文も未翻訳も出ない画面を見せる
// より、探しに行くほうがよいと決めました。同じコマンドの結果が、実行する PC と
// ゲームの有無で変わることは、承知のうえです。
//
// 見つからなくても止めません。作業コピーの無い画面（原文の欄は空、「未翻訳」は
// 判定しない）で始めます。ここで止めると、ゲームを入れていない PC では公開
// ファイルの手直しすらできなくなります。
//
// 候補が複数あっても選びません。並べて見せ、作業コピー無しで始めます。
//
// noGame は、その探索そのものを断る指定（--no-game）です。3つのコマンドすべてが
// 受けます。要るのは、探すのを既定にしたことで --root が読み書きの先を囲わなく
// なったからです。
//
// edit では、保存先が見つかったゲームのフォルダーの作業コピーになります。
// リポジトリを写して試す人は、自分の写しではなくゲームのフォルダーを直すことに
// なります。ゲームのフォルダーへ書けない PC（Program Files の既定の権限、権限を
// 絞った端末、読み取り専用で繋いだ外付け）では、画面は開くのに保存が落ち続けます。
//
// publish では、コミットする中身の入力がゲーム側になります。ゲーム側が古いと
// [publish.CheckBase] の守りが書き出しを止めます（設計どおりですが、公開
// ファイルを整え直したいだけの人には邪魔です）。
//
// diff では、出力が実行する PC で変わります。別の PC や機械と突き合わせる
// 使い方では困ります。
//
// どの場合も「ゲームは見ずにリポジトリの中だけを見る」を選べないと逃げ道が
// ありません。diff の --no-working と同じ向きの指定で、あちらは読むのを断り、
// こちらは探すことから断ります。
//
// explainNotFound は、探して見つからなかったときに案内を出すかです。edit だけが
// 真を渡します。見つからなければ読む先は1つも増えていないので、知らせる理由が
// ありません。publish と diff で出すと、ゲームを入れていない PC と CI で毎回
// 3行が標準エラーへ出ます。案内の文面も edit 向き（原文の欄と「未翻訳」の話）で、
// publish にその欄はありません。探し先が決まったときの1行は3つとも出します。
// あちらは読む先が実際に増えたときなので、黙るわけにいきません。
func resolveGameAuto(value string, noGame, explainNotFound bool, stderr io.Writer) (string, bool) {
	if noGame {
		if value != "" {
			fmt.Fprint(stderr, gameBothText)
			return "", false
		}
		// 1行も書きません。読む先が増えたときに書くのは、打っていない指定で
		// 増えたことに気づけないからです。これは打った人が減らした指定なので、
		// 断る字が要りません。どのファイルを読んだかは画面の「ファイル」の欄に
		// 出ます。
		return "", true
	}
	if value != "" {
		return resolveGame(value, stderr)
	}

	found := findGame()
	switch len(found) {
	case 0:
		if explainNotFound {
			fmt.Fprint(stderr, gameAutoNotFoundText)
		}
		return "", true
	case 1:
		// 決まったら必ず1行書きます。理由は [resolveGame] と同じで、打っていない
		// 指定で読む先が増えたときこそ、書かないと気づけません。
		fmt.Fprintf(stderr, gameFoundText, filepath.ToSlash(found[0].Path))
		return found[0].Path, true
	default:
		fmt.Fprintf(stderr, gameAutoAmbiguousText, len(found))
		for _, c := range found {
			fmt.Fprintf(stderr, "dwloc:         %s\n", filepath.ToSlash(c.Path))
		}
		return "", true
	}
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
		// 見つからない理由が OS ごとに違うので、次にやることも分けます。
		// 検出そのものは macOS でも走らせたままにしてあります。BepInEx が
		// 直れば、コードを足さずにそのまま見つかるようになるためです。
		if runtime.GOOS == "darwin" {
			fmt.Fprint(stderr, gameNotFoundMacText)
			return
		}
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
