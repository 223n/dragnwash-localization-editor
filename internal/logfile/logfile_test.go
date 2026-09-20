package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// at は試験で使う日時を1つ作る。
func at(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, time.Local)
}

// clock は次に返す時刻を持つ時計。試験の途中で t を差し替えて日付をまたがせる。
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// newWriter は時刻を決め打ちにした [Writer] を開く。
func newWriter(t *testing.T, dir string, c *clock) *Writer {
	t.Helper()
	w, err := open(dir, c.now)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// read は書かれたファイルを読む。
func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("%s が読めない: %v", name, err)
	}
	return string(b)
}

func TestNameIsDated(t *testing.T) {
	// 名前は dwloc_<日付>.log。ここが変わると「今日の分を送ってください」が通じなくなる。
	if got, want := Name(at(2026, 9, 21, 10, 30, 45)), "dwloc_20260921.log"; got != want {
		t.Errorf("名前が %q、%q を期待", got, want)
	}
}

func TestOpenMakesDir(t *testing.T) {
	// logs が無いところへ置かれても自分で作る。翻訳者に mkdir はさせない。
	dir := filepath.Join(t.TempDir(), Dir)
	c := &clock{at(2026, 9, 21, 10, 30, 45)}
	w := newWriter(t, dir, c)

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("フォルダーが作られていない: %v", err)
	}
	if got, want := filepath.Base(w.Path()), "dwloc_20260921.log"; got != want {
		t.Errorf("開いたのが %q、%q を期待", got, want)
	}
}

func TestWriteAddsTime(t *testing.T) {
	dir := t.TempDir()
	c := &clock{at(2026, 9, 21, 10, 30, 45)}
	w := newWriter(t, dir, c)

	if _, err := w.Write([]byte("dwloc edit: 待ち受けを始めます\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := read(t, dir, "dwloc_20260921.log")
	want := "10:30:45 dwloc edit: 待ち受けを始めます\n"
	if got != want {
		t.Errorf("中身が違う\n got %q\nwant %q", got, want)
	}
}

func TestPartialWritesBecomeOneLine(t *testing.T) {
	// fmt.Fprintf が1行を何回かに分けて呼んでも、ファイルには1行として並ぶ。
	dir := t.TempDir()
	c := &clock{at(2026, 9, 21, 10, 30, 45)}
	w := newWriter(t, dir, c)

	for _, part := range []string{"待ち受け ", "http://127.0.0.1", ":8123\n"} {
		if _, err := w.Write([]byte(part)); err != nil {
			t.Fatalf("Write(%q): %v", part, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := read(t, dir, "dwloc_20260921.log")
	want := "10:30:45 待ち受け http://127.0.0.1:8123\n"
	if got != want {
		t.Errorf("中身が違う\n got %q\nwant %q", got, want)
	}
}

func TestCloseWritesUnfinishedLine(t *testing.T) {
	// 改行で終わらないまま終わった分を落とさない。Enter 待ちの案内がこれになる。
	dir := t.TempDir()
	c := &clock{at(2026, 9, 21, 10, 30, 45)}
	w := newWriter(t, dir, c)

	if _, err := w.Write([]byte("続けるには Enter")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := read(t, dir, "dwloc_20260921.log")
	want := "10:30:45 続けるには Enter\n"
	if got != want {
		t.Errorf("中身が違う\n got %q\nwant %q", got, want)
	}
}

func TestBlankLineStaysBlank(t *testing.T) {
	// 区切りとして置かれた空行を、時刻だけの行にしない。
	dir := t.TempDir()
	c := &clock{at(2026, 9, 21, 10, 30, 45)}
	w := newWriter(t, dir, c)

	if _, err := w.Write([]byte("上\n\n下\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := read(t, dir, "dwloc_20260921.log")
	want := "10:30:45 上\n\n10:30:45 下\n"
	if got != want {
		t.Errorf("中身が違う\n got %q\nwant %q", got, want)
	}
}

func TestCarriageReturnIsDropped(t *testing.T) {
	dir := t.TempDir()
	c := &clock{at(2026, 9, 21, 10, 30, 45)}
	w := newWriter(t, dir, c)

	if _, err := w.Write([]byte("CRLF で来た行\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := read(t, dir, "dwloc_20260921.log")
	want := "10:30:45 CRLF で来た行\n"
	if got != want {
		t.Errorf("中身が違う\n got %q\nwant %q", got, want)
	}
}

func TestSameDayAppends(t *testing.T) {
	// 1日に何度実行しても同じファイルへ足す。上書きすると朝の分が消える。
	dir := t.TempDir()
	c := &clock{at(2026, 9, 21, 10, 30, 45)}

	w1 := newWriter(t, dir, c)
	if _, err := w1.Write([]byte("1回目\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	c.t = at(2026, 9, 21, 14, 12, 2)
	w2 := newWriter(t, dir, c)
	if _, err := w2.Write([]byte("2回目\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := read(t, dir, "dwloc_20260921.log")
	want := "10:30:45 1回目\n14:12:02 2回目\n"
	if got != want {
		t.Errorf("中身が違う\n got %q\nwant %q", got, want)
	}
}

func TestDayChangeMovesToNextFile(t *testing.T) {
	// 待ち受けは何時間も動く。日付をまたいだ分を前日のファイルへ続けない。
	dir := t.TempDir()
	c := &clock{at(2026, 9, 21, 23, 59, 59)}
	w := newWriter(t, dir, c)

	if _, err := w.Write([]byte("前の日\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	c.t = at(2026, 9, 22, 0, 0, 1)
	if _, err := w.Write([]byte("次の日\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got, want := read(t, dir, "dwloc_20260921.log"), "23:59:59 前の日\n"; got != want {
		t.Errorf("前の日の中身が違う\n got %q\nwant %q", got, want)
	}
	if got, want := read(t, dir, "dwloc_20260922.log"), "00:00:01 次の日\n"; got != want {
		t.Errorf("次の日の中身が違う\n got %q\nwant %q", got, want)
	}
}

func TestWriteKeepsGoingWhenFileFails(t *testing.T) {
	// いちばん大事な性質。記録が書けなくなっても画面への出力を止めない。
	// io.MultiWriter は最初の失敗でそこから先をやめるので、ここが誤りを返すと
	// 記録の道連れで画面が黙る。
	dir := t.TempDir()
	c := &clock{at(2026, 9, 21, 10, 30, 45)}
	w := newWriter(t, dir, c)

	// 裏でファイルを閉じて、書けない状態を作る。
	if err := w.file.Close(); err != nil {
		t.Fatalf("仕込みの Close: %v", err)
	}

	line := []byte("これは書けない\n")
	n, err := w.Write(line)
	if err != nil {
		t.Errorf("書けなくても Write は誤りを返さない: %v", err)
	}
	if n != len(line) {
		t.Errorf("Write が %d、%d を期待", n, len(line))
	}
	// 続けて呼んでも同じ。
	if _, err := w.Write([]byte("次も書けない\n")); err != nil {
		t.Errorf("2回目の Write が誤りを返した: %v", err)
	}
	// 失敗そのものは Close が返す。呼び出し側はここで1度だけ知らせられる。
	if err := w.Close(); err == nil {
		t.Error("Close が失敗を返していない")
	}
}

func TestWriteAfterCloseIsQuiet(t *testing.T) {
	dir := t.TempDir()
	c := &clock{at(2026, 9, 21, 10, 30, 45)}
	w := newWriter(t, dir, c)

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	line := []byte("閉じたあと\n")
	if n, err := w.Write(line); err != nil || n != len(line) {
		t.Errorf("閉じたあとの Write が n=%d err=%v", n, err)
	}
	if got := w.Path(); got != "" {
		t.Errorf("閉じたあとの Path が %q", got)
	}
	if err := w.Close(); err != nil {
		t.Errorf("2回目の Close が誤りを返した: %v", err)
	}
	if got, want := read(t, dir, "dwloc_20260921.log"), ""; got != want {
		t.Errorf("閉じたあとに書かれている: %q", got)
	}
}

func TestHideReplacesSecret(t *testing.T) {
	// 最初の1回の URL に載るトークンを、ファイルへは残さない。
	// 残すと、ログファイルを貼った不具合の報告からトークンが読める。
	const token = "s3cret-token"
	dir := t.TempDir()
	c := &clock{at(2026, 9, 21, 10, 30, 45)}
	w := newWriter(t, dir, c)

	w.Hide(token)
	if _, err := w.Write([]byte("開きます http://127.0.0.1:8123/?t=" + token + "\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := read(t, dir, "dwloc_20260921.log")
	if strings.Contains(got, token) {
		t.Errorf("トークンが残っている: %q", got)
	}
	want := "10:30:45 開きます http://127.0.0.1:8123/?t=***\n"
	if got != want {
		t.Errorf("中身が違う\n got %q\nwant %q", got, want)
	}
}

func TestHideIgnoresEmpty(t *testing.T) {
	// 空文字列を覚えると、どの行も全部が伏せ字になる。
	dir := t.TempDir()
	c := &clock{at(2026, 9, 21, 10, 30, 45)}
	w := newWriter(t, dir, c)

	w.Hide("")
	if _, err := w.Write([]byte("そのまま\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got, want := read(t, dir, "dwloc_20260921.log"), "10:30:45 そのまま\n"; got != want {
		t.Errorf("中身が違う\n got %q\nwant %q", got, want)
	}
}
