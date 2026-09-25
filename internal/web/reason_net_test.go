package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/edit"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// この束は、理由を目録へ通す仕掛けの「網の穴」を塞ぐ。
//
// 実装そのものは動いている。落ちるのは、次に誰かが理由を1つ足したときである。
// 足し忘れても試験が通ってしまうと、英語の画面にそこだけ日本語が出たまま
// 気づかれない。だからここは、正しさではなく「間違えたら落ちるか」を見る。

// TestFindingNotesCarryTheirReason は、注記の日本語と識別子がそろっていることを見る。
//
// [diff.Finding] は Note（日本語）と NoteReason（識別子つき）を別々の欄で持つ。
// 片方を入れ忘れる書き方ができてしまうので、そろっていることをここで見張る。
//
// 入れ忘れたときに何が起きるかは2通りある。識別子だけなら日本語が食い違い、
// Note だけなら画面は目録を引けない。後者は [server.findingNote] が日本語へ
// 落として拾うが、拾えばよいという話ではない。英語の画面にそこだけ日本語が出る。
func TestFindingNotesCarryTheirReason(t *testing.T) {
	repo, err := diff.LoadWith(newReasonRoot(t), diff.Options{
		Working:  true,
		OldOrder: func(string, string) ([]byte, error) { return oldReasonOrder(), nil },
	})
	if err != nil {
		t.Fatalf("読み込みに失敗した: %v", err)
	}
	report := diff.Compare(repo, nil)
	if len(report.Findings) == 0 {
		t.Fatal("見本から Finding が1つも出ていない。見本が壊れている")
	}

	notes := 0
	for _, f := range report.Findings {
		if f.Note == "" && f.NoteReason.Empty() {
			continue
		}
		notes++
		if f.Note != f.NoteReason.Text {
			t.Errorf("%s / %s: 注記の日本語が食い違う\n  Note:       %q\n  NoteReason: %q",
				f.Category, f.Key, f.Note, f.NoteReason.Text)
		}
		if f.Note != "" && f.NoteReason.ID == "" {
			t.Errorf("%s / %s: 注記に識別子が無い（%q）。英語の画面にここだけ日本語が出る",
				f.Category, f.Key, f.Note)
		}
	}
	if notes == 0 {
		t.Fatal("注記のある Finding が1つも無い。見本が薄すぎて何も確かめていない")
	}
}

// TestEditErrorFrameMatchesTheSourceText は、保存できない理由の「外枠」が
// Go 側と目録で同じ文面になっていることを見る。
//
// 外枠（「%d行目は編集できない: %s」）は internal/edit の書式と ja.json の
// 2か所に同じ日本語がある。中の理由は reason.* の網が見ているが、外枠は
// その網の外にあり、ずれても誰も落ちない。ずれると、画面と CLI と記録が
// 違う文面で同じことを言う。
func TestEditErrorFrameMatchesTheSourceText(t *testing.T) {
	s := newTestServer(t, Options{})
	cat := s.cat.forServer("ja")

	tests := []struct {
		name string
		err  error
		key  string
	}{
		{
			name: "編集できない行",
			err:  &edit.NotEditableError{Line: 17, Reason: "そんな行番号は無い"},
			key:  "error.not_editable",
		},
		{
			name: "書けない値",
			err:  &edit.InvalidValueError{Line: 6, Reason: "訳に NUL は入れられない"},
			key:  "error.invalid_value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var line, why string
			switch e := tt.err.(type) {
			case *edit.NotEditableError:
				line, why = "17", e.Reason
			case *edit.InvalidValueError:
				line, why = "6", e.Reason
			}
			got := s.cat.T(cat, tt.key, "line", line, "reason", why)
			if got == tt.key {
				t.Fatalf("目録に %s が無い", tt.key)
			}
			if got != tt.err.Error() {
				t.Errorf("外枠の文面が Go 側とずれている\n  目録:  %q\n  Go 側: %q", got, tt.err.Error())
			}
		})
	}
}

// TestEnglishScreenCoversEditReasons は、internal/edit の理由が実際に HTTP を
// 通って英語になることを見る。
//
// 既存の [TestEnglishScreenHasNoJapanese] は readOnlyReason と line.reason も
// 走査しているが、その見本ではどちらも常に空で、素通りしていた。列数の合わない
// 行を1行置いて、空でないことを数えたうえで日本語が無いことを見る。
func TestEnglishScreenCoversEditReasons(t *testing.T) {
	root := newReasonRoot(t)
	// ヘッダーは7列。8列の行を足すと、その行だけが編集できない行になる。
	path := filepath.Join(root, filepath.FromSlash("Translations/_discovered/ja.working.csv"))
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.TrimRight(string(body), "\n") +
		"\n3333333333333333,L01 Ryan,Ryan_1_intro,10,Ryan,Extra,やく,よけいなれつ\n"
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, Options{Root: root, UILang: "en"})
	rec := do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	got := decode[linesResponse](t, rec.Body.Bytes())

	reasons := 0
	for _, line := range got.Lines {
		if line.Reason == "" {
			continue
		}
		reasons++
		if hasJapanese(line.Reason) {
			t.Errorf("%d行目の編集できない理由に日本語が出ている: %q", line.Number, line.Reason)
		}
	}
	if reasons == 0 {
		t.Fatal("編集できない行が1つも出ていない。見本が効いていないので、何も確かめていない")
	}
}

// TestCarryoverBadgeReachesTheScreen は、引き継ぎ候補の注記が画面まで届くことを見る。
//
// この段でいちばん複雑なのが、行ごとに引き継ぎ先が変わる動的な注記である。
// ところがその注記だけは、[server.reasonText] を直に呼ぶ試験しか無く、
// バッジとして /api/lines に載るところまでを通っていなかった。旧再生順を
// 差し込む穴（Options.oldOrder）が無く、見本が git リポジトリでないためである。
func TestCarryoverBadgeReachesTheScreen(t *testing.T) {
	root := newReasonRoot(t)
	// 作業コピーを外す。
	//
	// 画面が並べるのは publish が入力に選ぶファイル（作業コピーがあればそれ）の
	// 行である。引き継ぎ候補が指すのは「再生順から消えた旧キーの行」なので、
	// 作業コピー（いま再生される行だけが入る）には無い。つまり作業コピーがある
	// あいだ、このバッジは原理的に行に付かない。件数の欄には出る。
	// ここで確かめたいのは注記が画面まで届くかなので、公開ファイルを並べる形にする。
	if err := os.Remove(filepath.Join(root, filepath.FromSlash("Translations/_discovered/ja.working.csv"))); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, Options{
		Root:     root,
		UILang:   "en",
		oldOrder: func(string, string) ([]byte, error) { return oldReasonOrder(), nil },
	})
	rec := do(t, s, http.MethodGet, "/api/lines?locale=ja", true, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d\n%s", rec.Code, rec.Body.String())
	}
	got := decode[linesResponse](t, rec.Body.Bytes())

	found := 0
	for _, line := range got.Lines {
		for _, b := range line.Badges {
			if b.Category != diff.CatCarryover.ID() {
				continue
			}
			found++
			if b.Note == "" {
				t.Errorf("%d行目の引き継ぎ候補に注記が無い", line.Number)
			}
			if hasJapanese(b.Note) {
				t.Errorf("%d行目の引き継ぎ候補の注記に日本語が出ている: %q", line.Number, b.Note)
			}
			// 引き継ぎ先のキーが入っていること。置換が効いていないと、
			// 英語にはなっていても「どこへ移すか」が消える。
			if !strings.Contains(b.Note, "Carry over to") {
				t.Errorf("%d行目の注記が引き継ぎ先を示していない: %q", line.Number, b.Note)
			}
		}
	}
	if found == 0 {
		t.Fatal("引き継ぎ候補のバッジが1つも出ていない。旧再生順が差し込めていない")
	}
}

// TestReasonCatalogCoversEveryIdentifier は、識別子が1つ残らず目録にあることを見る。
//
// [reason.All] をなぞる既存の試験と重なるが、こちらは「足りない識別子の名前」を
// 出す。目録に足し忘れたとき、どの鍵を足せばよいかがそのまま分かる。
func TestReasonCatalogCoversEveryIdentifier(t *testing.T) {
	s := newTestServer(t, Options{})
	for _, name := range []string{"ja", "en"} {
		cat := s.cat.forServer(name)
		for _, id := range reason.All() {
			key := "reason." + id
			if text := s.cat.T(cat, key); text == key {
				t.Errorf("%s の目録に %s が無い", name, key)
			}
		}
	}
}
