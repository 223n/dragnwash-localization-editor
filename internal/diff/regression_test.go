package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// TestWorkingCopyWithoutKeyColumn は、key 列を持たない作業コピーの行を
// 見落とさないことを確かめる。
//
// ゲーム内のUIエクスポートは `key,source_en,translation,object_path` で、
// key 列が空の行を普通に含む。publish はその行を source_en のハッシュで採用して
// 公開するので、見落とすと「訳を書けば公開される行」を未翻訳 0 件と報告する。
func TestWorkingCopyWithoutKeyColumn(t *testing.T) {
	tests := []struct {
		name    string
		working string
	}{
		{
			name:    "key 列そのものが無い",
			working: "source_en,translation\n" + srcHello + ",\n",
		},
		{
			name:    "key 列はあるが値が空",
			working: workingHeader + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRepo(t, map[string]string{
				"data/script_order.csv":                   orderCSV1,
				"Translations/ja/strings.csv":             publishedHeader,
				"Translations/_discovered/ja.working.csv": tt.working,
			}, true)
			rep := Compare(repo, nil)

			if got := rep.Locales[0].Counts[CatUntranslated]; got != 1 {
				t.Fatalf("未翻訳 = %d 件, want 1", got)
			}
			var f Finding
			for _, x := range rep.Findings {
				if x.Category == CatUntranslated {
					f = x
				}
			}
			if f.Key != keyHello {
				t.Errorf("Key = %q, want %q（source_en から導く）", f.Key, keyHello)
			}
			if got := rep.Locales[0].Counts[CatDropped]; got != 0 {
				t.Errorf("publish で捨てられる行 = %d 件, want 0", got)
			}
		})
	}
}

// TestWorkingCopyDuplicateKey は、同じキーが2回あるときに publish の採用規則へ
// そろえることを確かめる。
//
// publish は訳が空の行を採用表に入れないので、訳のある行が公開される
// （移植仕様「公開CSV生成 R18 / R20」）。行単位で見ると、公開される訳があるのに
// 未翻訳だと報告することになる。
func TestWorkingCopyDuplicateKey(t *testing.T) {
	tests := []struct {
		name string
		rows string
	}{
		{
			name: "訳が空の行が先",
			rows: "," + srcHello + ",\n," + srcHello + ",こんにちは\n",
		},
		{
			name: "訳のある行が先",
			rows: "," + srcHello + ",こんにちは\n," + srcHello + ",\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRepo(t, map[string]string{
				"data/script_order.csv":                   orderCSV1,
				"Translations/ja/strings.csv":             publishedHeader,
				"Translations/_discovered/ja.working.csv": "key,source_en,translation\n" + tt.rows,
			}, true)
			rep := Compare(repo, nil)

			if got := rep.Locales[0].Counts[CatUntranslated]; got != 0 {
				t.Errorf("未翻訳 = %d 件, want 0（訳のある行が publish に採用される）", got)
			}
		})
	}
}

// TestWorkingCopyKeysLeaveLocaleGap は、作業コピーが手元にあるキーを
// 「他のロケールにあって無い行」「どのロケールにも訳が無い行」から外すことを
// 確かめる。外さないと、1行の仕事が要作業2件に見える。
func TestWorkingCopyKeysLeaveLocaleGap(t *testing.T) {
	files := map[string]string{
		"data/script_order.csv": orderTwo,
		// ja は両方の訳を持つ。it は公開が空で、作業コピーだけがある。
		"Translations/ja/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
			keyBye + ",L01 Ryan,Ryan_1_intro,2,Kobold,さようなら\n",
		"Translations/it/strings.csv": publishedHeader,
		"Translations/_discovered/it.working.csv": "key,source_en,translation\n" +
			keyHello + "," + srcHello + ",Pronto?\n" +
			keyBye + "," + srcBye + ",\n",
	}

	repo := newRepo(t, files, true)
	rep := Compare(repo, []string{"it"})
	sum := rep.Locales[0]

	if got := sum.Counts[CatUntranslated]; got != 1 {
		t.Errorf("未翻訳 = %d 件, want 1", got)
	}
	if got := sum.Counts[CatLocaleGap]; got != 0 {
		t.Errorf("他のロケールにあって無い行 = %d 件, want 0（作業コピーにあるキーは外す）", got)
	}
	if got := sum.Counts[CatNotPublished]; got != 0 {
		t.Errorf("どのロケールにも訳が無い行 = %d 件, want 0", got)
	}

	// --no-working なら作業コピーを見ないので、2件とも他ロケールとの差になる。
	repo = newRepo(t, files, false)
	rep = Compare(repo, []string{"it"})
	if got := rep.Locales[0].Counts[CatLocaleGap]; got != 2 {
		t.Errorf("--no-working の他のロケールにあって無い行 = %d 件, want 2", got)
	}
}

// TestUnreadableOrderSuspendsJudgement は、再生順を読めないときに
// 「再生順に無い」を根拠にするカテゴリを判定しないことを確かめる。
//
// 判定すると、公開行のほぼ全部がそのカテゴリに化ける。実データの ja では
// 1570件が「台本から消えた行（要確認）」になり、終了コードも 1 になる。
func TestUnreadableOrderSuspendsJudgement(t *testing.T) {
	published := publishedHeader +
		keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
		keyUI + ",UI,,,UI,はじめる\n" +
		"line:aaaa1111,L01 Ryan,Ryan_1_intro,1,Ryan,やあ\n"

	tests := []struct {
		name  string
		order string
	}{
		{name: "ファイルが無い"},
		{name: "ヘッダーだけ", order: "section,phase,node,order,line_id,key,speaker,condition\n"},
		{
			// 行はあるが key 列と line_id 列を引けない。行数だけ見ていると
			// 読めたように見える。
			name: "列名が違って key も line_id も引けない",
			order: "section,phase,node,order,lineid,hashkey,speaker,condition\n" +
				"L01 Ryan,intro,N1,1,line:aaaa1111," + keyHello + ",Ryan,\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{"Translations/ja/strings.csv": published}
			if tt.order != "" {
				files["data/script_order.csv"] = tt.order
			}
			repo := newRepo(t, files, false)
			rep := Compare(repo, nil)
			sum := rep.Locales[0]

			for _, c := range []Category{CatVanished, CatScriptGap, CatUnknownOrigin, CatStrayLineID, CatNotPublished} {
				if sum.canJudge(c) {
					t.Errorf("%s を判定してしまっている", c)
				}
				if got := sum.Counts[c]; got != 0 {
					t.Errorf("%s = %d 件, want 0", c, got)
				}
			}
			if rep.Status() == StatusReview {
				t.Error("再生順を読めないのに要確認になっている")
			}
			// 台本に無い台詞ID行は台詞IDしか要らないが、キーも読めていないので理由は
			// 見出しと同じ「再生順を読めていません」にする。台詞IDのことだけを書くと、
			// line_id 列だけを直しに行かせることになる。
			if why := sum.JudgeBlockReason(CatStrayLineID); why.ID != reason.JudgeOrderUnreadable {
				t.Errorf("台本に無い台詞ID行の理由が違う: %q (%q)", why.ID, why.Text)
			}

			var b strings.Builder
			if err := rep.WriteText(&b, TextOptions{}); err != nil {
				t.Fatalf("WriteText が失敗した: %v", err)
			}
			if !strings.Contains(b.String(), "再生順を読めていません") {
				t.Errorf("再生順を読めないことを伝えていない:\n%s", b.String())
			}
		})
	}
}

// TestNotPublishedNeedsOrderKeys は、「どのロケールにも訳が無い行」を、再生順の
// キーを読めていないときに「0 件」ではなく保留にすることを確かめる。
//
// このカテゴリの行は再生順のキーからしか生まれない。公開ファイルにあるキーは
// 必ずどこかのロケールが持っているので、「他のロケールにあって無い行」へ回る。
// キーを読めていなければ1件も見つけようがないのに 0 件と書くと、訳の無い行は
// 無いと読まれる。引き継ぎ候補に台詞IDを要るものとして足したとき
// （TestCarryoverNeedsOrderLineIDs）と同じ理屈である。
//
// 読めているときは今までどおり数えること。保留に倒しすぎると、実データで
// 13ロケールとも出ている32件が画面と端末から消える。
func TestNotPublishedNeedsOrderKeys(t *testing.T) {
	published := publishedHeader + keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n"
	label := pad(CatNotPublished.String(), categoryNameWidth())

	tests := []struct {
		name      string
		order     string
		wantJudge bool
		wantLine  string
	}{
		{
			// Goodbye は再生順にだけあり、どのロケールにも訳が無い。
			name:      "キーを読めていれば数える",
			order:     orderTwo,
			wantJudge: true,
			wantLine:  label + "1 件",
		},
		{
			name:     "再生順が無ければ保留にする",
			wantLine: label + "判定していません（再生順を読めていません）",
		},
		{
			name:     "key 列を引けなければ保留にする",
			order:    "section,phase,node,order,line_id,hashkey,speaker,condition\n" + "L01 Ryan,intro,N1,1,line:aaaa1111," + keyBye + ",Ryan,\n",
			wantLine: label + "判定していません（再生順を読めていません）",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{"Translations/ja/strings.csv": published}
			if tt.order != "" {
				files["data/script_order.csv"] = tt.order
			}
			rep := Compare(newRepo(t, files, false), nil)
			sum := rep.Locales[0]

			if got := sum.CanJudge(CatNotPublished); got != tt.wantJudge {
				t.Fatalf("判定したか = %v, want %v", got, tt.wantJudge)
			}
			if !tt.wantJudge {
				if why := sum.JudgeBlockReason(CatNotPublished); why.ID != reason.JudgeOrderUnreadable {
					t.Errorf("理由が違う: %q (%q)", why.ID, why.Text)
				}
			}

			var b strings.Builder
			if err := rep.WriteText(&b, TextOptions{}); err != nil {
				t.Fatalf("WriteText が失敗した: %v", err)
			}
			if !strings.Contains(b.String(), tt.wantLine) {
				t.Errorf("%q が出ていない:\n%s", tt.wantLine, b.String())
			}
			if !tt.wantJudge && strings.Contains(b.String(), label+"0 件") {
				t.Errorf("判定できないのに 0 件と書いている:\n%s", b.String())
			}
		})
	}
}

// TestEmptyLocaleIsReported は、公開ファイルも作業コピーも無いロケールを
// 黙って落とさないことを確かめる。
//
// publish.DiscoverTargets は書き出す元が無いロケールを列挙から外す。publish には
// それが正しいが、この道具では「訳が1件も無い」という最大の要作業にあたる。
func TestEmptyLocaleIsReported(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("ディレクトリを作れない: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("ファイルを書けない: %v", err)
		}
	}
	write("data/script_order.csv", orderCSV1)
	write("Translations/ja/strings.csv", publishedHeader+keyHello+",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n")
	// ディレクトリだけを作る。新しい翻訳者が作った直後の状態。
	if err := os.MkdirAll(filepath.Join(root, "Translations", "it"), 0o755); err != nil {
		t.Fatalf("ディレクトリを作れない: %v", err)
	}
	// '_' 始まりは publish と同じく飛ばす。
	if err := os.MkdirAll(filepath.Join(root, "Translations", "_discovered"), 0o755); err != nil {
		t.Fatalf("ディレクトリを作れない: %v", err)
	}

	repo, err := Load(root, true)
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	if len(repo.EmptyLocales) != 1 || repo.EmptyLocales[0] != "it" {
		t.Fatalf("EmptyLocales = %q, want [it]", repo.EmptyLocales)
	}

	rep := Compare(repo, nil)
	var b strings.Builder
	if err := rep.WriteText(&b, TextOptions{}); err != nil {
		t.Fatalf("WriteText が失敗した: %v", err)
	}
	if !strings.Contains(b.String(), "訳が1件もないロケール: it") {
		t.Errorf("空のロケールを報告していない:\n%s", b.String())
	}
}

// TestWorkingFindingBorrowsPosition は、位置の列を持たない作業コピーの行に
// 再生順から位置を補うことを確かめる。3列とも空のときだけ補い、
// 片方だけ埋まっている行には手を出さない。
func TestWorkingFindingBorrowsPosition(t *testing.T) {
	tests := []struct {
		name        string
		row         string
		wantSection string
		wantNode    string
		wantOrder   string
	}{
		{
			name: "3列とも空なら補う",
			row:  "key,source_en,translation\n" + keyHello + "," + srcHello + ",\n",

			wantSection: "L01 Ryan", wantNode: "Ryan_1_intro", wantOrder: "1",
		},
		{
			name: "section だけ埋まっていれば触らない",
			row:  workingHeader + keyHello + ",手で書いた,,,," + srcHello + ",\n",

			wantSection: "手で書いた", wantNode: "", wantOrder: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRepo(t, map[string]string{
				"data/script_order.csv":                   orderCSV1,
				"Translations/ja/strings.csv":             publishedHeader,
				"Translations/_discovered/ja.working.csv": tt.row,
			}, true)
			rep := Compare(repo, nil)

			var f Finding
			for _, x := range rep.Findings {
				if x.Category == CatUntranslated {
					f = x
				}
			}
			if f.Key != keyHello {
				t.Fatalf("未翻訳が見つからない（Findings=%d件）", len(rep.Findings))
			}
			if f.Section != tt.wantSection || f.Node != tt.wantNode || f.OrderText != tt.wantOrder {
				t.Errorf("位置 = %q / %q / %q, want %q / %q / %q",
					f.Section, f.Node, f.OrderText, tt.wantSection, tt.wantNode, tt.wantOrder)
			}
		})
	}
}
