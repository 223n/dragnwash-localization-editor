package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// beforePublishWrite は、組み立てと確かめを終えて、入力の錠を取る直前に呼ぶ。
//
// 試験が、組み立てたあとに入力が書き換わる場面（画面の保存やゲームの書き出しが
// 割り込む場面）を作るための差し込み口で、ふだんは何もしない。
var beforePublishWrite = func() {}

// publishInputChangedText は、組み立てたあとに入力が変わったときの見出しです。
const publishInputChangedText = `dwloc: 組み立てたあとに入力が変わったので、1バイトも書きませんでした。
dwloc:       画面（dwloc edit）の保存やゲームの書き出しが、同じファイルを書いた直後です。
dwloc:       このまま書くと、そのとき入った訳が出力に入らず、入力と書き出し先が同じファイルなら
dwloc:       その訳が消えます。もう一度実行してください。
`

// lockInputs は、どのロケールの入力にも書き込みの錠（publish.LockFile）を掛ける。
// 返す関数で、掛けた順と逆に放す。
//
// 画面の保存（dwloc edit）は、版の照合から rename までを同じ錠で囲むので、錠を
// 持っているあいだは入力を書き換えない。錠はパスの順に掛けるので、publish を
// 2つ同時に回しても、互いに待ち合って止まることは無い。
//
// 同じファイルを指す入力（--path に同じファイルを綴りを変えて2度渡した、など）は
// 1度だけ掛ける。同じ錠を同じプロセスの中で2度取ろうとすると、自分を待って上限まで
// 止まる。
func lockInputs(targets []publish.Target) (func(), error) {
	var paths []string
	var seen []os.FileInfo
	for _, t := range targets {
		info, err := os.Stat(t.Input)
		if err == nil {
			dup := false
			for _, s := range seen {
				if os.SameFile(s, info) {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
			seen = append(seen, info)
		}
		paths = append(paths, t.Input)
	}
	sort.Strings(paths)

	var unlocks []func()
	release := func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}
	for _, p := range paths {
		unlock, err := publish.LockFile(p)
		if err != nil {
			release()
			return nil, err
		}
		unlocks = append(unlocks, unlock)
	}
	return release, nil
}

// reportInputChanged は、どのロケールの入力も、組み立てに使ったバイト列（inputs）から
// 変わっていないことを確かめる。変わっていれば、どのファイルかを出して止める
// （終了コード 1）。読み直せなければ終了コード 2。
func reportInputChanged(root string, targets []publish.Target, inputs [][]byte, stderr io.Writer) int {
	var changed []string
	for i, t := range targets {
		now, err := os.ReadFile(t.Input)
		if err != nil {
			fmt.Fprintf(stderr, "dwloc: %s を読み直せないので、1バイトも書きませんでした: %v\n",
				displayPath(root, t.Input), err)
			return exitError
		}
		if !bytes.Equal(now, inputs[i]) {
			changed = append(changed, displayPath(root, t.Input))
		}
	}
	if len(changed) == 0 {
		return exitOK
	}
	fmt.Fprint(stderr, publishInputChangedText)
	for _, p := range changed {
		fmt.Fprintf(stderr, "dwloc:   %s\n", p)
	}
	return exitProblems
}
