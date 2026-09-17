package gamedir

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
