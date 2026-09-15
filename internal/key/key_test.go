package key

import (
	"strings"
	"testing"
)

// TestForKnownVectors は、元リポジトリの文書に原文とキーの対応が書かれている
// 実データで For を裏付ける。ここが移植の正しさを確かめられる唯一の実測点なので、
// 出典を必ず添える。
func TestForKnownVectors(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
		doc    string
	}{
		{
			name:   "Options画面の項目名",
			source: "Language (Mod)",
			want:   "e3becbaee46cc0df",
			doc:    "CONTRIBUTING.ja.md:87 / CONTRIBUTING.md:73 に原文とキーが並記されている",
		},
		{
			name:   "複数キャラが言う短い台詞",
			source: "Wonderful!",
			want:   "84f325bca745e504",
			doc:    "CONTRIBUTING.md:96 の例に source_en 列つきで載っている",
		},
		{
			name:   "Ryanの1行目",
			source: "Hello?",
			want:   "0da72197e898ebe1",
			doc:    "docs/TRANSLATOR_EDITOR_RESEARCH.ja.md:67 のキーと一致する英語原文",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := For(tt.source)
			if got != tt.want {
				t.Errorf("For(%q) = %q, want %q（出典: %s）", tt.source, got, tt.want, tt.doc)
			}
		})
	}
}

// TestFor は、加工を一切しない（移植仕様 R15）ことを確かめる。
// 前後の空白・改行・書式タグ・多バイト文字は、いずれもそのままハッシュ対象に入る。
func TestFor(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			// 元実装は null だけを空文字で弾き、空文字は素通りする。
			// SHA-256("") の先頭8バイトはよく知られた e3b0c442 98fc1c14。
			name:   "空文字は本物に見えるキーになる",
			source: "",
			want:   "e3b0c44298fc1c14",
		},
		{
			name:   "UTF-8の多バイト文字",
			source: "もしもし？",
			want:   "7ad90c563c74760f",
		},
		{
			name:   "サロゲートペアになる絵文字",
			source: "🐊",
			want:   "c4c592cbefa0f47e",
		},
		{
			name:   "ASCIIと多バイトの混在",
			source: "日本語 (ja)",
			want:   "dbf3122ec59dc2a2",
		},
		{
			// トリムしないので " Wonderful! " は "Wonderful!" と別キーになる。
			name:   "前後に空白がある文字列はトリムされない",
			source: " Wonderful! ",
			want:   "42b3df7f61541bd1",
		},
		{
			name:   "末尾だけ空白がある文字列",
			source: "Wonderful! ",
			want:   "135e983c25c37f57",
		},
		{
			name:   "大文字小文字を区別する",
			source: "wonderful!",
			want:   "0aaa2a0cbaac3abd",
		},
		{
			name:   "LFを含む文字列",
			source: "Hello\nWorld",
			want:   "35c6b9f66dceb6cf",
		},
		{
			// CRLF は CR も含めてハッシュされるので LF 版と別キーになる。
			name:   "CRLFを含む文字列",
			source: "Hello\r\nWorld",
			want:   "1aeab64e644b8b84",
		},
		{
			// 元実装のコメントに "tags included" と明記がある。
			name:   "TMPの書式タグを含む文字列",
			source: "<size=70%>Language (Mod)</size>",
			want:   "a9ea3b6ee207deb4",
		},
		{
			name:   "TMPの太字タグ",
			source: "<b>Hi</b>",
			want:   "10a4f3c9c746b7dd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := For(tt.source)
			if got != tt.want {
				t.Errorf("For(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}

// TestForShape は、どんな入力でも「16桁の小文字16進」という形が崩れないことを確かめる。
func TestForShape(t *testing.T) {
	sources := []string{
		"", " ", "Wonderful!", "もしもし？", "🐊", "<b>Hi</b>", "Hello\r\nWorld",
		strings.Repeat("あ", 1000),
		"\x00\x01\x02",
		"\xff\xfe", // 不正なUTF-8バイト列。契約外だが落ちずに16桁を返すことは保証する
	}
	for _, s := range sources {
		got := For(s)
		if len(got) != Length {
			t.Errorf("For(%q) の長さが %d、want %d", s, len(got), Length)
		}
		if !LooksLike(got) {
			t.Errorf("For(%q) = %q は LooksLike を満たさない", s, got)
		}
	}
}

// TestForIsStable は、同じ入力に対して常に同じキーが返ることを確かめる。
// 元実装は [ThreadStatic] な SHA256 インスタンスを使い回すが、出力には影響しない。
func TestForIsStable(t *testing.T) {
	const source = "Language (Mod)"
	first := For(source)
	for i := 0; i < 100; i++ {
		if got := For(source); got != first {
			t.Fatalf("%d 回目の For(%q) = %q, 1回目は %q", i+1, source, got, first)
		}
	}
}

// TestHashOfEmpty は、公開している定数が実際の For("") と一致することを確かめる。
// この値が「本物に見える16桁キー」である点が、欠損列を空文字で表す移植の罠になる。
func TestHashOfEmpty(t *testing.T) {
	if got := For(""); got != HashOfEmpty {
		t.Errorf("For(\"\") = %q, HashOfEmpty = %q", got, HashOfEmpty)
	}
	if !LooksLike(HashOfEmpty) {
		t.Error("HashOfEmpty が LooksLike を満たさない。罠の前提が崩れている")
	}
}

func TestLooksLike(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"実データのキー", "e3becbaee46cc0df", true},
		{"数字だけ16桁", "0123456789012345", true},
		{"a-fだけ16桁", "abcdefabcdefabcd", true},
		{"空文字", "", false},
		{"15桁", "e3becbaee46cc0d", false},
		{"17桁", "e3becbaee46cc0df0", false},
		{"大文字を含む", "E3becbaee46cc0df", false},
		{"全部大文字", "ABCDEF0123456789", false},
		{"16進の範囲外の英字 g", "e3becbaee46cc0dg", false},
		{"16進の範囲外の英字 z", "zzzzzzzzzzzzzzzz", false},
		{"記号を含む", "e3becbaee46cc0d-", false},
		{"空白を含む", "e3becbaee46cc0d ", false},
		{"前後に空白がある16桁の中身", " e3becbaee46cc0d", false},
		{"0x接頭辞", "0xe3becbaee46cc0", false},
		{"台詞ID", "line:6046bedf", false},
		{
			// 多バイト文字はバイト長16になっても16進判定で落ちる。
			name:  "バイト長が16になる多バイト文字列",
			value: "ああああああああああああああああ"[:16],
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LooksLike(tt.value); got != tt.want {
				t.Errorf("LooksLike(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestLooksLikeLineID(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"実データの台詞ID", "line:6046bedf", true},
		{"実データの台詞ID その2", "line:ab423ac7", true},
		{"実体が1文字", "line:a", true},
		{"実体に大文字", "line:ABC", true},
		{"実体に許可された記号", "line:a_b-c.d", true},
		{"実体が記号だけ", "line:_-.", true},
		{
			// 全長64（実体59文字）は「64以下」なので通る。
			name:  "全長64（実体59文字）",
			value: LineIDPrefix + strings.Repeat("a", 59),
			want:  true,
		},
		{
			// 全長65（実体60文字）は弾かれる。-cmatch の {1,59} と一致。
			name:  "全長65（実体60文字）",
			value: LineIDPrefix + strings.Repeat("a", 60),
			want:  false,
		},
		{
			name:  "実体が大幅に長い",
			value: LineIDPrefix + strings.Repeat("a", 200),
			want:  false,
		},
		{"空文字", "", false},
		{"接頭辞だけ", "line:", false},
		{"接頭辞の途中まで", "line", false},
		{
			// Ordinal（大小区別あり）比較なので大文字の接頭辞は通らない。
			name:  "Line: で始まる",
			value: "Line:6046bedf",
			want:  false,
		},
		{"LINE: で始まる", "LINE:6046bedf", false},
		{"lIne: で始まる", "lIne:6046bedf", false},
		{"接頭辞がない", "6046bedf", false},
		{"接頭辞の前に空白", " line:6046bedf", false},
		{"末尾に空白", "line:6046bedf ", false},
		{"実体にコロン", "line:6046:bedf", false},
		{"実体にスラッシュ", "line:6046/bedf", false},
		{"実体に空白", "line:6046 bedf", false},
		{"実体に改行", "line:6046\nbedf", false},
		{"実体に多バイト文字", "line:もしもし", false},
		{"ハッシュキー", "e3becbaee46cc0df", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LooksLikeLineID(tt.value); got != tt.want {
				t.Errorf("LooksLikeLineID(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// TestLooksLikeAndLooksLikeLineIDAreDisjoint は、2つの判定が同時に true にならない
// ことを確かめる。呼び出し側は LooksLikeLineID → LooksLike の順に判定するので、
// 重なりがあると解釈が分岐する。
func TestLooksLikeAndLooksLikeLineIDAreDisjoint(t *testing.T) {
	values := []string{
		"e3becbaee46cc0df",
		"line:6046bedf",
		"line:abcdef0123",
		"0123456789abcdef",
		"",
		"line:",
	}
	for _, v := range values {
		if LooksLike(v) && LooksLikeLineID(v) {
			t.Errorf("%q が両方の判定で true になっている", v)
		}
	}
}
