package gamedir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// AutoValue は --game に書くと自動検出になる語。
//
// 自動検出を別のフラグ（--find-game など）にせず、この語1つで表したのは、
// 「ゲームのフォルダーをどう決めるか」の指定を1か所にまとめるためである。
// フラグが2つあると、両方を書いたときにどちらが勝つかという決まりが要る。
//
// この語と同じ名前のフォルダーを指したいときは、./auto のように区切りを付けるか
// 絶対パスで書く。ロケール名と違い、パスは書き方を変えられる。
const AutoValue = "auto"

// pluginsDir は BepInEx がプラグインを置く場所（ゲームのフォルダーからの相対）。
//
// ここだけ名前を決め打ちにしてよいのは、BepInEx 自身が決める並びだからである。
// 利用者が変えるのはゲームのフォルダー名とプラグインのフォルダー名で、
// その2つは下の走査でワイルドカードとして扱う。
var pluginsDir = []string{"BepInEx", "plugins"}

// steamCommonDir は Steam がゲームを展開する場所（ライブラリからの相対）。
var steamCommonDir = []string{"steamapps", "common"}

// ErrNotFound は候補が1つも無いことを表す番兵。
var ErrNotFound = errors.New("ゲームのプラグインフォルダーが見つからない")

// Plugin は見つかったプラグインのフォルダー1つ。
//
// 欄が1つしかないのに構造体にしてあるのは、[AmbiguousError] が候補を並べる型と
// して出てくるためである。文字列の並びで返すと、候補に何か添えたくなったときに
// 呼び出し側の型がすべて変わる。
//
// 辿ってきた Steam ライブラリを添える欄は置かない。候補のパスは
// <ライブラリ>/steamapps/common/... の形なので、ライブラリはパスの先頭に
// そのまま出ている。同じことを2つの欄で言うと、片方だけ空になる経路
// （--game で直に指定したとき）の意味を読み手が考えることになる。
type Plugin struct {
	// Path はプラグインのフォルダー。この下に Translations/_discovered がある。
	Path string
}

// AmbiguousError は候補が2つ以上あることを表す。
//
// どれか1つを選んで返さない。選んだ根拠（先に見つかった、など）は翻訳者から
// 見えないので、黙って別のゲームのファイルを直させることになる。
type AmbiguousError struct {
	// Candidates は見つかった候補。並びはパス順で、実行のたびに変わらない。
	Candidates []Plugin
}

func (e *AmbiguousError) Error() string {
	paths := make([]string, 0, len(e.Candidates))
	for _, c := range e.Candidates {
		paths = append(paths, filepath.ToSlash(c.Path))
	}
	return fmt.Sprintf("ゲームのプラグインフォルダーが%d個見つかった: %s",
		len(e.Candidates), strings.Join(paths, ", "))
}

// NotPluginError は --game に指定された場所が目印を持たないことを表す。
//
// [ErrNotFound] と分けてあるのは、直しかたが違うからである。指定した場所が
// 外れているときは打ち間違いか場所の取り違えで、自動検出が空振りしたときとは
// 次にやることが違う。
type NotPluginError struct {
	// Path は指定された場所。
	Path string
}

func (e *NotPluginError) Error() string {
	return fmt.Sprintf("%s は Translations/%s を持たない", filepath.ToSlash(e.Path), publish.DiscoveredDir)
}

// Resolve は --game の値から使うフォルダーを1つ決める。
//
// 値が [AutoValue] なら自動検出、それ以外はその場所だけを見る。空文字は
// 呼んではいけない（呼び出し側が「指定が無い」を先に切り分ける）。
//
// 指定された場所は、プラグインのフォルダーそのものでも、その親のゲームの
// フォルダーでもよい。翻訳者がエクスプローラーから貼り付けるのは後者のほうが
// 多く、そこで断ると BepInEx の並びを覚えてもらうことになる。
func Resolve(value string) (Plugin, error) {
	if value == AutoValue {
		return pick(Find(), ErrNotFound)
	}
	abs := canonical(value)
	return pick(Inspect(abs), &NotPluginError{Path: abs})
}

// Find は Steam のライブラリを辿って候補を並べる。並びはパス順。
//
// 誤りを返さない。読めないフォルダー（権限、外付けドライブを外した、など）は
// 飛ばして続ける。1つ読めないだけで探索そのものが止まると、ライブラリを
// 複数持っている人ほど当たらなくなる。1つも見つからなかったことは、
// 空の並びとして返る。
func Find() []Plugin { return findIn(SteamLibraries()) }

// findIn は与えられたライブラリだけを辿る。
//
// [Find] から切り出してあるのは試験のためである。ライブラリの探し方
// （レジストリ、既定の場所、libraryfolders.vdf）は実機に依るので、
// 走査のほうだけを一時ディレクトリで確かめられるようにしてある。
func findIn(libraries []string) []Plugin {
	var found []Plugin
	for _, lib := range libraries {
		common := filepath.Join(append([]string{lib}, steamCommonDir...)...)
		for _, game := range subdirs(common) {
			found = append(found, Inspect(game)...)
		}
	}
	return dedupe(found)
}

// Inspect は1つの場所を見て、そこにある候補を返す。
//
// 見るのは2段だけである。指定された場所そのものと、その下の
// BepInEx/plugins の各フォルダー。深さを決めずに歩き回らないのは、
// Steam のライブラリ全体を再帰で舐めると、ゲームを何十本も入れている人の
// 手元で目に見えて待たされるためである。
func Inspect(dir string) []Plugin {
	var found []Plugin
	if hasDiscovered(dir) {
		found = append(found, Plugin{Path: dir})
	}
	plugins := filepath.Join(append([]string{dir}, pluginsDir...)...)
	// plugins 直下に置かれたプラグインも見る。BepInEx は DLL をフォルダーに
	// 入れずに置くことも許すので、Translations/ がそこへ並ぶことがありうる。
	if hasDiscovered(plugins) {
		found = append(found, Plugin{Path: plugins})
	}
	for _, sub := range subdirs(plugins) {
		if hasDiscovered(sub) {
			found = append(found, Plugin{Path: sub})
		}
	}
	return dedupe(found)
}

// hasDiscovered は、そのフォルダーが目印（Translations/_discovered）を持つかを返す。
func hasDiscovered(dir string) bool {
	if dir == "" {
		return false
	}
	return isDir(filepath.Join(dir, publish.TranslationsDir, publish.DiscoveredDir))
}

// pick は候補の数から1つに決める。0件のときは notFound をそのまま返す。
func pick(found []Plugin, notFound error) (Plugin, error) {
	switch len(found) {
	case 0:
		return Plugin{}, notFound
	case 1:
		return found[0], nil
	default:
		return Plugin{}, &AmbiguousError{Candidates: found}
	}
}

// SteamLibraries は Steam のライブラリを並べる。並びは見つけた順で、重複は畳む。
// 実在しないライブラリ（外付けドライブを外した、など）は並べない。
//
// 先頭は Steam を入れた場所そのもの。libraryfolders.vdf はそこにしか無いので、
// ここを外すと別ドライブのライブラリも辿れなくなる。
func SteamLibraries() []string {
	var out []string
	for _, root := range steamRoots() {
		if !isDir(root) {
			continue
		}
		out = append(out, root)
		for _, lib := range libraryFolders(root) {
			if isDir(lib) {
				out = append(out, lib)
			}
		}
	}
	return dedupeStrings(out)
}

// steamRoots は Steam を入れた場所の候補を並べる。実在するかは見ない。
func steamRoots() []string {
	switch runtime.GOOS {
	case "windows":
		return windowsSteamRoots()
	case "darwin":
		return withHome("Library", "Application Support", "Steam")
	default:
		// Linux と、その他の Unix 系。~/.steam/steam は古くからの場所、
		// ~/.local/share/Steam は新しい場所。Flatpak で入れた人のために
		// .var の下も見る。どれも在るかどうかで絞るので、並べても害はない。
		var out []string
		out = append(out, withHome(".steam", "steam")...)
		out = append(out, withHome(".local", "share", "Steam")...)
		out = append(out, withHome(".var", "app", "com.valvesoftware.Steam",
			"data", "Steam")...)
		return out
	}
}

// withHome はホームディレクトリからの相対を絶対パスにする。
// ホームを取れないときは空を返す。
func withHome(parts ...string) []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{filepath.Join(append([]string{home}, parts...)...)}
}

// libraryFolders は <Steam>/steamapps/libraryfolders.vdf からライブラリを読む。
//
// 読めなければ何も返さない。ファイルが無いのは、ライブラリを1つしか持って
// いない古い Steam でも起こる。その場合も root 自身は候補に入っている。
func libraryFolders(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return nil
	}
	return parseLibraryPaths(string(data))
}

// subdirs は dir の直下のフォルダーを絶対パスで並べる。並びは名前順。
//
// 読めないときは空を返す。権限が無いフォルダーで探索を止めない。
func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		// DirEntry.IsDir() ではなく os.Stat で見る。ジャンクションや
		// シンボリックリンクで別ドライブへ逃がしたライブラリを辿るため。
		// internal/publish の isDir と同じ理由で、判定基準をそろえてある。
		if isDir(path) {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// isDir はディレクトリとして存在するかを返す。
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// dedupe は同じフォルダーを指す候補を畳む。並びはパス順。
//
// 並べ替えるのは、出す順を実行のたびに変えないためである。候補が複数あるときは
// その並びをそのまま人に見せて選ばせるので、実行のたびに入れ替わると
// 「さっきと違うものが出た」と読まれる。
func dedupe(found []Plugin) []Plugin {
	seen := make(map[string]struct{}, len(found))
	out := make([]Plugin, 0, len(found))
	for _, p := range found {
		key := pathKey(p.Path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return pathKey(out[i].Path) < pathKey(out[j].Path) })
	return out
}

// dedupeStrings は同じ場所を指すパスを畳む。並びは与えられた順のまま。
//
// [dedupe] と違って並べ替えないのは、ライブラリの並びに意味があるためである。
// Steam を入れた場所が先頭に来ていて、そこから libraryfolders.vdf を辿る。
func dedupeStrings(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		full := canonical(p)
		key := pathKey(full)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, full)
	}
	return out
}

// canonical は実際のファイルシステム上の綴りへそろえる。
//
// Windows のレジストリが返す値は小文字である（実測で
// c:/program files (x86)/steam）。そのまま出すと、翻訳者が見慣れた綴りと
// 違う場所に見える。決めたフォルダーを必ず画面と端末に出すのが今回の要なので、
// 見て分かる形にそろえておく。
//
// 辿れないとき（まだ無い場所を指された）は Abs と Clean だけかけて返す。
// [filepath.EvalSymlinks] はリンクを解いた先を返すので、ジャンクションで
// ライブラリを別ドライブへ逃がしている人には実体の場所が出る。指した場所より
// 実体のほうが、「どのファイルを読むのか」という問いの答えに近い。
func canonical(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		// Abs が失敗するのは作業ディレクトリを取れないときだけ。
		// 指定された値をそのまま使う。
		abs = path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return filepath.Clean(abs)
}

// pathKey は同じ場所かどうかを比べるための鍵を作る。
//
// Windows と macOS の既定のファイルシステムは大文字小文字を区別しない。
// レジストリが返す値は小文字（実測で c:/program files (x86)/steam）、
// libraryfolders.vdf は大文字混じり（C:\Program Files (x86)\Steam）なので、
// 畳まないと同じライブラリを2回走査して候補が2つに見える。
func pathKey(path string) string {
	key := filepath.Clean(path)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		key = strings.ToLower(key)
	}
	return key
}
