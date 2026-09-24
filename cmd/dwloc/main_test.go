package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/gamedir"
)

// TestMain は、この package の試験が実機の Steam を見に行かないようにする。
//
// edit は --game を省いてもゲームのフォルダーを探す（[resolveGameForEdit]）。
// 素のままにしておくと、ゲームを入れている PC では試験がそのフォルダーを読み、
// 入れていない PC では読まない。同じ試験が PC ごとに別のことを確かめる形になる。
// 探す穴（[findGame]）をここで塞いでおき、探させたい試験だけが
// [stubFindGame] で入れ替える。
//
// 塞げるのは --game を省いた道だけである。--game auto は [resolveGame] から
// gamedir.Resolve → gamedir.Find へ直に入るので、ここを差し替えても止まらない。
// いま auto を渡す試験は [TestValidateAcceptsButIgnoresGame] だけで、validate は
// ゲームを解決しないため実機を読まない。publish / diff / edit に auto の試験を
// 足すときは、この穴を先に塞ぐこと。塞がないと、ゲームを入れている PC でだけ
// 通る試験になる。
func TestMain(m *testing.M) {
	findGame = func() []gamedir.Plugin { return nil }
	os.Exit(m.Run())
}

// stubFindGame は自動検出の結果を paths に差し替える。試験が終わると元へ戻す。
//
// 実機の Steam を当てにしないのは [TestMain] と同じ理由である。見つかった
// ときの道は、一時ディレクトリに作ったフォルダーで確かめる。
func stubFindGame(t *testing.T, paths ...string) {
	t.Helper()

	was := findGame
	t.Cleanup(func() { findGame = was })
	findGame = func() []gamedir.Plugin {
		found := make([]gamedir.Plugin, 0, len(paths))
		for _, p := range paths {
			found = append(found, gamedir.Plugin{Path: p})
		}
		return found
	}
}

// runCLI は run を呼んで終了コードと出力を返す。
// テストはすべてこの入口を通す。main は os.Exit を呼ぶだけなので、
// 引数の解釈と終了コードの確認はここで足りる。
func runCLI(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// gitTestOpts は、テストで動かす git に与える設定を返す。
//
// 名前とメールは、手元の git 設定に依らないために与える。
//
// gc.auto と maintenance.auto は、commit や merge が裏で起こす
// 「git maintenance run --auto」を止めるために与える。この子プロセスは
// gc.autoDetach の既定（true）で本体から分離するため、テストの本体が
// 終わったあとも .git へ書き続けることがある。t.TempDir の後片付け
// （RemoveAll）と競ると、中身を消したあとのディレクトリへ書き戻され、
// 「directory not empty」で落ちる。CI で実際に起きた。
//
// 呼ぶたびに新しいスライスを返す。共有のスライスに append すると、
// 呼び出しごとに同じ配列を書き換えてしまう。
func gitTestOpts() []string {
	return []string{
		"-c", "user.name=dwloc test",
		"-c", "user.email=dwloc@example.invalid",
		"-c", "gc.auto=0",
		"-c", "maintenance.auto=false",
	}
}

// makeTree は一時ディレクトリに files を書き、そのルートを返す。
// キーはルートからの相対パスで、区切りは常にスラッシュで書く。
func makeTree(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("%s の親ディレクトリを作れない: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("%s を書けない: %v", rel, err)
		}
	}
	return root
}

// readFile はルート相対のファイルを読む。無ければテストを落とす。
func readFile(t *testing.T, root, rel string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("%s を読めない: %v", rel, err)
	}
	return string(data)
}

// checkContains は出力に want のすべてが含まれることを確かめる。
func checkContains(t *testing.T, label, got string, want []string) {
	t.Helper()

	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s に %q が含まれない\n--- %s ---\n%s", label, w, label, got)
		}
	}
}

func TestRunArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		// wantCode は期待する終了コード。
		wantCode int
		// wantStdout / wantStderr は含まれていてほしい文字列。
		wantStdout []string
		wantStderr []string
	}{
		{
			// 引数なしは画面を始める。翻訳リポジトリでない場所では、どこへ
			// 置けばよいかを案内して終わる。使い方は help で出す。
			name:       "引数なしで翻訳リポジトリでなければ置き場所を案内する",
			args:       nil,
			wantCode:   exitError,
			wantStderr: []string{"翻訳リポジトリではないようです", "Translations", "dwloc help"},
		},
		{
			name:       "help は使い方を標準出力へ出して成功する",
			args:       []string{"help"},
			wantCode:   exitOK,
			wantStdout: []string{"サブコマンド:", "--root"},
		},
		{
			name:       "--help も使い方を標準出力へ出す",
			args:       []string{"--help"},
			wantCode:   exitOK,
			wantStdout: []string{"サブコマンド:"},
		},
		{
			name:       "-h も使い方を標準出力へ出す",
			args:       []string{"-h"},
			wantCode:   exitOK,
			wantStdout: []string{"サブコマンド:"},
		},
		{
			name:       "知らないサブコマンドはエラーにする",
			args:       []string{"frobnicate"},
			wantCode:   exitError,
			wantStderr: []string{"知らないサブコマンドです", "frobnicate"},
		},
		{
			name:       "知らないフラグはエラーにする",
			args:       []string{"--nope"},
			wantCode:   exitError,
			wantStderr: []string{"使い方:"},
		},
		{
			name:       "version は版を1行で出す",
			args:       []string{"version"},
			wantCode:   exitOK,
			wantStdout: []string{"dwloc dev"},
		},
		{
			name:       "version は共通オプションを打たれても止まらない",
			args:       []string{"version", "--root", "."},
			wantCode:   exitOK,
			wantStdout: []string{"dwloc dev"},
		},
		{
			name:       "version の余分な引数はエラーにする",
			args:       []string{"version", "1.0"},
			wantCode:   exitError,
			wantStderr: []string{"余分な引数です"},
		},
		{
			name:       "version --help はそのサブコマンドの説明を出す",
			args:       []string{"version", "--help"},
			wantCode:   exitOK,
			wantStdout: []string{"使い方: dwloc version"},
		},
		{
			// 共通オプションは受けるが、それ以外の打ち間違いまで黙って通さない。
			name:       "version の知らないフラグはエラーにする",
			args:       []string{"version", "--nope"},
			wantCode:   exitError,
			wantStderr: []string{"-nope", "使い方: dwloc version"},
		},
		{
			name:       "publish の余分な引数はエラーにする",
			args:       []string{"publish", "ja"},
			wantCode:   exitError,
			wantStderr: []string{"余分な引数です", "ja"},
		},
		{
			name:       "validate の余分な引数はエラーにする",
			args:       []string{"validate", "ja"},
			wantCode:   exitError,
			wantStderr: []string{"余分な引数です"},
		},
		{
			name:       "validate --help はそのサブコマンドの説明を出す",
			args:       []string{"validate", "--help"},
			wantCode:   exitOK,
			wantStdout: []string{"使い方: dwloc validate", "--root"},
		},
		{
			name:       "publish --help はそのサブコマンドの説明を出す",
			args:       []string{"publish", "--help"},
			wantCode:   exitOK,
			wantStdout: []string{"使い方: dwloc publish", "--locale", "--dry-run"},
		},
		{
			name:       "--locale に空を渡すとエラーにする",
			args:       []string{"publish", "--locale", ""},
			wantCode:   exitError,
			wantStderr: []string{"ロケール名が空です"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(tt.args...)
			if code != tt.wantCode {
				t.Errorf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, tt.wantCode, stdout, stderr)
			}
			checkContains(t, "stdout", stdout, tt.wantStdout)
			checkContains(t, "stderr", stderr, tt.wantStderr)

			// 説明とエラーの出し分け。求められて出す説明は標準出力、
			// 失敗の報告は標準エラーへ出す約束になっている。
			if tt.wantCode == exitError && stdout != "" {
				t.Errorf("失敗したのに標準出力へ書いている:\n%s", stdout)
			}
		})
	}
}

func TestRunVersionUsesLinkerValue(t *testing.T) {
	// -ldflags "-X main.version=..." で差し替えられることの確認。
	// テストからは同じ変数を直接書き換えて代用する。
	saved := version
	t.Cleanup(func() { version = saved })

	version = "1.2.3"
	code, stdout, stderr := runCLI("version")
	if code != exitOK {
		t.Fatalf("終了コード = %d, 期待 %d (stderr: %s)", code, exitOK, stderr)
	}
	if stdout != "dwloc 1.2.3\n" {
		t.Errorf("stdout = %q, 期待 %q", stdout, "dwloc 1.2.3\n")
	}
}

func TestDisplayPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "ルート配下はスラッシュ区切りの相対になる",
			path: filepath.Join(root, "Translations", "ja", "strings.csv"),
			want: "Translations/ja/strings.csv",
		},
		{
			name: "ルート自身は . になる",
			path: root,
			want: ".",
		},
		{
			name: "ルートの外は渡されたパスのまま返す",
			path: filepath.Join(outside, "strings.csv"),
			want: filepath.ToSlash(filepath.Join(outside, "strings.csv")),
		},
		{
			// 外へ出たかどうかは ".." という名前の要素で決める。".." で始まるだけの
			// 名前をルートの外と取り違えると、中にあるファイルを絶対パスで出す。
			name: "名前が .. で始まるだけのディレクトリはルートの中",
			path: filepath.Join(root, "..hidden", "strings.csv"),
			want: "..hidden/strings.csv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := displayPath(root, tt.path); got != tt.want {
				t.Errorf("displayPath = %q, 期待 %q", got, tt.want)
			}
		})
	}
}

// TestDisplayPathRelativeRoot は、--root を省いたとき（"."）の見え方を確かめる。
//
// 既定の使い方はこれで、報告に出るパスもここで決まる。カレントディレクトリに
// よらず同じ文字列になること。
func TestDisplayPathRelativeRoot(t *testing.T) {
	got := displayPath(".", filepath.Join("Translations", "ja", "strings.csv"))
	if want := "Translations/ja/strings.csv"; got != want {
		t.Errorf("displayPath = %q, 期待 %q", got, want)
	}
}

// TestDisplayPathOtherVolume は、ルートと別のドライブにあるパスを確かめる。
//
// Windows では filepath.Rel が相対にできずに失敗する。そのときに空や壊れた
// 文字列を返すと、報告からどのファイルの話かが分からなくなる。渡されたままを
// スラッシュ区切りで返す。ドライブの無い OS では起きないので飛ばす。
func TestDisplayPathOtherVolume(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("ドライブ文字があるのは Windows だけ")
	}
	root := t.TempDir()
	// ルートと違うドライブ文字を選ぶ。存在しなくてよい（filepath.Abs は字面だけで決める）。
	drive := "Q:"
	if strings.EqualFold(filepath.VolumeName(root), drive) {
		drive = "R:"
	}
	path := drive + `\game\Translations\ja\strings.csv`

	if got, want := displayPath(root, path), drive+"/game/Translations/ja/strings.csv"; got != want {
		t.Errorf("displayPath = %q, 期待 %q", got, want)
	}
}

// setArgs は os.Args を差し替える。試験が終わると元へ戻す。
// mainWithRecord は引数を os.Args から読むので、run と違って引数で渡せない。
func setArgs(t *testing.T, args ...string) {
	t.Helper()

	was := os.Args
	t.Cleanup(func() { os.Args = was })
	os.Args = append([]string{"dwloc"}, args...)
}

// captureStd は os.Stdout と os.Stderr を一時ファイルへ向ける。
// 返した関数を呼ぶと元へ戻し、それまでに書かれたものを返す。
//
// mainWithRecord は画面への出力を os.Stdout と os.Stderr へ直に書く。
// 差し替えないと、試験の出力に混ざるうえ、何が出たかを確かめられない。
func captureStd(t *testing.T) func() (stdout, stderr string) {
	t.Helper()

	dir := t.TempDir()
	outFile, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	errFile, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	wasOut, wasErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, errFile

	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		os.Stdout, os.Stderr = wasOut, wasErr
		// 開いたままだと、Windows では t.TempDir の後片付けが消せない。
		outFile.Close()
		errFile.Close()
	}
	// 試験が途中で落ちても戻す。t.TempDir より後に積むので、消す前に閉じる。
	t.Cleanup(restore)

	return func() (string, string) {
		restore()
		return readFile(t, dir, "stdout"), readFile(t, dir, "stderr")
	}
}

// resetRecord は、mainWithRecord が入れた記録の行き先を試験の後で空に戻す。
//
// mainWithRecord はファイルを閉じたあとも record を空にしない（その直後に
// プロセスが終わる前提）。残ったままだと、後に走る試験の edit が閉じた記録へ
// 書きにいく。
func resetRecord(t *testing.T) {
	t.Helper()

	t.Cleanup(func() {
		record = nil
		hideFromRecord = nil
	})
}

// readLogs は dir/logs にある記録をすべて読み、ファイルの数と中身をつないだものを返す。
//
// 日付の入ったファイル名を試験の側で組み立てないのは、日付をまたいで走ったときに
// 名前が食い違うため。
func readLogs(t *testing.T, dir string) (int, string) {
	t.Helper()

	names, err := filepath.Glob(filepath.Join(dir, "logs", "dwloc_*.log"))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%s を読めない: %v", name, err)
		}
		all.Write(data)
	}
	return len(names), all.String()
}

// TestMainWithRecordWritesTheLog は、画面に出したものがカレントディレクトリの
// logs/dwloc_<日付>.log にも残ることを見る。
//
// 翻訳者に出力を貼り直してもらう代わりに、その日のファイルを添えてもらう、
// という約束の根拠である。同じ日の2回目は追記で、1回目を消さない。
func TestMainWithRecordWritesTheLog(t *testing.T) {
	resetRecord(t)
	dir := t.TempDir()
	t.Chdir(dir)
	setArgs(t, "version")

	for i := 1; i <= 2; i++ {
		read := captureStd(t)
		code := mainWithRecord()
		stdout, stderr := read()
		if code != exitOK {
			t.Fatalf("%d 回目の終了コード = %d\n%s", i, code, stderr)
		}
		if want := "dwloc " + version + "\n"; stdout != want {
			t.Errorf("%d 回目の標準出力 = %q, 期待 %q", i, stdout, want)
		}
		// 記録を始められたときは、画面に断りを出さない。
		if stderr != "" {
			t.Errorf("%d 回目の標準エラーへ何か出ている:\n%s", i, stderr)
		}
	}

	files, log := readLogs(t, dir)
	if files == 0 {
		t.Fatalf("logs に記録が無い")
	}
	// 実行の区切りはファイルにだけ入る。2回分とも残っている（上書きしていない）。
	header := "=== dwloc " + version + " version（" + runtime.GOOS + "/" + runtime.GOARCH + "）==="
	if got := strings.Count(log, header); got != 2 {
		t.Errorf("実行の区切りが %d 個、2 個を期待\n%s", got, log)
	}
	// 画面に出した1行も、時刻を付けて写してある。
	if got := strings.Count(log, " dwloc "+version+"\n"); got != 2 {
		t.Errorf("画面に出した行が %d 回、2 回を期待\n%s", got, log)
	}
}

// TestMainWithRecordKeepsTheExitCode は、失敗したときも終了コードをそのまま返し、
// 標準エラーへ出した理由が記録にも残ることを見る。
//
// 不具合の報告で要るのはむしろこちらである。記録を挟んだせいで終了コードが
// 0 に化けると、CI が失敗に気づかない。
func TestMainWithRecordKeepsTheExitCode(t *testing.T) {
	resetRecord(t)
	dir := t.TempDir()
	t.Chdir(dir)
	setArgs(t, "frobnicate")

	read := captureStd(t)
	code := mainWithRecord()
	stdout, stderr := read()
	if code != exitError {
		t.Fatalf("終了コード = %d、%d を期待", code, exitError)
	}
	if stdout != "" {
		t.Errorf("失敗したのに標準出力へ書いている:\n%s", stdout)
	}
	checkContains(t, "標準エラー", stderr, []string{"知らないサブコマンドです: frobnicate"})

	_, log := readLogs(t, dir)
	checkContains(t, "記録", log, []string{"=== dwloc " + version + " frobnicate（", "知らないサブコマンドです: frobnicate"})
}

// TestMainWithRecordRunsWithoutTheLog は、記録を始められなくても本体が動くことを見る。
//
// 読み取り専用の場所に置かれることは十分ある。そこで止めると「logs を作れない
// 場所では使えない道具」になる。logs という名前のファイルを置いて、フォルダーを
// 作れなくする（権限に頼らないので、どの OS でも、root でも同じに起きる）。
func TestMainWithRecordRunsWithoutTheLog(t *testing.T) {
	resetRecord(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logs"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	setArgs(t, "version")

	read := captureStd(t)
	code := mainWithRecord()
	stdout, stderr := read()
	if code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}
	if want := "dwloc " + version + "\n"; stdout != want {
		t.Errorf("標準出力 = %q, 期待 %q", stdout, want)
	}
	// 記録が無いことは1行で伝える。黙っていると、あとで記録を探した人が困る。
	checkContains(t, "標準エラー", stderr, []string{"記録を残せません", "このまま続けます"})
	// 記録の行き先は空のまま。半端に開いた記録へ edit が書きにいかない。
	if record != nil || hideFromRecord != nil {
		t.Errorf("記録を始めていないのに行き先が入っている")
	}
}

// TestMainWithRecordHidesTheToken は、edit のトークンが記録に残らないことを見る。
//
// トークンは最初の1回の URL に載って画面に出る。画面をそのまま写す記録には、
// 何もしなければそのまま入る。記録は不具合の報告に添えて手元の外へ出るので、
// そこにトークンがあると、待ち受けている間は誰でも画面を開ける。
//
// 記録はカレントディレクトリに置く。--root で指した翻訳リポジトリには作らない。
func TestMainWithRecordHidesTheToken(t *testing.T) {
	resetRecord(t)
	repo := editTree(t)
	dir := t.TempDir()
	t.Chdir(dir)
	setArgs(t, "edit", "--root", repo, "--no-game", "--no-browser", "--idle-timeout", "1ns")

	read := captureStd(t)
	code := mainWithRecord()
	stdout, stderr := read()
	if code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}

	// 画面にはトークンが出ている。出ていなければ、この試験は何も確かめていない。
	_, after, found := strings.Cut(stdout, "?t=")
	if !found {
		t.Fatalf("URL にトークンが載っていない:\n%s", stdout)
	}
	token := strings.Fields(after)[0]
	if token == "" {
		t.Fatalf("トークンが空:\n%s", stdout)
	}

	_, log := readLogs(t, dir)
	checkContains(t, "記録", log, []string{"http://127.0.0.1:", "?t=***"})
	if strings.Contains(log, token) {
		t.Errorf("記録にトークンが残っている:\n%s", log)
	}
	if _, err := os.Stat(filepath.Join(repo, "logs")); !os.IsNotExist(err) {
		t.Errorf("翻訳リポジトリの中に logs を作っている（err = %v）", err)
	}
}

// TestMainWithRecordShortensTheHome は、記録にだけ、利用者のホームのパスを ~ に
// 置き換えて書くことを見る。
//
// ホームのパスには利用者名が入る。記録は不具合の報告に添えて手元の外へ出るので、
// そのまま書くと利用者名も一緒に出ていく。画面には全文を出す。書けないファイルの
// 案内などは、権限の話が読めないと直し方に手が届かないためである。
func TestMainWithRecordShortensTheHome(t *testing.T) {
	resetRecord(t)
	home := t.TempDir()
	// os.UserHomeDir が見る変数は OS で違う（Windows は USERPROFILE、ほかは HOME）。
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	notRepo := filepath.Join(home, "somewhere")
	if err := os.MkdirAll(notRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	setArgs(t, "--root", notRepo)

	read := captureStd(t)
	code := mainWithRecord()
	_, stderr := read()
	if code != exitError {
		t.Fatalf("終了コード = %d\n%s", code, stderr)
	}
	// 画面には全文が出ている。出ていなければ、この試験は何も確かめていない。
	checkContains(t, "標準エラー", stderr, []string{notRepo})

	_, log := readLogs(t, dir)
	if strings.Contains(log, home) {
		t.Errorf("記録にホームのパスが残っている:\n%s", log)
	}
	// 見出し（引数）と、画面に出した案内の両方で置き換わっている。
	short := "~" + string(filepath.Separator) + "somewhere"
	if got := strings.Count(log, short); got < 2 {
		t.Errorf("記録に %q が %d 回、2 回以上を期待:\n%s", short, got, log)
	}
}

// TestDefaultStartsTheEditor は、サブコマンドを省くと画面が始まることを見る。
//
// 翻訳者はコマンドプロンプトに慣れていないことが多い。翻訳リポジトリへ dwloc を
// 置いてダブルクリックするだけで開ける、というのがこの経路の狙いである。
// ここが使い方の表示に戻ると、最初の1回で脱落する。
func TestDefaultStartsTheEditor(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "Translations", "ja"), 0o755); err != nil {
		t.Fatal(err)
	}

	var gotRoot string
	called := 0
	orig := startEdit
	startEdit = func(args []string, root, game string, stdout, stderr io.Writer) int {
		called++
		gotRoot = root
		if args != nil {
			t.Errorf("edit に引数を渡している: %q", args)
		}
		if game != "" {
			// --game を打っていないのでゲームのフォルダーは空で来る。埋まって
			// いたら、指定していない人が黙ってゲーム側を読む経路ができている。
			t.Errorf("--game を指定していないのに渡っている: %q", game)
		}
		return exitOK
	}
	t.Cleanup(func() { startEdit = orig })

	var out, errOut bytes.Buffer
	if code := run([]string{"--root", repo}, &out, &errOut); code != exitOK {
		t.Fatalf("終了コード = %d\n%s", code, errOut.String())
	}
	if called != 1 {
		t.Fatalf("edit を %d 回呼んだ、1回を期待", called)
	}
	if gotRoot != repo {
		t.Errorf("root = %q, 期待 %q", gotRoot, repo)
	}
	// 使い方を出さなくなったぶん、ほかのこともできると伝える手掛かりを残す。
	if !strings.Contains(out.String(), "dwloc help") {
		t.Errorf("ほかの使い方への案内が出ていない:\n%s", out.String())
	}
}

// TestDefaultWaitsForEnterOnlyWhenBare は、引数を1つも受け取っていないときだけ
// Enter を待つことを見る。
//
// ダブルクリックで開いた窓は、終わると同時に閉じる。案内を読む間も無く消えるので
// 待つ。一方、--root を付けて端末から呼んだ人を待たせる理由は無い。
func TestDefaultWaitsForEnterOnlyWhenBare(t *testing.T) {
	notRepo := t.TempDir()

	origIn := stdin
	t.Cleanup(func() { stdin = origIn })

	t.Run("素で呼ばれたら待つ", func(t *testing.T) {
		stdin = strings.NewReader("\n")
		var out, errOut bytes.Buffer
		// カレントディレクトリを翻訳リポジトリでない場所にして、素の呼び出しを作る。
		t.Chdir(notRepo)
		if code := run(nil, &out, &errOut); code != exitError {
			t.Fatalf("終了コード = %d", code)
		}
		// 「Enter」の3文字で見ると、t.TempDir が作る道（テスト名を含む）に当たる。
		// 実際に出す文そのもので見る。
		if !strings.Contains(errOut.String(), enterPrompt) {
			t.Errorf("Enter を待っていない:\n%s", errOut.String())
		}
	})

	t.Run("引数があれば待たない", func(t *testing.T) {
		stdin = strings.NewReader("")
		var out, errOut bytes.Buffer
		if code := run([]string{"--root", notRepo}, &out, &errOut); code != exitError {
			t.Fatalf("終了コード = %d", code)
		}
		if strings.Contains(errOut.String(), enterPrompt) {
			t.Errorf("引数があるのに Enter を待っている:\n%s", errOut.String())
		}
	})
}

// TestDefaultWaitsForEnterWhenTheEditorFails は、素で呼ばれた edit が誤りで
// 終わったときも Enter を待つことを見る。
//
// 列名の重複した公開ファイル、ロケールが1つも無い、ポートを取れない、などで
// edit は起動の途中で終わる。ダブルクリックで開いた窓は終わると同時に閉じるので、
// 待たないと理由を読む間も無く消え、どのファイルを直せばよいかが分からない。
//
// 正常に終わったとき（時間切れと Ctrl+C。どちらも終了コード0）は待たない。
// Ctrl+C を押した人を、もう1度 Enter で待たせる理由は無い。時間切れで窓が
// 閉じることは README に書いてある。
func TestDefaultWaitsForEnterWhenTheEditorFails(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "Translations"), 0o755); err != nil {
		t.Fatal(err)
	}
	origIn, origEdit := stdin, startEdit
	t.Cleanup(func() { stdin, startEdit = origIn, origEdit })

	for _, tt := range []struct {
		name     string
		editCode int
		args     []string
		wantWait bool
	}{
		{name: "素で呼ばれて誤りで終われば待つ", editCode: exitError, wantWait: true},
		{name: "素で呼ばれても正常に終われば待たない", editCode: exitOK, wantWait: false},
		{name: "引数があれば誤りで終わっても待たない", editCode: exitError, args: []string{"--root", repo}, wantWait: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdin = strings.NewReader("\n")
			startEdit = func([]string, string, string, io.Writer, io.Writer) int { return tt.editCode }
			t.Chdir(repo)

			var out, errOut bytes.Buffer
			if code := run(tt.args, &out, &errOut); code != tt.editCode {
				t.Fatalf("終了コード = %d、edit の %d をそのまま返すことを期待", code, tt.editCode)
			}
			if got := strings.Contains(errOut.String(), enterPrompt); got != tt.wantWait {
				t.Errorf("Enter を待った = %v、期待 %v\n%s", got, tt.wantWait, errOut.String())
			}
		})
	}
}

// TestNotARepoAdvisesRootOutsideWindows は、翻訳リポジトリでない場所で起動した
// ときの案内が、Windows 以外では dwloc を移させずに --root を勧めることを見る。
//
// 翻訳リポジトリの .gitignore が外しているのは dwloc.exe と dwloc*.log だけで、
// macOS と Linux の本体 dwloc は外れない。案内どおりに翻訳リポジトリへ移すと、
// git add -A で実行ファイルが Pull Request に入る。validate も上流の CI も
// それを指摘しない。
func TestNotARepoAdvisesRootOutsideWindows(t *testing.T) {
	notRepo := t.TempDir()
	var out, errOut bytes.Buffer
	if code := run([]string{"--root", notRepo}, &out, &errOut); code != exitError {
		t.Fatalf("終了コード = %d", code)
	}
	moveHint := "Translations フォルダーと同じ場所へ dwloc を移して"
	if runtime.GOOS == "windows" {
		checkContains(t, "標準エラー", errOut.String(), []string{moveHint, "dwloc edit --root"})
		return
	}
	if strings.Contains(errOut.String(), moveHint) {
		t.Errorf("Windows 以外で dwloc を翻訳リポジトリへ移させている:\n%s", errOut.String())
	}
	checkContains(t, "標準エラー", errOut.String(), []string{"--root", ".gitignore"})
}

// TestNotARepoMessage は、案内の文を OS ごとに見る。どの OS で走らせても、
// 両方の文を確かめられるようにする。
func TestNotARepoMessage(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		msg := notARepoMessage(goos)
		if strings.Contains(msg, "dwloc を移して") {
			t.Errorf("%s: dwloc を翻訳リポジトリへ移させている:\n%s", goos, msg)
		}
		checkContains(t, goos+" の案内", msg, []string{"探した場所: %s", "--root", ".gitignore", "dwloc help"})
	}
	checkContains(t, "windows の案内", notARepoMessage("windows"),
		[]string{"探した場所: %s", "dwloc を移して", "--root", "dwloc help"})
}

// TestLooksLikeRepo は、翻訳リポジトリらしさの見方を確かめる。
//
// 見るのは Translations ディレクトリの有無だけである。中身まで確かめないのは、
// ロケールの数え方を publish.DiscoverTargets と2か所に持たないためである。
func TestLooksLikeRepo(t *testing.T) {
	empty := t.TempDir()
	if looksLikeRepo(empty) {
		t.Error("Translations が無いのに翻訳リポジトリだと言っている")
	}

	withDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(withDir, "Translations"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !looksLikeRepo(withDir) {
		t.Error("Translations があるのに翻訳リポジトリでないと言っている")
	}

	// 同じ名前のファイルはディレクトリではない。
	withFile := t.TempDir()
	if err := os.WriteFile(filepath.Join(withFile, "Translations"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if looksLikeRepo(withFile) {
		t.Error("Translations がファイルなのに翻訳リポジトリだと言っている")
	}
}
