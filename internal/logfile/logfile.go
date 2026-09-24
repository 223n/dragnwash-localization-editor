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
	"sync"
	"time"
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
	return &Writer{dir: dir, now: now, day: day, file: f}, nil
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
