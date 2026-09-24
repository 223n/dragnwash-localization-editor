package csvfile

import (
	"slices"
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
// 確かめたうえで通す指定（PR2）で書く。
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
