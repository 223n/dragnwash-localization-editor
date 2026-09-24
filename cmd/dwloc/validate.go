package main

import (
	"fmt"
	"io"

	"github.com/223n/dragnwash-localization-editor/internal/validate"
)

// validateUsage は validate の説明。
const validateUsage = `使い方: dwloc validate [--root <ディレクトリ>] [--game <フォルダー>] [--no-game]

<ルート>/Translations 配下の公開ファイルを検証します。
tools/check-translations.py（翻訳リポジトリの dev ブランチの版）と同じ検査を行い、
同じ文面を標準出力へ書きます。
翻訳の Pull Request は main へ出すので、その CI は main の版で走ります。
main と dev の版の差は credits.txt の検査だけで、その分 dwloc validate のほうが
厳しくなります。上流の不具合を写していない入力（コメント行の引用符など）でも、
dwloc validate だけが問題を報告することがあります。上流はこれまで dev を
まとめて main へ入れてきたので、dev の版は次のリリースで main に入る見込みです。

検査する内容:
  - Translations/_discovered 配下がコミットされていないこと
  - 各ロケールの strings.local.csv がコミットされていないこと
  - 各ロケールに strings.csv があり、ヘッダーと各行の形が正しいこと
  - credits.txt があれば、最初の行が状態語
    （supervised、proofread、converted、provisional、fun）であること
  - textures/ があれば、中身が .png と credits.csv と fallback.txt だけであること。
    絵は小文字の .png、8MB 以下、4096x4096 以下の PNG で、credits.csv に
    1枚1行（file,author,note）の出典があり、fallback.txt に書いたロケールが
    実在すること

オプション:
  --root <ディレクトリ>
        翻訳リポジトリのルート（既定: カレントディレクトリ）
  --game <フォルダー>|auto
  --no-game
        受け取りますが、validate では使いません。検証するのはコミットする側の
        形なので、ゲームのフォルダーは判断に関わりません。

終了コード:
  0   問題なし
  1   問題あり
  2   検査できなかった（Translations が読めない、textures/credits.csv を
      CSV として読めない、など）
`

// runValidate は公開ファイルを検証します。
//
// 報告の文面と並びは元実装のままです（internal/validate の Report）。
// 問題は標準エラーではなく標準出力へ出します。元実装がそうしており、
// CI のログの拾い方が変わるためです（移植仕様「形式検証 R22 / R23」）。
func runValidate(args []string, defaultRoot string, stdout, stderr io.Writer) int {
	fs := newFlagSet("dwloc validate")
	// 既定値には共通オプションで受けた値を入れる。サブコマンド側でも指定されたら
	// そちらが後から上書きするので、後ろに書いた方が勝つ。
	root := fs.String("root", defaultRoot, "翻訳リポジトリのルート")
	// --game と --no-game は受けるだけで使わない。検証するのはコミットする側
	// （リポジトリ）の形で、ゲームのフォルダーはその判断に関わらない。それでも
	// 受けるのは、ほかの3つに付けて回っている指定をここだけ弾くと、打ち直しを
	// 強いることになるため。--no-game は打ち消す相手が無いので受けない、として
	// いたが、付けて回る側から見れば --game と同じで、validate だけが落ちていた。
	fs.String("game", "", "（validate では使いません）")
	fs.Bool("no-game", false, "（validate では使いません）")
	if code, ok := parseFlags(fs, args, validateUsage, stdout, stderr); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return unexpectedArg(fs, stderr)
	}

	// Tracked に nil を渡すと validate.GitTracked が使われ、git を呼ぶ。
	// git が無い環境では「あるだけ」で追跡ずみとみなす安全側の判定に落ちる
	// （internal/validate の GitTracked のコメント参照）。
	problems, err := validate.CheckTree(*root, nil)
	if err != nil {
		// ここに来るのは「検査できなかった」場合だけ。中身の問題は err ではなく
		// problems として返るので、区別して終了コード2にする。
		fmt.Fprintf(stderr, "dwloc: 検証できません: %v\n", err)
		return exitError
	}

	fmt.Fprint(stdout, validate.Report(problems))
	if len(problems) > 0 {
		return exitProblems
	}
	return exitOK
}
