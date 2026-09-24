package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// publishUsage は publish の説明。
const publishUsage = `使い方: dwloc publish [--root <ディレクトリ>] [--game <フォルダー>] [--no-game] [--locale <ロケール>] [--path <ファイル>] [--dry-run]

<ルート>/Translations 配下の各ロケールについて、公開用の strings.csv を作り直します。
tools/hash-strings.ps1 と同じ出力です。

入力は、<ロケール>.working.csv があればそれ、無ければ
Translations/<ロケール>/strings.csv 自身です。作業コピーはゲームのフォルダーを
先に見て、そこに無ければリポジトリの Translations/_discovered を見ます。
ゲームのフォルダーは、--game を省いても Steam のライブラリから探します。
探させないときは --no-game です。
出力は常に <ルート>/Translations/<ロケール>/strings.csv です。

書き出す前に、いまの公開ファイルに入っている訳が新しい出力に残るかを確かめます。
1つでも失われるなら、どのロケールも書かずに止まり、何が失われるかを表示します
（終了コード 1）。--dry-run でも同じ判定をします。この確認は外せません。

その前に、入力といまの公開ファイルの形も確かめます。publish は1物理行を
1レコードとして読むので、次の形のファイルでは訳を黙って失います。見つけたら
同じように止まり、どのファイルの何行目か、どう直せばよいかを表示します
（終了コード 1）。
  - ヘッダーに key 列も source_en 列も無い、または translation 列が無い
  - 引用符で囲んだ値が行をまたいでいる。いまの公開ファイルなら必ず止まり、
    入力なら、その行に訳が入っているか、1行ずつ読むと訳が変わるときに止まる
  - 閉じない引用符がファイルの終わりまで続く
  - 空でない行があるのに、行の区切りを読み違えて1行も読めない

ゲーム側の作業コピーを入力にしたときは、その前にもう1つ確かめます。ゲームに
入っている翻訳がコミット済みと食い違っていたら、同じように止まります。Mod は
「いま読み込んでいる訳」を作業コピーへ書き出すので、ゲーム側が古いと、その訳で
新しいコミットが巻き戻ります。訳は消えないため、上の確認では捕まりません。

止まったら、リポジトリの Translations/<ロケール>/strings.csv をゲームのフォルダーの
同じ場所へ写し、ゲームを起動し直して F1 → Translation → Export working copy を
押してください。既訳を1行でも直して publish を通すたびに、ゲーム側は1つ古くなります。

オプション:
  --root <ディレクトリ>
        翻訳リポジトリのルート（既定: カレントディレクトリ）
  --game <フォルダー>|auto
        作業コピーを探すゲームのプラグインフォルダー。auto と書くと Steam の
        ライブラリから探します。指定しなくても探します。
  --no-game
        ゲームのフォルダーを探しも読みもしません。コミットする中身を
        リポジトリの中だけで決めたいときに使います。
        ゲーム内で直した訳は入りません。打った訳が黙って落ちるので、
        「ゲームが古い」で止まったときの逃げ道には使わないでください。
  --locale <ロケール>
        対象のロケール。複数回指定するか、カンマ区切りで並べられます。
        省略すると Translations 配下のすべてが対象になります。
  --path <ファイル>
        Translations の走査をやめて、指定したファイルだけを変換します。
        入力と出力が同じファイルになります。複数回指定できます。
        --locale と同時には使えません。
  --dry-run
        何をするかを表示するだけで、ファイルは書きません。

すべての対象を先に組み立ててから書き出します。組み立てで1件でも失敗すれば
何も書きません。書き出しの途中で失敗したとき（書き込みの権限が無い、ディスクが
足りない、など）は、そこで止まります（終了コード 2）。巻き戻しはしないので、
それより前に書いたロケールは新しい内容のまま残り、標準出力に1行ずつ出ます。
書いた分も上の確認を通った出力なので、訳は失われません。書き出せなかった
ロケールと、まだ書いていないロケールは元の内容のままです。

終了コード:
  0   成功
  1   書くと訳が失われる、1行ずつ読むと訳を失う形のファイルがある、または
      ゲームに入っている翻訳が古いので止めた（どれも1バイトも書いていません）
  2   実行時のエラー（Translations が読めない、指定したロケールが無い、など）
`

// publishLossText は、書くと訳が失われると分かったときの見出しです。
//
// 「止めました」だけで終わらせず、何を確かめればよいかを添えます。この守りが
// 立つのは、たいてい入力にした作業コピーが途中までか壊れているときで、
// 見てもらう先は書き出し側ではなく入力のほうだからです。
const publishLossText = `dwloc: 訳が失われるので、1バイトも書きませんでした。
dwloc:       いまの公開ファイルに入っている訳が、新しい出力に残りません。
dwloc:       入力にした作業コピーが途中までになっていないか、壊れていないかを確かめてください。
dwloc:       ゲーム内で F1 → Translation → Export working copy を押すと、作業コピーを作り直せます。
`

// publishLossListMax は、失われる行を何件まで並べるかです。
//
// 全部は並べません。実測（この開発機）では、実機の ja.working.csv の先頭400行だけを
// 入力にすると1,367件が失われ、端末がその一覧で埋まります。埋まると、先に出した
// 「なぜ止めたか」が流れて消えます。件数は最後にまとめて出すので、ここは
// 「どんな行が失われるのか」を見るための見本として足ります。
const publishLossListMax = 20

// localeList は --locale の値を集める flag.Value です。
//
// 同じフラグを複数回置く書き方と、カンマ区切りで並べる書き方の両方を受けます。
// 前者は他のコマンドに合わせた形、後者は打つ回数を減らしたい人向けです。
// ロケール名にカンマは入らない（ディレクトリ名がそのままロケール名）ので、
// 両方を受けても解釈が割れません。
type localeList []string

// String は flag.Value の求めに応じた表示。既定値の表示に使われます。
func (l *localeList) String() string {
	if l == nil {
		return ""
	}
	return strings.Join(*l, ",")
}

// Set は1回ぶんの指定を取り込みます。
func (l *localeList) Set(value string) error {
	added := 0
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		*l = append(*l, part)
		added++
	}
	if added == 0 {
		// --locale "" や --locale , を黙って「全ロケール」に落とすと、
		// 絞ったつもりで全部書き換えることになる。ここで止める。
		return fmt.Errorf("ロケール名が空です")
	}
	return nil
}

// pathList は --path の値を集める flag.Value です。
//
// localeList と違いカンマでは分けません。パス名にカンマを入れられるためです。
// 1つ指定するたびに1件ずつ足してください。
type pathList []string

// String は flag.Value の求めに応じた表示です。
func (l *pathList) String() string {
	if l == nil {
		return ""
	}
	return strings.Join(*l, string(filepath.ListSeparator))
}

// Set は1回ぶんの指定を取り込みます。
func (l *pathList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("ファイル名が空です")
	}
	*l = append(*l, value)
	return nil
}

// runPublish は公開用CSVを生成します。
func runPublish(args []string, defaultRoot, defaultGame string, stdout, stderr io.Writer) int {
	fs := newFlagSet("dwloc publish", stderr)
	root := fs.String("root", defaultRoot, "翻訳リポジトリのルート")
	game := fs.String("game", defaultGame, gameFlagUsage)
	var locales localeList
	fs.Var(&locales, "locale", "対象のロケール")
	var paths pathList
	fs.Var(&paths, "path", "変換するファイル（入出力兼用）")
	noGame := fs.Bool("no-game", false, "ゲームのフォルダーを探しも読みもしない")
	dryRun := fs.Bool("dry-run", false, "書き込まずに内容だけ表示する")
	if code, ok := parseFlags(fs, args, publishUsage, stdout, stderr); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return unexpectedArg(fs.Arg(0), publishUsage, stderr)
	}
	if len(paths) > 0 && len(locales) > 0 {
		// 元実装の -Path は走査そのものを置き換えるので、絞り込みと重ねる意味が
		// ありません。黙ってどちらかを無視すると、絞ったつもりの指定が効かない
		// 事故になります。
		fmt.Fprintln(stderr, "dwloc: --path と --locale は同時に指定できません")
		return exitError
	}

	// ゲームのフォルダーは、何かを読み始める前に決めます。ゲーム側にしか
	// 作業コピーが無いロケールは、決めてからでないと入力が入れ替わりません。
	//
	// --path のときは決めません。あちらは走査そのものを置き換えるので、
	// 作業コピーを探す先がありません。それでも決めにいくと、「探し先にします」の
	// 1行だけが出て何にも効かない、という嘘になります。
	gamePath := ""
	if len(paths) == 0 {
		resolved, ok := resolveGameAuto(*game, *noGame, false, stderr)
		if !ok {
			return exitError
		}
		gamePath = resolved
	} else if *game != "" || *noGame {
		fmt.Fprintln(stderr,
			"dwloc: --path を指定したので --game と --no-game は使いません。走査をしないため、作業コピーを探す先がありません。")
	}

	// 再生順は全ロケールで共通なので1回だけ読む。
	data, err := publish.LoadOrder(*root)
	if err != nil {
		fmt.Fprintf(stderr, "dwloc: 再生順のデータを読めません: %v\n", err)
		return exitError
	}
	if len(data.Entries) == 0 {
		// 再生順が空でも生成はできるが、見出しが全て消えて全行が UI 見出しの下へ
		// 回るため、差分が全面的になる。黙って進めると事故になるので必ず伝える。
		fmt.Fprintf(stderr,
			"dwloc: 警告: %s に再生順の行がありません。見出しは出ず、すべての行が UI の下に並びます。\n",
			displayPath(*root, data.Source))
	}

	var targets []publish.Target
	if len(paths) > 0 {
		// 元実装の -Path 指定。走査をせず、指定したファイルを入出力兼用にする
		// （移植仕様 R8 前半）。ロケール名は決められないので空のままにする。
		for _, path := range paths {
			targets = append(targets, publish.Target{Input: path, Output: path})
		}
	} else {
		found, err := publish.DiscoverTargetsWithGame(*root, gamePath)
		if err != nil {
			fmt.Fprintf(stderr, "dwloc: Translations を読めません: %v\n", err)
			return exitError
		}
		found, err = selectLocales(found, locales)
		if err != nil {
			fmt.Fprintf(stderr, "dwloc: %v\n", err)
			return exitError
		}
		if len(found) == 0 {
			// Translations はあるが中に対象が1つも無い状態。成功として黙って終わると
			// 「書いたつもりで何も起きていない」ことに気づけないので、エラーにする。
			fmt.Fprintf(stderr, "dwloc: 対象になるロケールがありません: %s\n",
				displayPath(*root, filepath.Join(*root, publish.TranslationsDir)))
			return exitError
		}
		targets = found
	}

	// まず全件を組み立てる。書き出しはその後。組み立ての途中で失敗したとき
	// 「先頭の数ロケールだけ新しい内容、残りは古い内容」という半端な状態を
	// 作らないためです。入力ヘッダーの列名重複のように、読み始めて初めて分かる
	// 失敗があるので、事前の検査では代われません。
	//
	// 書き出しそのものの失敗（権限・ディスク不足など）までは防げません。
	// 下の書き出しの繰り返しは巻き戻さないので、そこで失敗したときは先に書いた
	// ロケールだけが新しい内容になります。使い方の説明はそのとおりに書いてあります。
	built := make([][]byte, len(targets))
	stats := make([]publish.Stats, len(targets))
	for i, t := range targets {
		out, st, err := publish.BuildTarget(data, t)
		if err != nil {
			fmt.Fprintf(stderr, "dwloc: %s を変換できません: %v\n", displayPath(*root, t.Input), err)
			return exitError
		}
		built[i], stats[i] = out, st
	}

	// 入力といまの公開ファイルが、1行ずつ読むと訳を失う形になっていないかを
	// 最初に見ます。この形のファイルは、下の2つの確認も同じ読み方で読むので、
	// 読み違えたまま「そろっている」「失われない」と判断してしまいます。
	if code := reportShape(*root, targets, stderr); code != exitOK {
		return code
	}

	// ゲーム側の作業コピーを入力にしたロケールでは、その作業コピーが建っている
	// 土台がコミット済みとそろっているかを先に見ます。ずれていると、訳は消えない
	// まま古い版へ巻き戻るので、次の reportLosses では捕まりません。
	if code := reportBaseDrift(*root, targets, stderr); code != exitOK {
		return code
	}

	// 組み立てたものを書くと訳が消えるなら、ここで止める。1件でもあれば
	// どのロケールも書きません。--dry-run でも同じ判定をします。書かないことは
	// どちらでも変わらないので、判定だけ変えると「dry-run では通ったのに
	// 本番で止まる」という食い違いが生まれます。
	if code := reportLosses(*root, targets, built, stderr); code != exitOK {
		return code
	}

	if *dryRun {
		changed := 0
		for i, t := range targets {
			note := "変更なし"
			if !sameContent(t.Output, built[i]) {
				note = fmt.Sprintf("変更あり: %d バイト", len(built[i]))
				changed++
			}
			fmt.Fprintf(stdout, "[dry-run] %s [%s]\n",
				stats[i].LogLine(displayPath(*root, t.Output), displayPath(*root, t.Input)), note)
		}
		fmt.Fprintf(stdout, "[dry-run] %d 件中 %d 件が変わります。ファイルは書いていません。\n",
			len(targets), changed)
		return exitOK
	}

	for i, t := range targets {
		out := displayPath(*root, t.Output)
		if err := publish.WriteBytes(t.Output, built[i]); err != nil {
			fmt.Fprintf(stderr, "dwloc: %s を書き出せません: %v\n", out, err)
			return exitError
		}
		// 元実装の Write-Host と同じ1行。数値の並びも同じなので、
		// 既存の手順書やログの読み方をそのまま使えます（移植仕様 R28）。
		fmt.Fprintln(stdout, stats[i].LogLine(out, displayPath(*root, t.Input)))
	}
	fmt.Fprintf(stdout, "%d 件を書き出しました。\n", len(targets))
	return exitOK
}

// publishShapeText は、1行ずつ読むと訳を失う形のファイルを見つけたときの見出しです。
//
// 「訳が失われる」（publishLossText）と文面を分けてあるのは、直す先が違うからです。
// あちらは入力が途中までか壊れていることを疑いますが、こちらはファイルの形その
// ものを直します。どの行をどう直すかは、1件ずつ下に添えます。
const publishShapeText = `dwloc: 1行ずつ読むと訳を失う形のファイルがあるので、1バイトも書きませんでした。
dwloc:       publish は1物理行を1レコードとして読みます。下の行はこの読み方では読み違え、
dwloc:       書き出すと訳が切り詰められたり、黙って落ちたりします。
`

// publishShapeFix は、形ごとの直し方です。キーは reason の識別子です。
//
// 行をまたぐ値のうち、原文（source_en）がまたいでいるものは翻訳者には直せません。
// 原文を変えるとキー（原文のハッシュ）が変わるからです。そこで、公開できる別の
// 道具と、訳を空に戻せばほかの行は publish できることを書いておきます。
var publishShapeFix = map[string]string{
	reason.PublishNoKeyColumn: "ヘッダーの行を key,section,node,order,speaker,translation などの形に直してください。" +
		"作業コピーなら、ゲーム内で F1 → Translation → Export working copy を押すと作り直せます。",
	reason.PublishNoTranslationColumn: "ヘッダーの行に translation 列を入れてください。" +
		"作業コピーなら、ゲーム内で F1 → Translation → Export working copy を押すと作り直せます。",
	reason.PublishMultilineCurrent: "その値の改行を取り除いて1行にまとめてから、もう一度実行してください。",
	reason.PublishMultilineTranslated: "その行の訳を空に戻すと、ほかの行は publish できます（その訳は公開されません）。" +
		"行をまたぐ値のある行を公開するには、tools/hash-strings.ps1 かゲーム内の Hash for commit を使ってください。",
	reason.PublishMultilineDiverges: "訳の入っていない行なら、作業コピーからその範囲の行を消しても公開される中身は変わりません。" +
		"消せないときは、tools/hash-strings.ps1 かゲーム内の Hash for commit を使ってください。",
	reason.PublishRowsUnread: "改行を LF か CRLF にして保存し直してから、もう一度実行してください。",
	reason.PublishUnclosedQuote: "引用符を閉じるか取り除いてから、もう一度実行してください。" +
		"値の中の \" は \"\" と2つ重ねて書きます。",
}

// publishShapeListMax は、形の崩れを何件まで並べるかです。publishLossListMax と
// 同じ理由で切ります。
const publishShapeListMax = 20

// reportShape は、1行ずつ読むと訳を失う形のファイルを報告します。
// 1件も無ければ exitOK を返します。
//
// 1件でもあれば exitProblems（1）で、どのロケールも書きません。reportLosses と
// 同じく、読んだうえで「書けば訳を失う」と分かったので 1 です。読めなくて
// 確かめられなかったときだけが 2 で、そのときの文面は reportLosses と同じにします。
// どちらの確認でも、読めないファイルに対してすることは同じだからです。
func reportShape(root string, targets []publish.Target, stderr io.Writer) int {
	var found []publish.Hazard
	for _, t := range targets {
		hazards, err := publish.CheckTargetShape(t)
		if err != nil {
			path := t.Output
			var shapeErr *publish.ShapeError
			if errors.As(err, &shapeErr) {
				path, err = shapeErr.Path, shapeErr.Err
			}
			fmt.Fprintf(stderr,
				"dwloc: %s を読めないので、訳が失われないことを確かめられません: %v\n",
				displayPath(root, path), err)
			return exitError
		}
		found = append(found, hazards...)
	}
	if len(found) == 0 {
		return exitOK
	}

	fmt.Fprint(stderr, publishShapeText)
	lastFile := ""
	for i, h := range found {
		if i >= publishShapeListMax {
			fmt.Fprintf(stderr, "dwloc:       ほかに %d か所あります。\n", len(found)-i)
			break
		}
		file := fileLabel(root, h)
		if file != lastFile {
			fmt.Fprintf(stderr, "dwloc:   %s\n", file)
			lastFile = file
		}
		fmt.Fprintf(stderr, "dwloc:       %s: %s\n", lineRange(h.Line, h.EndLine), h.Why)
		fmt.Fprintf(stderr, "dwloc:         直し方: %s\n", publishShapeFix[h.Why.ID])
	}
	fmt.Fprintf(stderr, "dwloc: 読み違える形が %d か所あります。直すまでは書きません。\n", len(found))
	return exitProblems
}

// fileLabel は、報告に出すファイルの見出しです。ロケールと、入力か書き出し先かを添えます。
func fileLabel(root string, h publish.Hazard) string {
	role := "入力"
	if h.Current {
		role = "いまの公開ファイル"
	}
	return fmt.Sprintf("%s%s（%s）", localePrefix(h.Locale), displayPath(root, h.Path), role)
}

// lineRange は物理行の範囲を「N行目」「N〜M行目」「ファイル全体」の形にします。
func lineRange(line, end int) string {
	switch {
	case line == 0:
		return "ファイル全体"
	case end <= line:
		return fmt.Sprintf("%d行目", line)
	default:
		return fmt.Sprintf("%d〜%d行目", line, end)
	}
}

// publishBaseDriftText は、ゲームに入っている訳がコミット済みと食い違って
// いたときの見出しです。
//
// 止める理由が「訳が消えるから」ではないので、文面を分けてあります。ここで
// 起きるのは巻き戻りで、消えるのとは直し方が違います。直す先はリポジトリでも
// 作業コピーでもなく、ゲームに入っている翻訳です。
const publishBaseDriftText = `dwloc: ゲームに入っている翻訳が古いので、1バイトも書きませんでした。
dwloc:       Modは「いま読み込んでいる訳」を作業コピーへ書き出します。
dwloc:       ゲーム側が古いと、その作業コピーも古い訳を持ち、publish で新しいコミットが巻き戻ります。
dwloc:       ゲームへ最新の翻訳を入れ直してから、もう一度実行してください。
`

// reportBaseDrift は、ゲーム側の作業コピーが建っている土台がコミット済みと
// そろっているかを確かめて報告します。
//
// そろっていれば exitOK です。1件でも食い違えば exitProblems（1）で、
// どのロケールも書きません。読めなくて確かめられなかったときだけが 2 です。
//
// コミット済みの公開ファイルがまだ無いロケール（新しい言語の最初の publish）は
// 確かめずに通します。巻き戻る先が無いからです。publish.CheckTargetLoss が
// 出力先の無いときを「失うものが無い」と扱うのと同じ考えです。ここで止めると、
// ゲームで入れた新しい言語の訳が、コミットする側へ1行も届きません。
func reportBaseDrift(root string, targets []publish.Target, stderr io.Writer) int {
	var found []publish.BaseResult
	for _, t := range targets {
		if t.GameBase == "" {
			continue
		}
		current, err := os.ReadFile(t.Output)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			fmt.Fprintf(stderr,
				"dwloc: %s を読めないので、ゲーム側とそろっているか確かめられません: %v\n",
				displayPath(root, t.Output), err)
			return exitError
		}
		res, err := publish.CheckBase(t, current)
		if err != nil {
			fmt.Fprintf(stderr,
				"dwloc: %s を読めないので、ゲーム側とそろっているか確かめられません: %v\n",
				t.GameBase, err)
			return exitError
		}
		if res.Count > 0 {
			found = append(found, res)
		}
	}
	if len(found) == 0 {
		return exitOK
	}

	fmt.Fprint(stderr, publishBaseDriftText)
	for _, res := range found {
		fmt.Fprintf(stderr, "dwloc:   %s（%d 件）\n", res.Locale, res.Count)
		for _, d := range res.Sample {
			fmt.Fprintf(stderr, "dwloc:       %s\n", d.Key)
			fmt.Fprintf(stderr, "dwloc:         コミット済み 「%s」\n", d.Repo)
			fmt.Fprintf(stderr, "dwloc:         ゲーム側     「%s」\n", d.Game)
		}
		if res.Count > len(res.Sample) {
			fmt.Fprintf(stderr, "dwloc:       ほかに %d 件あります。\n", res.Count-len(res.Sample))
		}
	}
	return exitProblems
}

// reportLosses は、書き出すと失われる訳を数えて報告します。
// 1件も無ければ exitOK を返し、呼び出し側はそのまま書き出しへ進みます。
//
// 1件でもあれば exitProblems（1）です。2 ではないのは、実行そのものは
// できているからで、validate が問題を見つけたときと同じ意味にそろえています。
// 読めなくて確かめられなかったときだけが 2 です。「失われないと分かった」と
// 「確かめられなかった」を同じ扱いにすると、確かめられないほうが素通りします。
//
// 報告に出すのは、ロケール・ファイル・行番号・キー・いまの訳の先頭だけです。
// 訳を丸ごと並べないのは internal/publish の Loss に書いた理由によります。
func reportLosses(root string, targets []publish.Target, built [][]byte, stderr io.Writer) int {
	found := make([][]publish.Loss, len(targets))
	total := 0
	for i, t := range targets {
		losses, err := publish.CheckTargetLoss(t, built[i])
		if err != nil {
			// いまの公開ファイルを読めない。失われないことを確かめられていないので、
			// 書かずに終わります。
			fmt.Fprintf(stderr,
				"dwloc: %s を読めないので、訳が失われないことを確かめられません: %v\n",
				displayPath(root, t.Output), err)
			return exitError
		}
		found[i] = losses
		total += len(losses)
	}
	if total == 0 {
		return exitOK
	}

	fmt.Fprint(stderr, publishLossText)
	shown := 0
	for i, t := range targets {
		if len(found[i]) == 0 {
			continue
		}
		fmt.Fprintf(stderr, "dwloc:   %s%s（%d 件）\n",
			localePrefix(t.Locale), displayPath(root, t.Output), len(found[i]))
		for _, l := range found[i] {
			if shown >= publishLossListMax {
				break
			}
			fmt.Fprintf(stderr, "dwloc:       %d行目 %s 「%s」 %s\n", l.Line, l.Key, l.Head, l.Why)
			shown++
		}
	}
	if total > shown {
		fmt.Fprintf(stderr, "dwloc:       ほかに %d 件あります。\n", total-shown)
	}
	fmt.Fprintf(stderr, "dwloc: 失われる訳が %d 件あります。1件でもあるあいだは書きません。\n", total)
	return exitProblems
}

// localePrefix はロケール名を報告の頭に付ける形にします。
// --path で走らせたときはロケールが決まらないので、何も付けません。
func localePrefix(locale string) string {
	if locale == "" {
		return ""
	}
	return locale + ": "
}

// selectLocales は --locale の指定で対象を絞ります。
//
// 並びは targets のまま（ディレクトリ名順）です。指定した順ではありません。
// 同じロケールを2回指定しても1回しか処理しません。
//
// 照合はまず完全一致で見て、無ければ大文字小文字を無視して見ます。
// pt-BR や zh-Hant のように大文字を含むロケール名があり、Windows では
// ディレクトリ名の大小が保たれないまま打たれることがあるためです。
// 既に存在するディレクトリの中から選ぶだけなので、緩めても新しい行き先は増えません。
func selectLocales(targets []publish.Target, want []string) ([]publish.Target, error) {
	if len(want) == 0 {
		return targets, nil
	}

	keep := make([]bool, len(targets))
	var missing []string
	for _, name := range want {
		found := false
		for i, t := range targets {
			if t.Locale == name {
				keep[i] = true
				found = true
			}
		}
		if !found {
			for i, t := range targets {
				if strings.EqualFold(t.Locale, name) {
					keep[i] = true
					found = true
				}
			}
		}
		if !found {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		available := "対象にできるロケールがありません"
		if len(targets) > 0 {
			available = "対象にできるのは " + strings.Join(localeNames(targets), ", ")
		}
		return nil, fmt.Errorf("--locale に指定したロケールがありません: %s（%s）",
			strings.Join(missing, ", "), available)
	}

	out := make([]publish.Target, 0, len(targets))
	for i, t := range targets {
		if keep[i] {
			out = append(out, t)
		}
	}
	return out, nil
}

// localeNames は targets のロケール名を並び順のまま取り出します。
func localeNames(targets []publish.Target) []string {
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		names = append(names, t.Locale)
	}
	return names
}

// sameContent は path の中身が want と同じかを返します。
// 読めなければ「同じではない」とみなします（--dry-run の表示にしか使いません）。
func sameContent(path string, want []byte) bool {
	got, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Equal(got, want)
}
