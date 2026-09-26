package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// beforePublishWrite は、組み立てと確かめを終えて、入力と書き出し先の錠を取る直前に
// 呼ぶ。
//
// 試験が、組み立てたあとに入力や書き出し先が書き換わる場面（画面の保存やゲームの
// 書き出しが割り込む場面）を作るための差し込み口で、ふだんは何もしない。
var beforePublishWrite = func() {}

// publishFilesChangedText は、組み立てたあとに、組み立てと確かめに使ったファイルが
// 変わったときの見出しです。
const publishFilesChangedText = `dwloc: 組み立てたあとに入力か書き出し先が変わったので、1バイトも書きませんでした。
dwloc:       画面（dwloc edit）の保存やゲームの書き出しが、同じファイルを書いた直後です。
dwloc:       このまま書くと、そのとき入った訳が出力に入らないか、書き出し先に入った訳が消えます。
dwloc:       もう一度実行してください。
`

// lockTargets は、どのロケールの入力と書き出し先にも、書き込みの錠
// （publish.LockFiles）を掛ける。返す関数で錠を放す。
//
// 画面の保存（dwloc edit）は、版の照合から rename までを同じ錠で囲むので、錠を
// 持っているあいだは、画面が開いたファイル（作業コピーか公開ファイル）を書き換えない。
// 入力だけに錠を掛けると、画面が公開ファイルを開いているときの保存（--no-game など）と、
// ゲーム側の作業コピーから公開ファイルを書く publish が直列にならない。
//
// ゲーム側の公開ファイルには掛けない。dwloc はそのファイルを書かないので、錠で
// 待ち合う相手がいない。書く直前に読み直すだけにする（reportFilesChanged）。
//
// 同じファイルを指すパス（入力と書き出し先が同じファイル、--path に同じファイルを
// 綴りを変えて2度渡した、など）には1度だけ掛け、掛ける順は錠のファイルの名前の順に
// する（publish.LockFiles）。2つの publish を同時に回しても、互いに待ち合って
// 止まることは無い。
func lockTargets(targets []publish.Target) (func(), error) {
	paths := make([]string, 0, 2*len(targets))
	for _, t := range targets {
		paths = append(paths, t.Input, t.Output)
	}
	return publish.LockFiles(paths)
}

// reportFilesChanged は、どのロケールの入力・書き出し先・ゲーム側の公開ファイルも、
// 組み立てと確かめに使った中身（before）から変わっていないことを確かめる。変わって
// いれば、どのファイルかを出して止める（終了コード 1）。読み直せなければ終了コード 2。
func reportFilesChanged(root string, targets []publish.Target, before []publish.Files, stderr io.Writer) int {
	var changed []string
	for i, t := range targets {
		now, err := publish.ReadFiles(t)
		if err != nil {
			fmt.Fprintf(stderr, "dwloc: %s\n", errorf(root, "%s を読み直せないので、1バイトも書きませんでした: %w",
				displayPath(root, shapeErrorPath(err, t.Input)), err))
			return exitError
		}
		for _, p := range publish.ChangedPaths(t, before[i], now) {
			changed = append(changed, displayPath(root, p))
		}
	}
	if len(changed) == 0 {
		return exitOK
	}
	fmt.Fprint(stderr, publishFilesChangedText)
	for _, p := range changed {
		fmt.Fprintf(stderr, "dwloc:   %s\n", p)
	}
	return exitProblems
}

// shapeErrorPath は、publish.ReadFiles の誤り err が指すファイルを返す。どのファイルか
// 分からなければ fallback を返す。
//
// 報告の頭にこのファイルを出し、誤りの本文からは同じパスを落とす（[errorText] が、
// 前置きに出ているパスを重ねない）。
func shapeErrorPath(err error, fallback string) string {
	var shapeErr *publish.ShapeError
	if errors.As(err, &shapeErr) {
		return shapeErr.Path
	}
	return fallback
}
