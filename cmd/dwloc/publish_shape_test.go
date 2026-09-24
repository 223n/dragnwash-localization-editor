package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
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
		// want は標準エラーに出ていてほしい文字列。
		want []string
	}{
		{
			name: "いまの公開ファイルの訳が行をまたぐ",
			published: "key,section,node,order,speaker,translation\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もしもし\nもしもし？\"\n",
			want: []string{
				"ja: " + jaPublishedPath + "（いまの公開ファイル）",
				"2〜3行目: 引用符で囲んだ値が行をまたいでいる",
				"直し方: その値の改行を取り除いて",
			},
		},
		{
			name: "作業コピーの原文が行をまたぎ、訳が入っている",
			working: "key,section,node,order,speaker,source_en,translation\r\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,もしもし？\r\n" +
				key.For(multiSource) + ",UI,,,UI,\"" + multiSource + "\",訳\r\n",
			want: []string{
				"ja.working.csv（入力）",
				"3〜5行目: 引用符で囲んだ値が行をまたぎ、この行には訳が入っている",
				"直し方: その行の訳を空に戻すと",
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
				game := makeGame(t, map[string]string{
					"Translations/_discovered/ja.working.csv": tc.working,
				})
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
	checkContains(t, "標準エラー", stderr, []string{"dwloc:   " + jaPublishedPath + "（いまの公開ファイル）"})
	if strings.Contains(stderr, "ja: ") {
		t.Errorf("--path なのにロケールを付けている:\n%s", stderr)
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
		}
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
