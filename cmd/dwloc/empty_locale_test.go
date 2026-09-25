package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// emptyLocaleTree は、要確認の無い ja と、ディレクトリだけの de を持つリポジトリを作る。
// de には公開ファイルも作業コピーも無い（翻訳者がディレクトリを作った直後の形）。
func emptyLocaleTree(t *testing.T) string {
	t.Helper()
	return diffTree(t, map[string]string{
		"Translations/ja/strings.csv": diffCleanJA,
		"Translations/de/.keep":       "",
	})
}

// TestRunDiffStrictCountsEmptyLocales は、diff --strict が「訳が1件もないロケール」を
// 要作業に数えて終了コードを1にすることを見る（改善の調査の cli-7）。
//
// 以前は標準エラーに「訳が1件もないロケールがあります」と出すだけで、--strict でも
// 終了コード0だった。--locale でそのロケールだけを指すと「報告するロケールが
// ありません。」を出して0で終わった。diff --strict を CI の関門にすると、訳の
// 無いロケールが通る。訳が1件も無いのは、この道具でいちばん大きい要作業である。
//
// --locale で報告から外したロケールは数えない。--strict が数えるのは、報告する
// ロケールの要作業だけである。
func TestRunDiffStrictCountsEmptyLocales(t *testing.T) {
	root := emptyLocaleTree(t)
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout []string
		wantStderr []string
		// noStderr は標準エラーに出てほしくない文字列。
		noStderr []string
	}{
		{
			name:       "--strict が無ければ 0",
			args:       nil,
			wantCode:   exitOK,
			wantStderr: []string{"訳が1件もないロケールがあります: de"},
		},
		{
			name:       "--strict では 1",
			args:       []string{"--strict"},
			wantCode:   exitProblems,
			wantStderr: []string{"訳が1件もないロケールがあります: de"},
		},
		{
			name:       "--strict と csv でも 1",
			args:       []string{"--strict", "--format", "csv"},
			wantCode:   exitProblems,
			wantStderr: []string{"訳が1件もないロケールがあります: de"},
		},
		{
			name:       "--locale で空のロケールだけを指しても 1",
			args:       []string{"--strict", "--locale", "de"},
			wantCode:   exitProblems,
			wantStdout: []string{"報告するロケールは、訳が1件もないロケール（de）だけです。"},
			wantStderr: []string{"訳が1件もないロケールがあります: de"},
		},
		{
			name:     "--locale で空のロケールを外せば数えない",
			args:     []string{"--strict", "--locale", "ja"},
			wantCode: exitOK,
			noStderr: []string{"訳が1件もないロケールがあります"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"diff", "--no-game", "--root", root}, tt.args...)
			code, stdout, stderr := runCLI(args...)
			if code != tt.wantCode {
				t.Errorf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, tt.wantCode, stdout, stderr)
			}
			checkContains(t, "stdout", stdout, tt.wantStdout)
			checkContains(t, "stderr", stderr, tt.wantStderr)
			for _, no := range tt.noStderr {
				if strings.Contains(stderr, no) {
					t.Errorf("stderr に %q がある:\n%s", no, stderr)
				}
			}
		})
	}
}

// TestPublishAndEditNameEmptyLocales は、publish と edit の --locale に、公開ファイルも
// 作業コピーも無いロケールを渡したとき、「指定したロケールがありません」ではなく、
// そのロケールに何が無いかと、作業コピーの作り方を伝えることを見る（cli-7）。
//
// ディレクトリは実在するので、「ありません」と言うと嘘になる。diff は同じロケールを
// 実在する名前として受ける。
func TestPublishAndEditNameEmptyLocales(t *testing.T) {
	root := emptyLocaleTree(t)
	for _, args := range [][]string{
		{"publish", "--no-game", "--root", root, "--locale", "de"},
		{"publish", "--no-game", "--root", root, "--locale", "ja,de"},
		{"edit", "--no-game", "--no-browser", "--root", root, "--locale", "DE"},
	} {
		t.Run(strings.Join(args[len(args)-2:], " ")+" "+args[0], func(t *testing.T) {
			code, stdout, stderr := runCLI(args...)
			if code != exitError {
				t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, exitError, stdout, stderr)
			}
			checkContains(t, "stderr", stderr, []string{
				"dwloc: de には公開ファイルも作業コピーもありません",
				"Export working copy",
			})
			if strings.Contains(stderr, "指定したロケールがありません") {
				t.Errorf("実在するロケールを無いと言っている:\n%s", stderr)
			}
		})
	}

	// 無い名前と一緒に渡したら、両方を伝える。
	code, _, stderr := runCLI("publish", "--no-game", "--root", root, "--locale", "de,fr")
	if code != exitError {
		t.Fatalf("終了コード = %d, 期待 %d\nstderr:\n%s", code, exitError, stderr)
	}
	checkContains(t, "stderr", stderr, []string{
		"dwloc: --locale に指定したロケールがありません: fr",
		"dwloc: de には公開ファイルも作業コピーもありません",
	})
}

// TestEmptyLocalesFor は、--locale を照合しないときと、Translations を読めない
// ときには、空のロケールを探さない（nil を返す）ことを見る。
func TestEmptyLocalesFor(t *testing.T) {
	root := emptyLocaleTree(t)
	if got := emptyLocalesFor(root, nil, nil); got != nil {
		t.Errorf("--locale が無いのに %q を返した", got)
	}
	if got := emptyLocalesFor(root, nil, []string{"de"}); !slices.Contains(got, "de") {
		t.Errorf("空のロケール = %q、de を期待", got)
	}
	if got := emptyLocalesFor(filepath.Join(root, "nope"), nil, []string{"de"}); got != nil {
		t.Errorf("読めないのに %q を返した", got)
	}
}
