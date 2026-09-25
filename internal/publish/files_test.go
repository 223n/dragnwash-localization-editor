package publish

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeFiles は dir の下に files（相対パス → 中身）を書く。
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestReadFiles は、publish が組み立てと確かめに使うファイルを1回ずつ読むことと、
// 読めないときにどのファイルかを添えた誤りを返すことを見る。
func TestReadFiles(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"in.csv":   "入力",
		"out.csv":  "書き出し先",
		"game.csv": "ゲーム側",
	})
	in := filepath.Join(dir, "in.csv")
	out := filepath.Join(dir, "out.csv")
	game := filepath.Join(dir, "game.csv")
	missing := filepath.Join(dir, "無い.csv")

	t.Run("3つとも読む", func(t *testing.T) {
		f, err := ReadFiles(Target{Input: in, Output: out, GameBase: game})
		if err != nil {
			t.Fatal(err)
		}
		if string(f.Input) != "入力" || string(f.Output) != "書き出し先" || string(f.GameBase) != "ゲーム側" {
			t.Errorf("読んだ中身 = %q / %q / %q", f.Input, f.Output, f.GameBase)
		}
	})
	t.Run("書き出し先が無ければゲーム側も読まない", func(t *testing.T) {
		// 土台の確かめも形の確かめも、書き出し先が無ければゲーム側を見ない。
		f, err := ReadFiles(Target{Input: in, Output: missing, GameBase: game})
		if err != nil {
			t.Fatal(err)
		}
		if f.Output != nil || f.GameBase != nil || string(f.Input) != "入力" {
			t.Errorf("読んだ中身 = %+v", f)
		}
	})
	t.Run("入力と書き出し先が同じファイル", func(t *testing.T) {
		f, err := ReadFiles(Target{Input: out, Output: filepath.Join(dir, ".", "out.csv")})
		if err != nil {
			t.Fatal(err)
		}
		if string(f.Input) != "書き出し先" || string(f.Output) != "書き出し先" || f.GameBase != nil {
			t.Errorf("読んだ中身 = %+v", f)
		}
	})

	for _, tc := range []struct {
		name string
		t    Target
		path string
	}{
		{"入力が無い", Target{Input: missing, Output: out}, missing},
		{"入力と同じ書き出し先が無い", Target{Input: missing, Output: missing}, missing},
		{"書き出し先がフォルダー", Target{Input: in, Output: dir}, dir},
		{"ゲーム側がフォルダー", Target{Input: in, Output: out, GameBase: dir}, dir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadFiles(tc.t)
			var shapeErr *ShapeError
			if !errors.As(err, &shapeErr) || shapeErr.Path != tc.path {
				t.Fatalf("ReadFiles = %v、%s を指す *ShapeError を期待", err, tc.path)
			}
		})
	}
	t.Run("無い入力は fs.ErrNotExist", func(t *testing.T) {
		if _, err := ReadFiles(Target{Input: missing, Output: missing}); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("ReadFiles = %v、fs.ErrNotExist を期待", err)
		}
	})
}

// TestChangedPaths は、読んだあとに中身か有無が変わったファイルだけを返すことを見る。
func TestChangedPaths(t *testing.T) {
	split := Target{Input: "in.csv", Output: "out.csv", GameBase: "game.csv"}
	same := Target{Input: "out.csv", Output: "out.csv"}
	before := Files{Input: []byte("a"), Output: []byte("b"), GameBase: []byte("c")}

	for _, tc := range []struct {
		name  string
		t     Target
		after Files
		want  []string
	}{
		{"変わらない", split, before, nil},
		{"入力", split, Files{Input: []byte("A"), Output: []byte("b"), GameBase: []byte("c")}, []string{"in.csv"}},
		{"書き出し先", split, Files{Input: []byte("a"), Output: []byte("B"), GameBase: []byte("c")}, []string{"out.csv"}},
		{"ゲーム側", split, Files{Input: []byte("a"), Output: []byte("b"), GameBase: []byte("C")}, []string{"game.csv"}},
		{"書き出し先が消えた", split, Files{Input: []byte("a"), GameBase: []byte("c")}, []string{"out.csv"}},
		{"3つとも", split, Files{Input: []byte("A"), Output: []byte("B")}, []string{"in.csv", "out.csv", "game.csv"}},
		{"同じファイルは1つとして言う", same, Files{Input: []byte("B"), Output: []byte("B")}, []string{"out.csv"}},
		{"ゲーム側の無い対象は見ない", Target{Input: "in.csv", Output: "out.csv"},
			Files{Input: []byte("a"), Output: []byte("b")}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ChangedPaths(tc.t, before, tc.after); !slices.Equal(got, tc.want) {
				t.Errorf("ChangedPaths = %q、%q を期待", got, tc.want)
			}
		})
	}

	// 空のファイルが作られた（無かった → 中身が空）ことも、変わったとみなす。
	created := ChangedPaths(split, Files{Input: []byte("a")}, Files{Input: []byte("a"), Output: []byte{}})
	if !slices.Equal(created, []string{"out.csv"}) {
		t.Errorf("空のファイルが作られたのに ChangedPaths = %q", created)
	}
}

// TestCheckBaseBytesWithoutGameBase は、ゲーム側の公開ファイルが無い（nil）とき、
// そろっているともいないとも言わないことを見る。
func TestCheckBaseBytesWithoutGameBase(t *testing.T) {
	res, err := CheckBaseBytes("ja", []byte(HeaderLine+"\n"), nil)
	if err != nil || res.Count != 0 || res.Locale != "ja" {
		t.Errorf("CheckBaseBytes = %+v, %v", res, err)
	}
}
