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
// 中身が守りを入れる前の dwloc と1バイトも変わらないことも見る（wantOut。
// 守りを入れる前の dwloc で書き出したものを写してある）。wantOut が空のものは、
// 形は通るが、訳が失われる確かめ（[CheckLoss]）で止まる入力で、書き出されない。
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
			// 1行ずつ読むと訳が line1 に切り詰められ、いまの公開ファイルも同じく
			// 読むので、守りを入れる前は終了コード0で書いていた。
			name:      "p-multiline-published",
			published: shapeH6 + shapeK1 + ",UI,,,UI,\"line1\nline2\"\n" + shapeK2 + ",UI,,,UI,b\n",
			want:      []wantHazard{{reason.PublishMultilineCurrent, 2, 3, true}},
		},
		{
			// 原文が行をまたぎ、訳が入っている。1行ずつ読むと行ごと捨てられ、
			// 新しい訳が公開ファイルに届かないまま終了コード0だった。
			name:      "p-multiline-source",
			published: shapeH6 + keyOne + ",UI,,,UI,a\n",
			working: shapeWorkingCRLF + keyOne + ",UI,,,UI,one,a\r\n" +
				key.For(shapeMultiSource) + ",UI,,,UI,\"" + shapeMultiSource + "\",訳\r\n",
			want: []wantHazard{{reason.PublishMultilineTranslated, 3, 5, false}},
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
			name:      "p-hash-in-quotes",
			published: shapeH6 + shapeK1 + ",UI,,,UI,\"first\n#second\"\n" + shapeK2 + ",UI,,,UI,b\n",
			want:      []wantHazard{{reason.PublishMultilineCurrent, 2, 3, true}},
		},
		{
			// 引用符なしのフィールドの途中の '"' は引用を開かない。偶奇だけを
			// 数えると止めてしまうが、どちらの読み方でも値は同じ。
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
			// 引用の中の単独の CR も、行単位の読み手は行の区切りにする。
			name:      "lone-cr-in-value",
			published: shapeH6 + shapeK1 + ",UI,,,UI,\"a\rb\"\n" + shapeK2 + ",UI,,,UI,b\n",
			want:      []wantHazard{{reason.PublishMultilineCurrent, 2, 3, true}},
		},
		{
			name:      "lone-cr-wc-source",
			published: shapeH6 + shapeK1 + ",UI,,,UI,a\n",
			working:   "key,source_en,translation\n" + shapeK1 + ",,a2\n" + key.For("he\rllo") + ",\"he\rllo\",x\n",
			want:      []wantHazard{{reason.PublishMultilineTranslated, 3, 4, false}},
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
				if h.Locale != "xx" || h.Path != wantPath {
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

// TestCheckInputShapeKeepsTheRealWorkingCopyShape は、ゲーム側の作業コピーの
// 実物と同じ形の入力で止まらず、組み立てた中身も変わらないことを固定する。
//
// 実物（ゲーム側の ja.working.csv）には、原文（source_en）が行をまたいで空行を
// 挟み、訳が空のレコードが1件ある。区切りは CRLF、値の中は LF である。その
// レコードは、1行ずつ読むと行ごと捨てられ（malformed dropped が2件増える）、
// 全体を読めば訳の空の行になる。どちらでも公開されないので、出力は同じになる。
// ここで止めると、翻訳者には直せない（原文を変えるとキーが変わる）理由で、
// そのロケールの publish が常に塞がる（検証の指摘 high）。
func TestCheckInputShapeKeepsTheRealWorkingCopyShape(t *testing.T) {
	multi := key.For(shapeMultiSource) + ",UI,,,UI,\"" + shapeMultiSource + "\",\r\n"
	before := shapeWorkingCRLF +
		key.For("one") + ",UI,,,UI,one,いち\r\n" +
		key.For("three") + ",UI,,,UI,three,\r\n"
	after := key.For("two") + ",UI,,,UI,two,に\r\n" +
		"line:aaaaaaaa,L01 Ryan,N1,1,Ryan,,せりふ\r\n"
	withMulti := before + multi + after
	without := before + after

	found, err := CheckInputShape([]byte(withMulti))
	if err != nil {
		t.Fatalf("CheckInputShape: %v", err)
	}
	if len(found) != 0 {
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
	// 行単位で読むと、そのレコードは1行目（原文のハッシュがキーと合わない）と
	// 続きの行（キーの形でない）の2件が捨てられる。実物の集計と同じ形。
	if stats.Dropped != 2 {
		t.Errorf("捨てた行 = %d、2 を期待（%+v）", stats.Dropped, stats)
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, current := range []bool{false, true} {
				check := CheckInputShape
				if current {
					check = CheckCurrentShape
				}
				found, err := check([]byte(tt.input))
				if err != nil {
					t.Fatalf("current=%v: %v", current, err)
				}
				// CheckCurrentShape の Current を埋めるのは呼び出し側なので、ここは false のまま。
				if got := hazardsOf(found); !slices.Equal(got, tt.want) {
					t.Errorf("current=%v\n got %+v\nwant %+v", current, got, tt.want)
				}
			}
		})
	}
}

// TestCheckInputShapeMultiline は (c) の細部を見る。
func TestCheckInputShapeMultiline(t *testing.T) {
	// 続きの行が、1行ずつ読むと別のキー（shapeK2）の訳 y" として読まれる原文。
	tricky := "x\n" + shapeK2 + ",,y"
	tests := []struct {
		name  string
		input string
		want  []wantHazard
	}{
		{
			// 行をまたぐレコードの訳は空だが、1行ずつ読むと続きの行が訳のある行になる。
			name:  "続きの行が別の訳として読まれる",
			input: "key,source_en,translation\n" + key.For(tricky) + ",\"" + tricky + "\",\n",
			want:  []wantHazard{{reason.PublishMultilineDiverges, 2, 3, false}},
		},
		{
			// 訳そのものが行をまたぐ。1行ずつ読むと1行目で切り詰められる。
			name:  "訳そのものが行をまたぐ",
			input: "key,translation\n" + shapeK1 + ",\"a\r\nb\"\r\n",
			want:  []wantHazard{{reason.PublishMultilineTranslated, 2, 3, false}},
		},
		{
			// 台詞ID行の訳が行をまたぐ場合も同じ。
			name:  "台詞ID行の訳が行をまたぐ",
			input: "key,translation\nline:aaaaaaaa,\"a\nb\"\n",
			want:  []wantHazard{{reason.PublishMultilineTranslated, 2, 3, false}},
		},
		{
			// ヘッダーが行をまたぐと、1行ずつ読んだヘッダーからは translation 列を
			// 引けない。全体を読むと訳が出てくるので、食い違いとしても止まる。
			name:  "ヘッダーが行をまたぐ",
			input: "key,\"source\n_en\",translation\n" + shapeK1 + ",,a\n",
			want: []wantHazard{
				{reason.PublishNoTranslationColumn, 1, 1, false},
				{reason.PublishMultilineDiverges, 1, 2, false},
			},
		},
		{
			// 1つは訳が入っていて、もう1つは続きの行が別の訳になる。両方を出す。
			name: "2つのレコードがそれぞれ別の理由で当たる",
			input: "key,source_en,translation\n" +
				shapeK1 + ",,\"a\nb\"\n" +
				key.For(tricky) + ",\"" + tricky + "\",\n",
			want: []wantHazard{
				{reason.PublishMultilineTranslated, 2, 3, false},
				{reason.PublishMultilineDiverges, 4, 5, false},
			},
		},
		{
			// 閉じない引用符があると、全体の読み方は後ろを飲み込むので突き合わせない。
			// 訳の入った行をまたぐレコードは、それとは別に出す。
			name: "閉じない引用符の前に訳の入った行をまたぐレコード",
			input: "key,translation\n" +
				shapeK1 + ",\"a\nb\"\n" +
				shapeK2 + ",\"c\n",
			want: []wantHazard{
				{reason.PublishMultilineTranslated, 2, 3, false},
				{reason.PublishUnclosedQuote, 4, 4, false},
			},
		},
		{
			// 訳の空の行をまたぐレコードの後ろに、閉じない引用符がある。全体の読み方は
			// 閉じない引用符から後ろを飲み込むので、突き合わせると訳が食い違って見え、
			// 行をまたぐレコードのほうを「別の訳として読まれる」と誤って指してしまう。
			// 閉じない引用符は (e) だけで出す。
			name: "閉じない引用符があれば訳を突き合わせない",
			input: "key,source_en,translation\n" +
				key.For("x\ny") + ",\"x\ny\",\n" +
				key.For("two") + ",two,\"c\n",
			want: []wantHazard{{reason.PublishUnclosedQuote, 4, 4, false}},
		},
		{
			// 行をまたいでから閉じないレコードに訳がある。そのレコードは (e) で出し、
			// (c) の「訳が入っている」では重ねて出さない。範囲が同じ2件が並ぶと、
			// 同じ行を2回直すよう読めてしまう。
			name: "行をまたいでから閉じないレコードに訳がある",
			input: "key,source_en,translation\n" +
				key.For("x\ny") + ",\"x\ny\",\n" +
				key.For("two") + ",two,\"c\nd\n",
			want: []wantHazard{{reason.PublishUnclosedQuote, 4, 5, false}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, err := CheckInputShape([]byte(tt.input))
			if err != nil {
				t.Fatal(err)
			}
			if got := hazardsOf(found); !slices.Equal(got, tt.want) {
				t.Errorf("\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// TestSameTranslationsFallsBackToTheFirstSpan は、どのレコードのせいかを決め
// られない食い違いでも止めることを見る。
//
// 食い違いは行をまたぐレコードからしか生まれないので、ふつうは範囲の中に
// 訳として読まれる行が見つかる。見つからないときでも、食い違いがあることは
// 確かなので、最初のレコードを指して止める。
func TestSameTranslationsFallsBackToTheFirstSpan(t *testing.T) {
	// 全体を読むと、訳の空のレコードが2〜3行目にまたがる（またいでいるのは note 列）。
	whole := csvfile.ReadPowerShellWhole([]byte("key,translation,note\n" + shapeK1 + ",,\"x\ny\"\n"))
	// 1行ずつ読んだ結果は手で作り、範囲の外に訳のある行を置く。
	table := csvfile.PowerShellTable{Header: []string{"key", "translation"}, HeaderLine: 1}
	table.Rows = []csvfile.NumberedRow{{Row: csvfile.NewRow(table.Header, []string{shapeK2, "b"}), Line: 9}}

	got := hazardsOf(multilineInputHazards(table, whole))
	want := []wantHazard{{reason.PublishMultilineDiverges, 2, 3, false}}
	if !slices.Equal(got, want) {
		t.Errorf("\n got %+v\nwant %+v", got, want)
	}
}

// TestSameTranslations は、訳の突き合わせが見るものと見ないものを固定する。
func TestSameTranslations(t *testing.T) {
	header := []string{"key", "speaker", "translation"}
	rows := func(recs ...[]string) *collected {
		var out []csvfile.Row
		for _, r := range recs {
			out = append(out, csvfile.NewRow(header, r))
		}
		return collectRows(out)
	}
	base := rows([]string{shapeK1, "UI", "a"}, []string{"line:aaaaaaaa", "", "せりふ"})

	tests := []struct {
		name  string
		other *collected
		same  bool
	}{
		{"同じ", rows([]string{shapeK1, "UI", "a"}, []string{"line:aaaaaaaa", "", "せりふ"}), true},
		// 並びと speaker は見ない。変わっても訳は失われない。
		{"並びと speaker が違う", rows([]string{"line:AAAAAAAA", "", "せりふ"}, []string{shapeK1, "Ryan", "a"}), true},
		{"訳が違う", rows([]string{shapeK1, "UI", "b"}, []string{"line:aaaaaaaa", "", "せりふ"}), false},
		{"キーが違う", rows([]string{shapeK2, "UI", "a"}, []string{"line:aaaaaaaa", "", "せりふ"}), false},
		{"台詞ID行の訳が違う", rows([]string{shapeK1, "UI", "a"}, []string{"line:aaaaaaaa", "", "ちがう"}), false},
		{"台詞IDが違う", rows([]string{shapeK1, "UI", "a"}, []string{"line:bbbbbbbb", "", "せりふ"}), false},
		{"行が少ない", rows([]string{shapeK1, "UI", "a"}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameTranslations(base, tt.other); got != tt.same {
				t.Errorf("sameTranslations = %v, want %v", got, tt.same)
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, err := CheckInputShape([]byte(tt.input))
			if err != nil {
				t.Fatal(err)
			}
			if got := hazardsOf(found); !slices.Equal(got, tt.want) {
				t.Errorf("\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// TestCheckCurrentShape は、いまの公開ファイルの側の (b)(e) を見る。
func TestCheckCurrentShape(t *testing.T) {
	tests := []struct {
		name    string
		current string
		want    []wantHazard
	}{
		{
			// 入力の側と違い、訳の有無によらず止める。
			name:    "訳の無い値が行をまたぐ",
			current: shapeH6 + shapeK1 + ",UI,\"N\n1\",,UI,a\n",
			want:    []wantHazard{{reason.PublishMultilineCurrent, 2, 3, false}},
		},
		{
			name:    "ヘッダーが行をまたぐ",
			current: "key,section,node,order,speaker,\"trans\nlation\"\n" + shapeK1 + ",UI,,,UI,a\n",
			want: []wantHazard{
				{reason.PublishNoTranslationColumn, 1, 1, false},
				{reason.PublishMultilineCurrent, 1, 2, false},
			},
		},
		{
			// 最終行で開いた引用符は1行に収まるが、閉じていない。ゲームは改行まで
			// 訳に含めて読む。
			name:    "最終行で開いた引用符",
			current: shapeH6 + shapeK1 + ",UI,,,UI,a\n" + shapeK2 + ",UI,,,UI,\"b\n",
			want:    []wantHazard{{reason.PublishUnclosedQuote, 3, 3, false}},
		},
		{
			// 1行ずつ読むと、閉じない引用符の中身 translation がそのまま列名になる
			// ので列はそろって見える。全体を読むと、ヘッダーがファイルの終わりまでを
			// 飲み込む。
			name:    "ヘッダーで開いた引用符",
			current: "key,section,node,order,speaker,\"translation\n" + shapeK1 + ",UI,,,UI,a\n",
			want:    []wantHazard{{reason.PublishUnclosedQuote, 1, 2, false}},
		},
		{
			// データ行で開いた引用符が行をまたぎ、閉じない。(e) だけで出し、(b) の
			// 「行をまたいでいる」では重ねて出さない。
			name:    "行をまたいでから閉じない値",
			current: shapeH6 + shapeK1 + ",UI,,,UI,\"b\nc\n",
			want:    []wantHazard{{reason.PublishUnclosedQuote, 2, 3, false}},
		},
		{
			name:    "ふつうの公開ファイル",
			current: shapeH6 + "\n# ===== UI =====\n" + shapeK1 + ",UI,,,UI,\"a, \"\"b\"\"\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, err := CheckCurrentShape([]byte(tt.current))
			if err != nil {
				t.Fatal(err)
			}
			if got := hazardsOf(found); !slices.Equal(got, tt.want) {
				t.Errorf("\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// TestCheckTargetShapeReadsBothFiles は、入力と書き出し先の両方を見ることと、
// 読めないときに誤りを返すことを見る。
func TestCheckTargetShapeReadsBothFiles(t *testing.T) {
	t.Run("入力と書き出し先の両方で見つける", func(t *testing.T) {
		target := shapeTarget(t,
			shapeH6+shapeK1+",UI,,,UI,\"a\nb\"\n",
			"key,translation\n"+shapeK2+",\"c\nd\"\n")
		found, err := CheckTargetShape(target)
		if err != nil {
			t.Fatal(err)
		}
		want := []wantHazard{
			{reason.PublishMultilineTranslated, 2, 3, false},
			{reason.PublishMultilineCurrent, 2, 3, true},
		}
		if got := hazardsOf(found); !slices.Equal(got, want) {
			t.Errorf("\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("書き出し先がまだ無ければ入力だけを見る", func(t *testing.T) {
		root := t.TempDir()
		input := filepath.Join(root, "in.csv")
		writeFile(t, input, "key,translation\n"+shapeK2+",\"c\"\n")
		found, err := CheckTargetShape(Target{Locale: "xx", Input: input, Output: filepath.Join(root, "out.csv")})
		if err != nil || len(found) != 0 {
			t.Errorf("%+v %v", found, err)
		}
	})

	t.Run("読めなければどのファイルかを添えて誤りを返す", func(t *testing.T) {
		root := t.TempDir()
		good := filepath.Join(root, "good.csv")
		writeFile(t, good, shapeH6)
		dup := filepath.Join(root, "dup.csv")
		writeFile(t, dup, "key,Key,translation\n"+shapeK1+","+shapeK1+",a\n")

		for _, tc := range []struct {
			name, input, output, wantPath string
		}{
			{"入力がディレクトリ", root, good, root},
			{"書き出し先がディレクトリ", good, root, root},
			{"入力の列名が重複", dup, good, dup},
			{"書き出し先の列名が重複", good, dup, dup},
			{"同じファイルで列名が重複", dup, dup, dup},
		} {
			found, err := CheckTargetShape(Target{Locale: "xx", Input: tc.input, Output: tc.output})
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
}
