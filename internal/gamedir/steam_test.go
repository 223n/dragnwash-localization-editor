package gamedir

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setHome は [os.UserHomeDir] が返す場所を dir にする。空にすると
// 「ホームを取れない」状態になる。
//
// 見る環境変数は OS で違う（Windows は USERPROFILE、Plan 9 は home）。
func setHome(t *testing.T, dir string) {
	t.Helper()

	switch runtime.GOOS {
	case "windows":
		t.Setenv("USERPROFILE", dir)
	case "plan9":
		t.Setenv("home", dir)
	default:
		t.Setenv("HOME", dir)
	}
}

// noReg は PATH を空のフォルダーだけにして、reg を起動できない状態にする。
func noReg(t *testing.T) {
	t.Helper()

	t.Setenv("PATH", t.TempDir())
}

// fakeReg は PATH を偽の reg だけにする。偽物は output をそのまま標準出力へ
// 書き、code で終わる。
//
// steamPathFromRegistry は reg query の出力を読むだけなので、偽物に決まった
// 出力を返させれば、本物のレジストリにも Steam の有無にも依らずに読み取りの門を
// 確かめられる。PATH を偽物のフォルダーだけにするのは、本物の reg.exe へ
// 抜けないようにするためである。
//
// Windows では reg.bat にして、出力は別のファイルから type で写す。echo で
// 書かせると、非ASCIIのバイト列がコンソールのコードページで変わりうる。
// ほかの OS ではシェルの組み込みの printf に8進で書かせ、外部のコマンドに頼らない。
func fakeReg(t *testing.T, output []byte, code int) {
	t.Helper()

	dir := t.TempDir()
	var name, script string
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(filepath.Join(dir, "reg.out"), output, 0o644); err != nil {
			t.Fatal(err)
		}
		name = "reg.bat"
		script = fmt.Sprintf("@type \"%%~dp0reg.out\"\r\n@exit /b %d\r\n", code)
	} else {
		var escaped strings.Builder
		for _, b := range output {
			fmt.Fprintf(&escaped, "\\%03o", b)
		}
		name = "reg"
		script = fmt.Sprintf("#!/bin/sh\nprintf '%s'\nexit %d\n", escaped.String(), code)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// writeLibraryFolders は root/steamapps/libraryfolders.vdf を、実機と同じ形で書く。
// パスの \ と " は VDF のエスケープにする（実機のファイルも \\ で書かれている）。
func writeLibraryFolders(t *testing.T, root string, libraries ...string) {
	t.Helper()

	var b strings.Builder
	b.WriteString("\"libraryfolders\"\n{\n")
	escape := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	for i, lib := range libraries {
		fmt.Fprintf(&b, "\t\"%d\"\n\t{\n\t\t\"path\"\t\t\"%s\"\n\t\t\"label\"\t\t\"\"\n\t}\n", i, escape.Replace(lib))
	}
	b.WriteString("}\n")

	dir := filepath.Join(root, "steamapps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "libraryfolders.vdf"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseLibraryPaths(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			// 実機の C:/Program Files (x86)/Steam/steamapps/libraryfolders.vdf から
			// 写した形。バックスラッシュが2重になっている。
			name: "実機と同じ形を読める",
			text: "\"libraryfolders\"\n{\n\t\"0\"\n\t{\n" +
				"\t\t\"path\"\t\t\"C:\\\\Program Files (x86)\\\\Steam\"\n" +
				"\t\t\"label\"\t\t\"\"\n\t}\n}\n",
			want: []string{`C:\Program Files (x86)\Steam`},
		},
		{
			name: "ライブラリが複数あれば全部返す",
			text: "\t\t\"path\"\t\t\"C:\\\\Steam\"\n\t\t\"path\"\t\t\"D:\\\\SteamLibrary\"\n",
			want: []string{`C:\Steam`, `D:\SteamLibrary`},
		},
		{
			name: "path 以外の鍵は拾わない",
			text: "\t\t\"label\"\t\t\"D:\\\\nope\"\n\t\t\"contentid\"\t\t\"123\"\n",
			want: nil,
		},
		{
			// apps の中は "<appid>" "<サイズ>" という組で、鍵が path ではない。
			name: "apps の中身を拾わない",
			text: "\t\t\"apps\"\n\t\t{\n\t\t\t\"228980\"\t\t\"25674515\"\n\t\t}\n",
			want: nil,
		},
		{
			name: "Linux 風の区切りもそのまま返す",
			text: "\t\t\"path\"\t\t\"/home/user/.local/share/Steam\"\n",
			want: []string{"/home/user/.local/share/Steam"},
		},
		{
			name: "閉じ引用符が無い行は落とす",
			text: "\t\t\"path\"\t\t\"C:\\\\Steam\n",
			want: nil,
		},
		{
			name: "値が空の行は落とす",
			text: "\t\t\"path\"\t\t\"\"\n",
			want: nil,
		},
		{
			name: "空のファイルでも落ちない",
			text: "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLibraryPaths(tt.text)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("読み取りが違う\ngot  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestParseRegValue(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			// 実機（Windows 11 Pro 26200）の reg query の出力をそのまま写した。
			name: "実機と同じ出力から読める",
			output: "\r\nHKEY_CURRENT_USER\\Software\\Valve\\Steam\r\n" +
				"    SteamPath    REG_SZ    c:/program files (x86)/steam\r\n\r\n",
			want: "c:/program files (x86)/steam",
		},
		{
			name: "値に空白が入っていても切らない",
			output: "HKEY_CURRENT_USER\\Software\\Valve\\Steam\r\n" +
				"    SteamPath    REG_SZ    D:\\Program Files\\Steam\r\n",
			want: `D:\Program Files\Steam`,
		},
		{
			name: "似た名前の値は拾わない",
			output: "    SteamPathOld    REG_SZ    D:\\old\r\n" +
				"    SteamPath    REG_SZ    D:\\new\r\n",
			want: `D:\new`,
		},
		{
			name:   "値が無ければ空",
			output: "HKEY_CURRENT_USER\\Software\\Valve\\Steam\r\n",
			want:   "",
		},
		{
			name:   "型だけで値が無い行は空",
			output: "    SteamPath    REG_SZ\r\n",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRegValue(tt.output, steamRegistryValue); got != tt.want {
				t.Errorf("読み取りが違う: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAsciiOnly(t *testing.T) {
	// reg.exe の出力は子プロセスのコードページで返る。実測でこの開発機は CP932 で、
	// 非ASCIIを含む値は化けたバイト列になる。化けた値を「読めた」ことにしない門。
	tests := []struct {
		name string
		in   []byte
		want bool
	}{
		{
			name: "実機の SteamPath はASCIIなので通す",
			in:   []byte("    SteamPath    REG_SZ    c:/program files (x86)/steam\r\n"),
			want: true,
		},
		{
			// 実測で拾った CP932 のバイト列（「アクティブなペン」）。
			// このうち c8 83 は UTF-8 としても読めてしまうので、
			// utf8.Valid では門にならない。
			name: "CP932 のバイト列は通さない",
			in: []byte{0x44, 0x65, 0x6c, 0x6c, 0x20, 0x83, 0x41, 0x83, 0x4e, 0x83, 0x65,
				0x83, 0x42, 0x83, 0x75, 0x82, 0xc8, 0x83, 0x79, 0x83, 0x93},
			want: false,
		},
		{
			// UTF-8 として正しくても通さない。reg の出力が UTF-8 である保証は
			// どこにも無く、正しいほうかどうかを見分ける手立ても無い。
			name: "UTF-8 の日本語も通さない",
			in:   []byte(`D:\ゲーム\Steam`),
			want: false,
		},
		{name: "空でも落ちない", in: nil, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := asciiOnly(tt.in); got != tt.want {
				t.Errorf("asciiOnly = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCutQuoted(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  string
		rest  string
		valid bool
	}{
		{name: "素の値", in: `abc" x`, want: "abc", rest: " x", valid: true},
		{name: "2重のバックスラッシュを戻す", in: `C:\\Steam"`, want: `C:\Steam`, valid: true},
		{name: "引用符のエスケープを戻す", in: `a\"b"`, want: `a"b`, valid: true},
		{name: "改行のエスケープを戻す", in: `a\nb"`, want: "a\nb", valid: true},
		{name: "タブのエスケープを戻す", in: `a\tb"`, want: "a\tb", valid: true},
		{
			// 知らないエスケープは後ろの1文字をそのまま出す（cutQuoted のコメント。
			// VDF の書き手に合わせてある）。閉じ引用符の判定もずらさない。
			name: "知らないエスケープは後ろの1文字にする", in: `a\qb" x`, want: "aqb", rest: " x", valid: true,
		},
		{name: "閉じ引用符が無い", in: `abc`, valid: false},
		{name: "末尾がバックスラッシュ", in: `abc\`, valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, rest, ok := cutQuoted(tt.in)
			if ok != tt.valid {
				t.Fatalf("ok = %v, want %v", ok, tt.valid)
			}
			if !ok {
				return
			}
			if got != tt.want {
				t.Errorf("値が違う: got %q, want %q", got, tt.want)
			}
			if rest != tt.rest {
				t.Errorf("残りが違う: got %q, want %q", rest, tt.rest)
			}
		})
	}
}

func TestSteamRootsAreAbsolute(t *testing.T) {
	// どの OS でも、返すのは絶対パスだけにする。相対パスが混じると、
	// 実行した場所によって当たるライブラリが変わる。
	for _, root := range steamRoots() {
		if !filepath.IsAbs(root) {
			t.Errorf("相対パスが混じっている: %q", root)
		}
	}
}

func TestWindowsSteamRootsIncludeDefaults(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows でだけ意味がある")
	}
	// レジストリを読めない環境でも、既定の場所は必ず候補に入る。
	got := windowsSteamRoots()
	want := `C:\Program Files (x86)\Steam`
	for _, root := range got {
		if strings.EqualFold(root, want) {
			return
		}
	}
	t.Errorf("既定の場所が候補に無い: %q", got)
}

func TestSteamPathFromRegistry(t *testing.T) {
	// 実機（Windows 11 Pro 26200）の reg query の出力と同じ形。
	const found = "\r\nHKEY_CURRENT_USER\\Software\\Valve\\Steam\r\n" +
		"    SteamPath    REG_SZ    c:/program files (x86)/steam\r\n\r\n"

	tests := []struct {
		name string
		// output が nil なら reg を置かない。
		output []byte
		code   int
		want   string
	}{
		{
			name:   "ASCII の値を読む",
			output: []byte(found),
			want:   "c:/program files (x86)/steam",
		},
		{
			// 「ゲーム」の CP932（83 51 81 5b 83 80）。化けた値を読んだことにすると、
			// 運悪く別の場所に当たったときに、翻訳者は違うゲームのファイルを直す。
			name: "非ASCIIを含む出力は読まない",
			output: []byte("\r\nHKEY_CURRENT_USER\\Software\\Valve\\Steam\r\n" +
				"    SteamPath    REG_SZ    D:/\x83\x51\x81\x5b\x83\x80/Steam\r\n\r\n"),
			want: "",
		},
		{
			// 値が無いとき reg は 1 で終わる。標準出力に何か出ていても信じない。
			name:   "reg が失敗したら読まない",
			output: []byte(found),
			code:   1,
			want:   "",
		},
		{
			// 切り詰めた Windows、権限を絞った端末。候補が1つ減るだけで続ける。
			name:   "reg を起動できなければ空",
			output: nil,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.output == nil {
				noReg(t)
			} else {
				fakeReg(t, tt.output, tt.code)
			}

			if got := steamPathFromRegistry(); got != tt.want {
				t.Errorf("steamPathFromRegistry() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWindowsSteamRootsOrder(t *testing.T) {
	// windowsSteamRoots は OS を見ないので、偽の reg と環境変数でどの OS でも
	// 確かめられる。並びは、レジストリ、環境変数の既定の場所、決め打ちの場所の順。
	// 先に来たものが SteamLibraries で先頭になり、そこから libraryfolders.vdf を辿る。
	fakeReg(t, []byte("    SteamPath    REG_SZ    D:/Games/Steam\r\n"), 0)
	t.Setenv("ProgramFiles(x86)", `E:\Apps (x86)`)
	// 空の環境変数から "Steam" という相対パスを作らない。相対パスが混じると、
	// 実行した場所によって当たるライブラリが変わる。
	t.Setenv("ProgramFiles", "")
	t.Setenv("ProgramW6432", `E:\Apps`)

	want := []string{
		"D:/Games/Steam",
		filepath.Join(`E:\Apps (x86)`, "Steam"),
		filepath.Join(`E:\Apps`, "Steam"),
		`C:\Program Files (x86)\Steam`,
		`C:\Program Files\Steam`,
	}
	got := windowsSteamRoots()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("候補が違う\ngot  %q\nwant %q", got, want)
	}
}

func TestWindowsSteamRootsWithoutRegistry(t *testing.T) {
	// reg を起動できなくても、既定の場所は候補に残る。レジストリだけに頼ると、
	// 切り詰めた Windows で何も出せない（windowsSteamRoots のコメント）。
	noReg(t)
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "ProgramW6432"} {
		t.Setenv(env, "")
	}

	want := []string{`C:\Program Files (x86)\Steam`, `C:\Program Files\Steam`}
	got := windowsSteamRoots()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("候補が違う\ngot  %q\nwant %q", got, want)
	}
}

func TestWithHome(t *testing.T) {
	t.Run("ホームからの相対を絶対パスにする", func(t *testing.T) {
		home := tempDir(t)
		setHome(t, home)

		got := withHome(".steam", "steam")
		want := filepath.Join(home, ".steam", "steam")
		if len(got) != 1 || got[0] != want {
			t.Errorf("withHome = %q, want [%q]", got, want)
		}
	})

	t.Run("ホームを取れなければ何も返さない", func(t *testing.T) {
		if runtime.GOOS == "ios" || runtime.GOOS == "android" {
			t.Skip("ホームが無くても既定の場所を返す OS")
		}
		// 空のホームから作ると ".steam/steam" という相対パスになり、
		// 実行した場所によって当たるライブラリが変わる。
		setHome(t, "")

		if got := withHome(".steam", "steam"); got != nil {
			t.Errorf("ホームが無いのに返した: %q", got)
		}
	})
}

func TestSteamRootsUnderHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows はホームではなくレジストリと Program Files を見る")
	}
	home := tempDir(t)
	setHome(t, home)

	var want []string
	if runtime.GOOS == "darwin" {
		want = []string{filepath.Join(home, "Library", "Application Support", "Steam")}
	} else {
		// 古くからの場所、新しい場所、Flatpak の順。
		want = []string{
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".local", "share", "Steam"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam"),
		}
	}
	got := steamRoots()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("候補が違う\ngot  %q\nwant %q", got, want)
	}
}

func TestLibraryFolders(t *testing.T) {
	t.Run("libraryfolders.vdf のライブラリを読む", func(t *testing.T) {
		root := tempDir(t)
		other := filepath.Join(tempDir(t), "SteamLibrary")
		writeLibraryFolders(t, root, root, other)

		got := libraryFolders(root)
		want := []string{root, other}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("読み取りが違う\ngot  %q\nwant %q", got, want)
		}
	})

	t.Run("ファイルが無ければ何も返さない", func(t *testing.T) {
		// ライブラリを1つしか持っていない古い Steam でも起こる。
		if got := libraryFolders(tempDir(t)); got != nil {
			t.Errorf("何も無いのに返した: %q", got)
		}
	})
}

func TestSteamLibraries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows は決め打ちの C:\\Program Files (x86)\\Steam も見るので、実機の Steam から切り離せない")
	}

	t.Run("Steam の後ろに libraryfolders.vdf のライブラリを並べる", func(t *testing.T) {
		setHome(t, tempDir(t))
		root := steamRoots()[0]
		other := tempDir(t)
		// 外付けドライブを外した、ライブラリを消した、など。
		gone := filepath.Join(tempDir(t), "SteamLibrary")
		// 実機の libraryfolders.vdf は Steam を入れた場所そのものも並べる。
		writeLibraryFolders(t, root, root, other, gone)

		got := SteamLibraries()
		// 実在しないライブラリは落とす（parseLibraryPaths のコメント）。
		want := []string{root, other}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("ライブラリが違う\ngot  %q\nwant %q", got, want)
		}
	})

	t.Run("リンクで同じ場所を指す Steam は1つに畳む", func(t *testing.T) {
		if runtime.GOOS == "darwin" {
			t.Skip("macOS は Steam を入れた場所の候補が1つだけ")
		}
		// Linux の Steam は ~/.steam/steam を ~/.local/share/Steam へのリンクにする。
		// 畳まないと同じライブラリを2回走査し、候補が2つに見える。
		home := tempDir(t)
		setHome(t, home)
		target := filepath.Join(home, ".local", "share", "Steam")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(home, ".steam"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(home, ".steam", "steam")); err != nil {
			t.Fatal(err)
		}

		got := SteamLibraries()
		if len(got) != 1 || got[0] != target {
			t.Errorf("ライブラリが違う\ngot  %q\nwant [%q]", got, target)
		}
	})

	t.Run("Steam が無ければ空", func(t *testing.T) {
		setHome(t, tempDir(t))

		if got := SteamLibraries(); len(got) != 0 {
			t.Errorf("何も無いのに返した: %q", got)
		}
	})
}
