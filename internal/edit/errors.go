package edit

import (
	"errors"
	"fmt"
)

// ErrReadOnly はファイル全体が読み取り専用であることを表す。
// ヘッダーが受理される4種のいずれでもないか、ヘッダー行そのものが無い。
// 理由の文言は [File.ReadOnlyReason] で取れる。
var ErrReadOnly = errors.New("このファイルは読み取り専用")

// ErrNoPath は [Parse] で作った（ファイルに紐づいていない）[File] を
// 保存しようとしたことを表す。
var ErrNoPath = errors.New("保存先のパスが無い")

// ErrConflict は版の照合に失敗したことを表す番兵。
// この誤りが返ったときは1バイトも書いていない。
// errors.Is で判定でき、詳細が要るなら errors.As で [ConflictError] を取る。
var ErrConflict = errors.New("ファイルが読み込み後に変わっている")

// ConflictError は保存直前の版の照合に失敗したことを表す。
//
// 自動保存が前提のモデルなので、ここで黙って上書きすると、別の窓や別のツール
// （publish の再生成など）が書いた内容を消す。書かずに返して、呼び出し側に
// 読み直させる。
type ConflictError struct {
	// Path は保存しようとしたファイル。
	Path string
	// Want は読み込んだときの版（SHA-256 の16進）。
	Want string
	// Got はいまファイルにある版。読み取り自体に失敗したときは空。
	Got string
	// Err は読み取りに失敗したときの原因。照合できただけなら nil。
	Err error
}

func (e *ConflictError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s を読み直せないので保存しない: %v", e.Path, e.Err)
	}
	return fmt.Sprintf("%s が読み込み後に変わっている（読んだ版 %s、いまの版 %s）", e.Path, short(e.Want), short(e.Got))
}

// Unwrap は [ErrConflict] と、あれば読み取り失敗の原因を返す。
// errors.Is(err, ErrConflict) と errors.Is(err, fs.ErrNotExist) の両方が使える。
func (e *ConflictError) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrConflict, e.Err}
	}
	return []error{ErrConflict}
}

// NotEditableError はその行を編集できないことを表す。
// 行が存在しない、データ行でない、列数がヘッダーと合わない、のいずれか。
type NotEditableError struct {
	// Line は1始まりの物理行番号。
	Line int
	// Reason は編集できない理由。そのまま画面に出せる日本語。
	Reason string
}

func (e *NotEditableError) Error() string {
	return fmt.Sprintf("%d行目は編集できない: %s", e.Line, e.Reason)
}

// InvalidValueError は訳の値そのものが受け付けられないことを表す。
type InvalidValueError struct {
	// Line は1始まりの物理行番号。
	Line int
	// Reason は受け付けられない理由。
	Reason string
}

func (e *InvalidValueError) Error() string {
	return fmt.Sprintf("%d行目に書けない値: %s", e.Line, e.Reason)
}

// short は版（SHA-256 の16進64桁）を画面向けに短く切る。
// 照合そのものは常に全桁で行う。
func short(version string) string {
	const n = 12
	if len(version) <= n {
		return version
	}
	return version[:n]
}
