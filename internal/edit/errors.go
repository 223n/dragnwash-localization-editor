package edit

import (
	"errors"
	"fmt"

	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// ErrReadOnly はファイル全体が読み取り専用であることを表す。
// 開いた引用符がファイルの終わりまで閉じないか、ヘッダー行そのものが無いか、
// ヘッダーが受理される4種のいずれでもないか、行の区切りがすべて単独の CR である。
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

// ErrRecheck は、書く前の事後確認が外れたことを表す番兵。この誤りが返ったときは
// 1バイトも書いていない。詳細が要るなら errors.As で [RecheckError] を取る。
var ErrRecheck = errors.New("書く前に読み直して確かめると合わない")

// RecheckError は、保存の直前にファイル全体を読み直したとき、編集モデルと同じに
// 読めなかったことを表す（書く前の事後確認の後半。決まったことのそのほか 4）。
//
// 行を特定できるように、原因の書き換えたレコードを指す。書き換えたレコードが2つ以上
// なら、1つずつ確かめ直して、それだけでも外れるレコードを指す（どれも単独では外れない
// ときは、外れたところに最も近い書き換えたレコード）。画面はその行を保存できない行に
// して、送り直しを止める。ほかの行は送り直せば書ける。
type RecheckError struct {
	// ID は書き換えたレコードの ID（[Line.ID]）。
	ID int
	// Line はそのレコードの最初の物理行（1始まり）。1バイトも書いていないので、ファイルに
	// いまある行番号で言う（訳の改行でずらす前のもの）。
	Line int
	// Cause は理由（reason.EditRecheckFailed）。文面は Cause.Text。
	Cause reason.Reason
}

func (e *RecheckError) Error() string {
	return "保存しない: " + e.Cause.Text
}

// Unwrap は [ErrRecheck] を返す。
func (e *RecheckError) Unwrap() error { return ErrRecheck }

// NotEditableError はその行を編集できないことを表す。
// 行が存在しない、データ行でない、編集できない行（列数がヘッダーと合わない、など）の
// いずれか。
type NotEditableError struct {
	// ID は要求された行の ID（[Line.ID]）。
	ID int
	// Line はその行の最初の物理行（1始まり）。そんな行が無ければ 0。
	Line int
	// Reason は編集できない理由。そのまま画面に出せる日本語。
	Reason string
	// Cause は [NotEditableError.Reason] と同じ理由を、識別子と置換の組で持つ。
	//
	// 文面と別に持つのは、画面（internal/web）が目録で差し替えるためである。
	// 「%d行目は編集できない: %s」という外枠にも目録の鍵があり、画面は Line と
	// この Cause から組み直す。Cause.Text は常に Reason と同じ文字列になる。
	Cause reason.Reason
}

func (e *NotEditableError) Error() string {
	if e.Line == 0 {
		return "編集できない: " + e.Reason
	}
	return fmt.Sprintf("%d行目は編集できない: %s", e.Line, e.Reason)
}

// notEditable は理由から [NotEditableError] を作る。
//
// 文面と識別子を別々に代入する場所を増やさないための入口である。片方だけ入った
// 誤りを返すと、画面は英語で出せるのに CLI が黙る（あるいはその逆）行ができる。
func notEditable(id, line int, why reason.Reason) *NotEditableError {
	return &NotEditableError{ID: id, Line: line, Reason: why.Text, Cause: why}
}

// InvalidValueError は訳の値そのものが受け付けられないことを表す。
type InvalidValueError struct {
	// ID は要求された行の ID（[Line.ID]）。
	ID int
	// Line はその行の最初の物理行（1始まり）。
	Line int
	// Reason は受け付けられない理由。
	Reason string
	// Cause は [InvalidValueError.Reason] と同じ理由を、識別子と置換の組で持つ。
	// 意味は [NotEditableError.Cause] と同じ。
	Cause reason.Reason
}

func (e *InvalidValueError) Error() string {
	return fmt.Sprintf("%d行目に書けない値: %s", e.Line, e.Reason)
}

// invalidValue は理由から [InvalidValueError] を作る。
func invalidValue(id, line int, why reason.Reason) *InvalidValueError {
	return &InvalidValueError{ID: id, Line: line, Reason: why.Text, Cause: why}
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
