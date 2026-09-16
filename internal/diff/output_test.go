package diff

import (
	"strings"
	"testing"
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
