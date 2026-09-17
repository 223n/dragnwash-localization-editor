package edit

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// KeyEdit はキーで行を指す書き換え1つ分。
//
// 行番号ではなくキーで指すのは、書き戻す先の行番号を呼び出し側が知らないため
// である。dwloc edit --game は、画面が並べているゲーム側の作業コピーと、
// コミットする側の公開ファイルの両方へ同じ訳を書く。2つは別々に作られた
// ファイルで、見出しの数も並びも違うので行番号は一致しない。共通するのは
// 先頭フィールド（キー）だけである。
type KeyEdit struct {
	// Key は差し替える行の先頭フィールド。空のときは何も書かない。
	Key string
	// Translation は差し替える訳。書けない値の判定は [File.SetTranslation] と同じ。
	Translation string
}

// KeyResult は [WriteByKey] の1件分の結果。並びは渡した並びのまま。
type KeyResult struct {
	// Rows は訳を差し替えた行数。0 ならそのキーの行がファイルに無い。
	Rows int
	// Why は書けなかったこと、または書いたうえでの断り。無ければ空。
	//
	// 「書けなかった」と「書いたが断りがある」を型で分けないのは、受け取る側
	// （internal/web）がどちらも同じ場所（行ごとの断り）に出すためである。
	// 分かれているのは Rows で、0 かどうかで書けたかが分かる。
	Why reason.Reason
}

// WriteByKey は path のファイルを開き、キーで行を引いて訳を差し替え、保存する。
//
// 1行も差し替えなかったときは書かない（ファイルの更新時刻も変えない）。
//
// 誤りを返すのは、ファイルを開けないとき、ヘッダーが受理できないとき
// （[ErrReadOnly]）、保存に失敗したとき（[ConflictError] を含む）である。
// そのときは1バイトも書いていない。行ごとの事情（キーが無い、複数ある）は
// 誤りではなく [KeyResult] で返す。ファイル全体を落とす話ではないからである。
//
// 版の照合は [File.Save] が中でやる。このファイルを画面が持っているわけでは
// ないので、呼び出し側から版を渡す道は用意しない。よそが同時に書いていれば
// [ErrConflict] が返り、1バイトも書かない。
func WriteByKey(path string, edits []KeyEdit) ([]KeyResult, error) {
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	if f.readOnly {
		return nil, fmt.Errorf("%w: %s", ErrReadOnly, f.readOnlyReason)
	}

	results := make([]KeyResult, len(edits))
	for i, e := range edits {
		results[i] = f.setByKey(e.Key, e.Translation)
	}
	if !f.Dirty() {
		// 1行も変わっていない。書かないのは、ファイルを見張っているゲームを
		// 無駄に起こさないためと、更新時刻だけが動くのを避けるため。
		return results, nil
	}
	if err := f.Save(); err != nil {
		return nil, err
	}
	return results, nil
}

// setByKey は key を持つデータ行すべての訳を差し替える。
//
// 「すべて」なのは、同じキーの行が2行あるときに片方だけ直すと、ファイルの中に
// 同じ原文の古い訳と新しい訳が並ぶためである。キーは原文のハッシュ
// （internal/key）なので、同じキーの行は同じ原文を指す。同じ訳になるのが
// 正しく、どちらが選ばれるかを気にしなくてよくなる。
//
// publish は同じキーの2行目以降を捨てる（先勝ち、移植仕様 R18）ので、
// 公開ファイルに同じキーが2回出るのは、ふつうは起きない。起きたときに
// 黙って先頭だけ直すと、選ばれなかったほうの古い訳が残り続ける。
func (f *File) setByKey(key, value string) KeyResult {
	if key == "" {
		// キー列が空の行。作業コピーにはありうる。引く手がかりが無いので、
		// 書かずに断る。行番号で当てにいくと、別の行の訳を消す。
		return KeyResult{Why: reason.New(reason.SaveNoKey,
			"キーが空の行は、もう一方のファイルから引けない")}
	}

	var res KeyResult
	var blocked reason.Reason
	for i := range f.lines {
		if f.lines[i].Kind != KindData || f.lines[i].Key() != key {
			continue
		}
		err := f.SetTranslation(f.lines[i].Number, value)
		if err == nil {
			res.Rows++
			continue
		}
		if blocked.Empty() {
			blocked = causeOf(err)
		}
	}

	switch {
	case res.Rows == 0 && !blocked.Empty():
		res.Why = blocked
	case res.Rows == 0:
		res.Why = reason.New(reason.SaveKeyMissing, "このキーの行が書き戻す先に無い")
	case res.Rows > 1:
		res.Why = reason.New(reason.SaveKeyDuplicated,
			fmt.Sprintf("書き戻す先に同じキーの行が%d行あり、どれも同じ訳にした", res.Rows),
			"count", strconv.Itoa(res.Rows))
	}
	return res
}

// causeOf は [File.SetTranslation] が返した誤りから理由を取り出す。
//
// 型で分けずに理由だけを返すのは、[KeyResult] が行ごとの断りを1つしか持たない
// ためである。書けなかったことは Rows が 0 であることで分かるので、
// 「なぜ書けなかったか」だけを残せばよい。
func causeOf(err error) reason.Reason {
	var notEditable *NotEditableError
	if errors.As(err, &notEditable) {
		return notEditable.Cause
	}
	var invalid *InvalidValueError
	if errors.As(err, &invalid) {
		return invalid.Cause
	}
	// 名前の付いていない誤り。文面だけを持たせて、画面はそのまま出す。
	// 空を返すと、書けなかった理由が画面から消える。
	return reason.Reason{Text: err.Error()}
}
