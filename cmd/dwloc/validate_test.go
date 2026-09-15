package main

import (
	"testing"
)

// validFile は検査を通る公開ファイル。キーは16桁の小文字16進、section は識別子、
// 訳は非空。これ以外の条件は internal/validate のテストで確かめている。
const validFile = "key,section,node,order,speaker,translation\n" +
	"e3b0c44298fc1c14,UI,,,UI,こんにちは\n"

func TestRunValidate(t *testing.T) {
	tests := []struct {
		name string
		// files はルート相対のパスと中身。
		files map[string]string
		// wantCode は期待する終了コード。
		wantCode int
		// wantStdout は完全一致で確かめたい標準出力。空なら確かめない。
		wantStdout string
		// containsStdout / containsStderr は含まれていてほしい文字列。
		containsStdout []string
		containsStderr []string
	}{
		{
			name: "問題が無ければ成功して translations OK だけを出す",
			files: map[string]string{
				"Translations/ja/strings.csv": validFile,
			},
			wantCode:   exitOK,
			wantStdout: "translations OK\n",
		},
		{
			name: "問題があれば終了コード1で報告する",
			files: map[string]string{
				// 公開ファイルに原文の列が残っている状態。ヘッダー検査に落ちる。
				"Translations/ja/strings.csv": "key,source_en,translation\nabcdef0123456789,Hello,こんにちは\n",
			},
			wantCode:       exitProblems,
			containsStdout: []string{"Translations/ja/strings.csv", "header is", "1 problem(s)."},
		},
		{
			name: "strings.csv の無いロケールも問題として数える",
			files: map[string]string{
				"Translations/ja/strings.csv": validFile,
				"Translations/de/.keep":       "",
			},
			wantCode:       exitProblems,
			containsStdout: []string{"Translations/de", "no strings.csv", "1 problem(s)."},
		},
		{
			name: "空のファイルは empty file として報告する",
			files: map[string]string{
				"Translations/ja/strings.csv": "",
			},
			wantCode:       exitProblems,
			containsStdout: []string{"empty file", "1 problem(s)."},
		},
		{
			name: "複数の問題は件数がそのまま出る",
			files: map[string]string{
				"Translations/ja/strings.csv": "key,section,node,order,speaker,translation\n" +
					"e3b0c44298fc1c14,UI,,,UI,\n" +
					"NOTAKEY,UI,,,UI,こんにちは\n",
			},
			wantCode:       exitProblems,
			containsStdout: []string{"empty translation", "key is not 16 lowercase hex digits or a line ID", "2 problem(s)."},
		},
		{
			name: "Translations が無ければ検査できないので終了コード2",
			files: map[string]string{
				"README.md": "",
			},
			wantCode:       exitError,
			containsStderr: []string{"検証できません"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := makeTree(t, tt.files)

			code, stdout, stderr := runCLI("validate", "--root", root)
			if code != tt.wantCode {
				t.Errorf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, tt.wantCode, stdout, stderr)
			}
			if tt.wantStdout != "" && stdout != tt.wantStdout {
				t.Errorf("stdout = %q, 期待 %q", stdout, tt.wantStdout)
			}
			checkContains(t, "stdout", stdout, tt.containsStdout)
			checkContains(t, "stderr", stderr, tt.containsStderr)
		})
	}
}

func TestRunValidateRootOption(t *testing.T) {
	// --root はサブコマンドの前後どちらにも置ける。後ろに書いた方が勝つ。
	root := makeTree(t, map[string]string{"Translations/ja/strings.csv": validFile})
	// empty は Translations が無いので、これがルートになると終了コード2になる。
	empty := makeTree(t, map[string]string{"README.md": ""})

	tests := []struct {
		name     string
		args     []string
		wantCode int
	}{
		{
			name:     "サブコマンドの後ろに置ける",
			args:     []string{"validate", "--root", root},
			wantCode: exitOK,
		},
		{
			name:     "サブコマンドの前に置ける",
			args:     []string{"--root", root, "validate"},
			wantCode: exitOK,
		},
		{
			name:     "両方にあれば後ろが勝つ",
			args:     []string{"--root", empty, "validate", "--root", root},
			wantCode: exitOK,
		},
		{
			name:     "後ろが誤っていれば前の指定では救われない",
			args:     []string{"--root", root, "validate", "--root", empty},
			wantCode: exitError,
		},
		{
			name:     "= で書いても同じ",
			args:     []string{"validate", "--root=" + root},
			wantCode: exitOK,
		},
		{
			name:     "ハイフン1つでも同じ",
			args:     []string{"validate", "-root", root},
			wantCode: exitOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(tt.args...)
			if code != tt.wantCode {
				t.Errorf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, tt.wantCode, stdout, stderr)
			}
		})
	}
}
