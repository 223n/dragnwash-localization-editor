package publish

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
)

// writeFile はテスト用に親ディレクトリごとファイルを作る。
func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("%s の親ディレクトリを作れない: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("%s を書けない: %v", path, err)
	}
}

func TestDiscoverTargets(t *testing.T) {
	tests := []struct {
		name string
		// files は作るファイル。キーが root からの相対パス。
		files map[string]string
		// dirs は中身の無いディレクトリ。
		dirs []string
		// want は "ロケール:入力の相対パス" の形で並べたもの。
		want []string
	}{
		{
			name: "作業コピーが無ければ公開ファイル自身が入力になる",
			files: map[string]string{
				"Translations/de/strings.csv": "key\n",
				"Translations/ja/strings.csv": "key\n",
			},
			want: []string{
				"de:Translations/de/strings.csv",
				"ja:Translations/ja/strings.csv",
			},
		},
		{
			name: "作業コピーがあればそちらを入力にする",
			files: map[string]string{
				"Translations/ja/strings.csv":             "key\n",
				"Translations/_discovered/ja.working.csv": "source_en\n",
			},
			want: []string{"ja:Translations/_discovered/ja.working.csv"},
		},
		{
			name: "公開ファイルが無くても作業コピーがあれば対象になる",
			files: map[string]string{
				"Translations/_discovered/ja.working.csv": "source_en\n",
			},
			dirs: []string{"Translations/ja"},
			want: []string{"ja:Translations/_discovered/ja.working.csv"},
		},
		{
			name:  "入力が1つも無いロケールは対象にしない",
			files: map[string]string{"Translations/ja/strings.csv": "key\n"},
			dirs:  []string{"Translations/empty"},
			want:  []string{"ja:Translations/ja/strings.csv"},
		},
		{
			name: "アンダースコアで始まるディレクトリとファイルは飛ばす",
			files: map[string]string{
				"Translations/ja/strings.csv":             "key\n",
				"Translations/_discovered/ja.working.csv": "source_en\n",
				"Translations/ignore.txt":                 "x\n",
			},
			want: []string{"ja:Translations/_discovered/ja.working.csv"},
		},
		{
			name:  "ロケールが1つも無ければ空",
			files: nil,
			dirs:  []string{"Translations"},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, TranslationsDir), 0o755); err != nil {
				t.Fatalf("Translations を作れない: %v", err)
			}
			for _, dir := range tt.dirs {
				if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
					t.Fatalf("%s を作れない: %v", dir, err)
				}
			}
			for rel, content := range tt.files {
				writeFile(t, filepath.Join(root, filepath.FromSlash(rel)), content)
			}

			targets, err := DiscoverTargets(root)
			if err != nil {
				t.Fatalf("DiscoverTargets がエラーを返した: %v", err)
			}

			var got []string
			for _, target := range targets {
				rel, err := filepath.Rel(root, target.Input)
				if err != nil {
					t.Fatalf("相対パスにできない: %v", err)
				}
				got = append(got, target.Locale+":"+filepath.ToSlash(rel))

				// 出力は常に <ロケール>/strings.csv。
				wantOutput := filepath.Join(root, TranslationsDir, target.Locale, StringsFile)
				if target.Output != wantOutput {
					t.Errorf("出力パスが違う: got %q, want %q", target.Output, wantOutput)
				}
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("対象が違う\ngot  %v\nwant %v", got, tt.want)
			}
		})
	}
}

func TestDiscoverTargetsMissingDir(t *testing.T) {
	if _, err := DiscoverTargets(t.TempDir()); err == nil {
		t.Fatal("Translations が無いのにエラーにならなかった")
	}
}

func TestLoadOrder(t *testing.T) {
	t.Run("両方のファイルを読む", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "data", "script_order.csv"),
			orderHeader+"L01 Ryan,intro,N1,1,line:aa,0000000000000001,Ryan,\n")
		writeFile(t, filepath.Join(root, "data", "level_flow.csv"),
			flowHeader+"0,Ryan,Sunny,,\n")

		data, err := LoadOrder(root)
		if err != nil {
			t.Fatalf("LoadOrder がエラーを返した: %v", err)
		}
		if len(data.Entries) != 1 {
			t.Fatalf("エントリ数が違う: %d", len(data.Entries))
		}
		if got := data.SectionTitle("L01 Ryan"); got != "Level 1: Ryan (Sunny)" {
			t.Errorf("セクション見出しが違う: %q", got)
		}
		if data.Source == "" {
			t.Error("Source が設定されていない")
		}
	})

	t.Run("ファイルが無くてもエラーにしない", func(t *testing.T) {
		data, err := LoadOrder(t.TempDir())
		if err != nil {
			t.Fatalf("LoadOrder がエラーを返した: %v", err)
		}
		if len(data.Entries) != 0 || len(data.Levels) != 0 {
			t.Errorf("空にならなかった: %d entries, %d levels", len(data.Entries), len(data.Levels))
		}
	})

	t.Run("BOM 付きの level_flow.csv を読める", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "data", "level_flow.csv"),
			"\xef\xbb\xbf"+flowHeader+"0,Ryan,,,\n")

		data, err := LoadOrder(root)
		if err != nil {
			t.Fatalf("LoadOrder がエラーを返した: %v", err)
		}
		if got := data.SectionTitle("L01 Ryan"); got != "Level 1: Ryan" {
			t.Errorf("BOM を剥がせていない: %q", got)
		}
	})
}

func TestWriteTarget(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "data", "script_order.csv"),
		orderHeader+"S1,,N1,1,,0000000000000001,Ryan,\n")
	writeFile(t, filepath.Join(root, TranslationsDir, "ja", StringsFile),
		"key,section,node,order,speaker,translation\n"+
			"# Language: 日本語 (ja)\n"+
			"\n"+
			"# ===== S1 =====\n"+
			"# --- N1 ---\n"+
			"0000000000000001,S1,N1,1,Ryan,あ\n")

	data, err := LoadOrder(root)
	if err != nil {
		t.Fatalf("LoadOrder がエラーを返した: %v", err)
	}
	targets, err := DiscoverTargets(root)
	if err != nil {
		t.Fatalf("DiscoverTargets がエラーを返した: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("対象が1件でない: %d件", len(targets))
	}

	before, err := os.ReadFile(targets[0].Output)
	if err != nil {
		t.Fatalf("書き出し前のファイルを読めない: %v", err)
	}

	stats, err := WriteTarget(data, targets[0])
	if err != nil {
		t.Fatalf("WriteTarget がエラーを返した: %v", err)
	}
	if wantStats := (Stats{Kept: 1, InPlayOrder: 1}); stats != wantStats {
		t.Errorf("集計が違う\ngot  %+v\nwant %+v", stats, wantStats)
	}

	after, err := os.ReadFile(targets[0].Output)
	if err != nil {
		t.Fatalf("書き出し後のファイルを読めない: %v", err)
	}
	// 入力＝出力の既定モードでは、公開ファイルを通しても内容が変わらない。
	if string(before) != string(after) {
		t.Errorf("入出力が同じファイルなのに内容が変わった\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

func TestWriteTargetCreatesMissingDir(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "in.csv")
	writeFile(t, input, "key,translation\n0000000000000001,あ\n")

	target := Target{Locale: "ja", Input: input, Output: filepath.Join(root, "out", "ja", StringsFile)}
	if _, err := WriteTarget(nil, target); err != nil {
		t.Fatalf("WriteTarget がエラーを返した: %v", err)
	}

	got, err := os.ReadFile(target.Output)
	if err != nil {
		t.Fatalf("書き出したファイルを読めない: %v", err)
	}
	want := "key,section,node,order,speaker,translation\n0000000000000001,,,,UI,あ\n"
	if string(got) != want {
		t.Errorf("出力が違う\ngot  %q\nwant %q", got, want)
	}
}

// TestLoadOrderFailsOnUnreadableFiles は、再生順のファイルがあるのに読めない
// ときに誤りを返すことを見る。
//
// 空として扱ってよいのは「無い」ときだけである。読めないのに空として続けると、
// 見出しが1つも出ず、全ての行が末尾の UI 見出しの下へ回った公開ファイルを
// 書き出す。訳は消えないので [CheckLoss] は止めない。並びだけが静かに壊れる。
func TestLoadOrderFailsOnUnreadableFiles(t *testing.T) {
	orderPath := filepath.Join("data", "script_order.csv")
	flowPath := filepath.Join("data", "level_flow.csv")

	tests := []struct {
		name string
		// dir はディレクトリとして作るパス。files は書くファイル。どちらも root からの相対。
		dir   string
		files map[string]string
		// wantDup は列名の重複として返ることを期待するか。
		wantDup bool
	}{
		{
			name: "script_order.csv の場所がディレクトリ",
			dir:  orderPath,
		},
		{
			name:  "level_flow.csv の場所がディレクトリ",
			dir:   flowPath,
			files: map[string]string{orderPath: orderHeader},
		},
		{
			// 読み手は2行に満たないファイルを空として扱うので、データ行を1本置く。
			name: "script_order.csv の列名が重複している",
			files: map[string]string{
				orderPath: "section,Section,node,order,line_id,key,speaker,condition\n" +
					"L01 Ryan,intro,N1,1,line:aa,0000000000000001,Ryan,\n",
			},
			wantDup: true,
		},
		{
			name: "level_flow.csv の列名が重複している",
			files: map[string]string{
				orderPath: orderHeader,
				flowPath:  "level,Level,weather,set_flags,end_flags\n0,Ryan,Sunny,,\n",
			},
			wantDup: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.dir != "" {
				mustMkdir(t, filepath.Join(root, tt.dir))
			}
			for rel, content := range tt.files {
				writeFile(t, filepath.Join(root, rel), content)
			}

			data, err := LoadOrder(root)
			if err == nil {
				t.Fatalf("誤りを返していない: %+v", data)
			}
			if data != nil {
				t.Errorf("誤りと一緒に再生順を返している: %+v", data)
			}
			var dup *csvfile.DuplicateColumnError
			if got := errors.As(err, &dup); got != tt.wantDup {
				t.Errorf("列名の重複として返ったか = %v、期待 %v（err = %v）", got, tt.wantDup, err)
			}
		})
	}
}

// TestBuildTargetFailsOnUnreadableFiles は、入力か出力先が読めないときに
// 組み立てずに誤りを返すことを見る。
//
// 出力先はヘッダー直下のコメント（# Language: ...）を写すためだけに読む（R23）。
// それでも「無い」と「読めない」は分ける。読めないのに無いとして続けると、
// コメントの消えた公開ファイルを組み立て、それが書き出される。
func TestBuildTargetFailsOnUnreadableFiles(t *testing.T) {
	t.Run("入力が無い", func(t *testing.T) {
		root := t.TempDir()
		target := Target{
			Locale: "ja",
			Input:  filepath.Join(root, "無い.csv"),
			Output: filepath.Join(root, StringsFile),
		}
		out, _, err := BuildTarget(nil, target)
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("err = %v, want fs.ErrNotExist", err)
		}
		if out != nil {
			t.Errorf("誤りと一緒に中身を返している: %q", out)
		}
	})

	t.Run("出力先がディレクトリ", func(t *testing.T) {
		root := t.TempDir()
		input := filepath.Join(root, "in.csv")
		writeFile(t, input, "key,translation\n0000000000000001,あ\n")
		output := filepath.Join(root, "ja")
		mustMkdir(t, output)

		out, _, err := BuildTarget(nil, Target{Locale: "ja", Input: input, Output: output})
		if err == nil {
			t.Fatalf("誤りを返していない: %q", out)
		}
		if out != nil {
			t.Errorf("誤りと一緒に中身を返している: %q", out)
		}
	})
}

// TestWriteTargetKeepsOutputOnFailure は、組み立てか書き出しに失敗したとき、
// いまの公開ファイルを1バイトも変えないことを見る。
//
// 既定では入力と出力が同じファイルである。途中で失敗して中途半端なものが残ると、
// それはコミット済みの訳を失うことを意味する。
func TestWriteTargetKeepsOutputOnFailure(t *testing.T) {
	const published = "key,section,node,order,speaker,translation\n" +
		"# Language: 日本語 (ja)\n" +
		"0000000000000001,UI,,,UI,残っていてほしい訳\n"

	t.Run("入力の列名が重複している", func(t *testing.T) {
		root := t.TempDir()
		input := filepath.Join(root, "in.csv")
		writeFile(t, input, "key,Key,translation\n0000000000000001,,新しい訳\n")
		output := filepath.Join(root, TranslationsDir, "ja", StringsFile)
		writeFile(t, output, published)

		stats, err := WriteTarget(nil, Target{Locale: "ja", Input: input, Output: output})
		var dup *csvfile.DuplicateColumnError
		if !errors.As(err, &dup) {
			t.Fatalf("err = %v, want *csvfile.DuplicateColumnError", err)
		}
		if stats != (Stats{}) {
			t.Errorf("書いていないのに集計を返した: %+v", stats)
		}
		if got := readString(t, output); got != published {
			t.Errorf("公開ファイルが変わった\ngot  %q\nwant %q", got, published)
		}
	})

	t.Run("出力先の親がファイル", func(t *testing.T) {
		// どこで止まるかは OS で違う。Linux は出力先を読む段で ENOTDIR になり、
		// Windows は「無い」と読めたあと、親を作る段で止まる。どちらでも、
		// 書かずに誤りを返すことだけを見る。
		root := t.TempDir()
		input := filepath.Join(root, "in.csv")
		writeFile(t, input, published)
		// ディレクトリを置くはずの場所にファイルがある。
		blocker := filepath.Join(root, "ja")
		writeFile(t, blocker, "ディレクトリではない\n")

		stats, err := WriteTarget(nil, Target{
			Locale: "ja", Input: input, Output: filepath.Join(blocker, StringsFile),
		})
		if err == nil {
			t.Fatal("誤りを返していない")
		}
		if stats != (Stats{}) {
			t.Errorf("書いていないのに集計を返した: %+v", stats)
		}
		if got := readString(t, blocker); got != "ディレクトリではない\n" {
			t.Errorf("行く手にあったファイルが変わった: %q", got)
		}
	})

	t.Run("公開ファイルは読めるが置き場に書けない", func(t *testing.T) {
		// 組み立てまでは通り、書き出しの段で止まる形。
		root := t.TempDir()
		input := filepath.Join(root, "in.csv")
		writeFile(t, input, "key,translation\n0000000000000001,新しい訳\n")
		dir := filepath.Join(root, TranslationsDir, "ja")
		output := filepath.Join(dir, StringsFile)
		writeFile(t, output, published)
		forbidNewFiles(t, dir)

		stats, err := WriteTarget(nil, Target{Locale: "ja", Input: input, Output: output})
		if err == nil {
			// 置き場に新しいファイルを作れないことは確かめてある。それでも通るのは、
			// 一時ファイルを経ずに公開ファイルを直接書き換えたときだけである。
			t.Fatalf("新しいファイルを作れない置き場で書き出しが通った。一時ファイルを経ていない（公開ファイル: %q）",
				readString(t, output))
		}
		if stats != (Stats{}) {
			t.Errorf("書けなかったのに集計を返した: %+v", stats)
		}
		if got := readString(t, output); got != published {
			t.Errorf("公開ファイルが変わった\ngot  %q\nwant %q", got, published)
		}
	})
}

// TestWriteBytesFailsWithoutTouchingTheOriginal は、書けないときに元のファイルを
// 残し、一時ファイルも残さないことを見る。
//
// 一時ファイルは出力先と同じディレクトリに作る。残ると Translations/<ロケール>/ に
// strings.csv.tmp… が溜まり、利用者はそれをコミットしかねない。
func TestWriteBytesFailsWithoutTouchingTheOriginal(t *testing.T) {
	t.Run("親を作れない", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, "ja")
		writeFile(t, blocker, "ディレクトリではない\n")

		if err := WriteBytes(filepath.Join(blocker, StringsFile), []byte("新しい内容\n")); err == nil {
			t.Fatal("誤りを返していない")
		}
		if got := readString(t, blocker); got != "ディレクトリではない\n" {
			t.Errorf("行く手にあったファイルが変わった: %q", got)
		}
	})

	t.Run("一時ファイルを作れない", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, StringsFile)
		writeFile(t, path, "古い内容\n")
		forbidNewFiles(t, dir)

		if err := WriteBytes(path, []byte("新しい内容\n")); err == nil {
			// 置き場に新しいファイルを作れないことは確かめてある。それでも通るのは、
			// 一時ファイルを経ずに出力先を直接書き換えたときだけである。
			t.Fatalf("新しいファイルを作れない置き場で書き出しが通った。一時ファイルを経ていない（中身: %q）",
				readString(t, path))
		}
		if got := readString(t, path); got != "古い内容\n" {
			t.Errorf("書けなかったのに中身が変わった: %q", got)
		}
		assertOnlyEntries(t, dir, StringsFile)
	})

	t.Run("置き換えに失敗しても一時ファイルを残さない", func(t *testing.T) {
		// 出力先がディレクトリなら rename は必ず失敗する。一時ファイルは
		// 書き終えたあとなので、消し忘れればそのまま残る。
		dir := t.TempDir()
		target := filepath.Join(dir, StringsFile)
		mustMkdir(t, target)

		if err := WriteBytes(target, []byte("新しい内容\n")); err == nil {
			t.Fatal("誤りを返していない")
		}
		assertOnlyEntries(t, dir, StringsFile)
	})
}

// forbidNewFiles は dir に新しいファイルを作れない状態にする。作れないことを
// 確かめられなければ、呼んだ試験を飛ばす。
//
// ディレクトリの書き込み権で止めるので、読み取り専用属性でファイルの作成を
// 止めない Windows ではこの形を作れない。root で走ると権限そのものが効かない。
//
// 止められたかは、調べる対象（[WriteBytes] など）の成否ではなく、ここで実際に
// ファイルを作ってみて確かめる。0o555 の中でも既にあるファイルの上書きは通る。
// 対象の成否で代用すると、対象が一時ファイルを経ずに出力先を直接書き換える形へ
// 戻ったときも「止められなかった環境」と読んで飛ばしてしまう。go test は飛ばした
// 試験を成功として数えるので、守りたい性質が崩れても誰も気づけない。
func forbidNewFiles(t *testing.T, dir string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("Windows ではディレクトリへの書き込みを権限で止められない")
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	// 後始末で消せるように戻す。t.Cleanup は後入れ先出しなので、呼び出し側が
	// 先に作った t.TempDir の削除より先に走る。
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	probe, err := os.CreateTemp(dir, "probe*")
	if err == nil {
		_ = probe.Close()
		_ = os.Remove(probe.Name())
		t.Skip("この環境ではディレクトリへの書き込みを止められないので飛ばす。root で走っていると効かない")
	}
}

// readString はファイルを文字列で読む。読めなければその場で止める。
func readString(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s を読めない: %v", path, err)
	}
	return string(data)
}

// assertOnlyEntries は dir の中身が names だけであることを確かめる。
func assertOnlyEntries(t *testing.T, dir string, names ...string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("%s を読めない: %v", dir, err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, names) {
		t.Errorf("%s の中身が %q、期待 %q", dir, got, names)
	}
}
