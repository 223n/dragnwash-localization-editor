package main

import (
	"bytes"
	"fmt"
	"io"
)

// teeWriter は、画面と記録（logs/dwloc_<日付>.log）の両方へ書く行き先です。
//
// mainWithRecord が標準出力と標準エラーをこれで包んで run へ渡します。
// サブコマンドはふつう、そのまま書けば両方に入ります。記録へ写してはいけない
// もの（原文や訳を含む報告の本文）だけを、[newUnrecorded] で画面だけへ書きます。
//
// io.MultiWriter で束ねていたころは、記録へ写さない道を作れませんでした。
// 束ねたあとの行き先から、画面だけの行き先を取り出せないためです。その結果、
// diff の本文（原文を含む）がそのまま記録へ入っていました。
type teeWriter struct {
	screen io.Writer
	record io.Writer
}

// Write は画面へ書いてから記録へ写します。
//
// 画面が先、記録が後です。画面へ書けなければ記録へは写さず、その誤りを返します
// （io.MultiWriter と同じ振る舞い）。記録の失敗は画面に響かせません。
// logfile.Writer はもともと誤りを返しません。
func (t *teeWriter) Write(p []byte) (int, error) {
	n, err := t.screen.Write(p)
	if err != nil {
		return n, err
	}
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	_, _ = t.record.Write(p)
	return n, nil
}

// unrecordedText は、記録へ写さなかった行の数を残す1行です。
//
// 省いたことそのものは記録に残します。黙って抜くと、記録を読んだ人には、
// 報告が空だったのか省いたのかが見分けられません。
const unrecordedText = "dwloc: 原文や訳を含む %d 行は記録しません（画面には出しました）。\n"

// unrecordedFileText は、diff --output のファイルへ書いた本文のうち、記録へ写さなかった
// 行の数を残す1行です。本文は画面に出していないので、[unrecordedText] と書き分けます。
const unrecordedFileText = "dwloc: 原文や訳を含む %d 行は記録しません（--output のファイルに書きました）。\n"

// unrecordedUnwrittenText は、diff --output のファイルへ書けなかったときに、記録へ
// 写さなかった行の数を残す1行です。[unrecordedFileText] のままだと、記録を読んだ人は
// ファイルができたと読みます。
const unrecordedUnwrittenText = "dwloc: 原文や訳を含む %d 行は記録しません（--output のファイルには書けませんでした）。\n"

// unrecorded は、原文や訳を含む出力を画面にだけ書く行き先です。
//
// 記録は不具合の報告に添えて手元の外へ出ます。README と Issue の雛形は、
// 記録をそのまま貼ってよいと案内しています。そこへ原文（ゲームの台本）が
// 入ると、公開の Issue で台本を配り直すことになります。訳の断片も、翻訳者が
// まだ公開していない作業の中身です。
//
// 画面には全部出します。報告の本文は、それを読むために走らせたものだからです。
//
// keep は、記録へ写してよい行を選ぶ関数です。nil なら1行も写しません。
// 写さなかった行は数えておき、[unrecorded.Close] で数だけを記録に残します。
//
// 記録を取っていないとき（試験のバッファ、記録を始められなかったとき）は、
// 受け取った行き先へそのまま書くだけです。
type unrecorded struct {
	screen io.Writer
	// record は記録の行き先です。nil なら記録を取っていません。
	record io.Writer
	keep   func(line []byte) bool
	// pending は改行を待っている途中の行です。
	pending []byte
	// omitted は記録へ写さなかった行の数です。
	omitted int
	// omittedText は、写さなかった行の数を残す1行の書式です。空なら [unrecordedText] です。
	omittedText string
}

// newUnrecorded は、w へ書くはずだった出力のうち、keep が選んだ行だけを記録へ写す
// 行き先を返します。w が記録へも書く行き先（[teeWriter]）でなければ、w へそのまま
// 書きます。
//
// 使い終わったら [unrecorded.Close] を呼んでください。呼ばないと、省いた行の数が
// 記録に残りません。
func newUnrecorded(w io.Writer, keep func(line []byte) bool) *unrecorded {
	if t, ok := w.(*teeWriter); ok {
		return &unrecorded{screen: t.screen, record: t.record, keep: keep}
	}
	return &unrecorded{screen: w}
}

// newUnrecordedTo は [newUnrecorded] と同じく、via が記録へも書く行き先なら、keep が
// 選んだ行だけを記録へ写す行き先を返します。本文は画面ではなく dst へ書きます
// （diff --output）。記録へ写さなかった行の数は [unrecordedFileText] で残します。
func newUnrecordedTo(dst, via io.Writer, keep func(line []byte) bool) *unrecorded {
	u := newUnrecorded(via, keep)
	u.screen = dst
	u.omittedText = unrecordedFileText
	return u
}

// Unwritten は、[newUnrecordedTo] の dst に組み立てた本文を、ファイルへ書けなかった
// ことを伝えます。[unrecorded.Close] が残す1行を [unrecordedUnwrittenText] に替えます。
// Close より前に呼んでください。
func (u *unrecorded) Unwritten() {
	u.omittedText = unrecordedUnwrittenText
}

// Write は画面へ全部書き、記録へは keep が選んだ行だけを写します。
func (u *unrecorded) Write(p []byte) (int, error) {
	n, err := u.screen.Write(p)
	if u.record == nil {
		return n, err
	}
	// 画面へ書けた分だけを振り分けます。書けなかった分を記録へ写すと、
	// 画面に無い行が記録にだけ残ります。
	u.pending = append(u.pending, p[:n]...)
	for {
		i := bytes.IndexByte(u.pending, '\n')
		if i < 0 {
			break
		}
		u.route(u.pending[:i+1])
		u.pending = u.pending[i+1:]
	}
	return n, err
}

// route は改行で終わる1行を、記録へ写すか数えるだけにするかに振り分けます。
func (u *unrecorded) route(line []byte) {
	if u.keep != nil && u.keep(line) {
		_, _ = u.record.Write(line)
		return
	}
	u.omitted++
}

// Record は記録にだけ書きます。画面には出しません。
//
// 画面に出した本文の代わりに、記録へ残してよい部分（行番号・キー・理由）を
// 書くためにあります。記録を取っていなければ何もしません。
func (u *unrecorded) Record(format string, args ...any) {
	if u.record == nil {
		return
	}
	fmt.Fprintf(u.record, format, args...)
}

// Close は、改行で終わっていない最後の行を始末し、写さなかった行があれば、
// その数を記録に1行残します。
func (u *unrecorded) Close() {
	if u.record == nil {
		return
	}
	if len(u.pending) > 0 {
		// 最後の行に改行を足してから振り分けます。足さないと、このあとに書く
		// 1行（省いた数）が同じ行へつながります。
		line := append(u.pending[:len(u.pending):len(u.pending)], '\n')
		u.pending = nil
		u.route(line)
	}
	if u.omitted > 0 {
		text := unrecordedText
		if u.omittedText != "" {
			text = u.omittedText
		}
		fmt.Fprintf(u.record, text, u.omitted)
		u.omitted = 0
	}
}

// diffListIndent は、diff の text 形式で一覧と内訳を書くときの字下げです
// （internal/diff の writeCategory）。
const diffListIndent = "        "

// diffHeadingLine は、diff の text 形式の行のうち、記録へ写してよいものを選びます。
//
// 写すのは、一覧より浅い字下げの行です。全体の前置き、ロケールの見出し、
// カテゴリごとの件数と「判定していません（理由）」、締めの1行がこれにあたります。
// 報告から何が起きたかを辿るには、これで足ります。
//
// 一覧と内訳は8桁の字下げで書かれます。原文と訳（「原文: 」「訳: 」）、話者名、
// 節やノードの名前はここに入ります。カテゴリの説明文もここに入りますが、
// 決まった文なので、落としても記録から読み取れることは減りません。
//
// 行の中身ではなく字下げで選ぶのは、中身で選ぶには、どの語が原文や訳なのかを
// CLI 側が知っていなければならないためです。字下げの幅が変わって原文が記録へ
// 漏れるようになったら、TestRecordLeavesOutRowContent が落ちます。
func diffHeadingLine(line []byte) bool {
	return !bytes.HasPrefix(line, []byte(diffListIndent))
}
