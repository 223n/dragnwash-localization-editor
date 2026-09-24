package main

import (
	"errors"
	"strings"
	"testing"
)

// TestArgumentErrorsAreOneLineInJapanese は、引数の誤りを日本語の1行で伝え、
// 使い方の全文ではなく「使い方は … --help で表示します」の1行を添えることを見る。
//
// flag パッケージの英語の1行のあとに使い方の全文（diff は80行を超える）を続けると、
// 端末が小さいときに理由が上へ流れて見えなくなる。単位の要る --idle-timeout は、
// どう書けばよいかも添える。使い方の中の「30s や 1h30m のように書きます」は、
// 全文に埋もれて読まれない。
func TestArgumentErrorsAreOneLineInJapanese(t *testing.T) {
	tests := []struct {
		name string
		args []string
		// want は標準エラーに含まれていてほしい文字列。
		want []string
	}{
		{
			name: "知らないオプション",
			args: []string{"--nope"},
			want: []string{"dwloc: 知らないオプションです: --nope", "使い方は dwloc --help で表示します。"},
		},
		{
			name: "サブコマンドの知らないオプション",
			args: []string{"version", "--nope"},
			want: []string{"dwloc: 知らないオプションです: --nope", "使い方は dwloc version --help で表示します。"},
		},
		{
			name: "整数でない値",
			args: []string{"diff", "--limit", "abc"},
			want: []string{`dwloc: --limit の値「abc」を読めません（整数で書きます）`, "使い方は dwloc diff --help で表示します。"},
		},
		{
			// Windows のパスを打ち間違えても、\ を \\ に化けさせずに打ったまま出す。
			// 化けると、記録でホームのパスを ~ に置き換える照合にも当たらない。
			name: "パスの形の値",
			args: []string{"edit", "--port", `C:\Users\someone\x`},
			want: []string{`dwloc: --port の値「C:\Users\someone\x」を読めません（整数で書きます）`},
		},
		{
			// 制御文字だけは %q と同じ形で書く。1つの誤りを1行に収める。
			name: "改行を含む値",
			args: []string{"diff", "--limit", "1\n2"},
			want: []string{`dwloc: --limit の値「1\n2」を読めません（整数で書きます）`},
		},
		{
			name: "大きすぎる値",
			args: []string{"diff", "--limit", "99999999999999999999"},
			want: []string{"dwloc: --limit の値", "大きすぎます"},
		},
		{
			name: "単位の無い時間",
			args: []string{"edit", "--idle-timeout", "30"},
			want: []string{`dwloc: --idle-timeout の値「30」を読めません`, "30s や 1h30m", "使い方は dwloc edit --help で表示します。"},
		},
		{
			name: "値の無いオプション",
			args: []string{"publish", "--locale"},
			want: []string{"dwloc: --locale には値が要ります", "使い方は dwloc publish --help で表示します。"},
		},
		{
			// 独自の値の型は、自分の言葉で理由を返す。それをそのまま添える。
			name: "空のロケール名",
			args: []string{"publish", "--locale", ""},
			want: []string{`dwloc: --locale の値「」を読めません（ロケール名が空です）`},
		},
		{
			name: "真偽値でない値",
			args: []string{"diff", "--all=maybe"},
			want: []string{`dwloc: --all の値「maybe」を読めません（値を付けないか、true か false を書きます）`},
		},
		{
			name: "オプションの書き方の誤り",
			args: []string{"validate", "---root"},
			want: []string{"dwloc: オプションの書き方が正しくありません: ---root", "使い方は dwloc validate --help で表示します。"},
		},
		{
			name: "余分な引数",
			args: []string{"publish", "ja"},
			want: []string{"dwloc: 余分な引数です: ja", "使い方は dwloc publish --help で表示します。"},
		},
		{
			name: "知らないサブコマンド",
			args: []string{"frobnicate"},
			want: []string{"dwloc: 知らないサブコマンドです: frobnicate", "使い方は dwloc help で表示します。"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(tt.args...)
			if code != exitError {
				t.Fatalf("終了コード = %d, 期待 %d\n%s", code, exitError, stderr)
			}
			if stdout != "" {
				t.Errorf("失敗したのに標準出力へ書いている:\n%s", stdout)
			}
			checkContains(t, "標準エラー", stderr, tt.want)
			// 理由の1行と、使い方の案内の1行だけ。使い方の全文は出さない。
			if lines := strings.Count(stderr, "\n"); lines != 2 {
				t.Errorf("標準エラーが %d 行、2 行を期待:\n%s", lines, stderr)
			}
			// flag パッケージの英語の文を出さない。
			for _, english := range []string{"flag provided", "invalid value", "parse error", "needs an argument", "bad flag syntax"} {
				if strings.Contains(stderr, english) {
					t.Errorf("英語の文 %q が出ている:\n%s", english, stderr)
				}
			}
		})
	}
}

// TestFlagErrorTextFallsBack は、見分けられない形の誤りを英語のまま返すことを見る。
//
// flag が文を変えたときに、理由まで消してしまわないため。言い換えられるところまでは
// 言い換え、残りはそのまま添える。
func TestFlagErrorTextFallsBack(t *testing.T) {
	fs := newFlagSet("dwloc test")
	fs.String("name", "", "文字列の指定")

	tests := []struct {
		name string
		msg  string
		want string
	}{
		{name: "知らない形", msg: "something new", want: "something new"},
		{name: "値が引用されていない", msg: "invalid value abc for flag -name: bad", want: "invalid value abc for flag -name: bad"},
		{name: "区切りが無い", msg: `invalid value "abc" somewhere`, want: `invalid value "abc" somewhere`},
		{name: "真偽値の区切りが無い", msg: `invalid boolean value "abc"`, want: `invalid boolean value "abc"`},
		{
			// 定義されていない名前と、整数でも時間でもない型は、flag の理由をそのまま添える。
			name: "知らない名前", msg: `invalid value "abc" for flag -other: bad`,
			want: `--other の値「abc」を読めません（bad）`,
		},
		{name: "文字列の型", msg: `invalid value "abc" for flag -name: bad`, want: `--name の値「abc」を読めません（bad）`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := flagErrorText(fs, errors.New(tt.msg)); got != tt.want {
				t.Errorf("flagErrorText = %q, 期待 %q", got, tt.want)
			}
		})
	}
}

// TestQuoteValue は、誤りの文に出す値の囲み方を見る。
//
// 打った値のまま見せる（\ を \\ にしない）。見えない文字と、文字として読めない
// バイトだけを %q と同じ形で書く。
func TestQuoteValue(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "abc", want: "「abc」"},
		{value: "", want: "「」"},
		{value: `C:\Users\someone\x`, want: `「C:\Users\someone\x」`},
		{value: "/home/someone/x", want: "「/home/someone/x」"},
		{value: `"引用" と空白`, want: `「"引用" と空白」`},
		{value: "1\n2\r3\t4", want: `「1\n2\r3\t4」`},
		{value: "a\u3000b", want: `「a\u3000b」`},
		{value: "a\xffb", want: `「a\xffb」`},
	}
	for _, tt := range tests {
		if got := quoteValue(tt.value); got != tt.want {
			t.Errorf("quoteValue(%q) = %q, 期待 %q", tt.value, got, tt.want)
		}
	}
}

// TestVersionFlagIsAnAlias は、--version を version の別名として受けることを見る。
//
// Issue の雛形は「最新の版でも起きることを確かめました」と尋ねる。確かめようと
// --version を打った人が、英語の誤りと使い方の全文で止まっていた。
func TestVersionFlagIsAnAlias(t *testing.T) {
	code, stdout, stderr := runCLI("--version")
	if code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}
	if want := "dwloc " + version + "\n"; stdout != want {
		t.Errorf("標準出力 = %q, 期待 %q", stdout, want)
	}

	// 後ろに余分な引数があれば、version と同じく断る。
	code, _, stderr = runCLI("--version", "diff")
	if code != exitError {
		t.Fatalf("余分な引数の終了コード = %d\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{"余分な引数です: diff"})
}

// TestHelpShowsTheSubcommandUsage は、help <サブコマンド> でそのサブコマンドの
// 使い方を出すことを見る。
//
// 以前は後ろの名前を黙って捨て、全体の使い方を出して終了コード0で終わっていた。
// 打ち間違いを黙って別の意味にしない（unexpectedArg と同じ考え方）。
func TestHelpShowsTheSubcommandUsage(t *testing.T) {
	for name, usage := range map[string]string{
		"validate": validateUsage,
		"publish":  publishUsage,
		"diff":     diffUsage,
		"edit":     editUsage,
		"version":  versionUsage,
		"help":     usageText,
	} {
		code, stdout, stderr := runCLI("help", name)
		if code != exitOK {
			t.Errorf("help %s の終了コード = %d\n%s", name, code, stderr)
		}
		if stdout != usage {
			t.Errorf("help %s が、そのサブコマンドの使い方を出していない:\n%s", name, stdout)
		}
	}

	code, _, stderr := runCLI("help", "nope")
	if code != exitError {
		t.Errorf("help nope の終了コード = %d", code)
	}
	checkContains(t, "help nope の標準エラー", stderr, []string{"知らないサブコマンドです: nope", "使い方は dwloc help で表示します。"})

	code, _, stderr = runCLI("help", "diff", "extra")
	if code != exitError {
		t.Errorf("help diff extra の終了コード = %d", code)
	}
	checkContains(t, "help diff extra の標準エラー", stderr, []string{"余分な引数です: extra"})
}

// TestUsageMentionsVersionAndHelp は、全体の使い方に --version と
// help <サブコマンド> が載っていることを見る。載っていなければ使われない。
func TestUsageMentionsVersionAndHelp(t *testing.T) {
	_, stdout, _ := runCLI("help")
	checkContains(t, "使い方", stdout, []string{"--version", "dwloc help <サブコマンド>"})
}
