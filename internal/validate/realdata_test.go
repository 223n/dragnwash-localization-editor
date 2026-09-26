package validate

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/sourcerepo"
)

// sourceRepo は元実装のリポジトリの場所を返す。環境変数（sourcerepo.Env）で指定した
// 場所に無ければ落とし、指定していなくて見つからなければ飛ばす（sourcerepo.Find）。
// CI には元リポジトリが無いので、飛ばせることが必須。
func sourceRepo(t *testing.T) string {
	t.Helper()
	return sourcerepo.Find(t, "Translations")
}

// realLocales は元リポジトリに実在するロケールを、Translations 直下から数えて返す。
// このぜんぶが問題なしになることが、この移植の到達目標。
//
// 以前は上流 003ed1e の13ロケールを決め打ちにしていた。実データの試験は上流 main に
// 追従する（改善の決定 31）ので、ロケールは入力から数える。いまの main は16ロケール。
func realLocales(t *testing.T, root string) []string {
	t.Helper()
	return sourcerepo.Locales(t, root)
}

// TestRealDataAllLocales は全ロケールの公開ファイルを1つずつ検査する。
// Python 版を同じリポジトリで動かすと "translations OK" / 終了コード0 になるので、
// ここも1件も問題が出てはならない。
//
// ファイルを直接読むので git を呼ばない。_discovered と strings.local.csv の
// 検査は含まれない（そちらは TestRealDataCheckTree）。
func TestRealDataAllLocales(t *testing.T) {
	root := sourceRepo(t)

	for _, locale := range realLocales(t, root) {
		t.Run(locale, func(t *testing.T) {
			path := filepath.Join(root, TranslationsDir, locale, PublishedFile)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s が読めない: %v", path, err)
			}
			name := TranslationsDir + "/" + locale + "/" + PublishedFile
			if problems := CheckFile(name, data); len(problems) != 0 {
				t.Errorf("問題が出た（Python 版は0件）:\n%s", Report(problems))
			}
		})
	}
}

// TestRealDataCheckTree は元リポジトリ全体を、元実装の main と同じ手順で検査する。
// git も含めた通しの確認になる。
func TestRealDataCheckTree(t *testing.T) {
	root := sourceRepo(t)

	problems, err := CheckTree(root, nil)
	if err != nil {
		t.Fatalf("CheckTree が失敗した: %v", err)
	}
	if len(problems) != 0 {
		t.Errorf("問題が出た（Python 版は0件）:\n%s", Report(problems))
	}
	if got := Report(problems); got != "translations OK\n" {
		t.Errorf("Report = %q, want %q", got, "translations OK\n")
	}
}

// TestRealDataLocaleList は走査が拾うロケールが、Translations 直下のディレクトリ
// （ファイルの ignore.txt と、'_' で始まるディレクトリを除く）と同じであることを見る。
// Translations/ignore.txt を拾ってしまうと "no strings.csv" が1件増える。
//
// 期待する一覧は、ディレクトリかどうかを os.Stat で決めて数える（sourcerepo.Locales）。
// ここでは os.ReadDir の DirEntry.IsDir で数え、2つの数え方がそろうことを見る。
// どのロケールにも公開ファイルがあることも見る。
func TestRealDataLocaleList(t *testing.T) {
	root := sourceRepo(t)
	want := realLocales(t, root)

	entries, err := os.ReadDir(filepath.Join(root, TranslationsDir))
	if err != nil {
		t.Fatalf("Translations が読めない: %v", err)
	}
	var got []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name()[0] == '_' {
			continue
		}
		got = append(got, entry.Name())
	}
	if !slices.Equal(got, want) {
		t.Errorf("ロケール = %q, want %q", got, want)
	}
	for _, locale := range got {
		if _, err := os.Stat(filepath.Join(root, TranslationsDir, locale, PublishedFile)); err != nil {
			t.Errorf("%s に %s が無い: %v", locale, PublishedFile, err)
		}
	}
}

// TestRealDataHeaderIsCurrent は全ロケールが現行の6列ヘッダーであることを見る。
// 3列・2列の旧ヘッダーも受理はするが、実データにはもう無いはず。
//
// あわせて、原文の列を足すと弾かれることも確かめる。source_en 列の検出は
// 専用の検査ではなくヘッダーの完全一致に乗っているので、実データで裏を取っておく。
func TestRealDataHeaderIsCurrent(t *testing.T) {
	root := sourceRepo(t)
	current := AcceptedHeaders()[0]

	for _, locale := range realLocales(t, root) {

		t.Run(locale, func(t *testing.T) {
			path := filepath.Join(root, TranslationsDir, locale, PublishedFile)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s が読めない: %v", path, err)
			}

			records, err := csvfile.ReadPythonRecords(data)
			if err != nil {
				t.Fatalf("CSV として読めない: %v", err)
			}
			if len(records) == 0 {
				t.Fatal("レコードが1つも無い")
			}
			if !slices.Equal(records[0].Fields, current) {
				t.Errorf("ヘッダー = %q, want %q", records[0].Fields, current)
			}

			// 原文の列を足したファイルはヘッダー不正の1件だけになる。
			broken := append([]byte("key,source_en,translation\n"), data...)
			problems := CheckFile(locale, broken)
			if len(problems) != 1 || problems[0].Line != 0 {
				t.Fatalf("原文の列を足したのに報告が %#v", problemStrings(problems))
			}
			if !strings.HasPrefix(problems[0].Message, "header is ['key', 'source_en', 'translation']") {
				t.Errorf("報告の本文 = %q", problems[0].Message)
			}
		})
	}
}
