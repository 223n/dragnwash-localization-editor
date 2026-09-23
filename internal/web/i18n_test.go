package web

import (
	"io/fs"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCatalogsLoad(t *testing.T) {
	c, err := loadCatalogs()
	if err != nil {
		t.Fatal(err)
	}
	if c.origin().Lang != originLang {
		t.Errorf("原典が %q", c.origin().Lang)
	}
	if c.fallback().Lang != fallbackLang {
		t.Errorf("受け皿が %q", c.fallback().Lang)
	}
	for _, lang := range c.langs {
		cat := c.byLang[lang]
		if cat.Name == "" {
			t.Errorf("%s に name が無い", lang)
		}
		if cat.Dir != "ltr" && cat.Dir != "rtl" {
			t.Errorf("%s の dir が %q", lang, cat.Dir)
		}
	}
}

// TestCatalogsHaveTheSameKeys は、積んだ目録が原典と同じ鍵を持つことを見る。
//
// 抜けていても T が原典で埋めるので画面は成り立つが、埋まった分は日本語のまま
// 出る。英語の画面に日本語が混ざるのは、抜けたことに誰も気づいていない印である。
func TestCatalogsHaveTheSameKeys(t *testing.T) {
	c, err := loadCatalogs()
	if err != nil {
		t.Fatal(err)
	}
	origin := c.origin()
	for _, lang := range c.langs {
		if lang == originLang {
			continue
		}
		cat := c.byLang[lang]
		var missing, extra []string
		for key := range origin.Messages {
			if _, ok := cat.Messages[key]; !ok {
				missing = append(missing, key)
			}
		}
		for key := range cat.Messages {
			if _, ok := origin.Messages[key]; !ok {
				extra = append(extra, key)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		if len(missing) > 0 {
			t.Errorf("%s.json に無い鍵: %v", lang, missing)
		}
		if len(extra) > 0 {
			t.Errorf("%s.json にしか無い鍵: %v", lang, extra)
		}
	}
}

// TestTranslatedCatalogsHaveNoJapanese は、原典以外の目録に日本語が
// 1文字も残っていないことを見る。
//
// [TestEnglishScreenHasNoJapanese] は待ち受けが組み立てた欄を見るので、応答に
// 載らない文言（畳んだ断り書き、条件のチップに添える数）までは届かない。鍵を
// 足すときに日本語をそのまま貼って訳し忘れるのは、ここでしか見つからない。
func TestTranslatedCatalogsHaveNoJapanese(t *testing.T) {
	c, err := loadCatalogs()
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range c.langs {
		if lang == originLang {
			continue
		}
		for key, text := range c.byLang[lang].Messages {
			if hasJapanese(text) {
				t.Errorf("%s.json の %s に日本語が残っている: %q", lang, key, text)
			}
		}
	}
}

// TestPlaceholdersMatch は、同じ鍵の置換名が言語間でそろっていることを見る。
//
// 片方だけ {count} を落とすと、その言語でだけ数が出ない画面になる。
func TestPlaceholdersMatch(t *testing.T) {
	c, err := loadCatalogs()
	if err != nil {
		t.Fatal(err)
	}
	origin := c.origin()
	for _, lang := range c.langs {
		if lang == originLang {
			continue
		}
		for key, want := range origin.Messages {
			got, ok := c.byLang[lang].Messages[key]
			if !ok {
				continue // 抜けは上の試験が報告する
			}
			if a, b := placeholders(want), placeholders(got); a != b {
				t.Errorf("%s の %s: 置換が %q、原典は %q", lang, key, b, a)
			}
		}
	}
}

// placeholders は文言に出てくる {name} を並べて返す。
func placeholders(text string) string {
	var names []string
	rest := text
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			break
		}
		close := strings.IndexByte(rest[open:], '}')
		if close < 0 {
			break
		}
		names = append(names, rest[open+1:open+close])
		rest = rest[open+close:]
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func TestExpand(t *testing.T) {
	cases := []struct {
		text string
		kv   []string
		want string
	}{
		{"行: {count}", []string{"count", "12"}, "行: 12"},
		{"{a} と {b}", []string{"a", "1", "b", "2"}, "1 と 2"},
		// 対応が無ければそのまま残す。消すと、抜けたことが画面から分からない。
		{"行: {count}", nil, "行: {count}"},
		{"{a}", []string{"a"}, "{a}"},
		// 値に {x} が入っていても2度目の差し替えはしない（Replacer は1周だけ）。
		{"{a}", []string{"a", "{b}", "b", "X"}, "{b}"},
	}
	for _, tc := range cases {
		if got := expand(tc.text, tc.kv...); got != tc.want {
			t.Errorf("expand(%q, %v) = %q、%q を期待", tc.text, tc.kv, got, tc.want)
		}
	}
}

func TestMissingKeyFallsBackToOrigin(t *testing.T) {
	c, err := loadCatalogs()
	if err != nil {
		t.Fatal(err)
	}
	en := c.byLang["en"]
	// 原典にしか無い鍵を作って確かめる。
	c.origin().Messages["test.only_origin"] = "原典の値"
	defer delete(c.origin().Messages, "test.only_origin")

	if got := c.T(en, "test.only_origin"); got != "原典の値" {
		t.Errorf("原典へ落ちていない: %q", got)
	}
	// どこにも無ければ鍵そのもの。空文字にすると、抜けが画面から消えてしまう。
	if got := c.T(en, "test.nowhere"); got != "test.nowhere" {
		t.Errorf("無い鍵が %q", got)
	}
}

func TestParseAcceptLanguage(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"ja", "ja"},
		{"ja-JP,ja;q=0.9,en;q=0.8", "ja-JP,ja,en"},
		{"en;q=0.2,ja;q=0.8", "ja,en"},
		// q が同じなら書かれた順のまま。
		{"de,fr", "de,fr"},
		{"", ""},
		{"*", ""},
		{"ja;q=0", ""},
		{"ja;q=bogus", ""},
		// 空の項目は飛ばす（連続したカンマ、末尾のカンマ）。
		{"ja,,en", "ja,en"},
		{"ja,", "ja"},
		{" , ", ""},
		// q は 0 から 1 まで。1 ちょうどは通し、外れた値の項目は落とす。
		{"ja;q=1", "ja"},
		{"ja;q=1.5,en", "en"},
		{"ja;q=-0.1,en", "en"},
	}
	for _, tc := range cases {
		got := strings.Join(parseAcceptLanguage(tc.header), ",")
		if got != tc.want {
			t.Errorf("parseAcceptLanguage(%q) = %q、%q を期待", tc.header, got, tc.want)
		}
	}
}

func TestLookupFallsBackToPrimarySubtag(t *testing.T) {
	c, err := loadCatalogs()
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"ja", "ja-JP", "JA", "ja-Hira-JP"} {
		if cat := c.lookup(lang); cat == nil || cat.Lang != "ja" {
			t.Errorf("%q が当たらない", lang)
		}
	}
	for _, lang := range []string{"", "de", "zz"} {
		if cat := c.lookup(lang); cat != nil {
			t.Errorf("%q が %q に当たった", lang, cat.Lang)
		}
	}
}

func TestServerMessagesUseOrigin(t *testing.T) {
	// 起動時のメッセージは、指定が無ければ原典（日本語）で出す。
	// このコマンドの使い方の説明は日本語に固定されているので、そこだけ
	// 英語になると1回の実行の出力が2言語に割れる。
	c, err := loadCatalogs()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.forServer("").Lang; got != originLang {
		t.Errorf("指定なしで %q", got)
	}
	if got := c.forServer("en").Lang; got != "en" {
		t.Errorf("--ui-lang en で %q", got)
	}
	if got := c.forServer("de").Lang; got != originLang {
		t.Errorf("知らない指定で %q", got)
	}
}

// TestLabelsAreTranslated は、diff のカテゴリと重さの表示名が目録にそろっている
// ことを見る。
//
// 目録に無ければ internal/diff の日本語名へ落ちるので画面は成り立つが、
// 英語の画面に日本語のバッジが混ざる。diff にカテゴリが増えたときに気づけるよう、
// 実際に組んだ応答の側から確かめる。
func TestLabelsAreTranslated(t *testing.T) {
	s := newTestServer(t, Options{UILang: "en"})
	rec := do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	got := decode[linesResponse](t, rec.Body.Bytes())
	if len(got.Counts) == 0 {
		t.Fatal("件数が空")
	}
	for _, c := range got.Counts {
		if c.Label == "category."+c.Category {
			t.Errorf("category.%s が目録に無い", c.Category)
		}
		if c.StatusLabel == "status."+c.Status {
			t.Errorf("status.%s が目録に無い", c.Status)
		}
		// 英語の画面に日本語名が落ちてきていないか。
		for _, r := range c.Label + c.StatusLabel {
			if r > 0x2000 {
				t.Errorf("英語の目録に日本語が混ざっている: %q / %q", c.Label, c.StatusLabel)
				break
			}
		}
	}
}

// catalogFile は目録1つぶんの中身を作る。試験で壊した目録と並べる、正しい目録。
func catalogFile(lang string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(`{"lang":"` + lang + `","dir":"ltr","name":"` + lang +
		`","messages":{"ui.reload":"x"}}`)}
}

// unreadableFS は name だけを読めない fs.FS。一覧には出るのに読めない目録を作る
// （権限が外れた、ほかのプログラムが掴んでいる、など）。
type unreadableFS struct {
	fstest.MapFS
	name string
}

func (f unreadableFS) Open(name string) (fs.File, error) {
	if name == f.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return f.MapFS.Open(name)
}

// ReadFile も塞ぐ。fstest.MapFS は ReadFile を持っているので、fs.ReadFile は
// Open を通らずにそちらを呼ぶ。
func (f unreadableFS) ReadFile(name string) ([]byte, error) {
	if name == f.name {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrPermission}
	}
	return f.MapFS.ReadFile(name)
}

// TestCatalogsRefuseToStartWhenBroken は、目録が1つでも読めなければ起動を止める
// ことを見る。
//
// 目録は人が手で足す（言語を増やす）。壊れたまま走らせると、画面の文言が鍵のまま
// 出るか、--ui-lang で指した言語と画面の言語が食い違う。どちらも翻訳者には
// 直しようがない。止めるときは、どのファイルが悪いかを誤りに書く。
func TestCatalogsRefuseToStartWhenBroken(t *testing.T) {
	good := func(extra map[string]*fstest.MapFile) fstest.MapFS {
		fsys := fstest.MapFS{
			catalogDir + "/ja.json": catalogFile("ja"),
			catalogDir + "/en.json": catalogFile("en"),
		}
		for name, file := range extra {
			fsys[name] = file
		}
		return fsys
	}
	broken := func(body string) map[string]*fstest.MapFile {
		return map[string]*fstest.MapFile{catalogDir + "/de.json": {Data: []byte(body)}}
	}

	cases := []struct {
		name string
		fsys fs.FS
		want string
	}{
		{"目録の置き場が無い", fstest.MapFS{}, catalogDir},
		{"JSON でない", good(broken(`{"lang":`)), "de.json"},
		{"lang が無い", good(broken(`{"dir":"ltr","messages":{}}`)), "de.json"},
		{"lang とファイル名が合わない", good(broken(`{"lang":"fr","dir":"ltr","messages":{}}`)), "de.json"},
		{"dir が ltr でも rtl でもない", good(broken(`{"lang":"de","dir":"ttb","messages":{}}`)), "de.json"},
		{"dir が無い", good(broken(`{"lang":"de","messages":{}}`)), "de.json"},
		{"原典が無い", fstest.MapFS{catalogDir + "/en.json": catalogFile("en")}, originLang + ".json"},
		{"受け皿が無い", fstest.MapFS{catalogDir + "/ja.json": catalogFile("ja")}, fallbackLang + ".json"},
		{"一覧にあるのに読めない", unreadableFS{good(broken(`{}`)), catalogDir + "/de.json"}, "de.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadCatalogsFrom(tc.fsys)
			if err == nil {
				t.Fatal("壊れた目録で起動してしまう")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("誤りに %q が無い: %v", tc.want, err)
			}
		})
	}
}

// TestCatalogsSkipWhatIsNotACatalog は、目録の置き場にある .json 以外のものを
// 読まないことを見る。
//
// 置き場に説明のファイルや古い目録を退避したディレクトリがあっても、それを
// 目録として読んで起動を止めたりはしない。
func TestCatalogsSkipWhatIsNotACatalog(t *testing.T) {
	c, err := loadCatalogsFrom(fstest.MapFS{
		catalogDir + "/ja.json":     catalogFile("ja"),
		catalogDir + "/en.json":     catalogFile("en"),
		catalogDir + "/README.md":   {Data: []byte("# 目録の書き方")},
		catalogDir + "/old/de.json": {Data: []byte(`{"lang":`)},
	})
	if err != nil {
		t.Fatalf("目録でないものまで読んで止まった: %v", err)
	}
	if want := []string{"en", "ja"}; !slices.Equal(c.langs, want) {
		t.Errorf("読んだ言語が %v、%v を期待", c.langs, want)
	}
}

// TestLookupFindsARegionalCatalog は、言語だけの指定で地域つきの目録に当たることを
// 見る。
//
// 目録が "pt-BR" しか無いとき、Accept-Language の "pt" や --ui-lang pt で英語へ
// 落とすと、読める言語があるのに読めない画面を出すことになる。言語タグは
// 大文字小文字を区別しない（目録は小文字で持つ）。
func TestLookupFindsARegionalCatalog(t *testing.T) {
	c, err := loadCatalogsFrom(fstest.MapFS{
		catalogDir + "/ja.json":    catalogFile("ja"),
		catalogDir + "/en.json":    catalogFile("en"),
		catalogDir + "/pt-BR.json": catalogFile("pt-BR"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"pt", "PT", " pt ", "pt-BR", "pt-br"} {
		if cat := c.lookup(lang); cat == nil || cat.Lang != "pt-BR" {
			t.Errorf("%q が pt-BR に当たらない: %+v", lang, cat)
		}
	}
	// 選び方の全体（--ui-lang > Accept-Language > en）を通しても同じ。
	if got := c.forRequest("", "pt;q=0.9,de").Lang; got != "pt-BR" {
		t.Errorf("Accept-Language の pt が %q になった", got)
	}
}
