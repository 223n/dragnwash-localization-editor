// Package sourcerepo は、実データの試験が読む翻訳リポジトリ（223n/dragnwash-localization の
// 手元のチェックアウト）の場所を決めるための部品です。
//
// 試験だけが使います。製品のコード（cmd と internal のほかのパッケージの本体）からは
// 呼びません。このパッケージは標準ライブラリのほかに何も読み込みません。
// internal/csvfile の試験からも使うためで、csvfile を読み込むと循環します。
//
// 以前は、実データの試験のあるパッケージ（csvfile・diff・edit・order・publish・
// validate・linekey）が、同じ探し方をそれぞれに持っていました。環境変数を指定した
// ときの扱い（[Find]）を1か所で決めるために、ここへまとめました。
//
// 実データの試験は、上流の main に追従します（改善の決定 31）。ロケールの数と行数は
// 決め打ちにせず、入力から数えます（[Locales]、[ContentLines]）。以前は上流 003ed1e に
// 固有の値（13 ロケール、ja の 1721 行、BOM 付きの level_flow.csv、8 列の
// script_order.csv など）を決め打ちにしていたので、いまの main を渡すと、コードと
// 関係なく落ちていました。特定の版に固有の形の確かめは、合成の見本の試験にあります。
package sourcerepo

import (
	"os"
	"path/filepath"
)

// Env は、翻訳リポジトリの場所を渡す環境変数です。
const Env = "DRAGNWASH_SOURCE_REPO"

// Candidates は、[Env] が無いときに探す場所です。作者の PC の、上流 main の
// チェックアウトです。ほかの PC と CI には無いので、試験は飛ばします。
//
// 以前は、作者の PC の古い worktree（上流 003ed1e）を先に探していました。実データの
// 試験は上流 main に追従する（改善の決定 31）ので、main のチェックアウトだけにしました。
var Candidates = []string{`C:\dev\223n\dragnwash-localization`}

// TB は、testing.TB のうち [Find] が使うものです。試験の中で、Fatalf と Skipf が
// 呼ばれたことを確かめられるように、testing.TB そのものではなくインターフェイスで
// 受けます。
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Skipf(format string, args ...any)
}

// Find は、ルートからの相対パス marker のある翻訳リポジトリのルートを返します。
//
//   - [Env] を指定したのに、そこに marker が無ければ Fatalf で落とします（改善の
//     決定 30）。打ち間違えたパスで実データの試験がすべて飛び、確かめたつもりで
//     何も確かめずに ok になるのを防ぎます。
//   - [Env] が無ければ [Candidates] を探し、どこにも無ければ Skipf で飛ばします。
//     CI には翻訳リポジトリが無いので、飛ばせることが要ります。
//
// Fatalf と Skipf のあとは空を返します（本物の testing.TB はそこで戻りません）。
func Find(t TB, marker ...string) string {
	t.Helper()

	if env := os.Getenv(Env); env != "" {
		if exists(filepath.Join(append([]string{env}, marker...)...)) {
			return env
		}
		t.Fatalf("%s に指定した %s に %s がありません。翻訳リポジトリのルートを指定してください",
			Env, env, filepath.Join(marker...))
		return ""
	}
	for _, root := range Candidates {
		if exists(filepath.Join(append([]string{root}, marker...)...)) {
			return root
		}
	}
	t.Skipf("翻訳リポジトリが見つからないので飛ばす（%s で場所を指定できる）", Env)
	return ""
}

// exists は path が在るかを返します。
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
