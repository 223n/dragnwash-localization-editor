package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ここは /api/export の試験。
//
// 書き出しは、翻訳者が選んだ場所へCSVを保存させる道である（issue #42）。
// 場所を決めるのはブラウザーで、待ち受けは中身を返すだけ。画面からパスを
// 受け取る経路は1つも作らない、というのがこの機能の要である。

// exportGet は書き出しを1回取りにいく。
func exportGet(t *testing.T, s *server, query string) *http.Response {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/export?"+query, true, nil)
	return rec.Result()
}

func TestExportPublishedIsThePublishForm(t *testing.T) {
	// publish が作るのと同じ形を返す。見出しのコメントが入るのがその印で、
	// いま編集しているファイル（見出しを持たない作業コピー）とは別物になる。
	s := newTestServer(t, Options{UILang: "ja"})
	rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form=published", true, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d、200 を期待: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "key,section,node,order,speaker,translation") {
		t.Errorf("公開ファイルの見出し行で始まっていない:\n%s", body)
	}
	if !strings.Contains(body, "# ") {
		t.Error("見出しのコメントが入っていない。publish の形になっていない")
	}
	if !strings.Contains(body, keyKept) {
		t.Error("訳のある行が出ていない")
	}
}

func TestExportWorkingIsTheFileAsItIs(t *testing.T) {
	// 組み立て直さない。「画面で直したものが、そのままの形で欲しい」という
	// 求めに別の形を返すのは答えになっていない。
	root := newTestRoot(t)
	s := newTestServer(t, Options{Root: root, UILang: "ja"})
	rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form=working", true, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("状態コードが %d、200 を期待: %s", rec.Code, rec.Body.String())
	}
	want, err := os.ReadFile(filepath.Join(root, "Translations", "ja", "strings.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != string(want) {
		t.Errorf("ファイルの中身と違う\n got %q\nwant %q", rec.Body.String(), string(want))
	}
}

func TestExportSaysItIsAnAttachment(t *testing.T) {
	// これが無いと、ブラウザーは保存ではなく表示へ回す。
	s := newTestServer(t, Options{UILang: "ja"})
	for _, form := range []string{"published", "working"} {
		rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form="+form, true, nil)
		if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="strings.csv"` {
			t.Errorf("%s: Content-Disposition が %q", form, got)
		}
		if got := rec.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
			t.Errorf("%s: Content-Type が %q", form, got)
		}
	}
}

func TestExportRejectsUnknownForm(t *testing.T) {
	s := newTestServer(t, Options{UILang: "ja"})
	for _, query := range []string{"locale=ja", "locale=ja&form=", "locale=ja&form=nope", "locale=ja&form=PUBLISHED"} {
		rec := do(t, s, http.MethodGet, "/api/export?"+query, true, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: 状態コードが %d、400 を期待", query, rec.Code)
		}
	}
}

func TestExportRejectsUnknownLocale(t *testing.T) {
	s := newTestServer(t, Options{UILang: "ja"})

	rec := do(t, s, http.MethodGet, "/api/export?form=published", true, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("ロケールを省いたのに %d", rec.Code)
	}

	// 当たらない名前は 404。誤りの文面に要求の値を書き戻さない。
	rec = do(t, s, http.MethodGet, "/api/export?locale=nope&form=published", true, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("状態コードが %d、404 を期待", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "nope") {
		t.Errorf("誤りの文面に要求の値が出ている: %q", rec.Body.String())
	}
}

func TestExportNeedsTheCookie(t *testing.T) {
	// ほかの経路と同じ守りを通ること。通らないと、この待ち受けが開いている
	// あいだ、ブラウザーの中の別の頁からCSVを丸ごと取れる道が1つできる。
	s := newTestServer(t, Options{UILang: "ja"})
	rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form=published", false, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("Cookie 無しで %d。404 を期待", rec.Code)
	}
}

func TestExportRefusesToLoseTranslations(t *testing.T) {
	// いちばん大事な性質。publish が「書くと訳が失われる」と止める中身を、
	// 書き出しからは出せてしまうと、守りは publish を回した人にしか効かない。
	// 翻訳者は落としたファイルを自分の手でリポジトリへ写せる。
	root := newTestRoot(t)
	// 作業コピーを、公開ファイルより1行少ない形で置く。
	working := filepath.Join(root, "Translations", "_discovered", "ja.working.csv")
	if err := os.MkdirAll(filepath.Dir(working), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"key,section,node,order,speaker,translation",
		keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？",
		"",
	}, "\n")
	if err := os.WriteFile(working, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, Options{Root: root, UILang: "ja"})
	rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form=published", true, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("状態コードが %d、409 を期待:\n%s", rec.Code, rec.Body.String())
	}
	// 失われる行の訳そのものは画面へ出さない。内訳は dwloc publish が出す。
	if strings.Contains(rec.Body.String(), jaVanished) {
		t.Errorf("誤りの文面に行の中身が出ている: %q", rec.Body.String())
	}

	// 同じ状態でも、いま編集しているファイルはそのまま出せること。
	// 止めるのは「publish の形にすると失われる」ときだけで、書き出し全部ではない。
	rec = do(t, s, http.MethodGet, "/api/export?locale=ja&form=working", true, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("編集中のファイルの書き出しまで止めている: %d", rec.Code)
	}
}

func TestExportNameIsSafeForTheHeader(t *testing.T) {
	// 名前はこちら側のパスから作るが、ロケール名はディレクトリ名から来る。
	// 引用符や改行を入れたディレクトリを作れば、ヘッダーを割れる。作れるのは
	// この待ち受けを動かしている本人だが、守りの範囲を「誰が作ったか」で決めない。
	for _, tc := range []struct {
		path string
		want string
	}{
		{filepath.Join("a", "strings.csv"), "strings.csv"},
		{filepath.Join("a", "ja.working.csv"), "ja.working.csv"},
		{filepath.Join("a", "pt-BR.working.csv"), "pt-BR.working.csv"},
		// 数字も通す字に入っている。落とすと名前ごと strings.csv になる。
		{filepath.Join("a", "strings_v2.csv"), "strings_v2.csv"},
		{filepath.Join("a", `bad".csv`), "strings.csv"},
		{filepath.Join("a", "bad\n.csv"), "strings.csv"},
		{filepath.Join("a", "日本語.csv"), "strings.csv"},
		{filepath.Join("a", ".csv"), "strings.csv"},
		{filepath.Join("a", "..csv"), "strings.csv"},
		{"", "strings.csv"},
	} {
		if got := exportName(tc.path); got != tc.want {
			t.Errorf("exportName(%q) が %q、%q を期待", tc.path, got, tc.want)
		}
	}
}

func TestExportIsRecorded(t *testing.T) {
	// 記録には形と大きさだけを残す。中身は残さない。
	var file strings.Builder
	s := newTestServer(t, Options{Record: &file, UILang: "ja"})
	do(t, s, http.MethodGet, "/api/export?locale=ja&form=published", true, nil)

	text := file.String()
	for _, want := range []string{"GET /api/export 200", "locale=ja", "form=published", "bytes="} {
		if !strings.Contains(text, want) {
			t.Errorf("記録に %q が無い:\n%s", want, text)
		}
	}
	for _, secret := range []string{jaVanished, "もしもし", "こんにちは", "設定"} {
		if strings.Contains(text, secret) {
			t.Errorf("記録に行の中身が出ている: %q\n%s", secret, text)
		}
	}
}

// newGameWithBase はゲーム側のフォルダーを作る。
//
// 置くのは2つ。ゲームに入っている公開ファイル（土台）と、その上で Mod が
// 書き出した作業コピーである。この2つが揃って初めて Target.GameBase が埋まり、
// [publish.CheckBase] が働く。
func newGameWithBase(t *testing.T, base, working string) string {
	t.Helper()
	game := t.TempDir()
	for rel, body := range map[string]string{
		filepath.Join("Translations", "ja", "strings.csv"):             base,
		filepath.Join("Translations", "_discovered", "ja.working.csv"): working,
	} {
		path := filepath.Join(game, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return game
}

func TestExportRefusesToRollBackNewerCommits(t *testing.T) {
	// publish が書く前に見る守りは2つある。訳が失われること（CheckLoss）と、
	// ゲームに入っている翻訳が古いこと（CheckBase）である。後者を通さないと、
	// 訳は消えないまま古い版へ静かに巻き戻ったCSVを書き出せてしまう。
	// 巻き戻りは CheckLoss では捕まらない。訳は消えておらず、書き換わっただけである。
	//
	// 実際に起きた: 実データで dwloc publish は「ゲームに入っている翻訳が古い」で
	// 止まるのに、画面の書き出しはそこを見ずに先の「訳が失われる」を出していた。
	base := strings.Join([]string{
		"key,section,node,order,speaker,translation",
		// 土台はコミット済みより古い訳を持つ（repo は「もしもし？」）。
		keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし",
		"",
	}, "\n")
	working := strings.Join([]string{
		"key,section,node,order,speaker,translation",
		keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし",
		keyKept2 + ",L01 Ryan,Ryan_1_intro,2,Kobold,こんにちは！",
		keyVanished + ",L01 Ryan,Ryan_1_intro,3,Ryan," + jaVanished,
		keyUI + ",UI,,,UI,設定",
		"",
	}, "\n")
	game := newGameWithBase(t, base, working)
	s := newTestServer(t, Options{Game: game, UILang: "ja"})

	rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form=published", true, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("状態コードが %d、409 を期待:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "巻き戻") {
		t.Errorf("巻き戻りの文面になっていない: %q", rec.Body.String())
	}
	// 行の中身は出さない。件数だけで足りる。
	if strings.Contains(rec.Body.String(), "もしもし") {
		t.Errorf("誤りの文面に訳が出ている: %q", rec.Body.String())
	}

	// 編集中のファイルのままなら出せること。止めるのは publish の形のときだけで、
	// 書き出し全部ではない。
	rec = do(t, s, http.MethodGet, "/api/export?locale=ja&form=working", true, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("編集中のファイルの書き出しまで止めている: %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "ja.working.csv") {
		t.Errorf("作業コピーの名前になっていない: %q", got)
	}
}

func TestExportChecksTheBaseBeforeTheLosses(t *testing.T) {
	// 守りの順番も publish と同じにする。両方に当たる状態で失われる訳のほうを
	// 先に出すと、同じ状態に対して画面と publish が別の理由を言う。直し方は
	// 別（片方はゲームへ入れ直す、もう片方は作業コピーを作り直す）なので、
	// 順番が違うと人が別の場所を直しにいく。
	base := strings.Join([]string{
		"key,section,node,order,speaker,translation",
		keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし",
		"",
	}, "\n")
	// 作業コピーは1行しかない。このまま出せば訳も失われる。
	working := strings.Join([]string{
		"key,section,node,order,speaker,translation",
		keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし",
		"",
	}, "\n")
	game := newGameWithBase(t, base, working)
	s := newTestServer(t, Options{Game: game, UILang: "ja"})

	rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form=published", true, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("状態コードが %d、409 を期待:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "巻き戻") {
		t.Errorf("土台の食い違いより先に、失われる訳を出している: %q", rec.Body.String())
	}
}

func TestExportRefusesUnsafeShapes(t *testing.T) {
	// publish は、1行ずつ読むと訳を失う形のファイルを書く前に止める。画面の
	// 書き出しも同じところで止めないと、切り詰めた訳や黙って落ちた行を、翻訳者が
	// 自分の手でリポジトリへ写せてしまう。この形は失われる訳の確かめ（CheckLoss）
	// では捕まらない。いまの公開ファイルも同じ読み方で読むからである。
	const multiline = "ながい\nやく"
	for _, tc := range []struct {
		name string
		// working はリポジトリの作業コピー。空なら置かない。
		working string
		// published は置き換える公開ファイル。空なら newTestRoot のまま。
		published string
	}{
		{
			name: "作業コピーの訳が行をまたぐ",
			working: strings.Join([]string{
				"key,section,node,order,speaker,translation",
				keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？",
				keyKept2 + ",L01 Ryan,Ryan_1_intro,2,Kobold,こんにちは！",
				keyVanished + ",L01 Ryan,Ryan_1_intro,3,Ryan," + jaVanished,
				keyUI + ",UI,,,UI,\"" + multiline + "\"",
				"",
			}, "\n"),
		},
		{
			name: "いまの公開ファイルの訳が行をまたぐ",
			published: strings.Join([]string{
				"key,section,node,order,speaker,translation",
				keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"" + multiline + "\"",
				"",
			}, "\n"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newTestRoot(t)
			for rel, body := range map[string]string{
				filepath.Join("Translations", "_discovered", "ja.working.csv"): tc.working,
				filepath.Join("Translations", "ja", "strings.csv"):             tc.published,
			} {
				if body == "" {
					continue
				}
				path := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			s := newTestServer(t, Options{Root: root, UILang: "ja"})

			rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form=published", true, nil)
			if rec.Code != http.StatusConflict {
				t.Fatalf("状態コードが %d、409 を期待:\n%s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if want := s.cat.T(s.cat.lookup("ja"), "error.export_unsafe_shape", "count", "1"); !strings.Contains(body, want) {
				t.Errorf("形の文面になっていない: %q", body)
			}
			// どのファイルの何行目かはパスを含むので出さない。訳の中身も出さない。
			for _, leak := range []string{root, filepath.ToSlash(root), ".csv", "ながい"} {
				if strings.Contains(body, leak) {
					t.Errorf("誤りの文面に %q が出ている: %q", leak, body)
				}
			}
			// 編集中のファイルはそのまま出せる。止めるのは publish の形だけである。
			rec = do(t, s, http.MethodGet, "/api/export?locale=ja&form=working", true, nil)
			if rec.Code != http.StatusOK {
				t.Errorf("編集中のファイルの書き出しまで止めている: %d", rec.Code)
			}
		})
	}
}

func TestExportChecksTheShapeBeforeTheBase(t *testing.T) {
	// 守りの順番は publish と同じ（形 → 土台の食い違い → 失われる訳）。
	// 形の崩れたファイルは、あとの2つの確かめも読み違えるので、先に止める。
	base := strings.Join([]string{
		"key,section,node,order,speaker,translation",
		// 土台はコミット済みより古い訳を持つ（repo は「もしもし？」）。
		keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし",
		"",
	}, "\n")
	working := strings.Join([]string{
		"key,section,node,order,speaker,translation",
		keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もしもし\n？\"",
		"",
	}, "\n")
	game := newGameWithBase(t, base, working)
	s := newTestServer(t, Options{Game: game, UILang: "ja"})

	rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form=published", true, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("状態コードが %d、409 を期待:\n%s", rec.Code, rec.Body.String())
	}
	if want := s.cat.T(s.cat.lookup("ja"), "error.export_unsafe_shape", "count", "1"); !strings.Contains(rec.Body.String(), want) {
		t.Errorf("土台の食い違いより先に形を見ていない: %q", rec.Body.String())
	}
}

// TestExportPublishedWritesTheFirstFileOfANewLocaleFromTheGame は、公開ファイルが
// まだ無いロケールでも、ゲーム側の作業コピーから公開の形を書き出せることを見る。
//
// Translations/ja はあるが strings.csv は無い。新しい言語を始めた翻訳者が
// ディレクトリだけを作り、訳はゲームの中で入れている、という形である。
// 以前は土台の確かめ（[publish.CheckBase]）の前にコミット済みの公開ファイルを
// 読み、無いことを読めないことと同じに扱って 500 を返していた。コミット済みが
// 無ければ巻き戻る先も無いので、確かめるものが無い。dwloc publish も同じ状態を
// 通す（cmd/dwloc の TestPublishWritesTheFirstFileOfANewLocaleFromTheGame）。
// 片方だけが止めると、同じ状態で publish は書けるのに画面からは書き出せない。
func TestExportPublishedWritesTheFirstFileOfANewLocaleFromTheGame(t *testing.T) {
	working := strings.Join([]string{
		"key,section,node,order,speaker,translation",
		keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？",
		keyKept2 + ",L01 Ryan,Ryan_1_intro,2,Kobold,こんにちは！",
		"",
	}, "\n")
	for _, tc := range []struct {
		name string
		// gameBase はゲーム側の Translations/ja/strings.csv。空なら置かない。
		gameBase string
	}{
		{"ゲーム側にも公開ファイルが無い", ""},
		{"ゲーム側には公開ファイルがある", strings.Join([]string{
			"key,section,node,order,speaker,translation",
			keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし",
			"",
		}, "\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newTestRoot(t)
			published := filepath.Join(root, "Translations", "ja", "strings.csv")
			if err := os.Remove(published); err != nil {
				t.Fatal(err)
			}
			game := t.TempDir()
			files := map[string]string{
				filepath.Join("Translations", "_discovered", "ja.working.csv"): working,
			}
			if tc.gameBase != "" {
				files[filepath.Join("Translations", "ja", "strings.csv")] = tc.gameBase
			}
			for rel, body := range files {
				path := filepath.Join(game, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			s := newTestServer(t, Options{Root: root, Game: game, UILang: "ja"})
			if s.target("ja") == nil || s.target("ja").GameBase == "" {
				t.Fatal("前提が崩れている。ゲーム側の作業コピーを読んでいない")
			}

			rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form=published", true, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("状態コードが %d、200 を期待:\n%s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="strings.csv"` {
				t.Errorf("Content-Disposition が %q", got)
			}
			body := rec.Body.String()
			for _, want := range []string{keyKept, "もしもし？", keyKept2, "こんにちは！"} {
				if !strings.Contains(body, want) {
					t.Errorf("書き出したものに %q が無い:\n%s", want, body)
				}
			}
			// 書き出しは中身を返すだけで、リポジトリへは1バイトも書かない。
			if _, err := os.Stat(published); !os.IsNotExist(err) {
				t.Errorf("書き出しなのに公開ファイルができている（err = %v）", err)
			}
		})
	}
}

func TestExportFailsWithoutLeakingThePath(t *testing.T) {
	// 起動したあとに読めなくなったときは 500 を返す。誤りの中身（パスを含む）は
	// 返さず、端末の記録にもロケールと形しか書かない。どこで止まったかは、
	// 記録の1行と、試せる形がもう1つあること（working / published）で追える。
	//
	// ゲーム側の2つは、公開の形にする前に見る土台の確かめ（[publish.CheckBase]）が
	// 読めなかった場合である。確かめられないまま出すと、巻き戻ったCSVを
	// 書き出せてしまう。dwloc publish も同じ場面で書かずに終わる。
	sameAsRepo := strings.Join([]string{
		"key,section,node,order,speaker,translation",
		keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？",
		"",
	}, "\n")
	breakWith := func(t *testing.T, path string, dir bool) {
		t.Helper()
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if !dir {
			return
		}
		// 同じ名前のディレクトリを置く。あるのに読めない、という形になる。
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name   string
		form   string
		game   bool
		damage func(t *testing.T, s *server, root string)
	}{
		{"編集中のファイルが消えた", exportFormWorking, false, func(t *testing.T, s *server, _ string) {
			breakWith(t, inputPath(t, s, "ja"), false)
		}},
		{"公開の形: 入力が消えた", exportFormPublished, false, func(t *testing.T, s *server, _ string) {
			breakWith(t, inputPath(t, s, "ja"), false)
		}},
		{"公開の形: 再生順を読めない", exportFormPublished, false, func(t *testing.T, _ *server, root string) {
			breakWith(t, filepath.Join(root, "data", "script_order.csv"), true)
		}},
		// 消えた（無い）のではなく、あるのに読めない。無いときは新しい言語の最初の
		// 書き出しとして通す（TestExportPublishedWritesTheFirstFileOfANewLocaleFromTheGame）。
		{"公開の形: コミット済みの公開ファイルを読めない", exportFormPublished, true, func(t *testing.T, s *server, _ string) {
			breakWith(t, s.target("ja").Output, true)
		}},
		{"公開の形: ゲーム側の土台を読めない", exportFormPublished, true, func(t *testing.T, s *server, _ string) {
			breakWith(t, s.target("ja").GameBase, true)
		}},
		// 読めるが、列名が重複していてどの列が訳かを決められない。入力が作業
		// コピーなら組み立ては通る（書き出し先はコメントを写すためにしか読まない）
		// ので、形の確かめで止まる。確かめられないまま出すと、失われる訳を見落とす。
		{"公開の形: コミット済みの公開ファイルの列名が重複している", exportFormPublished, true, func(t *testing.T, s *server, _ string) {
			dup := "key,Key,section,node,order,speaker,translation\n" +
				keyKept + "," + keyKept + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？\n"
			if err := os.WriteFile(s.target("ja").Output, []byte(dup), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var log strings.Builder
			root := newTestRoot(t)
			opt := Options{Root: root, UILang: "ja", Stderr: &log}
			if tc.game {
				opt.Game = newGameWithBase(t, sameAsRepo, sameAsRepo)
			}
			s := newTestServer(t, opt)
			if tc.game && s.target("ja").GameBase == "" {
				t.Fatal("前提が崩れている。ゲーム側の作業コピーを読んでいない")
			}
			tc.damage(t, s, root)

			rec := do(t, s, http.MethodGet, "/api/export?locale=ja&form="+tc.form, true, nil)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("状態コードが %d、500 を期待:\n%s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if want := s.cat.T(s.cat.lookup("ja"), "error.export_failed"); !strings.Contains(body, want) {
				t.Errorf("書き出せなかったと言っていない: %q", body)
			}
			if got := rec.Header().Get("Content-Disposition"); got != "" {
				t.Errorf("失敗したのに保存させようとしている: %q", got)
			}
			for _, leak := range []string{root, filepath.ToSlash(root), ".csv"} {
				if strings.Contains(body, leak) || strings.Contains(log.String(), leak) {
					t.Errorf("応答か記録にパスが出ている（%q）\n応答: %q\n記録: %q", leak, body, log.String())
				}
			}
			if want := "export failed locale=ja form=" + tc.form; !strings.Contains(log.String(), want) {
				t.Errorf("記録に %q が無い: %q", want, log.String())
			}
		})
	}
}

func TestExportWriteFailureIsRecordedWithoutContent(t *testing.T) {
	// 送っている途中で切れた（保存のダイアログで取り消した、タブを閉じた）。
	// 記録には書けなかったことだけを残し、CSV の中身は書かない。
	var log strings.Builder
	s := newTestServer(t, Options{UILang: "ja", Stderr: &log})
	w := newBrokenWriter()

	s.handleExport(w, httptest.NewRequest(http.MethodGet, "/api/export?locale=ja&form=working", nil))

	if !strings.Contains(log.String(), "dwloc edit: write failed") {
		t.Errorf("書けなかったことが記録に無い: %q", log.String())
	}
	for _, secret := range []string{jaVanished, "もしもし", "設定", keyKept} {
		if strings.Contains(log.String(), secret) {
			t.Errorf("記録に行の中身が出ている（%q）: %q", secret, log.String())
		}
	}
}
