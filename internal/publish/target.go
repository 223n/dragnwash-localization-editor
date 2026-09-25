package publish

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/order"
)

const (
	// TranslationsDir は公開ファイルが並ぶディレクトリ名（リポジトリルートからの相対）。
	TranslationsDir = "Translations"
	// DiscoveredDir は作業コピーの置き場。Translations の直下にあるが、名前が
	// '_' で始まるのでロケールとしては走査しない。
	DiscoveredDir = "_discovered"
	// StringsFile は公開ファイルの名前。
	StringsFile = "strings.csv"
	// WorkingSuffix は作業コピーのファイル名の後半。Translations/_discovered/<ロケール>.working.csv。
	WorkingSuffix = ".working.csv"

	// dataDir は再生順データの置き場。
	dataDir = "data"
	// scriptOrderFile は再生順の本体。
	scriptOrderFile = "script_order.csv"
	// levelFlowFile はセクション見出しの元になるレベル情報。
	levelFlowFile = "level_flow.csv"

	// localeSkipPrefix はロケールとして扱わないディレクトリ名の先頭。
	// 元実装の `-notlike '_*'` に対応する。
	localeSkipPrefix = "_"
)

// Target は1ロケール分の入出力の組（移植仕様「公開CSV生成 / データ構造」）。
type Target struct {
	// Locale は Translations 直下のディレクトリ名（"ja"、"pt-BR" など）。
	Locale string
	// Input は変換元。作業コピーがあればそれ、無ければ Output と同じパス。
	Input string
	// Output は書き出し先。常に Translations/<ロケール>/strings.csv。
	Output string
	// GameBase は Input の土台になっているゲーム側の公開ファイルの場所。
	//
	// 埋まるのは、Input がゲーム側の作業コピーになったときだけである。
	// リポジトリの作業コピーを採ったときも、公開ファイル自身を採ったときも空になる。
	// つまりこの欄は「入力がゲームから来たか」と「その土台はどこか」を同時に持つ。
	//
	// 要るのは、作業コピーの訳がコミット済みと違っていたときに、それが翻訳者の
	// 編集なのか、ゲーム側が古いための巻き戻りなのかを見分けるためである。
	// 2つを見比べるだけでは区別できない。土台（Modがその作業コピーを書き出した
	// ときに読んでいた公開ファイル）が3点目になる（[CheckBase]）。
	GameBase string
}

// DiscoverTargets は root 配下の Translations を走査して対象を列挙する
// （移植仕様 R8）。探し先はリポジトリの中だけである。
//
// [DiscoverTargetsWithGame] に空のゲームを渡したのと同じ結果になる。ゲームの
// フォルダーを持ち出す用の無い呼び出し側（試験、元リポジトリとの突き合わせ）の
// ために残してある。
func DiscoverTargets(root string) ([]Target, error) {
	return discover(root, "")
}

// DiscoverTargetsWithGame は [DiscoverTargets] と同じ列挙を、ゲーム側の作業コピーも
// 探し先に加えて行う。publish / diff / edit のいずれもこちらを使う。
//
// game が空なら [DiscoverTargets] と同じ結果を返す。--game を指定しないときの
// 振る舞いが、この探し先を足す前と1バイトも変わらないことの根拠になる。
//
// publish もここを通る。以前はこの関数を publish から呼ばせない作りにしていたが、
// それは誤りだった。publish が訳の空の行を書かない（R20）ため、未翻訳の行は
// 公開ファイルに存在しない。ゲーム側の作業コピーを publish が読まないかぎり、
// ゲームで入れた新しい訳はコミットする側へ1行も届かない。危ないのは作業コピーを
// 読むことではなく、作業コピーが不完全なときに訳が消えることなので、守りは
// 入力の探し方ではなく書き出す直前に置いてある（[CheckLoss]）。
func DiscoverTargetsWithGame(root, game string) ([]Target, error) {
	return discover(root, game)
}

// discover は Translations を走査して対象を列挙する。
//
// 規則:
//
//   - ディレクトリだけを見る。Translations/ignore.txt のようなファイルは対象外
//   - 名前が '_' で始まるディレクトリは飛ばす（_discovered を対象にしない）
//   - 作業コピー（<ロケール>.working.csv）があればそれを入力にする。
//     探す順はゲーム、リポジトリの順（[workingCopy]）
//   - 入力が存在しないロケールは対象にしない
//
// ロケールの列挙はリポジトリ側だけで行う。ゲーム側にしか無いロケールは対象に
// しない。出力は常にリポジトリの Translations/<ロケール>/strings.csv なので、
// ゲーム側から列挙すると、コミットする気の無いディレクトリを作ることになる。
//
// 並びはディレクトリ名順。元実装の Get-ChildItem も名前順で返す。
func discover(root, game string) ([]Target, error) {
	dir := filepath.Join(root, TranslationsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	targets := make([]Target, 0, len(entries))
	for _, entry := range entries {
		locale := entry.Name()
		if !isDir(filepath.Join(dir, locale)) {
			continue
		}
		if strings.HasPrefix(locale, localeSkipPrefix) {
			continue
		}
		output := filepath.Join(dir, locale, StringsFile)

		input, base := output, ""
		if working, inGame, ok := workingCopy(root, game, locale); ok {
			input = working
			if inGame {
				base = filepath.Join(game, TranslationsDir, locale, StringsFile)
			}
		}
		if !fileExists(input) {
			continue
		}
		targets = append(targets, Target{
			Locale: locale, Input: input, Output: output, GameBase: base,
		})
	}
	return targets, nil
}

// workingCopy は locale の作業コピーを探す。探す順はゲーム、リポジトリの順。
// 第2戻り値は、当たったのがゲーム側かどうか。
//
// ゲーム側を先に見るのは、Modが書き出した生のファイルを常に入力にするためである。
// 考え方が1つで済み、どちらが入力になるかが、リポジトリに置き忘れたファイルの
// 有無で変わらない。ゲーム側はModのホットリロードが読み書きするファイルで、
// 翻訳者がゲームの前で直しているのはそちらである。
//
// 代わりに、自分で <ルート>/Translations/_discovered へ置いたファイルは、
// ゲーム側に同じロケールの作業コピーがあるかぎり読まれなくなった。黙ったままには
// ならない。どのファイルを読んだかは、画面の「ファイル」の欄と、dwloc diff の
// 「作業コピー」の行と、dwloc publish の "<出力> <- <入力>" の行に必ず出る。
//
// 更新時刻は見ない。「新しいほうを採る」を規則にすると、どちらが入力になるかが
// 実行のたびに入れ替わりうる。
//
// この順は publish と edit で同じでなければならない。両方ともここを通っている。
// それは「edit が書く先は publish が入力に選ぶファイルと同じ」という約束の
// 土台である。片方だけ順を変えると、edit で入れた訳が publish に拾われず、
// コミットする側へ1行も届かない（internal/web の
// TestEditAndPublishPickTheSameInput で押さえてある）。
func workingCopy(root, game, locale string) (path string, inGame, ok bool) {
	name := locale + WorkingSuffix

	if game != "" {
		fromGame := filepath.Join(game, TranslationsDir, DiscoveredDir, name)
		if fileExists(fromGame) {
			return fromGame, true, true
		}
	}
	inRepo := filepath.Join(root, TranslationsDir, DiscoveredDir, name)
	if fileExists(inRepo) {
		return inRepo, false, true
	}
	return "", false, false
}

// WorkingPath は locale の作業コピーの置き場を返す。ファイルが無くても値を返す。
//
// game が空ならリポジトリ側、そうでなければゲーム側を返す。「作業コピーが
// ありません。ゲーム内で書き出してください」と伝えるための道なので、伝える先は
// Modが実際に書き出す場所でなければ案内にならない。
func WorkingPath(root, game, locale string) string {
	base := root
	if game != "" {
		base = game
	}
	return filepath.Join(base, TranslationsDir, DiscoveredDir, locale+WorkingSuffix)
}

// ScriptOrderPath は root 配下の再生順（data/script_order.csv）のパスを返す。
// ファイルが無くても値を返す。
func ScriptOrderPath(root string) string {
	return filepath.Join(root, dataDir, scriptOrderFile)
}

// LevelFlowPath は root 配下の見出しの表（data/level_flow.csv）のパスを返す。
// ファイルが無くても値を返す。
func LevelFlowPath(root string) string {
	return filepath.Join(root, dataDir, levelFlowFile)
}

// LoadOrder は root 配下の data/script_order.csv と data/level_flow.csv を、
// 公開CSV生成と同じ読み方で読む（移植仕様 R5 / R6）。
//
// どちらのファイルも無くてよい。無ければ空として扱い、エラーにしない。
// script_order.csv が空なら見出しは一切出ず、全ての行が末尾へ回る。
//
// エラーを返すのは、ヘッダーの列名が重複しているとき、閉じない引用符があるとき
// （csvfile.UnclosedQuoteError）、読み取りに失敗したとき。publish と画面の書き出しは、
// 閉じない引用符を先に形の確かめ（[CheckOrderShape]）で直し方の案内にして止める。
func LoadOrder(root string) (*order.Data, error) {
	orderPath := ScriptOrderPath(root)
	orderCSV, err := readIfExists(orderPath)
	if err != nil {
		return nil, err
	}
	flowCSV, err := readIfExists(LevelFlowPath(root))
	if err != nil {
		return nil, err
	}

	data, err := order.LoadPowerShell(orderCSV, flowCSV)
	if err != nil {
		return nil, err
	}
	// order パッケージは Source を設定しない約束なので、ここで入れる。
	data.Source = orderPath
	return data, nil
}

// UnclosedFile は、開いた引用符がファイルの終わりまで閉じないので読まなかった
// ファイル1つ。
type UnclosedFile struct {
	// Path はそのファイル。
	Path string
	// Line は引用符が開いた物理行（1始まり）。
	Line int
}

// LoadOrderMarked は [LoadOrder] と同じく読むが、閉じない引用符のあるファイルは
// 誤りにせず、行の無いファイル（無いときと同じ）として読み、どのファイルの何行目かを
// 返す。
//
// diff のための入口である。diff は閉じない引用符のあるファイルに依る判定だけを
// 「判定していません」にして報告を続ける（決まったことの 3）。全体を解釈して読むと
// 引用符が開いた行から後ろが1つの値に崩れるので、そのファイルの行は1つも使わない。
// 値を半分だけ使うと、どこまでが正しい行かを読み手は決められない。
//
// 列名の重複と読み取りの失敗は、[LoadOrder] と同じく誤りにする。
func LoadOrderMarked(root string) (*order.Data, []UnclosedFile, error) {
	orderPath := ScriptOrderPath(root)
	flowPath := LevelFlowPath(root)
	var unclosed []UnclosedFile
	read := func(path string) ([]byte, error) {
		data, err := readIfExists(path)
		if err != nil {
			return nil, err
		}
		if line := csvfile.SplitSegments(data).UnclosedLine; line > 0 {
			unclosed = append(unclosed, UnclosedFile{Path: path, Line: line})
			return nil, nil
		}
		return data, nil
	}
	orderCSV, err := read(orderPath)
	if err != nil {
		return nil, nil, err
	}
	flowCSV, err := read(flowPath)
	if err != nil {
		return nil, nil, err
	}

	data, err := order.LoadPowerShell(orderCSV, flowCSV)
	if err != nil {
		return nil, nil, err
	}
	data.Source = orderPath
	return data, unclosed, nil
}

// BuildTarget は1ターゲット分の入出力ファイルを読み、公開CSVのバイト列を組み立てる。
// ファイルへの書き出しは行わない。
//
// 入力と出力が同じパスのとき（作業コピーが無い既定の場合）は同じファイルを2回読む。
// 元実装も「CSVとして1回」「コメント引き継ぎ用に1回」の2回読んでいる（R8e）。
//
// publish（cmd/dwloc）はこれを使わず、[ReadFiles] で1回読んだ中身を [Build] に渡す。
// 書く直前に、錠の中で同じファイルを読み直して、組み立てに使った中身と比べるため
// である（改善の決定 3）。
func BuildTarget(data *order.Data, t Target) ([]byte, Stats, error) {
	inputCSV, err := os.ReadFile(t.Input)
	if err != nil {
		return nil, Stats{}, err
	}
	existingCSV, err := readIfExists(t.Output)
	if err != nil {
		return nil, Stats{}, err
	}
	return Build(data, inputCSV, existingCSV)
}

// SourceLineEndHint は、原文の CRLF を LF にするとキーが一致する入力の行1つ。
type SourceLineEndHint struct {
	// Line と EndLine は入力での物理行の範囲。
	Line, EndLine int
	// Key はその行の key 列の値（前後の空白を除いたもの）。
	Key string
}

// SourceLineEndHints は、t の入力のうち、訳が入っているのに原文のハッシュが key と
// 合わずに捨てられる行で、原文の CRLF を LF にすると key と一致するものを返す
// （決まったことのそのほか 7）。
//
// 表計算ソフトなどで作業コピーを保存し直すと、原文（source_en）の中の改行が LF から
// CRLF に変わることがある。キーは Mod が LF の原文から計算したものなので、合わなく
// なり、publish はその行を捨てる（R15。上流と同じ）。止めはしないが、新しい訳が
// 黙って公開されないので、呼び出し側が知らせる。値そのものは直さない。原文を
// 書き換えて取り込むと、上流の道具と出力が食い違う。
//
// 読めないときは誤りを返す。形の確かめを通ったあとで呼ぶ前提なので、閉じない引用符の
// 誤りはふつう起きない。
func SourceLineEndHints(t Target) ([]SourceLineEndHint, error) {
	data, err := os.ReadFile(t.Input)
	if err != nil {
		return nil, err
	}
	return SourceLineEndHintsIn(data)
}

// SourceLineEndHintsIn は [SourceLineEndHints] と同じ行を、入力の中身 data から探す。
// publish（cmd/dwloc）が、ほかの確かめと同じバイト列（[ReadFiles]）で探すためにある。
func SourceLineEndHintsIn(data []byte) ([]SourceLineEndHint, error) {
	f, err := csvfile.ReadPowerShell(data)
	if err != nil {
		return nil, err
	}
	var out []SourceLineEndHint
	for _, r := range f.Records {
		if r.Get(colTranslation) == "" {
			continue
		}
		if _, how := rowKey(r.Row); how != keyDropped {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(r.Get(colKey)))
		src := r.Get(colSourceEn)
		if k == "" || !strings.Contains(src, "\r\n") {
			continue
		}
		if key.For(strings.ReplaceAll(src, "\r\n", "\n")) == k {
			out = append(out, SourceLineEndHint{Line: r.Line, EndLine: r.EndLine, Key: strings.TrimSpace(r.Get(colKey))})
		}
	}
	return out, nil
}

// WriteTarget は [BuildTarget] の結果を t.Output へ丸ごと上書き保存する。
// BOM は付けない（移植仕様 R27）。
//
// 元実装と違い、出力先の親ディレクトリが無ければ作る。-Path でまだ存在しない
// 場所を指定したときに、元実装は例外で止まるが、ここでは書けるようにした。
func WriteTarget(data *order.Data, t Target) (Stats, error) {
	out, stats, err := BuildTarget(data, t)
	if err != nil {
		return Stats{}, err
	}
	if err := WriteBytes(t.Output, out); err != nil {
		return Stats{}, err
	}
	return stats, nil
}

// WriteBytes は out を path へ書く。中身は [WriteTarget] と同じで、組み立てと
// 書き出しを分けたい呼び出し側のために切り出してある。
//
// 一時ファイルへ書いてから [os.Rename] で置き換える。元実装の
// File.WriteAllText は出力先をその場で切り詰めてから書くため、途中で失敗すると
// 中途半端な strings.csv だけが残る。既定では入力と出力が同じファイル
// （[DiscoverTargets] が作業コピーの無いロケールに Input=Output を入れる）なので、
// それは原本を失うことを意味する。書くバイト列は変わらないので、元実装との
// バイト互換は崩れない（移植仕様 R27 のGoでの注意）。
//
// 一時ファイルは出力先と同じディレクトリに作る。os.Rename は別ボリュームへ
// またげないため、システムの一時ディレクトリを使うと失敗しうる。
//
// 出力先がリンクのときは、リンクを残したままリンク先へ書く（元実装の
// WriteAllText と同じ結果）。リンクをそのまま rename で置き換えると、リンクが
// 普通のファイルに変わり、訳がリンク先（ゲームのフォルダーなど）へ届かない。
//
//   - シンボリックリンクは、たどった先の実体を出力先にする（[resolveLink]）。
//     一時ファイルも実体のフォルダーに作る。
//   - ハードリンク（名前が2つ以上あるファイル）は、rename では片方の名前しか
//     新しくならないので、その場で書き直す（[overwrite]）。書く前に同じ
//     フォルダーの一時ファイルへ全部を書き切っておき、置き場に同じ大きさを
//     書けることを確かめてから書く。
//
// 権限は、いまあるファイルの値を引き継ぐ。新しいファイルは 0644 で作る。
func WriteBytes(path string, out []byte) error {
	target, err := resolveLink(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// os.CreateTemp は 0600 で作る。新しいファイルは共有する前提なので、元実装が
	// 作るファイルと同じ 0644 相当にする。既にあるファイルは、その値を引き継ぐ。
	// 0600 や 0640 に絞ったファイルを書くたびに広げると、絞った意図が黙って消える
	// （Windows では無視される）。
	perm := fs.FileMode(0o644)
	links := uint64(1)
	if info, err := os.Stat(target); err == nil {
		perm = info.Mode().Perm()
		links = linkCount(target, info)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(target)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// 失敗して抜けるときに一時ファイルを残さない。成功時は rename 済みで
	// 消す相手がいないため、Remove の失敗は無視してよい。ハードリンクの書き直しが
	// 途中で失敗したときだけは、書くはずだった中身として残す。
	keep := false
	defer func() {
		tmp.Close()
		if !keep {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(out); err != nil {
		return err
	}
	// 書いた中身をディスクへ落としてから rename する。ここを省くと、
	// rename の直後に電源が落ちたときに長さ0のファイルが残りうる。
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if links > 1 {
		touched, err := overwrite(target, out)
		if err != nil && touched {
			keep = true
			return fmt.Errorf("%s を書き直す途中で失敗しました。書くはずだった中身は %s に残しました: %w",
				target, tmpName, err)
		}
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, target)
}

// readIfExists はファイルを読む。存在しなければ nil を返しエラーにしない。
// 元実装の Test-Path で分岐している箇所に対応する。
func readIfExists(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

// fileExists は通常ファイルとして存在するかを返す。
//
// [os.Stat] なのでシンボリックリンクを辿る。元実装の Test-Path も辿る。
// 権限などで stat 自体に失敗した場合は false になり、対象から静かに外れる。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// isDir はディレクトリとして存在するかを返す。
//
// [os.ReadDir] が返す DirEntry.IsDir() ではなく [os.Stat] を使うのは、
// シンボリックリンクやジャンクションで置かれたロケールを辿るため。元実装の
// Get-ChildItem -Directory はそれらも Directory 属性を持つので列挙する。
// internal/validate/tree.go も同じ理由で os.Stat を使っており、判定基準を
// そろえてある。ここだけ DirEntry を使うと、validate では検査されるのに
// publish では再生成されないロケールが生まれる。
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
