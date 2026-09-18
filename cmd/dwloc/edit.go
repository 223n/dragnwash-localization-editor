package main

import (
	"fmt"
	"io"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/web"
)

// editUsage は edit の説明。
const editUsage = `使い方: dwloc edit [--root <ディレクトリ>] [--game <フォルダー>] [--no-game] [--locale <ロケール>] [--port <番号>] [--ui-lang <言語タグ>] [--idle-timeout <時間>] [--no-browser] [--verbose]

手元だけで待ち受けを始め、1ロケールの全行を1画面に並べます。
並びも見出しも、ファイルにあるとおりです。

訳の欄を選ぶと書き換えられます。入力が止まると自動で保存します。

保存先は1つで、publish が入力に選ぶファイルと同じです。作業コピーがあればそれ、
無ければ公開ファイル自身です。作業コピーはゲーム側を先に探します。

  <ゲーム>/Translations/_discovered/<ロケール>.working.csv   作業コピー（先）
  <ルート>/Translations/_discovered/<ロケール>.working.csv   作業コピー（後。自分で置いたとき）
  <ルート>/Translations/<ロケール>/strings.csv               コミットする側（publish が作る）

作業コピーを直しているときは、その訳がコミットする側へ入るのは dwloc publish を
回したときです。ここでは公開ファイルを書き換えません。訳の入っていない行は
公開ファイルに存在しない（publish が訳の空の行を書かない）ので、ここから書き戻す
すべがありません。

作業コピーへ書くと、ゲームが約2秒でホットリロードします。

原文の欄が埋まるのは、作業コピーを読めたときだけです。作業コピーはふつう、
ゲームのフォルダーにしかありません（リポジトリの Translations/_discovered は
.gitignore で外してあります）。そのため edit は、--game を省いてもゲームの
フォルダーを探します。見つからなければ、原文の欄が空のまま始めます。
publish と diff も同じように探します（3つとも同じ探し方です。
同じリポジトリから同じ答えが出ることを崩しません）。
探させたくないときは --no-game を付けます。読み書きするのは --root の中だけに
なります。
読み書きするファイルは、画面の上に出します。

publish は回しません。保存は「触った行の最終フィールドだけを差し替える」処理で、
再生成ではありません。触っていない行は1バイトも変わりません。

保存の前にファイルの版を照合します。手前でファイルが変わっていたら書かず、
画面に読み直させて、どちらの編集を残すかを選ばせます。

待ち受けるのは 127.0.0.1 だけです。--port を指定しても変わりません。
起動のたびに使い捨てのトークンを作り、URL に載せて標準出力へ出します。
その URL を最初に1回開いたときだけトークンを受け取り、以後は Cookie で通します。
トークンはファイルに書きません。標準出力にしか出しません。

外へは1バイトも通信しません。画面の資産は実行ファイルに埋め込んであり、
外部の CDN もフォントも参照しません。原文と訳は記録にも書きません。

オプション:
  --root <ディレクトリ>
        翻訳リポジトリのルート（既定: カレントディレクトリ）
  --game <フォルダー>|auto
        作業コピーを探すゲームのプラグインフォルダー。auto と書くと Steam の
        ライブラリから探します。省略したときも探します（publish と diff も
        同じです）。見つからないときや候補が複数あるときは、作業コピー無しで
        始めます。止まりません。
        原文の欄と「未翻訳」の判定は、ここが読めるかどうかで決まります。
        ここに作業コピーがあるロケールでは、保存先がその作業コピーになります。
        書いた訳をコミットする側へ入れるには dwloc publish を回します。
  --no-game
        ゲームのフォルダーを探しも読みもしません。読み書きするのは --root の
        中だけになります。リポジトリを写して試すとき、ゲームのフォルダーへ
        書けない PC のとき、ゲーム側が古くて読ませたくないときに使います。
        --game と一緒には指定できません。
  --locale <ロケール>
        最初に出すロケール。省略すると画面で選びます。
  --port <番号>
        待ち受けるポート（既定 0 で、空いているものを自動で取ります）。
        指定しても束ねる先は 127.0.0.1 のままです。
  --ui-lang <言語タグ>
        画面とメッセージの言語（ja、en）。省略するとブラウザーの
        Accept-Language を見て、当たらなければ en になります。
  --idle-timeout <時間>
        操作が絶えてから自分で終わるまで（既定 30m、0 で終わりません）。
        30s や 1h30m のように書きます。
  --no-browser
        ブラウザーを自動で開きません。URL は標準出力に出ます。
  --verbose
        要求を1行ずつ記録します。書くのはメソッド・パス・状態コード・
        所要時間・ロケール名・件数だけです。原文と訳は書きません。

状態バッジと件数は dwloc diff と同じ判定です。起動したときに1回だけ
突き合わせ、その結果を画面に出します。判定できていないものは「0 件」ではなく
理由を出します。

保存しても突き合わせはやり直しません。訳が入った行のぶんだけ「未翻訳」から
引くだけです。数え直すには、いったん終了して開き直してください。

終了コード:
  0   成功（Ctrl+C か --idle-timeout で終わったとき）
  2   実行時のエラー（Translations が読めない、指定したロケールが無い、
      ポートを開けない、など）
`

// editPortMax はポート番号の上限。
const editPortMax = 65535

// runEdit は待ち受けを始めます。
func runEdit(args []string, defaultRoot, defaultGame string, stdout, stderr io.Writer) int {
	fs := newFlagSet("dwloc edit", stderr)
	root := fs.String("root", defaultRoot, "翻訳リポジトリのルート")
	game := fs.String("game", defaultGame, gameFlagUsage)
	// 打ち消しは3つとも受けます。publish と diff も --game を省いた
	// ときに探さないので、打ち消す相手がありません。
	noGame := fs.Bool("no-game", false, "ゲームのフォルダーを探しも読みもしない")
	// diff や publish と違い、--locale は1つだけ受けます。画面に出せるのは
	// 1ロケールで、複数を受けても最初の1つしか使えません。使わない指定を
	// 受け取れる形にすると、指定したつもりで効いていない事故になります。
	locale := fs.String("locale", "", "最初に出すロケール")
	port := fs.Int("port", 0, "待ち受けるポート（0 で自動）")
	uiLang := fs.String("ui-lang", "", "画面の言語（ja、en）")
	idleTimeout := fs.Duration("idle-timeout", web.DefaultIdleTimeout, "操作が絶えてから終わるまで")
	noBrowser := fs.Bool("no-browser", false, "ブラウザーを開かない")
	verbose := fs.Bool("verbose", false, "要求を記録する")
	if code, ok := parseFlags(fs, args, editUsage, stdout, stderr); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return unexpectedArg(fs.Arg(0), editUsage, stderr)
	}
	if *port < 0 || *port > editPortMax {
		fmt.Fprintf(stderr, "dwloc: --port は 0 から %d です: %d\n", editPortMax, *port)
		return exitError
	}
	if *idleTimeout < 0 {
		// 負の値を 0（終わらない）に丸めると、切ったつもりの指定で
		// 待ち受けが残り続けます。打ち間違いを別の意味にしません。
		fmt.Fprintf(stderr, "dwloc: --idle-timeout は 0 以上です（0 で終わりません）: %s\n", *idleTimeout)
		return exitError
	}

	// ゲームのフォルダーは、ロケールを照合する前に決めます。ゲーム側にしか
	// 作業コピーが無いロケールは、決めてからでないと対象に入りません。
	//
	// --game を省いても探しに行きます。publish と diff も同じです（理由は
	// [resolveGameAuto] の doc コメント）。見つからなくても止まらないので、
	// ここで exitError になるのは --game に指定した場所が外れていたときと、
	// --game と --no-game を一緒に打たれたときだけです。
	//
	// 第3引数が true なのは edit だけです。探して見つからなかったときの案内を
	// 出すかどうかで、見つからなければ読む先は1つも増えていないので、
	// publish と diff で言う理由がありません。
	gamePath, ok := resolveGameAuto(*game, *noGame, true, stderr)
	if !ok {
		return exitError
	}

	// ロケール名の表記ゆれは、待ち受けを始める前にここで吸収します。
	// publish と diff が使っている selectLocales に通すので、当たらない名前の
	// 文面も「対象にできるのは …」の形でそろいます。待ち受け側は、こうして
	// 確かめた名前の完全一致しか受け付けません。
	selected := ""
	if *locale != "" {
		targets, err := publish.DiscoverTargetsWithGame(*root, gamePath)
		if err != nil {
			fmt.Fprintf(stderr, "dwloc: Translations を読めません: %v\n", err)
			return exitError
		}
		found, err := selectLocales(targets, []string{*locale})
		if err != nil {
			fmt.Fprintf(stderr, "dwloc: %v\n", err)
			return exitError
		}
		if len(found) == 0 {
			fmt.Fprintf(stderr, "dwloc: 対象になるロケールがありません: %s\n", *locale)
			return exitError
		}
		selected = found[0].Locale
	}

	err := web.Run(web.Options{
		Root:        *root,
		Game:        gamePath,
		Locale:      selected,
		Port:        *port,
		UILang:      *uiLang,
		IdleTimeout: *idleTimeout,
		NoBrowser:   *noBrowser,
		Verbose:     *verbose,
		Stdout:      stdout,
		Stderr:      stderr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "dwloc: %v\n", err)
		return exitError
	}
	return exitOK
}
