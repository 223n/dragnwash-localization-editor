package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// multiSource は、行をまたぐ原文。ゲーム側の作業コピーの実物にある形
// （値の中に LF が2つ、間に空行）をまねた架空の文である。
const multiSource = "para1\n\npara2"

// TestPublishStopsOnUnsafeShapes は、1行ずつ読むと訳を失う形のファイルで
// 止まることを見る。
//
// どれも守りを入れる前は終了コード 0 で書き出し、訳を切り詰めたり落としたり
// していた（最後の1つだけは、失われる訳の確認が別の文面で止めていた）。
// いまの公開ファイルも同じ読み方で読むので、失われる訳の確認では捕まらない。
func TestPublishStopsOnUnsafeShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		// published はリポジトリの Translations/ja/strings.csv。空なら lossRepo のまま。
		published string
		// working はゲーム側の作業コピー。空なら置かず、--no-game で走らせる。
		working string
		// game はゲームに入っている公開ファイル。空なら置かない。
		game string
		// want は標準エラーに出ていてほしい文字列。
		want []string
		// notWant は標準エラーに出ていてはいけない文字列。
		notWant []string
	}{
		{
			name: "いまの公開ファイルの訳が行をまたぐ",
			published: "key,section,node,order,speaker,translation\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もしもし\nもしもし？\"\n",
			// 直し方は、訳を壊さない抜け方を先に出す。複数行の訳は上流では正しい訳で、
			// 改行を取り除かせるとほかの翻訳者の訳を壊す。
			want: []string{
				"ja: " + jaPublishedPath + "（いまの公開ファイル）",
				"2〜3行目: 引用符で囲んだ値が行をまたいでいる",
				"直し方: このロケールは、いまの dwloc publish では書けません。",
				"ほかのロケールは、このロケール以外を --locale に並べれば publish できます。",
				"このロケールは、tools/hash-strings.ps1 かゲーム内の Hash for commit で書けます。",
				"誤って入った改行なら、取り除いて",
			},
		},
		{
			name: "作業コピーの原文が行をまたぎ、訳が入っている",
			working: "key,section,node,order,speaker,source_en,translation\r\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,もしもし？\r\n" +
				key.For(multiSource) + ",UI,,,UI,\"" + multiSource + "\",訳\r\n",
			// 訳は翻訳者が入れた正しい訳でありうるので、空に戻すのは条件付きの案内に
			// 留め、訳を保ったまま抜ける道を先に出す。
			want: []string{
				"ja.working.csv（入力）",
				"3〜5行目: 引用符で囲んだ値が行をまたぎ、この行には訳が入っている",
				"直し方: この行に訳があるうちは、このロケールをいまの dwloc publish では書けません。",
				"このロケール以外を --locale に並べれば",
				"tools/hash-strings.ps1 かゲーム内の Hash for commit で書けます。",
				"この訳をまだ公開しなくてよいなら、訳を空に戻すと、このロケールのほかの行は",
			},
		},
		{
			name: "作業コピーの引用符が閉じない",
			working: "key,source_en,translation\n" +
				keyHello + ",Hello?,\"もしもし？\n" +
				keyHiThere + ",Hi there!,やあ！\n",
			want: []string{
				"2〜3行目: 開いた引用符がファイルの終わりまで閉じない",
				`値の中の " は "" と2つ重ねて書きます`,
			},
		},
		{
			// 以前は失われる訳の確認が「訳が失われる」で止めていた。いまは原因の
			// ヘッダーを指す。
			name:    "作業コピーのヘッダーの引用符が閉じない",
			working: workingBadHeader,
			want: []string{
				"1行目: ヘッダーに translation 列が無い",
				"1〜2行目: 開いた引用符がファイルの終わりまで閉じない",
				"読み違える形が 2 か所あります。",
			},
		},
		{
			// 形の崩れとゲーム側の古さが同時にあるときは、形で止め、土台の報告は
			// 出さない。確かめる順（形 → ゲーム側の土台 → 失われる訳）を固定する。
			// 形の崩れたファイルは土台の確かめも読み違えるので、先に見る必要がある。
			name: "作業コピーの形が崩れ、ゲーム側も古い",
			working: "key,section,node,order,speaker,source_en,translation\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,もしもし\n" +
				keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,Hi there!,\"やあ\nやあ！\"\n",
			// コミット済みの「もしもし？」に対して、ゲーム側は古い「もしもし」を持つ。
			game: "key,section,node,order,speaker,translation\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし\n",
			want: []string{
				"ja.working.csv（入力）",
				"3〜4行目: 引用符で囲んだ値が行をまたぎ、この行には訳が入っている",
				"読み違える形が 1 か所あります。",
			},
			notWant: []string{"ゲームに入っている翻訳が古いので"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := lossRepo(t)
			if tc.published != "" {
				if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(jaPublishedPath)),
					[]byte(tc.published), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"publish", "--root", root, "--no-game"}
			if tc.working != "" {
				files := map[string]string{"Translations/_discovered/ja.working.csv": tc.working}
				if tc.game != "" {
					files[jaPublishedPath] = tc.game
				}
				game := makeGame(t, files)
				args = []string{"publish", "--root", root, "--game", game}
			}
			before := readFile(t, root, jaPublishedPath)

			_, _, wet := runCLI(args...)
			code, stdout, stderr := runCLI(append(args, "--dry-run")...)
			if code != exitProblems {
				t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
			}
			// --dry-run でも同じ判定をして、同じ報告を出す。
			if stderr != wet {
				t.Errorf("報告が --dry-run で変わっている\n--- dry-run ---\n%s\n--- 本番 ---\n%s", stderr, wet)
			}
			checkContains(t, "標準エラー", stderr, append([]string{
				"1行ずつ読むと訳を失う形のファイルがあるので、1バイトも書きませんでした。",
				"直すまでは書きません。",
			}, tc.want...))
			for _, s := range tc.notWant {
				if strings.Contains(stderr, s) {
					t.Errorf("標準エラーに %q が出ている:\n%s", s, stderr)
				}
			}
			if stdout != "" {
				t.Errorf("止めたのに標準出力へ書いている:\n%s", stdout)
			}
			if after := readFile(t, root, jaPublishedPath); after != before {
				t.Errorf("公開ファイルが変わっている\n--- 前 ---\n%s\n--- 後 ---\n%s", before, after)
			}
		})
	}
}

// TestPublishPassesTheRealWorkingCopyShape は、ゲーム側の作業コピーの実物と同じ
// 形では止まらず、書き出す中身も変わらないことを見る。
//
// 実物には、原文が行をまたぎ（区切りは CRLF、値の中は LF、空行を挟む）、訳が
// 空のレコードが1件ある。どちらの読み方でも公開されない行なので、止める理由が
// 無い。ここで止めると、翻訳者には直せない理由で ja の publish が常に塞がる。
func TestPublishPassesTheRealWorkingCopyShape(t *testing.T) {
	head := "key,section,node,order,speaker,source_en,translation\r\n" +
		keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,もしもし？\r\n"
	tail := keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,Hi there!,やあ！\r\n"
	multi := key.For(multiSource) + ",UI,,,UI,\"" + multiSource + "\",\r\n"

	publishWith := func(working string) (string, string) {
		t.Helper()
		root := lossRepo(t)
		publishOnce(t, root)
		game := makeGame(t, map[string]string{"Translations/_discovered/ja.working.csv": working})
		code, stdout, stderr := runCLI("publish", "--root", root, "--game", game)
		if code != exitOK {
			t.Fatalf("終了コード = %d\n%s", code, stderr)
		}
		return readFile(t, root, jaPublishedPath), stdout
	}

	got, stdout := publishWith(head + multi + tail)
	want, _ := publishWith(head + tail)
	if got != want {
		t.Errorf("行をまたぐ訳の空のレコードで出力が変わった\n--- あり ---\n%s\n--- なし ---\n%s", got, want)
	}
	// 1行ずつ読むとそのレコードは2件の malformed として捨てられる。実物の集計と同じ形。
	checkContains(t, "標準出力", stdout, []string{"2 malformed dropped"})
}

// TestPublishShapeReportIsCapped は、形の崩れが多いときの報告を見る。
//
// 一覧は先頭の publishShapeListMax 件で切り、残りは件数で伝える。失われる訳の
// 報告（publishLossListMax）と同じく、先に出した「なぜ止めたか」が流れて消えない
// ようにするためである。止めたときは、どのロケールも書かない。
func TestPublishShapeReportIsCapped(t *testing.T) {
	var b strings.Builder
	b.WriteString("key,section,node,order,speaker,translation\n")
	total := publishShapeListMax + 3
	for i := range total {
		fmt.Fprintf(&b, "%s,UI,,,UI,\"訳%02d\nつづき\"\n", key.For(fmt.Sprintf("Line %02d", i)), i)
	}
	jaPublished := b.String()
	dePublished := "key,section,node,order,speaker,translation\n" + helloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hallo\n"
	root := makeTree(t, map[string]string{
		"data/script_order.csv":       scriptOrderCSV,
		"Translations/de/strings.csv": dePublished,
		"Translations/ja/strings.csv": jaPublished,
	})

	code, stdout, stderr := runCLI("publish", "--root", root, "--no-game")
	if code != exitProblems {
		t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{
		fmt.Sprintf("ほかに %d か所あります。", total-publishShapeListMax),
		fmt.Sprintf("読み違える形が %d か所あります。", total),
	})
	if got := strings.Count(stderr, "直し方: "); got != publishShapeListMax {
		t.Errorf("並べた件数が %d、%d を期待\n%s", got, publishShapeListMax, stderr)
	}
	// 見出しはファイルごとに1回だけ出す。
	if got := strings.Count(stderr, "（いまの公開ファイル）"); got != 1 {
		t.Errorf("ファイルの見出しが %d 回出ている\n%s", got, stderr)
	}
	if stdout != "" {
		t.Errorf("止めたのに標準出力へ書いている:\n%s", stdout)
	}
	if got := readFile(t, root, "Translations/de/strings.csv"); got != dePublished {
		t.Errorf("形に問題の無い de だけ書いている:\n%s", got)
	}
}

// TestPublishShapeWithPathHasNoLocale は、--path で走らせたときの報告を見る。
// ロケールが決まらないので頭に付けず、ファイルの名前だけを出す。
func TestPublishShapeWithPathHasNoLocale(t *testing.T) {
	root := lossRepo(t)
	path := filepath.Join(root, filepath.FromSlash(jaPublishedPath))
	if err := os.WriteFile(path, []byte("key,translation\n"+keyHello+",\"a\nb\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLI("publish", "--root", root, "--path", path)
	if code != exitProblems {
		t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{
		"dwloc:   " + jaPublishedPath + "（いまの公開ファイル）",
		// --path と --locale は同時に使えないので、外し方は --path で案内する。
		"直し方: このファイルは、いまの dwloc publish では書けません。",
		"ほかのファイルは、このファイルを --path から外せば publish できます。",
	})
	if strings.Contains(stderr, "ja: ") {
		t.Errorf("--path なのにロケールを付けている:\n%s", stderr)
	}
	if strings.Contains(stderr, "--locale") || strings.Contains(stderr, "このロケール") {
		t.Errorf("--path なのにロケールで外すよう案内している:\n%s", stderr)
	}
}

// TestPublishShapeEscapeWithLocale は、行をまたぐ値で止まったときの直し方が
// 案内する抜け方で、実際にほかのロケールを書けることを見る。
//
// 複数行の訳は上流では正しい訳なので、いまの公開ファイルにあると、そのロケールは
// 全体を解釈する読み手が入るまで止まり続ける。--locale を付けずに走らせると
// どのロケールも書かない。案内どおり --locale でほかのロケールだけを選べば書け、
// 止まったロケールの公開ファイルには触れない。
func TestPublishShapeEscapeWithLocale(t *testing.T) {
	dePublished := "key,section,node,order,speaker,translation\n" +
		helloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"Hallo\nWelt\"\n"
	jaPublished := "key,section,node,order,speaker,translation\n" +
		helloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし\n"
	root := makeTree(t, map[string]string{
		"data/script_order.csv":       scriptOrderCSV,
		"Translations/de/strings.csv": dePublished,
		"Translations/ja/strings.csv": jaPublished,
	})
	// 案内どおりに抜けたときと比べるため、ja だけを書いたときの中身を先に作る。
	want := makeTree(t, map[string]string{
		"data/script_order.csv":       scriptOrderCSV,
		"Translations/ja/strings.csv": jaPublished,
	})
	if code, _, stderr := runCLI("publish", "--root", want, "--no-game"); code != exitOK {
		t.Fatalf("下ごしらえの publish の終了コード = %d\n%s", code, stderr)
	}

	code, _, stderr := runCLI("publish", "--root", root, "--no-game")
	if code != exitProblems {
		t.Fatalf("--locale なしの終了コード = %d、1 を期待\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{"de: Translations/de/strings.csv（いまの公開ファイル）"})
	if got := readFile(t, root, "Translations/ja/strings.csv"); got != jaPublished {
		t.Errorf("止めたのに ja を書いている:\n%s", got)
	}

	code, _, stderr = runCLI("publish", "--root", root, "--no-game", "--locale", "ja")
	if code != exitOK {
		t.Fatalf("--locale ja の終了コード = %d、0 を期待\n%s", code, stderr)
	}
	if got, w := readFile(t, root, "Translations/ja/strings.csv"), readFile(t, want, "Translations/ja/strings.csv"); got != w {
		t.Errorf("ja の中身が違う\n got %q\nwant %q", got, w)
	}
	if got := readFile(t, root, "Translations/de/strings.csv"); got != dePublished {
		t.Errorf("外した de を書き換えている:\n%s", got)
	}
}

// TestPublishShapeFixCoversEveryReason は、形の理由ごとに直し方があることを見る。
// 直し方が無いと、報告に「直し方: 」だけが出る。
func TestPublishShapeFixCoversEveryReason(t *testing.T) {
	for _, id := range reason.All() {
		if !strings.HasPrefix(id, "publish_") {
			continue
		}
		_, has := publishShapeFix[id]
		switch id {
		case reason.PublishRowGone, reason.PublishTranslationCleared, reason.PublishBaseDrift:
			// 形の理由ではない。直し方は見出しの文面が受け持つ。
			if has {
				t.Errorf("%s は形の理由ではないのに直し方がある", id)
			}
		default:
			if !has || publishShapeFix[id] == "" {
				t.Errorf("%s の直し方が無い", id)
			}
			// 埋め残しがあると、報告に {this} がそのまま出る。
			for _, locale := range []string{"ja", ""} {
				fix := shapeFix(publish.Hazard{Locale: locale, Why: reason.New(id, "")})
				if strings.ContainsAny(fix, "{}") {
					t.Errorf("%s（ロケール %q）の直し方に埋め残しがある: %s", id, locale, fix)
				}
			}
		}
	}
}

// TestShapeFixNamesTheWayOut は、行をまたぐ値の直し方が、ロケールを決めて
// 走らせたか --path で走らせたかに合わせて抜け方を変えることを固定する。
func TestShapeFixNamesTheWayOut(t *testing.T) {
	for _, id := range []string{
		reason.PublishMultilineCurrent, reason.PublishMultilineTranslated, reason.PublishMultilineDiverges,
	} {
		byLocale := shapeFix(publish.Hazard{Locale: "ja", Why: reason.New(id, "")})
		byPath := shapeFix(publish.Hazard{Why: reason.New(id, "")})
		if !strings.Contains(byLocale, "--locale") || strings.Contains(byLocale, "--path") {
			t.Errorf("%s: ロケールで走らせたのに --locale で案内していない: %s", id, byLocale)
		}
		if !strings.Contains(byPath, "--path") || strings.Contains(byPath, "--locale") {
			t.Errorf("%s: --path で走らせたのに --path で案内していない: %s", id, byPath)
		}
		for _, fix := range []string{byLocale, byPath} {
			if !strings.Contains(fix, "tools/hash-strings.ps1") {
				t.Errorf("%s: 上流の道具を案内していない: %s", id, fix)
			}
			// 上流の道具は、どちらもゲーム側の作業コピーをそのまま公開ファイルへ
			// 届けない。前提を添えないと、案内どおりに走らせて終了コード0で
			// 終わっても訳が入っていない。
			if !strings.Contains(fix, publishToolNote) {
				t.Errorf("%s: 上流の道具で書くときの前提（写す先）を添えていない: %s", id, fix)
			}
		}
	}
	// 形が壊れているだけのものは、ほかのロケールの話をしない。
	if fix := shapeFix(publish.Hazard{Locale: "ja", Why: reason.New(reason.PublishUnclosedQuote, "")}); strings.Contains(fix, "--locale") {
		t.Errorf("閉じない引用符の直し方に --locale が出ている: %s", fix)
	}
}

// TestLineRange は、報告に出す行の範囲の書き方を固定する。
func TestLineRange(t *testing.T) {
	for _, tc := range []struct {
		line, end int
		want      string
	}{
		{0, 0, "ファイル全体"},
		{3, 3, "3行目"},
		{3, 0, "3行目"},
		{3, 5, "3〜5行目"},
	} {
		if got := lineRange(tc.line, tc.end); got != tc.want {
			t.Errorf("lineRange(%d, %d) = %q, want %q", tc.line, tc.end, got, tc.want)
		}
	}
}

// TestReportLossesStillRefusesWhatItCannotRead は、失われる訳の確認が、読めない
// ファイルを「失われない」と扱わないことを見る。
//
// 読めないファイルは、ふつうは先に走る形の確認（reportShape）が終了コード 2 で
// 止める（TestPublishReportsWhenTheCurrentFileCannotBeChecked）。それでも
// こちらの確認も同じ扱いを保つ。2つの確認のあいだにファイルが書き換わることは
// あり、そのときに素通りさせないためである。
func TestReportLossesStillRefusesWhatItCannotRead(t *testing.T) {
	root := t.TempDir()
	// 書き出し先がディレクトリ。あるのに読めない、という形になる。
	target := publish.Target{Locale: "ja", Input: filepath.Join(root, "in.csv"), Output: root}
	var stderr strings.Builder
	code := reportLosses(root, []publish.Target{target}, [][]byte{[]byte(publish.HeaderLine + "\n")}, &stderr)
	if code != exitError {
		t.Fatalf("終了コード = %d、2 を期待\n%s", code, stderr.String())
	}
	checkContains(t, "標準エラー", stderr.String(), []string{"訳が失われないことを確かめられません"})
}

// TestPublishShapeReportsWholeFile は、行の区切りを読み違えたファイルで
// 「ファイル全体」を指して止まることを見る。
func TestPublishShapeReportsWholeFile(t *testing.T) {
	root := lossRepo(t)
	game := makeGame(t, map[string]string{
		// U+2028 で行を分けた作業コピー。読み手には1行に見える。
		"Translations/_discovered/ja.working.csv": "key,source_en,translation " +
			keyHello + ",Hello?,もしもし？ ",
	})
	code, _, stderr := runCLI("publish", "--root", root, "--game", game)
	if code != exitProblems {
		t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{
		"ファイル全体: 空でない行があるのに、1行ずつ読むと1行も読めない",
		"直し方: 改行を LF か CRLF にして保存し直して",
	})
}
