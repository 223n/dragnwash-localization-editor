package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// publishUsage は publish の説明。
const publishUsage = `使い方: dwloc publish [--root <ディレクトリ>] [--game <フォルダー>] [--no-game] [--locale <ロケール>] [--path <ファイル>] [--accept-multiline <ロケール>:<key>] [--dry-run]

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

再生順のデータ（data/script_order.csv と data/level_flow.csv）も、読む前に確かめます。
閉じない引用符があるときと、publish が引用せずにそのまま書く値（script_order.csv の
section・phase・node・condition・order 列と、level_flow.csv の dragon・weather・
set_flags・end_flags 列）に改行があるときは、同じように止まります（終了コード 1）。
改行があると、公開ファイルの見出しの行が2行に割れ、次に読むときデータの行として
読まれるからです。

行をまたぐ値の続きの行がレコードに見える形は、正しい複数行の値でも当たることが
あります。値を確かめて正しければ、止まったときの直し方に出る指定
（--accept-multiline <ロケール>:<key>）を付けると、そのレコードだけを通して書けます。

書く直前に、入力と書き出し先に、画面（dwloc edit）の保存と同じ書き込みの錠を
掛けてから、組み立てと確かめに使った入力・書き出し先・ゲーム側の公開ファイルを
読み直します。組み立てのあいだに画面の保存やゲームの書き出しが入って、1バイトでも
変わっていたら、1バイトも書かずに止まります（終了コード 1）。そのまま書くと、
そのとき入った訳が出力に入らないか、書き出し先に入った訳が消えるからです。
もう一度実行すれば、変わったあとの中身で確かめ直します。

表計算ソフトなどで作業コピーを保存し直すと、原文の中の改行が CRLF に変わり、
キーと合わなくなった行は公開されません（tools/hash-strings.ps1 と同じです）。
原文の CRLF を LF にするとキーが一致する行があれば、止めずに行とキーを表示します。

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
        公開ファイルも作業コピーも無いロケールは、書き出す元が無いので
        指定できません（終了コード 2）。
  --path <ファイル>
        Translations の走査をやめて、指定したファイルだけを変換します。
        入力と出力が同じファイルになります。複数回指定できます。
        --locale と同時には使えません。
  --accept-multiline <ロケール>:<key>
        行をまたぐ値の続きの行がそれだけでレコードに見える形を、確かめたうえで
        正しい複数行の値として通します。指定はレコード単位です。<key> はその
        レコードの key（key 列の値。key 列が無いか空なら、原文から作るキー）で、
        止まったときの直し方に、そのまま写せる形で出ます。表示された行を見て、
        引用符の閉じ位置が正しいと確かめてから指定してください。通した行は
        標準エラーに出します。通すレコードごとに1つずつ、複数回指定します
        （ファイル名にカンマを入れられるので、カンマでは分けません）。
        --path で走らせたときは、ロケールの代わりに --path に渡したファイルを
        書きます（<ファイル>:<key>）。パスに空白や括弧などがあれば、直し方には
        二重引用符で囲んで出します。$ のように、二重引用符の中でもシェルに
        よっては読み替える文字があるときは、使うシェルに合わせて書き直すよう
        添えます。ロケールやファイルだけの指定はできません。
        指定は、そのロケールの入力・いまの公開ファイル・ゲーム側の公開ファイルの
        どれでも、その key のレコードに効きます。同じロケールのほかのレコードは
        通しません。閉じない引用符、閉じ引用符の後ろに文字が続く形、単独の CR、
        再生順のデータの形は通しません。閉じ引用符の後ろに文字が続く行の
        あるレコードは、続きの行がレコードに見えても通しません。key が無いか、
        英数字と . _ : - のほかの文字を含むレコード（どれも publish は書きません）と、
        同じファイルに同じ key のレコードが2つ以上あるレコードも通せません。
        一度公開した行はいまの公開ファイルで毎回当たるので、そのレコードを
        書くたびに同じ指定が要ります。同じレコードの値に新しく入った閉じ忘れは
        同じ指定で通るので、通した行の一覧を毎回確かめてください。
        通せる行に当たらない指定は誤りにします。
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
  1   書くと訳が失われる、読み違える形のファイルがある、ゲームに入っている
      翻訳が古い、または組み立てたあとに入力か書き出し先が変わったので止めた
      （どれも1バイトも書いていません）
  2   実行時のエラー（Translations が読めない、指定したロケールが無い、
      --accept-multiline の指定が通せる行に当たらない、書き込みの錠を取れない、
      など）
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
// 足してください。値をロケールかファイルと key に分けるのは [resolveAccepts] です。
// 分けるには対象（ロケール名と --path のファイル）が要るので、ここでは分けません。
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
		return fmt.Errorf("指定が空です（<ロケール>:<key> の形で指定します）")
	}
	*l = append(*l, value)
	return nil
}

// acceptSpec は、--accept-multiline の指定1つを、対象（ロケールかファイル）と
// レコードの key に分けたものです。
type acceptSpec struct {
	// raw は指定の綴りのままです。報告に使います。
	raw string
	// locales は、ロケールで走らせたときに当たったロケール名（ディレクトリ名の綴り）です。
	// 照合は --locale と同じで、大文字小文字だけが違うディレクトリがあれば2つ以上に
	// なります。--path で走らせたときは空です。
	locales []string
	// path は、--path で走らせたときの対象のファイル（filepath.Clean 済み）です。
	path string
	// key はレコードの key です。比べ方は publish.SameKey です。
	key string
}

// names は、s が h のレコード（ロケールかファイルと key）を名指すかを返します。
// 形が通せるかは見ません。
func (s acceptSpec) names(h publish.Hazard) bool {
	if h.Key == "" {
		return false
	}
	if h.Locale != "" {
		if !slices.Contains(s.locales, h.Locale) {
			return false
		}
	} else if s.path == "" || s.path != filepath.Clean(h.Path) {
		return false
	}
	return publish.SameKey(s.key, h.Key)
}

// acceptSet は、--accept-multiline で通してよいと指定されたレコードです。
//
// 通す単位はレコードです（決まったことの 16）。ロケールで走らせたときはロケール名と
// key で、--path で走らせたときはファイルと key で引きます。--path ではロケール名が
// 決まらないためです。
//
// ロケール単位で通していたころは、正しい複数行の値を一度公開すると、いまの公開
// ファイルの形の確かめで毎回当たり、そのロケールを書くたびに付ける指定が、同じ
// ロケールに新しく入った飲み込み（続きの行がレコードに見える形）まで通していました。
// レコード単位なら、新しく入った飲み込みは別のレコードなので止まります。
type acceptSet struct {
	specs []acceptSpec
}

// covers は h を通す指定の番号をすべて返します。通さないなら空です。同じレコードを
// 2回指定したときは、どちらの番号も返します（どちらも当たった指定として数えます）。
//
// 通すのは、指定で通せる形（[publish.Hazard.Acceptable]。形と、key で1つのレコードに
// 名指せること）で、指定がそのレコードを名指すときだけです。
func (a acceptSet) covers(h publish.Hazard) []int {
	if !h.Acceptable() {
		return nil
	}
	var out []int
	for i, s := range a.specs {
		if s.names(h) {
			out = append(out, i)
		}
	}
	return out
}

// acceptFormText は、--accept-multiline の指定の形を案内する文です。ロケール単位の
// 指定（決まったことの 4 の例）を無くしたので、その形で渡されたときにも出します。
const acceptFormText = "通すレコードごとに <ロケール>:<key>（--path では <ファイル>:<key>）の形で1つずつ指定します。" +
	"止まったときの直し方に、写せる形で出ます。ロケールやファイルだけの指定はできません"

// resolveAccepts は --accept-multiline の指定を、対象（ロケールかファイル）と key に
// 分けます。
//
// 対象の無い指定は誤りにします。打ち間違えた名前を黙って無視すると、通したつもりの
// レコードが止まったままになり、何度走らせても同じところで止まります。レコードに
// 当たらない指定は、形を確かめてから [reportShape] が誤りにします。
func resolveAccepts(targets []publish.Target, values []string, byPath bool) (acceptSet, error) {
	parse := parseLocaleAccept
	if byPath {
		parse = parsePathAccept
	}
	var set acceptSet
	for _, v := range values {
		spec, err := parse(targets, v)
		if err != nil {
			return acceptSet{}, err
		}
		set.specs = append(set.specs, spec)
	}
	return set, nil
}

// parseLocaleAccept は、ロケールで走らせたときの指定 <ロケール>:<key> を分けます。
//
// 最初の ':' で分けます。ロケール名（ディレクトリ名）に ':' は入らず、key には
// 入ることがある（台詞ID の line:）からです。ロケール名の照合は --locale と同じです
// （[matchLocales]）。
func parseLocaleAccept(targets []publish.Target, v string) (acceptSpec, error) {
	locale, k, ok := strings.Cut(v, ":")
	locale, k = strings.TrimSpace(locale), strings.TrimSpace(k)
	if !ok || locale == "" || k == "" {
		return acceptSpec{}, fmt.Errorf("--accept-multiline はレコード単位で指定してください: %s（%s）", v, acceptFormText)
	}
	found, err := matchLocales(targets, nil, []string{locale}, "--accept-multiline")
	if err != nil {
		return acceptSpec{}, err
	}
	return acceptSpec{raw: v, locales: localeNames(found), key: k}, nil
}

// parsePathAccept は、--path で走らせたときの指定 <ファイル>:<key> を分けます。
//
// ファイル名にも key にも ':' が入りうる（Windows のドライブ名、台詞ID の line:）ので、
// ':' のどの位置で分けると前半が --path のファイル（filepath.Clean で比べる）に
// なるかで決めます。2か所以上で分けられるときは、どこまでがファイル名か決められない
// ので誤りにします。
func parsePathAccept(targets []publish.Target, v string) (acceptSpec, error) {
	isTarget := func(p string) bool {
		want := filepath.Clean(p)
		return slices.ContainsFunc(targets, func(t publish.Target) bool { return filepath.Clean(t.Input) == want })
	}
	if isTarget(v) {
		return acceptSpec{}, fmt.Errorf("--accept-multiline はレコード単位で指定してください: %s（%s）", v, acceptFormText)
	}
	var found []acceptSpec
	for i := 0; i < len(v); i++ {
		if v[i] == ':' && i > 0 && isTarget(v[:i]) {
			found = append(found, acceptSpec{raw: v, path: filepath.Clean(v[:i]), key: strings.TrimSpace(v[i+1:])})
		}
	}
	switch {
	case len(found) == 0:
		return acceptSpec{}, fmt.Errorf("--accept-multiline に指定したファイルが --path にありません: %s（%s）", v, acceptFormText)
	case len(found) > 1:
		return acceptSpec{}, fmt.Errorf("--accept-multiline の指定で、どこまでがファイル名か決められません: %s", v)
	case found[0].key == "":
		return acceptSpec{}, fmt.Errorf("--accept-multiline はレコード単位で指定してください: %s（%s）", v, acceptFormText)
	}
	return found[0], nil
}

// unmatchedAccepts は、どの行も通さなかった指定を、当たらなかった理由を添えて返します。
// used は指定ごとに通した行があったか、all は形の確かめで見つけたすべての形です。
func (a acceptSet) unmatchedAccepts(used []bool, all []publish.Hazard) []string {
	var out []string
	for i, s := range a.specs {
		if used[i] {
			continue
		}
		why := "そのレコードに、指定で通せる行がありません"
		if slices.ContainsFunc(all, s.names) {
			why = "そのレコードの行は、指定では通せない形です。直し方は上の一覧にあります"
		}
		out = append(out, s.raw+"（"+why+"）")
	}
	return out
}

// runPublish は公開用CSVを生成します。
func runPublish(args []string, defaultRoot, defaultGame string, stdout, stderr io.Writer) int {
	fs := newFlagSet("dwloc publish")
	root := fs.String("root", defaultRoot, "翻訳リポジトリのルート")
	game := fs.String("game", defaultGame, gameFlagUsage)
	var locales localeList
	fs.Var(&locales, "locale", "対象のロケール")
	var paths pathList
	fs.Var(&paths, "path", "変換するファイル（入出力兼用）")
	var accepts acceptList
	fs.Var(&accepts, "accept-multiline", "確かめたうえで複数行の値として通すレコード（<ロケール>:<key>。--path では <ファイル>:<key>）")
	noGame := fs.Bool("no-game", false, "ゲームのフォルダーを探しも読みもしない")
	dryRun := fs.Bool("dry-run", false, "書き込まずに内容だけ表示する")
	if code, ok := parseFlags(fs, args, publishUsage, stdout, stderr); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return unexpectedArg(fs, stderr)
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

	// 再生順のデータの形を、読む前に見ます。閉じない引用符は読み込みの誤り
	// （終了コード 2）ではなく、どのファイルの何行目かと直し方にして止めます。
	// 見出しの行へそのまま書く値の改行も、書けば公開ファイルの見出しが壊れるので
	// ここで止めます。どちらもどのロケールにも効くので、ロケールを数えあげる前に
	// 見ます。
	if code := reportOrderShape(*root, stderr); code != exitOK {
		return code
	}

	// 再生順は全ロケールで共通なので1回だけ読む。
	data, err := publish.LoadOrder(*root)
	if err != nil {
		fmt.Fprintf(stderr, "dwloc: %s\n", errorf(*root, "再生順のデータを読めません: %w", err))
		return exitError
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
			fmt.Fprintf(stderr, "dwloc: %s\n", errorf(*root, "%s を読めません: %w", publish.TranslationsDir, err))
			return exitError
		}
		found, err = selectLocales(found, emptyLocalesFor(*root, found, locales), locales)
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

	// 入力・いまの公開ファイル・ゲーム側の公開ファイルを、ここで1回ずつ読みます。
	// 下の形の確かめ・組み立て・2つの確認は、どれもこのバイト列を使い、書く直前に
	// 錠の中で読み直して、1バイトでも違えば書かずに止めます（reportFilesChanged）。
	// 確かめごとに読み直すと、確かめのあいだに画面の保存（dwloc edit）が書き出し先を
	// 書き換えたとき、書き換える前の中身で「失われない」と判断して、書き換えたあとの
	// 中身（画面が保存した訳）を上書きします。
	files := make([]publish.Files, len(targets))
	for i, t := range targets {
		f, err := publish.ReadFiles(t)
		if err != nil {
			fmt.Fprintf(stderr, "dwloc: %s\n", errorf(*root, "%s を読めないので、訳が失われないことを確かめられません: %w",
				displayPath(*root, shapeErrorPath(err, t.Output)), err))
			return exitError
		}
		files[i] = f
	}

	if len(data.Entries) == 0 {
		// 再生順が空でも生成はできるが、見出しが全て消えて全行が UI 見出しの下へ
		// 回るため、差分が全面的になる。黙って進めると事故になるので必ず伝える。
		//
		// 対象を決めて読んだあとで出します。Translations を読めないとき（--root の
		// 打ち間違いなど）は再生順も無いので、先に出すと、止まった本当の理由の前に
		// 的外れな警告が並びます。
		fmt.Fprintf(stderr,
			"dwloc: 警告: %s に再生順の行がありません。見出しは出ず、すべての行が UI の下に並びます。\n",
			displayPath(*root, data.Source))
	}

	// 入力・いまの公開ファイル・ゲーム側の公開ファイルが、読むと訳や原文を取り違える
	// 形になっていないかを最初に見ます。この形のファイルは、下の組み立てと2つの確認も
	// 同じ読み方で読むので、取り違えたまま「そろっている」「失われない」と判断して
	// しまいます。
	//
	// 組み立てより前に見るのは、組み立てが読み方の誤り（閉じない引用符など）で
	// 止まる前に、どのファイルの何行目をどう直すかを出すためです。組み立ての誤りは
	// 「変換できません」（終了コード 2）としか言えません。
	if code := reportShape(*root, targets, files, accepted, stderr); code != exitOK {
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
		out, st, err := publish.Build(data, files[i].Input, files[i].Output)
		if err != nil {
			fmt.Fprintf(stderr, "dwloc: %s\n", errorf(*root, "%s を変換できません: %w", displayPath(*root, t.Input), err))
			return exitError
		}
		built[i], stats[i] = out, st
	}

	// ゲーム側の作業コピーを入力にしたロケールでは、その作業コピーが建っている
	// 土台がコミット済みとそろっているかを先に見ます。ずれていると、訳は消えない
	// まま古い版へ巻き戻るので、次の reportLosses では捕まりません。
	if code := reportBaseDrift(*root, targets, files, stderr); code != exitOK {
		return code
	}

	// 原文の改行が CRLF に変わってキーと合わず、黙って公開されない訳があれば知らせる。
	// 止めはしません（上流も同じ行を落として書きます）。失われる訳の確認より前に
	// 出すのは、その行の訳がいまの公開ファイルにあると、下の確認が「失われる」で
	// 止めるためです。止まった理由がここに書いてあります。
	if code := reportSourceLineEnds(*root, targets, files, stderr); code != exitOK {
		return code
	}

	// 組み立てたものを書くと訳が消えるなら、ここで止める。1件でもあれば
	// どのロケールも書きません。--dry-run でも同じ判定をします。書かないことは
	// どちらでも変わらないので、判定だけ変えると「dry-run では通ったのに
	// 本番で止まる」という食い違いが生まれます。
	if code := reportLosses(*root, targets, files, built, stderr); code != exitOK {
		return code
	}

	if *dryRun {
		changed := 0
		for i, t := range targets {
			note := "変更なし"
			if files[i].Output == nil || !bytes.Equal(files[i].Output, built[i]) {
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

	// 書く直前に、組み立てと確かめに使ったファイル（入力・書き出し先・ゲーム側の
	// 公開ファイル）を読み直して確かめます（改善の決定 3）。組み立てたあとに画面の
	// 保存（dwloc edit）やゲームが入力を書き換えていたら、その訳の入っていない中身を
	// 書くことになります。画面が書き出し先（公開ファイル）を開いて保存していたら、
	// 書くとその訳が消えます。
	//
	// 読み直してから書き終えるまでは、画面の保存と同じ OS の錠を、入力と書き出し先の
	// 両方に掛けておき、そのあいだに dwloc edit の保存が入らないようにします。画面は
	// 開いたファイル（作業コピーか公開ファイル）に錠を掛けて書くので、入力だけに
	// 掛けると、画面が公開ファイルを開いているときの保存と、作業コピーから公開
	// ファイルを書く publish が直列になりません。
	beforePublishWrite()
	unlock, err := lockTargets(targets)
	if err != nil {
		fmt.Fprintf(stderr, "dwloc: %s\n", errorf(*root, "入力と書き出し先の錠を取れないので、1バイトも書きませんでした: %w", err))
		return exitError
	}
	defer unlock()
	if code := reportFilesChanged(*root, targets, files, stderr); code != exitOK {
		return code
	}

	for i, t := range targets {
		out := displayPath(*root, t.Output)
		if err := publish.WriteBytes(t.Output, built[i]); err != nil {
			fmt.Fprintf(stderr, "dwloc: %s\n", errorf(*root, "%s を書き出せません: %w", out, err))
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

// publishSwallowFix は、飲み込み（引用符が別の行で閉じる形）の直し方の頭です。
//
// 引用符の閉じ位置を確かめることと、値の中の " を "" と書くことです。閉じ忘れた " を
// 足すか、値の中の " を重ねれば、後ろの行は値から外れます。
const publishSwallowFix = "引用符の閉じ位置を確かめてください。値を閉じる \" が抜けていれば足し、値の中の \" は \"\" と2つ重ねて書きます。"

// 続きの行がレコードに見えるだけの飲み込みの直し方の後ろに添える文です。正しい
// 複数行の値でも当たる（原文の2行目がカンマを多く含むなど）ので、確かめたうえで
// 通す指定を案内します。指定はレコード単位で、{accept} は --accept-multiline に
// 渡す値（<ロケール>:<key>。[shellWord] でシェルに貼れる形にしたもの）です。
// そのレコードを key で1つに名指せないときは、指定を案内せず、なぜ通せないかを
// 書きます（[publish.Hazard.Acceptable]）。
const (
	publishSwallowAcceptFix = "{line}行目が値の一部として正しい（正しい複数行の値）なら、確かめたうえで --accept-multiline {accept} を付けると書けます。"
	publishSwallowNoKeyFix  = "{line}行目が値の一部として正しくても、このレコードには指定に使える key が無いので、--accept-multiline では通せません" +
		"（飲み込んだのがヘッダーか、key 列も原文も空のレコードか、key 列の値に英数字と . _ : - のほかの文字があるレコードです。" +
		"レコードなら、その key のままでは公開されません）。"
	publishSwallowDupKeyFix = "{line}行目が値の一部として正しくても、同じ key（{key}）のレコードがこのファイルに {count} 件あり、" +
		"どのレコードを通すか決められないので、--accept-multiline では通せません。" +
		"publish が書くのは、そのうち訳の入った最初のレコードだけです。要らないレコードを消してから、もう一度実行してください。"
)

// publishShapeFix は、形ごとの直し方です。キーは reason の識別子です。
//
// 文面の {line} は、理由の置換の line（飲み込まれたと疑う物理行など）で、
// {accept} は --accept-multiline に渡す値です。どちらも [shapeFix] が埋めます。
//
// 閉じ引用符の後ろに文字が続く飲み込みは、どの書き手も作らないので、指定を
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
	reason.PublishSwallowKeyShaped:   publishSwallowFix + publishSwallowAcceptFix,
	reason.PublishSwallowSameColumns: publishSwallowFix + publishSwallowAcceptFix,
	reason.PublishSwallowTextAfterQuote: "引用符の閉じ位置を確かめてください。{line}行目の \" が、前の行で開いた値を閉じています。" +
		"値を閉じる \" が抜けていれば足し、値の中の \" は \"\" と2つ重ねて書きます。",
	reason.PublishLoneCR: "値の中の単独の CR を LF に直すか取り除いてから、もう一度実行してください。",
	reason.PublishCRCut: "{line}行目の手前（前の行の終わり）にある CR を取り除くか、値全体を引用符で囲んでから、もう一度実行してください。" +
		"値に改行を入れたいなら、値を引用符で囲み、改行を LF にします。",
	reason.PublishOrderLineBreak: "{column} 列の値から改行（CR と LF）を取り除いてから、もう一度実行してください。",
}

// publishLoneCRSourceFix は、原文（source_en 列）の中の単独の CR の直し方です。
// マップのキーは、publish.LoneCRKeyKind が返す、その行のキーの決まり方です。
//
// 値の中の単独の CR は LF に直すよう案内しますが、原文を直してよいかは、その行の
// キーの決まり方で変わります（移植仕様 R12〜R17）。どれも架空の作業コピーを上流の
// tools/hash-strings.ps1（pwsh 7.6.6）と dwloc に通して確かめました。キーの決まり方
// （key 列のキー、台詞ID、2列）ごとの代表は、上流との突き合わせの入力の表
// （testdata/upstream の lone-cr-source-*）に置いてあります。
//
//   - 台詞ID の行: キーを原文から作らないので、LF に直しても取り除いても公開されます。
//   - key 列のキーがいまの原文から作ったものと一致する行: 直すとキーと合わなくなり、
//     malformed dropped に数えられるだけで、止まりも知らせもせずに公開されなくなります。
//     上流の道具も、この行を公開しません。翻訳者にできるのは、訳を空に戻して、
//     ほかの行を書くことです。
//   - key 列が無いか空の行: キーはいまの原文から作るので、直すとキーが変わり、ゲームが
//     引かないキーで公開されます。これも訳を空に戻すよう案内します。
//   - 改行を LF にそろえると key 列のキーと一致する行: いまは公開されません。LF に
//     直すと公開されます。取り除くとキーと合わないので、LF に直す案内だけにします。
//   - どちらでもキーと合わない行: 直しても公開されません。
var publishLoneCRSourceFix = map[string]string{
	publish.LoneCRKeyLineID: "原文（source_en 列）の単独の CR を LF に直すか取り除いてから、もう一度実行してください。" +
		"この行は台詞ID の行で、キーを原文から作らないので、原文を直しても訳は公開されます。",
	publish.LoneCRKeyMatches: "原文（source_en 列）は直さないでください。key 列のキーはいまの原文から作ったものなので、" +
		"直すとキーと合わなくなり、その行は黙って公開されなくなります（上流の tools/hash-strings.ps1 もこの行を公開しません）。" +
		"この行の訳を空に戻すと、ほかの行は書けます。",
	publish.LoneCRKeyFromSource: "原文（source_en 列）は直さないでください。key 列が無いか空なので、キーはいまの原文から作ります。" +
		"直すとキーが変わり、ゲームが引かないキーで公開されます。" +
		"この行の訳を空に戻すと、ほかの行は書けます。",
	publish.LoneCRKeyMatchesLF: "原文（source_en 列）の改行を、単独の CR も含めて LF にそろえてから、もう一度実行してください。" +
		"いまの原文は key 列のキーと合わないので、この行は公開されません。LF にそろえると一致します（CR を取り除くと一致しません）。",
	publish.LoneCRKeyMismatch: "この行は、原文（source_en 列）の改行を LF にそろえても key 列のキーと合わないので、直しても公開されません。" +
		"この行の訳を空に戻すと、ほかの行は書けます。",
}

// publishLoneCRKeyFix は、key 列の中の単独の CR の直し方です。
//
// LF に直すと、キーの途中に改行が残り、キーの形でなくなってその行が黙って落ちる
// ことがあります。取り除く直し方だけを案内します。
const publishLoneCRKeyFix = "key 列の値から単独の CR を取り除いてから、もう一度実行してください。"

// loneCRFix は、単独の CR の直し方を列と、原文ならキーの決まり方（理由の置換
// key_kind）で分けます。列名の照合は、publish が列を引くときと同じく ASCII の
// 大文字小文字を区別しません（csvfile.FoldASCII）。
//
// 原文でキーの決まり方が分からないとき（publish が置換を入れなかったとき）は、
// 原文を直させない案内にします。直すとキーと合わなくなる行で「直してよい」と
// 案内すると、訳が止まりも知らせもせずに公開されなくなるからです。
func loneCRFix(column, keyKind string) string {
	switch csvfile.FoldASCII(column) {
	case csvfile.FoldASCII("source_en"):
		if fix, ok := publishLoneCRSourceFix[keyKind]; ok {
			return fix
		}
		return publishLoneCRSourceFix[publish.LoneCRKeyMatches]
	case csvfile.FoldASCII("key"):
		return publishLoneCRKeyFix
	}
	return publishShapeFix[reason.PublishLoneCR]
}

// publishGameBaseFix は、ゲーム側の公開ファイルで見つけた形の直し方の頭に添える文です。
//
// ゲーム側の公開ファイルは、Mod が読み込んでいる訳の土台で、publish はコミット済みと
// 突き合わせるためだけに読みます（「ゲームに入っている翻訳が古い」の確かめ）。
// 手で直すより、コミット済みの公開ファイルを写し直すほうが確かです。
const publishGameBaseFix = "ゲーム側の公開ファイルは、リポジトリの Translations/<ロケール>/strings.csv を同じ場所へ写し直すと直ります。" +
	"手で直すときは次のとおりです。"

// acceptValue は、h のレコードを通すときに --accept-multiline に渡す値です。
//
// ロケールを決めて走らせたなら <ロケール>:<key>、--path で走らせたなら
// <--path に渡したとおりのファイル>:<key> です。--path ではロケール名が決まらない
// ためです。ファイルを相対にして出さないのは、--accept-multiline は --path に渡した
// 綴りと引き当てるので、書き換えると当たらなくなるからです。
//
// key はそのまま出します（publish.Visible の印に置き換えません）。写した値で
// 引き当てるので、印に置き換えると当たらなくなるからです。出すのは指定に使える
// key（publish.NameableKey。英数字と . _ : - だけ）のときだけなので、報告の行を
// 崩す文字は入りません。
func acceptValue(h publish.Hazard) string {
	target := h.Locale
	if target == "" {
		target = h.Path
	}
	return target + ":" + h.Key
}

// shellWord は、--accept-multiline に渡す値 v を、案内に出す形にします。
//
// 案内の指定は、そのままシェルに貼って使われます（決まったことの 16）。--path に
// 渡したファイルのパスには、空白や括弧や ' が入ることがあります（Steam の既定の
// C:\Program Files (x86)\... や、ゲームのフォルダー名の Drag'n Wash）。引用符で
// 囲まずに出すと、貼ったときにシェルが語に割ったり、括弧を式として読んだりして、
// 使い方の誤り（終了コード 2）になります。
//
// そこで、どのシェルでも引用符なしで1語のまま届く文字（ASCII の英数字と
// . _ - : /）だけの値はそのまま出し、それ以外は二重引用符で囲みます。二重引用符は、
// PowerShell・cmd・bash のどれでも、空白・括弧・'・&・; などをそのまま渡します。
// バックスラッシュも囲みます。bash は引用符の外のバックスラッシュを取り除くためです。
// 値は key で終わる（key は [publish.NameableKey] の文字だけ）ので、閉じる引用符の
// 直前がバックスラッシュになる（Windows の引数の規則で \" と読まれる）ことはありません。
//
// 二重引用符の中でも読み替えるシェルのある文字を含むときは、第2戻り値を false に
// します。$ と `（bash と PowerShell）、!（bash）、%（cmd）、続けて書いた \\（bash）、
// PowerShell が引用符として読む “ ” „、引用符そのもの、制御文字です。囲んだ形のまま
// 出し、使うシェルに合わせて書き直すよう添えます（[publishAcceptShellNote]）。
func shellWord(v string) (word string, exact bool) {
	plain, exact := v != "", !strings.Contains(v, `\\`)
	for _, r := range v {
		switch {
		case 'a' <= r && r <= 'z', 'A' <= r && r <= 'Z', '0' <= r && r <= '9', strings.ContainsRune("._-:/", r):
		case strings.ContainsRune("$`!%\"\u201c\u201d\u201e", r), unicode.IsControl(r):
			plain, exact = false, false
		default:
			plain = false
		}
	}
	if plain {
		return v, true
	}
	return `"` + v + `"`, exact
}

// publishAcceptShellNote は、通す指定の値に、二重引用符の中でもシェルによっては読み替える
// 文字があるときに、案内の後ろに添える文です（[shellWord]）。
const publishAcceptShellNote = "この値には、二重引用符の中でもシェルによっては読み替える文字（$ など）があるので、" +
	"使うシェルに合わせて書き直してから付けてください。"

// shapeFix は、h の直し方を報告に出す形にします。
//
// 続きの行がレコードに見える飲み込みでは、そのレコードを key で1つに名指せるときだけ
// 通す指定（[acceptValue]）を、シェルに貼れる形（[shellWord]）で案内します。
// 名指せないときは、なぜ通せないかを書きます。
func shapeFix(h publish.Hazard) string {
	line, column, keyKind := "", "", ""
	for i := 0; i+1 < len(h.Why.Args); i += 2 {
		switch h.Why.Args[i] {
		case "line":
			line = h.Why.Args[i+1]
		case "column":
			column = h.Why.Args[i+1]
		case "key_kind":
			keyKind = h.Why.Args[i+1]
		}
	}
	fix := publishShapeFix[h.Why.ID]
	switch {
	case h.Why.ID == reason.PublishLoneCR:
		fix = loneCRFix(column, keyKind)
	case h.AcceptableShape() && !publish.NameableKey(h.Key):
		fix = publishSwallowFix + publishSwallowNoKeyFix
	case h.AcceptableShape() && h.KeyRecords > 1:
		fix = publishSwallowFix + publishSwallowDupKeyFix
	}
	accept, exact := shellWord(acceptValue(h))
	if !exact && strings.Contains(fix, "{accept}") {
		fix += publishAcceptShellNote
	}
	fix = strings.NewReplacer("{line}", line, "{accept}", accept, "{column}", column,
		"{key}", h.Key, "{count}", strconv.Itoa(h.KeyRecords)).Replace(fix)
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
// 同じく、読んだうえで「書けば訳を取り違える」と分かったので 1 です。確かめるのは
// runPublish が読んだ中身（files）で、読めなかったときは、runPublish が読んだ
// ところで 2 にしています（文面は reportLosses と同じです）。
//
// --accept-multiline の指定が1つでも、通せる行に当たらなければ 2 です（決まったことの
// 16）。打ち間違えた key を黙って無視すると、通したつもりのレコードが止まったままに
// なります。そのときも、形の報告（通した行と止めた行）は先に出します。当たらない
// 理由が、そのレコードの形にあることがあるからです。
//
// 通した行は、止めるときも書くときも標準エラーに出します。止めるときに出すのは、
// 止まった原因を直したあとで、同じ指定で何が通るかを先に見せるためです。
func reportShape(root string, targets []publish.Target, files []publish.Files, accept acceptSet, stderr io.Writer) int {
	var all, found, passed []publish.Hazard
	var passedBy []string
	used := make([]bool, len(accept.specs))
	for i, t := range targets {
		for _, h := range publish.CheckShapeFiles(t, files[i]) {
			all = append(all, h)
			by := accept.covers(h)
			if len(by) == 0 {
				found = append(found, h)
				continue
			}
			for _, i := range by {
				used[i] = true
			}
			passed = append(passed, h)
			passedBy = append(passedBy, accept.specs[by[0]].raw)
		}
	}
	if len(passed) > 0 {
		fmt.Fprint(stderr, publishAcceptText)
		writeHazards(root, passed, stderr, func(i int, _ publish.Hazard) string {
			// 次に書くときも同じ指定が要るので、案内と同じく貼れる形で出す。
			word, _ := shellWord(passedBy[i])
			return "指定: --accept-multiline " + word
		})
	}
	code := exitOK
	if len(found) > 0 {
		fmt.Fprint(stderr, publishShapeText)
		writeHazards(root, found, stderr, func(_ int, h publish.Hazard) string { return "直し方: " + shapeFix(h) })
		fmt.Fprintf(stderr, "dwloc: 読み違える形が %d か所あります。直すまでは書きません。\n", len(found))
		code = exitProblems
	}
	if unmatched := accept.unmatchedAccepts(used, all); len(unmatched) > 0 {
		for _, u := range unmatched {
			fmt.Fprintf(stderr, "dwloc: --accept-multiline の指定が、通せる行に当たりません: %s\n", u)
		}
		code = exitError
	}
	return code
}

// reportOrderShape は、再生順のデータ（data/script_order.csv と data/level_flow.csv）の
// 読み違える形を報告します。1件も無ければ exitOK を返します。
//
// 1件でもあれば exitProblems（1）で、どのロケールも書きません。--accept-multiline では
// 通しません（publish.CheckOrderShape）。読めなくて確かめられなかったときだけが 2 です。
// 見出しの文面は reportShape と同じにします。直す先が再生順のデータであることは、
// ファイルの見出し（「再生順のデータ」）と1件ずつの理由が言います。
func reportOrderShape(root string, stderr io.Writer) int {
	hazards, err := publish.CheckOrderShape(root)
	if err != nil {
		fmt.Fprintf(stderr, "dwloc: %s\n", errorf(root, "再生順のデータを読めません: %s: %w",
			displayPath(root, shapeErrorPath(err, root)), err))
		return exitError
	}
	if len(hazards) == 0 {
		return exitOK
	}
	fmt.Fprint(stderr, publishShapeText)
	writeHazards(root, hazards, stderr, func(_ int, h publish.Hazard) string { return "直し方: " + shapeFix(h) })
	fmt.Fprintf(stderr, "dwloc: 読み違える形が %d か所あります。直すまでは書きません。\n", len(hazards))
	return exitProblems
}

// writeHazards は形の崩れを1件ずつ出します。ファイルの見出しはファイルが変わる
// ときだけ出し、先頭の [publishShapeListMax] 件で切ります。1件ごとに、detail が返す
// 1行（直し方、または通した指定）を添えます。detail の引数は hazards の中の番号です。
func writeHazards(root string, hazards []publish.Hazard, stderr io.Writer, detail func(int, publish.Hazard) string) {
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
		fmt.Fprintf(stderr, "dwloc:         %s\n", detail(i, h))
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
	case h.Order:
		role = "再生順のデータ"
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
func reportBaseDrift(root string, targets []publish.Target, files []publish.Files, stderr io.Writer) int {
	var found []publish.BaseResult
	for i, t := range targets {
		if t.GameBase == "" || files[i].Output == nil {
			continue
		}
		res, err := publish.CheckBaseBytes(t.Locale, files[i].Output, files[i].GameBase)
		if err != nil {
			fmt.Fprintf(stderr, "dwloc: %s\n", errorf(root,
				"%s を読めないので、ゲーム側とそろっているか確かめられません: %w",
				displayPath(root, t.GameBase), err))
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
	// 食い違う訳の見本は画面にだけ出します。記録には、ロケールと件数とキーを残します。
	samples := newUnrecorded(stderr, nil)
	defer samples.Close()
	for _, res := range found {
		fmt.Fprintf(stderr, "dwloc:   %s（%d 件）\n", res.Locale, res.Count)
		for _, d := range res.Sample {
			fmt.Fprintf(stderr, "dwloc:       %s\n", publish.Visible(d.Key))
			fmt.Fprintf(samples, "dwloc:         コミット済み 「%s」\n", d.Repo)
			fmt.Fprintf(samples, "dwloc:         ゲーム側     「%s」\n", d.Game)
		}
		if res.Count > len(res.Sample) {
			fmt.Fprintf(stderr, "dwloc:       ほかに %d 件あります。\n", res.Count-len(res.Sample))
		}
	}
	return exitProblems
}

// publishSourceLineEndText は、原文の CRLF を LF にするとキーが一致する行を
// 見つけたときの見出しです。止めない知らせなので、「書きませんでした」とは言いません。
const publishSourceLineEndText = `dwloc: 注意: 原文の改行が CRLF になっていて、キーと合わずに公開されない訳があります。
dwloc:       表計算ソフトなどで作業コピーを保存し直すと、原文（source_en）の中の改行が LF から CRLF に変わります。
dwloc:       ゲーム内で F1 → Translation → Export working copy を押して作業コピーを作り直すか、原文の中の CRLF を LF に直してください。
`

// reportSourceLineEnds は、原文の CRLF を LF にするとキーが一致する行を知らせます。
// 止めないので、読めたときは見つけても exitOK です。読めなかったときだけが 2 です。
//
// 行とキーは出しますが、原文は出しません。原文はゲームの台本で、報告が貼られる先へ
// 出していくものではないからです。
func reportSourceLineEnds(root string, targets []publish.Target, files []publish.Files, stderr io.Writer) int {
	printed := false
	for i, t := range targets {
		hints, err := publish.SourceLineEndHintsIn(files[i].Input)
		if err != nil {
			fmt.Fprintf(stderr, "dwloc: %s\n", errorf(root, "%s を読めません: %w", displayPath(root, t.Input), err))
			return exitError
		}
		if len(hints) == 0 {
			continue
		}
		if !printed {
			fmt.Fprint(stderr, publishSourceLineEndText)
			printed = true
		}
		fmt.Fprintf(stderr, "dwloc:   %s%s（入力、%d 件）\n", localePrefix(t.Locale), displayPath(root, t.Input), len(hints))
		for i, h := range hints {
			if i >= publishLossListMax {
				fmt.Fprintf(stderr, "dwloc:       ほかに %d 件あります。\n", len(hints)-i)
				break
			}
			fmt.Fprintf(stderr, "dwloc:       %s %s: 原文の CRLF を LF にするとキーが一致します\n",
				lineRange(h.Line, h.EndLine), publish.Visible(h.Key))
		}
	}
	return exitOK
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
func reportLosses(root string, targets []publish.Target, files []publish.Files, built [][]byte, stderr io.Writer) int {
	found := make([][]publish.Loss, len(targets))
	total := 0
	for i, t := range targets {
		var losses []publish.Loss
		var err error
		if files[i].Output != nil {
			// 書き出し先がまだ無ければ、失うものが無い。
			losses, err = publish.CheckLoss(t.Locale, files[i].Output, built[i])
		}
		if err != nil {
			// いまの公開ファイルを解釈できない。失われないことを確かめられていないので、
			// 書かずに終わります。
			fmt.Fprintf(stderr, "dwloc: %s\n", errorf(root,
				"%s を読めないので、訳が失われないことを確かめられません: %w",
				displayPath(root, t.Output), err))
			return exitError
		}
		found[i] = losses
		total += len(losses)
	}
	if total == 0 {
		return exitOK
	}

	fmt.Fprint(stderr, publishLossText)
	// 訳の先頭は画面にだけ出します。記録には、行番号とキーと理由を残します。
	samples := newUnrecorded(stderr, nil)
	defer samples.Close()
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
			// 行は開始行を主に範囲で出す（決まったことのそのほか 3）。キーは、キーを
			// 決められなかった行では key 列の値そのままなので、制御文字を印に置き換える。
			// 訳の先頭は画面にだけ出し、記録には行とキーと理由だけを残します。
			fmt.Fprintf(samples, "dwloc:       %s %s 「%s」 %s\n",
				lineRange(l.Line, l.EndLine), publish.Visible(l.Key), l.Head, l.Why)
			samples.Record("dwloc:       %s %s %s\n", lineRange(l.Line, l.EndLine), publish.Visible(l.Key), l.Why)
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
//
// empty は、公開ファイルも作業コピーも無いロケール（publish.EmptyLocales）です。
// 対象にはできませんが、ディレクトリは実在するので、当たったら「ありません」ではなく、
// 何が無いかと作業コピーの作り方を伝えます（改善の調査の cli-7）。
func selectLocales(targets []publish.Target, empty, want []string) ([]publish.Target, error) {
	if len(want) == 0 {
		return targets, nil
	}
	return matchLocales(targets, empty, want, "--locale")
}

// emptyLocaleText は、公開ファイルも作業コピーも無いロケールを --locale で指されたときの
// 文です。%s にはロケール名が入ります。
const emptyLocaleText = "%s には公開ファイルも作業コピーもありません" +
	"（ゲーム内でその言語を選び、F1 → Translation → Export working copy を押すと作業コピーができます）"

// emptyLocalesFor は、--locale の照合に使う、公開ファイルも作業コピーも無いロケールを
// 返します。want が空なら照合しないので、読みません。
//
// 読めなければ nil を返し、当たらない名前は「指定したロケールがありません」の文に
// 落とします。直前に同じディレクトリを読めているので、ふつうは起きません。
func emptyLocalesFor(root string, targets []publish.Target, want []string) []string {
	if len(want) == 0 {
		return nil
	}
	empty, err := publish.EmptyLocales(root, targets)
	if err != nil {
		return nil
	}
	return empty
}

// matchLocales は want に並べたロケール名に当たる対象を返します。照合の仕方は
// [selectLocales] に書いたとおりで、当たらない名前があれば、flag（指定の名前）を
// 添えた誤りを返します。--locale と --accept-multiline が同じ照合を使うためです。
// empty（公開ファイルも作業コピーも無いロケール）に当たった名前は、[emptyLocaleText] の
// 文で断ります。
func matchLocales(targets []publish.Target, empty, want []string, flag string) ([]publish.Target, error) {
	keep := make([]bool, len(targets))
	var missing, emptyHit []string
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
		if found {
			continue
		}
		if hit, ok := matchName(empty, name); ok {
			if !slices.Contains(emptyHit, hit) {
				emptyHit = append(emptyHit, hit)
			}
			continue
		}
		missing = append(missing, name)
	}
	var msgs []string
	if len(missing) > 0 {
		available := "対象にできるロケールがありません"
		if len(targets) > 0 {
			available = "対象にできるのは " + strings.Join(localeNames(targets), ", ")
		}
		msgs = append(msgs, fmt.Sprintf("%s に指定したロケールがありません: %s（%s）",
			flag, strings.Join(missing, ", "), available))
	}
	if len(emptyHit) > 0 {
		msgs = append(msgs, fmt.Sprintf(emptyLocaleText, strings.Join(emptyHit, ", ")))
	}
	if len(msgs) > 0 {
		return nil, errors.New(strings.Join(msgs, "\ndwloc: "))
	}

	out := make([]publish.Target, 0, len(targets))
	for i, t := range targets {
		if keep[i] {
			out = append(out, t)
		}
	}
	return out, nil
}

// matchName は names の中から name に当たるものを返します。照合は [selectLocales] と
// 同じで、完全一致を先に見て、外れたら大文字小文字を無視します。
func matchName(names []string, name string) (string, bool) {
	if slices.Contains(names, name) {
		return name, true
	}
	for _, n := range names {
		if strings.EqualFold(n, name) {
			return n, true
		}
	}
	return "", false
}

// localeNames は targets のロケール名を並び順のまま取り出します。
func localeNames(targets []publish.Target) []string {
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		names = append(names, t.Locale)
	}
	return names
}
