// Package logfile は実行の記録をファイルへ残します。
//
// 置き場はカレントディレクトリの logs で、ファイル名は dwloc_<日付>.log です。
// 1日に何度実行しても同じファイルへ追記するので、「今日の分を送ってください」
// と言えば、その日にあったことが1つのファイルで揃います。
//
// 古いファイルは消しません。道具が人のファイルを消さないほうが安全だという
// 判断で、溜まったものの始末は人に任せます。
//
// # 何を書くか
//
// 画面に出したものを写します。標準出力と標準エラーの両方を [Writer] へ束ねるのは
// 呼び出し側（cmd/dwloc）で、このパッケージは受け取ったバイト列を行に切って
// 時刻を付けるだけです。
//
// 書いてはいけないものの判断は、ここではなく書く側にあります。原文と訳を
// 記録に載せない約束は、edit の要求の記録では internal/web が守り（internal/web の
// パッケージコメント「外へ出さない」）、diff の本文と publish の訳の断片では
// cmd/dwloc が守ります（画面にだけ書き、ここへは渡しません）。ここを通ることで
// 緩みはしません。
//
// # 行の組み立て
//
// [Writer] は改行で切れるまで溜めてから書きます。fmt.Fprintf が1行を何回かに
// 分けて呼ばれても、ファイルには時刻付きの1行として並びます。改行で終わらない
// まま終わった分は [Writer.Close] が書き出します。
package logfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// Dir は記録を置くフォルダー名です。カレントディレクトリからの相対で使います。
	//
	// 直下に置かずフォルダーを1つ挟むのは、翻訳リポジトリの作業ツリーへ
	// 日ごとのファイルが積み上がるのを避けるためです。翻訳リポジトリ側の
	// .gitignore に *.log があるとは限らないので、こちらで1つにまとめます。
	Dir = "logs"

	// namePrefix と nameExt はファイル名の前後です。
	namePrefix = "dwloc_"
	nameExt    = ".log"

	// dayLayout はファイル名に使う日付の形です。
	dayLayout = "20060102"

	// timeLayout は行頭に付ける時刻の形です。日付はファイル名にあるので付けません。
	timeLayout = "15:04:05"

	// hiddenMark は [Writer.Hide] で伏せた文字列の代わりに書くものです。
	hiddenMark = "***"
)

// Writer は日付ごとのファイルへ行単位で書きます。
//
// 書き込みが失敗しても Write は成功を返し、以後は書くのをやめます。記録が
// 残せないことを理由に翻訳の作業を止めないためです。最初の失敗は [Writer.Close]
// が返すので、呼び出し側はそこで1度だけ知らせられます。
type Writer struct {
	dir string
	now func() time.Time

	mu sync.Mutex
	// day は今開いているファイルの日付です。
	day string
	// file は今開いているファイルです。Close のあとは nil になります。
	file *os.File
	// line は改行を待っている途中の行です。
	line []byte
	// err は最初に起きた書き込みの失敗です。入ったら以後は書きません。
	err error
	// hidden は書く前に伏せる文字列です。[Writer.Hide] が足します。
	hidden []string
	// shortened は書く前に短くするパスです。[Writer.ShortenPath] が足します。
	shortened []shortPath
	// foldCase はパスの大文字と小文字を区別せずに探すかです。Windows だけ真です。
	foldCase bool
}

// shortPath は、記録へ書く前に置き換えるパスと、置き換える先の組です。
type shortPath struct {
	from []byte
	to   []byte
}

// ShortenPath は、記録へ書く前に dir を short に置き換えるよう覚えます。
//
// 利用者のホームのパス（C:\Users\<名前>、/home/<名前>）を ~ にするためにあります。
// ホームのパスには利用者名が入り、記録は不具合の報告に添えて手元の外へ出ます。
// 画面には全文を出したままにします。書けないファイルの案内などは、権限の話が
// 読めないと直し方に手が届かないためです（internal/web の rows.go）。
//
// 区切りが \ と / のどちらで書かれていても置き換えます。dwloc は同じパスを
// スラッシュ区切りに直して出すことがあるためです（displayPath など）。
// Windows では大文字と小文字を区別しません。打たれた --root の綴りが、
// ホームの綴りとそろっているとは限らないためです。
//
// 置き換えるのは、パスの切れ目で始まって終わるところだけです。/home/al を覚えても、
// /home/alice や /mnt/home/al は置き換えません。
//
// 空のパスとルートそのもの（/ や C:\）は覚えません。覚えると、ほかのパスまで
// 書き換わって読めなくなります。
func (w *Writer) ShortenPath(dir, short string) {
	dir = strings.TrimRight(dir, `/\`)
	if dir == "" || filepath.Dir(dir) == dir || strings.HasSuffix(dir, ":") {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, form := range []string{dir, filepath.ToSlash(dir)} {
		dup := false
		for _, s := range w.shortened {
			if string(s.from) == form {
				dup = true
			}
		}
		if !dup {
			w.shortened = append(w.shortened, shortPath{from: []byte(form), to: []byte(short)})
		}
	}
}

// replacePath は line の中の from を to に置き換えます。前後がパスの切れ目の
// ところだけを置き換えます（[Writer.ShortenPath]）。fold が真なら、ASCII の
// 大文字と小文字を区別せずに探します。
func replacePath(line, from, to []byte, fold bool) []byte {
	haystack, needle := line, from
	if fold {
		// 長さを変えずに小文字へそろえます。ASCII だけを畳むので、位置は元の
		// line と同じまま使えます。
		haystack, needle = asciiLower(line), asciiLower(from)
	}
	var out []byte
	copied, at := 0, 0
	for {
		i := bytes.Index(haystack[at:], needle)
		if i < 0 {
			break
		}
		start := at + i
		end := start + len(needle)
		if !pathBoundaryBefore(line[:start]) || !pathBoundaryAfter(line[end:]) {
			at = start + 1
			continue
		}
		out = append(out, line[copied:start]...)
		out = append(out, to...)
		copied, at = end, end
	}
	if out == nil {
		return line
	}
	return append(out, line[copied:]...)
}

// asciiLower は ASCII の大文字だけを小文字にした写しを返します。
func asciiLower(b []byte) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return out
}

// pathBoundaryBefore は、before の直後でパスが始まってよいかを返します。
// 直前がパスの一部（名前の文字か区切り）なら、もっと長いパスの途中です。
func pathBoundaryBefore(before []byte) bool {
	if len(before) == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRune(before)
	return r != '/' && r != '\\' && !isNameRune(r)
}

// pathBoundaryAfter は、after の直前でパスの一部が終わってよいかを返します。
// 続きが区切りなら、その下のパスです。名前の文字なら、別の名前の途中です。
func pathBoundaryAfter(after []byte) bool {
	if len(after) == 0 {
		return true
	}
	r, _ := utf8.DecodeRune(after)
	return r == '/' || r == '\\' || !isNameRune(r)
}

// isNameRune は、ファイル名の中に続けて現れうる文字かを返します。
func isNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_'
}

// Hide は記録へ書く前に伏せる文字列を1つ覚えます。
//
// 画面に出したものをそのまま写す以上、写してよくないものは写す側で落とすしか
// ありません。dwloc edit のトークンがこれにあたります。最初の1回の URL に
// 載っており、その URL は画面に出るので、何もしなければファイルにも残ります。
// 要求の記録からトークンを落としている（internal/web の logger）のと同じ理由で、
// ファイルにも残しません。
//
// 空文字列は覚えません。覚えると、どの行も全部が伏せ字になります。
func (w *Writer) Hide(secret string) {
	if secret == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.hidden = append(w.hidden, secret)
}

// Open は dir の中に今日のファイルを用意し、追記で書ける [Writer] を返します。
//
// フォルダーが無ければ作ります。ここで失敗したら記録は始めません。書けない
// まま進めても、あとから「記録が残っていない」と気付くだけだからです。
func Open(dir string) (*Writer, error) {
	return open(dir, time.Now)
}

// open は時計を差し替えられる [Open] です。日付をまたぐ振る舞いを試すためにあります。
func open(dir string, now func() time.Time) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	day := now().Format(dayLayout)
	f, err := openDay(dir, day)
	if err != nil {
		return nil, err
	}
	return &Writer{dir: dir, now: now, day: day, file: f, foldCase: runtime.GOOS == "windows"}, nil
}

// Name は日付に対するファイル名を返します。
func Name(t time.Time) string {
	return namePrefix + t.Format(dayLayout) + nameExt
}

// openDay は1日分のファイルを追記で開きます。
func openDay(dir, day string) (*os.File, error) {
	return os.OpenFile(filepath.Join(dir, namePrefix+day+nameExt),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

// Path は今書いているファイルの場所を返します。閉じたあとは空になります。
func (w *Writer) Path() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return ""
	}
	return w.file.Name()
}

// Write は受け取ったバイト列を行に切って書きます。
//
// 戻り値は常に len(p) と nil です。io.MultiWriter は最初の失敗でそこから先を
// やめるので、ここが誤りを返すと、記録が書けなくなった時点で画面への出力まで
// 止まります。記録のために画面を失わせません。
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil || w.err != nil {
		return len(p), nil
	}

	now := w.now()
	w.rotate(now)

	rest := p
	for w.err == nil {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			w.line = append(w.line, rest...)
			break
		}
		w.line = append(w.line, rest[:i]...)
		w.flushLine(now)
		rest = rest[i+1:]
	}
	return len(p), nil
}

// Close は途中の行を書き出してファイルを閉じます。
//
// 返すのは最初に起きた書き込みの失敗か、閉じるときの失敗です。2回目以降の
// 呼び出しは何もせず nil を返します。
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	if len(w.line) > 0 {
		w.flushLine(w.now())
	}
	err := w.file.Close()
	w.file = nil
	if w.err != nil {
		return w.err
	}
	return err
}

// rotate は日付が変わっていたら次のファイルへ移ります。
//
// 待ち受けは何時間も動くので、日付をまたいだ分が前日のファイルに続くことが
// あります。「1日1ファイル」を名乗る以上、書くたびに見ます。
//
// 途中の行は移る前に書き出します。前日のファイルへ今日の時刻で入りますが、
// 行を2つのファイルへ割るよりは読めます。
func (w *Writer) rotate(now time.Time) {
	day := now.Format(dayLayout)
	if day == w.day {
		return
	}
	if len(w.line) > 0 {
		w.flushLine(now)
	}
	f, err := openDay(w.dir, day)
	if err != nil {
		w.err = err
		return
	}
	_ = w.file.Close()
	w.file = f
	w.day = day
}

// flushLine は溜めた1行を時刻付きで書き出します。
//
// 空行には時刻を付けません。区切りとして置かれた空行が、時刻だけの行になって
// 見た目の意味を失うのを避けるためです。行末の CR は落とします。
func (w *Writer) flushLine(now time.Time) {
	line := bytes.TrimSuffix(w.line, []byte("\r"))
	for _, secret := range w.hidden {
		line = bytes.ReplaceAll(line, []byte(secret), []byte(hiddenMark))
	}
	for _, s := range w.shortened {
		line = replacePath(line, s.from, s.to, w.foldCase)
	}
	var err error
	if len(line) == 0 {
		_, err = w.file.Write([]byte{'\n'})
	} else {
		_, err = fmt.Fprintf(w.file, "%s %s\n", now.Format(timeLayout), line)
	}
	w.line = w.line[:0]
	if err != nil && w.err == nil {
		w.err = err
	}
}
