package validate

import (
	"errors"
	"os"
	"os/exec"
)

// Tracked は「そのパスを git が追跡しているか」を返す関数。
// path は絶対パスで渡される。
//
// 追跡の有無を関数として外から差せるようにしたのは、判定の実体が
// 外部プロセスの呼び出しだから。[CheckTree] の残りの部分はファイルを読むだけで、
// テストでも自由に組み立てられる。git を呼ぶかどうかまでこのパッケージが
// 勝手に決めてしまうと、その一点のためにテストが git リポジトリを要求する。
//
// 既定の実装は [GitTracked]。[CheckTree] に nil を渡せばそれが使われるので、
// 普段の呼び出し側は何も意識しなくてよい。
type Tracked func(path string) bool

// GitTracked は root をカレントディレクトリとして
// `git ls-files --error-unmatch <絶対パス>` を実行する [Tracked] を返す。
//
// 規則（移植仕様「形式検証 R7」）:
//   - 終了コード0のときだけ true。非ゼロ終了は「追跡されていない」であって
//     エラーではない。リポジトリの外で実行した場合もここに落ちて false になる。
//   - 標準出力も標準エラーも捨てる。
//   - git を起動できないとき（PATH に無い、root が無い）だけ、
//     「存在すれば追跡されている」という判定に落とす。
//
// 最後のフォールバックは安全側に倒すための約束。元実装のコメントは
// 「maintainer keeps an untracked copy locally on purpose」と書いており、
// 手元の作業コピーを誤検出する代わりに、原文の混入を見逃さない方を選んでいる。
// git が無い環境では strings.local.csv が「あるだけ」で問題として報告される。
// それが嫌なら自前の [Tracked] を渡す。
//
// 渡すパスは絶対パスのまま。Windows では `C:\...` の形になるが git は受け付ける。
func GitTracked(root string) Tracked {
	return func(path string) bool {
		cmd := exec.Command("git", "ls-files", "--error-unmatch", path)
		cmd.Dir = root
		err := cmd.Run()
		if err == nil {
			return true
		}
		// 起動はできて非ゼロで終わった＝追跡されていない。
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false
		}
		// 起動できなかった（Python の OSError に当たる）。
		_, statErr := os.Stat(path)
		return statErr == nil
	}
}
