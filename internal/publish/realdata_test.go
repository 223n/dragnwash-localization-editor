package publish

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/sourcerepo"
)

// minPublishedLocales は Translations 直下にあるロケールの数の下限。
//
// 数を書き定めていた（13）が、それは「いまワークツリーに何ロケールあるか」で
// あって、この実装の性質ではない。本体のチェックアウトには16ロケールあり、
// DRAGNWASH_SOURCE_REPO をそちらへ向けると、16ロケールとも入力とバイト一致して
// いるのに、ロケール数の照合だけで落ちていた。ロケールは増えるものなので、
// 突き合わせる相手はそのリポジトリ自身から数える（[localeDirs]）。
//
// 下限も見る。0 と 0 が一致してしまうと、Translations をそもそも読めていない
// ことに気づけない。13 は、このリポジトリのどのチェックアウトにもあった数である。
const minPublishedLocales = 13

// localeDirs は root/Translations 直下のロケールの数を数える。
//
// 数え方は [DiscoverTargets] と同じで、ディレクトリだけを見て、名前が '_' で
// 始まるものを飛ばす。ignore.txt はファイルなので数に入らない。
func localeDirs(t *testing.T, root string) int {
	t.Helper()

	dir := filepath.Join(root, TranslationsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("%s を読めない: %v", dir, err)
	}
	n := 0
	for _, entry := range entries {
		if !isDir(filepath.Join(dir, entry.Name())) {
			continue
		}
		if strings.HasPrefix(entry.Name(), localeSkipPrefix) {
			continue
		}
		n++
	}
	if n < minPublishedLocales {
		t.Fatalf("ロケールが %d しかない。Translations を読めていない: %s", n, dir)
	}
	return n
}

// sourceRepo は元実装のリポジトリの場所を返す。環境変数（sourcerepo.Env）で指定した
// 場所に無ければ落とし、指定していなくて見つからなければ飛ばす（sourcerepo.Find）。
// CI には元リポジトリが無いので、飛ばせることが必須。
func sourceRepo(t *testing.T) string {
	t.Helper()
	return sourcerepo.Find(t, "data", "script_order.csv")
}

// TestRealDataRoundTrip は元リポジトリの各ロケールの strings.csv を入力にして
// 組み立て直し、入力とバイト単位で一致することを確かめる。
//
// これらのファイルは tools/hash-strings.ps1 の出力そのものなので、移植が正しければ
// 再生成は冪等になる。ノード見出しの再出力条件や、order 列を生の綴りのまま書く点を
// 間違えると、ここで必ず落ちる。
//
// 元リポジトリのファイルは読むだけで、絶対に書き換えない。
func TestRealDataRoundTrip(t *testing.T) {
	root := sourceRepo(t)

	data, err := LoadOrder(root)
	if err != nil {
		t.Fatalf("再生順データを読めない: %v", err)
	}
	targets, err := DiscoverTargets(root)
	if err != nil {
		t.Fatalf("対象を列挙できない: %v", err)
	}
	if want := localeDirs(t, root); len(targets) != want {
		t.Errorf("ロケール数が違う: got %d, want %d", len(targets), want)
	}

	matched := 0
	for _, target := range targets {
		t.Run(target.Locale, func(t *testing.T) {
			// 元リポジトリには Translations/_discovered が無いので、
			// 入力は公開ファイル自身になるはず。
			if target.Input != target.Output {
				t.Fatalf("入力が公開ファイルでない: %q", target.Input)
			}

			want, err := os.ReadFile(target.Input)
			if err != nil {
				t.Fatalf("入力を読めない: %v", err)
			}
			got, _, err := BuildTarget(data, target)
			if err != nil {
				t.Fatalf("組み立てに失敗した: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("バイト一致しない\n%s", firstDifference(got, want))
				return
			}
			matched++
		})
	}
	t.Logf("バイト一致したロケール: %d/%d", matched, len(targets))
}

// TestRealDataWriteToTempDir は一時ディレクトリへ実際に書き出しても同じ内容になる
// ことを確かめる。元リポジトリには一切書き込まない。
func TestRealDataWriteToTempDir(t *testing.T) {
	root := sourceRepo(t)

	data, err := LoadOrder(root)
	if err != nil {
		t.Fatalf("再生順データを読めない: %v", err)
	}
	targets, err := DiscoverTargets(root)
	if err != nil {
		t.Fatalf("対象を列挙できない: %v", err)
	}

	tmp := t.TempDir()
	for _, source := range targets {
		want, err := os.ReadFile(source.Input)
		if err != nil {
			t.Fatalf("入力を読めない: %v", err)
		}

		// 出力先だけを一時ディレクトリへ向ける。入力は元リポジトリのまま読む。
		target := Target{
			Locale: source.Locale,
			Input:  source.Input,
			Output: filepath.Join(tmp, TranslationsDir, source.Locale, StringsFile),
		}
		// 出力先が空なのでヘッダー直下のコメントは引き継がれない。
		// 引き継ぎ込みで一致するかは TestRealDataRoundTrip が見ている。
		if _, err := WriteTarget(data, target); err != nil {
			t.Fatalf("%s の書き出しに失敗した: %v", source.Locale, err)
		}

		got, err := os.ReadFile(target.Output)
		if err != nil {
			t.Fatalf("書き出したファイルを読めない: %v", err)
		}
		comments := HeaderComments(want)
		if len(comments) == 0 && !bytes.Equal(got, want) {
			t.Errorf("%s: コメントが無いのに一致しない\n%s", source.Locale, firstDifference(got, want))
		}
		if len(comments) > 0 && bytes.Equal(got, want) {
			t.Errorf("%s: コメントを引き継いでいないのに一致してしまった", source.Locale)
		}
	}

	// 元リポジトリのファイルが変わっていないことを念のため確かめる。
	for _, source := range targets {
		if _, err := os.Stat(source.Input); err != nil {
			t.Fatalf("元リポジトリのファイルが失われた: %v", err)
		}
	}
}

// TestRealDataNodeHeaderRevisit は同一セクション内でノードが再訪される箇所が
// 実データに2件あり、そこでノード見出しが2回出ることを確かめる。
// seen 集合で実装すると全ロケールで2行ずれる箇所の回帰テスト。
func TestRealDataNodeHeaderRevisit(t *testing.T) {
	root := sourceRepo(t)

	data, err := LoadOrder(root)
	if err != nil {
		t.Fatalf("再生順データを読めない: %v", err)
	}
	input, err := os.ReadFile(filepath.Join(root, TranslationsDir, "ja", StringsFile))
	if err != nil {
		t.Fatalf("ja/strings.csv を読めない: %v", err)
	}
	out, _, err := Build(data, input, input)
	if err != nil {
		t.Fatalf("組み立てに失敗した: %v", err)
	}

	tests := []struct {
		header string
		want   int
	}{
		{header: "# --- cum: Conrad_finished_jerkoff_3 ---", want: 2},
		{header: "# --- intro: Ryan_5_intro ---", want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			if got := bytes.Count(out, []byte(tt.header+"\n")); got != tt.want {
				t.Errorf("見出しの数が違う: got %d, want %d", got, tt.want)
			}
		})
	}
}

// firstDifference は最初に食い違ったバイト位置とその前後を、報告用に整形する。
func firstDifference(got, want []byte) string {
	n := min(len(got), len(want))
	i := 0
	for i < n && got[i] == want[i] {
		i++
	}

	line := 1 + bytes.Count(want[:i], []byte("\n"))
	from := max(i-60, 0)
	to := func(b []byte) int { return min(i+60, len(b)) }

	return "最初の相違: バイト " + strconv.Itoa(i) + "（want の " + strconv.Itoa(line) + " 行目付近）" +
		"\n  got  ..." + string(got[min(from, len(got)):to(got)]) + "..." +
		"\n  want ..." + string(want[from:to(want)]) + "..." +
		"\n  長さ got=" + strconv.Itoa(len(got)) + " want=" + strconv.Itoa(len(want))
}
