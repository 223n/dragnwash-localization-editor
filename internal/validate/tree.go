package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// TranslationsDir はロケールごとのディレクトリを束ねる場所。リポジトリルート直下。
	TranslationsDir = "Translations"

	// DiscoveredDir は作業コピー（英語原文つき）の置き場。名前が '_' で始まるので
	// ロケール走査からは外れる。ここにあるファイルは1つもコミットされてはならない。
	DiscoveredDir = "_discovered"

	// PublishedFile は公開ファイルの名前。検査の対象はこれだけ。
	PublishedFile = "strings.csv"

	// LocalFile はロケールごとの作業コピーの名前。手元に置くのは構わないが、
	// 英語原文を含むのでコミットされていてはならない。
	LocalFile = "strings.local.csv"
)

// mustNotBeCommitted は _discovered 配下と strings.local.csv で共通の文言。
const mustNotBeCommitted = "must not be committed (contains source text)"

// CheckTree は root 配下の Translations をすべて検査する。元実装の main に対応する。
//
// root はリポジトリのルート。元実装はスクリプト自身の位置から2階層上を
// ルートにするが（移植仕様「形式検証 R1」）、Go のバイナリには
// 「ソースの位置」が無いので、決めるのは呼び出し側の仕事にした。
//
// tracked は「そのパスを git が追跡しているか」を返す関数。nil なら
// [GitTracked] を root に対して作って使う。テストや、git を呼びたくない場所では
// 自前の関数を渡す（[Tracked] 参照）。
//
// 見る順序は上流 dev の main() のまま（移植仕様 R3〜R6）。問題の並びもこの順になる。
//
//  1. Translations/_discovered の直下にあるファイルのうち、追跡されているもの。
//     サブディレクトリには降りない。
//  2. Translations 直下の各ディレクトリを名前順に。名前が '_' で始まるものは飛ばす。
//     ロケールごとに strings.local.csv（追跡されていたら問題）、strings.csv
//     （あれば中身を検査、無ければ "no strings.csv"）、credits.txt（あれば
//     最初の行の状態語）、textures/（あれば中身）の順。
//
// エラーを返すのは Translations が読めないとき、あると分かっているファイルや
// フォルダーが読めないとき、textures/credits.csv が CSV として読めないときだけ。
// 元実装はどれもトレースバックで異常終了する（移植仕様「形式検証 / 未決の点」）。
// 落ちるより、何が読めなかったかを呼び出し側へ返す方がCIで原因が分かる。
// 中身の問題は error ではなく [Problem] として返るので、error が非nilなら
// 「検査できなかった」の意味になる。
func CheckTree(root string, tracked Tracked) ([]Problem, error) {
	if tracked == nil {
		tracked = GitTracked(root)
	}
	show := newDisplay(root)
	translations := filepath.Join(root, TranslationsDir)

	problems := []Problem{}

	discovered := filepath.Join(translations, DiscoveredDir)
	// 元実装の is_dir() はシンボリックリンクを辿るので os.Stat を使う。
	// 無い・ディレクトリでないときは丸ごと飛ばす（エラーにしない）。
	if info, err := os.Stat(discovered); err == nil && info.IsDir() {
		entries, err := os.ReadDir(discovered)
		if err != nil {
			return nil, fmt.Errorf("%s が読めない: %w", show.of(discovered), err)
		}
		for _, entry := range entries {
			path := filepath.Join(discovered, entry.Name())
			// DirEntry ではなく os.Stat で見る。元実装の is_file() はリンクを辿るため、
			// DirEntry.Type() で判定するとファイルへのリンクを見逃す。見逃す方向の
			// 差は「原文の混入を防ぐ」というこの検査の目的に反する
			// （移植仕様「敵対検証」[low]）。
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if tracked(path) {
				problems = append(problems, Problem{Path: show.of(path), Message: mustNotBeCommitted})
			}
		}
	}

	// os.ReadDir は名前の昇順（バイト順）で返す。元実装の sorted() は Windows では
	// 大文字小文字を無視した順になるが、実在のロケール名（de, eo, ..., zh-Hant）は
	// 先頭が小文字でそろっているので並びは変わらない。仮にずれても出力の順が
	// 変わるだけで、合否には影響しない。
	entries, err := os.ReadDir(translations)
	if err != nil {
		return nil, fmt.Errorf("%s が読めない: %w", show.of(translations), err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "_") {
			continue
		}
		dir := filepath.Join(translations, name)
		// ここも os.Stat。ディレクトリへのシンボリックリンクをロケールとして扱う
		// 元実装（Path.is_dir()）に合わせる。Translations/ignore.txt のような
		// 通常ファイルはここで外れる。
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}

		local := filepath.Join(dir, LocalFile)
		if _, err := os.Stat(local); err == nil && tracked(local) {
			problems = append(problems, Problem{Path: show.of(local), Message: mustNotBeCommitted})
		}

		published := filepath.Join(dir, PublishedFile)
		if _, err := os.Stat(published); err != nil {
			// 「無い」ことを指すので、表示パスはファイルではなくディレクトリ。
			// 上流はここで次のロケールへ飛ばず、credits.txt と textures/ も見る。
			problems = append(problems, Problem{Path: show.of(dir), Message: "no " + PublishedFile})
		} else {
			data, err := os.ReadFile(published)
			if err != nil {
				return nil, fmt.Errorf("%s が読めない: %w", show.of(published), err)
			}
			problems = append(problems, CheckFile(show.of(published), data)...)
		}

		// 上流 dev の 912f519。無ければ何も言わない（ゲームは暫定訳として扱う）。
		credits := filepath.Join(dir, CreditsFile)
		if _, err := os.Stat(credits); err == nil {
			data, err := os.ReadFile(credits)
			if err != nil {
				return nil, fmt.Errorf("%s が読めない: %w", show.of(credits), err)
			}
			problems = append(problems, checkCredits(show.of(credits), data)...)
		}

		// 上流の cc01bfc。is_dir() なので、同じ名前のファイルは見ない。
		textures := filepath.Join(dir, TexturesDir)
		if info, err := os.Stat(textures); err == nil && info.IsDir() {
			found, err := checkTextures(show, translations, name, textures)
			if err != nil {
				return nil, err
			}
			problems = append(problems, found...)
		}
	}
	return problems, nil
}

// display は報告に出すパスの整形を受け持つ。元実装の display() に対応する。
type display struct {
	// root は正規化ずみのリポジトリルート。
	root string
}

// newDisplay は root を絶対パスにし、可能ならシンボリックリンクも解決して覚える。
//
// 元実装は path.resolve().relative_to(ROOT) なので、ルートと対象の両方が
// 解決ずみであることを前提にしている（移植仕様「敵対検証」[low] R2）。
// 片方だけ解決すると、リンク越しのパスで相対化に失敗したりしなかったりする。
func newDisplay(root string) display {
	return display{root: resolvePath(root)}
}

// of はリポジトリ相対のスラッシュ区切りを返す。ルート配下でなければ
// 渡されたパスをそのまま返す（元実装の ValueError 経路）。
func (d display) of(path string) string {
	rel, err := filepath.Rel(d.root, resolvePath(path))
	if err != nil {
		return path
	}
	// filepath.Rel は ".." を含む結果も成功で返すので、自前で外を弾く。
	// Python の relative_to はこの場合 ValueError になる。
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return filepath.ToSlash(rel)
}

// resolvePath は絶対パスにしてからシンボリックリンクを解決する。
//
// 存在しないパス（行き先の無いリンクなど）では EvalSymlinks が失敗するので、
// 解決できる祖先まで遡って祖先だけを解決し、残りをそのまま付け直す。元実装の
// Path.resolve(strict=False) と同じく「解決できるところまで」解決する。祖先を
// 解決しないと、ルート（解決ずみ）と形が食い違い、相対化に失敗する。Windows の
// 8.3 形式の短い名前（C:\Users\RUNNER~1）や、macOS の /var → /private/var が
// 祖先にあると、そうなる。
//
// どこも解決できなければ絶対パスのままを返す。
func resolvePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	rest := ""
	for p := path; ; {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			if rest == "" {
				return resolved
			}
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return path
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}
