package web

import (
	"os/exec"
	"runtime"
)

// openBrowser は既定のブラウザーで url を開く。
//
// OS ごとの標準のコマンドを、引数を固定して直に起動する。シェルを介さない
// （exec.Command はシェルを通さずに実行ファイルを起動する）ので、url が
// どんな文字を含んでもコマンドとして解釈されることはない。url はこちらが
// 組み立てた値だが、外から来た値を混ぜないという形をここで固定しておく。
//
// 開けないことは珍しくない。WSL やSteam Deck では既定のブラウザーが
// 見つからないことがある。だから呼ぶ側は、開けたかどうかに関わらず URL を
// 標準出力へ出す。この関数の失敗は待ち受けを止めない。
//
// 待たない（Wait を呼ばない）のは、xdg-open も open もすぐ戻るが、
// rundll32 は開いたあとも居残ることがあるため。待つと待ち受けの開始が遅れる。
func openBrowser(url string) error {
	return browserCommand(runtime.GOOS, url).Start()
}

// browserCommand は goos で url を開くコマンドを組み立てる。起動はしない。
//
// 組み立てと起動を分けてあるのは、試験で引数の並びを見るためである。起動まで
// 行う形のままだと、確かめるには本物のブラウザーを開くしかない。
func browserCommand(goos, url string) *exec.Cmd {
	switch goos {
	case "windows":
		// url.dll の FileProtocolHandler は、既定のブラウザーを引くための
		// Windows の標準の入口。cmd /c start と違い、シェルの解釈が入らない。
		return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	case "darwin":
		return exec.Command("open", url)
	default:
		return exec.Command("xdg-open", url)
	}
}
