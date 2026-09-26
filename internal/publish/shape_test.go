package publish

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// 形の確かめの見本に使う値。英文はどれも架空の文である。
const (
	shapeK1 = "0123456789abcdef"
	shapeK2 = "fedcba9876543210"
	// shapeH6 は公開ファイルのヘッダー。
	shapeH6 = "key,section,node,order,speaker,translation\n"
	// shapeWorkingCRLF は作業コピーのヘッダー。ゲームが書く実物と同じく CRLF で終わる。
	shapeWorkingCRLF = "key,section,node,order,speaker,source_en,translation\r\n"
	// shapeMultiSource は、行をまたぐ原文。空の行を挟む。ゲーム側の作業コピーの
	// 実物にある形（LF 2つ、間に空行）をまねた架空の文。
	shapeMultiSource = "para1\n\npara2"
)

// wantHazard は試験で期待する Hazard の要点。
type wantHazard struct {
	id        string
	line, end int
	current   bool
}

// hazardsOf は Hazard の並びを比べやすい形にする。
func hazardsOf(found []Hazard) []wantHazard {
	var out []wantHazard
	for _, h := range found {
		out = append(out, wantHazard{h.Why.ID, h.Line, h.EndLine, h.Current})
	}
	return out
}

// TestCheckTargetShapeUpstreamCases は、上流の調査（報告と検証）が使った入力で、
// 止める／止めないが意図どおりかを見る。
//
// 名前の p- で始まるものは、報告と検証が上流の hash-strings.ps1 と dwloc の
// 両方に通した入力そのもの（英文は架空）。止めないものについては、組み立てた
// 中身も見る（wantOut）。wantOut が空のものは、形は通るが、訳が失われる確かめ
// （[CheckLoss]）で止まる入力で、書き出されない。
//
// 行をまたぐ値は、全体を解釈する読み手へ移ってから、上流 main と同じく正しい値と
// して読み書きする（p-multiline-published、p-multiline-source、p-hash-in-quotes）。
// 行単位で読んでいたときは、訳が1行目で切り詰められるので形の確かめ (b)(c) で
// 止めていた。
func TestCheckTargetShapeUpstreamCases(t *testing.T) {
	keyOne, keyTwo := key.For("one"), key.For("two")
	tests := []struct {
		name      string
		published string
		// working は作業コピー。空なら置かない（入力と書き出し先が同じファイル）。
		working string
		want    []wantHazard
		wantOut string
	}{
		{
			name:      "p-english-key",
			published: shapeH6 + shapeK1 + ",UI,,,UI,a\nHello there,UI,,,UI,こんにちは\n",
		},
		{
			// 行をまたぐ訳は、全体を解釈して1つの訳として読み、そのまま書く。
			name:      "p-multiline-published",
			published: shapeH6 + shapeK1 + ",UI,,,UI,\"line1\nline2\"\n" + shapeK2 + ",UI,,,UI,b\n",
			wantOut:   shapeH6 + shapeK1 + ",,,,UI,\"line1\nline2\"\n" + shapeK2 + ",,,,UI,b\n",
		},
		{
			// 原文が行をまたぎ、訳が入っている。行単位で読んでいたときは行ごと
			// 捨てられ、新しい訳が公開ファイルに届かないまま終了コード0だった。
			name:      "p-multiline-source",
			published: shapeH6 + keyOne + ",UI,,,UI,a\n",
			working: shapeWorkingCRLF + keyOne + ",UI,,,UI,one,a\r\n" +
				key.For(shapeMultiSource) + ",UI,,,UI,\"" + shapeMultiSource + "\",訳\r\n",
			wantOut: shapeH6 + keyOne + ",,,,UI,a\n" + key.For(shapeMultiSource) + ",,,,UI,訳\n",
		},
		{
			// ゲーム側の作業コピーの実物と同じ形（区切りは CRLF、値の中は LF、
			// 訳は空）。ここで止めると ja の publish が常に塞がる（検証の指摘 high）。
			name:      "p-multiline-source-untranslated",
			published: shapeH6 + keyOne + ",UI,,,UI,a\n",
			working: shapeWorkingCRLF + keyOne + ",UI,,,UI,one,a\r\n" +
				key.For(shapeMultiSource) + ",UI,,,UI,\"" + shapeMultiSource + "\",\r\n",
			wantOut: shapeH6 + keyOne + ",,,,UI,a\n",
		},
		{
			name:      "p-wc-missing-row",
			published: shapeH6 + shapeK1 + ",UI,,,UI,a\n" + shapeK2 + ",UI,,,UI,b\n",
			working:   "key,source_en,translation\n" + shapeK1 + ",,a2\n",
		},
		{
			name:      "p-wc-cleared",
			published: shapeH6 + shapeK1 + ",UI,,,UI,a\n" + shapeK2 + ",UI,,,UI,b\n",
			working:   "key,source_en,translation\n" + shapeK1 + ",,a2\n" + shapeK2 + ",,\n",
		},
		{
			// 守りではなく読み手の直し（上流の報告 #8）で通る。守りを入れる前の dwloc は
			// ヘッダーだけを書いていた。いまの出力は上流 main と1バイトも違わない。
			name:      "p-ws-above-header",
			published: "   \n" + shapeH6 + shapeK1 + ",UI,,,UI,a\n",
			wantOut:   shapeH6 + shapeK1 + ",,,,UI,a\n",
		},
		{
			name:      "p-comment-blank-header",
			published: "# note\n\n" + shapeH6 + shapeK1 + ",UI,,,UI,a\n",
			wantOut:   shapeH6 + shapeK1 + ",,,,UI,a\n",
		},
		{
			// 引用の中の '#' で始まる行は値の一部で、コメントではない。
			name:      "p-hash-in-quotes",
			published: shapeH6 + shapeK1 + ",UI,,,UI,\"first\n#second\"\n" + shapeK2 + ",UI,,,UI,b\n",
			wantOut:   shapeH6 + shapeK1 + ",,,,UI,\"first\n#second\"\n" + shapeK2 + ",,,,UI,b\n",
		},
		{
			// 引用符なしのフィールドの途中の '"' は引用を開かない。
			name:      "p-bare-quote",
			published: shapeH6 + shapeK1 + ",UI,,,UI,5\" screen\n\n# ===== UI and other text =====\n" + shapeK2 + ",UI,,,UI,b\n",
			wantOut:   shapeH6 + shapeK1 + ",,,,UI,\"5\"\" screen\"\n" + shapeK2 + ",,,,UI,b\n",
		},
		{
			name:      "p-typo-key",
			published: shapeH6 + "0123456789abcde,UI,,,UI,訳\n",
		},
		{
			// 上流の全文の読み方は、2行目で開いた引用符に3行目（英語の原文を含む）を
			// 飲み込み、公開ファイルへ漏らす（上流の報告 #11）。
			name:      "p-unclosed-quote-wc",
			published: shapeH6 + keyOne + ",UI,,,UI,a\n" + keyTwo + ",UI,,,UI,b\n",
			working:   "key,source_en,translation\n" + keyOne + ",one,\"いち\n" + keyTwo + ",two,に\n",
			want:      []wantHazard{{reason.PublishUnclosedQuote, 2, 3, false}},
		},
		{
			// CR だけの改行。読み手は単独の CR でも行を分けるので、正しく読める
			// （検証の指摘「単独の CR」。上流 main はここで訳をすべて消す）。
			name:      "cr-only-file",
			published: "key,section,node,order,speaker,translation\r" + shapeK1 + ",UI,,,UI,a\r" + shapeK2 + ",UI,,,UI,b\r",
			wantOut:   shapeH6 + shapeK1 + ",,,,UI,a\n" + shapeK2 + ",,,,UI,b\n",
		},
		{
			name:      "cr-only-wc",
			published: shapeH6 + shapeK1 + ",UI,,,UI,a\n",
			working:   "key,source_en,translation\r" + shapeK1 + ",,a2\r" + key.For("hello") + ",hello,x\r",
			wantOut:   shapeH6 + shapeK1 + ",,,,UI,a2\n" + key.For("hello") + ",,,,UI,x\n",
		},
		{
			// 引用の中の単独の CR は値に残る。上流の道具はその行を落とすので止める。
			name:      "lone-cr-in-value",
			published: shapeH6 + shapeK1 + ",UI,,,UI,\"a\rb\"\n" + shapeK2 + ",UI,,,UI,b\n",
			want:      []wantHazard{{reason.PublishLoneCR, 2, 3, true}},
		},
		{
			// 原文の単独の CR も、訳の入った行なら止める。上流の道具は行ごと落とす。
			name:      "lone-cr-wc-source",
			published: shapeH6 + shapeK1 + ",UI,,,UI,a\n",
			working:   "key,source_en,translation\n" + shapeK1 + ",,a2\n" + key.For("he\rllo") + ",\"he\rllo\",x\n",
			want:      []wantHazard{{reason.PublishLoneCR, 3, 4, false}},
		},
		{
			name:      "published-hash-quoted-translation",
			published: shapeH6 + shapeK1 + ",UI,,,UI,\"#tag\"\n",
			wantOut:   shapeH6 + shapeK1 + ",,,,UI,#tag\n",
		},
		{
			name:      "english-key-wc",
			published: shapeH6 + shapeK1 + ",UI,,,UI,a\nHello there,UI,,,UI,x\n",
			working:   "key,source_en,translation\n" + shapeK1 + ",,a2\n",
		},
		{
			// key 列の無い source_en,translation だけの作業コピーは、上流も受け付ける
			// 正規の入力（検証の指摘）。
			name:      "srcen-tr-wc",
			published: shapeH6,
			working:   "source_en,translation\nhello,x\n",
			wantOut:   shapeH6 + key.For("hello") + ",,,,UI,x\n",
		},
		{
			name:      "ws-mid",
			published: shapeH6 + shapeK1 + ",UI,,,UI,a\n   \n" + shapeK2 + ",UI,,,UI,b\n",
			wantOut:   shapeH6 + shapeK1 + ",,,,UI,a\n" + shapeK2 + ",,,,UI,b\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := shapeTarget(t, tt.published, tt.working)
			found, err := CheckTargetShape(target)
			if err != nil {
				t.Fatalf("CheckTargetShape: %v", err)
			}
			if got := hazardsOf(found); !slices.Equal(got, tt.want) {
				t.Errorf("見つけた形\n got %+v\nwant %+v", got, tt.want)
			}
			for _, h := range found {
				wantPath := target.Input
				if h.Current {
					wantPath = target.Output
				}
				if h.Locale != "xx" || h.Path != wantPath || h.GameBase {
					t.Errorf("どのファイルかが違う: %+v", h)
				}
			}
			if tt.wantOut == "" {
				return
			}
			out, _, err := BuildTarget(nil, target)
			if err != nil {
				t.Fatalf("BuildTarget: %v", err)
			}
			if string(out) != tt.wantOut {
				t.Errorf("組み立てた中身が変わった\n got %q\nwant %q", out, tt.wantOut)
			}
			losses, err := CheckTargetLoss(target, out)
			if err != nil || len(losses) != 0 {
				t.Errorf("書き出せるはずの入力で、失われる訳の確かめが止めている: %+v %v", losses, err)
			}
		})
	}
}

// shapeTarget は Translations/xx/strings.csv と作業コピーを置き、その対象を返す。
func shapeTarget(t *testing.T, published, working string) Target {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, TranslationsDir, "xx", StringsFile), published)
	if working != "" {
		writeFile(t, filepath.Join(root, TranslationsDir, DiscoveredDir, "xx"+WorkingSuffix), working)
	}
	targets, err := DiscoverTargets(root)
	if err != nil || len(targets) != 1 {
		t.Fatalf("DiscoverTargets: %+v %v", targets, err)
	}
	return targets[0]
}

// TestCheckShapeKeepsTheRealWorkingCopyShape は、ゲーム側の作業コピーの
// 実物と同じ形の入力で止まらず、組み立てた中身も変わらないことを固定する。
//
// 実物（ゲーム側の ja.working.csv）には、原文（source_en）が行をまたいで空行を
// 挟み、訳が空のレコードが1件ある。区切りは CRLF、値の中は LF である。全体を
// 解釈して読むと訳の空の行になり、公開されないので、出力は同じになる。ここで
// 止めると、翻訳者には直せない（原文を変えるとキーが変わる）理由で、そのロケールの
// publish が常に塞がる（検証の指摘 high）。
func TestCheckShapeKeepsTheRealWorkingCopyShape(t *testing.T) {
	multi := key.For(shapeMultiSource) + ",UI,,,UI,\"" + shapeMultiSource + "\",\r\n"
	before := shapeWorkingCRLF +
		key.For("one") + ",UI,,,UI,one,いち\r\n" +
		key.For("three") + ",UI,,,UI,three,\r\n"
	after := key.For("two") + ",UI,,,UI,two,に\r\n" +
		"line:aaaaaaaa,L01 Ryan,N1,1,Ryan,,せりふ\r\n"
	withMulti := before + multi + after
	without := before + after

	if found := CheckShape([]byte(withMulti)); len(found) != 0 {
		t.Fatalf("実物と同じ形で止めている: %+v", found)
	}

	// 行をまたぐレコードがあってもなくても、組み立てた中身は1バイトも変わらない。
	got, stats, err := Build(nil, []byte(withMulti), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want, _, err := Build(nil, []byte(without), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("行をまたぐ訳の空のレコードで出力が変わった\n got %q\nwant %q", got, want)
	}
	// 全体を解釈して読むので、そのレコードは原文のハッシュがキーと合う訳の空の行に
	// なり、捨てられない（converted に数える）。行単位で読んでいたときは、1行目
	// （原文のハッシュがキーと合わない）と続きの行（キーの形でない）の2件が
	// malformed dropped だった。上流 main の集計と同じになった。
	if stats.Dropped != 0 || stats.Converted != 4 {
		t.Errorf("捨てた行 = %d、変換した行 = %d、0 と 4 を期待（%+v）", stats.Dropped, stats.Converted, stats)
	}
}

// TestCheckShapeHeaders は (a) ヘッダーの列を見る。
func TestCheckShapeHeaders(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []wantHazard
	}{
		{
			name:  "key 列も source_en 列も無い",
			input: "section,speaker,translation\r\nUI,UI,x\r\n",
			want:  []wantHazard{{reason.PublishNoKeyColumn, 1, 1, false}},
		},
		{
			name:  "translation 列が無い",
			input: "key,source_en\n" + shapeK1 + ",\n",
			want:  []wantHazard{{reason.PublishNoTranslationColumn, 1, 1, false}},
		},
		{
			// ヘッダーの無い、データ行1行だけのファイル。その行がヘッダーになり、
			// どの列も引けない。行が無いので、以前は黙って通っていた。
			name:  "ヘッダーが無くデータ行だけ",
			input: shapeK1 + ",UI,,,UI,a\n",
			want: []wantHazard{
				{reason.PublishNoKeyColumn, 1, 1, false},
				{reason.PublishNoTranslationColumn, 1, 1, false},
			},
		},
		{
			// 最後の列名が行をまたぐ。列名に改行が入り、translation 列を引けない。
			name:  "ヘッダーが行をまたぐ",
			input: "key,source_en,\"trans\nlation\"\none,いち\n",
			want:  []wantHazard{{reason.PublishNoTranslationColumn, 1, 2, false}},
		},
		{
			// 上流はこのヘッダーを飛ばし、次の行をヘッダーにするので、訳を1行も公開
			// しない（hash-header-quoted。決まったことの 12 と 15）。
			name:  "引用符で囲んだ '#' で始まる列名",
			input: "\"#key\",source_en,translation\n" + key.For("one") + ",one,いち\n",
			want:  []wantHazard{{reason.PublishHashHeader, 1, 1, false}},
		},
		{
			// 先頭の半角空白は読み手が落とすので、読んだ列名は '#' で始まる
			// （hash-header-leading-space）。
			name:  "空白のあとの '#' で始まる列名",
			input: " #key,source_en,translation\n" + key.For("one") + ",one,いち\n",
			want:  []wantHazard{{reason.PublishHashHeader, 1, 1, false}},
		},
		{
			// 引用の中の空白のあとの '#' は、上流も飛ばさずその名前の列にし、source_en
			// から訳を書く（hash-header-quoted-space）。訳を失う形ではないので止めない。
			name:  "引用の中の空白のあとの '#'",
			input: "\" #key\",source_en,translation\n" + key.For("one") + ",one,いち\n",
		},
		{
			// 行頭の '#' はコメントとして落ち、次の行がヘッダーになる（hash-header-unquoted）。
			name:  "行頭の '#' のヘッダー",
			input: "#key,source_en,translation\n" + key.For("one") + ",one,いち\n",
			want: []wantHazard{
				{reason.PublishNoKeyColumn, 2, 2, false},
				{reason.PublishNoTranslationColumn, 2, 2, false},
			},
		},
		{
			// ヘッダーの中で開いた引用符が閉じない。列名にファイルの終わりまでが
			// 入っているので、列が無いとは言わず、閉じない引用符だけを出す。
			name:  "ヘッダーの引用符が閉じない",
			input: "key,\"source_en,translation\n" + key.For("one") + ",one,いち\n",
			want:  []wantHazard{{reason.PublishUnclosedQuote, 1, 2, false}},
		},
		{
			name:  "列名の大文字小文字は問わない",
			input: "Key,Source_EN,Translation\n" + shapeK1 + ",,a\n",
		},
		{
			name:  "source_en と translation だけでよい",
			input: "source_en,translation\nhello,x\n",
		},
		{
			name:  "ヘッダーだけでも正しければ通す",
			input: "key,translation\n",
		},
		{
			name:  "中身が無ければ見ない",
			input: "# comment\n\n",
		},
		{
			// 列名の重複は、ここでは見ない（組み立てと失われる訳の確かめが誤りにする）。
			name:  "列名の重複",
			input: "key,Key,translation\n" + shapeK1 + "," + shapeK1 + ",a\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hazardsOf(CheckShape([]byte(tt.input))); !slices.Equal(got, tt.want) {
				t.Errorf("\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// TestCheckShapeMultilineValues は、行をまたぐ値のうち、正しい値として通すものと、
// 飲み込み (f) として止めるものを見る。
//
// 飲み込みの見分けは csvfile.FindSwallows で、ここでは publish がそれを理由つきの
// Hazard にし、飲み込まれたと疑う物理行ごとに1件を返すことを固定する。範囲は
// 飲み込んだレコードの物理行で、疑う物理行は理由の置換 line に入る。
func TestCheckShapeMultilineValues(t *testing.T) {
	keyOne, keyTwo, keyThree := key.For("one"), key.For("two"), key.For("three")
	tests := []struct {
		name  string
		input string
		want  []wantHazard
		// lines は、飲み込みの理由の置換 line に入っているはずの物理行。
		lines []string
	}{
		{
			name:  "訳が行をまたぐ",
			input: "key,translation\n" + shapeK1 + ",\"a\r\nb\"\r\n",
		},
		{
			name:  "台詞ID行の訳が行をまたぐ",
			input: "key,translation\nline:aaaaaaaa,\"a\nb\"\n",
		},
		{
			// 値の中の '#' で始まる行と空行は、単独で読んでもレコードにならない。
			name:  "訳の中の '#' の行と空行",
			input: "key,translation\n" + shapeK1 + ",\"a\n\n# b\"\n",
		},
		{
			// 閉じ忘れた引用符が、次の行（キーの形で始まる）を飲み込み、'#' の行の
			// 引用符で閉じる（swallow-7col-hash-close と同じ形）。
			name: "キーの形の行を飲み込む",
			input: shapeWorkingCRLF +
				keyOne + ",UI,,,UI,one,\"いち\r\n" +
				keyTwo + ",UI,,,UI,two,\r\n" +
				"# note \"\r\n" +
				keyThree + ",UI,,,UI,three,さん\r\n",
			want:  []wantHazard{{reason.PublishSwallowKeyShaped, 2, 4, false}},
			lines: []string{"3"},
		},
		{
			// 2列の作業コピーで次の行を飲み込む（swallow-2col-hash-close と同じ形）。
			// キーの形でなくても、区切りの数がヘッダーと同じならレコードに見える。
			name:  "ヘッダーと同じ列の数の行を飲み込む",
			input: "source_en,translation\none,\"いち\ntwo,\n# note \"\nthree,さん\n",
			want:  []wantHazard{{reason.PublishSwallowSameColumns, 2, 4, false}},
			lines: []string{"3"},
		},
		{
			// 飲み込まれた行が自分の値を引用符で開く（swallow-2col-own-quote と同じ形）。
			name:  "閉じ引用符の後ろに文字が続く",
			input: "source_en,translation\none,\"いち\n\"Alpha line\nBeta line\",に\ntwo,さん\n",
			want:  []wantHazard{{reason.PublishSwallowTextAfterQuote, 2, 3, false}},
			lines: []string{"3"},
		},
		{
			// 1つのレコードが2つの行を飲み込めば、行ごとに1件ずつ出す。
			name: "2つの行を飲み込む",
			input: "key,source_en,translation\n" +
				keyOne + ",one,\"いち\n" +
				keyTwo + ",two,に\n" +
				keyThree + ",three,さん\"\n",
			want: []wantHazard{
				{reason.PublishSwallowKeyShaped, 2, 4, false},
				{reason.PublishSwallowKeyShaped, 2, 4, false},
			},
			lines: []string{"3", "4"},
		},
		{
			// 閉じない引用符のレコードは飲み込みとして見ない。(e) だけで出す。
			name:  "閉じない引用符",
			input: "key,source_en,translation\n" + keyOne + ",one,\"いち\n" + keyTwo + ",two,に\n",
			want:  []wantHazard{{reason.PublishUnclosedQuote, 2, 3, false}},
		},
		{
			// 閉じない引用符の前にある飲み込みは出す。
			name: "閉じない引用符の前の飲み込み",
			input: "key,source_en,translation\n" +
				keyOne + ",one,\"いち\n" + keyTwo + ",two,に\"\n" +
				keyThree + ",three,\"さん\n",
			want: []wantHazard{
				{reason.PublishSwallowKeyShaped, 2, 3, false},
				{reason.PublishUnclosedQuote, 4, 4, false},
			},
			lines: []string{"3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := CheckShape([]byte(tt.input))
			if got := hazardsOf(found); !slices.Equal(got, tt.want) {
				t.Errorf("\n got %+v\nwant %+v", got, tt.want)
			}
			var lines []string
			for _, h := range found {
				if strings.HasPrefix(h.Why.ID, "publish_swallow_") {
					if len(h.Why.Args) != 2 || h.Why.Args[0] != "line" {
						t.Errorf("置換 line が無い: %+v", h.Why)
						continue
					}
					lines = append(lines, h.Why.Args[1])
					if !strings.HasPrefix(h.Why.Text, h.Why.Args[1]+"行目") {
						t.Errorf("文面が疑う物理行から始まらない: %q", h.Why.Text)
					}
				}
			}
			if !slices.Equal(lines, tt.lines) {
				t.Errorf("疑う物理行 = %v、want %v", lines, tt.lines)
			}
		})
	}
}

// TestHazardAcceptable は、確かめたうえで通す指定で通せる形を固定する。
//
// 通せるのは、続きの行が単独で読むとレコードに見える形だけである。正当な複数行の
// 値でも当たる（ml-continuation-looks-like-row、ml-source-translated-2col）。
// 閉じ引用符の後ろに文字が続く形は、どの書き手も作らないので通させない。
//
// 指定はレコード単位なので、形が通せても、key で1つのレコードに名指せなければ
// 通させない（決まったことの 16）。
func TestHazardAcceptable(t *testing.T) {
	for _, tc := range []struct {
		id    string
		shape bool
	}{
		{reason.PublishSwallowKeyShaped, true},
		{reason.PublishSwallowSameColumns, true},
		{reason.PublishSwallowTextAfterQuote, false},
		{reason.PublishUnclosedQuote, false},
		{reason.PublishLoneCR, false},
		{reason.PublishCRCut, false},
		{reason.PublishHashHeader, false},
		{reason.PublishNoKeyColumn, false},
		{reason.PublishRowsUnread, false},
	} {
		named := Hazard{Why: reason.New(tc.id, ""), Key: shapeK1, KeyRecords: 1}
		if got := named.AcceptableShape(); got != tc.shape {
			t.Errorf("%s: AcceptableShape = %v、want %v", tc.id, got, tc.shape)
		}
		if got := named.Acceptable(); got != tc.shape {
			t.Errorf("%s: key で名指せるときの Acceptable = %v、want %v", tc.id, got, tc.shape)
		}
		// key が無い（ヘッダー、key 列も原文も空）、指定に使えない文字（空白、改行）、
		// 同じ key のレコードが2つ。
		for _, h := range []Hazard{
			{Why: named.Why},
			{Why: named.Why, Key: "bad key", KeyRecords: 1},
			{Why: named.Why, Key: "line:a\nb", KeyRecords: 1},
			{Why: named.Why, Key: shapeK1, KeyRecords: 2},
		} {
			if h.Acceptable() {
				t.Errorf("%s: key で1つに名指せないのに通せる: %+v", tc.id, h)
			}
		}
	}
}

// TestNameableKey は、確かめたうえで通す指定に書ける key を固定する。16桁の16進と
// 台詞ID（R10 の文字）は書ける。ほかの文字を含む key のレコードは publish が書かない。
func TestNameableKey(t *testing.T) {
	for _, tc := range []struct {
		k    string
		want bool
	}{
		{shapeK1, true},
		{strings.ToUpper(shapeK1), true},
		{"line:0a0b_c.d-E", true},
		{"", false},
		{"bad key", false},
		{"a,b", false},
		{"\"k\"", false},
		{"k\r", false},
		{"k\n", false},
		{"k\t", false},
		{"キー", false},
		{"k$HOME", false},
		{"k'", false},
	} {
		if got := NameableKey(tc.k); got != tc.want {
			t.Errorf("NameableKey(%q) = %v、want %v", tc.k, got, tc.want)
		}
	}
}

// TestSameKey は key の比べ方を固定する。台詞ID は綴りのまま、それ以外は ASCII の
// 大文字小文字を区別しない（publish がキーを小文字にしてから扱う。R13）。
func TestSameKey(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{shapeK1, shapeK1, true},
		{shapeK1, strings.ToUpper(shapeK1), true},
		{shapeK1, shapeK2, false},
		{"line:abc", "line:abc", true},
		{"line:abc", "line:ABC", false},
		{"line:abc", "LINE:abc", false},
		{"LINE:abc", "line:ABC", false},
		// 台詞ID の形でない key どうしは畳む。
		{"LINE:abc", "Line:ABC", true},
		// ASCII のほかの文字では畳まない（K は KELVIN SIGN）。
		{"k", "K", false},
	} {
		if got := SameKey(tc.a, tc.b); got != tc.want {
			t.Errorf("SameKey(%q, %q) = %v、want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestCheckShapeSwallowKeys は、飲み込みの Hazard に、飲み込んだレコードの key と、
// 同じファイルで同じ key を持つレコードの数が入ることを見る（決まったことの 16）。
func TestCheckShapeSwallowKeys(t *testing.T) {
	keyOne, keyTwo := key.For("one"), key.For("two")
	for _, tt := range []struct {
		name  string
		input string
		// keys と counts は、飲み込みの Hazard ごとの Key と KeyRecords。
		keys   []string
		counts []int
		// acceptable は、形が通せる Hazard が指定で通せるか。
		acceptable []bool
	}{
		{
			name: "key 列の値",
			input: "key,source_en,translation\n" +
				keyOne + ",one,\"いち\n" + keyTwo + ",two,に\"\n",
			keys: []string{keyOne}, counts: []int{1}, acceptable: []bool{true},
		},
		{
			// 大文字の16進は綴りのまま返す。比べるときは SameKey で畳む。
			name: "key 列の値（大文字）",
			input: "key,source_en,translation\n" +
				strings.ToUpper(keyOne) + ",one,\"いち\n" + keyTwo + ",two,に\"\n",
			keys: []string{strings.ToUpper(keyOne)}, counts: []int{1}, acceptable: []bool{true},
		},
		{
			// key 列が空なら、原文から作るキー（R14）。
			name: "key 列が空",
			input: "key,section,node,order,speaker,source_en,translation\n" +
				",UI,,,UI,one,\"いち\n,,,,,,に\"\n",
			keys: []string{keyOne}, counts: []int{1}, acceptable: []bool{true},
		},
		{
			// 2列の作業コピーでは原文から作るキー。原文が行をまたげば、その全体から作る。
			// 続きの行「para2",段落」は、単独で読むとヘッダーと同じ2列に見える。
			name:  "2列の作業コピー",
			input: "source_en,translation\n\"para1\npara2\",段落\n",
			keys:  []string{key.For("para1\npara2")}, counts: []int{1}, acceptable: []bool{true},
		},
		{
			// 同じ key のレコードがほかにあれば数える。形の崩れの無いレコードも入れる。
			name: "同じ key のレコードが2つ",
			input: "key,source_en,translation\n" +
				keyOne + ",one,\"いち\n" + keyTwo + ",two,に\"\n" +
				strings.ToUpper(keyOne) + ",one,いち\n",
			keys: []string{keyOne}, counts: []int{2}, acceptable: []bool{false},
		},
		{
			// key 列の値に空白があると指定に書けない。publish もこのレコードを書かない。
			name: "key 列の値に空白",
			input: "key,source_en,translation\n" +
				"bad key,one,\"いち\n" + keyTwo + ",two,に\"\n",
			keys: []string{"bad key"}, counts: []int{1}, acceptable: []bool{false},
		},
		{
			// key 列も原文も空なら key は無い。
			name: "key 列も原文も空",
			input: "key,source_en,translation\n" +
				",,\"いち\n" + keyTwo + ",two,に\"\n",
			keys: []string{""}, counts: []int{0}, acceptable: []bool{false},
		},
		{
			// 飲み込んだのがヘッダーなら、レコードが無いので key も無い。
			name:  "ヘッダーが飲み込む",
			input: "key,\"translation\n" + keyTwo + ",に\"\n" + keyOne + ",いち\n",
			keys:  []string{""}, counts: []int{0}, acceptable: []bool{false},
		},
		{
			// 閉じ引用符の後ろに文字が続く形にも key は入る（報告と、当たらない指定の
			// 理由に使う）が、形が通せないので通さない。
			name:  "閉じ引用符の後ろに文字が続く",
			input: "source_en,translation\none,\"いち\n\"Alpha line\nBeta line\",に\ntwo,さん\n",
			keys:  []string{keyOne}, counts: []int{1}, acceptable: []bool{false},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var keys []string
			var counts []int
			var acceptable []bool
			for _, h := range CheckShape([]byte(tt.input)) {
				if !strings.HasPrefix(h.Why.ID, "publish_swallow_") {
					if h.Key != "" || h.KeyRecords != 0 {
						t.Errorf("飲み込みでない形に key を入れている: %+v", h)
					}
					continue
				}
				keys = append(keys, h.Key)
				counts = append(counts, h.KeyRecords)
				acceptable = append(acceptable, h.Acceptable())
			}
			if !slices.Equal(keys, tt.keys) || !slices.Equal(counts, tt.counts) || !slices.Equal(acceptable, tt.acceptable) {
				t.Errorf("key %q、数 %v、通せる %v\nwant key %q、数 %v、通せる %v",
					keys, counts, acceptable, tt.keys, tt.counts, tt.acceptable)
			}
		})
	}
}

// TestCheckShapeLoneCR は (g) のうち、値の中の単独の CR を見る。
func TestCheckShapeLoneCR(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []wantHazard
		// args は理由の置換に入っているはずの並び。
		args []string
	}{
		{
			name:  "訳の中の単独の CR",
			input: "key,translation\n" + shapeK1 + ",\"い\rち\"\n",
			want:  []wantHazard{{reason.PublishLoneCR, 2, 3, false}},
			args:  []string{"column", "translation"},
		},
		{
			// 訳の入った行なら、ほかの列の単独の CR でも止める。上流の道具は行ごと落とす。
			name:  "訳の入った行の speaker の単独の CR",
			input: "key,speaker,translation\n" + shapeK1 + ",\"U\rI\",訳\n",
			want:  []wantHazard{{reason.PublishLoneCR, 2, 3, false}},
			args:  []string{"column", "speaker"},
		},
		{
			// 原文なら、直し方を分けるためにキーの決まり方を添える（LoneCRKeyKind）。
			// 列名は、publish が列を引くときと同じく大文字小文字を問わない。
			name:  "訳の入った行の原文の単独の CR",
			input: "key,Source_EN,translation\n" + key.For("a\rb") + ",\"a\rb\",訳\n",
			want:  []wantHazard{{reason.PublishLoneCR, 2, 3, false}},
			args:  []string{"column", "Source_EN", "key_kind", LoneCRKeyMatches},
		},
		{
			// 訳の空の行はどちらの道具でも公開されないので止めない。失うものが無い。
			name:  "訳の空の行の原文の単独の CR",
			input: "key,source_en,translation\n" + key.For("a\rb") + ",\"a\rb\",\n",
		},
		{
			// CRLF と LF は単独の CR ではない。
			name:  "値の中の CRLF",
			input: "key,translation\n" + shapeK1 + ",\"い\r\nち\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := CheckShape([]byte(tt.input))
			if got := hazardsOf(found); !slices.Equal(got, tt.want) {
				t.Errorf("\n got %+v\nwant %+v", got, tt.want)
			}
			for _, h := range found {
				if !slices.Equal(h.Why.Args, tt.args) {
					t.Errorf("置換 = %q、want %q", h.Why.Args, tt.args)
				}
			}
		})
	}
}

// TestLoneCRKeyKind は、原文に単独の CR がある行のキーの決まり方を見る。
//
// 直し方（cmd/dwloc）はこれで分かれる。台詞ID の行はキーを原文から作らないので
// 原文を直してよく、キーがいまの原文から作ったものなら直させない。列名だけで
// 分けると、台詞ID の行でも「訳を空に戻す」と案内し、公開できる訳を捨てさせる
// （検証の指摘）。キーの決め方は publish が書くときと同じ（rowKey）でなければならない。
func TestLoneCRKeyKind(t *testing.T) {
	const cr = "Alpha\rBeta"
	lfKey := key.For("Alpha\nBeta")
	for _, tc := range []struct {
		name, input, want string
	}{
		{"台詞ID の行", "key,source_en,translation\nline:0a0b0c01,\"" + cr + "\",訳\n", LoneCRKeyLineID},
		// 台詞ID は key 列の前後の空白を除いて見る（R11・R12）。
		{"前後に空白のある台詞ID", "key,source_en,translation\n\" line:0a0b0c01 \",\"" + cr + "\",訳\n", LoneCRKeyLineID},
		{"キーがいまの原文から作ったもの", "key,source_en,translation\n" + key.For(cr) + ",\"" + cr + "\",訳\n", LoneCRKeyMatches},
		// キーは小文字にしてから比べる（R13）。
		{"キーが大文字の16進", "key,source_en,translation\n" + strings.ToUpper(key.For(cr)) + ",\"" + cr + "\",訳\n", LoneCRKeyMatches},
		{"key 列が空", "key,source_en,translation\n,\"" + cr + "\",訳\n", LoneCRKeyFromSource},
		{"key 列が空白だけ", "key,source_en,translation\n\"  \",\"" + cr + "\",訳\n", LoneCRKeyFromSource},
		{"key 列の無い2列の作業コピー", "source_en,translation\n\"" + cr + "\",訳\n", LoneCRKeyFromSource},
		{"改行を LF にそろえると一致する", "key,source_en,translation\n" + lfKey + ",\"" + cr + "\",訳\n", LoneCRKeyMatchesLF},
		// CRLF も LF にそろえてから比べる。単独の CR だけを直すと一致しない。
		{"CRLF と単独の CR が混ざる", "key,source_en,translation\n" + key.For("a\nb\nc") + ",\"a\r\nb\rc\",訳\n", LoneCRKeyMatchesLF},
		// CR を取り除くと一致する形は、LF にそろえる直し方では直らないので合わない扱い。
		{"CR を取り除くと一致する", "key,source_en,translation\n" + key.For("AlphaBeta") + ",\"" + cr + "\",訳\n", LoneCRKeyMismatch},
		{"どちらでも合わない", "key,source_en,translation\n" + shapeK1 + ",\"" + cr + "\",訳\n", LoneCRKeyMismatch},
		{"キーの形でない key", "key,source_en,translation\nnot-a-key,\"" + cr + "\",訳\n", LoneCRKeyMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := csvfile.ReadPowerShell([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if len(f.Records) != 1 {
				t.Fatalf("レコードが %d 件", len(f.Records))
			}
			if got := LoneCRKeyKind(f.Records[0].Row); got != tc.want {
				t.Errorf("LoneCRKeyKind = %q、want %q", got, tc.want)
			}
			// 形の確かめも同じ値を置換に入れる。2列の作業コピーでは、続きの行が
			// ヘッダーと同じ列の数に見えるので、飲み込みの確かめも当たる。
			found := CheckShape([]byte(tc.input))
			i := slices.IndexFunc(found, func(h Hazard) bool { return h.Why.ID == reason.PublishLoneCR })
			if i < 0 {
				t.Fatalf("単独の CR で止めていない: %+v", found)
			}
			if got := found[i].Why.Args; !slices.Equal(got[2:], []string{"key_kind", tc.want}) {
				t.Errorf("形の確かめの置換 = %q、key_kind %q を期待", got, tc.want)
			}
		})
	}
}

// TestCheckShapeCRCut は (g) のうち、引用の外の単独の CR で切れた値を見る
// （決まったことの 11 と 14）。見分けは csvfile.FindCRCuts で、ここでは publish が
// それを止める理由にすることと、ファイルで分けることを固定する。
func TestCheckShapeCRCut(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []wantHazard
		// next は理由の置換 line に入っているはずの物理行。
		next string
	}{
		{
			// lone-cr-unquoted-value と同じ形。ゲームは「いち」と読み、publish は「い」を書く。
			name: "値の途中の単独の CR",
			input: shapeWorkingCRLF + key.For("one") + ",UI,,,UI,one,い\rち\r\n" +
				key.For("two") + ",UI,,,UI,two,に\r\n",
			want: []wantHazard{{reason.PublishCRCut, 2, 2, false}},
			next: "3",
		},
		{
			// 値が CR の直後の '#' で切れる。後半はコメントとして落ちる。
			name: "CR の直後が '#'",
			input: "key,translation\n" + shapeK1 + ",い\r# ち\n" +
				shapeK2 + ",に\n",
			want: []wantHazard{{reason.PublishCRCut, 2, 2, false}},
			next: "3",
		},
		{
			// 訳の空の行でも止める。切れた後半が訳を持つことがある。
			name:  "切れた後半に訳がある",
			input: "key,source_en,translation\n" + key.For("one") + ",o\rne,訳\n",
			want:  []wantHazard{{reason.PublishCRCut, 2, 2, false}},
			next:  "3",
		},
		{
			// 行の区切りがすべて単独の CR のファイルは、CR だけの改行のファイルとして読む。
			name:  "CR だけの改行のファイル",
			input: "key,translation\r# ===== L =====\r" + shapeK1 + ",a\r\r" + shapeK2 + ",b\r",
		},
		{
			// lone-cr-line-end と同じ形。次の行がレコードに見えるので、改行しただけと見る。
			name:  "行末の単独の CR",
			input: shapeH6 + shapeK1 + ",UI,,,UI,a\r" + shapeK2 + ",UI,,,UI,b\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := CheckShape([]byte(tt.input))
			if got := hazardsOf(found); !slices.Equal(got, tt.want) {
				t.Errorf("\n got %+v\nwant %+v", got, tt.want)
			}
			for _, h := range found {
				if !slices.Equal(h.Why.Args, []string{"line", tt.next}) {
					t.Errorf("置換が違う: %+v", h.Why.Args)
				}
			}
		})
	}
}

// TestCheckShapeUnreadLines は (d) を見る。
//
// 読み手が分ける改行（LF・CRLF・CR）より広く分けて、レコードになる行が2行以上
// あるのに、読み手が1行も読めないときに止める。
func TestCheckShapeUnreadLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []wantHazard
	}{
		{
			// U+2028 で行を分けたファイル。読み手には1行に見え、全体がヘッダーになる。
			name:  "LS で区切ったファイル",
			input: "key,source_en,translation " + shapeK1 + ",,a ",
			want: []wantHazard{
				{reason.PublishRowsUnread, 0, 0, false},
				{reason.PublishNoTranslationColumn, 1, 1, false},
			},
		},
		{
			// 先頭のコメントと NEL（U+0085）でつながり、読み手には全体がコメントに見える。
			name:  "NEL でコメントにつながったファイル",
			input: "# note\u0085key,translation\u0085" + shapeK1 + ",a\n",
			want:  []wantHazard{{reason.PublishRowsUnread, 0, 0, false}},
		},
		{
			// 広く分けてレコードになる行が1行だけで、読み手にはヘッダーも無い。
			// 行が1行でも、ヘッダーが無いのに読める行があれば読み違えている。
			name:  "コメントに LS でつながった1行だけ",
			input: "# c " + shapeK1 + ",x\n",
			want:  []wantHazard{{reason.PublishRowsUnread, 0, 0, false}},
		},
		{
			// CR だけの改行は読み手が分けるので止めない。
			name:  "CR だけの改行",
			input: "key,translation\r" + shapeK1 + ",a\r",
		},
		{
			// データ行が無いのは、訳が1つも無いファイルとしてありうる。
			name:  "ヘッダーだけ",
			input: "key,translation\r\n",
		},
		{
			// データ行がレコードにならない行（"," や '""'）だけなら、読めない行は無い。
			name:  "レコードにならない行だけ",
			input: "key,translation\n,\n\"\"\n",
		},
		{
			// 行をまたぐヘッダーだけでデータの無いファイル。広く分けると2行になるが、
			// 読み手は正しく0件と読んでいる。行単位で数えていたときは誤って止めていた。
			name:  "行をまたぐヘッダーだけ",
			input: "key,\"trans\nlation\",translation\n",
		},
		{
			// 値の中の U+2028 は行の区切りではなく、読み手はレコードを読めている。
			name:  "値の中の LS",
			input: "key,translation\n" + shapeK1 + ",い ち\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hazardsOf(CheckShape([]byte(tt.input))); !slices.Equal(got, tt.want) {
				t.Errorf("\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// TestCheckShapeUnclosed は (e) を見る。範囲は引用符が開いた物理行から、
// ファイルの最後の物理行まで。
func TestCheckShapeUnclosed(t *testing.T) {
	tests := []struct {
		name    string
		current string
		want    []wantHazard
	}{
		{
			// 最終行で開いた引用符は1行に収まるが、閉じていない。ゲームは改行まで
			// 訳に含めて読む。
			name:    "最終行で開いた引用符",
			current: shapeH6 + shapeK1 + ",UI,,,UI,a\n" + shapeK2 + ",UI,,,UI,\"b\n",
			want:    []wantHazard{{reason.PublishUnclosedQuote, 3, 3, false}},
		},
		{
			name:    "ヘッダーで開いた引用符",
			current: "key,section,node,order,speaker,\"translation\n" + shapeK1 + ",UI,,,UI,a\n",
			want:    []wantHazard{{reason.PublishUnclosedQuote, 1, 2, false}},
		},
		{
			name:    "行をまたいでから閉じない値",
			current: shapeH6 + shapeK1 + ",UI,,,UI,\"b\nc\n",
			want:    []wantHazard{{reason.PublishUnclosedQuote, 2, 3, false}},
		},
		{
			// 閉じない引用符のレコードの中の単独の CR は見ない。値にファイルの
			// 終わりまでが入っているので、どの値の話か決められない。
			name:    "閉じない値の中の単独の CR",
			current: shapeH6 + shapeK1 + ",UI,,,UI,\"b\rc\n",
			want:    []wantHazard{{reason.PublishUnclosedQuote, 2, 3, false}},
		},
		{
			name:    "ふつうの公開ファイル",
			current: shapeH6 + "\n# ===== UI =====\n" + shapeK1 + ",UI,,,UI,\"a, \"\"b\"\"\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hazardsOf(CheckShape([]byte(tt.current))); !slices.Equal(got, tt.want) {
				t.Errorf("\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// TestCheckTargetShapeReadsEveryFile は、入力・書き出し先・ゲーム側の公開ファイルの
// すべてを見ることと、読めないときに誤りを返すことを見る。
func TestCheckTargetShapeReadsEveryFile(t *testing.T) {
	// 飲み込み（引用符が別の行で閉じる形）。
	swallow := shapeH6 + shapeK1 + ",UI,,,UI,\"a\n" + shapeK2 + ",UI,,,UI,b\"\n"

	t.Run("入力と書き出し先の両方で見つける", func(t *testing.T) {
		target := shapeTarget(t, swallow, "key,translation\n"+shapeK2+",\"c\rd\"\n")
		found, err := CheckTargetShape(target)
		if err != nil {
			t.Fatal(err)
		}
		want := []wantHazard{
			{reason.PublishLoneCR, 2, 3, false},
			{reason.PublishSwallowKeyShaped, 2, 3, true},
		}
		if got := hazardsOf(found); !slices.Equal(got, want) {
			t.Errorf("\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("入力と書き出し先が同じファイルでも見つける", func(t *testing.T) {
		// 作業コピーの無いロケールと --path の経路。飲み込みの守りは、ここでも効かないと
		// いけない（批評の high）。同じファイルの中で訳を比べても食い違いは見えない。
		found, err := CheckTargetShape(shapeTarget(t, swallow, ""))
		if err != nil {
			t.Fatal(err)
		}
		want := []wantHazard{{reason.PublishSwallowKeyShaped, 2, 3, true}}
		if got := hazardsOf(found); !slices.Equal(got, want) {
			t.Errorf("\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("ゲーム側の公開ファイルでも見つける", func(t *testing.T) {
		target := shapeTarget(t, shapeH6+shapeK1+",UI,,,UI,a\n", "key,translation\n"+shapeK1+",a2\n")
		target.GameBase = filepath.Join(t.TempDir(), StringsFile)
		writeFile(t, target.GameBase, shapeH6+shapeK1+",UI,,,UI,\"a\n")
		found, err := CheckTargetShape(target)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 1 || found[0].Why.ID != reason.PublishUnclosedQuote ||
			!found[0].GameBase || found[0].Current || found[0].Path != target.GameBase {
			t.Errorf("ゲーム側の公開ファイルの閉じない引用符: %+v", found)
		}
	})

	t.Run("書き出し先がまだ無ければ入力だけを見る", func(t *testing.T) {
		root := t.TempDir()
		input := filepath.Join(root, "in.csv")
		writeFile(t, input, "key,translation\n"+shapeK2+",\"c\"\n")
		// 書き出し先が無いときは、土台の確かめもゲーム側の公開ファイルを読まない。
		game := filepath.Join(root, "game.csv")
		writeFile(t, game, shapeH6+shapeK1+",UI,,,UI,\"a\n")
		found, err := CheckTargetShape(Target{Locale: "xx", Input: input, Output: filepath.Join(root, "out.csv"), GameBase: game})
		if err != nil || len(found) != 0 {
			t.Errorf("%+v %v", found, err)
		}
	})

	t.Run("ゲーム側の公開ファイルが無ければ見ない", func(t *testing.T) {
		target := shapeTarget(t, shapeH6+shapeK1+",UI,,,UI,a\n", "key,translation\n"+shapeK1+",a2\n")
		target.GameBase = filepath.Join(t.TempDir(), StringsFile)
		if found, err := CheckTargetShape(target); err != nil || len(found) != 0 {
			t.Errorf("%+v %v", found, err)
		}
	})

	t.Run("読めなければどのファイルかを添えて誤りを返す", func(t *testing.T) {
		root := t.TempDir()
		good := filepath.Join(root, "good.csv")
		writeFile(t, good, shapeH6)

		for _, tc := range []struct {
			name, input, output, game, wantPath string
		}{
			{"入力がディレクトリ", root, good, "", root},
			{"書き出し先がディレクトリ", good, root, "", root},
			{"ゲーム側の公開ファイルがディレクトリ", good, good, root, root},
		} {
			found, err := CheckTargetShape(Target{Locale: "xx", Input: tc.input, Output: tc.output, GameBase: tc.game})
			var shapeErr *ShapeError
			if !errors.As(err, &shapeErr) {
				t.Errorf("%s: 誤りが %v（%+v）", tc.name, err, found)
				continue
			}
			if shapeErr.Path != tc.wantPath || !strings.HasPrefix(shapeErr.Error(), tc.wantPath+": ") {
				t.Errorf("%s: どのファイルかが違う: %v", tc.name, shapeErr)
			}
			if errors.Unwrap(err) == nil {
				t.Errorf("%s: 元の誤りを取り出せない", tc.name)
			}
		}
	})

	// 列名の重複は形の確かめでは誤りにしない。形の確かめは組み立てより前に走るので、
	// ここで誤りにすると、入力の列名の重複が「変換できない」でなく「読めない」と
	// 伝わる。組み立て（Build）と失われる訳の確かめ（CheckLoss）が誤りにする。
	t.Run("列名の重複は組み立てと失われる訳の確かめに任せる", func(t *testing.T) {
		root := t.TempDir()
		good := filepath.Join(root, "good.csv")
		writeFile(t, good, shapeH6)
		dup := filepath.Join(root, "dup.csv")
		writeFile(t, dup, "key,Key,translation\n"+shapeK1+","+shapeK1+",a\n")

		for _, tc := range []struct{ name, input, output string }{
			{"入力の列名が重複", dup, good},
			{"書き出し先の列名が重複", good, dup},
			{"同じファイルで列名が重複", dup, dup},
		} {
			found, err := CheckTargetShape(Target{Locale: "xx", Input: tc.input, Output: tc.output})
			if err != nil || len(found) != 0 {
				t.Errorf("%s: %+v %v", tc.name, found, err)
			}
		}
		var dupErr *csvfile.DuplicateColumnError
		if _, _, err := BuildTarget(nil, Target{Input: dup, Output: good}); !errors.As(err, &dupErr) {
			t.Errorf("組み立てが列名の重複を誤りにしない: %v", err)
		}
		if _, err := CheckTargetLoss(Target{Input: good, Output: dup}, []byte(shapeH6)); !errors.As(err, &dupErr) {
			t.Errorf("失われる訳の確かめが列名の重複を誤りにしない: %v", err)
		}
	})
}

// TestCheckOrderShape は、再生順のデータ（data/script_order.csv と data/level_flow.csv）の
// 閉じない引用符と、publish が引用せずにそのまま書く値の改行を見つけることを見る
// （決まったことのそのほか 8）。
//
// 全体を解釈して読むと、引用した値に改行が入りうる。見出しの行へそのまま書くと、
// 見出しの2行目が '#' で始まらない行になり、次に publish で読むときデータの行として
// 読まれる。上流も同じ壊れ方をする。
func TestCheckOrderShape(t *testing.T) {
	const orderRow = "L01 Ryan,intro,N1,1,line:aa," + shapeK1 + ",Ryan,\n"
	type found struct {
		file, id, column string
		line, end        int
	}
	tests := []struct {
		name        string
		order, flow string
		want        []found
	}{
		{name: "どちらもふつう", order: orderHeader + orderRow, flow: flowHeader + "0,Ryan,Sunny,,\n"},
		{name: "どちらも無い"},
		{
			// 見出しに使う列ごとに1件。改行は LF・CRLF・単独の CR のどれでも止める。
			name: "script_order.csv の見出しに使う値",
			order: orderHeader +
				"\"L01\nRyan\",intro,N1,1,line:aa," + shapeK1 + ",Ryan,\n" +
				"L01 Ryan,\"in\r\ntro\",\"N\r2\",2,line:bb," + shapeK2 + ",Ryan,\"$a\n$b\"\n",
			want: []found{
				{"script_order.csv", reason.PublishOrderLineBreak, "section", 2, 3},
				{"script_order.csv", reason.PublishOrderLineBreak, "phase", 4, 7},
				{"script_order.csv", reason.PublishOrderLineBreak, "node", 4, 7},
				{"script_order.csv", reason.PublishOrderLineBreak, "condition", 4, 7},
			},
		},
		{
			// order 列は本体の行へエスケープせずに書く（移植仕様 R24f）。
			name:  "script_order.csv の order 列",
			order: orderHeader + "L01 Ryan,intro,N1,\"1\n2\",line:aa," + shapeK1 + ",Ryan,\n",
			want:  []found{{"script_order.csv", reason.PublishOrderLineBreak, "order", 2, 3}},
		},
		{
			// 話者は本体の行へ引用して書くので、改行があっても行は割れない。
			name:  "script_order.csv の speaker 列は見ない",
			order: orderHeader + "L01 Ryan,intro,N1,1,line:aa," + shapeK1 + ",\"Ry\nan\",\n",
		},
		{
			name: "level_flow.csv の見出しに使う値",
			flow: flowHeader + "0,\"Ry\nan\",Sunny,\"a | b\",\"c\nd\"\n",
			want: []found{
				{"level_flow.csv", reason.PublishOrderLineBreak, "dragon", 2, 4},
				{"level_flow.csv", reason.PublishOrderLineBreak, "end_flags", 2, 4},
			},
		},
		{
			// 閉じない引用符のファイルは、値の改行を見ない。そこから後ろが1つの値に
			// 崩れているので、直す先は引用符である。
			name:  "閉じない引用符",
			order: orderHeader + "\"L01\nRyan,intro,N1,1,line:aa," + shapeK1 + ",Ryan,\n",
			flow:  "level,dragon\n0,\"Ryan\n",
			want: []found{
				{"script_order.csv", reason.PublishUnclosedQuote, "", 2, 3},
				{"level_flow.csv", reason.PublishUnclosedQuote, "", 2, 2},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.order != "" {
				writeFile(t, ScriptOrderPath(root), tt.order)
			}
			if tt.flow != "" {
				writeFile(t, LevelFlowPath(root), tt.flow)
			}
			hazards, err := CheckOrderShape(root)
			if err != nil {
				t.Fatal(err)
			}
			var got []found
			for _, h := range hazards {
				if !h.Order || h.Locale != "" || h.Current || h.GameBase {
					t.Errorf("再生順のデータの印が違う: %+v", h)
				}
				column := ""
				for i := 0; i+1 < len(h.Why.Args); i += 2 {
					if h.Why.Args[i] == "column" {
						column = h.Why.Args[i+1]
					}
				}
				got = append(got, found{filepath.Base(h.Path), h.Why.ID, column, h.Line, h.EndLine})
				if h.Acceptable() {
					t.Errorf("確かめたうえで通せる形にしている: %+v", h)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}

	t.Run("読めないファイル", func(t *testing.T) {
		root := t.TempDir()
		mustMkdir(t, ScriptOrderPath(root))
		_, err := CheckOrderShape(root)
		var shapeErr *ShapeError
		if !errors.As(err, &shapeErr) || shapeErr.Path != ScriptOrderPath(root) {
			t.Errorf("読めないファイルを ShapeError にしていない: %v", err)
		}
	})
}

// TestHasSourceColumn は、ヘッダーに source_en 列があるか（作業コピーの形か）の
// 見分けを見る。publish --path が、作業コピーを渡されたら書かずに止めるのに使う。
func TestHasSourceColumn(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"公開ファイル", shapeH6 + shapeK1 + ",L01,N,1,A,訳\n", false},
		{"2列の作業コピー", "source_en,translation\nAn invented line,訳\n", true},
		{"7列の作業コピー", "key,section,node,order,speaker,source_en,translation\n", true},
		{"列名の大文字小文字は区別しない", "key,Source_EN,translation\n", true},
		{"コメントと空行の後ろのヘッダー", "# memo\n\nkey,source_en,translation\n", true},
		{"空のファイル", "", false},
		{"ヘッダーの中で引用符が閉じない", "key,\"source_en,translation\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasSourceColumn([]byte(tt.data)); got != tt.want {
				t.Errorf("HasSourceColumn = %v, want %v", got, tt.want)
			}
		})
	}
}
