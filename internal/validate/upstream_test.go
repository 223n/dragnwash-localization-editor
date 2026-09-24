package validate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// upstreamCheckerEnv は上流の tools/check-translations.py の場所を渡す環境変数。
//
// 設定されていて Python が見つかれば、表の入力ごとに上流を実際に走らせ、
// 報告が表の upstream（無ければ want）と一致するかも確かめる。設定されていなければ
// dwloc の側だけを確かめる。CI には上流のリポジトリも Python も無いので、
// 飛ばせることが必須。上流の PR の CI は dev の版で走るので、dev の版を渡す。
//
//	git -C <上流> show upstream/dev:tools/check-translations.py > check-translations.py
//	DWLOC_UPSTREAM_CHECKER=$PWD/check-translations.py go test ./internal/validate -run Upstream
const upstreamCheckerEnv = "DWLOC_UPSTREAM_CHECKER"

// upstreamPythonEnv は上流を走らせる Python の実行ファイル。無ければ python3、
// python の順に探す。
const upstreamPythonEnv = "DWLOC_PYTHON"

// 見本の行。値はすべて架空のもので、ゲームの台本は使わない。
const (
	upHeader = "key,section,node,order,speaker,translation\n"
	upRowA   = keyA + ",UI,,,UI,あ\n"
	upKeyB   = "fedcba9876543210"
	upPath   = "Translations/xx/strings.csv"
)

// upstreamCase は上流と突き合わせる入力1つ。
type upstreamCase struct {
	name string
	// files はルート相対のパスと中身。
	files map[string]string
	// want は dwloc の報告（Problem.String() の並び）。nil なら translations OK。
	want []string
	// why は、上流の不具合を写さないと決めた入力にだけ書く理由。空でなければ、
	// 上流は want ではなく upstream を出す（nil なら translations OK）。
	why      string
	upstream []string
	// wantErr が空でなければ、dwloc は報告ではなくこの文字列を含むエラーを返す。
	// 上流はそこで異常終了するので、upstreamCrash を標準エラーに含めて終わること。
	wantErr       string
	upstreamCrash string
}

// upstreamCases は、上流の調査（f816618、c8fda90、912f519）で dwloc と
// 判定が割れた入力と、その周りの境目。
func upstreamCases() []upstreamCase {
	long := strings.Repeat
	withStrings := func(files map[string]string) map[string]string {
		files[upPath] = upHeader + upRowA
		return files
	}
	credits := func(body string) map[string]string {
		return withStrings(map[string]string{"Translations/xx/credits.txt": body})
	}
	const (
		notStatus  = `" is not a status; use one of supervised, proofread, converted, provisional, fun`
		parseError = "could not be parsed as CSV (field larger than field limit (131072))"
	)

	return []upstreamCase{
		// #1（f816618）: 空白だけの行を落とさない。
		{
			name:  "空白だけの行は1フィールドのレコード",
			files: map[string]string{upPath: upHeader + upRowA + "   \n" + upKeyB + ",UI,,,UI,い\n"},
			want:  []string{upPath + ":3: expected 6 fields, got 1"},
		},
		{
			name:  "タブだけの行も同じ",
			files: map[string]string{upPath: upHeader + upRowA + "\t\n"},
			want:  []string{upPath + ":3: expected 6 fields, got 1"},
		},
		{
			name:  "ヘッダーの上の空白だけの行がヘッダーになる",
			files: map[string]string{upPath: "   \n" + upHeader + upRowA},
			want:  []string{upPath + ": header is ['   ']" + headerSuffix},
		},
		{
			name:  "完全な空行だけならempty file",
			files: map[string]string{upPath: "\n\r\n\n"},
			want:  []string{upPath + ": empty file"},
		},
		{
			name:  "ヘッダーの前後の完全な空行は飛ばす",
			files: map[string]string{upPath: "\n\n" + upHeader + "\n" + upRowA + "\n"},
		},

		// #2（f816618）: フィールドの長さの上限。ほかの検査は止める。
		{
			name:  "131073文字のフィールド",
			files: map[string]string{upPath: upHeader + keyA + ",UI,,,UI," + long("x", 131073) + "\n" + upKeyB + ",UI,,,UI,\n"},
			want:  []string{upPath + ":2: " + parseError},
		},
		{
			name:  "131072文字のフィールドは通る",
			files: map[string]string{upPath: upHeader + keyA + ",UI,,,UI," + long("x", 131072) + "\n"},
		},
		{
			name:  "上限は文字数で数える",
			files: map[string]string{upPath: upHeader + keyA + ",UI,,,UI," + long("あ", 131073) + "\n"},
			want:  []string{upPath + ":2: " + parseError},
		},
		{
			name:  "多バイト文字でちょうど上限なら通る",
			files: map[string]string{upPath: upHeader + keyA + ",UI,,,UI," + long("あ", 131072) + "\n"},
		},
		{
			name:  "複数行の引用値は超えた行を指す",
			files: map[string]string{upPath: upHeader + keyA + ",UI,,,UI,\"a\n" + long("x", 131071) + "\nb\"\n"},
			want:  []string{upPath + ":3: " + parseError},
		},
		{
			name:  "長すぎるコメント行",
			files: map[string]string{upPath: upHeader + "# " + long("x", 131073) + "\n" + upRowA},
			want:  []string{upPath + ":2: " + parseError},
		},
		{
			// Python 3.11 から NUL で csv.Error にならない。上流の CI は 3.12。
			name:  "NULはただの文字",
			files: map[string]string{upPath: upHeader + keyA + ",UI,,,UI,a\x00b\n"},
		},

		// #3（c8fda90）: コメントかどうかを偶奇で決める。
		{
			name:  "引用値の途中の'#'行は値の一部",
			files: map[string]string{upPath: upHeader + keyA + ",UI,,,UI,\"ひとつ\n#ふたつ\"\n" + upKeyB + ",UI,,,UI,\n"},
			want:  []string{upPath + ":4: empty translation"},
		},
		{
			name: "引用値の途中の'#'行はCRLFでも値の一部",
			files: map[string]string{upPath: strings.ReplaceAll(
				upHeader+keyA+",UI,,,UI,\"ひとつ\n#ふたつ\"\n"+upKeyB+",UI,,,UI,\n", "\n", "\r\n")},
			want: []string{upPath + ":4: empty translation"},
		},
		{
			name:  "引用値の途中の空行も値の一部",
			files: map[string]string{upPath: upHeader + keyA + ",UI,,,UI,\"ひとつ\n\nふたつ\"\n" + upKeyB + ",UI,,,UI,い\n"},
		},
		{
			name:  "引用符で囲んだ'#'始まりのキーはコメントではない",
			files: map[string]string{upPath: upHeader + "\"#1 路地\",UI,,,UI,あ\n"},
			want:  []string{upPath + ":2: key is not 16 lowercase hex digits or a line ID"},
		},
		{
			// #4b: 偶奇は裸の '"' も数えるので、後ろの '#' 行がデータになる。
			name:  "裸の引用符のあとの'#'行はデータ",
			files: map[string]string{upPath: upHeader + keyA + ",UI,,,UI,5\" 画面\n\n# ===== 見出し =====\n" + upKeyB + ",UI,,,UI,い\n"},
			want:  []string{upPath + ":4: expected 6 fields, got 1"},
		},
		{
			// 偶奇の上ではコメントでも、パーサーが引用値を読んでいる途中なら値に入る。
			name:  "引用値の途中に来た偶奇上のコメント行",
			files: map[string]string{upPath: upHeader + keyA + ",x\"y,UI,,UI,\"c\n#z\"\n" + upKeyB + ",UI,,,UI,\n"},
			want: []string{
				upPath + ":2: section does not look like an identifier",
				upPath + ":4: empty translation",
			},
		},
		{
			name:  "引用符が閉じているコメント行",
			files: map[string]string{upPath: upHeader + "# a,\"b,c\",d\n" + keyA + ",UI,,,UI,\n"},
			want:  []string{upPath + ":3: empty translation"},
		},
		{
			name:  "CRだけの改行",
			files: map[string]string{upPath: strings.ReplaceAll(upHeader+"# メモ\n"+upRowA+upKeyB+",UI,,,UI,\n", "\n", "\r")},
			want:  []string{upPath + ":4: empty translation"},
		},

		// 上流の不具合を写さない入力。
		{
			name:  "コメント行の引用符はレコードを開かない",
			files: map[string]string{upPath: upHeader + "# メモ,\"開いたまま\n" + keyA + ",UI,,,UI,\n" + upKeyB + ",UI,,,UI,い\n"},
			want:  []string{upPath + ":3: empty translation"},
			why: "上流はコメント行も csv.reader に通すので、開いた引用符が後ろの行を" +
				"コメントのレコードに飲み込み、空の訳を見逃す。ゲームはコメント行をレコードにしない",
			upstream: nil,
		},
		{
			// 4行目の裸の '"' が、上流ではコメント行の開いた引用を閉じる。
			name: "コメント行が開いた引用が後ろで閉じる",
			files: map[string]string{upPath: upHeader + "# メモ,\"開いたまま\n" + keyA + ",UI,,,UI,\n" +
				upKeyB + ",UI,,,UI,5\" 画面\n" + keyA + ",UI,,,UI,い\n"},
			want: []string{
				upPath + ":3: empty translation",
				upPath + ":5: duplicate key (see line 3)",
			},
			why:      "同上。閉じるまでの行がまとめて検査から漏れる",
			upstream: nil,
		},
		{
			// #4a: 上流の comment_lines は splitlines() で行を数えるので、U+2028 で
			// 行番号がずれる。
			name:     "値の中のU+2028で行番号をずらさない",
			files:    map[string]string{upPath: upHeader + keyA + ",UI,,,UI,あ\u2028い\n# --- 見出し ---\n" + upKeyB + ",UI,,,UI,う\n"},
			want:     nil,
			why:      "上流はコメント行をデータとして誤報する",
			upstream: []string{upPath + ":3: expected 6 fields, got 1"},
		},
		{
			name:     "値の中のU+2028のあとの本物の問題を見逃さない",
			files:    map[string]string{upPath: upHeader + keyA + ",UI,,,UI,あ\u2028い\n# --- 見出し ---\n" + upKeyB + ",UI,,,UI,\n"},
			want:     []string{upPath + ":4: empty translation"},
			why:      "上流は誤報を出し、4行目をコメントと取り違えて空の訳を見逃す",
			upstream: []string{upPath + ":3: expected 6 fields, got 1"},
		},

		// credits.txt（912f519）。
		{
			name:  "credits.txtの状態語が違う",
			files: credits("done\n名前\n"),
			want:  []string{"Translations/xx/credits.txt:1: \"done" + notStatus},
		},
		{
			name:  "credits.txtがコメントと空行だけ",
			files: credits("# メモ\n\n  \n"),
			want: []string{"Translations/xx/credits.txt: empty; the first line is the status " +
				"(supervised, proofread, converted, provisional, fun)"},
		},
		{
			name:  "credits.txtは前後の空白と大文字小文字を問わない",
			files: credits("\ufeff# メモ\n  Supervised  \n名前\n"),
		},
		{
			name:  "credits.txtの空白始まりの'#'もコメント",
			files: credits("\n  # メモ\nnope\n"),
			want:  []string{"Translations/xx/credits.txt:3: \"nope" + notStatus},
		},
		{
			// Python の lower() は 'İ' を2文字にするので状態語にならない。
			name:  "credits.txtの大文字小文字はASCIIだけ",
			files: credits("prov\u0130sional\n"),
			want:  []string{"Translations/xx/credits.txt:1: \"prov\u0130sional" + notStatus},
		},
		{
			name:  "credits.txtはCRでも行を分ける",
			files: credits("\r\rnope\r"),
			want:  []string{"Translations/xx/credits.txt:3: \"nope" + notStatus},
		},
		{
			name: "strings.csvが無くてもcredits.txtを見る",
			files: map[string]string{
				"Translations/xx/credits.txt": "done\n",
			},
			want: []string{
				"Translations/xx: no strings.csv",
				"Translations/xx/credits.txt:1: \"done" + notStatus,
			},
		},
	}
}

// TestUpstreamCases は、上流 dev と判定が割れていた入力で dwloc の報告を確かめる。
// 上流の場所が渡されていれば、上流を実際に走らせた結果とも突き合わせる。
func TestUpstreamCases(t *testing.T) {
	checker, python := upstreamChecker(t)

	for _, tt := range upstreamCases() {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for path, content := range tt.files {
				writeFile(t, filepath.Join(root, filepath.FromSlash(path)), content)
			}

			got, err := CheckTree(root, trackedSet())
			switch {
			case tt.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("エラー = %v, want %q を含む（報告 = %#v）", err, tt.wantErr, problemStrings(got))
				}
			case err != nil:
				t.Fatalf("CheckTree が失敗した: %v", err)
			default:
				if lines := problemStrings(got); !slices.Equal(lines, tt.want) {
					t.Errorf("dwloc の報告が違う\n got = %#v\nwant = %#v", lines, tt.want)
				}
			}

			if checker == "" {
				return
			}
			stdout, stderr, code := runUpstream(t, python, checker, root)
			if tt.upstreamCrash != "" {
				if code == 0 || !strings.Contains(stderr, tt.upstreamCrash) {
					t.Errorf("上流が異常終了しなかった（終了コード %d）\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
				}
				return
			}
			want := tt.want
			if tt.why != "" {
				want = tt.upstream
			}
			if expected := reportOf(want); stdout != expected {
				t.Errorf("上流の報告が表と違う（%s）\n got:\n%s\nwant:\n%s\nstderr:\n%s", tt.why, stdout, expected, stderr)
			}
		})
	}
}

// upstreamChecker は上流のスクリプトと Python の場所を返す。
// どちらかが無ければ空文字を返し、突き合わせは飛ばす。
func upstreamChecker(t *testing.T) (checker, python string) {
	t.Helper()
	checker = os.Getenv(upstreamCheckerEnv)
	if checker == "" {
		t.Logf("%s が無いので、上流との突き合わせは飛ばす", upstreamCheckerEnv)
		return "", ""
	}
	if _, err := os.Stat(checker); err != nil {
		t.Fatalf("%s=%s が読めない: %v", upstreamCheckerEnv, checker, err)
	}
	candidates := []string{"python3", "python"}
	if env := os.Getenv(upstreamPythonEnv); env != "" {
		candidates = []string{env}
	}
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return checker, path
		}
	}
	t.Fatalf("%s が指定されているのに Python が見つからない", upstreamCheckerEnv)
	return "", ""
}

// runUpstream は root/tools/ に上流のスクリプトを置いて走らせる。上流はスクリプトの
// 2階層上をリポジトリのルートにするため。
func runUpstream(t *testing.T, python, checker, root string) (stdout, stderr string, code int) {
	t.Helper()
	script, err := os.ReadFile(checker)
	if err != nil {
		t.Fatalf("%s が読めない: %v", checker, err)
	}
	path := filepath.Join(root, "tools", "check-translations.py")
	writeFile(t, path, string(script))

	cmd := exec.Command(python, path)
	cmd.Dir = root
	// Windows でも UTF-8 で出させる。既定のままだと非 ASCII の報告で
	// UnicodeEncodeError になる（移植仕様「形式検証 / 敵対検証」[low]）。
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("上流を起動できない: %v", err)
		}
		code = exitErr.ExitCode()
	}
	// Windows の Python は print の改行を CRLF にする。
	return strings.ReplaceAll(out.String(), "\r\n", "\n"), errOut.String(), code
}

// reportOf は報告の行の並びを、上流が標準出力へ書く形にする。[Report] と同じ形。
func reportOf(lines []string) string {
	if len(lines) == 0 {
		return "translations OK\n"
	}
	return strings.Join(lines, "\n") + "\n\n" + strconv.Itoa(len(lines)) + " problem(s).\n"
}
