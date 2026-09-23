package diff

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/order"
)

// sampleReport は出力の確認に使う報告を作る。
// 要作業・要確認・参考が1つずつ出るようにしてある。
func sampleReport(t *testing.T, useWorking bool) (*Repo, *Report) {
	t.Helper()
	files := map[string]string{
		"data/script_order.csv": orderFile(
			orderRow{"L01 Ryan", "intro", "Ryan_1_intro", "1", "line:aaaa1111", keyHello, "Ryan", ""},
			orderRow{"Unused", "", "Start", "1", "line:cccc3333", keyThanks, "Kobold", ""},
			// どのロケールも訳を持たないキー。実データの32件にあたる。
			orderRow{"Unused", "", "Start", "2", "line:dddd4444", keyWow, "Kobold", ""},
		),
		"Translations/ja/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
			keyBye + ",L01 Ryan,Ryan_1_intro,2,Ryan,さようなら\n" +
			keyUI + ",UI,,,UI,はじめる\n" +
			keyOptions + ",UI,,,UI,設定\n",
		"Translations/de/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hallo\n" +
			keyBye + ",L01 Ryan,Ryan_1_intro,2,Ryan,Auf Wiedersehen\n" +
			keyUI + ",UI,,,UI,Start\n" +
			keyOptions + ",UI,,,UI,Optionen\n" +
			keyThanks + ",Unused,Start,1,Kobold,Danke\n",
	}
	if useWorking {
		files["Translations/_discovered/ja.working.csv"] = workingHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n" +
			keyThanks + ",Unused,Start,1,Kobold," + srcThanks + ",\n"
	}
	repo := newRepo(t, files, useWorking)
	return repo, Compare(repo, []string{"ja"})
}

func TestWriteText(t *testing.T) {
	tests := []struct {
		name       string
		useWorking bool
		opt        TextOptions
		want       []string
		notWant    []string
	}{
		{
			name: "作業コピーが無ければ判定できないと書く",
			opt:  TextOptions{},
			want: []string{
				"未翻訳",
				"判定していません（作業コピーがありません）",
				"要作業",
				"要確認",
				"参考",
				"台本から消えた行",
				"要確認が 1 行あります。",
			},
			notWant: []string{"未翻訳                    0 件"},
		},
		{
			name:       "作業コピーがあれば未翻訳を数える",
			useWorking: true,
			opt:        TextOptions{},
			want: []string{
				"作業コピー",
				"1 件",
			},
			notWant: []string{"判定できません（作業コピーがありません）"},
		},
		{
			name: "参考は既定では一覧にしない",
			opt:  TextOptions{},
			want: []string{
				"由来を判定できない行",
				"（--all で一覧）",
			},
			notWant: []string{keyUI},
		},
		{
			name: "--all なら参考も一覧にする",
			opt:  TextOptions{All: true},
			want: []string{keyUI},
		},
		{
			name: "--limit で一覧を切り詰める",
			opt:  TextOptions{All: true, Limit: 1},
			want: []string{"（残り"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, rep := sampleReport(t, tt.useWorking)
			opt := tt.opt
			opt.Root = repo.Root

			var b strings.Builder
			if err := rep.WriteText(&b, opt); err != nil {
				t.Fatalf("書き出しに失敗した: %v", err)
			}
			got := b.String()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("%q が出ていない:\n%s", want, got)
				}
			}
			for _, not := range tt.notWant {
				if strings.Contains(got, not) {
					t.Errorf("%q が出てしまっている:\n%s", not, got)
				}
			}
		})
	}
}

// TestWriteTextPathIsRelative は報告に絶対パスを出さないことを確かめる。
// 手元の絶対パスには利用者名が入ることがあり、CIのログや不具合報告へ貼られると漏れる。
func TestWriteTextPathIsRelative(t *testing.T) {
	repo, rep := sampleReport(t, false)

	var b strings.Builder
	if err := rep.WriteText(&b, TextOptions{Root: repo.Root}); err != nil {
		t.Fatalf("書き出しに失敗した: %v", err)
	}
	got := b.String()
	if strings.Contains(got, repo.Root) {
		t.Errorf("絶対パスが出ている:\n%s", got)
	}
	for _, want := range []string{"data/script_order.csv", "Translations/ja/strings.csv"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が出ていない:\n%s", want, got)
		}
	}
}

// TestWriteTextNotPublishedIsFolded は「どのロケールにも訳が無い行」を
// ノード別に畳んで出すことを確かめる。実データでは13ロケールとも同じ32件が出るので、
// 一覧にすると毎日「できない作業」を並べることになる。
func TestWriteTextNotPublishedIsFolded(t *testing.T) {
	repo, rep := sampleReport(t, false)

	var b strings.Builder
	if err := rep.WriteText(&b, TextOptions{Root: repo.Root}); err != nil {
		t.Fatalf("書き出しに失敗した: %v", err)
	}
	got := b.String()
	for _, want := range []string{
		"どのロケールにも訳が無い行",
		"Unused / Start",
		"うち 1 件は Unused（今のゲームでは到達しません）です。",
		"Translations/ignore.txt",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が出ていない:\n%s", want, got)
		}
	}
}

func TestWriteCSV(t *testing.T) {
	_, rep := sampleReport(t, false)

	var b strings.Builder
	if err := rep.WriteCSV(&b); err != nil {
		t.Fatalf("書き出しに失敗した: %v", err)
	}
	got := b.String()

	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if lines[0] != CSVHeader {
		t.Fatalf("ヘッダーが違う: %q", lines[0])
	}
	if len(lines) != 1+len(rep.Findings) {
		t.Fatalf("行数が違う: got %d, want %d", len(lines)-1, len(rep.Findings))
	}
	for _, line := range lines[1:] {
		if n := strings.Count(line, ","); n < 10 {
			t.Errorf("列が足りない: %q", line)
		}
	}
	if strings.Contains(got, "\r") {
		t.Error("CR が入っている")
	}
	if strings.HasPrefix(got, "\xef\xbb\xbf") {
		t.Error("BOM が付いている")
	}
	for _, want := range []string{"ja,vanished,review,", "ja,unknown_origin,info,", "ja,not_published,info,"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が出ていない:\n%s", want, got)
		}
	}
}

// TestWriteCSVEscapes は訳にカンマや引用符があってもCSVが壊れないことを確かめる。
func TestWriteCSVEscapes(t *testing.T) {
	repo := newRepo(t, map[string]string{
		"data/script_order.csv": orderCSV1,
		"Translations/ja/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
			keyBye + `,L01 Ryan,Ryan_1_intro,2,Ryan,"やあ、""元気"" かい"` + "\n",
	}, false)
	rep := Compare(repo, nil)

	var b strings.Builder
	if err := rep.WriteCSV(&b); err != nil {
		t.Fatalf("書き出しに失敗した: %v", err)
	}
	got := b.String()
	if !strings.Contains(got, `"やあ、""元気"" かい"`) {
		t.Errorf("引用符を戻せていない:\n%s", got)
	}

	// 書いたものを読み戻して列数が保たれることを確かめる。
	rows, err := ReadRows([]byte(got))
	if err != nil {
		t.Fatalf("読み戻せない: %v", err)
	}
	if len(rows) != len(rep.Findings) {
		t.Fatalf("読み戻した行数が違う: got %d, want %d", len(rows), len(rep.Findings))
	}
	if rows[0].Translation != `やあ、"元気" かい` {
		t.Errorf("訳が壊れている: %q", rows[0].Translation)
	}
}

func TestCategoryTable(t *testing.T) {
	seenID := make(map[string]bool, len(categories))
	seenName := make(map[string]bool, len(categories))
	for _, c := range categories {
		if c.String() == "" || c.ID() == "" {
			t.Errorf("%d の名前か識別子が空", int(c))
		}
		if seenID[c.ID()] {
			t.Errorf("識別子が重複している: %s", c.ID())
		}
		if seenName[c.String()] {
			t.Errorf("名前が重複している: %s", c.String())
		}
		seenID[c.ID()] = true
		seenName[c.String()] = true
	}

	tests := []struct {
		status Status
		name   string
		id     string
	}{
		{StatusInfo, "参考", "info"},
		{StatusTodo, "要作業", "todo"},
		{StatusReview, "要確認", "review"},
	}
	for _, tt := range tests {
		if tt.status.String() != tt.name {
			t.Errorf("表示名が違う: got %s, want %s", tt.status, tt.name)
		}
		if tt.status.id() != tt.id {
			t.Errorf("識別子が違う: got %s, want %s", tt.status.id(), tt.id)
		}
	}
	// 重い順に大きい値であること。終了コードの判定がこの順序に依存する。
	if !(StatusReview > StatusTodo && StatusTodo > StatusInfo) {
		t.Error("重さの順序が崩れている")
	}
}

// TestCategoryNoteReason は、カテゴリの既定の注記と、その識別子つきの理由が
// そろっていることを確かめる。NoteReason.Text は常に Note と同じ文字列になる約束で、
// ずれると CLI と画面で別の理由が出る。注記の無いカテゴリは空の理由を返す。
func TestCategoryNoteReason(t *testing.T) {
	for _, c := range categories {
		r := c.noteReason()
		if c.note() == "" {
			if !r.Empty() {
				t.Errorf("%s: 注記が無いのに理由がある: %+v", c, r)
			}
			continue
		}
		if r.Text != c.note() {
			t.Errorf("%s: 文面が注記と違う: %q / %q", c, r.Text, c.note())
		}
		if r.ID == "" {
			t.Errorf("%s: 識別子が空", c)
		}
	}
}

// TestWriteTextWithoutLocales は、報告するロケールが無いときに「要確認はありません」と
// 書かず、そのことを締めで言うことを確かめる。何も比べていないのに要確認が無いと
// 書くと、比べた結果として読まれる。
func TestWriteTextWithoutLocales(t *testing.T) {
	var b strings.Builder
	if err := (&Report{}).WriteText(&b, TextOptions{}); err != nil {
		t.Fatalf("書き出しに失敗した: %v", err)
	}
	got := b.String()
	for _, want := range []string{
		"再生順      （場所が分かりません）   0 行",
		"再生順を読めていません。台本から消えた行などは判定しません。",
		"公開        0 ロケールを読みました",
		"報告するロケールがありません。",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が出ていない:\n%s", want, got)
		}
	}
	if strings.Contains(got, "要確認はありません") {
		t.Errorf("何も比べていないのに要確認が無いと書いている:\n%s", got)
	}
}

// TestWriteTextLocaleHeader はロケールごとの見出し（公開ファイル・作業コピー・
// はみ出しの記録の行）の書き分けを確かめる。
func TestWriteTextLocaleHeader(t *testing.T) {
	published := publishedHeader + keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n"
	working := workingHeader + keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + srcHello + ",こんにちは\n"

	tests := []struct {
		name       string
		files      map[string]string
		useWorking bool
		want       []string
		notWant    []string
	}{
		{
			name: "形の分からない公開行は数だけ出して validate へ送る",
			files: map[string]string{
				"data/script_order.csv":       orderCSV1,
				"Translations/ja/strings.csv": published + "English,,,,,壊れた行\n",
			},
			want: []string{"ハッシュ 1 行 / 台詞ID 0 行 / 形も分からない 1 行（dwloc validate で確かめてください）"},
		},
		{
			name: "原文が未取得の行を数える",
			files: map[string]string{
				"data/script_order.csv":                   orderCSV1,
				"Translations/ja/strings.csv":             published,
				"Translations/_discovered/ja.working.csv": working + keyUI + ",UI,,,UI,,\n",
			},
			useWorking: true,
			want:       []string{"作業コピー  Translations/_discovered/ja.working.csv   2 行 / 原文が未取得 1 行"},
		},
		{
			name: "--no-working で読まなかった作業コピーは書き出しを促さない",
			// 既にある作業コピーを「ゲーム内で書き出してください」と促すと、
			// 済んでいる作業をやり直させることになる。
			files: map[string]string{
				"data/script_order.csv":                   orderCSV1,
				"Translations/ja/strings.csv":             published,
				"Translations/_discovered/ja.working.csv": working,
			},
			want: []string{
				"作業コピー  読みませんでした（Translations/_discovered/ja.working.csv、--no-working）",
				"未翻訳と publish で捨てられる行は判定しません。",
				"判定していません（作業コピーを読んでいません）",
			},
			notWant: []string{"ゲーム内で作業コピーを書き出すと", "作業コピーがありません"},
		},
		{
			name: "作業コピーが無ければ書き出しを促す",
			files: map[string]string{
				"data/script_order.csv":       orderCSV1,
				"Translations/ja/strings.csv": published,
			},
			useWorking: true,
			want: []string{
				"作業コピー  ありません（Translations/_discovered/ja.working.csv）",
				"ゲーム内で作業コピーを書き出すと判定できるようになります。",
				"判定していません（作業コピーがありません）",
			},
		},
		{
			name: "はみ出しの記録を読めたら場所と行数を出す",
			files: map[string]string{
				"data/script_order.csv":       orderCSV1,
				"Translations/ja/strings.csv": published,
				"Translations/_discovered/layout_risks.csv": layoutRisksHeader +
					layoutRow(srcHello, "こんにちは", "x", "240", "180", "1.33", "Canvas/A") +
					layoutRow(srcBye, "さようなら", "x", "240", "180", "1.2", "Canvas/B"),
			},
			useWorking: true,
			want:       []string{"はみ出しの記録  Translations/_discovered/layout_risks.csv   2 行"},
		},
		{
			name: "はみ出しの記録が無ければ見出しに出さない",
			// 無いほうが普通なので、毎回言わない。判定できないことはカテゴリの行が言う。
			files: map[string]string{
				"data/script_order.csv":       orderCSV1,
				"Translations/ja/strings.csv": published,
			},
			useWorking: true,
			want:       []string{"判定していません（ゲーム内で Check translation layout を走らせた記録がありません）"},
			notWant:    []string{"はみ出しの記録  "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRepo(t, tt.files, tt.useWorking)
			rep := Compare(repo, nil)

			var b strings.Builder
			if err := rep.WriteText(&b, TextOptions{Root: repo.Root}); err != nil {
				t.Fatalf("書き出しに失敗した: %v", err)
			}
			got := b.String()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("%q が出ていない:\n%s", want, got)
				}
			}
			for _, not := range tt.notWant {
				if strings.Contains(got, not) {
					t.Errorf("%q が出てしまっている:\n%s", not, got)
				}
			}
		})
	}
}

// TestWriteTextScriptGap は「台本に無い台詞行」の説明に、記録されている話者を
// 重ねずに名前順で並べることを確かめる。孤児とは断定しない書き方も固定する。
func TestWriteTextScriptGap(t *testing.T) {
	repo := newRepo(t, map[string]string{
		"data/script_order.csv": orderCSV1,
		"Translations/ja/strings.csv": publishedHeader +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,こんにちは\n" +
			keyBye + ",UI,,,Conrad,さようなら\n" +
			keyThanks + ",UI,,,Ryan,ありがとう\n" +
			keyWow + ",UI,,,Conrad,わあ\n",
	}, false)
	rep := Compare(repo, nil)
	if got := counts(t, rep, "ja")[CatScriptGap]; got != 3 {
		t.Fatalf("台本に無い台詞行 = %d 件, want 3", got)
	}

	var b strings.Builder
	if err := rep.WriteText(&b, TextOptions{}); err != nil {
		t.Fatalf("書き出しに失敗した: %v", err)
	}
	for _, want := range []string{
		"再生順にありませんが、Conrad、Ryan の台詞として記録されています。",
		"原文が変わって取り残された訳です。公開ファイルだけでは決められません。",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("%q が出ていない:\n%s", want, b.String())
		}
	}
}

// TestWriteScriptGapDetail は話者名の並べ方を固定する。空の話者は名前に数えず、
// 名前が1つも無ければ「〜の台詞として」の1行を出さない（空の名前で文を作らない）。
func TestWriteScriptGapDetail(t *testing.T) {
	const tail = "        data/script_order.csv がゲームの台詞を全部は持っていないためか、\n" +
		"        原文が変わって取り残された訳です。公開ファイルだけでは決められません。\n"

	tests := []struct {
		name string
		list []Finding
		want string
	}{
		{
			name: "重ねずに名前順",
			list: []Finding{{Speaker: "Ryan"}, {Speaker: ""}, {Speaker: "Conrad"}, {Speaker: "Ryan"}},
			want: "        再生順にありませんが、Conrad、Ryan の台詞として記録されています。\n" + tail,
		},
		{
			name: "名前が無ければ説明だけ",
			list: []Finding{{Speaker: ""}},
			want: tail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			writeScriptGapDetail(&b, tt.list)
			if b.String() != tt.want {
				t.Errorf("違う:\n got\n%s\n want\n%s", b.String(), tt.want)
			}
		})
	}
}

// TestWriteNodeBreakdown は「どのロケールにも訳が無い行」の畳み方を固定する。
//
// 件数の多い順、同数はノード名順（実行ごとに並びを変えないため）。位置の無い行は
// 「位置が分かりません」にまとめる。全角の名前が混ざっても件数の桁がそろうこと。
func TestWriteNodeBreakdown(t *testing.T) {
	// 位置の無い行を L01 Ryan より先に置く。同数の並びが出てきた順ではなく
	// 名前順で決まることを見るため。
	list := []Finding{
		{Section: order.UnusedSection, Node: "Start"},
		{},
		{Section: "L01 Ryan", Node: "N1"},
		{Section: order.UnusedSection, Node: "Start"},
	}
	// 最も広い名前は「（位置が分かりません）」の表示幅 22。そこへ余白 4 を足す。
	const unused = "        うち 2 件は Unused（今のゲームでは到達しません）です。\n"
	const ignore = "        Translations/ignore.txt で記録の対象外になっている可能性があります。\n"
	ryan := "            L01 Ryan / N1" + strings.Repeat(" ", 26-13) + "1\n"
	unknown := "            （位置が分かりません）" + strings.Repeat(" ", 26-22) + "1\n"

	t.Run("ignore.txt の見当を添える", func(t *testing.T) {
		var b strings.Builder
		writeNodeBreakdown(&b, list, true)
		want := "            Unused / Start" + strings.Repeat(" ", 26-14) + "2\n" + ryan + unknown + unused + ignore
		if b.String() != want {
			t.Errorf("違う:\n got\n%s\n want\n%s", b.String(), want)
		}
	})

	t.Run("見当を添えない", func(t *testing.T) {
		var b strings.Builder
		writeNodeBreakdown(&b, list[1:3], false)
		if want := ryan + unknown; b.String() != want {
			t.Errorf("違う:\n got\n%s\n want\n%s", b.String(), want)
		}
	})
}

// TestCarryCopiedFirst は、「複製」を先に回した写しを返し、元の並びは変えないことを
// 確かめる。複製どうし・移動どうしはキー順のまま（安定ソート）。
func TestCarryCopiedFirst(t *testing.T) {
	in := []Finding{
		{Key: "1", CarryKind: CarryMoved},
		{Key: "2", CarryKind: CarryCopied},
		{Key: "3", CarryKind: CarryMoved},
		{Key: "4", CarryKind: CarryCopied},
	}
	got := carryCopiedFirst(in)

	keysOf := func(list []Finding) string {
		var out []string
		for _, f := range list {
			out = append(out, f.Key)
		}
		return strings.Join(out, ",")
	}
	if k := keysOf(got); k != "2,4,1,3" {
		t.Errorf("並びが違う: got %s, want 2,4,1,3", k)
	}
	// Report.Findings と CSV の並びは変えない約束。元の並びを書き換えると崩れる。
	if k := keysOf(in); k != "1,2,3,4" {
		t.Errorf("元の並びを書き換えている: %s", k)
	}
}

// TestWriteTextCarryoverLimitKeepsCopied は、--limit で一覧を切り詰めても
// 「複製」の行が残ることを確かめる。
//
// 複製の旧行はいまも別の場所で再生されている。一覧から落ちると、翻訳者は
// 内訳の「複製 1 件」だけを見せられ、どの行かを知らないまま旧行の訳を消しうる。
// キー順では複製が後ろに来るように作ってある。
func TestWriteTextCarryoverLimitKeepsCopied(t *testing.T) {
	const (
		movedFrom  = "1111111111111111"
		copiedFrom = "9999999999999999"
		movedTo    = "aaaaaaaaaaaaaaaa"
		copiedTo   = "bbbbbbbbbbbbbbbb"
	)
	repo := newRepoWith(t, map[string]string{
		"data/script_order.csv": orderFile(
			introRow("1", "line:0001", movedTo),
			introRow("2", "line:0002", copiedTo),
			unusedRow("1", "line:0009", copiedFrom),
		),
		"Translations/ja/strings.csv": publishedHeader +
			movedFrom + ",L01 Ryan,Ryan_1_intro,1,Ryan,あ\n" +
			copiedFrom + ",L01 Ryan,Ryan_1_intro,2,Ryan,い\n",
	}, Options{OldOrder: fixedOldOrder(orderFile(
		introRow("1", "line:0001", movedFrom),
		introRow("2", "line:0002", copiedFrom),
		unusedRow("1", "line:0009", copiedFrom),
	))})
	rep := Compare(repo, nil)
	sum := rep.Locales[0]
	if sum.CarryMoved != 1 || sum.CarryCopied != 1 {
		t.Fatalf("前提が崩れている: 移動 %d / 複製 %d", sum.CarryMoved, sum.CarryCopied)
	}

	var b strings.Builder
	if err := rep.WriteText(&b, TextOptions{Limit: 1}); err != nil {
		t.Fatalf("書き出しに失敗した: %v", err)
	}
	got := b.String()
	if !strings.Contains(got, "        複製   L01 Ryan / Ryan_1_intro / 2   Ryan   "+copiedFrom) {
		t.Errorf("複製の行が一覧に残っていない:\n%s", got)
	}
	if strings.Contains(got, "        移動   ") {
		t.Errorf("移動の行が複製より先に残っている:\n%s", got)
	}
	if !strings.Contains(got, "（残り 1 件は --limit 0 で出ます）") {
		t.Errorf("切り詰めたことを書いていない:\n%s", got)
	}
	// 「台本から消えた行」と重なるのは移動の1件だけ。複製の旧キーは生きている。
	if !strings.Contains(got, "うち 1 件には引き継ぎ候補があります") {
		t.Errorf("重なりの数が違う:\n%s", got)
	}

	// 並べ替えるのは text の一覧だけ。CSV はキー順のまま。
	var csv strings.Builder
	if err := rep.WriteCSV(&csv); err != nil {
		t.Fatalf("CSV を書けない: %v", err)
	}
	moved := strings.Index(csv.String(), "ja,carryover,review,"+movedFrom)
	copied := strings.Index(csv.String(), "ja,carryover,review,"+copiedFrom)
	if moved < 0 || copied < 0 || moved > copied {
		t.Errorf("CSV の並びを変えている:\n%s", csv.String())
	}
}

// TestWriteTextCarryHintOnlyWhenOverlapping は、「台本から消えた行」のうち引き継ぎ
// 候補の付いた行が無ければ、その断り書きを出さないことを確かめる。「うち 0 件」と
// 書くと、候補を探したが無かったのか、何かが漏れたのかを読み手に考えさせる。
func TestWriteTextCarryHintOnlyWhenOverlapping(t *testing.T) {
	repo := newRepoWith(t, map[string]string{
		// line:0003 の行ごと消えた。キーの変わった行は無い。
		"data/script_order.csv": orderFile(
			introRow("1", "line:0001", keyA),
			introRow("2", "line:0002", keyB),
			introRow("3", "line:0004", keyD),
		),
		"Translations/ja/strings.csv": publishedFour(keyA, keyB, keyC, keyD),
	}, Options{OldOrder: fixedOldOrder(carryOldOrder)})
	rep := Compare(repo, nil)
	sum := rep.Locales[0]
	if !sum.CanJudge(CatCarryover) || sum.Counts[CatVanished] != 1 {
		t.Fatalf("前提が崩れている: 判定=%v 台本から消えた行=%d", sum.CanJudge(CatCarryover), sum.Counts[CatVanished])
	}

	var b strings.Builder
	if err := rep.WriteText(&b, TextOptions{}); err != nil {
		t.Fatalf("書き出しに失敗した: %v", err)
	}
	if strings.Contains(b.String(), "には引き継ぎ候補があります") {
		t.Errorf("重なりが無いのに断り書きを出している:\n%s", b.String())
	}
	if !strings.Contains(b.String(), pad(CatCarryover.String(), categoryNameWidth())+"0 件") {
		t.Errorf("判定できたのに 0 件と書いていない:\n%s", b.String())
	}
}

// TestWriteTextFindingsStayInLocale は、ロケールごとの一覧に他のロケールの行が
// 混ざらないことを確かめる。混ざると、翻訳者は自分の担当でない行を直しに行く。
func TestWriteTextFindingsStayInLocale(t *testing.T) {
	published := publishedHeader + keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,訳\n"
	repo := newRepo(t, map[string]string{
		"data/script_order.csv": orderCSV1,
		// 台詞ID行はロケール間の差（ハッシュキーだけを見る）に出ないので、
		// それぞれのロケールにだけ出る行を作れる。
		"Translations/de/strings.csv": published + "line:deonly01,,,,,de の行\n",
		"Translations/ja/strings.csv": published + "line:jaonly01,,,,,ja の行\n",
	}, false)
	rep := Compare(repo, nil)

	var b strings.Builder
	if err := rep.WriteText(&b, TextOptions{}); err != nil {
		t.Fatalf("書き出しに失敗した: %v", err)
	}
	got := b.String()
	deStart := strings.Index(got, "\nde  ")
	jaStart := strings.Index(got, "\nja  ")
	if deStart < 0 || jaStart < 0 || deStart > jaStart {
		t.Fatalf("ロケールの見出しが見つからないか並びが違う:\n%s", got)
	}
	de, ja := got[deStart:jaStart], got[jaStart:]
	if !strings.Contains(de, "line:deonly01") || strings.Contains(de, "line:jaonly01") {
		t.Errorf("de の一覧が違う:\n%s", de)
	}
	if !strings.Contains(ja, "line:jaonly01") || strings.Contains(ja, "line:deonly01") {
		t.Errorf("ja の一覧が違う:\n%s", ja)
	}
}

// TestFindingLine は一覧の1行の組み立てを固定する。
func TestFindingLine(t *testing.T) {
	const sep = "   "
	tests := []struct {
		name string
		f    Finding
		want string
	}{
		{
			name: "位置・話者・キー・訳を並べ、カテゴリ共通の理由は繰り返さない",
			f: Finding{Category: CatVanished, Section: "L01 Ryan", Node: "N1", OrderText: "3",
				Speaker: "Ryan", Key: keyHello, Translation: "こんにちは", Note: noteVanished},
			want: "L01 Ryan / N1 / 3" + sep + "Ryan" + sep + keyHello + sep + "訳: こんにちは",
		},
		{
			name: "位置が無ければそう書く",
			f:    Finding{Category: CatNotPublished, Key: keyHello, Note: noteNotPublished},
			want: "（位置が分かりません）" + sep + keyHello,
		},
		{
			name: "訳が無ければ原文を出す",
			f:    Finding{Category: CatUntranslated, Section: "UI", Key: keyUI, SourceEn: srcUI, Note: noteUntranslated},
			want: "UI" + sep + keyUI + sep + "原文: " + srcUI,
		},
		{
			name: "行ごとの理由は括弧で添える",
			f:    Finding{Category: CatLocaleGap, Section: "UI", Key: keyUI, Translation: "はじめる", Note: "他の 2 ロケールにあります"},
			want: "UI" + sep + keyUI + sep + "訳: はじめる" + sep + "（他の 2 ロケールにあります）",
		},
		{
			name: "引き継ぎ候補は移動か複製かを左端に置く",
			f: Finding{Category: CatCarryover, CarryKind: CarryCopied, Section: "L01 Ryan",
				Key: keyB, Translation: "い", Note: "引き継ぎ先 " + keyB2},
			want: "複製" + sep + "L01 Ryan" + sep + keyB + sep + "訳: い" + sep + "（引き継ぎ先 " + keyB2 + "）",
		},
		{
			name: "区別の無い引き継ぎ候補には何も付けない",
			f:    Finding{Category: CatCarryover, Section: "L01 Ryan", Key: keyB},
			want: "L01 Ryan" + sep + keyB,
		},
		{
			name: "訳の改行は1行に畳む",
			f:    Finding{Category: CatVanished, Section: "UI", Key: keyUI, Translation: "一行目\r\n二行目", Note: noteVanished},
			want: "UI" + sep + keyUI + sep + "訳: 一行目 二行目",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findingLine(tt.f); got != tt.want {
				t.Errorf("違う:\n got  %q\n want %q", got, tt.want)
			}
		})
	}
}

func TestClip(t *testing.T) {
	forty := strings.Repeat("あ", textClipRunes)
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "短ければそのまま", in: "こんにちは", want: "こんにちは"},
		{name: "上限ちょうどは畳まない", in: forty, want: forty},
		{name: "上限を超えたら文字数で切って印を付ける", in: forty + "い", want: forty + "…"},
		{name: "改行とタブは空白にする", in: "a\r\nb\nc\rd\te", want: "a b c d e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clip(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDisplayWidth は等幅端末での表示幅の近似を、範囲の端で確かめる。
// 桁がずれても意味は変わらないが、ずれると一覧の件数の列がそろわなくなる。
func TestDisplayWidth(t *testing.T) {
	tests := []struct {
		r    rune
		want int
	}{
		{'A', 1}, {'é', 1}, {'ｱ', 1}, // 半角カナは1
		{0x1100, 2}, {0x115F, 2}, {0x1160, 1}, // ハングル字母
		{0x2E80, 2}, {0x3000, 2}, {0x303E, 2}, {0x303F, 1}, // 記号と全角空白
		{0x3041, 2}, {'あ', 2}, {0x33FF, 2}, // かな
		{0x3400, 2}, {0x4DBF, 2}, {'漢', 2}, {0x9FFF, 2}, // CJK
		{0xA000, 2}, {0xA4CF, 2}, {0xA4D0, 1}, // イ文字
		{0xAC00, 2}, {0xD7A3, 2}, {0xD7A4, 1}, // ハングル音節
		{0xF900, 2}, {0xFAFF, 2}, // CJK互換漢字
		{0xFE30, 2}, {0xFE6F, 2}, {0xFE70, 1}, // CJK互換形
		{0xFF01, 2}, {0xFF60, 2}, {0xFF61, 1}, // 全角英数と半角カナの境
		{0xFFE0, 2}, {0xFFE6, 2}, {0xFFE7, 1}, // 全角記号
		{0x20000, 2}, {0x3FFFD, 2}, {0x3FFFE, 1}, // CJK拡張B以降
	}
	for _, tt := range tests {
		if got := displayWidth(string(tt.r)); got != tt.want {
			t.Errorf("U+%04X: got %d, want %d", tt.r, got, tt.want)
		}
	}
	if got := displayWidth("台本 A"); got != 6 {
		t.Errorf("混ざった文字列の幅: got %d, want 6", got)
	}
}

func TestPad(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{"abc", 5, "abc  "},
		{"日本", 5, "日本 "},
		{"abc", 3, "abc"},
		// 既に超えていれば削らない。削ると名前が読めなくなる。
		{"日本語", 5, "日本語"},
	}
	for _, tt := range tests {
		if got := pad(tt.in, tt.width); got != tt.want {
			t.Errorf("pad(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
		}
	}
}

// TestRelPath は報告に出すパスの相対化を確かめる。
//
// 手元の絶対パスには利用者名が入ることがあるので、ルートの中は相対にする。
// ルートの外は相対にしない。"../../" の並びは読み手がどこを指すか追えない。
func TestRelPath(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "other", "x.csv")

	tests := []struct {
		name string
		root string
		path string
		want string
	}{
		{name: "パスが空なら空", root: root, path: "", want: ""},
		{name: "ルートが空ならそのまま", root: "", path: filepath.Join("a", "b.csv"), want: "a/b.csv"},
		{name: "ルートの中は相対", root: root, path: filepath.Join(root, "Translations", "ja", "strings.csv"), want: "Translations/ja/strings.csv"},
		{name: "ルートの外はそのまま", root: root, path: outside, want: filepath.ToSlash(outside)},
		{name: "ルートの親もそのまま", root: root, path: filepath.Dir(root), want: filepath.ToSlash(filepath.Dir(root))},
		// ".." で始まるだけの名前はルートの中にある。外と取り違えない。
		{name: "名前が .. で始まるだけならルートの中", root: root, path: filepath.Join(root, "..hidden"), want: "..hidden"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := relPath(tt.root, tt.path); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("ドライブが違えばそのまま", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("ドライブ文字は Windows だけ")
		}
		if got, want := relPath(`C:\repo`, `D:\game\ja.working.csv`), "D:/game/ja.working.csv"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

// errWriter は必ず失敗する書き出し先。
type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

// TestWriteFailure は、書き出しの失敗を黙って捨てないことを確かめる。
// cmd/dwloc はこの誤りで終了コードを決める。捨てると、途中で切れた報告が
// 成功として扱われる。
func TestWriteFailure(t *testing.T) {
	_, rep := sampleReport(t, false)
	want := errors.New("書き出し先がいっぱい")

	if err := rep.WriteText(errWriter{want}, TextOptions{}); !errors.Is(err, want) {
		t.Errorf("text の失敗を返していない: %v", err)
	}
	if err := rep.WriteCSV(errWriter{want}); !errors.Is(err, want) {
		t.Errorf("csv の失敗を返していない: %v", err)
	}
}
