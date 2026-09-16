package diff

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/order"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// Locale は1ロケール分の読み込み結果。
type Locale struct {
	// Name は Translations 直下のディレクトリ名（"ja"、"pt-BR" など）。
	Name string
	// PublishedPath は Translations/<ロケール>/strings.csv。存在しないこともある。
	PublishedPath string
	// WorkingPath は Translations/_discovered/<ロケール>.working.csv。
	// 存在しなくても値は入る。「どこに置けばよいか」を表示で伝えるため。
	WorkingPath string

	// Published は公開ファイルの行。ファイルが無ければ空。
	Published []Row
	// Working は作業コピーの行。読まなかったときは空。
	Working []Row
	// HasWorking は作業コピーを読んだかどうか。
	// ファイルが無いときと --no-working で読まなかったときの両方で false になる。
	HasWorking bool
	// WorkingExists は作業コピーのファイルが実在するかどうか。
	//
	// HasWorking と分けてあるのは、「ありません」と「読みませんでした」を
	// 区別して伝えるため。--no-working を付けた利用者に「ゲーム内で書き出して
	// ください」と促すと、すでに済んでいる作業をやり直させることになる。
	WorkingExists bool
}

// Repo は比較に必要なものを読み終えた状態。
type Repo struct {
	// Root は翻訳リポジトリのルート。
	Root string
	// Order は再生順。ファイルが無くても nil にはならず、行が0件になる。
	Order *order.Data
	// OrderPath は再生順の読み込み元（data/script_order.csv）。
	OrderPath string
	// OldOrder は1つ前の版の再生順。取れなかったときは nil。
	// 使うのは引き継ぎ候補だけで、ほかの8カテゴリはこれを見ない。
	OldOrder *order.Data
	// OldOrderReason は [Repo.OldOrder] が nil のときの理由。取れたときは空。
	//
	// 「0 件」と書けない場面を利用者に伝えるために持つ。旧版が取れないのに
	// 引き継ぎ候補を 0 件と書くと、「移すべき訳は無い」と読まれてしまう。
	OldOrderReason string
	// Locales はディレクトリ名順。--locale で絞っても、ここには全ロケールが入る。
	Locales []Locale
	// EmptyLocales は Translations 直下にディレクトリだけがあり、公開ファイルも
	// 作業コピーも無いロケールの名前。ディレクトリ名順。
	//
	// publish.DiscoverTargets は入力の無いロケールを列挙から外す。publish には
	// それが正しい（書き出す元が無い）が、この道具では「訳が1件も無い」という
	// 最大の要作業にあたるので、黙って消さずに別に持つ。
	EmptyLocales []string
}

// Options は [LoadWith] の指定。
//
// 引数を増やさず構造体にしたのは、旧再生順の取り方を差せるようにしたあとも
// [Load] の呼び出し側（cmd/dwloc）を変えずに済ませるため。
type Options struct {
	// Working が true なら作業コピーも読む。
	Working bool
	// OldOrder は1つ前の版の再生順を返す関数。nil なら [GitOldOrder]。
	//
	// テストで旧再生順を直に渡すための穴でもある。ここを固定にすると、
	// 引き継ぎ候補を試すだけで git リポジトリが要る形になってしまう。
	OldOrder OldOrderSource
}

// Load は root 配下を読む。旧再生順の取り方は [GitOldOrder]。
//
// ロケールの列挙と作業コピーの有無の判定は publish.DiscoverTargets に委ねる。
// publish が入力に選ぶファイルと、この道具が「作業コピー」と呼ぶファイルが
// 食い違うと、「publish を回すとどうなるか」という主張が成り立たなくなる。
// Target.Input != Target.Output であることが、作業コピーがある状態そのものになる。
//
// useWorking が false のときは作業コピーを読まない。公開ファイルだけで何が
// 言えるかを再現したいとき（作業コピーが古いときを含む）に使う。
//
// エラーを返すのは Translations を走査できないとき、再生順のヘッダーに列名の
// 重複があるとき、ファイルを読めないとき、公開ファイルか作業コピーのヘッダーに
// 列名の重複があるとき。公開ファイルが存在しないロケール（作業コピーだけがある
// 状態）はエラーにせず、公開0行として扱う。
func Load(root string, useWorking bool) (*Repo, error) {
	return LoadWith(root, Options{Working: useWorking})
}

// LoadWith は [Load] と同じことを、指定を変えられる形で行う。
func LoadWith(root string, opt Options) (*Repo, error) {
	data, err := publish.LoadOrder(root)
	if err != nil {
		return nil, fmt.Errorf("再生順のデータを読めません: %w", err)
	}
	targets, err := publish.DiscoverTargets(root)
	if err != nil {
		return nil, fmt.Errorf("%s を読めません: %w", publish.TranslationsDir, err)
	}

	repo := &Repo{
		Root:      root,
		Order:     data,
		OrderPath: data.Source,
		Locales:   make([]Locale, 0, len(targets)),
	}
	// 旧再生順が取れなくてもエラーにはしない。引き継ぎ候補1カテゴリだけが
	// 判定できなくなる話で、残り8カテゴリは旧版が無くても成り立つ。
	// git の無い環境で道具そのものが動かなくなるほうが困る。
	repo.OldOrder, repo.OldOrderReason = loadOldOrder(root, repo.OrderPath, opt.OldOrder)

	for _, t := range targets {
		loc := Locale{
			Name:          t.Locale,
			PublishedPath: t.Output,
			WorkingPath:   workingPath(root, t.Locale),
		}

		published, err := readRowsFile(t.Output)
		if err != nil {
			return nil, err
		}
		loc.Published = published

		// 作業コピーの有無は publish と同じ根拠で決める。
		// Input と Output が違えば、publish はそのファイルを入力にする。
		if t.Input != t.Output {
			loc.WorkingExists = true
			// publish が実際に読むパスを覚えておく。_discovered 以外の場所を
			// 入力にする将来の変更があっても、表示が嘘にならないようにする。
			loc.WorkingPath = t.Input
			if opt.Working {
				working, err := readRowsFile(t.Input)
				if err != nil {
					return nil, err
				}
				loc.Working = working
				loc.HasWorking = true
			}
		}
		repo.Locales = append(repo.Locales, loc)
	}

	empty, err := emptyLocales(root, targets)
	if err != nil {
		return nil, err
	}
	repo.EmptyLocales = empty
	return repo, nil
}

// loadOldOrder は1つ前の版の再生順を読む。読めなかったときは理由を日本語で返す。
//
// 理由を error のまま持ち回らずに文字列にするのは、そのまま画面に出す文面だから。
// 呼び出し側（表示）で種類ごとに場合分けする予定が無いのに型を残すと、
// 「どの理由なら何を書くか」の分岐が表示側へ漏れる。
func loadOldOrder(root, orderPath string, src OldOrderSource) (*order.Data, string) {
	if src == nil {
		src = GitOldOrder
	}
	raw, err := src(root, orderPath)
	if err != nil {
		return nil, err.Error()
	}
	data, err := order.LoadPowerShell(raw, nil)
	if err != nil {
		return nil, "1つ前の再生順を読めません"
	}
	if !hasLineIDKeys(data) {
		// 取り出せはしたが、台詞IDとキーの組が1つも無い。列名が変わった古い版か、
		// 取り違えた別のファイル。突き合わせる手がかりが無いので判定しない。
		// ここで 0 件と書くと「移すべき訳は無い」と読まれる。
		return nil, "1つ前の再生順に台詞IDとキーの組がありません"
	}
	return data, ""
}

// hasLineIDKeys は台詞IDとキーがそろった行が1つでもあるかを返す。
func hasLineIDKeys(data *order.Data) bool {
	for _, e := range data.Entries {
		if e.LineID != "" && e.Key != "" {
			return true
		}
	}
	return false
}

// emptyLocales は Translations 直下のロケールのうち、publish が対象にしなかった
// ものを返す。ディレクトリ名順。
//
// 走査そのものを publish.DiscoverTargets と重ねているのは、両者の判定が食い違うと
// 「publish を回すとどうなるか」という主張が崩れるため。ここで見るのは
// 「DiscoverTargets が返さなかったディレクトリ」だけで、判定の規則は作らない。
func emptyLocales(root string, targets []publish.Target) ([]string, error) {
	dir := filepath.Join(root, publish.TranslationsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%s を読めません: %w", publish.TranslationsDir, err)
	}
	covered := make(map[string]struct{}, len(targets))
	for _, t := range targets {
		covered[t.Locale] = struct{}{}
	}

	var out []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "_") {
			continue
		}
		if _, ok := covered[name]; ok {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.IsDir() {
			// ディレクトリでないものは publish も見ない。stat に失敗したものも
			// 「無かった」側に倒す。ここで止めると、読めないファイルが1つあるだけで
			// 報告そのものが出なくなる。
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

// workingPath は作業コピーの既定の置き場を返す。ファイルが無くても値を返す。
func workingPath(root, locale string) string {
	return filepath.Join(root, publish.TranslationsDir, publish.DiscoveredDir, locale+publish.WorkingSuffix)
}

// readRowsFile はCSVファイルを読んで [Row] に直す。
// ファイルが無ければ0行を返しエラーにしない（作業コピーだけがあるロケール、
// あるいはディレクトリだけ作った直後のロケールのため）。
func readRowsFile(path string) ([]Row, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := ReadRows(data)
	if err != nil {
		// パスはそのまま返し、相対化は表示側（cmd/dwloc の displayPath）に任せる。
		// このパッケージは表示の基準になるルートを知らない。
		return nil, &FileError{Path: path, Err: err}
	}
	return rows, nil
}

// FileError はどのファイルで失敗したかを持つエラー。
//
// パスを文面に埋め込まずに持ち回るのは、表示のときに相対化するため。
// 手元の絶対パスには利用者名が入ることがあり、CIのログや不具合報告へ
// 貼られるとそのまま漏れる。
type FileError struct {
	Path string
	Err  error
}

func (e *FileError) Error() string {
	return fmt.Sprintf("%s: %v", filepath.ToSlash(e.Path), e.Err)
}

func (e *FileError) Unwrap() error { return e.Err }
