package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// originLang は原典の言語タグ。ここに無い鍵はどの言語にも無い。
//
// 原典を1つ決めておくのは、鍵の集合を1か所で持つためである。積む側（en.json）に
// 抜けがあっても、原典の値で埋まれば画面は成り立つ。逆向きの穴埋めはしない。
const originLang = "ja"

// fallbackLang は何も当たらなかったときの言語タグ。
//
// ブラウザー側の選び方は「--ui-lang > Accept-Language の照合 > en」なので、
// 最後の受け皿は en になる。
const fallbackLang = "en"

// catalogDir は目録の置き場（[uiFS] の中）。
const catalogDir = "ui/i18n"

// Catalog は1言語ぶんの目録。
//
// 文言をコードから追い出してここへ置く。Go 側（起動時のメッセージ、誤りの文面）と
// ブラウザー側が同じ JSON を読む。2か所に別々の文言表を持つと、同じことを
// 違う言い回しで言う画面になる。
type Catalog struct {
	// Lang は言語タグ（BCP 47）。ファイル名と一致する。
	Lang string `json:"lang"`
	// Dir は組む向き。"ltr" か "rtl"。
	//
	// 目録に持たせてあるのは、UI 自体を右から左に組む言語（アラビア語など）が
	// 来たときに、骨組みを直さずに済ませるため。画面はこの値を <html dir> に写す。
	Dir string `json:"dir"`
	// Name はその言語での言語名。選ぶ画面に出す。
	Name string `json:"name"`
	// Messages は鍵から文言への対応。鍵は ASCII の識別子で、値だけを差し替える。
	Messages map[string]string `json:"messages"`
}

// catalogs は読み込み済みの目録。言語タグは小文字で持つ。
type catalogs struct {
	byLang map[string]*Catalog
	// langs は言語タグを並べたもの（並びは固定）。
	langs []string
}

// loadCatalogs は [uiFS] の目録を全部読む。
//
// 起動時に1回だけ呼ぶ。読めない目録があれば起動を止める。画面に出る文言の
// 半分が鍵のまま出るような状態で走らせても、翻訳者には直しようがない。
func loadCatalogs() (*catalogs, error) {
	return loadCatalogsFrom(uiFS)
}

// loadCatalogsFrom は fsys の [catalogDir] にある目録を全部読む。
//
// 読む先を引数にしてあるのは、試験で壊れた目録を渡すためである。埋め込みは
// go build の時点で固まるので、[uiFS] のままでは止める側の道を1度も通せない。
func loadCatalogsFrom(fsys fs.FS) (*catalogs, error) {
	entries, err := fs.ReadDir(fsys, catalogDir)
	if err != nil {
		return nil, fmt.Errorf("%s を読めません: %w", catalogDir, err)
	}
	c := &catalogs{byLang: make(map[string]*Catalog, len(entries))}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(catalogDir, name))
		if err != nil {
			return nil, fmt.Errorf("%s を読めません: %w", name, err)
		}
		var cat Catalog
		if err := json.Unmarshal(data, &cat); err != nil {
			return nil, fmt.Errorf("%s を解釈できません: %w", name, err)
		}
		if cat.Lang == "" {
			return nil, fmt.Errorf("%s に lang がありません", name)
		}
		if strings.TrimSuffix(name, ".json") != cat.Lang {
			// ファイル名と中身がずれていると、--ui-lang で指す名前と
			// 画面に出る言語が食い違う。起動時に止める。
			return nil, fmt.Errorf("%s の lang が %q でファイル名と合いません", name, cat.Lang)
		}
		if cat.Dir != "ltr" && cat.Dir != "rtl" {
			return nil, fmt.Errorf("%s の dir が %q です（ltr か rtl）", name, cat.Dir)
		}
		c.byLang[strings.ToLower(cat.Lang)] = &cat
	}
	if _, ok := c.byLang[originLang]; !ok {
		return nil, fmt.Errorf("原典の目録 %s.json がありません", originLang)
	}
	if _, ok := c.byLang[fallbackLang]; !ok {
		return nil, fmt.Errorf("受け皿の目録 %s.json がありません", fallbackLang)
	}
	for lang := range c.byLang {
		c.langs = append(c.langs, lang)
	}
	sort.Strings(c.langs)
	return c, nil
}

// lookup は言語タグに当たる目録を返す。当たらなければ nil。
//
// 照合は小文字にしてから完全一致を見て、外れたら先頭の主部分（"ja-JP" の "ja"）で
// 見る。既にある目録から選ぶだけなので、緩めても行き先は増えない。
func (c *catalogs) lookup(lang string) *Catalog {
	if lang == "" {
		return nil
	}
	want := strings.ToLower(strings.TrimSpace(lang))
	if cat, ok := c.byLang[want]; ok {
		return cat
	}
	if i := strings.IndexByte(want, '-'); i > 0 {
		if cat, ok := c.byLang[want[:i]]; ok {
			return cat
		}
	}
	// "ja" の指定に対して "ja-JP" しか無い、という向きも拾う。
	for _, lang := range c.langs {
		if i := strings.IndexByte(lang, '-'); i > 0 && lang[:i] == want {
			return c.byLang[lang]
		}
	}
	return nil
}

// CheckUILang は、--ui-lang に渡された tag が目録のどれかに当たるかを確かめる。
// 空なら何もしない（画面が Accept-Language で選ぶ）。
//
// 当たらなければ、渡せる言語タグを並べた誤りを返す。当たらない値を黙って原典の
// ja に落とすと（[catalogs.forServer]）、英語を選んだつもりで eng と打った人に
// 読めない日本語の案内が出る。画面は Accept-Language で選ぶので、黒い窓と画面で
// 言語が割れる。落とし先の向きは変えずに、入口で断る。
//
// 照合は画面と同じ（[catalogs.lookup]）で、大文字と小文字、地域の付いた形
// （en-US）も当たりにする。
func CheckUILang(tag string) error {
	if tag == "" {
		return nil
	}
	c, err := loadCatalogs()
	if err != nil {
		return err
	}
	if c.lookup(tag) != nil {
		return nil
	}
	// 原典を先に並べる。案内の文は「ja か en」の順で読ませたい。
	langs := []string{originLang}
	for _, lang := range c.langs {
		if lang != originLang {
			langs = append(langs, lang)
		}
	}
	return fmt.Errorf("--ui-lang は %s です: %s", strings.Join(langs, " か "), tag)
}

// fallback は受け皿の目録を返す。
func (c *catalogs) fallback() *Catalog { return c.byLang[fallbackLang] }

// origin は原典の目録を返す。
func (c *catalogs) origin() *Catalog { return c.byLang[originLang] }

// forRequest はブラウザーへ返す目録を選ぶ。
//
// 選び方は「--ui-lang の指定 > Accept-Language の照合 > en」。uiLang が当たれば
// Accept-Language は見ない。翻訳者が明示した指定を、ブラウザーの設定で
// 上書きされては指定した意味が無い。
func (c *catalogs) forRequest(uiLang, acceptLanguage string) *Catalog {
	if cat := c.lookup(uiLang); cat != nil {
		return cat
	}
	for _, lang := range parseAcceptLanguage(acceptLanguage) {
		if cat := c.lookup(lang); cat != nil {
			return cat
		}
	}
	return c.fallback()
}

// forServer は Go 側（起動時のメッセージ、誤りの文面）が使う目録を選ぶ。
//
// [catalogs.forRequest] と違い、原典へ落とす。Accept-Language に当たるものが
// 無い場面だからである。起動時のメッセージに要求は無く、このコマンドの
// 使い方の説明（cmd/dwloc）はプロジェクトの方針で日本語に固定されている。
// そこだけ英語になると、同じ実行の出力が2言語に割れる。
func (c *catalogs) forServer(uiLang string) *Catalog {
	if cat := c.lookup(uiLang); cat != nil {
		return cat
	}
	return c.origin()
}

// acceptLanguageItem は Accept-Language の1項目。
type acceptLanguageItem struct {
	lang string
	q    float64
}

// parseAcceptLanguage は Accept-Language を q の大きい順の言語タグにする。
//
// 厳密な RFC 9110 の実装ではない。既にある目録から選ぶだけなので、解釈に
// 失敗した項目は落とせばよく、落としても行き先が増えることはない。
// "*" は落とす。何にでも当たる指定を通すと、目録の並び順で言語が決まってしまう。
func parseAcceptLanguage(header string) []string {
	if strings.TrimSpace(header) == "" {
		return nil
	}
	var items []acceptLanguageItem
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lang, rest, _ := strings.Cut(part, ";")
		lang = strings.TrimSpace(lang)
		if lang == "" || lang == "*" {
			continue
		}
		q := 1.0
		if key, value, ok := strings.Cut(rest, "="); ok && strings.TrimSpace(key) == "q" {
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || parsed < 0 || parsed > 1 {
				continue
			}
			q = parsed
		}
		if q == 0 {
			// q=0 は「要らない」という指定。当たりにしない。
			continue
		}
		items = append(items, acceptLanguageItem{lang: lang, q: q})
	}
	// q が同じ項目は書かれた順のまま残す。安定ソートにしてあるのはそのため。
	sort.SliceStable(items, func(i, j int) bool { return items[i].q > items[j].q })

	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.lang)
	}
	return out
}

// T は鍵に対応する文言を返す。
//
// 置換は {name} の1形式だけにする。複数形の規則は持ち込まない。「行: 12」のように
// 数を添える言い回しへそろえてあるので、言語ごとに単数形と複数形を分ける必要が無い。
// 規則を持ち込むと、アラビア語のように6つの形を持つ言語で目録の形が変わる。
//
// kv は鍵と値を交互に並べる。奇数個のときは最後の1つを落とす。
//
// 鍵が無いときは原典の値を使い、原典にも無ければ鍵そのものを返す。空文字を返すと
// 画面から文言が消えるだけで、どこが抜けているのか分からなくなる。
func (c *catalogs) T(cat *Catalog, key string, kv ...string) string {
	text, ok := "", false
	if cat != nil {
		text, ok = cat.Messages[key]
	}
	if !ok || text == "" {
		if origin := c.origin(); origin != nil {
			text, ok = origin.Messages[key]
		}
	}
	if !ok || text == "" {
		return key
	}
	return expand(text, kv...)
}

// expand は {name} を差し替える。
func expand(text string, kv ...string) string {
	if len(kv) < 2 {
		return text
	}
	pairs := make([]string, 0, len(kv))
	for i := 0; i+1 < len(kv); i += 2 {
		pairs = append(pairs, "{"+kv[i]+"}", kv[i+1])
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

// itoa は置換に渡す数を文字列にする。呼び出し側を短くするためだけの薄い包み。
func itoa(n int) string { return strconv.Itoa(n) }
