package main

import (
	"fmt"
	"maps"
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
				// 通す指定はレコード単位で、そのまま写せる形（<ロケール>:<key>）で出す。
				"3行目が値の一部として正しい（正しい複数行の値）なら、確かめたうえで --accept-multiline ja:" + keyHello + " を付けると書けます。",
			},
		},
		{
			// 同じ key のレコードが2つあると、指定ではどちらを通すか決められない。
			// 通す指定は案内せず、要らないレコードを消すよう案内する。
			name: "いまの公開ファイルで同じ key のレコードが2つあり、片方が飲み込む",
			published: "key,section,node,order,speaker,translation\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もしもし\n" +
				keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,やあ！\"\n" +
				keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？\n",
			want: []string{
				"2〜3行目: 3行目が、単独で読むとキーの形で始まるレコードに見える。",
				"3行目が値の一部として正しくても、同じ key（" + keyHello + "）のレコードがこのファイルに 2 件あり、" +
					"どのレコードを通すか決められないので、--accept-multiline では通せません。",
				"要らないレコードを消してから、もう一度実行してください。",
			},
			notWant: []string{"を付けると書けます"},
		},
		{
			// key 列の値に空白がある。publish はこのレコードを書かない（R15）。指定に
			// 写すとシェルで割れるので、key を出さず、通せないと案内する。
			name: "作業コピーの key 列の値に指定に使えない文字",
			working: "key,section,node,order,speaker,source_en,translation\n" +
				"bad key,L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,\"もしもし\n" +
				keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,Hi there!,やあ！\"\n",
			want: []string{
				"2〜3行目: 3行目が、単独で読むとキーの形で始まるレコードに見える。",
				"3行目が値の一部として正しくても、このレコードには指定に使える key が無いので、--accept-multiline では通せません",
			},
			notWant: []string{"を付けると書けます", "bad key"},
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
			// key 列のキーがいまの原文から作ったものなら、原文は直させない。直すとキーと
			// 合わず、その行は止まりも知らせもせずに公開されなくなる（malformed dropped に
			// 数えられるだけ）。上流の道具もこの行を公開しない。キーの決まり方ごとの
			// 直し方と、案内どおりに直した結果は TestPublishLoneCRSourceFix が見る。
			name: "作業コピーの原文の中の単独の CR",
			working: "key,source_en,translation\n" +
				key.For("Hello\rthere") + ",\"Hello\rthere\",もしもし？\n",
			want: []string{
				"2〜3行目: source_en 列の値に単独の CR（後ろに LF の続かない CR）がある。",
				"直し方: 原文（source_en 列）は直さないでください。key 列のキーはいまの原文から作ったものなので、直すとキーと合わなくなり、",
				"この行の訳を空に戻すと、ほかの行は書けます。",
			},
			notWant: []string{"LF に直すか取り除いて"},
		},
		{
			// 台詞ID の行はキーを原文から作らないので、原文を直してよい。訳を空に戻す
			// 案内をすると、公開できる訳を翻訳者が自分で捨てることになる（検証の指摘）。
			name: "作業コピーの台詞ID の行の原文の中の単独の CR",
			working: "key,section,node,order,speaker,source_en,translation\n" +
				"line:aaaaaaaa,L01 Ryan,Ryan_1_intro,1,Ryan,\"Hello\rthere\",もしもし？\n",
			want: []string{
				"2〜3行目: source_en 列の値に単独の CR（後ろに LF の続かない CR）がある。",
				"直し方: 原文（source_en 列）の単独の CR を LF に直すか取り除いてから、もう一度実行してください。",
				"この行は台詞ID の行で、キーを原文から作らないので、原文を直しても訳は公開されます。",
			},
			notWant: []string{"直さないでください", "訳を空に戻す"},
		},
		{
			// key 列の単独の CR は取り除かせる。LF に直すと、キーの途中の改行でキーの形で
			// なくなることがある。
			name: "作業コピーの key 列の中の単独の CR",
			working: "key,source_en,translation\n" +
				"\"" + keyHello + "\r\",Hello?,もしもし？\n",
			want: []string{
				"2〜3行目: key 列の値に単独の CR（後ろに LF の続かない CR）がある。",
				"直し方: key 列の値から単独の CR を取り除いてから、もう一度実行してください。",
			},
			notWant: []string{"LF に直す", "原文（source_en 列）は直さないでください"},
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
// --path に渡したとおりのファイルと key（<ファイル>:<key>）を案内する。
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
	spec := path + ":" + keyHello
	checkContains(t, "標準エラー", stderr, []string{
		"dwloc:   " + jaPublishedPath + "（いまの公開ファイル）",
		"--accept-multiline " + spec + " を付けると書けます。",
	})
	if strings.Contains(stderr, "ja: ") || strings.Contains(stderr, "ja:"+keyHello) {
		t.Errorf("--path なのにロケールを付けている:\n%s", stderr)
	}
	if strings.Contains(stderr, "--locale") || strings.Contains(stderr, "このロケール") {
		t.Errorf("--path なのにロケールで案内している:\n%s", stderr)
	}

	// ファイルだけの指定（ロケール単位にあたる古い形）は、レコード単位で指定するよう
	// 案内して止める。
	code, _, stderr = runCLI("publish", "--root", root, "--path", path, "--accept-multiline", path)
	if code != exitError {
		t.Fatalf("ファイルだけの指定の終了コード = %d、2 を期待\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{"--accept-multiline はレコード単位で指定してください: " + path + "（"})

	// 案内どおりに写すと書ける。
	code, _, stderr = runCLI("publish", "--root", root, "--path", path, "--accept-multiline", spec)
	if code != exitOK {
		t.Fatalf("--accept-multiline を付けた終了コード = %d\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{
		"--accept-multiline の指定で、次の行を正しい複数行の値として通します。",
		"指定: --accept-multiline " + spec,
	})
}

// TestPublishAcceptMultiline は、確かめたうえで通す指定（--accept-multiline）を見る。
//
// 通すのは、指定したレコード（<ロケール>:<key>）の、続きの行が単独で読むとレコードに
// 見える形だけである（決まったことの 16）。正しい複数行の値でも当たる（原文の2行目が
// カンマを多く含むなど）ので、止めたままにすると、そのロケールを publish できなくなる。
// 通した行は標準エラーに出す。
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
	deSpec := "de:" + helloKey

	// unchanged は、止めたときにどのファイルも書いていないことを見る。
	unchanged := func(t *testing.T, root string, want map[string]string) {
		t.Helper()
		for rel, content := range want {
			if !strings.HasPrefix(rel, "Translations/") {
				continue
			}
			if got := readFile(t, root, rel); got != content {
				t.Errorf("止めたのに %s を書いている:\n%s", rel, got)
			}
		}
	}

	t.Run("指定が無ければ止め、通すための指定を写せる形で出す", func(t *testing.T) {
		root := makeTree(t, files)
		code, _, stderr := runCLI("publish", "--root", root, "--no-game")
		if code != exitProblems {
			t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{
			"de: Translations/de/strings.csv（いまの公開ファイル）",
			"2〜3行目: 3行目が、単独で読むとヘッダーと同じ列の数のレコードに見える。",
			"--accept-multiline " + deSpec + " を付けると書けます。",
		})
		unchanged(t, root, files)
	})

	t.Run("指定したレコードの行を通して書く", func(t *testing.T) {
		root := makeTree(t, files)
		// ロケール名の大文字小文字は --locale と同じく問わない。key も、台詞ID の
		// ほかは問わない（publish がキーを小文字にしてから扱う。R13）。
		spec := "DE:" + strings.ToUpper(helloKey)
		code, stdout, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", spec)
		if code != exitOK {
			t.Fatalf("終了コード = %d\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{
			"--accept-multiline の指定で、次の行を正しい複数行の値として通します。",
			"de: Translations/de/strings.csv（いまの公開ファイル）",
			"2〜3行目: 3行目が、単独で読むとヘッダーと同じ列の数のレコードに見える。",
			"指定: --accept-multiline " + spec,
		})
		if strings.Contains(stderr, "直し方") || strings.Contains(stderr, shapeStopText) {
			t.Errorf("通したのに止めるときの文面が出ている:\n%s", stderr)
		}
		checkContains(t, "標準出力", stdout, []string{"2 件を書き出しました。"})
		checkContains(t, "de の公開ファイル", readFile(t, root, "Translations/de/strings.csv"),
			[]string{"\"Hallo\n,,,,,Welt\"\n"})
	})

	t.Run("ロケールだけの指定は止めてレコード単位の形を案内する", func(t *testing.T) {
		for _, spec := range []string{"de", "de:", "de: ", ":" + helloKey} {
			root := makeTree(t, files)
			code, stdout, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", spec)
			if code != exitError {
				t.Fatalf("%q: 終了コード = %d、2 を期待\n%s", spec, code, stderr)
			}
			checkContains(t, "標準エラー", stderr, []string{
				"--accept-multiline はレコード単位で指定してください: " + strings.TrimSpace(spec) + "（" + acceptFormText + "）",
			})
			if stdout != "" {
				t.Errorf("%q: 止めたのに標準出力へ書いている:\n%s", spec, stdout)
			}
			unchanged(t, root, files)
		}
	})

	t.Run("同じロケールのほかの飲み込みは通さない", func(t *testing.T) {
		// 検証の指摘の筋書き。de の公開ファイルには、確かめて公開した正しい複数行の訳が
		// ある。de の作業コピーでは Alpha の訳の引用符が閉じず、キーの形で始まる
		// Bravo と Charlie の行を飲み込む。ロケール単位の指定では、公開済みの行を通す
		// ための指定が、この飲み込みまで通していた。
		keyAlpha := key.For("Alpha")
		working := "key,section,node,order,speaker,source_en,translation\n" +
			helloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello,\"Hallo\n,,,,,Welt\"\n" +
			keyAlpha + ",UI,,,UI,Alpha,\"Eins\n" +
			key.For("Bravo") + ",UI,,,UI,Bravo,Zwei\n" +
			key.For("Charlie") + ",UI,,,UI,Charlie,Drei\"\n"
		withWorking := maps.Clone(files)
		withWorking["Translations/_discovered/de.working.csv"] = working
		root := makeTree(t, withWorking)

		code, stdout, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", deSpec)
		if code != exitProblems {
			t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
		}
		passed, stopped, ok := strings.Cut(stderr, shapeStopText)
		if !ok {
			t.Fatalf("形の確かめで止めていない:\n%s", stderr)
		}
		checkContains(t, "通した行", passed, []string{
			publishAcceptText, "de: Translations/de/strings.csv（いまの公開ファイル）", "指定: --accept-multiline " + deSpec,
		})
		checkContains(t, "止めた行", stopped, []string{
			"de: Translations/_discovered/de.working.csv（入力）",
			"4〜6行目: 5行目が、単独で読むとキーの形で始まるレコードに見える。",
			"4〜6行目: 6行目が、単独で読むとキーの形で始まるレコードに見える。",
			"--accept-multiline de:" + keyAlpha + " を付けると書けます。",
			"読み違える形が 2 か所あります。直すまでは書きません。",
		})
		if strings.Contains(passed, "de.working.csv") {
			t.Errorf("指定していないレコードの飲み込みを通している:\n%s", stderr)
		}
		if stdout != "" {
			t.Errorf("止めたのに標準出力へ書いている:\n%s", stdout)
		}
		unchanged(t, root, withWorking)
	})

	t.Run("複数のレコードは1つずつ複数回指定する", func(t *testing.T) {
		both := maps.Clone(files)
		both["Translations/ja/strings.csv"] = "key,section,node,order,speaker,translation\n" +
			helloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もし\n,,,,,もし\"\n"
		jaSpec := "ja:" + helloKey

		// カンマでは分けない。--path ではファイル名を受けるので、カンマを区切りに
		// できない。カンマで並べると1つの指定として読み、key が当たらない。
		root := makeTree(t, both)
		code, _, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", deSpec+","+jaSpec)
		if code != exitError {
			t.Fatalf("カンマで並べた指定: 終了コード = %d、2 を期待\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{
			"--accept-multiline の指定が、通せる行に当たりません: " + deSpec + "," + jaSpec + "（そのレコードに、指定で通せる行がありません）",
		})
		unchanged(t, root, both)

		code, _, stderr = runCLI("publish", "--root", root, "--no-game", "--accept-multiline", deSpec, "--accept-multiline", jaSpec)
		if code != exitOK {
			t.Fatalf("複数回の指定: 終了コード = %d\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{"指定: --accept-multiline " + deSpec, "指定: --accept-multiline " + jaSpec})

		// 同じ指定を2回書いても、どちらも当たった指定として数える。
		root = makeTree(t, files)
		code, _, stderr = runCLI("publish", "--root", root, "--no-game", "--accept-multiline", deSpec, "--accept-multiline", deSpec)
		if code != exitOK {
			t.Fatalf("同じ指定を2回: 終了コード = %d\n%s", code, stderr)
		}

		_, usage, _ := runCLI("publish", "--help")
		checkContains(t, "使い方", usage, []string{
			"--accept-multiline <ロケール>:<key>",
			"通すレコードごとに1つずつ、複数回指定します\n        （ファイル名にカンマを入れられるので、カンマでは分けません）。",
		})
	})

	t.Run("同じ key のレコードが2つあれば通さない", func(t *testing.T) {
		// 指定はレコードを key で名指すので、同じファイルに同じ key のレコードが2つ
		// あると、どちらを確かめたのかが決まらない。ゲームの作業コピーと publish の
		// 書く公開ファイルでは key が重ならないので、手で直したファイルだけに起きる。
		dup := maps.Clone(files)
		dup["Translations/de/strings.csv"] = dePublished + helloKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hallo\n"
		root := makeTree(t, dup)
		code, _, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", deSpec)
		if code != exitError {
			t.Fatalf("終了コード = %d、2 を期待\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{
			shapeStopText,
			"同じ key（" + helloKey + "）のレコードがこのファイルに 2 件あり",
			"--accept-multiline の指定が、通せる行に当たりません: " + deSpec +
				"（そのレコードの行は、指定では通せない形です。直し方は上の一覧にあります）",
		})
		if strings.Contains(stderr, "として通します") || strings.Contains(stderr, "を付けると書けます") {
			t.Errorf("同じ key のレコードを通すか、通す指定を案内している:\n%s", stderr)
		}
		unchanged(t, root, dup)
	})

	// 通せない形は、そのレコードを指定しても形の確かめで止まり、指定は当たらない指定
	// （終了コード2）になる。どの見本も、形の確かめを通してしまえば、組み立てと失われる
	// 訳の確かめは止めない（書くか、閉じない引用符は組み立ての誤りで終了コード2になる）。
	// ほかの確かめで止まる見本では、通す指定が形を通してしまっても試験が通るので、
	// 守りを確かめたことにならない。
	for _, tc := range []struct {
		name string
		// published は ja の公開ファイル（入力と書き出し先が同じ経路）。1行目は
		// lossRepo と同じヘッダー。
		published string
		// want は、形の確かめが出す理由の一部。
		want string
		// named は、指定したレコード（ja:keyHello）に形の崩れがあるか。あれば当たらない
		// 理由は「通せない形」、無ければ「通せる行がない」になる。飲み込みのほかの形の
		// Hazard は key を持たない。
		named bool
	}{
		{
			// 飲み込まれた行が自分の値を引用符で開く形。4行目は正しいレコードなので、
			// 通してしまうと「もし↵Alpha」を訳として書く。
			name: "閉じ引用符の後ろに文字が続く",
			published: keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もし\n" +
				"\"Alpha\n" +
				keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,やあ！\n",
			want:  "2〜3行目: 3行目で、行をまたいだ引用が閉じたすぐ後ろに文字が続く。",
			named: true,
		},
		{
			// 3行目は単独で読むとキーの形で始まるが、閉じ引用符の後ろに文字が続く。
			// 通すと、Hi there! の行が「もし」の訳に入って公開され、その行は消える。
			name: "キーの形の行で閉じ引用符の後ろに文字が続く",
			published: keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もし\n" +
				keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,\"やあ\"！\n",
			want:  "2〜3行目: 3行目で、行をまたいだ引用が閉じたすぐ後ろに文字が続く。",
			named: true,
		},
		{
			// 3行目は単独で読むとヘッダーと同じ6列に見える（key 列に英文を書いた追記の形）が、
			// 英文を開く引用符が「もし」を閉じ、後ろに Good day が続く。
			name: "ヘッダーと同じ列の数の行で閉じ引用符の後ろに文字が続く",
			published: keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もし\n" +
				"\"Good day, friend\",UI,,,UI,こんにちは\n" +
				keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,やあ！\n",
			want:  "2〜3行目: 3行目で、行をまたいだ引用が閉じたすぐ後ろに文字が続く。",
			named: true,
		},
		{
			name:      "値の中の単独の CR",
			published: keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もし\rもし？\"\n",
			want:      "2〜3行目: translation 列の値に単独の CR（後ろに LF の続かない CR）がある。",
		},
		{
			name: "引用符で囲まない値が単独の CR で切れる",
			published: keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,もし\rもし？\n" +
				keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,やあ！\n",
			want: "2行目: 引用符で囲まない値が行の終わりの単独の CR で切れ、3行目にある続きが別の行として読まれる。",
		},
		{
			name: "閉じない引用符",
			published: keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,\"もしもし？\n" +
				keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,やあ！\n",
			want: "2〜3行目: 開いた引用符がファイルの終わりまで閉じない",
		},
	} {
		t.Run("通さない: "+tc.name, func(t *testing.T) {
			root := lossRepo(t)
			published := "key,section,node,order,speaker,translation\n" + tc.published
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(jaPublishedPath)), []byte(published), 0o644); err != nil {
				t.Fatal(err)
			}
			spec := "ja:" + keyHello
			code, stdout, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", spec)
			if code != exitError {
				t.Fatalf("終了コード = %d、2 を期待\n%s", code, stderr)
			}
			why := "そのレコードに、指定で通せる行がありません"
			if tc.named {
				why = "そのレコードの行は、指定では通せない形です。直し方は上の一覧にあります"
			}
			// 止めたのが形の確かめで、通す指定が1件も通していないこと。
			checkContains(t, "標準エラー", stderr, []string{
				shapeStopText, tc.want, "直し方: ", "読み違える形が 1 か所あります。直すまでは書きません。",
				"--accept-multiline の指定が、通せる行に当たりません: " + spec + "（" + why + "）",
			})
			for _, s := range []string{"として通します", "を付けると書けます", "訳が失われるので"} {
				if strings.Contains(stderr, s) {
					t.Errorf("標準エラーに %q が出ている:\n%s", s, stderr)
				}
			}
			if stdout != "" {
				t.Errorf("止めたのに標準出力へ書いている:\n%s", stdout)
			}
			if got := readFile(t, root, jaPublishedPath); got != published {
				t.Errorf("公開ファイルが変わっている\n--- 前 ---\n%q\n--- 後 ---\n%q", published, got)
			}
		})
	}

	t.Run("当たらない指定は誤りにする", func(t *testing.T) {
		// 無いロケールは、形を確かめる前に止める。
		root := makeTree(t, files)
		code, _, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", "xx:"+helloKey)
		if code != exitError {
			t.Fatalf("無いロケール: 終了コード = %d、2 を期待\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{
			"--accept-multiline に指定したロケールがありません: xx（対象にできるのは de, ja）",
		})
		if strings.Contains(stderr, shapeStopText) {
			t.Errorf("無いロケールなのに形を確かめている:\n%s", stderr)
		}

		// ロケールはあるが、そのロケールに、その key のレコードで止まる行が無い。
		// 形の報告を先に出してから誤りにする（de は指定が無いので止まったまま）。
		for _, spec := range []string{"de:" + shapeKeyUnrelated, "ja:" + helloKey} {
			code, stdout, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", spec)
			if code != exitError {
				t.Fatalf("%s: 終了コード = %d、2 を期待\n%s", spec, code, stderr)
			}
			checkContains(t, "標準エラー", stderr, []string{
				shapeStopText,
				"--accept-multiline " + deSpec + " を付けると書けます。",
				"--accept-multiline の指定が、通せる行に当たりません: " + spec + "（そのレコードに、指定で通せる行がありません）",
			})
			if strings.Contains(stderr, "として通します") {
				t.Errorf("%s: 指定していない de のレコードを通している:\n%s", spec, stderr)
			}
			if stdout != "" {
				t.Errorf("%s: 止めたのに標準出力へ書いている:\n%s", spec, stdout)
			}
		}
		unchanged(t, root, files)

		// ほかがすべて通っても、当たらない指定が1つでもあれば書かない。
		code, stdout, stderr := runCLI("publish", "--root", root, "--no-game",
			"--accept-multiline", deSpec, "--accept-multiline", "de:"+shapeKeyUnrelated)
		if code != exitError {
			t.Fatalf("当たる指定と当たらない指定: 終了コード = %d、2 を期待\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{
			publishAcceptText,
			"--accept-multiline の指定が、通せる行に当たりません: de:" + shapeKeyUnrelated,
		})
		if strings.Contains(stderr, shapeStopText) || stdout != "" {
			t.Errorf("止める文面が出ているか、書いている:\n%s\n%s", stderr, stdout)
		}
		unchanged(t, root, files)
	})

	t.Run("--path では指定したファイルのレコードだけを通す", func(t *testing.T) {
		// 2つのファイルに同じ形（de と同じ、2行目が6列に見える複数行の訳）を置き、
		// 片方だけを指定する。もう片方は、確かめていないので止める。
		root := makeTree(t, map[string]string{
			"data/script_order.csv": scriptOrderCSV,
			"a/strings.csv":         dePublished,
			"b/strings.csv":         dePublished,
		})
		a := filepath.Join(root, "a", "strings.csv")
		b := filepath.Join(root, "b", "strings.csv")
		code, stdout, stderr := runCLI("publish", "--root", root, "--path", a, "--path", b, "--accept-multiline", a+":"+helloKey)
		if code != exitProblems {
			t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
		}
		passed, stopped, ok := strings.Cut(stderr, shapeStopText)
		if !ok {
			t.Fatalf("形の確かめで止めていない:\n%s", stderr)
		}
		labelA := "dwloc:   a/strings.csv（いまの公開ファイル）"
		labelB := "dwloc:   b/strings.csv（いまの公開ファイル）"
		checkContains(t, "通した行", passed, []string{publishAcceptText, labelA})
		checkContains(t, "止めた行", stopped, []string{
			labelB, "--accept-multiline " + b + ":" + helloKey + " を付けると書けます。", "読み違える形が 1 か所あります。",
		})
		if strings.Contains(passed, labelB) || strings.Contains(stopped, labelA) {
			t.Errorf("指定していない b を通したか、指定した a を止めている:\n%s", stderr)
		}
		if stdout != "" {
			t.Errorf("止めたのに標準出力へ書いている:\n%s", stdout)
		}
		for _, rel := range []string{"a/strings.csv", "b/strings.csv"} {
			if got := readFile(t, root, rel); got != dePublished {
				t.Errorf("%s が変わっている:\n%s", rel, got)
			}
		}
	})

	t.Run("--path で台詞ID の key を指定する", func(t *testing.T) {
		// ファイル名（Windows ではドライブ名）にも key（line:）にも ':' が入る。
		// ':' のどこで分けると前半が --path のファイルになるかで分ける。
		published := "key,section,node,order,speaker,translation\n" +
			"line:aaaaaaaa,L01 Ryan,Ryan_1_intro,1,Ryan,\"Hallo\n,,,,,Welt\"\n"
		root := makeTree(t, map[string]string{"data/script_order.csv": scriptOrderCSV, "a/strings.csv": published})
		a := filepath.Join(root, "a", "strings.csv")
		_, _, stderr := runCLI("publish", "--root", root, "--path", a)
		spec := a + ":line:aaaaaaaa"
		checkContains(t, "標準エラー", stderr, []string{"--accept-multiline " + spec + " を付けると書けます。"})
		code, _, stderr := runCLI("publish", "--root", root, "--path", a, "--accept-multiline", spec)
		if code != exitOK {
			t.Fatalf("終了コード = %d\n%s", code, stderr)
		}
		// 台詞ID は綴りのまま比べる。
		code, _, stderr = runCLI("publish", "--root", root, "--path", a, "--accept-multiline", a+":LINE:aaaaaaaa")
		if code != exitError {
			t.Fatalf("綴りの違う台詞ID: 終了コード = %d、2 を期待\n%s", code, stderr)
		}
	})

	t.Run("--path に無いファイルの指定は誤りにする", func(t *testing.T) {
		root := makeTree(t, files)
		path := filepath.Join(root, "Translations", "ja", "strings.csv")
		other := filepath.Join(root, "Translations", "de", "strings.csv")
		code, _, stderr := runCLI("publish", "--root", root, "--path", path, "--accept-multiline", other+":"+helloKey)
		if code != exitError {
			t.Fatalf("終了コード = %d、2 を期待\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{"--accept-multiline に指定したファイルが --path にありません: " + other + ":" + helloKey})
	})

	t.Run("空の指定は誤りにする", func(t *testing.T) {
		root := makeTree(t, files)
		code, _, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", " ")
		if code != exitError {
			t.Fatalf("終了コード = %d、2 を期待\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{"指定が空です（<ロケール>:<key> の形で指定します）"})
	})
}

// TestParsePathAccept は、--path で走らせたときの指定の分け方を見る。どこまでが
// ファイル名か決められない指定は誤りにする。
func TestParsePathAccept(t *testing.T) {
	targets := []publish.Target{{Input: "a", Output: "a"}, {Input: "a:b", Output: "a:b"}}
	if _, err := parsePathAccept(targets, "a:b:c"); err == nil || !strings.Contains(err.Error(), "どこまでがファイル名か決められません") {
		t.Errorf("2通りに分けられる指定を受けた: %v", err)
	}
	got, err := parsePathAccept(targets, "a:b:line:x")
	if err == nil {
		t.Errorf("2通りに分けられる指定を受けた: %+v", got)
	}
	got, err = parsePathAccept(targets[:1], "a: "+shapeKeyUnrelated+" ")
	if err != nil || got.path != "a" || got.key != shapeKeyUnrelated {
		t.Errorf("分け方が違う: %+v, %v", got, err)
	}
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
// 固定する。通せる形だけに案内し、渡す値はレコード単位で、ロケールを決めて走らせたなら
// <ロケール>:<key>、--path で走らせたなら <ファイル>:<key> にする。key で1つの
// レコードに名指せないときは案内せず、なぜ通せないかを書く。ゲーム側の公開ファイル
// では、写し直す直し方を先に出す。
func TestShapeFixNamesTheAcceptTarget(t *testing.T) {
	for _, id := range []string{reason.PublishSwallowKeyShaped, reason.PublishSwallowSameColumns} {
		why := reason.New(id, "", "line", "7")
		byLocale := shapeFix(publish.Hazard{Locale: "ja", Path: "Translations/ja/strings.csv", Why: why,
			Key: "line:0a0b0c01", KeyRecords: 1})
		byPath := shapeFix(publish.Hazard{Path: "some/file.csv", Why: why, Key: keyHello, KeyRecords: 1})
		if !strings.Contains(byLocale, "7行目が値の一部として正しい") ||
			!strings.Contains(byLocale, "--accept-multiline ja:line:0a0b0c01 を付けると書けます。") {
			t.Errorf("%s: ロケールで走らせたときの案内が違う: %s", id, byLocale)
		}
		if !strings.Contains(byPath, "--accept-multiline some/file.csv:"+keyHello+" を付けると書けます。") {
			t.Errorf("%s: --path で走らせたときの案内が違う: %s", id, byPath)
		}

		// key が無いか、指定に使えない文字を含むなら、key を出さずに通せないと書く。
		// 印に置き換えた key（publish.Visible）を写しても当たらないので、出さない。
		for _, k := range []string{"", "bad key", "k\r\n" + keyHiThere} {
			fix := shapeFix(publish.Hazard{Locale: "ja", Why: why, Key: k, KeyRecords: 1})
			if !strings.Contains(fix, "このレコードには指定に使える key が無いので、--accept-multiline では通せません") ||
				strings.Contains(fix, "を付けると書けます") || (k != "" && strings.Contains(fix, k)) ||
				strings.ContainsAny(fix, "\r\n{}") {
				t.Errorf("%s: key %q の案内が違う: %q", id, k, fix)
			}
		}
		// 同じ key のレコードが2つ以上あるなら、どれを通すか決められない。
		dup := shapeFix(publish.Hazard{Locale: "ja", Why: why, Key: keyHello, KeyRecords: 3})
		if !strings.Contains(dup, "同じ key（"+keyHello+"）のレコードがこのファイルに 3 件あり") ||
			strings.Contains(dup, "を付けると書けます") {
			t.Errorf("%s: 同じ key のレコードの案内が違う: %s", id, dup)
		}
	}
	for _, id := range []string{
		reason.PublishSwallowTextAfterQuote, reason.PublishUnclosedQuote, reason.PublishLoneCR, reason.PublishCRCut,
		reason.PublishOrderLineBreak,
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

// TestLoneCRFix は、単独の CR の直し方を列と、原文ならキーの決まり方（理由の置換
// key_kind）で分けることを見る。列名は publish が列を引くときと同じく、ASCII の
// 大文字小文字を区別しない。
func TestLoneCRFix(t *testing.T) {
	general := publishShapeFix[reason.PublishLoneCR]
	kinds := []string{
		publish.LoneCRKeyLineID, publish.LoneCRKeyMatches, publish.LoneCRKeyFromSource,
		publish.LoneCRKeyMatchesLF, publish.LoneCRKeyMismatch,
	}
	// キーの決まり方ごとに直し方がある。足し忘れると、原文を直させない案内に落ちる。
	for _, kind := range kinds {
		if publishLoneCRSourceFix[kind] == "" {
			t.Errorf("キーの決まり方 %q の直し方が無い", kind)
		}
	}
	if len(publishLoneCRSourceFix) != len(kinds) {
		t.Errorf("原文の直し方が %d 件、%d 件を期待", len(publishLoneCRSourceFix), len(kinds))
	}

	matches := publishLoneCRSourceFix[publish.LoneCRKeyMatches]
	tests := []struct {
		column, keyKind, want string
	}{
		{"Source_EN", publish.LoneCRKeyLineID, publishLoneCRSourceFix[publish.LoneCRKeyLineID]},
		// キーの決まり方が分からなければ、原文を直させない。直すとキーと合わなくなる
		// 行で「直してよい」と案内すると、訳が黙って公開されなくなる。
		{"source_en", "", matches},
		{"source_en", "unknown", matches},
		{"key", "", publishLoneCRKeyFix},
		{"KEY", publish.LoneCRKeyMatches, publishLoneCRKeyFix},
		{"translation", "", general},
		// キーの決まり方は原文の直し方にだけ効く。
		{"translation", publish.LoneCRKeyMatches, general},
		{"speaker", "", general},
		// ASCII 以外の文字で畳むと source_en になる列名は、publish が source_en 列として
		// 引かない（csvfile.FoldASCII）ので、原文の直し方にしない。
		{"ſource_en", publish.LoneCRKeyMatches, general},
	}
	for _, kind := range kinds {
		tests = append(tests, struct{ column, keyKind, want string }{"source_en", kind, publishLoneCRSourceFix[kind]})
	}
	for _, tc := range tests {
		if got := loneCRFix(tc.column, tc.keyKind); got != tc.want {
			t.Errorf("loneCRFix(%q, %q) = %q, want %q", tc.column, tc.keyKind, got, tc.want)
		}
		args := []string{"column", tc.column}
		if tc.keyKind != "" {
			args = append(args, "key_kind", tc.keyKind)
		}
		fix := shapeFix(publish.Hazard{Locale: "ja", Why: reason.New(reason.PublishLoneCR, "", args...)})
		if fix != tc.want {
			t.Errorf("shapeFix（列 %q、キー %q）= %q, want %q", tc.column, tc.keyKind, fix, tc.want)
		}
		// 単独の CR は通す指定では通さないので、案内もしない。
		if strings.Contains(fix, "--accept-multiline") {
			t.Errorf("shapeFix（列 %q、キー %q）が通す指定を案内している: %s", tc.column, tc.keyKind, fix)
		}
	}
}

// TestPublishLoneCRSourceFix は、原文（source_en 列）の単独の CR で止めたときの
// 直し方が、キーの決まり方ごとに、案内どおりに直したときの publish の結果と合うことを
// 見る。
//
// 直し方は「原文を直す」「原文は直さず訳を空に戻す」のどちらかで、どちらが正しいかは
// キーを原文から作るかどうかで決まる。列名だけで「直さない」と案内すると、台詞ID の
// 行（キーを原文から作らない）でも、公開できる訳を翻訳者が自分で捨てることになる
// （検証の指摘）。ここでは、原文を LF にそろえたときと CR を取り除いたときに、
// その行の訳がどのキーで公開されるか（されないか）を、案内の文面と並べて固定する。
// 上流の tools/hash-strings.ps1（pwsh 7.6.6）も、同じ入力で同じキーに書く（書かない）
// ことを確かめてある。
func TestPublishLoneCRSourceFix(t *testing.T) {
	const cr, lf, removed = "Alpha\rBeta", "Alpha\nBeta", "AlphaBeta"
	// 1行目は公開ファイルにある訳。作業コピーに残さないと、失われる訳の確かめが止める。
	head := "key,section,node,order,speaker,source_en,translation\n" +
		keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,もしもし？\n"
	for _, tc := range []struct {
		name string
		// key は確かめる行の key 列。
		key string
		// want と notWant は、止めたときの直し方に出る／出ない文字列。
		want, notWant []string
		// wantLF と wantRemoved は、原文を LF にそろえたとき・CR を取り除いたときに、
		// その行の訳が公開ファイルに書かれるキー。空なら書かれない。
		wantLF, wantRemoved string
	}{
		{
			name: "台詞ID の行",
			key:  "line:cccccccc",
			want: []string{
				"直し方: 原文（source_en 列）の単独の CR を LF に直すか取り除いてから、もう一度実行してください。",
				"原文を直しても訳は公開されます。",
			},
			notWant:     []string{"直さないでください", "訳を空に戻す"},
			wantLF:      "line:cccccccc",
			wantRemoved: "line:cccccccc",
		},
		{
			name: "key 列のキーがいまの原文から作ったもの",
			key:  key.For(cr),
			want: []string{
				"直し方: 原文（source_en 列）は直さないでください。key 列のキーはいまの原文から作ったものなので、",
				"直すとキーと合わなくなり、その行は黙って公開されなくなります",
				"この行の訳を空に戻すと、ほかの行は書けます。",
			},
			notWant: []string{"LF に直す", "LF にそろえてから"},
		},
		{
			// ゲームはいまの原文（CR のまま）から作ったキーで引くので、直した原文の
			// キーでは引かない。
			name: "key 列が空",
			key:  "",
			want: []string{
				"直し方: 原文（source_en 列）は直さないでください。key 列が無いか空なので、キーはいまの原文から作ります。",
				"直すとキーが変わり、ゲームが引かないキーで公開されます。",
				"この行の訳を空に戻すと、ほかの行は書けます。",
			},
			notWant:     []string{"黙って公開されなくなります", "LF に直す"},
			wantLF:      key.For(lf),
			wantRemoved: key.For(removed),
		},
		{
			// キーは Mod が LF の原文から計算したもので、改行が CR に変わっている。
			name: "改行を LF にそろえると key と一致する",
			key:  key.For(lf),
			want: []string{
				"直し方: 原文（source_en 列）の改行を、単独の CR も含めて LF にそろえてから、もう一度実行してください。",
				"（CR を取り除くと一致しません）",
			},
			notWant: []string{"直さないでください", "取り除いてから", "訳を空に戻す"},
			wantLF:  key.For(lf),
		},
		{
			name:    "どちらでも key と合わない",
			key:     shapeKeyUnrelated,
			want:    []string{"直しても公開されません。", "この行の訳を空に戻すと、ほかの行は書けます。"},
			notWant: []string{"LF にそろえてから", "取り除いてから"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := func(src, tr string) string {
				return tc.key + ",UI,,,UI,\"" + src + "\"," + tr + "\n"
			}
			publishWorking := func(working string) (int, string, string) {
				t.Helper()
				root := lossRepo(t)
				game := makeGame(t, map[string]string{"Translations/_discovered/ja.working.csv": working})
				code, _, stderr := runCLI("publish", "--root", root, "--game", game)
				return code, stderr, readFile(t, root, jaPublishedPath)
			}

			code, stderr, _ := publishWorking(head + row(cr, "訳"))
			if code != exitProblems {
				t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
			}
			checkContains(t, "標準エラー", stderr, append([]string{"3〜4行目: source_en 列の値に単独の CR"}, tc.want...))
			for _, s := range tc.notWant {
				if strings.Contains(stderr, s) {
					t.Errorf("標準エラーに %q が出ている:\n%s", s, stderr)
				}
			}

			for _, fixed := range []struct{ how, src, want string }{
				{"LF にそろえた", lf, tc.wantLF},
				{"CR を取り除いた", removed, tc.wantRemoved},
			} {
				code, stderr, out := publishWorking(head + row(fixed.src, "訳"))
				if code != exitOK {
					t.Fatalf("%s: 終了コード = %d\n%s", fixed.how, code, stderr)
				}
				published := publishedKeyOf(out, "訳")
				if published != fixed.want {
					t.Errorf("%s: 訳を書いたキー = %q、%q を期待\n%s", fixed.how, published, fixed.want, out)
				}
			}

			// 訳を空に戻せば、止まらずにほかの行を書く。
			code, stderr, out := publishWorking(head + row(cr, "") + keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,Hi there!,やあ！\n")
			if code != exitOK {
				t.Fatalf("訳を空に戻した: 終了コード = %d\n%s", code, stderr)
			}
			if publishedKeyOf(out, "やあ！") != keyHiThere {
				t.Errorf("訳を空に戻した: ほかの行を書いていない\n%s", out)
			}
		})
	}
}

// shapeKeyUnrelated は、どの見本の原文から作ったキーとも合わない16桁のキー。
const shapeKeyUnrelated = "0123456789abcdef"

// publishedKeyOf は、公開ファイル out のうち、訳が tr の行のキー（最初の列）を返す。
// 無ければ空。
func publishedKeyOf(out, tr string) string {
	for line := range strings.Lines(out) {
		line = strings.TrimSuffix(line, "\n")
		if k, _, ok := strings.Cut(line, ","); ok && strings.HasSuffix(line, ","+tr) {
			return k
		}
	}
	return ""
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

// TestPublishStopsOnOrderShape は、再生順のデータ（data/script_order.csv と
// data/level_flow.csv）の閉じない引用符と、見出しの行へそのまま書く値の改行で
// 止まることを見る（決まったことのそのほか 8）。
//
// 閉じない引用符は、読み込みの誤り（終了コード 2「再生順のデータを読めません」）では
// なく、どの行をどう直すかを出して終了コード 1 で止める。見出しの値の改行は、書くと
// 公開ファイルの見出しの2行目が '#' で始まらない行になり、次に読むときデータの行に
// なる。どちらも --accept-multiline では通さない。
func TestPublishStopsOnOrderShape(t *testing.T) {
	const orderHeader = "section,phase,node,order,line_id,key,speaker,condition\n"
	for _, tc := range []struct {
		name, file, content string
		want                []string
	}{
		{
			name: "見出しに使う値の改行",
			file: "data/script_order.csv",
			content: orderHeader +
				"L01 Ryan,intro,\"Ryan_1\nintro\",1,line:aaaaaaaa," + keyHello + ",Ryan,\n",
			want: []string{
				"dwloc:   data/script_order.csv（再生順のデータ）",
				"2〜3行目: node 列の値に改行がある。publish はこの値を引用せずにそのまま書く",
				"直し方: node 列の値から改行（CR と LF）を取り除いてから、もう一度実行してください。",
			},
		},
		{
			name:    "見出しの表の値の改行",
			file:    "data/level_flow.csv",
			content: "level,dragon,weather,set_flags,end_flags\n0,Ryan,\"Sun\r\nny\",,\n",
			want: []string{
				"dwloc:   data/level_flow.csv（再生順のデータ）",
				"2〜3行目: weather 列の値に改行がある。",
			},
		},
		{
			name: "閉じない引用符",
			file: "data/script_order.csv",
			content: orderHeader +
				"L01 Ryan,intro,Ryan_1_intro,1,line:aaaaaaaa," + keyHello + ",\"Ryan,\n",
			want: []string{
				"dwloc:   data/script_order.csv（再生順のデータ）",
				"2行目: 開いた引用符がファイルの終わりまで閉じない",
				`直し方: 引用符を閉じるか取り除いてから、もう一度実行してください。`,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := lossRepo(t)
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(tc.file)), []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			before := readFile(t, root, jaPublishedPath)
			code, stdout, stderr := runCLI("publish", "--root", root, "--no-game", "--accept-multiline", "ja:"+keyHello)
			if code != exitProblems {
				t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
			}
			checkContains(t, "標準エラー", stderr, append([]string{
				shapeStopText, "読み違える形が 1 か所あります。直すまでは書きません。",
			}, tc.want...))
			if strings.Contains(stderr, "再生順のデータを読めません") || strings.Contains(stderr, publishAcceptText) {
				t.Errorf("読み込みの誤りか、通す指定として扱っている:\n%s", stderr)
			}
			if stdout != "" {
				t.Errorf("止めたのに標準出力へ書いている:\n%s", stdout)
			}
			if after := readFile(t, root, jaPublishedPath); after != before {
				t.Errorf("公開ファイルが変わっている")
			}
		})
	}

	t.Run("読めない", func(t *testing.T) {
		root := lossRepo(t)
		flow := filepath.Join(root, "data", "level_flow.csv")
		if err := os.Mkdir(flow, 0o755); err != nil {
			t.Fatal(err)
		}
		code, _, stderr := runCLI("publish", "--root", root, "--no-game")
		if code != exitError {
			t.Fatalf("終了コード = %d、2 を期待\n%s", code, stderr)
		}
		checkContains(t, "標準エラー", stderr, []string{"dwloc: 再生順のデータを読めません: data/level_flow.csv: "})
	})
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
