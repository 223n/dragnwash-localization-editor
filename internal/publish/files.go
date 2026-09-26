package publish

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
)

// Files は、1ロケールの組み立てと確かめに使うファイルの中身を、1回ずつ読んだもの。
//
// publish（cmd/dwloc）は、形の確かめ・組み立て・土台の食い違い・失われる訳の確かめを、
// どれもこの同じバイト列で行い、書く直前に錠の中で読み直して、1バイトでも違えば
// 書かずに止まる（改善の決定 3）。確かめごとにファイルを読み直す形だと、確かめの
// あいだに画面の保存（dwloc edit）が書き出し先を書き換えたとき、書き換える前の
// 中身で「失われない」と判断して、書き換えたあとの中身（画面が保存した訳）を
// 上書きしうる。
type Files struct {
	// Input は入力の中身。
	Input []byte
	// Output は書き出し先のいまの中身。ファイルが無ければ nil。
	Output []byte
	// GameBase はゲーム側の公開ファイルの中身。t.GameBase が空のとき、書き出し先が
	// 無いとき、ゲーム側の公開ファイルが無いときは nil。読むのは、土台の確かめ
	// （[CheckBase]）と形の確かめ（[CheckTargetShape]）がそのファイルを見る場合だけで
	// ある。
	GameBase []byte
}

// ReadFiles は t の入力・書き出し先・ゲーム側の公開ファイルを読む。
//
// 入力と書き出し先が同じファイル（作業コピーの無いロケールと --path）なら1回だけ
// 読み、Input と Output に同じ中身を入れる。入力は無ければ誤りで、書き出し先と
// ゲーム側の公開ファイルは無くてよい（nil）。
//
// 誤りは [ShapeError] に包み、どのファイルを読めなかったかを添える。
func ReadFiles(t Target) (Files, error) {
	var f Files
	if !sameTarget(t) {
		input, err := os.ReadFile(t.Input)
		if err != nil {
			return Files{}, &ShapeError{Path: t.Input, Err: err}
		}
		f.Input = input
	}
	current, err := readIfExists(t.Output)
	if err != nil {
		return Files{}, &ShapeError{Path: t.Output, Err: err}
	}
	f.Output = current
	if sameTarget(t) {
		if current == nil {
			// 入力が無い（--path に無いファイルを渡した、など）。
			return Files{}, &ShapeError{Path: t.Input,
				Err: &fs.PathError{Op: "open", Path: t.Input, Err: fs.ErrNotExist}}
		}
		f.Input = current
	}
	if current == nil || t.GameBase == "" {
		return f, nil
	}
	game, err := readIfExists(t.GameBase)
	if err != nil {
		return Files{}, &ShapeError{Path: t.GameBase, Err: err}
	}
	f.GameBase = game
	return f, nil
}

// sameTarget は、t の入力と書き出し先が同じファイルか（作業コピーの無いロケールと
// --path）を返す。
func sameTarget(t Target) bool {
	return filepath.Clean(t.Input) == filepath.Clean(t.Output)
}

// ChangedPaths は、before を読んだあとに中身が変わったファイル（t の入力・書き出し先・
// ゲーム側の公開ファイルのうち、after と中身か有無が違うもの）のパスを返す。
// 入力と書き出し先が同じファイルなら、1つとして返す。
func ChangedPaths(t Target, before, after Files) []string {
	var out []string
	if !sameTarget(t) && !sameBytes(before.Input, after.Input) {
		out = append(out, t.Input)
	}
	if !sameBytes(before.Output, after.Output) {
		out = append(out, t.Output)
	}
	if t.GameBase != "" && !sameBytes(before.GameBase, after.GameBase) {
		out = append(out, t.GameBase)
	}
	return out
}

// sameBytes は、a と b が同じ中身かを返す。片方だけが nil（ファイルが無い）なら、
// 中身が空でも違うとみなす。
func sameBytes(a, b []byte) bool {
	return (a == nil) == (b == nil) && bytes.Equal(a, b)
}
