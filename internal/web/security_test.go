package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestNoOutboundHTTP は、このパッケージが外向きの通信を持たないことを字面で見張る。
//
// 振る舞いの試験にできない種類の約束である。「外へ出さない」ことは、出す経路が
// 1つも無いことでしか示せない。走らせて確かめようとすると、その1回の実行で
// 通らなかっただけかもしれない。
//
// 落ちたときに直すべきなのは、たいていこの試験ではなく足された経路のほうである。
func TestNoOutboundHTTP(t *testing.T) {
	// 外向きに使える道具の名前。要求を組み立てる側の入口を並べる。
	//
	// 字面ではなく構文木で見るのは、注記や試験の中の名前を数えないため。
	// 「注記に書いてあるから落ちる」試験は、いずれ注記のほうを削って通される。
	forbidden := map[string]map[string]bool{
		"http": {
			"Client": true, "DefaultClient": true, "DefaultTransport": true,
			"Get": true, "Head": true, "Post": true, "PostForm": true,
			"NewRequest": true, "NewRequestWithContext": true, "Transport": true,
		},
		"net":  {"Dial": true, "DialTimeout": true},
		"exec": {
			// ブラウザーを開く1か所（browser.go）だけが例外。ここでは名前を
			// 数えず、下の TestOnlyBrowserRunsCommands で置き場所を見張る。
		},
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				if forbidden[ident.Name][sel.Sel.Name] {
					t.Errorf("%s が %s.%s を使っている。このパッケージは外へ通信しない",
						filepath.Base(name), ident.Name, sel.Sel.Name)
				}
				return true
			})
		}
	}
}

// TestOnlyBrowserRunsCommands は、外のプログラムを起動するのが browser.go だけで
// あることを見る。
//
// 起動そのものは要る（既定のブラウザーを開く）が、置き場所を1つに縛っておくと、
// 「どこで何を起動しているか」を1ファイル読むだけで確かめられる。
func TestOnlyBrowserRunsCommands(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") || name == "browser.go" {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), `"os/exec"`) {
			t.Errorf("%s が os/exec を取り込んでいる。起動は browser.go だけにする", name)
		}
	}
}

// TestAssetsHaveNoExternalReference は、埋め込んだ資産が外を参照しないことを見張る。
//
// CDN のスクリプトも、外部のフォントも、追跡用の画像も置かない。1つでもあると、
// 画面を開いた瞬間に翻訳者の PC から外へ通信が出る。CSP でも塞いであるが、
// 塞いだうえで書かないことにしておく（CSP を緩めた人が同時に気づけるように）。
func TestAssetsHaveNoExternalReference(t *testing.T) {
	forbidden := []string{"http://", "https://", "//cdn", "@import", "fonts.googleapis", "data:text/html"}
	err := fs.WalkDir(uiFS, "ui", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		body, err := fs.ReadFile(uiFS, p)
		if err != nil {
			return err
		}
		for _, bad := range forbidden {
			if strings.Contains(string(body), bad) {
				t.Errorf("%s に %q がある。外部の資産は参照しない", p, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestPageHasNoInlineScriptOrStyle は、CSP と頁の書き方が食い違っていないことを見る。
//
// script-src 'self' / style-src 'self' には 'unsafe-inline' が無いので、行内の
// <script> も style 属性も動かない。書いてしまうと、画面の一部が黙って
// 効かなくなる（ブラウザーのコンソールにしか出ない）。
func TestPageHasNoInlineScriptOrStyle(t *testing.T) {
	body, err := fs.ReadFile(uiFS, "ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	if strings.Contains(html, "<script>") {
		t.Error("index.html に行内スクリプトがある")
	}
	if strings.Contains(html, " style=") {
		t.Error("index.html に style 属性がある")
	}
	if strings.Contains(html, "<form") {
		t.Error("index.html に form がある（CSP の form-action 'none' と食い違う）")
	}
}

// TestTitleHasNoRowContent は、題名に行の中身が入らないことを見る。
//
// 題名はブラウザーの履歴に残る。履歴は待ち受けが終わったあとも残るので、
// ここへ原文が入ると、他のどの守りも効かないまま台本が残る。
func TestTitleHasNoRowContent(t *testing.T) {
	body, err := fs.ReadFile(uiFS, "ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	start := strings.Index(html, "<title>")
	end := strings.Index(html, "</title>")
	if start < 0 || end < start {
		t.Fatal("title が無い")
	}
	if got := html[start+len("<title>") : end]; got != "dwloc" {
		t.Errorf("title が %q。固定の名前だけにする", got)
	}
	// app.js が題名に入れてよいのは目録の文言だけ。行の値を混ぜていないか見る。
	js, err := fs.ReadFile(uiFS, "ui/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), `document.title = t("app.title")`) {
		t.Error("app.js が題名に目録以外を入れている可能性がある")
	}
}

// TestUIDoesNotCount は、画面側が自分で数えていないことを見る。
//
// 件数と状態は internal/diff から来たものだけを描く。画面が数え始めると、
// diff/doc.go が名指しで警告している誤検出を作り直すことになる。
//
// 字面の試験なので万全ではない。数え方を足したときに、この試験の存在が
// 「そこは足す場所ではない」と伝われば十分である。
func TestUIDoesNotCount(t *testing.T) {
	body, err := fs.ReadFile(uiFS, "ui/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(body)
	for _, bad := range []string{".filter(", ".reduce(", "badges.length", "counts.length", "+= 1", "++"} {
		if strings.Contains(js, bad) {
			t.Errorf("app.js に %q がある。件数と状態は待ち受けが渡したものだけを描く", bad)
		}
	}
}

// TestCatalogsAreEmbedded は、目録が実行ファイルに入っていることを見る。
func TestCatalogsAreEmbedded(t *testing.T) {
	for _, name := range []string{"ui/index.html", "ui/app.css", "ui/app.js",
		path.Join(catalogDir, "ja.json"), path.Join(catalogDir, "en.json")} {
		if _, err := fs.ReadFile(uiFS, name); err != nil {
			t.Errorf("%s が埋め込まれていない: %v", name, err)
		}
	}
	// ディスク上のファイルと埋め込みが食い違っていないことも見ておく。
	// 埋め込みは go build の時点で固まるので、編集したのに反映されていない、
	// という取り違えがいちばん起きやすい。
	onDisk, err := os.ReadFile(filepath.FromSlash("ui/index.html"))
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := fs.ReadFile(uiFS, "ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(embedded) {
		t.Error("ui/index.html の中身が埋め込みと違う")
	}
}

// TestEditorInputBlocksOutsideHelp は、入力欄がブラウザーの「お節介」を全部
// 切っていることを見る。
//
// 綴り検査は、内蔵翻訳と同じく入力の中身を外部のサービスへ送りうる経路である。
// CSP はこの頁が出す通信しか塞げないので、ブラウザー自身が出す通信は属性で止める。
// autocorrect / autocapitalize / autocomplete は、それに加えて訳を勝手に
// 書き換えさせないために切る。
//
// 字面の試験である。入力欄を作り直したときに、この試験の存在が
// 「その4つは必ず付ける」と伝わればよい。
func TestEditorInputBlocksOutsideHelp(t *testing.T) {
	body, err := fs.ReadFile(uiFS, "ui/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(body)
	for _, want := range []string{
		`setAttribute("spellcheck", "false")`,
		`setAttribute("autocorrect", "off")`,
		`setAttribute("autocapitalize", "off")`,
		`setAttribute("autocomplete", "off")`,
		`setAttribute("translate", "no")`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js に %s が無い。入力欄は外へ出しうる経路を全部切る", want)
		}
	}
}

// TestEditorDoesNotStealEnterWhileComposing は、変換中に Enter を横取りして
// いないことを見る。
//
// 横取りすると ja / ko / zh-Hans / zh-Hant で変換を確定できなくなる。
// この4言語のために入力欄の方式（1行だけ差し替える input）を選んでいる。
func TestEditorDoesNotStealEnterWhileComposing(t *testing.T) {
	body, err := fs.ReadFile(uiFS, "ui/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(body)
	for _, want := range []string{"compositionstart", "compositionend", "e.isComposing"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js に %s が無い。変換中は Enter を横取りしない", want)
		}
	}
}

// TestPageWarnsBeforeLosingEdits は、未保存のまま閉じさせない仕掛けがあることを見る。
func TestPageWarnsBeforeLosingEdits(t *testing.T) {
	body, err := fs.ReadFile(uiFS, "ui/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(body)
	for _, want := range []string{"beforeunload", "e.returnValue"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js に %s が無い。未保存のまま閉じさせない", want)
		}
	}
}

// TestAssetsHaveNoControlBytes は、埋め込んだ資産に制御文字が紛れていないことを見る。
//
// 実際に起きた事故の見張りである。app.js に書いた "\u0000"（6文字のエスケープ）が、
// 書き出しの途中で生の NUL 1バイトになっていた。文法としては通ってしまうので、
// 動かして気づくのは難しい。
func TestAssetsHaveNoControlBytes(t *testing.T) {
	err := fs.WalkDir(uiFS, "ui", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		body, err := fs.ReadFile(uiFS, p)
		if err != nil {
			return err
		}
		if !utf8.Valid(body) {
			t.Errorf("%s が正しいUTF-8ではない", p)
		}
		for i, b := range body {
			switch b {
			case '\t', '\n', '\r':
				continue
			}
			if b < 0x20 || b == 0x7f {
				t.Fatalf("%s の %d バイト目に制御文字 %#x がある", p, i, b)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
