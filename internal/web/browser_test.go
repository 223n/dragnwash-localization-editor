package web

import (
	"slices"
	"testing"
)

// ここはブラウザーを開く1か所（browser.go）の試験。
//
// 本物のブラウザーは開かない。組み立てたコマンドの引数を見るか、開くコマンドが
// 見つからない状態で呼ぶだけにする。見つからないコマンドは起動されない。

func TestBrowserCommandPassesTheURLAsOneArgument(t *testing.T) {
	// シェルを介さず、URL を1つの引数のまま渡す。& や ; や空白や引用符が入って
	// いても、コマンドとして解釈されることも、引数が割れることもない。
	// URL はこちらが組み立てた値だが、外から来た値を混ぜないという形を固定しておく。
	const openURL = `http://127.0.0.1:1/?t=a&b;c d"e|f` + "`g`$(h)"

	cases := []struct {
		goos string
		want []string
	}{
		// cmd /c start を使わないのは、start がシェルの中で解釈されるため。
		{"windows", []string{"rundll32.exe", "url.dll,FileProtocolHandler", openURL}},
		{"darwin", []string{"open", openURL}},
		{"linux", []string{"xdg-open", openURL}},
		// ほかの Unix も xdg-open に任せる。
		{"freebsd", []string{"xdg-open", openURL}},
	}
	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			cmd := browserCommand(tc.goos, openURL)
			if !slices.Equal(cmd.Args, tc.want) {
				t.Errorf("引数が %q、%q を期待", cmd.Args, tc.want)
			}
			if cmd.Process != nil {
				t.Error("組み立てただけなのに起動している")
			}
		})
	}
}

func TestOpenBrowserFailsWithoutAnOpener(t *testing.T) {
	// 開く手立てが無い環境（WSL や Steam Deck で既定のブラウザーが見つからない）では
	// 誤りを返す。落ちも止まりもしない。受け取った announce が「開けなかった」と
	// 言い、URL は出したまま待ち受けを続ける（[TestAnnounceTellsHowToOpen]）。
	//
	// PATH を空のディレクトリにして、開くコマンドを見つからなくする。
	t.Setenv("PATH", t.TempDir())
	if err := openBrowser("http://127.0.0.1:1/?t=token"); err == nil {
		t.Error("開くコマンドが無いのに誤りを返さない")
	}
}
