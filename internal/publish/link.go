package publish

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// resolveLink は、path がシンボリックリンクなら、たどった先の実体のパスを返す。
// リンクでなければ path をそのまま返す。
//
// 最後の要素がリンクのときだけたどる。途中のディレクトリがリンクやジャンクション
// でも、一時ファイルと出力先は同じ実体のフォルダーに入るので、rename は
// そのまま通る（ロケールのフォルダーをリンクで置く形は、もとから壊れない）。
// 余計にたどらないのは、パスの綴りを変えないためでもある。
//
// リンク先が無いときは誤りを返す。書けば、リンクが普通のファイルに置き換わるか、
// どこか別の場所にファイルができる。どちらも、リンクを置いた人の意図と違う。
//
// path が無いときや調べられないときは、path をそのまま返す。無ければ新しく作り、
// 調べられない理由があれば、このあとの書き込みが同じ理由で失敗して報せる。
func resolveLink(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return path, nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%s はシンボリックリンクですが、リンク先をたどれません: %w", path, err)
	}
	return resolved, nil
}

// overwrite は path の中身を、その場で out に書き直す。
//
// ハードリンクのあるファイルのためにある。rename で置き換えると、書いた名前だけが
// 新しいファイルになり、ほかの名前は古い中身のまま残る。
//
// touched は、path を切り詰めたかどうかである。開けずに失敗したときは偽で、
// 中身は元のまま残っている。真で誤りが返ったときは、中身が途中までになっている。
func overwrite(path string, out []byte) (touched bool, err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return false, err
	}
	if _, err := f.Write(out); err != nil {
		f.Close()
		return true, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return true, err
	}
	return true, f.Close()
}
