package sourcerepo

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// MinLocales は、翻訳リポジトリの Translations 直下にあるロケールの数の下限です。
//
// ロケールの数は入力から数えますが、下限も見ます。0 と 0 が一致してしまうと、
// Translations をそもそも読めていないことに気づけません。13 は、このリポジトリの
// どのチェックアウトにもあった数です。
const MinLocales = 13

// Locales は root/Translations 直下のロケールの名前を、ディレクトリ名順に返します。
//
// 数え方は publish.DiscoverTargets と同じで、ディレクトリだけを見て、名前が '_' で
// 始まるものを飛ばします（ignore.txt はファイルなので入りません）。読めないときと、
// [MinLocales] より少ないときは Fatalf で落とします。
func Locales(t TB, root string) []string {
	t.Helper()

	dir := filepath.Join(root, "Translations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("%s を読めない: %v", dir, err)
		return nil
	}
	var out []string
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join(dir, entry.Name()))
		if err != nil || !info.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		out = append(out, entry.Name())
	}
	if len(out) < MinLocales {
		t.Fatalf("ロケールが %d しかない。Translations を読めていない: %s", len(out), dir)
		return nil
	}
	return out
}

// Line は、[ContentLines] が返す1行です。
type Line struct {
	// Number は物理行の番号（1始まり）です。
	Number int
	// Text は行の中身です。行末の CR と LF は含みません。
	Text string
}

// ContentLines は、CSV のファイル data の物理行のうち、空行でも '#' で始まる行でもない
// ものを返します。先頭の BOM は除きます。
//
// 実データの試験が、読み手（internal/csvfile）とは別の数え方で行数を出すためにあります。
// 値の中に改行を持つレコードや、空白だけの行は、読み手と数え方が割れます。実データに
// 行をまたぐレコードが無いことは、csvfile の TestRealDataWholeReaderAgrees が見ています。
func ContentLines(data []byte) []Line {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var out []Line
	for i, raw := range strings.Split(string(data), "\n") {
		text := strings.TrimSuffix(raw, "\r")
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		out = append(out, Line{Number: i + 1, Text: text})
	}
	return out
}

// PlainFields は、引用符を含まない行 text をカンマで分けます。引用符を含む行は、
// 分け方が読み手と同じになる保証が無いので、第2戻り値を false にします。
//
// 実データの試験が、読み手の読んだ値を、読み手とは別の読み方と突き合わせるために
// あります。
func PlainFields(text string) ([]string, bool) {
	if strings.Contains(text, `"`) {
		return nil, false
	}
	return strings.Split(text, ","), true
}
