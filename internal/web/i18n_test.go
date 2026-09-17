package web

import (
	"net/http"
	"sort"
	"strings"
	"testing"
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
