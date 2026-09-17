package gamedir

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// regTimeout は reg query を待つ上限。
//
// 待ち受けでもバッチでもないので、ここで止まったまま戻らないことだけを避ける。
// 実測（この開発機、Windows 11 Pro 26200）で初回 32 ミリ秒、以降 10 ミリ秒
// 前後だったので、2秒あれば刺さっている以外の理由で切れることはない。
const regTimeout = 2 * time.Second

// steamRegistryKey は Steam が入れた場所を書くレジストリの鍵。
//
// HKCU（利用者ごと）を見る。HKLM 側にも場所は入るが、そちらは
// インストーラーが書いた値で、あとから別のドライブへ移した人には古い。
const steamRegistryKey = `HKCU\Software\Valve\Steam`

// steamRegistryValue はその鍵の中で場所が入っている値名。
const steamRegistryValue = "SteamPath"

// windowsSteamRoots は Windows で Steam を入れた場所の候補を並べる。
//
// レジストリと既定の場所の両方を見る。どちらか一方にしないのは、
// 取りこぼす人が違うためである。既定の場所だけだと D:\Steam のように
// 移した人に当たらず、レジストリだけだと reg.exe を起動できない環境
// （切り詰めた Windows、権限を絞った端末）で何も出せない。
//
// レジストリを os/exec の reg query で読むことにしたのは、標準ライブラリに
// レジストリを読む手段が無く、このリポジトリでは外部依存を足さないと決めて
// いるためである。reg.exe は Windows に必ずあり、出力の形
// （「値名<空白>型<空白>値」）は表示言語を変えても変わらない。値名も型名も
// ASCII で、翻訳されない。
//
// ただし出力の文字コードは UTF-8 ではない（[asciiOnly] の注記を参照）。
// そのため、読み取った値は ASCII のときしか採らない。非ASCIIの場所へ Steam を
// 入れている人にはレジストリが効かず、既定の場所だけが頼りになる。当たらなければ
// --game <フォルダー> で直に指定してもらう（[ErrNotFound] の案内）。
//
// 起動する処理をここへ入れるのは、--game auto と書かれたときだけである。
// 指定が無ければ [Resolve] ごと呼ばれないので、ふだんの実行では走らない。
func windowsSteamRoots() []string {
	var out []string
	if path := steamPathFromRegistry(); path != "" {
		out = append(out, path)
	}
	// 既定の場所。32ビット版の Steam なので Program Files (x86) に入るが、
	// 環境変数が無い（切り詰めた環境）ときのために素の場所も並べる。
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "ProgramW6432"} {
		if dir := os.Getenv(env); dir != "" {
			out = append(out, filepath.Join(dir, "Steam"))
		}
	}
	out = append(out, `C:\Program Files (x86)\Steam`, `C:\Program Files\Steam`)
	return out
}

// steamPathFromRegistry はレジストリから Steam を入れた場所を読む。
// 読めなければ空を返す。誤りは返さない（候補が1つ減るだけで、続けられる）。
func steamPathFromRegistry() string {
	ctx, cancel := context.WithTimeout(context.Background(), regTimeout)
	defer cancel()

	// 引数は固定で、外から来た値を1つも混ぜない。混ぜる場所を作ると、
	// いつか --game の値がそのままここへ渡る。
	cmd := exec.CommandContext(ctx, "reg", "query", steamRegistryKey, "/v", steamRegistryValue)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	if !asciiOnly(out) {
		// 読めないものを読んだことにしない。理由は [asciiOnly] にある。
		return ""
	}
	return parseRegValue(string(out), steamRegistryValue)
}

// asciiOnly は、すべてのバイトが 0x80 未満かを返す。
//
// reg.exe の出力は UTF-8 ではない。子プロセスのコードページで返る。実測
// （この開発機、Windows 11 Pro 26200）で `chcp` は 932 を返し、非ASCIIを含む
// レジストリの値は CP932 のバイト列で返った。
//
//	DisplayName REG_SZ Dell 83 41 83 4e 83 65 83 42 83 75 82 c8 83 79 83 93 Service
//
// これは「アクティブなペン」の CP932 である。出力全体に utf8.Valid をかけると
// false になる。つまり、そのまま string へ入れるとパスが化ける。化けたパスは
// どのフォルダーにも当たらないので実害は無い……とは言えない。運悪く別の場所に
// 当たったときに、翻訳者は「なぜかここを直している」状態になる。
//
// コードページからの変換表は標準ライブラリに無く、このリポジトリでは外部依存を
// 足さないと決めている。chcp 65001 を挟む手もあるが、コードページはコンソールの
// 状態で、子プロセスのために付け替えると呼び出し側の端末に影響が残りうる。
// 挟んだところで、cmd.exe を1つ増やして起動を遅くするだけで、変換の正しさは
// reg.exe 任せのままである。
//
// 代わりに「ASCII のときだけ信じる」ことにした。全バイトが 0x80 未満なら、
// CP932 でも CP949 でも CP1252 でも UTF-8 でも同じバイト列になるので、
// その1点においては変換が要らない。utf8.Valid ではなく ASCII で見るのは、
// CP932 のバイト列が偶然 UTF-8 として通ることがあるためである。上の例の
// 82 c8 83 79 のうち c8 83 は U+0203 として読めてしまう。
func asciiOnly(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

// parseRegValue は reg query の出力から値を取り出す。
//
// 出力はこの形。字下げと空白の数は版によって違うので、数えない。
//
//	HKEY_CURRENT_USER\Software\Valve\Steam
//	    SteamPath    REG_SZ    c:/program files (x86)/steam
//
// 値そのものに空白が入る（Program Files）ので、型名より後ろを丸ごと取る。
// 型名で切るのは、値名と型名の間の空白だけを頼りに分けると、値名に空白を
// 含む別の値を拾ったときに崩れるためである。
func parseRegValue(output, name string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "\r"))
		rest, ok := strings.CutPrefix(line, name)
		if !ok || rest == "" {
			continue
		}
		// 値名の直後は必ず空白。ここを見ないと SteamPathOld のような
		// 別の値名を当たりにしてしまう。
		if !isSpace(rest[0]) {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			continue
		}
		// 先頭は型名（REG_SZ など）。それを落とした残りが値で、値の中の
		// 空白は残す。
		value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), fields[0]))
		if value != "" {
			return value
		}
	}
	return ""
}

// isSpace は半角の空白か水平タブかを返す。
func isSpace(b byte) bool { return b == ' ' || b == '\t' }

// parseLibraryPaths は libraryfolders.vdf からライブラリの場所を拾う。
//
// VDF の完全な構文解析はしない。要るのは "path" の値だけで、入れ子の深さも
// 型も見る必要が無い。行ごとに『"鍵" "値"』の組を読み、鍵が path のものを
// 集める。ここで拾いすぎても、実在しないフォルダーは [SteamLibraries] が
// 落とす。
//
// 値の中の \\ と \" は VDF のエスケープなので戻す。戻さないと
// C:\\Program Files (x86)\\Steam のままになり、どのフォルダーにも当たらない。
func parseLibraryPaths(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := vdfPair(line)
		if !ok || key != "path" || value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

// vdfPair は『"鍵" "値"』の1行を2つに分ける。組になっていなければ ok が false。
func vdfPair(line string) (key, value string, ok bool) {
	rest := line
	fields := make([]string, 0, 2)
	for range 2 {
		start := strings.IndexByte(rest, '"')
		if start < 0 {
			return "", "", false
		}
		field, after, done := cutQuoted(rest[start+1:])
		if !done {
			return "", "", false
		}
		fields = append(fields, field)
		rest = after
	}
	return fields[0], fields[1], true
}

// cutQuoted は閉じ引用符までを取り出し、エスケープを戻す。
// 閉じ引用符が無ければ ok が false。
func cutQuoted(s string) (value, rest string, ok bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if i+1 >= len(s) {
				return "", "", false
			}
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				// \\ と \" はその文字そのもの。知らないエスケープも
				// 後ろの1文字をそのまま出す（VDF の書き手に合わせる）。
				b.WriteByte(s[i])
			}
		case '"':
			return b.String(), s[i+1:], true
		default:
			b.WriteByte(s[i])
		}
	}
	return "", "", false
}
