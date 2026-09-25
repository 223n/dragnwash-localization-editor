package edit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
	"github.com/223n/dragnwash-localization-editor/internal/sourcerepo"
)

// minPublishedLocales は Translations 直下にあるロケールの数の下限。
//
// 数を書き定めていた（13）が、それは「いまワークツリーに何ロケールあるか」で
// あって、この実装の性質ではない。本体のチェックアウトには16ロケールあり、
// DRAGNWASH_SOURCE_REPO をそちらへ向けると、ロケール数の照合だけで落ちていた。
// ロケールは増えるものなので、突き合わせる相手はそのリポジトリ自身から数える
// （[localeDirs]）。
//
// 下限も見る。0 と 0 が一致してしまうと、Translations をそもそも読めていない
// ことに気づけない。13 は、このリポジトリのどのチェックアウトにもあった数である。
const minPublishedLocales = 13

// minDataLines は1ロケールあたりのデータ行数の下限。実データは1680行台なので、
// これを下回ったら「読めていない」ということ。行数はロケールごとに違う
// （台詞ID行の数が違う）ので、固定値では確かめない。
const minDataLines = 1000

// sourceRepo は元実装のリポジトリの場所を返す。環境変数（sourcerepo.Env）で指定した
// 場所に無ければ落とし、指定していなくて見つからなければ飛ばす（sourcerepo.Find）。
// CI には元リポジトリが無いので、飛ばせることが必須。
func sourceRepo(t *testing.T) string {
	t.Helper()
	return sourcerepo.Find(t, "data", "script_order.csv")
}

// realTargets は元リポジトリの全ロケールの公開ファイルを列挙する。
// 元リポジトリのファイルは読むだけで、絶対に書き換えない。
func realTargets(t *testing.T) []publish.Target {
	t.Helper()
	root := sourceRepo(t)
	targets, err := publish.DiscoverTargets(root)
	if err != nil {
		t.Fatalf("対象を列挙できない: %v", err)
	}
	if want := localeDirs(t, root); len(targets) != want {
		t.Fatalf("ロケール数が違う: got %d, want %d", len(targets), want)
	}
	return targets
}

// localeDirs は root/Translations 直下のロケールの数を数える。
//
// 数え方は publish.DiscoverTargets と同じで、ディレクトリだけを見て、名前が
// '_' で始まるものを飛ばす。ignore.txt はファイルなので数に入らない。
func localeDirs(t *testing.T, root string) int {
	t.Helper()

	dir := filepath.Join(root, "Translations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("%s を読めない: %v", dir, err)
	}
	n := 0
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join(dir, entry.Name()))
		if err != nil || !info.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		n++
	}
	if n < minPublishedLocales {
		t.Fatalf("ロケールが %d しかない。Translations を読めていない: %s", n, dir)
	}
	return n
}

// TestRealDataRoundTrip は全ロケールの公開ファイルを読んで、1行も編集せずに
// 書き戻すと入力とバイト単位で完全に一致することを確かめる。
//
// このパッケージの約束「触っていない行は1バイトも変えない」を、実データで
// いちばん強く縛るテスト。行の生テキストをそのまま持たずに組み立て直す実装に
// 変えたら、見出しの空行や引用の仕方の違いでここが必ず落ちる。
func TestRealDataRoundTrip(t *testing.T) {
	for _, target := range realTargets(t) {
		t.Run(target.Locale, func(t *testing.T) {
			want, err := os.ReadFile(target.Input)
			if err != nil {
				t.Fatalf("入力を読めない: %v", err)
			}

			f := Parse(want)
			if f.ReadOnly() {
				t.Fatalf("読み取り専用になった: %s", f.ReadOnlyReason())
			}
			if got := strings.Join(f.Header(), ","); got != "key,section,node,order,speaker,translation" {
				t.Fatalf("ヘッダーが %q", got)
			}
			if got := f.Bytes(); !bytes.Equal(got, want) {
				t.Errorf("バイト一致しない: %d バイト vs %d バイト（最初の差は %d バイト目）",
					len(got), len(want), firstDiff(got, want))
			}
			if f.Dirty() {
				t.Error("編集していないのに Dirty")
			}

			// 実データのデータ行はすべて6列なので、1行も編集不可にならないはず。
			data, editable, comments, blanks := countKinds(f)
			if data < minDataLines {
				t.Errorf("データ行が %d 行しかない", data)
			}
			if editable != data {
				t.Errorf("編集できないデータ行がある: %d / %d", data-editable, data)
			}
			if comments == 0 || blanks == 0 {
				t.Errorf("見出しか空行を読み落としている: comment=%d blank=%d", comments, blanks)
			}
		})
	}
}

// TestRealDataSingleLineEdit は実データで1行の訳を差し替え、その行以外が
// 1バイトも変わらないことを確かめる。
//
// 確かめ方は編集モデルの内部を使わない。書き出したバイト列を
// csvfile.SplitPythonLines で分け直し、目的の行以外を元の行と突き合わせる。
func TestRealDataSingleLineEdit(t *testing.T) {
	for _, target := range realTargets(t) {
		t.Run(target.Locale, func(t *testing.T) {
			before, err := os.ReadFile(target.Input)
			if err != nil {
				t.Fatalf("入力を読めない: %v", err)
			}

			// 先頭・真ん中・末尾のデータ行を1本ずつ試す。
			var editable []Line
			for _, line := range Parse(before).Lines() {
				if line.Kind == KindData && line.Editable {
					editable = append(editable, line)
				}
			}
			if len(editable) < minDataLines {
				t.Fatalf("編集できるデータ行が %d 行しかない", len(editable))
			}
			picks := []Line{editable[0], editable[len(editable)/2], editable[len(editable)-1]}

			for _, pick := range picks {
				f := Parse(before)
				if err := f.SetTranslation(pick.ID, `テスト訳 "引用", カンマ`); err != nil {
					t.Fatalf("%d行目の書き換えに失敗した: %v", pick.Number, err)
				}
				after := f.Bytes()
				assertOnlyLineChanged(t, before, after, pick.Number)

				// 書き戻した行を読み直すと、同じ ID で入れた値がそのまま取れる。
				line, _ := Parse(after).Line(pick.ID)
				if got := line.Translation(); got != `テスト訳 "引用", カンマ` || line.Number != pick.Number {
					t.Errorf("%d行目の訳が %q（読み直した行番号 %d）", pick.Number, got, line.Number)
				}
			}
		})
	}
}

// TestRealDataSyntheticWorkingCopy は実データの公開ファイルから作業コピー
// （source_en 付きの7列）を合成して、同じ検査を通す。
//
// 元リポジトリに Translations/_discovered は入っていないので、作業コピーは
// 手元で合成するしかない。合成の仕方は WorkingCopy.cs に合わせて、見出しと
// 空行をそのまま残し、speaker と translation の間に source_en を1列足す。
// 未訳行（末尾が `,` で終わる行）もそのまま残す。
func TestRealDataSyntheticWorkingCopy(t *testing.T) {
	for _, target := range realTargets(t) {
		t.Run(target.Locale, func(t *testing.T) {
			published, err := os.ReadFile(target.Input)
			if err != nil {
				t.Fatalf("入力を読めない: %v", err)
			}
			before := synthesizeWorkingCopy(t, published)

			f := Parse(before)
			if f.ReadOnly() {
				t.Fatalf("読み取り専用になった: %s", f.ReadOnlyReason())
			}
			if len(f.Header()) != 7 {
				t.Fatalf("ヘッダーが %q", f.Header())
			}
			if got := f.Bytes(); !bytes.Equal(got, before) {
				t.Fatalf("バイト一致しない（最初の差は %d バイト目）", firstDiff(got, before))
			}

			data, editable, _, _ := countKinds(f)
			if data < minDataLines {
				t.Fatalf("データ行が %d 行しかない", data)
			}
			if editable != data {
				t.Fatalf("編集できないデータ行がある: %d / %d", data-editable, data)
			}

			// 未訳行（訳が空の行）を1本選んで訳を入れる。作業コピーで
			// いちばん多い編集の形で、ParsePowerShellRecord が末尾の空
			// フィールドを落とすせいで壊しやすいのもこの形。
			var pick Line
			for _, line := range f.Lines() {
				if line.Kind == KindData && line.Translation() == "" {
					pick = line
					break
				}
			}
			if pick.ID == 0 {
				t.Fatal("訳が空の行が1本も無い")
			}
			if err := f.SetTranslation(pick.ID, "訳を入れた"); err != nil {
				t.Fatalf("%d行目の書き換えに失敗した: %v", pick.Number, err)
			}
			assertOnlyLineChanged(t, before, f.Bytes(), pick.Number)
		})
	}
}

// synthesizeWorkingCopy は公開ファイルのバイト列から作業コピーを合成する。
// 見出し・空行・改行はそのまま、データ行だけ source_en を1列足す。
func synthesizeWorkingCopy(t *testing.T, published []byte) []byte {
	t.Helper()

	var out bytes.Buffer
	header := false
	rows := 0
	for _, line := range csvfile.SplitPythonLines(published) {
		body, term := splitTerminator(line.Text)
		switch {
		case strings.HasPrefix(body, "#"), !isRecord(body):
			out.WriteString(line.Text)
		case !header:
			header = true
			out.WriteString(strings.Join(workingHeader, ",") + term)
		default:
			fields, _ := csvfile.ParsePowerShellRecord(body)
			for len(fields) < 6 {
				fields = append(fields, "")
			}
			if len(fields) != 6 {
				t.Fatalf("%d行目が6列でない: %q", line.Number, body)
			}
			// source_en は原文の代わり。引用が要る値・空の値・カンマを含む値が
			// 混ざるようにして、前半の列に引用フィールドがある行も作る。
			source := []string{
				"Hello there",
				`He said "hi", then left`,
				"",
				"Comma, inside",
			}[rows%4]
			// 5行に1行は未訳にする。公開ファイルは訳が空の行を出力しないが、
			// 作業コピーはまだ訳していない行こそ多く持つ（WorkingCopy.cs は
			// ゲームが読んだキーを全部書く）。その形を再現しないと、
			// いちばん多い編集対象を検査しないまま通ってしまう。
			translation := fields[5]
			if rows%5 == 4 {
				translation = ""
			}
			rows++
			out.WriteString(csvfile.JoinFields(
				fields[0], fields[1], fields[2], fields[3], fields[4], source, translation) + term)
		}
	}
	if rows < minDataLines {
		t.Fatalf("合成したデータ行が %d 行しかない", rows)
	}
	return out.Bytes()
}

// assertOnlyLineChanged は before と after を物理行に分け直し、number 行だけが
// 違うことを確かめる。編集モデルの内部状態は使わない。
func assertOnlyLineChanged(t *testing.T, before, after []byte, number int) {
	t.Helper()

	if bytes.HasPrefix(before, []byte("\xef\xbb\xbf")) != bytes.HasPrefix(after, []byte("\xef\xbb\xbf")) {
		t.Fatal("BOM の有無が変わった")
	}

	oldLines := csvfile.SplitPythonLines(before)
	newLines := csvfile.SplitPythonLines(after)
	if len(oldLines) != len(newLines) {
		t.Fatalf("行数が変わった: %d -> %d", len(oldLines), len(newLines))
	}

	changed := 0
	for i := range oldLines {
		if oldLines[i].Text == newLines[i].Text {
			continue
		}
		changed++
		if oldLines[i].Number != number {
			t.Errorf("%d行目が変わった:\n before %q\n after  %q",
				oldLines[i].Number, oldLines[i].Text, newLines[i].Text)
			continue
		}
		// 変わってよいのは最終フィールドだけ。
		oldBody, oldTerm := splitTerminator(oldLines[i].Text)
		newBody, newTerm := splitTerminator(newLines[i].Text)
		if oldTerm != newTerm {
			t.Errorf("%d行目の改行が %q -> %q", number, oldTerm, newTerm)
		}
		offsets := csvfile.FieldOffsets(oldBody)
		prefix := oldBody[:offsets[len(offsets)-1]]
		if !strings.HasPrefix(newBody, prefix) {
			t.Errorf("%d行目の前半が変わった:\n got %q\nwant接頭辞 %q", number, newBody, prefix)
		}
	}
	if changed != 1 {
		t.Errorf("変わった行が %d 本（1本のはず）", changed)
	}
}

// countKinds は種類ごとの行数を数える。editable はデータ行のうち編集できる数。
func countKinds(f *File) (data, editable, comments, blanks int) {
	for _, line := range f.Lines() {
		switch line.Kind {
		case KindData:
			data++
			if line.Editable {
				editable++
			}
		case KindComment:
			comments++
		case KindBlank:
			blanks++
		}
	}
	return data, editable, comments, blanks
}

// firstDiff は最初に食い違うバイトの位置を返す。同じなら -1。
func firstDiff(a, b []byte) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}
