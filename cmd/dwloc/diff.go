package main

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// diffUsage は diff の説明。
const diffUsage = `使い方: dwloc diff [--root <ディレクトリ>] [--game <フォルダー>] [--no-game] [--locale <ロケール>] [--no-working] [--all] [--limit <件数>] [--format text|csv] [--strict]

<ルート>/Translations の公開ファイルと data/script_order.csv を突き合わせ、
翻訳者が次にやることと、確かめたほうがよい行を並べます。

作業コピー（Translations/_discovered/<ロケール>.working.csv）があれば、
未翻訳の行も出します。無ければ、その判定だけを「判定できません」と伝えます。
同じフォルダーの layout_risks.csv（ゲーム内の Check translation layout が
書きます）があれば、はみ出しの恐れがある行も出します。
作業コピーはふつう、ゲームのフォルダーにしかありません。
そのため --game を省いても、ゲームのフォルダーを探します。
探させないときは --no-game です。

オプション:
  --root <ディレクトリ>
        翻訳リポジトリのルート（既定: カレントディレクトリ）
  --game <フォルダー>|auto
        ゲームに入れたプラグインのフォルダー。auto と書くと Steam の
        ライブラリから探します。指定しなくても探します。
        探し先は標準エラーへ1行出します（--format csv の
        標準出力を汚さないためです）。実際にそこから読んだかどうかは、
        ロケールごとの「作業コピー」の行に出るパスで分かります。
        ここから読むのは作業コピーだけで、再生順は常にリポジトリ側から
        読みます。引き継ぎ候補は git の履歴にある1つ前の再生順が根拠なので、
        履歴の無いゲーム側へ替えると、その判定そのものが消えます。
  --locale <ロケール>
        報告するロケール。複数回指定するか、カンマ区切りで並べられます。
        省略すると全ロケールを報告します。
        絞っても公開ファイルは全ロケール読みます。言語間の比較の
        母集合を欠かさないためで、絞れるのは報告だけです。
  --no-game
        ゲームのフォルダーを探しも読みもしません。別のPCや機械と出力を
        突き合わせるときに使います。--game と同時には指定できません。
  --no-working
        作業コピーがあっても読みません。公開ファイルだけで何が言えるかを
        再現するための指定です。作業コピーが古いときにも使えます。
        同じフォルダーの layout_risks.csv も読みません。どちらもゲームが
        書くファイルで、手元で再現できる入力ではありません。
  --all
        参考のカテゴリも1件ずつ並べます。既定は件数と理由だけです。
  --limit <件数>
        1つのカテゴリに並べる上限（既定 20、0 で全件）。
        件数そのものは上限に関係なく必ず出ます。
  --format text|csv
        出力の形式（既定 text）。csv は
        locale,category,status,key,section,node,order,speaker,source_en,translation,note
        の11列で、表計算にそのまま貼れます。
  --strict
        要作業（未翻訳・他のロケールにあって無い行）があるときも
        終了コードを1にします。CI 向けです。

ゲームが更新されて英文が変わると、その行のキーも変わります。旧キーの訳を
どの新キーへ移せばよいかの見当を「引き継ぎ候補」として出します。訳は
書き換えません。中身を確かめてから、作業コピーで移してください。

見当は data/script_order.csv の1つ前の版を git から読み、新旧を台詞ID
(line_id) で突き合わせて求めます。旧キーがいまの再生順のどこにも無ければ
「移動」（旧行はもう要りません）、別の行で生きていれば「複製」（元の行の訳は
残してください）です。一覧では行の先頭にどちらかが付き、「複製」を先に並べます
（--limit で切り詰めても残るようにするためです）。
「移動」の行は「台本から消えた行」にも出ます。

git が無い、git リポジトリでない、再生順の履歴が1版しかない、といったときは
「引き継ぎ候補」を 0 件とは書かず、理由を添えて保留します。--format csv では
書く場所が無いので、その保留を標準エラーへ書きます。carryover の行が無いこと
だけを見て「引き継ぎ先は無い」と読まないでください。

data/script_order.csv が更新されたあと、dwloc publish より先に走らせてください。
publish は再生順に置けなかった行の section 列を 'UI' に書き直すため、
「台本から消えた行」の根拠が publish 後には弱くなります。
再生順の更新をコミットする前なら、1つ前の版を HEAD からそのまま読めます。

終了コード:
  0   要確認なし（未翻訳が何件残っていても 0）
  1   要確認あり（--strict のときは要作業も数えます）
  2   実行時のエラー（Translations が読めない、指定したロケールが無い、
      CSV のヘッダーに列名の重複がある、など）
`

// diffLimitDefault は --limit の既定値。
//
// 20 にしたのは、端末を1画面さかのぼれば全部見える程度に収めるため。
// 件数そのものは上限に関わらず必ず出るので、切り詰めても
// 「何件あるか」を見落とすことはありません。
const diffLimitDefault = 20

// 出力の形式。--format の値と一対一に対応します。
const (
	diffFormatText = "text"
	diffFormatCSV  = "csv"
)

// runDiff は公開ファイルと再生順を突き合わせて報告します。
func runDiff(args []string, defaultRoot, defaultGame string, stdout, stderr io.Writer) int {
	fs := newFlagSet("dwloc diff", stderr)
	root := fs.String("root", defaultRoot, "翻訳リポジトリのルート")
	game := fs.String("game", defaultGame, gameFlagUsage)
	var locales localeList
	fs.Var(&locales, "locale", "報告するロケール")
	noWorking := fs.Bool("no-working", false, "作業コピーを読まない")
	noGame := fs.Bool("no-game", false, "ゲームのフォルダーを探しも読みもしない")
	all := fs.Bool("all", false, "参考のカテゴリも一覧にする")
	limit := fs.Int("limit", diffLimitDefault, "1カテゴリに並べる上限（0 で全件）")
	format := fs.String("format", diffFormatText, "出力の形式（text または csv）")
	strict := fs.Bool("strict", false, "要作業があるときも終了コードを1にする")
	if code, ok := parseFlags(fs, args, diffUsage, stdout, stderr); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return unexpectedArg(fs.Arg(0), diffUsage, stderr)
	}
	if *format != diffFormatText && *format != diffFormatCSV {
		fmt.Fprintf(stderr, "dwloc: --format は %s か %s です: %s\n", diffFormatText, diffFormatCSV, *format)
		return exitError
	}
	if *limit < 0 {
		// 負の値を 0（全件）に丸めると、絞ったつもりで全件出ることになります。
		// 打ち間違いを黙って別の意味にしないため、ここで止めます。
		fmt.Fprintf(stderr, "dwloc: --limit は 0 以上です（0 で全件）: %d\n", *limit)
		return exitError
	}

	gamePath, ok := resolveGameAuto(*game, *noGame, false, stderr)
	if !ok {
		return exitError
	}

	repo, err := diff.LoadWith(*root, diff.Options{Working: !*noWorking, Game: gamePath})
	if err != nil {
		fmt.Fprintf(stderr, "dwloc: %s\n", diffErrorText(*root, err))
		return exitError
	}
	if len(repo.Order.Entries) == 0 || !hasOrderKeys(repo) {
		// 再生順が読めていないと、「再生順に無い」を根拠にするカテゴリが
		// どれも成り立ちません。internal/diff はその判定を止めますが、
		// 止めたこと自体は csv 形式の出力に出ないので、ここで必ず伝えます。
		fmt.Fprintf(stderr,
			"dwloc: 警告: %s から再生順を読めません。台本から消えた行などは判定しません。\n",
			displayPath(*root, repo.OrderPath))
	}
	if len(repo.EmptyLocales) > 0 {
		// 公開ファイルも作業コピーも無いロケールです。publish は対象にしないので、
		// 黙っていると「訳が1件も無い」という最大の要作業が消えます。
		fmt.Fprintf(stderr, "dwloc: 訳が1件もないロケールがあります: %s\n",
			strings.Join(repo.EmptyLocales, ", "))
	}
	if len(repo.Locales) == 0 {
		// 比較する相手が1つも無い状態です。報告を「0 件」と書いて成功で終わると、
		// 何も比べていないことに気づけないので、publish と同じくエラーにします。
		fmt.Fprintf(stderr, "dwloc: 対象になるロケールがありません: %s\n",
			displayPath(*root, filepath.Join(*root, publish.TranslationsDir)))
		return exitError
	}
	if err := checkDiffLocales(repo.Locales, repo.EmptyLocales, locales); err != nil {
		fmt.Fprintf(stderr, "dwloc: %v\n", err)
		return exitError
	}

	// 報告だけを絞ります。比較の母集合は Compare が常に全ロケールから作ります。
	report := diff.Compare(repo, locales)

	if *format == diffFormatCSV {
		// csv には「判定していません」が出ません。行が無いことと、判定して
		// いないことが見分けられないので、保留は必ず標準エラーへ書きます。
		//
		// text 形式では重ねて出しません。本文がロケールごとに同じことを既に
		// 書いているからです。git を使っていない利用者の毎回の実行に、
		// 読む必要のない警告を足すことになります。
		warnHeldCarryover(report, stderr)
	}

	if *format == diffFormatCSV {
		if err := report.WriteCSV(stdout); err != nil {
			fmt.Fprintf(stderr, "dwloc: 結果を書き出せません: %v\n", err)
			return exitError
		}
	} else {
		opt := diff.TextOptions{Root: *root, All: *all, Limit: *limit}
		if err := report.WriteText(stdout, opt); err != nil {
			fmt.Fprintf(stderr, "dwloc: 結果を書き出せません: %v\n", err)
			return exitError
		}
	}

	return diffExitCode(report, *strict)
}

// diffExitCode は報告から終了コードを決めます。
//
// 要作業（未翻訳など）が何件残っていても既定では 0 のままにします。翻訳者が
// 毎日走らせる道具で、作業が残っていることは異常ではないからです。CI を常時
// 赤くすると誰も見なくなります。--strict はその判断を CI 側へ預けるための指定で、
// 未翻訳だけではなく要作業すべてを数えます（片方だけを数えると、どちらが
// 効いているのかを使う側が覚えていなければならなくなるため）。
func diffExitCode(report *diff.Report, strict bool) int {
	if report.Status() == diff.StatusReview {
		return exitProblems
	}
	if strict && report.CountByStatus(diff.StatusTodo) > 0 {
		return exitProblems
	}
	return exitOK
}

// checkDiffLocales は --locale に無いロケールが混じっていないかを確かめます。
//
// diff.Compare は当たらない名前を黙って無視します（エラーを返せる署名では
// ないため）。打ち間違いを「そのロケールは問題なし」に化けさせないよう、
// 呼ぶ前にここで弾きます。
//
// 照合を publish.Target に詰め替えて selectLocales に任せているのは、
// 完全一致を先に見て外れたら大文字小文字を無視するという規則と、
// 「対象にできるのは …」というエラーの文面を publish と1か所で保つためです。
func checkDiffLocales(found []diff.Locale, empty []string, want []string) error {
	if len(want) == 0 {
		return nil
	}
	targets := make([]publish.Target, 0, len(found)+len(empty))
	for _, loc := range found {
		targets = append(targets, publish.Target{Locale: loc.Name})
	}
	// 公開ファイルも作業コピーも無いロケールも、名前としては当たりにします。
	// ディレクトリは実在するので、「そのロケールはありません」と言うと嘘になります。
	for _, name := range empty {
		targets = append(targets, publish.Target{Locale: name})
	}
	_, err := selectLocales(targets, want)
	return err
}

// hasOrderKeys は再生順のキーを1種でも読めたかを返します。
//
// 行数ではなくキーの種類数で見ます。行はあるのに key 列を引けないファイル
// （列名が違う、など）では、行数だけ見ていると読めたように見えてしまいます。
func hasOrderKeys(repo *diff.Repo) bool {
	for _, e := range repo.Order.Entries {
		if e.Key != "" {
			return true
		}
	}
	return false
}

// diffErrorText は読み込みの失敗を、ルートからの相対パスで書き直します。
//
// internal/diff は表示の基準になるルートを知らないので、パスを持ったまま
// エラーを返します（diff.FileError）。手元の絶対パスには利用者名が入ることが
// あり、CIのログや不具合報告へ貼られるとそのまま漏れます。
func diffErrorText(root string, err error) string {
	var fileErr *diff.FileError
	if errors.As(err, &fileErr) {
		return fmt.Sprintf("%s: %v", displayPath(root, fileErr.Path), fileErr.Err)
	}
	return err.Error()
}

// warnHeldCarryover は、引き継ぎ候補を保留したことを標準エラーへ書きます。
//
// csv 形式のためにあります。csv は Finding を1行ずつ並べるだけなので、
// 「候補が0件だった」と「候補を判定していない」が同じ姿（行が無い）になります。
// 黙っていると「移すべき訳は無い」と読まれ、消えた行の訳を捨てる判断に直結します。
//
// 保留したかどうかと理由は、ロケールごとの要約（[diff.Summary.CanJudge] と
// [diff.Summary.JudgeBlockReason]）から取ります。text 形式の本文の
// 「判定していません（理由）」と同じ判断・同じ文面になります。1つ前の再生順が
// 無いことだけを見ていたころは、いまの再生順に台詞IDが無くて保留したときに
// 何も書かず、一方で1つ前の再生順が無いときは別の場所と2度書いていました。
// 理由が空のときの受け皿も JudgeBlockReason が持っているので、「（）」には
// なりません。
//
// 同じ理由のロケールは1行にまとめます。報告するロケールが全部同じ理由なら、
// ロケール名は書きません。git を使っていない利用者の毎回の実行に、全ロケールの
// 名前を並べることになるためです。
//
// 再生順のキーを読めていないロケールは飛ばします。runDiff が先に「再生順を
// 読めません。台本から消えた行などは判定しません」と書いていて、引き継ぎ候補も
// そこに入るからです。同じ理由を2度書くことになります。
func warnHeldCarryover(report *diff.Report, stderr io.Writer) {
	type held struct {
		why     string
		locales []string
	}
	var groups []held
	index := make(map[string]int)
	for _, sum := range report.Locales {
		if sum.CanJudge(diff.CatCarryover) || !sum.OrderKeys {
			continue
		}
		why := sum.JudgeBlockReason(diff.CatCarryover).Text
		i, ok := index[why]
		if !ok {
			i = len(groups)
			index[why] = i
			groups = append(groups, held{why: why})
		}
		groups[i].locales = append(groups[i].locales, sum.Locale)
	}
	if len(groups) == 0 {
		return
	}
	for _, g := range groups {
		if len(g.locales) == len(report.Locales) {
			fmt.Fprintf(stderr, "dwloc: 警告: 引き継ぎ候補は判定しません（%s）。\n", g.why)
			continue
		}
		fmt.Fprintf(stderr, "dwloc: 警告: %s の引き継ぎ候補は判定しません（%s）。\n",
			strings.Join(g.locales, ", "), g.why)
	}
	fmt.Fprintf(stderr, "dwloc:       carryover の行が無いことは、引き継ぎ先が無いという意味ではありません。\n")
}
