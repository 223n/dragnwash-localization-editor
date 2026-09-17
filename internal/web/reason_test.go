package web

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/edit"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// hasJapanese は、ひらがな・カタカナ・漢字・全角記号が1文字でも入っているかを返す。
//
// 英語の画面に日本語が残っていないことを数えるための判定である。行の中身（訳）は
// 日本語でよいので、そこを除くのは呼び出し側の役目（どの欄を見るかで分ける）。
func hasJapanese(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0x3040 && r <= 0x309F: // ひらがな
			return true
		case r >= 0x30A0 && r <= 0x30FF: // カタカナ
			return true
		case r >= 0x4E00 && r <= 0x9FFF: // CJK統合漢字
			return true
		case r >= 0x3400 && r <= 0x4DBF: // CJK拡張A
			return true
		case r >= 0xFF01 && r <= 0xFF60: // 全角記号（「（」「）」「。」など）
			return true
		case r >= 0x3000 && r <= 0x303F: // 句読点・かぎ括弧
			return true
		}
	}
	return false
}

// TestReasonKeysAreInCatalogs は、[reason.All] の識別子が全部の目録にあることを見る。
//
// 抜けていても画面は成り立つ（元の日本語へ落ちる）が、英語の画面にそこだけ
// 日本語が出る。識別子を足して目録へ足し忘れたことに、人の目ではなくここで
// 気づけるようにする。
func TestReasonKeysAreInCatalogs(t *testing.T) {
	c, err := loadCatalogs()
	if err != nil {
		t.Fatal(err)
	}
	ids := reason.All()
	if len(ids) == 0 {
		t.Fatal("識別子が1つも無い")
	}
	for _, lang := range c.langs {
		cat := c.byLang[lang]
		for _, id := range ids {
			if _, ok := cat.Messages["reason."+id]; !ok {
				t.Errorf("%s.json に reason.%s が無い", lang, id)
			}
		}
	}
}

// TestReasonTextFallsBackToTheOriginalText は、目録に鍵が無いときに元の文面へ
// 落ちることを見る。
//
// 落ちること自体が約束である。訳されていない文が出るほうが、何も出ないより
// よい。鍵そのもの（"reason.xxx"）を画面に出すことだけは避ける。
func TestReasonTextFallsBackToTheOriginalText(t *testing.T) {
	s := newTestServer(t, Options{UILang: "en"})
	en := s.cat.lookup("en")

	cases := []struct {
		name string
		why  reason.Reason
		want string
	}{
		{"目録に無い識別子", reason.New("no_such_reason_id", "しかたなく日本語"), "しかたなく日本語"},
		{"識別子を持たない理由", reason.New("", "名前の無い理由"), "名前の無い理由"},
		{"空の理由", reason.Reason{}, ""},
	}
	for _, tc := range cases {
		if got := s.reasonText(en, tc.why); got != tc.want {
			t.Errorf("%s: %q、%q を期待", tc.name, got, tc.want)
		}
	}
}

// TestReasonTextUsesTheCatalog は、識別子があれば目録の文面が出て、置換も
// 効くことを見る。
func TestReasonTextUsesTheCatalog(t *testing.T) {
	s := newTestServer(t, Options{UILang: "en"})
	en := s.cat.lookup("en")

	why := reason.New(reason.EditFieldCount,
		"フィールド数がヘッダーと合わない（ヘッダーは7列、この行は8列）",
		"header", "7", "row", "8")
	got := s.reasonText(en, why)
	if hasJapanese(got) {
		t.Fatalf("英語の目録から日本語が出た: %q", got)
	}
	if !strings.Contains(got, "7") || !strings.Contains(got, "8") {
		t.Errorf("置換が効いていない: %q", got)
	}
}

// TestJapaneseCatalogMatchesTheSourceText は、ja の目録が internal/diff と
// internal/edit の文面と1字も違わないことを見る。
//
// 同じ日本語が2か所にある（そうした理由は internal/reason の doc コメント）。
// 写しはいずれずれる。ずれると、CLI と ja の画面が同じことを違う言い回しで言う。
// 置換まで含めて突き合わせるので、{name} の付け方を間違えたときもここで落ちる。
//
// 見本を手で組み立てず、実際に判定を走らせて出てきた理由を見るのは、手で組むと
// Go 側の文面を書き写すことになり、ずれを見つけられなくなるからである。
func TestJapaneseCatalogMatchesTheSourceText(t *testing.T) {
	s := newTestServer(t, Options{UILang: "ja"})
	ja := s.cat.lookup("ja")

	seen := make(map[string]struct{})
	check := func(where string, why reason.Reason) {
		t.Helper()
		if why.Empty() {
			return
		}
		if why.ID == "" {
			t.Errorf("%s: 識別子を持たない理由: %q", where, why.Text)
			return
		}
		seen[why.ID] = struct{}{}
		if got := s.reasonText(ja, why); got != why.Text {
			t.Errorf("%s (%s): 目録は %q、元の文面は %q", where, why.ID, got, why.Text)
		}
	}

	for _, why := range diffReasons(t) {
		check("diff", why)
	}
	for _, why := range editReasons(t) {
		check("edit", why)
	}

	// 見本が痩せていないことを確かめる。[reason.All] の全部を通したい。
	for _, id := range reason.All() {
		if _, ok := seen[id]; !ok {
			t.Errorf("reason.%s を1度も通していない", id)
		}
	}
}

// diffReasons は internal/diff が作る理由の見本を集める。
func diffReasons(t *testing.T) []reason.Reason {
	t.Helper()
	var out []reason.Reason

	root := newReasonRoot(t)
	repo, err := diff.LoadWith(root, diff.Options{
		Working:  true,
		OldOrder: func(string, string) ([]byte, error) { return oldReasonOrder(), nil },
	})
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	report := diff.Compare(repo, nil)
	for _, f := range report.Findings {
		out = append(out, f.NoteReason)
	}

	// 旧再生順を取り出せない理由。git を呼ばずに、番兵をそのまま返させる。
	// ここを通すと [diff.LoadWith] の当てはめ（oldOrderReasonID）まで確かめられる。
	fail := func(err error) diff.OldOrderSource {
		return func(string, string) ([]byte, error) { return nil, err }
	}
	sources := []diff.OldOrderSource{
		fail(diff.ErrNoGit), fail(diff.ErrNoRepository), fail(diff.ErrNotTracked),
		fail(diff.ErrOnlyOneVersion), fail(diff.ErrGitFailed),
		// 取り出せたが再生順として読めない（列名が重複している）。
		func(string, string) ([]byte, error) { return []byte("key,key\na,b\n"), nil },
		// 読めたが台詞IDとキーの組が無い。
		func(string, string) ([]byte, error) { return []byte("section,key\nUI,\n"), nil },
	}
	for _, src := range sources {
		r, err := diff.LoadWith(root, diff.Options{Working: true, OldOrder: src})
		if err != nil {
			t.Fatalf("LoadWith: %v", err)
		}
		out = append(out, reason.New(r.OldOrderReasonID, r.OldOrderReason))
	}

	// 判定を止める理由は、そろえた入力だけでは全部は出ない。残りは
	// [diff.Summary] を直に組んで出させる。Summary の欄はどれも公開されている。
	base := diff.Summary{Locale: "ja", Counts: map[diff.Category]int{}}
	notRead := base
	notRead.WorkingExists = true
	out = append(out, notRead.JudgeBlockReason(diff.CatUntranslated))
	out = append(out, base.JudgeBlockReason(diff.CatUntranslated))
	out = append(out, base.JudgeBlockReason(diff.CatVanished))

	ready := base
	ready.OrderKeys, ready.OrderLineIDs, ready.HasWorking = true, true, true
	out = append(out, ready.JudgeBlockReason(diff.CatCarryover))

	// 旧版が更新後の内容に見えるとき。[diff.Compare] が立てる印を写して作る。
	stale := newStaleSummary(t, root)
	out = append(out, stale.JudgeBlockReason(diff.CatCarryover))

	return out
}

// newStaleSummary は「読めた旧版が、いまの版と同じ内容に見える」要約を作る。
//
// いまの再生順をそのまま旧再生順として渡すと、キーの変化が1行も見つからない。
// 台本から消えた行はあるので、[diff.Compare] は旧版を疑って印を立てる。
func newStaleSummary(t *testing.T, root string) diff.Summary {
	t.Helper()
	now, err := os.ReadFile(filepath.Join(root, "data", "script_order.csv"))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := diff.LoadWith(root, diff.Options{
		Working:  true,
		OldOrder: func(string, string) ([]byte, error) { return now, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, sum := range diff.Compare(repo, nil).Locales {
		if sum.OldOrderStale {
			return sum
		}
	}
	t.Fatal("旧版を疑う印が立たなかった")
	return diff.Summary{}
}

// editReasons は internal/edit が作る理由の見本を集める。
func editReasons(t *testing.T) []reason.Reason {
	t.Helper()
	var out []reason.Reason

	// ヘッダー行が無いファイルと、受理されないヘッダーのファイル。
	out = append(out, edit.Parse([]byte("# comment only\n")).ReadOnlyCause())
	out = append(out, edit.Parse([]byte("a,b,c\n1,2,3\n")).ReadOnlyCause())

	// 列数がヘッダーと合わない行。
	wide := edit.Parse([]byte("key,translation\nk,v,extra\n"))
	if line, ok := wide.Line(2); ok {
		out = append(out, line.Cause)
	} else {
		t.Fatal("2行目が無い")
	}

	// 訳を消すとレコードでなくなる行（キーも空の2列）。
	blankable := edit.Parse([]byte("key,translation\n,v\n"))
	if err := blankable.SetTranslation(2, ""); err != nil {
		t.Fatalf("空にできない: %v", err)
	}
	if line, ok := blankable.Line(2); ok {
		out = append(out, line.Cause)
	} else {
		t.Fatal("2行目が無い")
	}

	// 書き換えを断る経路。誤りの型から理由を取り出す。
	f := edit.Parse([]byte("key,translation\n# 見出し\nk,v\n"))
	refuse := []struct {
		line  int
		value string
	}{
		{0, "x"},      // そんな行番号は無い
		{2, "x"},      // データ行ではない
		{3, "a\nb"},   // 訳に改行
		{3, "a\x00b"}, // 訳に NUL
		{3, "a\xffb"}, // 不正なUTF-8
	}
	for _, tc := range refuse {
		err := f.SetTranslation(tc.line, tc.value)
		if err == nil {
			t.Fatalf("%d行目 %q が通ってしまった", tc.line, tc.value)
		}
		out = append(out, causeOf(t, err))
	}

	// 列数の合わない行への書き換え。理由は行が持っているものがそのまま出る。
	if err := wide.SetTranslation(2, "x"); err == nil {
		t.Error("列数の合わない行が書けてしまった")
	} else {
		out = append(out, causeOf(t, err))
	}
	return out
}

// causeOf は internal/edit の誤りから理由を取り出す。
func causeOf(t *testing.T, err error) reason.Reason {
	t.Helper()
	var notEditable *edit.NotEditableError
	if errors.As(err, &notEditable) {
		return notEditable.Cause
	}
	var invalid *edit.InvalidValueError
	if errors.As(err, &invalid) {
		return invalid.Cause
	}
	t.Fatalf("知らない誤り: %T %v", err, err)
	return reason.Reason{}
}

// 理由の見本を作るための原文。キーはこの原文から計算する。
const (
	// カンマを入れないのは、作業コピーの source_en 列へそのまま書くため。
	// 引用すればよいが、見本の CSV に引用の規則を持ち込まないほうが読める。
	srcAOld = "A the old line."
	srcANew = "A the new line."
	srcBOld = "B the old line."
	srcBNew = "B the new line."
	srcCOld = "C the old line."
	srcCNew = "C the new line."
)

// newReasonRoot は、9カテゴリの注記がひととおり出る翻訳リポジトリを作る。
//
// 台詞を3つ用意して、3つとも原文が書き換わった（＝キーが変わった）ことにする。
// 旧キーの行き先は [oldReasonOrder] との突き合わせで決まり、3つで引き継ぎ候補の
// 3通り（移動・複製・生きている位置が分からない複製）が出る。
//
//	A  旧キーはいまの再生順のどこにも無い              → 移動
//	B  旧キーは別の行で生きていて、その位置も分かる    → 複製
//	C  旧キーは別の行で生きているが、位置の列が空      → 複製（位置が分からない）
func newReasonRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel string, lines []string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// いまの再生順。B と C の旧キーは、別の行として残してある。
	write("data/script_order.csv", []string{
		"section,phase,node,order,line_id,key,speaker,condition",
		"L01 Ryan,intro,Ryan_1_intro,1,line:aaaaaaaa," + key.For(srcANew) + ",Ryan,",
		"L01 Ryan,intro,Ryan_1_intro,2,line:bbbbbbbb," + key.For(srcBNew) + ",Ryan,",
		"L01 Ryan,intro,Ryan_1_intro,3,line:cccccccc," + key.For(srcCNew) + ",Ryan,",
		"L01 Ryan,intro,Ryan_2_more,7,line:bbbbbb22," + key.For(srcBOld) + ",Ryan,",
		",intro,,,line:cccccc22," + key.For(srcCOld) + ",,",
		"",
	})

	// 公開ファイル。旧キーの行が訳つきで残っている状態。
	write("Translations/ja/strings.csv", []string{
		"key,section,node,order,speaker,translation",
		"",
		"# ===== Level 1: Ryan (Sunny) =====",
		"# --- intro: Ryan_1_intro ---",
		key.For(srcAOld) + ",L01 Ryan,Ryan_1_intro,1,Ryan,むかしのやくA",
		key.For(srcBOld) + ",L01 Ryan,Ryan_1_intro,2,Ryan,むかしのやくB",
		key.For(srcCOld) + ",L01 Ryan,Ryan_1_intro,3,Ryan,むかしのやくC",
		// 再生順に同じ台詞IDが無い行。
		"line:zzzzzzzz,L01 Ryan,Ryan_1_intro,4,Ryan,まいごのせりふ",
		"",
		"# ===== UI =====",
		// section が UI で話者が残っている行（台本に無い台詞行）。
		"dddddddddddddddd,UI,,,Ryan,だいほんにないせりふ",
		// section も話者も UI の行（由来を判定できない行）。
		"eeeeeeeeeeeeeeee,UI,,,UI,せってい",
		"",
	})

	// 別のロケールにだけある行を作る。
	write("Translations/he/strings.csv", []string{
		"key,section,node,order,speaker,translation",
		"1111111111111111,UI,,,UI,שלום",
		"",
	})

	// 作業コピー。未翻訳の行と、publish に捨てられる2通りの行を置く。
	write("Translations/_discovered/ja.working.csv", []string{
		"key,section,node,order,speaker,source_en,translation",
		key.For(srcANew) + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcANew + ",",
		// key が16桁hexでも line: でもなく、原文も無い。
		"notahashkey,L01 Ryan,Ryan_1_intro,8,Ryan,,",
		// 原文のハッシュが key と一致しない。
		"2222222222222222,L01 Ryan,Ryan_1_intro,9,Ryan,Rewritten source,やく",
		"",
	})

	return root
}

// oldReasonOrder は [newReasonRoot] に対応する「1つ前の版の再生順」を返す。
//
// git を呼ばずにこれを差し込む。引き継ぎ候補のためだけに、テストが git
// リポジトリを要求する形にしない（[diff.OldOrderSource] がある理由そのもの）。
func oldReasonOrder() []byte {
	return []byte(strings.Join([]string{
		"section,phase,node,order,line_id,key,speaker,condition",
		"L01 Ryan,intro,Ryan_1_intro,1,line:aaaaaaaa," + key.For(srcAOld) + ",Ryan,",
		"L01 Ryan,intro,Ryan_1_intro,2,line:bbbbbbbb," + key.For(srcBOld) + ",Ryan,",
		"L01 Ryan,intro,Ryan_1_intro,3,line:cccccccc," + key.For(srcCOld) + ",Ryan,",
		"",
	}, "\n"))
}

// TestCarryoverNoteIsTranslated は、引き継ぎ候補の注記が ja でも en でも
// 正しく出ることを見る。
//
// 行ごとに引き継ぎ先が違う動的な文なので、鍵だけでは覆えない。置換
// （{target} と {live}）が効いているところまで確かめる。
func TestCarryoverNoteIsTranslated(t *testing.T) {
	root := newReasonRoot(t)
	repo, err := diff.LoadWith(root, diff.Options{
		Working:  true,
		OldOrder: func(string, string) ([]byte, error) { return oldReasonOrder(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	byID := make(map[string]diff.Finding)
	for _, f := range diff.Compare(repo, nil).Findings {
		if f.Category == diff.CatCarryover {
			byID[f.NoteReason.ID] = f
		}
	}
	want := []string{reason.NoteCarryMoved, reason.NoteCarryCopied, reason.NoteCarryCopiedUnknown}
	for _, id := range want {
		if _, ok := byID[id]; !ok {
			t.Fatalf("%s の候補が出ていない（出たのは %v）", id, keysOf(byID))
		}
	}

	s := newTestServer(t, Options{})
	for _, id := range want {
		f := byID[id]
		for _, lang := range []string{"ja", "en"} {
			got := s.reasonText(s.cat.lookup(lang), f.NoteReason)
			if !strings.Contains(got, f.CarryTo) {
				t.Errorf("%s / %s: 引き継ぎ先 %q が入っていない: %q", id, lang, f.CarryTo, got)
			}
			switch lang {
			case "en":
				if hasJapanese(got) {
					t.Errorf("%s: 英語の注記に日本語が混ざっている: %q", id, got)
				}
			case "ja":
				if got != f.Note {
					t.Errorf("%s: ja の注記が CLI と違う: %q / %q", id, got, f.Note)
				}
			}
		}
	}

	// 複製は「旧キーがどこで生きているか」を伝える。位置が分かるほうだけ、
	// その位置が文面に入っていることを見る。
	live := byID[reason.NoteCarryCopied]
	for _, lang := range []string{"ja", "en"} {
		got := s.reasonText(s.cat.lookup(lang), live.NoteReason)
		if !strings.Contains(got, "Ryan_2_more") {
			t.Errorf("%s: 旧キーが生きている位置が入っていない: %q", lang, got)
		}
	}
}

// keysOf は失敗メッセージ用に識別子を並べる。
func keysOf(m map[string]diff.Finding) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestEnglishScreenHasNoJapanese は、英語の画面の「文言の欄」に日本語が
// 1文字も出ないことを見る。
//
// 行の中身（訳・原文・生の行）は数えない。そこはファイルの値なので日本語でよい。
// 見るのは、この道具が書いた文だけである。
func TestEnglishScreenHasNoJapanese(t *testing.T) {
	s := newTestServer(t, Options{Root: newReasonRoot(t), UILang: "en"})
	rec := do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d", rec.Code)
	}
	got := decode[linesResponse](t, rec.Body.Bytes())

	check := func(where, text string) {
		t.Helper()
		if hasJapanese(text) {
			t.Errorf("%s に日本語が出ている: %q", where, text)
		}
	}
	check("readOnlyReason", got.ReadOnlyReason)
	for _, n := range got.Notes {
		check("notes", n)
	}
	for _, st := range got.Stats {
		check("stats", st.Label)
	}
	for _, c := range got.Counts {
		check("counts.label", c.Label)
		check("counts.statusLabel", c.StatusLabel)
		check("counts.reason", c.Reason)
	}
	badges := 0
	for _, line := range got.Lines {
		check("line.reason", line.Reason)
		for _, b := range line.Badges {
			badges++
			check("badge.label", b.Label)
			check("badge.note", b.Note)
		}
	}

	// 欄が空のまま通っては確かめたことにならない。バッジの注記と、
	// 判定を止めた理由が、どちらも1つ以上出ていること。
	if badges == 0 {
		t.Error("バッジが1つも出ていない")
	}
	held := 0
	for _, c := range got.Counts {
		if !c.Judged && c.Reason != "" {
			held++
		}
	}
	if held == 0 {
		t.Error("判定を止めた理由が1つも出ていない")
	}
}

// TestSaveErrorIsTranslated は、保存できない行の理由が英語で出ることを見る。
//
// 外枠（「12行目は編集できない: …」）も中の理由も目録から組む。どちらかが
// 日本語のままだと、英語の文の途中に日本語が挟まる。
func TestSaveErrorIsTranslated(t *testing.T) {
	root := newEditRoot(t)
	// 列数がヘッダーと合わない行を、作業コピーの末尾に足す。
	path := filepath.Join(root, "Translations", "_discovered", "ja.working.csv")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("k,a,b,c\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, lang := range []string{"en", "ja"} {
		s := newTestServer(t, Options{Root: root, UILang: lang})
		lines := getLines(t, s, "ja")

		target := 0
		for _, line := range lines.Lines {
			if line.Kind == lineKindData && !line.Editable {
				target = line.Number
			}
		}
		if target == 0 {
			t.Fatalf("%s: 編集できない行が無い", lang)
		}

		rec := save(t, s, "ja", lines.Version, rowEdit{Line: target, Translation: "x"})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: 状態コードが %d\n%s", lang, rec.Code, rec.Body.String())
		}
		got := decode[errorResponse](t, rec.Body.Bytes())
		if len(got.Results) != 1 || got.Results[0].Error == "" {
			t.Fatalf("%s: 結果が %+v", lang, got.Results)
		}
		msg := got.Results[0].Error

		switch lang {
		case "en":
			if hasJapanese(msg) {
				t.Errorf("英語の理由に日本語が混ざっている: %q", msg)
			}
			if !strings.Contains(msg, "columns") {
				t.Errorf("列数の理由になっていない: %q", msg)
			}
		case "ja":
			// CLI（internal/edit の Error()）と同じ文面のままであること。
			if !strings.Contains(msg, "フィールド数がヘッダーと合わない") {
				t.Errorf("ja の理由が変わっている: %q", msg)
			}
		}
	}
}
