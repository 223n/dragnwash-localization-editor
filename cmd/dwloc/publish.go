package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// publishUsage は publish の説明。
const publishUsage = `使い方: dwloc publish [--root <ディレクトリ>] [--locale <ロケール>] [--path <ファイル>] [--dry-run]

<ルート>/Translations 配下の各ロケールについて、公開用の strings.csv を作り直します。
tools/hash-strings.ps1 と同じ出力です。

入力は、Translations/_discovered/<ロケール>.working.csv があればそれ、
無ければ Translations/<ロケール>/strings.csv 自身です。
出力は常に <ルート>/Translations/<ロケール>/strings.csv です。
ゲームのフォルダーは読みません。

オプション:
  --root <ディレクトリ>
        翻訳リポジトリのルート（既定: カレントディレクトリ）
  --game <フォルダー>|auto
        publish では受け付けません。指定があると、何も書かずに終わります。
        受け取るだけ受け取って断るのは、共通オプションとしてサブコマンドの前に
        置かれても黙って無視しないためです。
        断る理由は、publish が入力から公開ファイルを作り直す処理だからです。
        ゲーム側の作業コピーを入力にすると、そこに無い行と訳が空の行が、
        コミット済みの公開ファイルから消えます。
  --locale <ロケール>
        対象のロケール。複数回指定するか、カンマ区切りで並べられます。
        省略すると Translations 配下のすべてが対象になります。
  --path <ファイル>
        Translations の走査をやめて、指定したファイルだけを変換します。
        入力と出力が同じファイルになります。複数回指定できます。
        --locale と同時には使えません。
  --dry-run
        何をするかを表示するだけで、ファイルは書きません。

すべての対象を先に組み立ててから書き出します。1件でも失敗すれば何も書きません。

終了コード:
  0   成功
  2   実行時のエラー（Translations が読めない、指定したロケールが無い、など）
`

// publishGameRefusedText は publish が --game を断るときの案内です。
//
// 断るだけで終わらせず、やりたかったこと（ゲームで直した訳をコミットする側へ
// 入れる）への道を1行で示します。示さないと、翻訳者は「では手で写すのか」で
// 止まります。
const publishGameRefusedText = `dwloc: publish は --game を受け付けません。ゲームのフォルダーは読みません。
dwloc:       publish は入力から公開ファイルを作り直す処理です。ゲーム側の作業コピーを入力にすると、
dwloc:       そこに無い行と訳が空の行が、コミット済みの公開ファイルから消えます。
dwloc:       ゲームで直した訳をコミットする側へ入れるには dwloc edit --game を使ってください。
dwloc:       保存のたびに、公開ファイルとゲーム側の作業コピーの両方の「その行」を差し替えます。
dwloc:       publish は --game を付けずに実行してください。
`

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
	dryRun := fs.Bool("dry-run", false, "書き込まずに内容だけ表示する")
	if code, ok := parseFlags(fs, args, publishUsage, stdout, stderr); !ok {
		return code
	}
	if fs.NArg() > 0 {
		return unexpectedArg(fs.Arg(0), publishUsage, stderr)
	}
	if *game != "" {
		// 受け取るだけ受け取って、ここで断ります。黙って無視すると、
		// 「ゲーム側の作業コピーから公開ファイルを作った」と読まれます。
		// 実際には読んでいないので、訳が入っていないことに気づけません。
		//
		// 弾かずに使うともっと悪い。publish は入力から作り直す処理なので、
		// ゲーム側の作業コピーを入力にすると、そこに無い行がコミット済みの
		// 公開ファイルから消えます（internal/publish の doc コメントに実測値）。
		fmt.Fprint(stderr, publishGameRefusedText)
		return exitError
	}
	if len(paths) > 0 && len(locales) > 0 {
		// 元実装の -Path は走査そのものを置き換えるので、絞り込みと重ねる意味が
		// ありません。黙ってどちらかを無視すると、絞ったつもりの指定が効かない
		// 事故になります。
		fmt.Fprintln(stderr, "dwloc: --path と --locale は同時に指定できません")
		return exitError
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
		found, err := publish.DiscoverTargets(*root)
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

	// まず全件を組み立てる。書き出しはその後。途中で失敗したとき
	// 「先頭の数ロケールだけ新しい内容、残りは古い内容」という半端な状態を
	// 作らないためです。入力ヘッダーの列名重複のように、読み始めて初めて分かる
	// 失敗があるので、事前の検査では代われません。
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
