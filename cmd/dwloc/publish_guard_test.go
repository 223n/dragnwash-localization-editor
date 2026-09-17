package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 原文とそのキー。作業コピーの key 列と source_en から計算したハッシュが
// 食い違う行は publish が捨てる（移植仕様 R15）ので、見本のキーは実際の
// ハッシュでなければならない。値は internal/key.For で確かめたもの。
const (
	keyHello   = "0da72197e898ebe1" // "Hello?"
	keyHiThere = "d451d2a79e0a1f87" // "Hi there!"
)

// jaPublishedPath は公開ファイルのルート相対パス。
const jaPublishedPath = "Translations/ja/strings.csv"

// lossRepo は守りを試すための翻訳リポジトリを作る。
//
// 公開ファイルに入っているのは "Hello?" の行だけである。"Hi there!" は未訳で、
// publish が訳の空の行を書かない（R20）ため公開ファイルに存在しない。
// 「未翻訳の行」と「公開ファイルに無い行」が同じ集合だという、この段の前提を
// そのまま形にしてある。
func lossRepo(t *testing.T) string {
	t.Helper()

	return makeTree(t, map[string]string{
		"data/script_order.csv": "section,phase,node,order,line_id,key,speaker,condition\n" +
			"L01 Ryan,intro,Ryan_1_intro,1,line:aaaaaaaa," + keyHello + ",Ryan,\n" +
			"L01 Ryan,intro,Ryan_1_intro,2,line:bbbbbbbb," + keyHiThere + ",Kobold,\n",
		jaPublishedPath: "key,section,node,order,speaker,translation\n" +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？\n",
	})
}

// workingBoth は、2行とも訳が入っている作業コピー。
//
// 2行目は公開ファイルにまだ無い行で、これが公開ファイルへ増えることが、
// publish に作業コピーを読ませる理由そのものである。
const workingBoth = "key,section,node,order,speaker,source_en,translation\n" +
	keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,もしもし？\n" +
	keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,Hi there!,やあ！\n"

// workingPartial は、途中までしか書けていない作業コピー。
// 公開ファイルに訳が入っている行（"Hello?"）が無い。
const workingPartial = "key,section,node,order,speaker,source_en,translation\n" +
	keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,Hi there!,やあ！\n"

// workingBadHeader は、ヘッダーの引用符が閉じていない作業コピー。
//
// source_en と translation が1つの列名に融合して translation 列を引けなくなり、
// 全行が「訳が空」と見なされて落ちる。壊れ方としては最も静かで、この守りが
// 無いと公開ファイルがヘッダー1行だけになる。
const workingBadHeader = "key,section,node,order,speaker,\"source_en,translation\n" +
	keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,もしもし？\n"

// workingCleared は、公開ファイルに訳がある行の訳を空にした作業コピー。
const workingCleared = "key,section,node,order,speaker,source_en,translation\n" +
	keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,Hello?,\n" +
	keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,Hi there!,やあ！\n"

// publishOnce は --game 無しで1回 publish を通し、見出しを整えた状態にする。
//
// 見本の公開ファイルは見出しを持たないので、ここを飛ばすと「守りが止めた」と
// 「見出しを足した」が同じ差分に混ざる。
func publishOnce(t *testing.T, root string) string {
	t.Helper()

	if code, _, stderr := runCLI("publish", "--root", root); code != exitOK {
		t.Fatalf("下ごしらえの publish の終了コード = %d\n%s", code, stderr)
	}
	return readFile(t, root, jaPublishedPath)
}

func TestPublishWithGameCarriesNewTranslations(t *testing.T) {
	// 守りが邪魔をしないこと。正常な作業コピーで未訳の行を訳したら、
	// その行が公開ファイルへ増える。これが publish がゲームを読む理由である。
	root := lossRepo(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": workingBoth,
	})
	before := publishOnce(t, root)
	if strings.Contains(before, keyHiThere) {
		t.Fatalf("前提が崩れている。未訳の行が公開ファイルにある:\n%s", before)
	}

	code, stdout, stderr := runCLI("publish", "--root", root, "--game", game)
	if code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}
	// 探し先は必ず伝える。黙って別の場所を読み始めない。
	checkContains(t, "標準エラー", stderr, []string{"作業コピーの探し先にします"})
	// どのファイルを入力にしたかは、ロケールごとの1行に出る。
	checkContains(t, "標準出力", stdout, []string{"ja.working.csv"})

	after := readFile(t, root, jaPublishedPath)
	if after == before {
		t.Fatalf("公開ファイルが1バイトも変わっていない:\n%s", after)
	}
	checkContains(t, "公開ファイル", after, []string{keyHiThere, "やあ！", "もしもし？"})
	if len(after) <= len(before) {
		t.Errorf("行が増えていない: 前 %d バイト、後 %d バイト", len(before), len(after))
	}
}

func TestPublishStopsWhenTranslationsWouldBeLost(t *testing.T) {
	// 守りが効くこと。どの壊れ方でも、公開ファイルは1バイトも変わらない。
	for _, tc := range []struct {
		name    string
		working string
	}{
		{"作業コピーが途中まで", workingPartial},
		{"ヘッダーの引用符が閉じていない", workingBadHeader},
		{"公開ファイルにある行の訳を空にした", workingCleared},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := lossRepo(t)
			game := makeGame(t, map[string]string{
				"Translations/_discovered/ja.working.csv": tc.working,
			})
			before := publishOnce(t, root)

			code, stdout, stderr := runCLI("publish", "--root", root, "--game", game)
			if code != exitProblems {
				t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
			}
			checkContains(t, "標準エラー", stderr, []string{
				"訳が失われるので、1バイトも書きませんでした",
				"ja: " + jaPublishedPath,
				keyHello,
				"1件でもあるあいだは書きません",
			})
			if strings.Contains(stdout, "書き出しました") {
				t.Errorf("書き出したと出ている:\n%s", stdout)
			}
			if after := readFile(t, root, jaPublishedPath); after != before {
				t.Errorf("公開ファイルが変わっている\n--- 前 ---\n%s\n--- 後 ---\n%s", before, after)
			}
		})
	}
}

func TestPublishDryRunStopsTheSameWay(t *testing.T) {
	// --dry-run でも同じ判定をして、同じ報告を出す。書かないことは変わらないので、
	// 判定だけ変えると「dry-run では通ったのに本番で止まる」食い違いが生まれる。
	root := lossRepo(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": workingPartial,
	})
	before := publishOnce(t, root)

	_, _, wet := runCLI("publish", "--root", root, "--game", game)
	code, stdout, dry := runCLI("publish", "--root", root, "--game", game, "--dry-run")
	if code != exitProblems {
		t.Fatalf("終了コード = %d、1 を期待\n%s", code, dry)
	}
	if dry != wet {
		t.Errorf("報告が --dry-run で変わっている\n--- dry-run ---\n%s\n--- 本番 ---\n%s", dry, wet)
	}
	if strings.Contains(stdout, "[dry-run]") {
		t.Errorf("止めたのに予定を並べている:\n%s", stdout)
	}
	if after := readFile(t, root, jaPublishedPath); after != before {
		t.Errorf("公開ファイルが変わっている:\n%s", after)
	}
}

func TestPublishLossReportShowsOnlyTheHeadOfTheTranslation(t *testing.T) {
	// 失われる訳を丸ごと並べない。報告は不具合の報せとして手元の外へ貼られうる。
	const long = "これはとてもながいやくで、ぜんぶはでてこないはずです。"
	root := makeTree(t, map[string]string{
		"data/script_order.csv": "section,phase,node,order,line_id,key,speaker,condition\n" +
			"L01 Ryan,intro,Ryan_1_intro,1,line:aaaaaaaa," + keyHello + ",Ryan,\n",
		jaPublishedPath: "key,section,node,order,speaker,translation\n" +
			keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan," + long + "\n",
	})
	game := makeGame(t, map[string]string{
		// その行が無い作業コピー。書けば long の訳が消える。
		"Translations/_discovered/ja.working.csv": "key,section,node,order,speaker,source_en,translation\n" +
			keyHiThere + ",L01 Ryan,Ryan_1_intro,2,Kobold,Hi there!,やあ！\n",
	})

	code, _, stderr := runCLI("publish", "--root", root, "--game", game)
	if code != exitProblems {
		t.Fatalf("終了コード = %d、1 を期待\n%s", code, stderr)
	}
	if strings.Contains(stderr, long) {
		t.Errorf("訳が丸ごと出ている:\n%s", stderr)
	}
	// 先頭は出す。出さないと、どの行が消えるのかを訳の中身で見分けられない。
	checkContains(t, "標準エラー", stderr, []string{string([]rune(long)[:8]), "…"})
}

func TestPublishReportsWhenTheCurrentFileCannotBeChecked(t *testing.T) {
	// いまの公開ファイルを読めなければ、失われないことを確かめられていない。
	// 「確かめられなかった」を「失われない」と同じ扱いにすると、そこだけ素通りする。
	root := lossRepo(t)
	// 列名が重複したヘッダー。publish の読み方（ConvertFrom-Csv 相当）が
	// 例外にする唯一の壊れ方である。
	broken := "key,key,node,order,speaker,translation\n" +
		keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,もしもし？\n"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(jaPublishedPath)),
		[]byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": workingBoth,
	})

	code, _, stderr := runCLI("publish", "--root", root, "--game", game)
	if code != exitError {
		t.Fatalf("終了コード = %d、2 を期待\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{"訳が失われないことを確かめられません"})
	if after := readFile(t, root, jaPublishedPath); after != broken {
		t.Errorf("公開ファイルが変わっている:\n%s", after)
	}
}

func TestPublishSaysGameIsUnusedWithPath(t *testing.T) {
	// --path は走査そのものを置き換えるので、作業コピーを探す先が無い。
	// 黙って受けると、探し先の1行だけが出て何にも効かない形になる。
	root := lossRepo(t)
	game := makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": workingBoth,
	})
	path := filepath.Join(root, filepath.FromSlash(jaPublishedPath))

	code, _, stderr := runCLI("publish", "--root", root, "--game", game, "--path", path)
	if code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}
	checkContains(t, "標準エラー", stderr, []string{"--game は使いません"})
	if strings.Contains(stderr, "作業コピーの探し先にします") {
		t.Errorf("使わないのに探し先を出している:\n%s", stderr)
	}
}

func TestPublishUsageSaysItChecksForLoss(t *testing.T) {
	// 説明に守りのことが書いてある。書いていないと、終了コード 1 を見た人が
	// 「validate と同じ何か」としか読めない。
	_, stdout, _ := runCLI("publish", "--help")
	checkContains(t, "publish の説明", stdout, []string{"失われる", "--dry-run でも同じ判定"})
}
