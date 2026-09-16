package main

import (
	"fmt"
	"io"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/web"
)

// editUsage は edit の説明。
const editUsage = `使い方: dwloc edit [--root <ディレクトリ>] [--locale <ロケール>] [--port <番号>] [--ui-lang <言語タグ>] [--idle-timeout <時間>] [--no-browser] [--verbose]

手元だけで待ち受けを始め、1ロケールの全行を1画面に並べます。
並びも見出しも、ファイルにあるとおりです。

いまは読み取り専用です。訳の書き換えと保存はできません。

待ち受けるのは 127.0.0.1 だけです。--port を指定しても変わりません。
起動のたびに使い捨てのトークンを作り、URL に載せて標準出力へ出します。
その URL を最初に1回開いたときだけトークンを受け取り、以後は Cookie で通します。
トークンはファイルに書きません。標準出力にしか出しません。

外へは1バイトも通信しません。画面の資産は実行ファイルに埋め込んであり、
外部の CDN もフォントも参照しません。原文と訳は記録にも書きません。

オプション:
  --root <ディレクトリ>
        翻訳リポジトリのルート（既定: カレントディレクトリ）
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

終了コード:
  0   成功（Ctrl+C か --idle-timeout で終わったとき）
  2   実行時のエラー（Translations が読めない、指定したロケールが無い、
      ポートを開けない、など）
`

// editPortMax はポート番号の上限。
const editPortMax = 65535

// runEdit は待ち受けを始めます。
func runEdit(args []string, defaultRoot string, stdout, stderr io.Writer) int {
	fs := newFlagSet("dwloc edit", stderr)
	root := fs.String("root", defaultRoot, "翻訳リポジトリのルート")
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

	// ロケール名の表記ゆれは、待ち受けを始める前にここで吸収します。
	// publish と diff が使っている selectLocales に通すので、当たらない名前の
	// 文面も「対象にできるのは …」の形でそろいます。待ち受け側は、こうして
	// 確かめた名前の完全一致しか受け付けません。
	selected := ""
	if *locale != "" {
		targets, err := publish.DiscoverTargets(*root)
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
