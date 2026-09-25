package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// publishStopReasons は、publish が確かめで止まって終了コード 1 を返す理由の並び。
//
// 同じ並びを、publish の使い方（publishUsage）、全体の使い方（dwloc help の
// usageText）、main.go のパッケージの注記、README（ja・en）の終了コードの一覧の
// 5か所に書いている。publish の止まり方を足したときに、どれか1か所だけが古いまま
// 残ると、そこだけを読んで CI を組んだ人が終了コード 1 の意味を取り違える
// （改善の調査の cli-9 と tests-docs-7）。[TestExitCodeOneReasonsAgree] が、
// 5か所がこの並びとそろっていることを見る。
var publishStopReasons = []string{
	"書くと訳が失われる",
	"読み違える形のファイルがある",
	"ゲームに入っている翻訳が古い",
	"組み立てたあとに入力か書き出し先が変わった",
}

// publishStopReasonsEn は [publishStopReasons] の英語版の README での書き方。
// 並びは同じにする。
var publishStopReasonsEn = []string{
	"writing would lose translations",
	"a file has a shape it would misread",
	"the translation in the game is older",
	"the input or the output changed after it was assembled",
}

// publish --check が、書き換えの要るロケールを見つけて終了コード 1 を返すことの、
// 5か所での書き方。止まる理由（[publishStopReasons]）とは別の1件として並べる。
const (
	checkInPublishUsage = "--check では、書き換えが要るロケールがあるときも 1 です"
	checkInHelp         = "publish --check で書き換えが要るロケールがある"
	checkInComment      = "publish --check が書き換えの要るロケールを見つけたとき"
	checkInReadme       = "`publish --check`が、書き換えが要るロケールを見つけたとき"
	checkInReadmeEn     = "When `publish --check` finds a locale that needs rewriting"
)

// readRepoFile は、このリポジトリのファイルを、ルートからの相対パスで読む。
// 試験は cmd/dwloc をカレントにして走る。
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("%s を読めない: %v", rel, err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

// exitCodeEntry は使い方の「終了コード:」の節から、code の項目を1続きの文にして返す。
//
// 項目は「  1   」で始まり、次の項目か節の終わりまで字下げした行が続く。項目の番号と
// 行の頭の空白を除いてつなぐ。日本語の文なので、つなぎ目に空白は足さない。
func exitCodeEntry(t *testing.T, usage string, code int) string {
	t.Helper()
	_, section, ok := strings.Cut(usage, "\n終了コード:\n")
	if !ok {
		t.Fatalf("使い方に「終了コード:」の節が無い:\n%s", usage)
	}
	head := "  " + strconv.Itoa(code) + "   "
	var b strings.Builder
	in := false
	for line := range strings.SplitSeq(section, "\n") {
		switch {
		case strings.HasPrefix(line, head):
			in = true
			b.WriteString(strings.TrimSpace(strings.TrimPrefix(line, head)))
		case in && strings.HasPrefix(line, "      "):
			b.WriteString(strings.TrimSpace(line))
		case in:
			return b.String()
		}
	}
	if !in {
		t.Fatalf("終了コード %d の項目が無い:\n%s", code, section)
	}
	return b.String()
}

// bracketed は s の中の「…」を、出てくる順に返す。
func bracketed(s string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`「([^」]*)」`).FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// packageComment は main.go のパッケージの注記を、// を外して1続きの文にして返す。
func packageComment(t *testing.T) string {
	t.Helper()
	src := readRepoFile(t, "cmd/dwloc/main.go")
	head, _, ok := strings.Cut(src, "\npackage main\n")
	if !ok {
		t.Fatal("main.go に package main の行が無い")
	}
	var b strings.Builder
	for line := range strings.SplitSeq(head, "\n") {
		b.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "//")))
	}
	return b.String()
}

// readmeExitList は README の終了コードの一覧を返す。lead は一覧の前の1行
// （件数を書く行）の頭で、count はその行が言う件数、items は一覧の各行（「- 」を
// 除いたもの）である。
func readmeExitList(t *testing.T, readme, lead string) (count string, items []string) {
	t.Helper()
	i := strings.Index(readme, lead)
	if i < 0 {
		t.Fatalf("README に %q の行が無い", lead)
	}
	rest := readme[i+len(lead):]
	line, rest, _ := strings.Cut(rest, "\n")
	count = line
	rest = strings.TrimLeft(rest, "\n")
	for item := range strings.SplitSeq(rest, "\n") {
		if !strings.HasPrefix(item, "- ") {
			break
		}
		items = append(items, strings.TrimPrefix(item, "- "))
	}
	if len(items) == 0 {
		t.Fatalf("README の %q の後ろに一覧が無い", lead)
	}
	return count, items
}

// numberWords は README.en の件数の書き方（英語の数詞）。
var numberWords = map[string]int{
	"five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
}

// TestExitCodeOneReasonsAgree は、publish が止まって終了コード 1 を返す理由の並びが、
// publish の使い方、dwloc help、main.go の注記、README（ja・en）でそろっていることを
// 見る。README とパッケージの注記が言う件数（「次の7つ」「README の終了コードの7つ」）
// も、README の一覧の行数と合わせる。
func TestExitCodeOneReasonsAgree(t *testing.T) {
	t.Run("publish の使い方", func(t *testing.T) {
		entry := exitCodeEntry(t, publishUsage, exitProblems)
		body, _, ok := strings.Cut(entry, "ので止めた")
		if !ok {
			t.Fatalf("終了コード1の項目が「…ので止めた」の形でない: %s", entry)
		}
		got := strings.Split(strings.Replace(body, "、または", "、", 1), "、")
		if !slices.Equal(got, publishStopReasons) {
			t.Errorf("publish の使い方の終了コード1の理由 = %q, want %q", got, publishStopReasons)
		}
		if !strings.Contains(entry, checkInPublishUsage) {
			t.Errorf("publish の使い方の終了コード1に --check の場合が無い: %s", entry)
		}
	})

	t.Run("dwloc help", func(t *testing.T) {
		entry := exitCodeEntry(t, usageText, exitProblems)
		_, tail, ok := strings.Cut(entry, "publish が")
		if !ok {
			t.Fatalf("終了コード1の項目に publish が無い: %s", entry)
		}
		if got := bracketed(tail); !slices.Equal(got, publishStopReasons) {
			t.Errorf("dwloc help の終了コード1の publish の理由 = %q, want %q", got, publishStopReasons)
		}
		if !strings.Contains(entry, checkInHelp) {
			t.Errorf("dwloc help の終了コード1に --check の場合が無い: %s", entry)
		}
	})

	count, items := readmeExitList(t, readRepoFile(t, "README.md"), "1が返るのは次の")

	t.Run("README", func(t *testing.T) {
		var got []string
		for _, item := range items {
			if strings.HasPrefix(item, "`publish`が") {
				got = append(got, bracketed(item)...)
			}
		}
		if !slices.Equal(got, publishStopReasons) {
			t.Errorf("README の publish の理由 = %q, want %q", got, publishStopReasons)
		}
		if !slices.Contains(items, checkInReadme) {
			t.Errorf("README の一覧に %q が無い: %q", checkInReadme, items)
		}
		if want := strconv.Itoa(len(items)) + "つです。"; count != want {
			t.Errorf("README の件数の行 = %q, 一覧は %d 行（want %q）", count, len(items), want)
		}
	})

	t.Run("README.en", func(t *testing.T) {
		countEn, itemsEn := readmeExitList(t, readRepoFile(t, "README.en.md"), "1 comes back in these ")
		var got []string
		for _, item := range itemsEn {
			if reason, ok := strings.CutPrefix(item, "When `publish` judges that "); ok {
				got = append(got, strings.TrimSuffix(reason, " and stops"))
			}
		}
		if !slices.Equal(got, publishStopReasonsEn) {
			t.Errorf("README.en の publish の理由 = %q, want %q", got, publishStopReasonsEn)
		}
		if !slices.Contains(itemsEn, checkInReadmeEn) {
			t.Errorf("README.en の一覧に %q が無い: %q", checkInReadmeEn, itemsEn)
		}
		word, _, _ := strings.Cut(countEn, " ")
		if numberWords[word] != len(itemsEn) {
			t.Errorf("README.en の件数の行 = %q, 一覧は %d 行", countEn, len(itemsEn))
		}
		if len(itemsEn) != len(items) {
			t.Errorf("README.en の一覧は %d 行、README は %d 行", len(itemsEn), len(items))
		}
	})

	t.Run("main.go の注記", func(t *testing.T) {
		comment := packageComment(t)
		_, tail, ok := strings.Cut(comment, "publish が")
		if !ok {
			t.Fatalf("パッケージの注記に publish が無い: %s", comment)
		}
		head, _, _ := strings.Cut(tail, "と判断して")
		if got := bracketed(head); !slices.Equal(got, publishStopReasons) {
			t.Errorf("パッケージの注記の publish の理由 = %q, want %q", got, publishStopReasons)
		}
		want := "README の終了コードの" + strconv.Itoa(len(items)) + "つ"
		if !strings.Contains(comment, want) {
			t.Errorf("パッケージの注記に %q が無い（README の一覧は %d 行）:\n%s", want, len(items), comment)
		}
		if !strings.Contains(comment, checkInComment) {
			t.Errorf("パッケージの注記に %q が無い:\n%s", checkInComment, comment)
		}
	})
}
