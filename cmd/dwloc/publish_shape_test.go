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

// shapeStopText は、読み違える形で止まったときの見出しの1行目。
const shapeStopText = "読み違える形のファイルがあるので、1バイトも書きませんでした。"

// TestPublishStopsOnUnsafeShapes は、読むと訳や原文を取り違える形のファイルで
// 止まることを見る。
//
// 全体を解釈して読むと、引用符の閉じ誤りは後ろの行（英語の原文やほかの行の
// キー）を値に飲み込む。上流の hash-strings.ps1 はそのまま公開ファイルへ書く
// （上流の報告 #11）。いまの公開ファイルの訳は消えないので、失われる訳の確認では
// 捕まらない。
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
			// 入力と書き出し先が同じ経路（作業コピーの無いロケール）でも止まる。
			// 同じファイルの中で訳を比べても、飲み込みは見えない（批評の high）。
			name: "いまの公開ファイルで引用符が別の行で閉じる",
			published: "key,section,node,order,speaker,translation\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もしもし\n" +
				keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,やあ！\"\n",
			want: []string{
				"ja: " + jaPublishedPath + "（いまの公開ファイル）",
				"2〜3行目: 3行目が、単独で読むとキーの形で始まるレコードに見える。",
				`直し方: 引用符の閉じ位置を確かめてください。値を閉じる " が抜けていれば足し、値の中の " は "" と2つ重ねて書きます。`,
				"3行目が値の一部として正しい（正しい複数行の値）なら、確かめたうえで --accept-multiline ja を付けると書けます。",
			},
		},
		{
			// 飲み込まれた行が自分の値を引用符で開く形。どの書き手も作らないので、
			// 通す指定は案内しない。
			name: "作業コピーで閉じ引用符の後ろに文字が続く",
			working: "source_en,translation\n" +
				"Hello?,\"もしもし\n" +
				"\"Alpha line\nBeta line\",に\n",
			want: []string{
				"ja.working.csv（入力）",
				"2〜3行目: 3行目で、行をまたいだ引用が閉じたすぐ後ろに文字が続く。",
				`3行目の " が、前の行で開いた値を閉じています。`,
			},
			notWant: []string{"--accept-multiline"},
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
			// ヘッダーの中で開いた引用符が閉じない。列名にファイルの終わりまでが入るので、
			// 「translation 列が無い」とは言わず、閉じない引用符だけを出す。
			name:    "作業コピーのヘッダーの引用符が閉じない",
			working: workingBadHeader,
			want: []string{
				"1〜2行目: 開いた引用符がファイルの終わりまで閉じない",
				"読み違える形が 1 か所あります。",
			},
			notWant: []string{"translation 列が無い"},
		},
		{
			// 上流はこのヘッダーを飛ばし、次の行をヘッダーにして、訳を1行も公開しない。
			name: "作業コピーの最初の列名が '#' で始まる",
			working: "\"#key\",source_en,translation\n" +
				keyHello + ",Hello?,もしもし？\n",
			want: []string{
				"1行目: ヘッダーの最初の列名が '#' で始まる。",
				"直し方: ヘッダーの最初の列名から '#' を取り除き",
			},
		},
		{
			name: "作業コピーの訳の中の単独の CR",
			working: "key,source_en,translation\n" +
				keyHello + ",Hello?,\"もし\rもし？\"\n",
			want: []string{
				"2〜3行目: translation 列の値に単独の CR（後ろに LF の続かない CR）がある。",
				"直し方: 値の中の単独の CR を LF に直すか取り除いてから",
			},
		},
		{
			// 引用符で囲まない値が単独の CR で切れる。ゲームは「もしもし？」と読むが、
			// そのまま書くと「もし」だけが公開される。
			name: "作業コピーの値が単独の CR で切れる",
			working: "key,source_en,translation\n" +
				keyHello + ",Hello?,もし\rもし？\n",
			want: []string{
				"2行目: 引用符で囲まない値が行の終わりの単独の CR で切れ、3行目にある続きが別の行として読まれる。",
				"直し方: 3行目の手前（前の行の終わり）にある CR を取り除くか、値全体を引用符で囲んで",
			},
		},
		{
			// ゲーム側の公開ファイルも同じ確かめを通す（決まったことの 3）。土台の確かめが
			// 読み違えると、巻き戻りを見逃す。直し方は、コミット済みを写し直すことを先に出す。
			name: "ゲーム側の公開ファイルの引用符が閉じない",
			working: "key,section,node,order,speaker,source_en,translation\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,もしもし？\n",
			game: "key,section,node,order,speaker,translation\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もしもし？\n",
			want: []string{
				"（ゲーム側の公開ファイル）",
				"2行目: 開いた引用符がファイルの終わりまで閉じない",
				"直し方: ゲーム側の公開ファイルは、リポジトリの Translations/<ロケール>/strings.csv を同じ場所へ写し直すと直ります。",
			},
			notWant: []string{"ゲームに入っている翻訳が古いので"},
		},
		{
			// 形の崩れとゲーム側の古さが同時にあるときは、形で止め、土台の報告は
			// 出さない。確かめる順（形 → 組み立て → ゲーム側の土台 → 失われる訳）を固定する。
			// 形の崩れたファイルは土台の確かめも読み違えるので、先に見る必要がある。
			name: "作業コピーの形が崩れ、ゲーム側も古い",
			working: "key,section,node,order,speaker,source_en,translation\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,もしもし\n" +
				keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,Hi there!,\"やあ\rやあ！\"\n",
			// コミット済みの「もしもし？」に対して、ゲーム側は古い「もしもし」を持つ。
			game: "key,section,node,order,speaker,translation\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし\n",
			want: []string{
				"ja.working.csv（入力）",
				"3〜4行目: translation 列の値に単独の CR",
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
				shapeStopText,
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

// TestPublishWritesMultilineValues は、引用符で囲んだ正しい複数行の値を、上流 main と
// 同じく1つの値として読み、読んだとおりに書くことを見る。
//
// 行単位で読んでいたときは、行をまたぐ訳は1行目で切り詰められるので、形の確かめで
// 止めていた（いまの公開ファイルなら必ず、入力なら訳が入っていれば）。値の中の改行は
// LF も CRLF も直さずに書く（上流とバイト一致させるため）。
func TestPublishWritesMultilineValues(t *testing.T) {
	root := lossRepo(t)
	publishOnce(t, root)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": "key,section,node,order,speaker,source_en,translation\r\n" +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,\"もし\r\nもし？\"\r\n" +
			keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,Hi there!,\"や\n\n# あ！\"\r\n" +
			key.For(multiSource) + ",UI,,,UI,\"" + multiSource + "\",段落\r\n",
	})
	code, stdout, stderr := runCLI("publish", "--root", root, "--game", game)
	if code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}
	got := readFile(t, root, jaPublishedPath)
	checkContains(t, "公開ファイル", got, []string{
		keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もし\r\nもし？\"\n",
		keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,\"や\n\n# あ！\"\n",
		key.For(multiSource) + ",UI,,,UI,段落\n",
	})
	checkContains(t, "標準出力", stdout, []string{"3 converted", "0 malformed dropped"})

	// 書いた公開ファイルを入力にしてもう一度通すと、同じバイト列が返る（冪等）。
	code, _, stderr = runCLI("publish", "--root", root, "--no-game")
	if code != exitOK {
		t.Fatalf("2回目の終了コード = %d\n%s", code, stderr)
	}
	if again := readFile(t, root, jaPublishedPath); again != got {
		t.Errorf("2回目で公開ファイルが変わった\n--- 1回目 ---\n%q\n--- 2回目 ---\n%q", got, again)
	}
}

// TestPublishHintsCRLFSource は、表計算ソフトなどで保存し直して原文の LF が CRLF に
// なった行を、止めずに知らせることを見る（決まったことのそのほか 7）。
//
// キーは LF の原文から計算したものなので合わず、publish はその行を捨てる（上流と
// 同じ）。新しい訳が黙って公開されないので、行とキーを出す。原文は出さない。
// その訳がいまの公開ファイルにあれば、失われる訳の確認が止め、知らせはその前に出る。
func TestPublishHintsCRLFSource(t *testing.T) {
	crlf := "para1\r\n\r\npara2"
	keyMulti := key.For(multiSource)
	working := "key,section,node,order,speaker,source_en,translation\r\n" +
		keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,もしもし？\r\n" +
		keyMulti + ",UI,,,UI,\"" + crlf + "\",段落の訳\r\n"
	hint := "3〜5行目 " + keyMulti + ": 原文の CRLF を LF にするとキーが一致します"

	t.Run("新しい訳なら知らせて書く", func(t *testing.T) {
		root := lossRepo(t)
		game := makeGame(t, map[string]string{"Translations/_discovered/ja.working.csv": working})
		code, _, stderr := runCLI("publish", "--root", root, "--game", game)
		if code != exitOK {
			t.Fatalf("終了コード = %d\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{
			"注意: 原文の改行が CRLF になっていて、キーと合わずに公開されない訳があります。",
			"ja: " + filepath.ToSlash(filepath.Join(game, "Translations", "_discovered", "ja.working.csv")) + "（入力、1 件）",
			hint,
		})
		if strings.Contains(stderr, "para1") {
			t.Errorf("原文を出している:\n%s", stderr)
		}
		if got := readFile(t, root, jaPublishedPath); strings.Contains(got, "段落の訳") {
			t.Errorf("キーの合わない行を書いている:\n%s", got)
		}
	})

	t.Run("公開済みの訳なら知らせてから失われる訳で止める", func(t *testing.T) {
		root := lossRepo(t)
		published := "key,section,node,order,speaker,translation\n" +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？\n" +
			keyMulti + ",UI,,,UI,段落の訳\n"
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(jaPublishedPath)), []byte(published), 0o644); err != nil {
			t.Fatal(err)
		}
		game := makeGame(t, map[string]string{"Translations/_discovered/ja.working.csv": working})
		code, _, stderr := runCLI("publish", "--root", root, "--no-game", "--dry-run")
		if code != exitOK {
			t.Fatalf("作業コピーを読まないときの終了コード = %d\n%s", code, stderr)
		}
		code, _, stderr = runCLI("publish", "--root", root, "--game", game)
		if code != exitProblems {
			t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
		}
		at := strings.Index(stderr, hint)
		lost := strings.Index(stderr, "訳が失われるので")
		if at < 0 || lost < 0 || at > lost {
			t.Errorf("知らせが失われる訳の報告より前に出ていない:\n%s", stderr)
		}
	})
}

// TestReportSourceLineEnds は、原文の CRLF の知らせの一覧を切ることと、読めない
// 入力で終了コード2になることを見る。
func TestReportSourceLineEnds(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	b.WriteString("key,source_en,translation\n")
	total := publishLossListMax + 2
	for i := range total {
		src := fmt.Sprintf("Line %02d\nnext", i)
		fmt.Fprintf(&b, "%s,\"%s\",訳\n", key.For(src), strings.ReplaceAll(src, "\n", "\r\n"))
	}
	input := filepath.Join(root, "in.csv")
	if err := os.WriteFile(input, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	if code := reportSourceLineEnds(root, []publish.Target{{Input: input, Output: input}}, &stderr); code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, stderr.String())
	}
	checkContains(t, "標準エラー", stderr.String(), []string{
		fmt.Sprintf("in.csv（入力、%d 件）", total),
		"ほかに 2 件あります。",
	})
	if got := strings.Count(stderr.String(), "原文の CRLF を LF にするとキーが一致します"); got != publishLossListMax {
		t.Errorf("並べた件数が %d、%d を期待", got, publishLossListMax)
	}

	stderr.Reset()
	if code := reportSourceLineEnds(root, []publish.Target{{Input: root, Output: input}}, &stderr); code != exitError {
		t.Fatalf("読めない入力で終了コード = %d、2 を期待\n%s", code, stderr.String())
	}
}

// TestPublishPassesTheRealWorkingCopyShape は、ゲーム側の作業コピーの実物と同じ
// 形では止まらず、書き出す中身も変わらないことを見る。
//
// 実物には、原文が行をまたぎ（区切りは CRLF、値の中は LF、空行を挟む）、訳が
// 空のレコードが1件ある。公開されない行なので、止める理由が無い。ここで止めると、
// 翻訳者には直せない理由で ja の publish が常に塞がる。
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
	// 全体を解釈して読むので、そのレコードは訳の空の行になり、捨てられない。
	// 行単位で読んでいたときは2件の malformed dropped だった（上流 main は 0）。
	checkContains(t, "標準出力", stdout, []string{"3 converted", "0 malformed dropped"})
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
		fmt.Fprintf(&b, "%s,UI,,,UI,\"訳%02d\rつづき\"\n", key.For(fmt.Sprintf("Line %02d", i)), i)
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
// ロケールが決まらないので頭に付けず、ファイルの名前だけを出す。通す指定には、
// --path に渡したとおりのファイルを案内する。
func TestPublishShapeWithPathHasNoLocale(t *testing.T) {
	root := lossRepo(t)
	path := filepath.Join(root, filepath.FromSlash(jaPublishedPath))
	if err := os.WriteFile(path, []byte("key,translation\n"+keyHello+",\"a\n"+keyHiThere+",b\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLI("publish", "--root", root, "--path", path)
	if code != exitProblems {
		t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{
		"dwloc:   " + jaPublishedPath + "（いまの公開ファイル）",
		"--accept-multiline " + path + " を付けると書けます。",
	})
	if strings.Contains(stderr, "ja: ") {
		t.Errorf("--path なのにロケールを付けている:\n%s", stderr)
	}
	if strings.Contains(stderr, "--locale") || strings.Contains(stderr, "このロケール") {
		t.Errorf("--path なのにロケールで案内している:\n%s", stderr)
	}

	// 案内どおりにそのファイルを指定すると書ける。
	code, _, stderr = runCLI("publish", "--root", root, "--path", path, "--accept-multiline", path)
	if code != exitOK {
		t.Fatalf("--accept-multiline を付けた終了コード = %d\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{"--accept-multiline の指定で、次の行を正しい複数行の値として通します。"})
}

// TestPublishAcceptMultiline は、確かめたうえで通す指定（--accept-multiline）を見る。
//
// 通すのは、指定したロケールの、続きの行が単独で読むとレコードに見える形だけである。
// 正しい複数行の値でも当たる（原文の2行目がカンマを多く含むなど）ので、止めたまま
// にすると、そのロケールを publish できなくなる。通した行は標準エラーに出す。
func TestPublishAcceptMultiline(t *testing.T) {
	// de の訳の2行目は、単独で読むとヘッダーと同じ6列のレコードに見える。
	dePublished := "key,section,node,order,speaker,translation\n" +
		helloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"Hallo\n,,,,,Welt\"\n"
	jaPublished := "key,section,node,order,speaker,translation\n" +
		helloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし\n"
	files := map[string]string{
		"data/script_order.csv":       scriptOrderCSV,
		"Translations/de/strings.csv": dePublished,
		"Translations/ja/strings.csv": jaPublished,
	}

	t.Run("指定が無ければ止める", func(t *testing.T) {
		root := makeTree(t, files)
		code, _, stderr := runCLI("publish", "--root", root, "--no-game")
		if code != exitProblems {
			t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{
			"de: Translations/de/strings.csv（いまの公開ファイル）",
			"2〜3行目: 3行目が、単独で読むとヘッダーと同じ列の数のレコードに見える。",
			"--accept-multiline de を付けると書けます。",
		})
		if got := readFile(t, root, "Translations/ja/strings.csv"); got != jaPublished {
			t.Errorf("止めたのに ja を書いている:\n%s", got)
		}
	})

	t.Run("指定したロケールの行を通して書く", func(t *testing.T) {
		root := makeTree(t, files)
		// 大文字小文字は --locale と同じく問わない。
		code, stdout, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", "DE")
		if code != exitOK {
			t.Fatalf("終了コード = %d\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{
			"--accept-multiline の指定で、次の行を正しい複数行の値として通します。",
			"de: Translations/de/strings.csv（いまの公開ファイル）",
			"2〜3行目: 3行目が、単独で読むとヘッダーと同じ列の数のレコードに見える。",
		})
		if strings.Contains(stderr, "直し方") || strings.Contains(stderr, shapeStopText) {
			t.Errorf("通したのに止めるときの文面が出ている:\n%s", stderr)
		}
		checkContains(t, "標準出力", stdout, []string{"2 件を書き出しました。"})
		checkContains(t, "de の公開ファイル", readFile(t, root, "Translations/de/strings.csv"),
			[]string{"\"Hallo\n,,,,,Welt\"\n"})
	})

	t.Run("ほかのロケールの指定では通さない", func(t *testing.T) {
		root := makeTree(t, files)
		code, _, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", "ja")
		if code != exitProblems {
			t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
		}
		if strings.Contains(stderr, "として通します") {
			t.Errorf("指定していない de を通している:\n%s", stderr)
		}
	})

	t.Run("閉じ引用符の後ろに文字が続く形は通さない", func(t *testing.T) {
		root := makeTree(t, map[string]string{
			"data/script_order.csv": scriptOrderCSV,
			"Translations/ja/strings.csv": "key,section,node,order,speaker,translation\n" +
				helloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もし\n\"Alpha\nBeta\",UI,,,UI,訳\n",
		})
		code, _, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", "ja")
		if code != exitProblems {
			t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{"行をまたいだ引用が閉じたすぐ後ろに文字が続く"})
	})

	t.Run("当たらない指定は誤りにする", func(t *testing.T) {
		root := makeTree(t, files)
		code, _, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", "xx")
		if code != exitError {
			t.Fatalf("終了コード = %d、2 を期待\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{
			"--accept-multiline に指定したロケールがありません: xx（対象にできるのは de, ja）",
		})
	})

	t.Run("--path に無いファイルの指定は誤りにする", func(t *testing.T) {
		root := makeTree(t, files)
		path := filepath.Join(root, "Translations", "ja", "strings.csv")
		code, _, stderr := runCLI("publish", "--root", root, "--path", path,
			"--accept-multiline", filepath.Join(root, "Translations", "de", "strings.csv"))
		if code != exitError {
			t.Fatalf("終了コード = %d、2 を期待\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{"--accept-multiline に指定したファイルが --path にありません"})
	})

	t.Run("空の指定は誤りにする", func(t *testing.T) {
		root := makeTree(t, files)
		code, _, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", " ")
		if code != exitError {
			t.Fatalf("終了コード = %d、2 を期待\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{"ロケール名かファイル名が空です"})
	})
}

// TestAcceptListString は flag.Value としての表示を確かめる。
func TestAcceptListString(t *testing.T) {
	var nilList *acceptList
	if got := nilList.String(); got != "" {
		t.Errorf("nil の表示 = %q", got)
	}
	l := acceptList{"de", "ja"}
	if got := l.String(); got != "de ja" {
		t.Errorf("表示 = %q", got)
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
			// 埋め残しがあると、報告に {line} がそのまま出る。
			for _, locale := range []string{"ja", ""} {
				fix := shapeFix(publish.Hazard{Locale: locale, Path: "x.csv", Why: reason.New(id, "", "line", "3")})
				if strings.ContainsAny(fix, "{}") {
					t.Errorf("%s（ロケール %q）の直し方に埋め残しがある: %s", id, locale, fix)
				}
			}
		}
	}
}

// TestShapeFixNamesTheAcceptTarget は、直し方が、確かめたうえで通す指定を案内する形を
// 固定する。通せる形だけに案内し、渡す値はロケールを決めて走らせたならロケール名、
// --path で走らせたならそのファイルにする。ゲーム側の公開ファイルでは、写し直す直し方を
// 先に出す。
func TestShapeFixNamesTheAcceptTarget(t *testing.T) {
	for _, id := range []string{reason.PublishSwallowKeyShaped, reason.PublishSwallowSameColumns} {
		why := reason.New(id, "", "line", "7")
		byLocale := shapeFix(publish.Hazard{Locale: "ja", Path: "Translations/ja/strings.csv", Why: why})
		byPath := shapeFix(publish.Hazard{Path: "some/file.csv", Why: why})
		if !strings.Contains(byLocale, "7行目が値の一部として正しい") || !strings.Contains(byLocale, "--accept-multiline ja を") {
			t.Errorf("%s: ロケールで走らせたときの案内が違う: %s", id, byLocale)
		}
		if !strings.Contains(byPath, "--accept-multiline some/file.csv を") {
			t.Errorf("%s: --path で走らせたときの案内が違う: %s", id, byPath)
		}
	}
	for _, id := range []string{
		reason.PublishSwallowTextAfterQuote, reason.PublishUnclosedQuote, reason.PublishLoneCR, reason.PublishCRCut,
	} {
		if fix := shapeFix(publish.Hazard{Locale: "ja", Why: reason.New(id, "", "line", "7")}); strings.Contains(fix, "--accept-multiline") {
			t.Errorf("%s: 通せない形に通す指定を案内している: %s", id, fix)
		}
	}
	game := shapeFix(publish.Hazard{Locale: "ja", GameBase: true, Why: reason.New(reason.PublishUnclosedQuote, "")})
	if !strings.HasPrefix(game, publishGameBaseFix) {
		t.Errorf("ゲーム側の公開ファイルで写し直す案内を先に出していない: %s", game)
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

// TestPublishShapeRefusesWhatItCannotRead は、形を確かめるためにファイルを読めない
// とき、終了コード2で止まることを見る。形の確かめは組み立てより前に走るので、
// 読めない入力はここで止まる。「確かめられなかった」を「形に問題が無い」と同じ
// 扱いにすると、そこだけ素通りする。
func TestPublishShapeRefusesWhatItCannotRead(t *testing.T) {
	root := lossRepo(t)
	dir := filepath.Join(root, "Translations", "ja")
	code, stdout, stderr := runCLI("publish", "--root", root, "--path", dir)
	if code != exitError {
		t.Fatalf("終了コード = %d、2 を期待\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{"を読めないので、訳が失われないことを確かめられません"})
	if stdout != "" {
		t.Errorf("止めたのに標準出力へ書いている:\n%s", stdout)
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
		"ファイル全体: 空でない行があるのに、1行も読めない",
		"直し方: 改行を LF か CRLF にして保存し直して",
	})
}
