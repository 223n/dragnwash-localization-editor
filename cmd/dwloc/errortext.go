package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/223n/dragnwash-localization-editor/internal/diff"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// errorText は誤り err を、報告に出す1行の文にします。validate・diff・publish・edit の
// どれも、ファイルを読み書きできなかったときの文はここを通します。
//
// 言い換えるのは、誤りの中のパスと、OS が返す理由です。
//
//   - パスは [displayPath] でルートからの相対にします。手元の絶対パスには利用者名が
//     入ることがあり、記録（logs/dwloc_<日付>.log）を添えた不具合の報告や CI の
//     ログから漏れます。internal の各パッケージは表示の基準になるルートを知らないので、
//     パスを持ったまま誤りを返します（*fs.PathError、*os.LinkError、diff.FileError、
//     publish.ShapeError）。前置きだけを相対にしても、%w で包んだ誤りの中のパスは
//     絶対のまま残るので、包んだ誤りを1つずつ開いて書き換えます。
//   - OS の理由（The system cannot find the path specified. など）は、よくあるもの
//     だけを日本語にします（[pathReason]）。ほかのコマンドの出力が日本語なので、
//     1回の実行の出力が2言語に割れないようにするためです。知らない理由は OS の文の
//     ままにします。理由そのものを落とすと、直し方に手が届かなくなります。
//
// 前置きがすでに同じパスを名指しているとき（「Translations が読めない: 」など）は、
// 誤りの側のパスを重ねて書かず、理由だけにします。
func errorText(root string, err error) string {
	return rewriteError(root, err, pathReason)
}

// errorTextKeepingReason は [errorText] と同じくパスを相対にしますが、OS の理由は
// 言い換えません。
//
// edit で --ui-lang に日本語以外を指定したときに使います。edit が黒い窓に出す文は
// --ui-lang の言語に従うので、理由だけを日本語にすると、英語を選んだ人の出力が
// 2言語に割れます。
func errorTextKeepingReason(root string, err error) string {
	return rewriteError(root, err, func(_, _ string, err error) string { return err.Error() })
}

// rewriteError は [errorText] の本体です。reason は OS の理由を文にする関数です。
func rewriteError(root string, err error, reason func(op, path string, err error) string) string {
	msg := err.Error()
	for _, e := range errorChain(err) {
		switch e := e.(type) {
		case *fs.PathError:
			msg = replaceAbout(msg, e.Error(), displayPath(root, e.Path), reason(e.Op, e.Path, e.Err))
		case *os.LinkError:
			// rename などの2つのパスを持つ誤り。書き換えたかった先（New）で言います。
			// Old は書き出しの途中の一時ファイルで、利用者が知っている名前ではありません。
			msg = replaceAbout(msg, e.Error(), displayPath(root, e.New), reason(e.Op, e.New, e.Err))
		case *diff.FileError:
			msg = replacePrefix(msg, filepath.ToSlash(e.Path)+": ", displayPath(root, e.Path))
		case *publish.ShapeError:
			msg = replacePrefix(msg, e.Path+": ", displayPath(root, e.Path))
		}
	}
	return msg
}

// errorf は fmt.Errorf で組んだ誤りを [errorText] で文にします。誤りに前置きを
// 付けて出すときに使います。前置きの中のパスは、呼ぶ側が displayPath で書きます。
func errorf(root, format string, args ...any) string {
	return errorText(root, fmt.Errorf(format, args...))
}

// errorChain は err と、err が包んだ誤りを、外から順に並べて返します。
// errors.Join で束ねた誤り（Unwrap() []error）も開きます。
func errorChain(err error) []error {
	var out []error
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		out = append(out, e)
		switch u := e.(type) {
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		case interface{ Unwrap() []error }:
			for _, inner := range u.Unwrap() {
				walk(inner)
			}
		}
	}
	walk(err)
	return out
}

// replaceAbout は msg の中の old（パスを持つ誤りの文）を、「shown: why」に置き換えます。
// old より前に shown がすでに出ていれば、why だけにします。old が無ければ msg のままです。
func replaceAbout(msg, old, shown, why string) string {
	i := strings.Index(msg, old)
	if i < 0 {
		return msg
	}
	repl := shown + ": " + why
	if strings.Contains(msg[:i], shown) {
		repl = why
	}
	return msg[:i] + repl + msg[i+len(old):]
}

// replacePrefix は msg の中の old（「パス: 」の形の、誤りの頭のパス）を「shown: 」に
// 置き換えます。old より前に shown がすでに出ていれば、取り除きます。
func replacePrefix(msg, old, shown string) string {
	i := strings.Index(msg, old)
	if i < 0 {
		return msg
	}
	repl := shown + ": "
	if strings.Contains(msg[:i], shown) {
		repl = ""
	}
	return msg[:i] + repl + msg[i+len(old):]
}

// pathReason は、ファイルを読み書きできなかった理由を日本語にします。
//
// op と path は、Windows でディレクトリをファイルとして読んだときを見分けるために
// 受けます。Linux と macOS は EISDIR を返しますが、Windows は
// ERROR_INVALID_FUNCTION（Incorrect function.）を返すので、理由の値だけでは
// 分かりません。読めなかったパスがディレクトリかどうかを見て決めます。
//
// Windows では、途中がファイルのパス（ファイルの下を指したパス）も「見つかりません」に
// なります（ERROR_PATH_NOT_FOUND は ErrNotExist と ENOTDIR の両方に当たるので、
// ErrNotExist を先に見ます）。
func pathReason(op, path string, err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "見つかりません"
	case errors.Is(err, fs.ErrPermission):
		return "権限がありません"
	case errors.Is(err, syscall.ENOTDIR):
		return "フォルダーではありません"
	case errors.Is(err, syscall.EISDIR), op == "read" && isDir(path):
		return "フォルダーです（ファイルを指定してください）"
	}
	return err.Error()
}

// isDir は path がディレクトリかを返します。調べられなければ false です。
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
