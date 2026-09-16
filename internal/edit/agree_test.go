package edit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
)

// TestSetTranslationAgreesAcrossReaders は、書いた訳を3つの読み手が同じ値として
// 読むことを確かめる。
//
// 同じファイルを次の3つが読む。どれか1つでも違う値を返すと、翻訳者が画面で
// 見ている値と、ホットリロードでゲームが実際に表示する値が食い違う。
//
//	ゲーム内Mod   csvfile.ParseCSharpRecords（CsvReader.cs の移植）
//	publish       csvfile.ParsePowerShellRecord（ConvertFrom-Csv の移植）
//	validate      csvfile.ParsePythonRecords（Python の csv.reader の移植）
func TestSetTranslationAgreesAcrossReaders(t *testing.T) {
	values := []string{
		"ふつうの訳",
		"",
		" 先頭に空白",
		"末尾に空白  ",
		"   ",
		"\t先頭タブ",
		"カンマ, を含む",
		`引用符 " を含む`,
		`he said "hi"`,
		"<size=70%>タグ</size>",
		" 前後とも空白 ",
		"混ざった \" と , と空白 ",
	}

	for _, want := range values {
		t.Run(want, func(t *testing.T) {
			const header = "key,section,node,order,speaker,source_en,translation\n"
			f := Parse([]byte(header + "k1,s,n,1,sp,Hello,古い\n"))
			if err := f.SetTranslation(2, want); err != nil {
				t.Fatalf("SetTranslation が失敗した: %v", err)
			}

			line, _ := f.Line(2)
			if got := line.Translation(); got != want {
				t.Errorf("編集モデル = %q, want %q", got, want)
			}

			out := f.Bytes()
			body := dataLine(t, out)

			// ゲーム内Mod の読み方。
			if recs := csvfile.ParseCSharpRecords(string(out)); len(recs) >= 2 {
				if got := recs[1][len(recs[1])-1]; got != want {
					t.Errorf("ゲーム内Mod の読み = %q, want %q（行: %q）", got, want, body)
				}
			} else {
				t.Errorf("ゲーム内Mod の読みでレコードが足りない: %d", len(recs))
			}

			// publish の読み方。末尾の空フィールドを落とす契約なので、
			// 区切りの数まで空文字で埋めてから最終列を見る（refresh と同じ手当て）。
			fields, _ := csvfile.ParsePowerShellRecord(body)
			for len(fields) < len(csvfile.FieldOffsets(body)) {
				fields = append(fields, "")
			}
			if got := fields[len(fields)-1]; got != want {
				t.Errorf("publish の読み = %q, want %q（行: %q）", got, want, body)
			}

			// validate の読み方。
			recs := csvfile.ParsePythonRecords(csvfile.ReadPythonLines(out))
			if len(recs) >= 2 {
				got := recs[1].Fields[len(recs[1].Fields)-1]
				if got != want {
					t.Errorf("validate の読み = %q, want %q（行: %q）", got, want, body)
				}
			} else {
				t.Errorf("validate の読みでレコードが足りない: %d", len(recs))
			}
		})
	}
}

// TestSetTranslationIsIdempotent は、画面に出ている値をそのまま入れ直しても
// バイトが変わらないことを確かめる。
//
// 変わると、自動保存が「何も変えていないのに書く」ことになり、ファイルを
// 見張っているゲームを無駄に起こす。差分にも出る。
func TestSetTranslationIsIdempotent(t *testing.T) {
	values := []string{"ふつうの訳", " 先頭", "末尾  ", "   ", `" と , `, ""}

	for _, v := range values {
		t.Run(v, func(t *testing.T) {
			const header = "key,translation\n"
			f := Parse([]byte(header + "abc,古い\n"))
			if err := f.SetTranslation(2, v); err != nil {
				t.Fatalf("1回目が失敗した: %v", err)
			}
			first := string(f.Bytes())

			line, _ := f.Line(2)
			shown := line.Translation()
			if err := f.SetTranslation(2, shown); err != nil {
				t.Fatalf("2回目が失敗した: %v", err)
			}
			if second := string(f.Bytes()); second != first {
				t.Errorf("入れ直しでバイトが変わった\n1回目 %q\n2回目 %q", first, second)
			}
		})
	}
}

// dataLine は出力の2行目（ヘッダーの次）を改行なしで返す。
func dataLine(t *testing.T, out []byte) string {
	t.Helper()
	lines := csvfile.SplitPythonLines(out)
	if len(lines) < 2 {
		t.Fatalf("行が足りない: %d", len(lines))
	}
	body, _ := splitTerminator(lines[1].Text)
	return body
}

// TestSaveRetriesAndRechecksVersion は、再試行の経路そのものと、そのあいだに
// 第三者が書いた内容を消さないことを確かめる。
//
// 再試行が起きるのは「ほかのプロセスがファイルを掴んでいる」ときで、それは
// 別の窓や publish の再生成が書き込む場面そのものである。版の照合を冒頭の
// 1回で済ませると、待っているあいだの書き込みを黙って消す。
func TestSaveRetriesAndRechecksVersion(t *testing.T) {
	t.Run("書けないときは再試行してから諦める", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "strings.csv")
		const original = "key,translation\nabc,古い\n"
		if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}

		f, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.SetTranslation(2, "新しい"); err != nil {
			t.Fatal(err)
		}

		// 読み取り専用にすると、Windows では rename が失敗する。
		// 版の照合は通るので、再試行の経路まで届く。
		if err := os.Chmod(path, 0o444); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

		start := time.Now()
		saveErr := f.Save()
		elapsed := time.Since(start)

		if saveErr == nil {
			// 環境によっては読み取り専用でも rename が通る。そのときは
			// 再試行の経路を確かめられないので飛ばす。
			t.Skip("この環境では読み取り専用のファイルへ書けたので飛ばす")
		}
		// 10ms + 30ms + 100ms を空けて4回試す。
		if elapsed < 140*time.Millisecond {
			t.Errorf("再試行していない: %v しか掛かっていない（誤り: %v）", elapsed, saveErr)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != original {
			t.Errorf("書けなかったのに中身が変わった\ngot  %q\nwant %q", got, original)
		}
	})

	t.Run("再試行中の書き込みを消さない", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "strings.csv")
		const original = "key,translation\nabc,古い\n"
		if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}

		f, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.SetTranslation(2, "編集した訳"); err != nil {
			t.Fatal(err)
		}

		// 保存の直前に第三者が書く。版が変わるので、1バイトも書かずに
		// 競合として返らなければならない。
		const other = "key,translation\nabc,他のツールが書いた\n"
		if err := os.WriteFile(path, []byte(other), 0o644); err != nil {
			t.Fatal(err)
		}

		err = f.Save()
		if err == nil {
			t.Fatal("競合を見逃して保存した")
		}
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != other {
			t.Errorf("第三者の書き込みを消した\ngot  %q\nwant %q", got, other)
		}
	})
}

// TestSaveWithoutChangesKeepsModTime は「中身が同じなら書かない」を、時刻の
// 刻みに左右されない形で確かめる。
//
// 直前に書いたファイルの更新時刻を比べるだけでは、rename の時刻が同じ刻みに
// 収まって偽の合格になる（同じ環境で200回中31回起きた）。過去の時刻を明示的に
// 付けてから確かめる。
func TestSaveWithoutChangesKeepsModTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "strings.csv")
	if err := os.WriteFile(path, []byte("key,translation\nabc,訳\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}

	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("保存が失敗した: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Truncate(time.Second).Equal(past) {
		t.Errorf("変えていないのに書いた。更新時刻 %v, want %v", info.ModTime(), past)
	}
}

// TestTranslationHidesMiscountedColumns は、列数がヘッダーと合わない行で
// 訳ではない列を訳として返さないことを確かめる。
//
// 列が足りない行の最終フィールドは speaker などの別の列である。書き込み側は
// 同じ理由で止めているので、読み出し側もそろえないと、画面の訳欄に無関係な値が並ぶ。
func TestTranslationHidesMiscountedColumns(t *testing.T) {
	const header = "key,section,node,order,speaker,translation\n"
	f := Parse([]byte(header +
		"k1,s,n,1,sp,ふつう\n" +
		"k2,s,n,1,sp,extra,over\n" +
		"k3,s,n,1,short\n"))

	tests := []struct {
		line int
		want string
		ok   bool
	}{
		{2, "ふつう", true},
		{3, "", false},
		{4, "", false},
	}
	for _, tt := range tests {
		line, found := f.Line(tt.line)
		if !found {
			t.Fatalf("%d 行目が無い", tt.line)
		}
		if line.Editable != tt.ok {
			t.Errorf("%d 行目 Editable = %v, want %v", tt.line, line.Editable, tt.ok)
		}
		if got := line.Translation(); got != tt.want {
			t.Errorf("%d 行目 Translation = %q, want %q", tt.line, got, tt.want)
		}
		if line.Text == "" {
			t.Errorf("%d 行目 Text が空。生の行は見られなければならない", tt.line)
		}
	}
}

// TestRefreshRecomputesKind は、訳を消した結果その行がレコードでなくなったとき、
// モデルと読み直しが食い違わないことを確かめる。
//
// 2列のヘッダーでキーが空の行を空にすると "," になり、これはレコードとして
// 読まれない。Kind を据え置くと「モデルはデータ行、読み直すと空行」になる。
func TestRefreshRecomputesKind(t *testing.T) {
	f := Parse([]byte("key,translation\n,古い\n"))
	if err := f.SetTranslation(2, ""); err != nil {
		t.Fatalf("SetTranslation が失敗した: %v", err)
	}

	after, _ := f.Line(2)
	reloaded, _ := Parse(f.Bytes()).Line(2)

	if after.Kind != reloaded.Kind {
		t.Errorf("Kind が食い違う: 編集後 %v, 読み直し %v", after.Kind, reloaded.Kind)
	}
	if after.Editable != reloaded.Editable {
		t.Errorf("Editable が食い違う: 編集後 %v, 読み直し %v", after.Editable, reloaded.Editable)
	}
}

// TestSetTranslationRejectsUnwritableValues は、後段の誰も検出しない値を
// この層で止めることを確かめる。
//
// internal/validate は doc コメントで「不正なUTF-8でエラーにしない」
// 「NUL の _csv.Error を再現しない」と明言している。ここで止めないと、
// 壊れた値が誰にも気づかれずに公開ファイルまで届く。
func TestSetTranslationRejectsUnwritableValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"改行", "前\n後"},
		{"復帰", "前\r後"},
		{"NUL", "前\x00後"},
		{"不正なUTF-8", "\xff\xfe壊れた"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Parse([]byte("key,translation\nabc,古い\n"))
			before := string(f.Bytes())
			err := f.SetTranslation(2, tt.value)
			if err == nil {
				t.Fatalf("書けない値を受け入れた: %q", tt.value)
			}
			var invalid *InvalidValueError
			if !errors.As(err, &invalid) {
				t.Errorf("err = %v, want *InvalidValueError", err)
			}
			if got := string(f.Bytes()); got != before {
				t.Errorf("拒んだのに中身が変わった: %q", got)
			}
		})
	}
}
