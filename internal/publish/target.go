package publish

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
}

// DiscoverTargets は root 配下の Translations を走査して対象を列挙する
// （移植仕様 R8）。
//
// 規則:
//
//   - ディレクトリだけを見る。Translations/ignore.txt のようなファイルは対象外
//   - 名前が '_' で始まるディレクトリは飛ばす（_discovered を対象にしない）
//   - Translations/_discovered/<ロケール>.working.csv があればそれを入力にする
//   - 入力が存在しないロケールは対象にしない
//
// 並びはディレクトリ名順。元実装の Get-ChildItem も名前順で返す。
func DiscoverTargets(root string) ([]Target, error) {
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
		working := filepath.Join(dir, DiscoveredDir, locale+WorkingSuffix)

		input := output
		if fileExists(working) {
			input = working
		}
		if !fileExists(input) {
			continue
		}
		targets = append(targets, Target{Locale: locale, Input: input, Output: output})
	}
	return targets, nil
}

// LoadOrder は root 配下の data/script_order.csv と data/level_flow.csv を、
// 公開CSV生成と同じ読み方で読む（移植仕様 R5 / R6）。
//
// どちらのファイルも無くてよい。無ければ空として扱い、エラーにしない。
// script_order.csv が空なら見出しは一切出ず、全ての行が末尾へ回る。
//
// エラーを返すのはヘッダーの列名が重複しているときと、読み取りに失敗したとき。
func LoadOrder(root string) (*order.Data, error) {
	orderPath := filepath.Join(root, dataDir, scriptOrderFile)
	flowPath := filepath.Join(root, dataDir, levelFlowFile)

	orderCSV, err := readIfExists(orderPath)
	if err != nil {
		return nil, err
	}
	flowCSV, err := readIfExists(flowPath)
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

// BuildTarget は1ターゲット分の入出力ファイルを読み、公開CSVのバイト列を組み立てる。
// ファイルへの書き出しは行わない。
//
// 入力と出力が同じパスのとき（作業コピーが無い既定の場合）は同じファイルを2回読む。
// 元実装も「CSVとして1回」「コメント引き継ぎ用に1回」の2回読んでいる（R8e）。
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
func WriteBytes(path string, out []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// 失敗して抜けるときに一時ファイルを残さない。成功時は rename 済みで
	// 消す相手がいないため、Remove の失敗は無視してよい。
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
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
	// os.CreateTemp は 0600 で作る。出力先は共有する前提のファイルなので、
	// 元実装が作るファイルと同じ 0644 相当にそろえる（Windows では無視される）。
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
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
