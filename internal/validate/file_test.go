package validate

import (
	"slices"
	"strings"
	"testing"
)

// validHeader は公開ファイルの現行ヘッダー。テストの土台にする。
const validHeader = "key,section,node,order,speaker,translation\n"

// 有効なキーの例。値そのものに意味は無いが、16桁の小文字16進であることが要る。
const (
	keyA = "0123456789abcdef"
	keyB = "0123456789abcde0"
)

// problemStrings は報告を1行ずつの文字列にする。期待値と比べやすくするため。
func problemStrings(problems []Problem) []string {
	out := make([]string, 0, len(problems))
	for _, p := range problems {
		out = append(out, p.String())
	}
	return out
}

// checkFileTest は1件ぶんのテスト。want は Problem.String() の並び。
type checkFileTest struct {
	name string
	// data はファイルの中身。行頭・行末を見やすくするため、テスト側で
	// バッククォート文字列を使う。
	data string
	want []string
}

func runCheckFileTests(t *testing.T, tests []checkFileTest) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := problemStrings(CheckFile("f.csv", []byte(tt.data)))
			if !slices.Equal(got, tt.want) {
				t.Errorf("問題の並びが違う\n got = %#v\nwant = %#v", got, tt.want)
			}
		})
	}
}

// TestCheckFileHeader はヘッダーの受理と拒否を見る。
// 「英語原文の列が混ざっていないこと」の検査はここに乗っている。
// source_en 列を持つファイルは専用の検査ではなく、この完全一致に落ちて弾かれる。
func TestCheckFileHeader(t *testing.T) {
	const suffix = "; the published file must be 'key,section,node,order,speaker,translation'" +
		" (run tools/hash-strings.ps1 before committing)"

	runCheckFileTests(t, []checkFileTest{
		{
			name: "6列の現行ヘッダー",
			data: validHeader + keyA + ",UI,,,UI,やあ\n",
		},
		{
			name: "3列の旧ヘッダー",
			data: "key,speaker,translation\n" + keyA + ",Ryan,やあ\n",
		},
		{
			name: "2列の旧ヘッダー",
			data: "key,translation\n" + keyA + ",やあ\n",
		},
		{
			name: "source_en 列がある（英語原文の混入）",
			data: "key,source_en,translation\n" + keyA + ",Hello,やあ\n",
			want: []string{`f.csv: header is ['key', 'source_en', 'translation']` + suffix},
		},
		{
			name: "列の順が違う",
			data: "key,translation,speaker\n",
			want: []string{`f.csv: header is ['key', 'translation', 'speaker']` + suffix},
		},
		{
			name: "大文字になっている",
			data: "Key,Translation\n",
			want: []string{`f.csv: header is ['Key', 'Translation']` + suffix},
		},
		{
			name: "列名の前後に空白がある",
			data: "key, translation\n",
			want: []string{`f.csv: header is ['key', ' translation']` + suffix},
		},
		{
			name: "列が足りない",
			data: "key\n" + keyA + "\n",
			want: []string{`f.csv: header is ['key']` + suffix},
		},
		{
			// ヘッダーが違えば列の意味も当てにならないので、行は1つも見ない。
			name: "ヘッダーが違うと行の問題は報告されない",
			data: "key,source_en,translation\nでたらめ\nでたらめ\n",
			want: []string{`f.csv: header is ['key', 'source_en', 'translation']` + suffix},
		},
		{
			name: "先頭のBOMは剥がされる",
			data: "\xef\xbb\xbf" + validHeader + keyA + ",UI,,,UI,やあ\n",
		},
		{
			// BOM を剥がすのは先頭1個だけ。2個目は普通の文字として残る。
			name: "BOMが2個あると列名に残って弾かれる",
			data: "\xef\xbb\xbf\xef\xbb\xbfkey,translation\n",
			want: []string{`f.csv: header is ['\ufeffkey', 'translation']` + suffix},
		},
		{
			name: "ヘッダーの前にコメント行があってもよい",
			data: "# メモ\n" + validHeader + keyA + ",UI,,,UI,やあ\n",
		},
	})
}

// TestCheckFileEmpty は中身が無いと見なされる場合を見る。行番号は付かない。
func TestCheckFileEmpty(t *testing.T) {
	runCheckFileTests(t, []checkFileTest{
		{
			name: "サイズ0",
			data: "",
			want: []string{"f.csv: empty file"},
		},
		{
			name: "全部コメント行",
			data: "# ひとつ\n# ふたつ\n",
			want: []string{"f.csv: empty file"},
		},
		{
			name: "全部完全な空行",
			data: "\n\r\n\n",
			want: []string{"f.csv: empty file"},
		},
		{
			// 上流 f816618 から、空白だけの行は落とさずに1フィールドのレコードとして
			// 読む。最初のレコードなのでヘッダーとして比べられて外れる。
			// 003ed1e までは物理行ごと落として "empty file" だった。
			name: "空白だけの行はヘッダーになる",
			data: "\n   \n\t\n\r\n",
			want: []string{`f.csv: header is ['   ']` + headerSuffix},
		},
		{
			// Python の str.isspace() は U+001C〜U+001F を空白とみなすが、
			// csv.reader にとってはただの文字なので、これも1フィールドのレコード。
			name: "Python だけが空白とみなす文字だけの行もヘッダーになる",
			data: "\x1c\n\x1f\n",
			want: []string{`f.csv: header is ['\x1c']` + headerSuffix},
		},
	})
}

// headerSuffix はヘッダー不正の文面の後半。
const headerSuffix = "; the published file must be 'key,section,node,order,speaker,translation'" +
	" (run tools/hash-strings.ps1 before committing)"

// TestCheckFileKey はキーの形の検査を見る。メッセージにキーの値は出さない。
func TestCheckFileKey(t *testing.T) {
	const bad = "f.csv:2: key is not 16 lowercase hex digits or a line ID"

	runCheckFileTests(t, []checkFileTest{
		{name: "16桁の小文字16進", data: "key,translation\n" + keyA + ",やあ\n"},
		{name: "台詞ID", data: "key,translation\nline:6046bedf,やあ\n"},
		{
			name: "台詞IDの本体が59文字",
			data: "key,translation\nline:" + strings.Repeat("a", 59) + ",やあ\n",
		},
		{
			name: "台詞IDの本体が60文字",
			data: "key,translation\nline:" + strings.Repeat("a", 60) + ",やあ\n",
			want: []string{bad},
		},
		{
			name: "台詞IDの本体が空",
			data: "key,translation\nline:,やあ\n",
			want: []string{bad},
		},
		{
			name: "大文字16進",
			data: "key,translation\n0123456789ABCDEF,やあ\n",
			want: []string{bad},
		},
		{
			name: "15桁",
			data: "key,translation\n0123456789abcde,やあ\n",
			want: []string{bad},
		},
		{
			name: "17桁",
			data: "key,translation\n0123456789abcdef0,やあ\n",
			want: []string{bad},
		},
		{
			name: "空のキー",
			data: "key,translation\n,やあ\n",
			want: []string{bad},
		},
		{
			name: "前後に空白が付いたキーはトリムされない",
			data: "key,translation\n " + keyA + " ,やあ\n",
			want: []string{bad},
		},
		{
			name: "台詞IDの接頭辞が大文字",
			data: "key,translation\nLINE:6046bedf,やあ\n",
			want: []string{bad},
		},
		{
			// Python の '$' は末尾の改行1個を許すので元実装はこれを通す。
			// ここでは通さない（移植仕様「未決の点」に対する判断）。
			name: "キーの末尾に改行が付く",
			data: "key,translation\n\"" + keyA + "\n\",やあ\n",
			want: []string{"f.csv:2: key is not 16 lowercase hex digits or a line ID"},
		},
	})
}

// TestCheckFileDuplicate は重複キーの検出を見る。参照先は常に最初の行。
func TestCheckFileDuplicate(t *testing.T) {
	runCheckFileTests(t, []checkFileTest{
		{
			name: "2回目で報告する",
			data: "key,translation\n" + keyA + ",ひとつ\n" + keyA + ",ふたつ\n",
			want: []string{"f.csv:3: duplicate key (see line 2)"},
		},
		{
			// setdefault なので3回目も参照先は1回目。2回目ではない。
			name: "3回目も1回目を指す",
			data: "key,translation\n" + keyA + ",ひとつ\n" + keyA + ",ふたつ\n" + keyA + ",みっつ\n",
			want: []string{
				"f.csv:3: duplicate key (see line 2)",
				"f.csv:4: duplicate key (see line 2)",
			},
		},
		{
			name: "コメント行と空行をまたいでも元の行番号を指す",
			data: "key,translation\n" + keyA + ",ひとつ\n# 見出し\n\n" + keyA + ",ふたつ\n",
			want: []string{"f.csv:5: duplicate key (see line 2)"},
		},
		{
			// 台詞IDは大文字を許すので、大小が違えば別のキー。正規化はしない。
			name: "台詞IDは大文字小文字を区別する",
			data: "key,translation\nline:Abc,ひとつ\nline:abc,ふたつ\n",
		},
		{
			// キーの形が不正でも seen には入るので、不正なキーの重複も見つかる。
			name: "不正なキーでも重複は検出する",
			data: "key,translation\nでたらめ,ひとつ\nでたらめ,ふたつ\n",
			want: []string{
				"f.csv:2: key is not 16 lowercase hex digits or a line ID",
				"f.csv:3: key is not 16 lowercase hex digits or a line ID",
				"f.csv:3: duplicate key (see line 2)",
			},
		},
	})
}

// TestCheckFileTranslation は空の訳の検出を見る。
func TestCheckFileTranslation(t *testing.T) {
	runCheckFileTests(t, []checkFileTest{
		{
			name: "空文字",
			data: "key,translation\n" + keyA + ",\n",
			want: []string{"f.csv:2: empty translation"},
		},
		{
			name: "スペースだけ",
			data: "key,translation\n" + keyA + ",\"   \"\n",
			want: []string{"f.csv:2: empty translation"},
		},
		{
			name: "全角スペースだけ",
			data: "key,translation\n" + keyA + ",　\n",
			want: []string{"f.csv:2: empty translation"},
		},
		{
			// Go の strings.TrimSpace では空にならない文字。
			// Python の str.strip() は落とすので、元実装はこれを空と判定する。
			name: "U+001C だけ",
			data: "key,translation\n" + keyA + ",\x1c\n",
			want: []string{"f.csv:2: empty translation"},
		},
		{
			name: "6列ヘッダーでは最終列を見る",
			data: validHeader + keyA + ",UI,,,UI,\n",
			want: []string{"f.csv:2: empty translation"},
		},
	})
}

// TestCheckFileFieldCount は列数不一致を見る。合わない行は以降の検査を飛ばす。
func TestCheckFileFieldCount(t *testing.T) {
	runCheckFileTests(t, []checkFileTest{
		{
			name: "足りない",
			data: validHeader + keyA + ",UI,,,UI\n",
			want: []string{"f.csv:2: expected 6 fields, got 5"},
		},
		{
			name: "多い",
			data: validHeader + keyA + ",UI,,,UI,やあ,よけい\n",
			want: []string{"f.csv:2: expected 6 fields, got 7"},
		},
		{
			// 列数が合わない行は key も訳も section も見られない。
			name: "合わない行の他の問題は報告されない",
			data: "key,translation\nでたらめ\n",
			want: []string{"f.csv:2: expected 2 fields, got 1"},
		},
		{
			// seen に登録されないので、後続の同じキーが重複として出なくなる。
			// 元実装の癖なのでそのまま写している。
			name: "合わない行のキーは重複の基準にならない",
			data: "key,translation\n" + keyA + "\n" + keyA + ",ひとつ\n" + keyA + ",ふたつ\n",
			want: []string{
				"f.csv:2: expected 2 fields, got 1",
				"f.csv:4: duplicate key (see line 3)",
			},
		},
		{
			// 先頭に空白があると '#' でもコメントと見なされない。
			name: "空白始まりのコメントもどきはCSV行として読まれる",
			data: "key,translation\n # メモ\n",
			want: []string{"f.csv:2: expected 2 fields, got 1"},
		},
		{
			// 上流 f816618 から、空白だけの行も1フィールドのレコードとして読む。
			name: "空白だけの行は列数不一致",
			data: "key,translation\n" + keyA + ",やあ\n   \n\t\n" + keyB + ",やあ\n",
			want: []string{
				"f.csv:3: expected 2 fields, got 1",
				"f.csv:4: expected 2 fields, got 1",
			},
		},
		{
			// 完全な空行は0フィールドのレコードで、番号を消費するだけ。
			name: "完全な空行は問題にならない",
			data: "key,translation\n\n" + keyA + ",やあ\r\n\r\n",
		},
	})
}

// TestCheckFileIdentifier は section / node の識別子らしさを見る。
func TestCheckFileIdentifier(t *testing.T) {
	// 6列ヘッダーの行を組み立てる。
	row := func(section, node string) string {
		return validHeader + keyA + "," + section + "," + node + ",1,Ryan,やあ\n"
	}

	runCheckFileTests(t, []checkFileTest{
		{name: "英数字と下線", data: row("UI", "Ryan_1_intro")},
		{name: "どちらも空", data: row("", "")},
		{name: "レベルのセクション名", data: row("L01 Ryan", "Ryan_1_intro")},
		{name: "2桁の上限", data: row("L15 Alexander", "Start")},
		{
			name: "スペースが2個",
			data: row("L01  Ryan", "Start"),
			want: []string{"f.csv:2: section does not look like an identifier"},
		},
		{
			name: "語が2つ",
			data: row("L01 Ryan Extra", "Start"),
			want: []string{"f.csv:2: section does not look like an identifier"},
		},
		{
			name: "数字が1桁",
			data: row("L1 Ryan", "Start"),
			want: []string{"f.csv:2: section does not look like an identifier"},
		},
		{
			name: "ハイフンを含む",
			data: row("pt-BR", "Start"),
			want: []string{"f.csv:2: section does not look like an identifier"},
		},
		{
			name: "node が文になっている",
			data: row("UI", "Hello there"),
			want: []string{"f.csv:2: node does not look like an identifier"},
		},
		{
			// 報告の順は section が先、node が後。
			name: "両方おかしい",
			data: row("だめ", "だめ"),
			want: []string{
				"f.csv:2: section does not look like an identifier",
				"f.csv:2: node does not look like an identifier",
			},
		},
		{
			// 3列・2列のヘッダーには section も node も無いので発動しない。
			name: "3列ヘッダーでは検査しない",
			data: "key,speaker,translation\n" + keyA + ",Hello there,やあ\n",
		},
	})
}

// TestCheckFileProblemOrder は同じ行に複数の問題があるときの並びを見る。
// 元実装の append 順は フィールド数 → キー → 重複 → 空の訳 → section → node。
func TestCheckFileProblemOrder(t *testing.T) {
	data := validHeader +
		"だめ,だめ,だめ,1,Ryan,\n" +
		"だめ,だめ,だめ,1,Ryan,\n"
	want := []string{
		"f.csv:2: key is not 16 lowercase hex digits or a line ID",
		"f.csv:2: empty translation",
		"f.csv:2: section does not look like an identifier",
		"f.csv:2: node does not look like an identifier",
		"f.csv:3: key is not 16 lowercase hex digits or a line ID",
		"f.csv:3: duplicate key (see line 2)",
		"f.csv:3: empty translation",
		"f.csv:3: section does not look like an identifier",
		"f.csv:3: node does not look like an identifier",
	}
	if got := problemStrings(CheckFile("f.csv", []byte(data))); !slices.Equal(got, want) {
		t.Errorf("問題の並びが違う\n got = %#v\nwant = %#v", got, want)
	}
}

// TestCheckFileLineNumbers は報告の行番号が元ファイルの物理行番号であることを見る。
// コメント行と空行を除いたあとの番号ではない。
func TestCheckFileLineNumbers(t *testing.T) {
	runCheckFileTests(t, []checkFileTest{
		{
			// 1:ヘッダー 2:コメント 3:空行 4:問題のある行
			name: "コメント行と空行は番号を消費する",
			data: "key,translation\n# 見出し\n\nでたらめ,やあ\n",
			want: []string{"f.csv:4: key is not 16 lowercase hex digits or a line ID"},
		},
		{
			name: "ヘッダーの前にコメントがある",
			data: "# ひとつ\n# ふたつ\nkey,translation\nでたらめ,やあ\n",
			want: []string{"f.csv:4: key is not 16 lowercase hex digits or a line ID"},
		},
		{
			name: "CRLFでも番号は変わらない",
			data: "key,translation\r\n# 見出し\r\n\r\nでたらめ,やあ\r\n",
			want: []string{"f.csv:4: key is not 16 lowercase hex digits or a line ID"},
		},
		{
			// Python の io.open(newline="") は単独 CR も行の終わりとみなす。
			name: "CRだけの改行でも番号は進む",
			data: "key,translation\r# 見出し\rでたらめ,やあ\r",
			want: []string{"f.csv:3: key is not 16 lowercase hex digits or a line ID"},
		},
		{
			name: "末尾に改行が無くても最後の行を読む",
			data: "key,translation\nでたらめ,やあ",
			want: []string{"f.csv:2: key is not 16 lowercase hex digits or a line ID"},
		},
	})
}

// TestCheckFileMultilineQuoted は引用フィールドが複数行にまたがるときに
// 行番号がずれないことを見る。報告は「そのレコードの先頭の物理行」を指す。
//
// ここがずれると、以降のすべての報告が別の行を指す。移植でいちばん壊れやすい
// ところなので、またぎ方を変えた場合を並べて確かめる。
func TestCheckFileMultilineQuoted(t *testing.T) {
	runCheckFileTests(t, []checkFileTest{
		{
			// 1:ヘッダー 2:コメント 3:空行 4-5:2行にまたがる行 6:次の行
			// 移植仕様「形式検証 R15」の実測例と同じ並び。
			name: "2行にまたがる",
			data: "key,translation\n# 見出し\n\nあいう,\"ふた\nつの行\"\nえおか,やあ\n",
			want: []string{
				"f.csv:4: key is not 16 lowercase hex digits or a line ID",
				"f.csv:6: key is not 16 lowercase hex digits or a line ID",
			},
		},
		{
			name: "4行にまたがる",
			data: "key,translation\nあいう,\"い\nろ\nは\nに\"\nえおか,やあ\n",
			want: []string{
				"f.csv:2: key is not 16 lowercase hex digits or a line ID",
				"f.csv:6: key is not 16 lowercase hex digits or a line ID",
			},
		},
		{
			// またいだ行を含んでも、そのあとの重複の参照先は正しい先頭行を指す。
			name: "またいだ行のキーが重複の基準になる",
			data: "key,translation\n" + keyA + ",\"ふた\nつの行\"\n" + keyA + ",やあ\n",
			want: []string{"f.csv:4: duplicate key (see line 2)"},
		},
		{
			// ヘッダー自体がまたがっていても、次の行の番号は正しく追える。
			name: "ヘッダーがまたがる",
			data: "\"key\n\",translation\nでたらめ,やあ\n",
			want: []string{
				`f.csv: header is ['key\n', 'translation']; the published file must be ` +
					`'key,section,node,order,speaker,translation' (run tools/hash-strings.ps1 before committing)`,
			},
		},
		{
			name: "CRLFでまたがっても番号は合う",
			data: "key,translation\r\nあいう,\"ふた\r\nつの行\"\r\nえおか,やあ\r\n",
			want: []string{
				"f.csv:2: key is not 16 lowercase hex digits or a line ID",
				"f.csv:4: key is not 16 lowercase hex digits or a line ID",
			},
		},
		{
			// 上流 f816618 から、引用フィールドの途中にある空行は値の一部として残る。
			// 003ed1e までは物理行ごと落として、訳文から改行が1つ消えていた。
			// どちらでも行番号の対応は崩れない。
			name: "またいだ途中の空行は値に残る",
			data: "key,translation\nあいう,\"ふた\n\nつの行\"\nえおか,やあ\n",
			want: []string{
				"f.csv:2: key is not 16 lowercase hex digits or a line ID",
				"f.csv:5: key is not 16 lowercase hex digits or a line ID",
			},
		},
		{
			// 上流 c8fda90 から、引用フィールドの途中にある '#' 始まりの行は
			// コメントではなく値の一部になる。003ed1e までは閉じ引用符ごと落ちて
			// 残りの行を飲み込み、4行目の空の訳を見逃していた。
			name: "またいだ途中の'#'始まりの行は値に残り後ろも検査される",
			data: "key,translation\n" + keyA + ",\"ふた\n#つの行\"\n" + keyB + ",\n",
			want: []string{"f.csv:4: empty translation"},
		},
	})
}

// TestCheckFileBareQuote は引用符で囲まないフィールド中の裸の '"' を見る。
//
// Go の encoding/csv は LazyQuotes を立てないとここでパースを打ち切り、
// 以降の行がまるごと無検査になる。翻訳者が he said "hi" と書くのは十分ありうる
// 入力なので、打ち切られていないことを確かめる（移植仕様「敵対検証」[high]）。
func TestCheckFileBareQuote(t *testing.T) {
	runCheckFileTests(t, []checkFileTest{
		{
			name: "裸の二重引用符は素通しする",
			data: "key,translation\n" + keyA + `,he said "hi"` + "\n",
		},
		{
			name: "裸の二重引用符の後ろの行も検査される",
			data: "key,translation\n" + keyA + `,he said "hi"` + "\n" + keyA + ",ふたつ\n" +
				"でたらめ,\n",
			want: []string{
				"f.csv:3: duplicate key (see line 2)",
				"f.csv:4: key is not 16 lowercase hex digits or a line ID",
				"f.csv:4: empty translation",
			},
		},
	})
}

// TestLooksLikeIdentifier は識別子の判定を単体で見る。
func TestLooksLikeIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"空文字", "", true},
		{"UI", "UI", true},
		{"下線と数字", "Ryan_1_intro", true},
		{"数字だけ", "0123", true},
		{"レベル", "L01 Ryan", true},
		{"レベルの2桁目が9", "L99 Zoe", true},
		{"ハイフン", "pt-BR", false},
		{"スペースだけ", " ", false},
		{"スペース2個", "L01  Ryan", false},
		{"語が2つ", "L01 Ryan Extra", false},
		{"数字が1桁", "L1 Ryan", false},
		{"数字が3桁", "L001 Ryan", false},
		{"名前が空", "L01 ", false},
		{"名前に数字", "L01 Ryan1", false},
		{"小文字のl", "l01 Ryan", false},
		{"日本語", "見出し", false},
		{"末尾に改行", "UI\n", false},
		{"レベルの末尾に改行", "L01 Ryan\n", false},
		// Python の `\d` は Unicode の十進数字すべてに当たる。元実装の挙動に合わせる。
		{"アラビア数字", "L٠١ Ryan", true},
		{"全角数字", "L０１ Ryan", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeIdentifier(tt.value); got != tt.want {
				t.Errorf("looksLikeIdentifier(%q) = %t, want %t", tt.value, got, tt.want)
			}
		})
	}
}

// TestAcceptedHeaders は受理されるヘッダーの並びと、複製が返ることを見る。
func TestAcceptedHeaders(t *testing.T) {
	got := AcceptedHeaders()
	want := [][]string{
		{"key", "section", "node", "order", "speaker", "translation"},
		{"key", "speaker", "translation"},
		{"key", "translation"},
	}
	if len(got) != len(want) {
		t.Fatalf("種類 = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Errorf("%d番目 = %q, want %q", i, got[i], want[i])
		}
	}

	// 書き換えても元に響かないこと。
	got[0][0] = "壊した"
	if AcceptedHeaders()[0][0] != "key" {
		t.Error("返したスライスへの書き換えが元に響いている")
	}
}
