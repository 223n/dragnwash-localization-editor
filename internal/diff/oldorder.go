package diff

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// OldOrderSource は「1つ前の版の再生順（data/script_order.csv）」の中身を返す関数。
//
// root は翻訳リポジトリのルート、orderPath はいまの再生順のパス。
//
// 関数型にして外から差せるようにしたのは、既定の実装が外部プロセス（git）の
// 呼び出しだから。internal/validate の [validate.Tracked] と同じ作法にしてある。
// 旧再生順を要るのは引き継ぎ候補だけで、ほかの8カテゴリはファイルを読むだけで
// 判定できる。既定を固定してしまうと、その1カテゴリのためにパッケージ全体の
// テストが git リポジトリを要求することになる。
//
// 取り出せないときは error を返す。呼び出し側はその文面を「判定できない理由」
// としてそのまま画面に出すので、日本語で、1行に収まる短さにすること。
type OldOrderSource func(root, orderPath string) ([]byte, error)

// 旧再生順を取り出せない理由。文面はそのまま利用者の画面に出る。
var (
	// ErrNoGit は git を起動できないこと（PATH に無い、root が無い）。
	ErrNoGit = errors.New("git を実行できません")
	// ErrNoRepository は root が git リポジトリでないか、コミットが1つも無いこと。
	ErrNoRepository = errors.New("git リポジトリではないか、コミットがありません")
	// ErrNotTracked は再生順が git の管理下に無いこと。
	// git add もしていない（インデックスにも HEAD にも無い）か、リポジトリの外にある。
	ErrNotTracked = errors.New("再生順が git の管理下にありません")
	// ErrNotCommitted は再生順を git add しただけで、まだ一度もコミットしていない
	// （インデックスにはあるが HEAD に無い）こと。
	//
	// ErrNotTracked と分けてあるのは、利用者が取るべき手が違うからである。git は
	// この状態を追跡中と答える（git status は新しいファイル、git ls-files は成功）。
	// 「管理下にありません」と書くと git add し直させるが、それでは何も変わらない。
	// 要るのはコミットである。
	ErrNotCommitted = errors.New("再生順がまだコミットされていません")
	// ErrOnlyOneVersion は再生順の履歴が1版しか無いこと。
	// 最初のコミットの直後がこれにあたる。比べる相手がいない。
	ErrOnlyOneVersion = errors.New("再生順の履歴が1版しかありません")
	// ErrGitFailed は git は動いたが取り出しに失敗したこと。
	ErrGitFailed = errors.New("git から1つ前の再生順を取り出せません")
)

// GitOldOrder は git から1つ前の版の再生順を取り出す既定の [OldOrderSource]。
//
// 取り方は2通りある。どちらを使うかは、作業ツリーの再生順が HEAD と違うかで決める。
//
//   - 違う → HEAD の版が「前回の公開に使った版」。ゲームが更新され、ゲーム内で
//     Export game flow をして、まだコミットしていない状態。いちばん多い流れがこれ。
//   - 同じ → その再生順を最後に変えたコミットの1つ前の版を使う。
//     更新をもうコミットしてしまったあとで走らせた場合。
//
// 読むだけで、リポジトリには一切書き込まない。stash も checkout もしない。
//
// 失敗はすべて「候補を出さない」に倒す。間違った引き継ぎ候補は、翻訳者に
// 間違った訳を別の行へ移させる。訳が孤児のまま残るより悪い結果なので、
// 旧版が確かに取れたときだけ判定する。
func GitOldOrder(root, orderPath string) ([]byte, error) {
	rel, err := filepath.Rel(root, orderPath)
	if err != nil {
		return nil, fmt.Errorf("再生順の場所をルートからの相対にできません: %w", err)
	}
	// git は常にスラッシュ区切り。Windows の "\" を渡すと引けない。
	// 先頭に "./" を付けるのは、<コミット>:<パス> の <パス> をカレントディレクトリ
	// からの相対だと git に伝えるため。root が git リポジトリのルートとは限らず、
	// サブディレクトリに置かれた翻訳リポジトリでも同じ書き方で通る。
	spec := "./" + filepath.ToSlash(rel)

	// リポジトリかどうかを先に見る。あとの `git diff --quiet HEAD` は、
	// リポジトリの外でも「差分あり」と同じ終了コード 1 を返す（HEAD を読めない、
	// という別の理由で）。先に弾いておかないと、理由を取り違えた文面を出す。
	if err := gitCheckHead(root); err != nil {
		return nil, err
	}

	differs, err := gitWorktreeDiffers(root, spec)
	if err != nil {
		return nil, err
	}
	if differs {
		// HEAD と違うには、HEAD に再生順がまだ無い場合も入る。git add しただけで
		// コミットしていない再生順がそれで、git diff は新しいファイルとして
		// 「差分あり」を返す。そのまま git show すると失敗し、「git から取り出せ
		// ません」と git の故障を疑わせる理由になる。gitPreviousRevision が親の
		// 中を確かめるのと同じく、HEAD の中の再生順があるかを先に確かめる。
		//
		// ここで HEAD に無いなら、再生順はインデックスにある。git diff HEAD は
		// インデックスにも HEAD にも無いファイル（git add していないもの）を
		// 比べないので、それなら「差分なし」になり、この枝へは来ない。
		check := exec.Command("git", "rev-parse", "--verify", "--quiet", "HEAD:"+spec)
		check.Dir = root
		if err := check.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return nil, ErrNotCommitted
			}
			return nil, ErrNoGit
		}
		return gitShow(root, "HEAD:"+spec)
	}
	rev, err := gitPreviousRevision(root, spec)
	if err != nil {
		return nil, err
	}
	return gitShow(root, rev+":"+spec)
}

// gitCheckHead は root が git リポジトリで、コミットが1つ以上あることを確かめる。
func gitCheckHead(root string) error {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "HEAD")
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return ErrNoRepository
		}
		return ErrNoGit
	}
	return nil
}

// gitWorktreeDiffers は作業ツリーの再生順が HEAD と違うかを返す。
//
// 自分でバイト列を比べずに git に訊くのは、改行の扱い（core.autocrlf）を
// git と同じに保つため。Windows では作業ツリーが CRLF、blob が LF になり、
// 素朴に比べると「毎回違う」と出る。
func gitWorktreeDiffers(root, spec string) (bool, error) {
	cmd := exec.Command("git", "diff", "--quiet", "HEAD", "--", spec)
	cmd.Dir = root
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == 1 {
			// git diff の約束どおり、1 は「差分あり」。
			return true, nil
		}
		// それ以外はリポジトリでない、HEAD が無い（コミットが1つも無い）など。
		return false, ErrNotTracked
	}
	return false, ErrNoGit
}

// gitPreviousRevision は再生順を最後に変えたコミットの、その親を返す。
//
// `git rev-list -2 HEAD -- <path>` の2つ目を使ってはいけない。それは「そのパスを
// 変えた2番目に新しいコミット」であって「最後の変更の直前の内容」ではない。
// このプロジェクトは git-flow でマージコミットを必ず残すので、履歴が分岐して
// いると2つ目に側枝のコミットが来る。その版には既に新しい内容が入っていることが
// あり、そのまま旧再生順として読むと差がほとんど出ず、「引き継ぎ候補 0 件」を
// 保留ではなく判定済みの結論として出してしまう。
//
// 最後に変えたコミット C の第1親（C^）なら、マージでも通常のコミットでも
// 「その変更が入る直前の内容」になる。C がマージコミットなら C^ は取り込み先の
// 側で、まだ変更が入っていない。
func gitPreviousRevision(root, spec string) (string, error) {
	cmd := exec.Command("git", "rev-list", "-1", "HEAD", "--", spec)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", ErrNotTracked
		}
		return "", ErrNoGit
	}
	revs := strings.Fields(string(out))
	if len(revs) == 0 {
		// その道のりで1度も変わっていない＝管理下に無い。
		return "", ErrNotTracked
	}
	parent := revs[0] + "^"
	// 親が無い（最初のコミットで入ったきり）なら比べる相手がいない。
	// 親はあってもそこに再生順が無い（2つ目以降のコミットで足したきり）のも
	// 同じことなので、親そのものではなく親の中の再生順があるかを確かめる。
	// 親だけを見ると、後者を git show の失敗（ErrGitFailed）として報告してしまう。
	check := exec.Command("git", "rev-parse", "--verify", "--quiet", parent+":"+spec)
	check.Dir = root
	if err := check.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", ErrOnlyOneVersion
		}
		return "", ErrNoGit
	}
	return parent, nil
}

// gitShow は `git show <rev>` の出力をそのまま返す。
func gitShow(root, rev string) ([]byte, error) {
	cmd := exec.Command("git", "show", rev)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, ErrGitFailed
		}
		return nil, ErrNoGit
	}
	return out, nil
}
