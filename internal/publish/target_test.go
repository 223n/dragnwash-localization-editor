package publish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
