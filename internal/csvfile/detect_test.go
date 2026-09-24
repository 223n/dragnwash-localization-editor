package csvfile

import (
	"slices"
	"strings"
	"testing"
)

// TestFindSwallowsOnFixture は、上流との突き合わせの入力の表の全件で、飲み込みの
// 検出が当たる入力とその場所を固定する。表に無い入力は、当たらないことを見る。
//
// 上流はどの入力も止めずに書く。飲み込みの8件（swallow-*）は、上流では英語の原文や
// キーが訳に入って公開される形で、どれも当たらなければならない。訳の空の行を
// 飲み込む形（swallow-7col-hash-close）はキーの形で、キー列の空いた英文の行
// （swallow-7col-empty-key-english）と2列の作業コピー（swallow-2col-hash-close）は
// 区切りの数で当たる（決まったことの 4）。飲み込まれた行が自分の値を引用符で開く
// 3件（swallow-*-own-quote）は、続きの行を単独で読むと引用が開いたまま終わり、
// キーの形にも区切りの数にも当たらない。閉じ引用符の後ろに文字が続くことで当たる。
//
// 実物と同じ形の複数行の原文（ml-source-real-shape）や、複数行の訳（ml-translation-*）、
// 値の中の '#' の行は当たってはいけない。当たると、正当なファイルの publish が
// 常に塞がる。
//
// 正当でも当たるものが2件ある。原文の2行目がカンマを多く含む形
// （ml-continuation-looks-like-row）と、2列の作業コピーで原文が行をまたぐ形
// （ml-source-translated-2col）である。形だけでは閉じ誤りと見分けられないので、
// 確かめたうえで通す指定（dwloc publish --accept-multiline）で書く。
func TestFindSwallowsOnFixture(t *testing.T) {
	want := map[string][]Swallow{
		"swallow-3col":                   {{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignSameColumns}},
		"swallow-7col-hash-close":        {{ID: 2, Line: 2, EndLine: 4, SwallowedLine: 3, Sign: SignKeyShaped}},
		"swallow-2col-hash-close":        {{ID: 2, Line: 2, EndLine: 4, SwallowedLine: 3, Sign: SignSameColumns}},
		"swallow-7col-empty-key-english": {{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignSameColumns}},
		"swallow-6col-published":         {{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignSameColumns}},
		"ml-continuation-looks-like-row": {{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignSameColumns}},
		"ml-source-translated-2col":      {{ID: 3, Line: 3, EndLine: 5, SwallowedLine: 5, Sign: SignSameColumns}},
		// 次の3件は閉じ引用符の後ろの文字で当たる。上の swallow-7col-empty-key-english と
		// swallow-6col-published も閉じ引用符の後ろに文字が続くが、区切りの数が先に当たる。
		"swallow-2col-own-quote":           {{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignTextAfterQuote}},
		"swallow-7col-empty-key-own-quote": {{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignTextAfterQuote}},
		"swallow-6col-published-own-quote": {{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignTextAfterQuote}},
	}
	cases := loadFixtureCases(t).Cases
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			got := FindSwallows(SplitSegments([]byte(c.Text)))
			if !slices.Equal(got, want[c.Name]) {
				t.Errorf("飲み込み\n got %+v\nwant %+v", got, want[c.Name])
			}
		})
	}
	for name := range want {
		if !slices.ContainsFunc(cases, func(c fixtureCase) bool { return c.Name == name }) {
			t.Errorf("表の %s は入力の表に無い", name)
		}
	}
}

// TestFindSwallows は、入力の表に無い形で飲み込みの検出の境目を見る。
func TestFindSwallows(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []Swallow
	}{
		{
			// 上流 main の hash-strings.ps1 が認める「key 列に英文」の追記の形を、いまの
			// 公開ファイルが飲み込む（批評の high、S1）。キーの形ではないが区切りの数で当たる。
			name: "公開ファイルが key 列に英文のある行を飲み込む",
			text: "key,section,node,order,speaker,translation\n" +
				"0123456789abcdef,UI,,,UI,\"いち\n" +
				"\"Hello, there\",UI,,,UI,こんにちは\n" +
				"fedcba9876543210,UI,,,UI,に\n",
			want: []Swallow{{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignSameColumns}},
		},
		{
			name: "台詞ID で始まる行を飲み込む",
			text: "key,translation\nk,\"a\nline:0a0b0c02,b\"\n",
			want: []Swallow{{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignKeyShaped}},
		},
		{
			// キーは前後の空白を除いて小文字にしてから見る（publish の R11、R13）。
			name: "大文字の16進と前の空白もキーの形",
			text: "key,section,translation\nk,s,\"a\n ABCDEF0123456789 ,x\"\n",
			want: []Swallow{{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignKeyShaped}},
		},
		{
			// ヘッダーの末尾の空の列も区切りに数える。表計算ソフトで保存すると付く形。
			name: "ヘッダーの末尾の空の列も数える",
			text: "key,translation,\nk,\"a\nx,y,\",\n",
			want: []Swallow{{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignSameColumns}},
		},
		{
			name: "ヘッダーが行をまたいでデータの行を飲み込む",
			text: "key,\"translation\nabc,x\",extra\n",
			want: []Swallow{{ID: 1, Line: 1, EndLine: 2, SwallowedLine: 2, Sign: SignSameColumns}},
		},
		{
			name: "区切りの数が違えば当たらない",
			text: "key,section,translation\nk,s,\"a\nx,y\nz,w,v,u\"\n",
		},
		{
			// 飲み込まれた行が自分の原文を引用符で開き、その引用符が one の訳の閉じ忘れを
			// 閉じる。訳は "いち\nAlpha line" になる。続きの行を単独で読むと引用が開いたまま
			// 終わる（区切りは1つ）ので、キーの形にも区切りの数にも当たらない。
			name: "2列の作業コピーで飲み込まれた行が引用符を開く",
			text: "source_en,translation\none,\"いち\n\"Alpha line\nBeta line\",に\ntwo,さん\n",
			want: []Swallow{{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignTextAfterQuote}},
		},
		{
			// キー列の空いた英文の行の原文が複数行。単独で読むと区切りは6つで、ヘッダーの7つと違う。
			name: "7列の作業コピーでキー列の空いた行が引用符を開く",
			text: "key,section,node,order,speaker,source_en,translation\r\n" +
				"7692c3ad3540bb80,UI,,,UI,one,\"いち\r\n" +
				",UI,,,UI,\"Alpha line\n\nBeta line\",\r\n" +
				"8b5b9db0c13db242,UI,,,UI,three,さん\r\n",
			want: []Swallow{{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignTextAfterQuote}},
		},
		{
			// 入力と書き出し先が同じ経路（公開ファイル）。key 列に複数行の英文のある追記の形を飲み込む。
			name: "公開ファイルで key 列の英文が引用符を開く",
			text: "key,section,node,order,speaker,translation\n" +
				"0123456789abcdef,UI,,,UI,\"いち\n" +
				"\"Alpha line\nBeta line\",UI,,,UI,訳\n" +
				"fedcba9876543210,UI,,,UI,に\n",
			want: []Swallow{{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignTextAfterQuote}},
		},
		{
			// 閉じ方で見るので、単独ではコメントになる行でも当たる。
			name: "閉じ引用符の後ろの文字は '#' の行でも見る",
			text: "key,translation\nk,\"a\n# x\"tail\n",
			want: []Swallow{{ID: 2, Line: 2, EndLine: 3, SwallowedLine: 3, Sign: SignTextAfterQuote}},
		},
		{
			// 値が改行や空白で終わると、閉じ引用符が行頭に来る。後ろは区切りか改行なので当たらない。
			name: "値が改行で終わる複数行の値",
			text: "key,section,translation\nk,s,\"a\n\"\nk2,s,\"b\n  \",\"c\n\"\n",
		},
		{
			// 同じ行の中で開いて閉じた引用の後ろの文字（3行目の "c"d）は、続きの行にあっても
			// 飲み込みの証拠にならない。
			name: "同じ行の中で閉じた引用の後ろの文字は見ない",
			text: "key,translation,note\nk,\"a\nb\",\"c\"d\n",
		},
		{
			// 単独で読むとコメントになる行は、レコードを飲み込んだ証拠にならない。
			// 読んでしまうと、この行は区切りの数がヘッダーと同じ（2つ）なので当たる。
			name: "値の中の '#' の行は見ない",
			text: "source_en,translation\none,\"いち\n# a, b\"\n",
		},
		{
			// 空行と空白だけの行も同じ。列が1つのヘッダーでは、読んでしまうと区切りの数が
			// 同じ（1つ）になって当たる。
			name: "値の中の空行と空白だけの行は見ない",
			text: "translation\n\"a\n\n  \nb,c\"\n",
		},
		{
			// 閉じない引用符は UnclosedQuoteError が先に止めるので、飲み込みとしては見ない。
			name: "閉じない引用符は見ない",
			text: "key,translation\nk,\"a\n0123456789abcdef,b\n",
		},
		{
			name: "ヘッダーの無いファイル",
			text: "# a\n\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FindSwallows(SplitSegments([]byte(tt.text))); !slices.Equal(got, tt.want) {
				t.Errorf("飲み込み\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// TestFindCRCuts は、引用符で囲まない値が単独の CR で切れた形を見つけることを固定する。
//
// ゲームは引用の外の CR を捨てて続きまで読むので、切れたまま公開すると訳を失う
// （決まったことの 11）。一方、行末の単独の CR で改行しただけのファイル
// （lone-cr-line-end、CR だけの改行のファイル）は、意図して読む形なので当たってはいけない。
func TestFindCRCuts(t *testing.T) {
	const header7 = "key,section,node,order,speaker,source_en,translation\r\n"
	tests := []struct {
		name string
		text string
		want []CRCut
	}{
		{
			name: "値の途中の単独の CR",
			text: header7 + "7692c3ad3540bb80,L01 Ember,Ember_1_intro,1,Ember,one,い\rち\r\n" +
				"3fc4ccfe745870e2,L01 Ember,Ember_1_intro,2,Moss,two,に\r\n",
			want: []CRCut{{ID: 2, Line: 2, EndLine: 2, NextLine: 3}},
		},
		{
			name: "行をまたいだレコードの後ろの単独の CR",
			text: "key,translation\nk,\"a\nb\"c\rd\n",
			want: []CRCut{{ID: 2, Line: 2, EndLine: 3, NextLine: 4}},
		},
		{
			name: "次の行がキーの形なら行末の CR",
			text: "key,section,node,order,speaker,translation\n0123456789abcdef,UI,,,UI,a\rfedcba9876543210,UI,,,UI,b\n",
		},
		{
			// 見出しのコメント行と空行の前でも単独の CR で改行する。
			name: "CR だけの改行のファイル",
			text: "key,translation\r# ===== L =====\r0123456789abcdef,a\r\rfedcba9876543210,b\r",
		},
		{
			// 空のレコードは読み手が黙って落とす行で、切れた値の後半ではない。
			name: "CR だけの改行のファイルの ',' と '\"\"' の行",
			text: "key,source_en,translation\r7692c3ad3540bb80,one,いち\r,\r3fc4ccfe745870e2,two,に\r\"\"\r8b5b9db0c13db242,three,さん\r",
		},
		{
			// 次の物理行だけを単独で読むと、原文の引用が開いたまま終わって区切りが6つになる。
			// レコード全体では7つで、ヘッダーと同じ。
			name: "CR だけの改行のファイルでキー列の空いた行の原文が行をまたぐ",
			text: "key,section,node,order,speaker,source_en,translation\r" +
				"7692c3ad3540bb80,UI,,,UI,one,いち\r" +
				",UI,,,UI,\"Alpha line\n\nBeta line\",\r" +
				"3fc4ccfe745870e2,UI,,,UI,two,に\r",
		},
		{
			// LF で改行したファイルでは、単独の CR の次が空行でも切れた値と見る
			// （決まったことの 14）。空白だけの行も空行である。
			name: "次の行が空白だけ",
			text: "key,translation\nk,a\r  \nk2,b\nk3,c\r\t\nk4,d\n",
			want: []CRCut{{ID: 2, Line: 2, EndLine: 2, NextLine: 3}, {ID: 5, Line: 5, EndLine: 5, NextLine: 6}},
		},
		{
			name: "次の行が全角空白だけ",
			text: "key,translation\nk,a\r　\nk2,b\n",
			want: []CRCut{{ID: 2, Line: 2, EndLine: 2, NextLine: 3}},
		},
		{
			name: "次の行が空行",
			text: "key,translation\r\nk,a\r\r\nk2,b\r\n",
			want: []CRCut{{ID: 2, Line: 2, EndLine: 2, NextLine: 3}},
		},
		{
			// 値が CR の直後の '#' で切れる形（い\r# ち）。後半はコメントとして落ちる。
			name: "次の行が '#' で始まる",
			text: "key,translation\nk,い\r# ち\nk2,b\n",
			want: []CRCut{{ID: 2, Line: 2, EndLine: 2, NextLine: 3}},
		},
		{
			name: "次の行が空のレコード",
			text: "key,translation\nk,い\r,\nk2,b\n",
			want: []CRCut{{ID: 2, Line: 2, EndLine: 2, NextLine: 3}},
		},
		{
			// CRLF で改行したファイルの行末に、単独の CR が1つだけ紛れ込んだ形。次の行が
			// レコードに見えるので、改行しただけと見る。
			name: "改行の混ざったファイルで次がレコード",
			text: "key,translation\r\n0123456789abcdef,a\rfedcba9876543210,b\r\n",
		},
		{
			// 行をまたぐレコードでも、区切りがヘッダーより少なくキーの形でもなければ当たる。
			name: "次のレコードが行をまたいでも区切りが足りない",
			text: "key,section,translation\nk,s,い\rち,\"x\ny\"\nk2,s,b\n",
			want: []CRCut{{ID: 2, Line: 2, EndLine: 2, NextLine: 3}},
		},
		{
			name: "最終行の単独の CR",
			text: "key,translation\nk,a\r",
		},
		{
			name: "ヘッダーの無いファイル",
			text: "# a\r",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FindCRCuts(SplitSegments([]byte(tt.text))); !slices.Equal(got, tt.want) {
				t.Errorf("単独の CR で切れた値\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}

	// 入力の表では、PR0 で未決に置いた lone-cr-unquoted-value だけが当たる。
	for _, c := range loadFixtureCases(t).Cases {
		got := FindCRCuts(SplitSegments([]byte(c.Text)))
		if (len(got) > 0) != (c.Name == "lone-cr-unquoted-value") {
			t.Errorf("%s: 単独の CR で切れた値 %+v", c.Name, got)
		}
	}

	// 入力の表の改行をすべて単独の CR に直した写し（CR だけの改行のファイル）では、
	// どの入力にも当たらない（決まったことの 14）。CR だけの改行のファイルは意図して
	// 読む形で、単独の CR は行の区切りと見分けられないためである。ファイルで分ける前は、
	// 次の2件が切れた値ではないのに当たっていた。
	//
	//   - soft-hyphen-comment: U+00AD のあとの '#' の行は、読み手が序数で比べて
	//     データのレコードにする（上流と意図して違える点）。単独ではレコードに見えない
	//   - ml-header: ヘッダーは3列（最後の列名が行をまたぐ）、データは2列で、キーの
	//     形でもない
	//
	// lone-cr-unquoted-value も、改行を単独の CR に直すと、値の中の CR と行の区切りを
	// 見分けられなくなり、当たらない。
	crOnly := strings.NewReplacer("\r\n", "\r", "\n", "\r")
	for _, c := range loadFixtureCases(t).Cases {
		text := crOnly.Replace(c.Text)
		segs := SplitSegments([]byte(text))
		if got := FindCRCuts(segs); len(got) > 0 {
			t.Errorf("%s（CR だけの改行）: 単独の CR で切れた値 %+v", c.Name, got)
		}
		// 改行を直した写しがほんとうに CR だけの改行になっていることも見る。当たらない
		// 理由が「ファイルで分けたから」でなく「単独の CR が無いから」だと、試験が
		// 何も確かめていないことになる。
		if strings.Contains(text, "\r") && !crOnlyLineBreaks(segs) && len(segs.List) > 1 {
			t.Errorf("%s（CR だけの改行）: CR だけの改行のファイルと見ていない", c.Name)
		}
	}
	// 分けたことで当たらなくなった2件は、ファイルで分ける前の見分け方（次のセグメントが
	// レコードに見えるか）では当たる形であることを確かめておく。ここが外れると、上の
	// 確かめは何も守っていない。
	for _, name := range []string{"soft-hyphen-comment", "ml-header"} {
		segs := SplitSegments([]byte(crOnly.Replace(fixtureText(t, name))))
		if !crOnlyLineBreaks(segs) || !nextSegmentNotRecordAfterCR(segs) {
			t.Errorf("%s（CR だけの改行）: 単独の CR の次にレコードに見えないセグメントが無い", name)
		}
	}
}

// fixtureText は入力の表から name の text を返す。
func fixtureText(t *testing.T, name string) string {
	t.Helper()
	for _, c := range loadFixtureCases(t).Cases {
		if c.Name == name {
			return c.Text
		}
	}
	t.Fatalf("入力の表に %s が無い", name)
	return ""
}

// nextSegmentNotRecordAfterCR は、単独の CR で終わるフィールドを持つセグメントの
// 次に、レコードに見えないセグメント（空行・コメント・空のレコード、またはキーの形でも
// 区切りの数がヘッダーと同じでもないレコード）があるかを返す。
func nextSegmentNotRecordAfterCR(segs Segments) bool {
	header, _ := segs.Header()
	for i, seg := range segs.List {
		if !seg.HasFields() || seg.Term != TermCR || i+1 >= len(segs.List) {
			continue
		}
		next := segs.List[i+1]
		if next.Kind != SegmentRecord || (!keyShaped(next.Fields[0]) && len(next.Offsets) != len(header.Offsets)) {
			return true
		}
	}
	return false
}

// TestCROnlyLineBreaks は、行の区切りがすべて単独の CR のファイルの見分け方を固定する。
func TestCROnlyLineBreaks(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       bool
	}{
		{"CR だけ", "key,translation\rk,a\r", true},
		{"最後の行に改行が無い", "key,translation\rk,a", true},
		{"LF が1つ混ざる", "key,translation\rk,a\n", false},
		{"CRLF が1つ混ざる", "key,translation\r\nk,a\r", false},
		// 引用の中の LF は値の一部で、行の区切りではない。
		{"引用の中の LF", "key,translation\rk,\"a\nb\"\r", true},
		{"改行が無い", "key,translation", false},
		{"空", "", false},
	} {
		if got := crOnlyLineBreaks(SplitSegments([]byte(tc.text))); got != tc.want {
			t.Errorf("%s: crOnlyLineBreaks = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestLoneCRValues は、値の中の単独の CR を見つけ、CRLF は見逃すことを固定する。
func TestLoneCRValues(t *testing.T) {
	f := ReadPowerShellMarked([]byte("a,b,c\n\"x\ry\",\"p\r\nq\",\"z\r\"\n1,2,3\n"))
	want := []ValueSpot{{ID: 2, Line: 2, EndLine: 5, Column: "a"}, {ID: 2, Line: 2, EndLine: 5, Column: "c"}}
	if got := LoneCRValues(f); !slices.Equal(got, want) {
		t.Errorf("単独の CR\n got %+v\nwant %+v", got, want)
	}

	for _, c := range loadFixtureCases(t).Cases {
		got := LoneCRValues(ReadPowerShellMarked([]byte(c.Text)))
		switch c.Name {
		case "lone-cr-in-quoted-translation":
			if want := []ValueSpot{{ID: 2, Line: 2, EndLine: 3, Column: "translation"}}; !slices.Equal(got, want) {
				t.Errorf("%s: got %+v, want %+v", c.Name, got, want)
			}
		default:
			if len(got) > 0 {
				t.Errorf("%s: 単独の CR を含む値があると言っている: %+v", c.Name, got)
			}
		}
	}
}

// TestLineBreakValues は、見出しに使う列の値の改行を見つけることを固定する。
func TestLineBreakValues(t *testing.T) {
	text := "section,phase,node,speaker\n" +
		"\"L01\nRyan\",intro,n1,\"a\nb\"\n" +
		"L02,\"p\rq\",n2,c\n" +
		"L03,x,n3,d\n"
	f := ReadPowerShellMarked([]byte(text))

	got := LineBreakValues(f, "section", "phase", "node", "condition")
	want := []ValueSpot{{ID: 2, Line: 2, EndLine: 4, Column: "section"}, {ID: 3, Line: 5, EndLine: 6, Column: "phase"}}
	if !slices.Equal(got, want) {
		t.Errorf("見出しの列\n got %+v\nwant %+v", got, want)
	}

	// 列を渡さなければ、すべての列を見る。
	got = LineBreakValues(f)
	want = []ValueSpot{
		{ID: 2, Line: 2, EndLine: 4, Column: "section"}, {ID: 2, Line: 2, EndLine: 4, Column: "speaker"},
		{ID: 3, Line: 5, EndLine: 6, Column: "phase"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("すべての列\n got %+v\nwant %+v", got, want)
	}
}

// TestCSharpDisagreements は、ゲームの読み方と値が割れるレコードを見つけることを固定する。
func TestCSharpDisagreements(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []Disagreement
	}{
		{
			// 値の中の LF・CRLF・'#' の行・空行は、どちらの読み方でも同じ値になる。
			name: "複数行の値は割れない",
			text: "key,source_en,translation\r\n" +
				"0123456789abcdef,\"para1\n\npara2\",\"一行目\n# 二行目\"\r\n" +
				"fedcba9876543210,\"a\r\nb\",\"c, \"\"d\"\"\"\r\n",
		},
		{
			// ゲームはフィールドの途中の '"' から引用を始め、次の '"' まで飲み込む
			// （移植仕様「CSVとキー生成 R6」）。飲み込まれた行はゲームの読み方に無い。
			name: "フィールドの途中の引用符",
			text: "key,translation\nk1,5\" screen\nk2,b\nk3,c\"d\nk4,e\n",
			want: []Disagreement{
				{ID: 2, Line: 2, EndLine: 2, Column: "translation"},
				{ID: 3, Line: 3, EndLine: 3},
				{ID: 4, Line: 4, EndLine: 4},
			},
		},
		{
			name: "引用符で囲まない値の前の空白",
			text: "key,translation\nk1,  x\nk2,y\n",
			want: []Disagreement{{ID: 2, Line: 2, EndLine: 2, Column: "translation"}},
		},
		{
			// key の前の空白は、主の読み手は削り（k1）、ゲームは残す（" k1"）。組にする鍵は
			// 前後の空白を除いて作るので、同じレコードとして組になり、key 列が割れる。
			// 除かずに組にすると、ゲームの読み方にそのレコードが無いことになり、どの列が
			// 割れたかを言えなくなる。
			name: "key の前の空白は除いて組にする",
			text: "key,translation\n k1,x\nk2,y\n",
			want: []Disagreement{{ID: 2, Line: 2, EndLine: 2, Column: "key"}},
		},
		{
			// ゲームは引用の外の CR を捨てて続きまで読む。切れた後半はゲームの読み方に無い。
			name: "引用の外の単独の CR",
			text: "key,translation\nk1,い\rち\nk2,b\n",
			want: []Disagreement{{ID: 2, Line: 2, EndLine: 2, Column: "translation"}, {ID: 3, Line: 3, EndLine: 3}},
		},
		{
			// 同じキーが2つあれば出現順に組にする。1つ目どうしで比べると2つ目が割れて見える。
			name: "同じキーの行は出現順に組にする",
			text: "key,translation\nk1,a\nk1,b\n",
		},
		{
			name: "key の無い2列の作業コピーは source_en で組にする",
			text: "source_en,translation\none,いち\n\"two\nlines\",に\n",
		},
		{
			// key も source_en も空の行は組にできないので見ない。
			name: "組にできない行は見ない",
			text: "key,translation\n,x\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CSharpDisagreements(ReadPowerShellMarked([]byte(tt.text))); !slices.Equal(got, tt.want) {
				t.Errorf("食い違い\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// TestRecordSignString は、理由の名前を固定する。呼び出し側が理由の文に使う。
func TestRecordSignString(t *testing.T) {
	want := map[RecordSign]string{
		SignKeyShaped: "key-shaped", SignSameColumns: "same-columns", SignTextAfterQuote: "text-after-quote",
		RecordSign(0): "unknown",
	}
	for sign, name := range want {
		if got := sign.String(); got != name {
			t.Errorf("%d.String() = %q, want %q", int(sign), got, name)
		}
	}
}
