package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// diffUsage は diff の説明。
const diffUsage = `使い方: dwloc diff [--root <ディレクトリ>] [--game <フォルダー>] [--no-game] [--locale <ロケール>] [--no-working] [--all] [--limit <件数>] [--format text|csv] [--raw-csv] [--output <ファイル>] [--strict]

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
        判定しなかったカテゴリ（作業コピーが無いときの未翻訳など）は
        行が無いだけになるので、そのカテゴリと理由を標準エラーへ
        理由ごとに1行で書きます。
        先頭が = + - @ タブ CR の値は、表計算が式として読まないように
        頭に ' を付けます（-では、また → '-では、また）。
  --raw-csv
        --format csv の値に ' を付けず、そのまま書きます。機械と
        突き合わせるときに使います。表計算で開くと式として読まれる
        ことがあります。--format csv と一緒に使います。
  --output <ファイル>
        結果を標準出力の代わりにファイルへ書きます。バイト列をそのまま
        書くので、リダイレクト（>）と違って文字が化けません。
        Windows PowerShell 5.1 の > は日本語を化けさせ、化けた字が改行を
        飲み込んで csv の行がつながります。ファイルに残すときはこちらを
        使ってください。csv には BOM を付け（表計算ソフトが UTF-8 と
        見分けられるように）、text には付けません。パスはカレント
        ディレクトリからの相対です。書き出し先のフォルダーは作りません。
        翻訳リポジトリの Translations と data、ゲームの Translations の
        中には書けません（diff・publish・edit が読むファイルを上書き
        しないためです）。
  --strict
        要作業（未翻訳・他のロケールにあって無い行）があるときも
        終了コードを1にします。CI 向けです。
        訳が1件もないロケール（ディレクトリだけがあり、公開ファイルも
        作業コピーも無いロケール）も要作業に数えます。--locale で報告から
        外したロケールは数えません。

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
再生順に台詞ID (line_id) が無いときは「台本に無い台詞ID行」も保留し、
--format csv では同じく標準エラーへ書きます。

公開ファイル・作業コピー・layout_risks.csv・data/script_order.csv のどれかで、
開いた引用符がファイルの終わりまで閉じないときは、止まらずにそのファイルを
読まずに続けます。publish と同じくファイル全体を解釈して読むので、そのままでは
開いた行から後ろがすべて1つの値になるからです。そのファイルに依るカテゴリは
「判定していません（作業コピーの N行目の引用符が閉じません）」と書き、
どのファイルの何行目かを標準エラーへ書いて、終了コードを 1 にします。
公開ファイルならそのロケールのすべてのカテゴリと、ほかのロケールの
「他のロケールにあって無い行」「どのロケールにも訳が無い行」、
作業コピーなら作業コピーを要るカテゴリ、layout_risks.csv なら
「はみ出しの恐れがある行」、data/script_order.csv ならすべてのカテゴリが
判定されません。
data/level_flow.csv の閉じない引用符は、diff が見出しの文言を使わないので
判定を止めず、終了コードも変えません。publish と画面の書き出しはこのファイルで
止まるので、どの行かを標準エラーへ1行だけ書きます。

data/script_order.csv が更新されたあと、dwloc publish より先に走らせてください。
publish は再生順に置けなかった行の section 列を 'UI' に書き直すため、
「台本から消えた行」の根拠が publish 後には弱くなります。
再生順の更新をコミットする前なら、1つ前の版を HEAD からそのまま読めます。

終了コード:
  0   要確認なし（未翻訳が何件残っていても 0）
  1   要確認あり（--strict のときは要作業も数えます）、または
      閉じない引用符で読めず、判定していないファイルがある
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
	fs := newFlagSet("dwloc diff")
	root := fs.String("root", defaultRoot, "翻訳リポジトリのルート")
	game := fs.String("game", defaultGame, gameFlagUsage)
	var locales localeList
	fs.Var(&locales, "locale", "報告するロケール")
	noWorking := fs.Bool("no-working", false, "作業コピーを読まない")
	noGame := fs.Bool("no-game", false, "ゲームのフォルダーを探しも読みもしない")
	all := fs.Bool("all", false, "参考のカテゴリも一覧にする")
	limit := fs.Int("limit", diffLimitDefault, "1カテゴリに並べる上限（0 で全件）")
	format := fs.String("format", diffFormatText, "出力の形式（text または csv）")
	rawCSV := fs.Bool("raw-csv", false, "csv の値に ' を付けずにそのまま書く")
	strict := fs.Bool("strict", false, "要作業があるときも終了コードを1にする")
	output := fs.String("output", "", "結果を標準出力の代わりに書くファイル")
	if code, ok := parseFlags(fs, args, diffUsage, stdout, stderr); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return unexpectedArg(fs, stderr)
	}
	if *format != diffFormatText && *format != diffFormatCSV {
		fmt.Fprintf(stderr, "dwloc: --format は %s か %s です: %s\n", diffFormatText, diffFormatCSV, *format)
		return exitError
	}
	if *rawCSV && *format != diffFormatCSV {
		// text 形式には効かない指定です。黙って受けると、付けたつもりで効いて
		// いない事故になります。
		fmt.Fprintln(stderr, "dwloc: --raw-csv は --format csv と一緒に使います")
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
	if *output != "" {
		// 読む前に確かめます。書き出し先が誤っていると分かっているのに、読んで
		// 報告を組み立ててから止めると、止まった理由の前に警告が並びます。
		if code := checkDiffOutput(*root, gamePath, *output, stderr); code != exitOK {
			return code
		}
	}

	repo, err := diff.LoadWith(*root, diff.Options{Working: !*noWorking, Game: gamePath})
	if err != nil {
		fmt.Fprintf(stderr, "dwloc: %s\n", errorText(*root, err))
		return exitError
	}
	// 報告だけを絞ります。比較の母集合は Compare が常に全ロケールから作ります。
	// 当たらない名前は Compare が黙って無視するので、下の checkDiffLocales が断ります。
	// 先に組み立てるのは、報告するロケールのうち訳が1件も無いもの（ReportedEmpty）を、
	// 下の警告にも使うためです。
	report := diff.Compare(repo, locales)

	if len(report.ReportedEmpty) > 0 {
		// 公開ファイルも作業コピーも無いロケールです。publish は対象にしないので、
		// 黙っていると「訳が1件も無い」という最大の要作業が消えます。--strict では
		// 要作業に数えます（diffExitCode）。--locale で報告から外したロケールは、
		// --strict が数えないので、ここでも名指ししません。
		//
		// 再生順の警告より先に出します。csv 形式では、再生順の警告のあとに
		// 判定を保留したカテゴリの行と、字下げした締めの1行が続きます
		// （warnHeldCategories）。あいだにこの行が挟まると、締めがこの行の
		// 続きに読めてしまいます。
		fmt.Fprintf(stderr, "dwloc: 訳が1件もないロケールがあります: %s\n",
			strings.Join(report.ReportedEmpty, ", "))
	}
	if repo.LevelFlowUnclosed > 0 {
		// 見出しの表の閉じない引用符は、diff の判定に使わないので終了コードを変えません
		// （決まったことの 18）。それでも publish と画面の書き出しはこのファイルで止まる
		// （形の確かめ）ので、diff が通ったあとで初めて気づくことにならないよう、1行だけ
		// 伝えます。再生順の警告より先に出すのは、上の行と同じ理由です。
		fmt.Fprintf(stderr,
			"dwloc: 警告: %s の %d行目で開いた引用符がファイルの終わりまで閉じません。diff は見出しの文言を使わないので判定は変えませんが、publish と画面の書き出しはこのファイルで止まります。\n",
			displayPath(*root, repo.LevelFlowPath), repo.LevelFlowUnclosed)
	}
	if repo.OrderUnclosed == 0 && (len(repo.Order.Entries) == 0 || !hasOrderKeys(repo)) {
		// 再生順が読めていないと、「再生順に無い」を根拠にするカテゴリが
		// どれも成り立ちません。internal/diff はその判定を止めますが、
		// 止めたこと自体は csv 形式の出力に出ないので、ここで必ず伝えます。
		//
		// 閉じない引用符で読めなかったときは、下の warnUnclosed がどの行かを
		// 添えて伝えます。こちらでも書くと、同じ理由を2通りの言い方で並べます。
		fmt.Fprintf(stderr,
			"dwloc: 警告: %s から再生順を読めません。台本から消えた行などは判定しません。\n",
			displayPath(*root, repo.OrderPath))
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

	// 閉じない引用符で読めなかったファイルは、形式に関わらず標準エラーへ書きます。
	// text 形式の本文もロケールごとに書きますが、--locale で絞ると、ほかのロケールの
	// 公開ファイルが原因で止めたカテゴリの、原因のファイルが本文に出ません。
	warnUnclosed(*root, report, stderr)

	if *format == diffFormatCSV {
		// csv には「判定していません」が出ません。行が無いことと、判定して
		// いないことが見分けられないので、判定を保留したカテゴリは必ず
		// 標準エラーへ書きます。
		//
		// text 形式では重ねて出しません。本文がロケールごとに同じことを既に
		// 書いているからです。作業コピーや git を使っていない利用者の毎回の
		// 実行に、読む必要のない警告を足すことになります。
		warnHeldCategories(report, stderr)
	}

	// 報告の本文は原文と訳を含むので、記録（logs/dwloc_<日付>.log）へは写しません。
	// csv は1行も写さず、text は見出しと件数と理由の行だけを写します（record.go）。
	//
	// --output のときは、本文を標準出力の代わりにファイルへ書きます（改善の決定 8）。
	// いったん全部を組み立ててから書くので、途中で失敗しても半端なファイルは
	// 残りません（publish.WriteBytes は一時ファイルから置き換えます）。
	keep := diffHeadingLine
	if *format == diffFormatCSV {
		keep = nil
	}
	var file bytes.Buffer
	var body *unrecorded
	if *output != "" {
		if *format == diffFormatCSV {
			// 表計算ソフトが UTF-8 と見分けられるよう、csv には BOM を付けます。
			// 付けないと、Excel はダブルクリックで開いたときに Shift_JIS として
			// 読むことがあります。text は付けません（ほかの道具で読むときに、
			// 1行目の頭に見えない3バイトが残らないように）。
			file.WriteString(utf8BOMText)
		}
		body = newUnrecordedTo(&file, stdout, keep)
	} else {
		body = newUnrecorded(stdout, keep)
	}
	defer body.Close()
	var werr error
	if *format == diffFormatCSV {
		write := report.WriteCSV
		if *rawCSV {
			write = report.WriteCSVRaw
		}
		werr = write(body)
	} else {
		werr = report.WriteText(body, diff.TextOptions{Root: *root, All: *all, Limit: *limit})
	}
	if werr != nil {
		fmt.Fprintf(stderr, "dwloc: 結果を書き出せません: %v\n", werr)
		return exitError
	}
	if *output != "" {
		if err := publish.WriteBytes(*output, file.Bytes()); err != nil {
			fmt.Fprintf(stderr, "dwloc: %s\n", errorf(*root, "結果を %s に書き出せません: %w", filepath.ToSlash(*output), err))
			return exitError
		}
		fmt.Fprintf(stderr, "dwloc: 結果を %s に書きました。\n", filepath.ToSlash(*output))
	}

	return diffExitCode(report, *strict)
}

// utf8BOMText は UTF-8 の BOM です。diff --output が csv の頭に付けます。
const utf8BOMText = "\xef\xbb\xbf"

// checkDiffOutput は、diff --output の書き出し先を確かめます。書いてよければ exitOK です。
//
// 翻訳リポジトリの Translations と data、ゲームの Translations の中には書きません。
// そこは diff・publish・edit が読む場所で、公開ファイルや作業コピーを報告で
// 上書きすると訳を失います。まだ無い名前でも、書くと次の実行から公開ファイルや
// 作業コピーとして読まれます（Translations/<新しい名前>/strings.csv なら新しい
// ロケールになります）。フォルダーの照合はファイルの同一性（os.SameFile）で見るので、
// 大文字小文字の違い、リンク、8.3 形式の短い名前で書いても当たります。
//
// 書き出し先のフォルダーが無いときも止めます。作ると、打ち間違えた名前の
// フォルダーへ黙って書くことになります。
func checkDiffOutput(root, game, out string, stderr io.Writer) int {
	abs, err := filepath.Abs(out)
	if err != nil {
		fmt.Fprintf(stderr, "dwloc: %s\n", errorf(root, "--output のパスを解けません: %w", err))
		return exitError
	}
	guarded := []string{
		filepath.Join(root, publish.TranslationsDir),
		filepath.Dir(publish.ScriptOrderPath(root)),
	}
	if game != "" {
		guarded = append(guarded, filepath.Join(game, publish.TranslationsDir))
	}
	if within(abs, guarded) {
		fmt.Fprintf(stderr,
			"dwloc: --output には、diff が読むフォルダーの中を指定できません（翻訳リポジトリの Translations と data、ゲームの Translations）: %s\n",
			filepath.ToSlash(out))
		return exitError
	}
	if !isDir(filepath.Dir(abs)) {
		fmt.Fprintf(stderr, "dwloc: --output の書き出し先のフォルダーがありません: %s\n", filepath.ToSlash(filepath.Dir(out)))
		return exitError
	}
	return exitOK
}

// within は、path の親をたどったどれかが、dirs のどれかと同じフォルダーかを返します。
// 無いフォルダーは比べません（path の親のうちまだ無いものと、dirs のうち無いもの）。
func within(path string, dirs []string) bool {
	var guards []os.FileInfo
	for _, d := range dirs {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			guards = append(guards, info)
		}
	}
	for cur := filepath.Dir(path); len(guards) > 0; {
		if info, err := os.Stat(cur); err == nil {
			for _, g := range guards {
				if os.SameFile(info, g) {
					return true
				}
			}
		}
		next := filepath.Dir(cur)
		if next == cur {
			break
		}
		cur = next
	}
	return false
}

// diffExitCode は報告から終了コードを決めます。
//
// 要作業（未翻訳など）が何件残っていても既定では 0 のままにします。翻訳者が
// 毎日走らせる道具で、作業が残っていることは異常ではないからです。CI を常時
// 赤くすると誰も見なくなります。--strict はその判断を CI 側へ預けるための指定で、
// 未翻訳だけではなく要作業すべてを数えます（片方だけを数えると、どちらが
// 効いているのかを使う側が覚えていなければならなくなるため）。
//
// 閉じない引用符で読めなかったファイルがあれば 1 にします（決まったことのそのほか 6）。
// そのファイルに依るカテゴリは判定していないので、要確認が1件も無くても
// 「要確認なし」とは言えません。0 で終わると、CI は直すべきファイルを見逃します。
// 2 にしないのは、実行そのものは最後まで済み、報告も出ているからです。
//
// --strict では、報告するロケールに訳が1件もないロケール（公開ファイルも作業コピーも
// 無いロケール）があるときも 1 にします（改善の調査の cli-7）。そのロケールには
// 比べる行が無いので Finding は1件も出ませんが、訳が1件も無いことは、その
// ロケールのいちばん大きい要作業です。数えないと、diff --strict を CI の関門に
// したとき、訳の無いロケールが通ります。
func diffExitCode(report *diff.Report, strict bool) int {
	if report.Status() == diff.StatusReview || len(report.Unclosed) > 0 {
		return exitProblems
	}
	if strict && (report.CountByStatus(diff.StatusTodo) > 0 || len(report.ReportedEmpty) > 0) {
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
	_, err := selectLocales(targets, nil, want)
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

// warnUnclosed は、閉じない引用符で読めなかったファイルを、どの行で開いたかと
// 直し方を添えて標準エラーへ書きます。
//
// どのカテゴリを止めたかは書きません。text 形式は本文の「判定していません（…）」が、
// csv 形式は warnHeldCategories が、カテゴリごとに書きます。ここはどのファイルの
// 何行目を直せばよいかを、パスで言うための行です。internal/diff の理由の文は
// パスを持たない（表示の基準のルートを知らない）ので、パスはここで出します。
func warnUnclosed(root string, report *diff.Report, stderr io.Writer) {
	for _, u := range report.Unclosed {
		fmt.Fprintf(stderr,
			"dwloc: 警告: %s の %d行目で開いた引用符がファイルの終わりまで閉じないので、読みませんでした。そのファイルに依るカテゴリは判定しません。\n",
			displayPath(root, u.Path), u.Line)
	}
	if len(report.Unclosed) > 0 {
		fmt.Fprintln(stderr, "dwloc:       引用符を閉じるか取り除いてください。値の中の \" は \"\" と2つ重ねて書きます。")
	}
}

// warnHeldCategories は、判定を保留したカテゴリを標準エラーへ書きます。
//
// csv 形式のためにあります。csv は Finding を1行ずつ並べるだけなので、
// 「0件だった」と「判定していない」が同じ姿（行が無い）になります。黙っていると、
// 未翻訳なら「訳し残しは無い」、publish で捨てられる行なら「捨てられる訳は無い」と
// 読まれます。引き継ぎ候補なら「移すべき訳は無い」と読まれ、消えた行の訳を
// 捨てる判断に直結します。
//
// 見るのは全カテゴリです。台詞IDを要るカテゴリ（引き継ぎ候補と台本に無い台詞ID行）
// だけを見ていたころは、作業コピーやはみ出しの記録が無いことによる保留を書いて
// いませんでした。使い方の「無ければ、その判定だけを『判定できません』と
// 伝えます」が、text 形式でしか成り立っていませんでした。
//
// 保留したかどうかと理由は、ロケールごとの要約（[diff.Summary.CanJudge] と
// [diff.Summary.JudgeBlockReason]）から取ります。text 形式の本文の
// 「判定していません（理由）」と同じ判断・同じ文面になります。理由が空のときの
// 受け皿も JudgeBlockReason が持っているので、「（）」にはなりません。
// どのカテゴリが何を要るかを CLI 側に並べて持つと、表の印を足し引きしたときに、
// 警告だけが古いまま残ります。並べる順は [diff.Categories] の表示順です。
// csv には text 形式の見出しが無いので、キーは読めていて台詞IDだけが無いことは、
// この理由の文面（「再生順に台詞ID (line_id) がありません」）だけで伝わります。
//
// 同じ理由で止めたカテゴリは1行にまとめ、同じ行になるロケールも1行にまとめます。
// 報告するロケールが全部同じなら、ロケール名は書きません。作業コピーの無い CI では
// 毎回の実行で全ロケールが同じ理由で止まるので、全ロケールの名前を並べずに、
// 理由ごとに1行で済みます。台詞IDが無いときも2つのカテゴリが同じ理由で止まるので、
// 1行です。
//
// 再生順のキーを読めていないロケールでは、「再生順を読めていません」で止めた
// カテゴリの理由の行を書きません。runDiff が先に「再生順を読めません。台本から
// 消えた行などは判定しません」と書いていて、同じ理由を2度書くことになるためです。
// ロケールごとは飛ばしません。作業コピーやはみ出しの記録による保留はその警告に
// 入らないので、飛ばすと未翻訳を判定していないことが消えます。
//
// 締めの1行（csv の category 列の値を並べる行）には、理由の行を省いたカテゴリも
// 並べます。runDiff の警告が名指しするのは「台本から消えた行など」までで、
// not_published や script_gap で絞った人には、その行が無いことが 0 件に見えます。
// 理由の行が1つも無いときも締めは書きます。字下げした締めは、すぐ上の再生順の
// 警告の続きとして読めます。
func warnHeldCategories(report *diff.Report, stderr io.Writer) {
	cats := diff.Categories()
	type held struct {
		why     string
		cats    []diff.Category
		locales []string
	}
	var groups []held
	index := make(map[string]int)
	// どこかのロケールで止めたカテゴリ。締めの1行で csv の category 列の値を並べる。
	// 理由の行を再生順の警告に任せたカテゴリも入れる。
	anyHeld := make(map[diff.Category]bool)
	for _, sum := range report.Locales {
		// このロケールで止めたカテゴリを、理由ごとに寄せる。
		var whys []string
		byWhy := make(map[string][]diff.Category)
		for _, c := range cats {
			if sum.CanJudge(c) {
				continue
			}
			anyHeld[c] = true
			block := sum.JudgeBlockReason(c)
			if !sum.OrderKeys && block.ID == reason.JudgeOrderUnreadable {
				continue
			}
			why := block.Text
			if _, ok := byWhy[why]; !ok {
				whys = append(whys, why)
			}
			byWhy[why] = append(byWhy[why], c)
		}
		for _, why := range whys {
			key := why + "\x00" + joinCategoryIDs(byWhy[why])
			i, ok := index[key]
			if !ok {
				i = len(groups)
				index[key] = i
				groups = append(groups, held{why: why, cats: byWhy[why]})
			}
			groups[i].locales = append(groups[i].locales, sum.Locale)
		}
	}
	if len(anyHeld) == 0 {
		return
	}
	for _, g := range groups {
		names := joinCategoryNames(g.cats)
		if len(g.locales) == len(report.Locales) {
			fmt.Fprintf(stderr, "dwloc: 警告: %sは判定しません（%s）。\n", names, g.why)
			continue
		}
		fmt.Fprintf(stderr, "dwloc: 警告: %s の%sは判定しません（%s）。\n",
			strings.Join(g.locales, ", "), names, g.why)
	}
	var heldCats []diff.Category
	for _, c := range cats {
		if anyHeld[c] {
			heldCats = append(heldCats, c)
		}
	}
	fmt.Fprintf(stderr, "dwloc:       %s の行が無いことは、0 件という意味ではありません。\n",
		joinCategoryIDs(heldCats))
}

// joinCategoryNames はカテゴリの日本語名を「」で囲み、「と」でつなぎます。
// text 形式の見出し（「再生順に台詞ID (line_id) がありません。…は判定しません。」）と
// 同じ書き方です。
func joinCategoryNames(cats []diff.Category) string {
	names := make([]string, len(cats))
	for i, c := range cats {
		names[i] = "「" + c.String() + "」"
	}
	return strings.Join(names, "と")
}

// joinCategoryIDs はカテゴリの csv の識別子（category 列の値）を「 と 」でつなぎます。
func joinCategoryIDs(cats []diff.Category) string {
	ids := make([]string, len(cats))
	for i, c := range cats {
		ids[i] = c.ID()
	}
	return strings.Join(ids, " と ")
}
