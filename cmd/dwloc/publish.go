package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// publishUsage は publish の説明。
const publishUsage = `使い方: dwloc publish [--root <ディレクトリ>] [--game <フォルダー>] [--no-game] [--locale <ロケール>] [--path <ファイル>] [--accept-multiline <ロケール>] [--dry-run]

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

ゲーム側の作業コピーを入力にしたときは、その前にもう1つ確かめます。ゲームに
入っている翻訳がコミット済みと食い違っていたら、同じように止まります。Mod は
「いま読み込んでいる訳」を作業コピーへ書き出すので、ゲーム側が古いと、その訳で
新しいコミットが巻き戻ります。訳は消えないため、上の確認では捕まりません。

止まったら、リポジトリの Translations/<ロケール>/strings.csv をゲームのフォルダーの
同じ場所へ写し、ゲームを起動し直して F1 → Translation → Export working copy を
押してください。既訳を1行でも直して publish を通すたびに、ゲーム側は1つ古くなります。

この2つの確認より前に、入力・いまの公開ファイル・ゲーム側の公開ファイルの形も
確かめます。publish は tools/hash-strings.ps1 と同じくファイル全体を解釈して読み、
引用符で囲んだ値は行をまたいでも1つの値になります。次の形のファイルは、そのまま
書くと英語の原文が訳として公開されたり、訳が黙って落ちたりします。見つけたら
同じように止まり、どのファイルの何行目か、どう直せばよいかを表示します
（終了コード 1）。
  - ヘッダーに key 列も source_en 列も無い、translation 列が無い、または最初の
    列名が '#' で始まる
  - 閉じない引用符がファイルの終わりまで続く
  - 引用符が別の行で閉じ、後ろの行を値に飲み込んでいると見られる（行をまたぐ
    値の続きの行が、それだけでレコードに見える）
  - 値の中に単独の CR（後ろに LF の続かない CR）がある
  - 引用符で囲まない値が、行の終わりの単独の CR で切れている
  - 空でない行があるのに、行の区切りを読み違えて1行も読めない

行をまたぐ値の続きの行がレコードに見える形は、正しい複数行の値でも当たることが
あります。値を確かめて正しければ、--accept-multiline でそのロケールを指定すると
書けます。

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
  --accept-multiline <ロケール>
        行をまたぐ値の続きの行がそれだけでレコードに見える形を、確かめたうえで
        正しい複数行の値として通します。止まったときに表示された行を見て、
        引用符の閉じ位置が正しいと確かめてから指定してください。通した行は
        標準エラーに出します。複数回指定するか、カンマ区切りで並べられます。
        --path で走らせたときは、ロケールの代わりにそのファイルを指定します。
        閉じない引用符、閉じ引用符の後ろに文字が続く形、単独の CR は通しません。
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
  1   書くと訳が失われる、読み違える形のファイルがある、または
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

// acceptList は --accept-multiline の値を集める flag.Value です。
//
// カンマでは分けません。--path で走らせたときはファイル名を受けるので、
// pathList と同じ理由でカンマを区切りにできません。1つ指定するたびに1件ずつ
// 足してください。
type acceptList []string

// String は flag.Value の求めに応じた表示です。
func (l *acceptList) String() string {
	if l == nil {
		return ""
	}
	return strings.Join(*l, " ")
}

// Set は1回ぶんの指定を取り込みます。
func (l *acceptList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		// 空の指定を黙って受けると、通したつもりの行が止まったままになり、
		// 何が効いていないのか分からなくなります。
		return fmt.Errorf("ロケール名かファイル名が空です")
	}
	*l = append(*l, value)
	return nil
}

// acceptSet は、--accept-multiline で通してよいと指定された対象です。
//
// ロケールで走らせたときはロケール名で、--path で走らせたときはファイルで引きます。
// --path ではロケール名が決まらないためです。
type acceptSet struct {
	locales map[string]bool
	paths   map[string]bool
}

// covers は h を指定で通してよいかを返します。通せる形（[publish.Hazard.Acceptable]）で、
// そのロケールかファイルが指定されているときだけです。
func (a acceptSet) covers(h publish.Hazard) bool {
	if !h.Acceptable() {
		return false
	}
	if h.Locale != "" {
		return a.locales[h.Locale]
	}
	return a.paths[filepath.Clean(h.Path)]
}

// resolveAccepts は --accept-multiline の指定を、対象の中から引き当てます。
//
// 当たらない指定は誤りにします。打ち間違えた名前を黙って無視すると、通したつもりの
// ロケールが止まったままになり、何度走らせても同じところで止まります。ロケール名の
// 照合は --locale と同じです（[matchLocales]）。--path で走らせたときは、--path に
// 渡したファイルのどれかと同じファイルを指定します。
func resolveAccepts(targets []publish.Target, values []string, byPath bool) (acceptSet, error) {
	set := acceptSet{locales: map[string]bool{}, paths: map[string]bool{}}
	if len(values) == 0 {
		return set, nil
	}
	if !byPath {
		found, err := matchLocales(targets, values, "--accept-multiline")
		if err != nil {
			return acceptSet{}, err
		}
		for _, t := range found {
			set.locales[t.Locale] = true
		}
		return set, nil
	}
	var missing []string
	for _, v := range values {
		want := filepath.Clean(v)
		if !slices.ContainsFunc(targets, func(t publish.Target) bool { return filepath.Clean(t.Input) == want }) {
			missing = append(missing, v)
			continue
		}
		set.paths[want] = true
	}
	if len(missing) > 0 {
		return acceptSet{}, fmt.Errorf("--accept-multiline に指定したファイルが --path にありません: %s",
			strings.Join(missing, ", "))
	}
	return set, nil
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
	var accepts acceptList
	fs.Var(&accepts, "accept-multiline", "確かめたうえで複数行の値として通すロケール（--path ではファイル）")
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
	accepted, err := resolveAccepts(targets, accepts, len(paths) > 0)
	if err != nil {
		fmt.Fprintf(stderr, "dwloc: %v\n", err)
		return exitError
	}

	// 入力・いまの公開ファイル・ゲーム側の公開ファイルが、読むと訳や原文を取り違える
	// 形になっていないかを最初に見ます。この形のファイルは、下の組み立てと2つの確認も
	// 同じ読み方で読むので、取り違えたまま「そろっている」「失われない」と判断して
	// しまいます。
	//
	// 組み立てより前に見るのは、組み立てが読み方の誤り（閉じない引用符など）で
	// 止まる前に、どのファイルの何行目をどう直すかを出すためです。組み立ての誤りは
	// 「変換できません」（終了コード 2）としか言えません。
	if code := reportShape(*root, targets, accepted, stderr); code != exitOK {
		return code
	}

	// 全件を組み立てる。書き出しはその後。組み立ての途中で失敗したとき
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

// publishShapeText は、読み違える形のファイルを見つけたときの見出しです。
//
// 「訳が失われる」（publishLossText）と文面を分けてあるのは、直す先が違うからです。
// あちらは入力が途中までか壊れていることを疑いますが、こちらはファイルの形その
// ものを直します。どの行をどう直すかは、1件ずつ下に添えます。
const publishShapeText = `dwloc: 読み違える形のファイルがあるので、1バイトも書きませんでした。
dwloc:       publish はファイル全体を解釈して読みます。下の行はそのまま書くと、
dwloc:       英語の原文やほかの行が訳として公開されたり、訳が黙って落ちたりします。
`

// publishShapeFix は、形ごとの直し方です。キーは reason の識別子です。
//
// 文面の {line} は、理由の置換の line（飲み込まれたと疑う物理行など）で、
// {accept} は --accept-multiline に渡す値です。どちらも [shapeFix] が埋めます。
//
// 飲み込み（引用符が別の行で閉じる形）の直し方は、引用符の閉じ位置を確かめることと、
// 値の中の " を "" と書くことです。閉じ忘れた " を足すか、値の中の " を重ねれば、
// 後ろの行は値から外れます。続きの行がレコードに見えるだけの形は、正しい複数行の
// 値でも当たる（原文の2行目がカンマを多く含むなど）ので、確かめたうえで通す指定を
// 案内します。閉じ引用符の後ろに文字が続く形は、どの書き手も作らないので、指定を
// 案内しません。
var publishShapeFix = map[string]string{
	reason.PublishNoKeyColumn: "ヘッダーの行を key,section,node,order,speaker,translation などの形に直してください。" +
		"作業コピーなら、ゲーム内で F1 → Translation → Export working copy を押すと作り直せます。",
	reason.PublishNoTranslationColumn: "ヘッダーの行に translation 列を入れてください。" +
		"作業コピーなら、ゲーム内で F1 → Translation → Export working copy を押すと作り直せます。",
	reason.PublishHashHeader: "ヘッダーの最初の列名から '#' を取り除き、key,section,node,order,speaker,translation などの形に直してください。" +
		"作業コピーなら、ゲーム内で F1 → Translation → Export working copy を押すと作り直せます。",
	reason.PublishRowsUnread: "改行を LF か CRLF にして保存し直してから、もう一度実行してください。",
	reason.PublishUnclosedQuote: "引用符を閉じるか取り除いてから、もう一度実行してください。" +
		"値の中の \" は \"\" と2つ重ねて書きます。",
	reason.PublishSwallowKeyShaped: "引用符の閉じ位置を確かめてください。値を閉じる \" が抜けていれば足し、値の中の \" は \"\" と2つ重ねて書きます。" +
		"{line}行目が値の一部として正しい（正しい複数行の値）なら、確かめたうえで --accept-multiline {accept} を付けると書けます。",
	reason.PublishSwallowSameColumns: "引用符の閉じ位置を確かめてください。値を閉じる \" が抜けていれば足し、値の中の \" は \"\" と2つ重ねて書きます。" +
		"{line}行目が値の一部として正しい（正しい複数行の値）なら、確かめたうえで --accept-multiline {accept} を付けると書けます。",
	reason.PublishSwallowTextAfterQuote: "引用符の閉じ位置を確かめてください。{line}行目の \" が、前の行で開いた値を閉じています。" +
		"値を閉じる \" が抜けていれば足し、値の中の \" は \"\" と2つ重ねて書きます。",
	reason.PublishLoneCR: "値の中の単独の CR を LF に直すか取り除いてから、もう一度実行してください。",
	reason.PublishCRCut: "{line}行目の手前（前の行の終わり）にある CR を取り除くか、値全体を引用符で囲んでから、もう一度実行してください。" +
		"値に改行を入れたいなら、値を引用符で囲み、改行を LF にします。",
}

// publishGameBaseFix は、ゲーム側の公開ファイルで見つけた形の直し方の頭に添える文です。
//
// ゲーム側の公開ファイルは、Mod が読み込んでいる訳の土台で、publish はコミット済みと
// 突き合わせるためだけに読みます（「ゲームに入っている翻訳が古い」の確かめ）。
// 手で直すより、コミット済みの公開ファイルを写し直すほうが確かです。
const publishGameBaseFix = "ゲーム側の公開ファイルは、リポジトリの Translations/<ロケール>/strings.csv を同じ場所へ写し直すと直ります。" +
	"手で直すときは次のとおりです。"

// shapeFix は、h の直し方を報告に出す形にします。
//
// --accept-multiline に渡す値は、ロケールを決めて走らせたならロケール名、--path で
// 走らせたなら --path に渡したとおりのファイルです。--path ではロケール名が
// 決まらないためです。ファイルを相対にして出さないのは、--accept-multiline は
// --path に渡した綴りと引き当てるので、書き換えると当たらなくなるからです。
func shapeFix(h publish.Hazard) string {
	accept := h.Locale
	if accept == "" {
		accept = h.Path
	}
	line := ""
	for i := 0; i+1 < len(h.Why.Args); i += 2 {
		if h.Why.Args[i] == "line" {
			line = h.Why.Args[i+1]
		}
	}
	fix := strings.NewReplacer("{line}", line, "{accept}", accept).Replace(publishShapeFix[h.Why.ID])
	if h.GameBase {
		fix = publishGameBaseFix + fix
	}
	return fix
}

// publishShapeListMax は、形の崩れを何件まで並べるかです。publishLossListMax と
// 同じ理由で切ります。
const publishShapeListMax = 20

// publishAcceptText は、--accept-multiline で通した形の見出しです。
//
// 通した行も必ず出します。止めずに書く以上、何を正しいと見なしたかが後から
// 分からないと、飲み込みを誤って通したときに気づく手がかりが残りません。
const publishAcceptText = "dwloc: --accept-multiline の指定で、次の行を正しい複数行の値として通します。\n"

// reportShape は、読み違える形のファイルを報告します。1件も無ければ（あるいは
// すべて --accept-multiline で通せば）exitOK を返します。
//
// 1件でも残れば exitProblems（1）で、どのロケールも書きません。reportLosses と
// 同じく、読んだうえで「書けば訳を取り違える」と分かったので 1 です。読めなくて
// 確かめられなかったときだけが 2 で、そのときの文面は reportLosses と同じにします。
// どちらの確認でも、読めないファイルに対してすることは同じだからです。
//
// 通した行は、止めるときも書くときも標準エラーに出します。止めるときに出すのは、
// 止まった原因を直したあとで、同じ指定で何が通るかを先に見せるためです。
func reportShape(root string, targets []publish.Target, accept acceptSet, stderr io.Writer) int {
	var found, passed []publish.Hazard
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
		for _, h := range hazards {
			if accept.covers(h) {
				passed = append(passed, h)
			} else {
				found = append(found, h)
			}
		}
	}
	if len(passed) > 0 {
		fmt.Fprint(stderr, publishAcceptText)
		writeHazards(root, passed, stderr, false)
	}
	if len(found) == 0 {
		return exitOK
	}

	fmt.Fprint(stderr, publishShapeText)
	writeHazards(root, found, stderr, true)
	fmt.Fprintf(stderr, "dwloc: 読み違える形が %d か所あります。直すまでは書きません。\n", len(found))
	return exitProblems
}

// writeHazards は形の崩れを1件ずつ出します。ファイルの見出しはファイルが変わる
// ときだけ出し、先頭の [publishShapeListMax] 件で切ります。withFix なら直し方も添えます。
func writeHazards(root string, hazards []publish.Hazard, stderr io.Writer, withFix bool) {
	lastFile := ""
	for i, h := range hazards {
		if i >= publishShapeListMax {
			fmt.Fprintf(stderr, "dwloc:       ほかに %d か所あります。\n", len(hazards)-i)
			return
		}
		file := fileLabel(root, h)
		if file != lastFile {
			fmt.Fprintf(stderr, "dwloc:   %s\n", file)
			lastFile = file
		}
		fmt.Fprintf(stderr, "dwloc:       %s: %s\n", lineRange(h.Line, h.EndLine), h.Why)
		if withFix {
			fmt.Fprintf(stderr, "dwloc:         直し方: %s\n", shapeFix(h))
		}
	}
}

// fileLabel は、報告に出すファイルの見出しです。ロケールと、入力・書き出し先・
// ゲーム側の公開ファイルのどれかを添えます。
func fileLabel(root string, h publish.Hazard) string {
	role := "入力"
	switch {
	case h.Current:
		role = "いまの公開ファイル"
	case h.GameBase:
		role = "ゲーム側の公開ファイル"
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
	return matchLocales(targets, want, "--locale")
}

// matchLocales は want に並べたロケール名に当たる対象を返します。照合の仕方は
// [selectLocales] に書いたとおりで、当たらない名前があれば、flag（指定の名前）を
// 添えた誤りを返します。--locale と --accept-multiline が同じ照合を使うためです。
func matchLocales(targets []publish.Target, want []string, flag string) ([]publish.Target, error) {
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
		return nil, fmt.Errorf("%s に指定したロケールがありません: %s（%s）",
			flag, strings.Join(missing, ", "), available)
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
