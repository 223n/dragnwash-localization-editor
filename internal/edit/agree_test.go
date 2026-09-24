package edit

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
//	publish       csvfile.ParsePowerShellRecord（ConvertFrom-Csv の移植）と、
//	              全体を解釈する主の読み手 csvfile.ReadPowerShell（PR2 から publish が使う）
//	validate      csvfile.ReadPythonRecords（Python の csv.reader の移植）
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
			if file, err := csvfile.ReadPowerShell(out); err != nil || len(file.Records) != 1 {
				t.Errorf("主の読み手で読めない: %v（%d 件）", err, len(file.Records))
			} else if got := file.Records[0].Get("translation"); got != want {
				t.Errorf("主の読み手の読み = %q, want %q（行: %q）", got, want, body)
			}

			// validate の読み方。
			recs, err := csvfile.ReadPythonRecords(out)
			if err != nil {
				t.Fatalf("validate の読みが失敗した: %v", err)
			}
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

// TestMultilineValuesAgreeAcrossReaders は、複数行の値と、引用の中で '#' から
// 始まる行を含む値を書いたファイルを、3つの読み手が同じ値として読むことを確かめる。
//
// 全体を解釈する読み手へ移す作業の PR3 と PR4 で、保存はレコードの最終フィールド
// （区切りの関数の Offsets の最後）から本体の終わりまでを差し替え、元の終端を
// そのまま残す形になる。いまの SetTranslation は改行を拒むので、ここでは同じ
// 差し替えを試験の中で組み立て、値は保存と同じ escapeTranslation で書く。
// ファイルは実物の作業コピーと同じ形（レコードの区切りは CRLF、原文は空行を挟む
// 複数行、値の中は LF）にする。
//
// 3つの読み手のどれかが別の値を返すと、画面で見ている訳と、ゲームが表示する訳と、
// 検証が見る訳が食い違う。とくに '#' で始まる行は、行単位で読む道具がコメントとして
// 落とす形なので、引用の中なら値に入ることをここで縛る。
func TestMultilineValuesAgreeAcrossReaders(t *testing.T) {
	const (
		header = "key,section,node,order,speaker,source_en,translation\r\n"
		// 原文が段落を空行で分けた複数行で、訳が空のレコード。実物の作業コピーに
		// 1件だけある形と同じ（合成した文）。
		record = "f2ea4a1f0e4e8626,UI,,,UI,\"para1\n\npara2\",\r\n"
		// 後ろのレコードが飲み込まれていないことを見るための行。
		after = "# --- node ---\r\n3fc4ccfe745870e2,UI,,,UI,two,に\r\n"
	)
	values := []string{
		"一行目\n二行目",
		"一行目\n\n  \n四行目",
		"一行目\n# 二行目",
		"# 先頭の行\n二行目",
		"一行目\n#\n#三行目",
		"\n",
		"\n# だけ\n",
		"カンマ,\nと改行",
		"引用符 \"hi\"\n# と '#'",
		"末尾に空白  \n 先頭に空白",
		"CRLF の\r\n改行",
		// 引用の中の単独の CR も、3つの読み手は同じ値に読む。publish が止めるのは、
		// 上流の道具（Remove-NonRecords）があとでそのレコードを落とすためで、
		// ここの3つの読み手の差のためではない（csvfile.LoneCRValues）。
		"単独の\rCR",
		"末尾の CR\r",
	}

	for _, want := range values {
		t.Run(want, func(t *testing.T) {
			segs := csvfile.SplitSegments([]byte(header + record + after))
			seg := segs.List[1]
			body := segs.Body(seg)
			start := seg.Offsets[len(seg.Offsets)-1] - seg.Start
			out := []byte(header + body[:start] + escapeTranslation(want) + string(seg.Term) + after)

			// ゲーム内Mod の読み方。
			game := csvfile.ReadCSharpRows(out)
			if len(game) != 2 {
				t.Fatalf("ゲーム内Mod の読みで %d 件（2件のはず）", len(game))
			}

			// publish の読み方（全体を解釈する主の読み手）。飲み込みと見なされないこと、
			// ゲームの読み方と割れないことも見る。
			file, err := csvfile.ReadPowerShell(out)
			if err != nil || len(file.Records) != 2 {
				t.Fatalf("主の読み手で読めない: %v（%d 件）", err, len(file.Records))
			}
			if got := csvfile.FindSwallows(file.Segments); got != nil {
				t.Errorf("飲み込みと見なされた: %+v", got)
			}
			if got := csvfile.CSharpDisagreements(file); got != nil {
				t.Errorf("ゲームの読み方と割れた: %+v", got)
			}

			// validate の読み方。先頭のレコードはヘッダー。
			recs, err := csvfile.ReadPythonRecords(out)
			if err != nil || len(recs) != 3 {
				t.Fatalf("validate の読みで読めない: %v（%d 件）", err, len(recs))
			}

			for i, r := range file.Records {
				for c, col := range []string{"key", "section", "node", "order", "speaker", "source_en", "translation"} {
					ps, cs, py := r.Get(col), game[i].Get(col), recs[i+1].Fields[c]
					if ps != cs || ps != py {
						t.Errorf("[%d] %s が食い違う: 主の読み手 %q、ゲーム内Mod %q、validate %q", i, col, ps, cs, py)
					}
				}
			}
			if got := file.Records[0].Get("translation"); got != want {
				t.Errorf("書いた訳 = %q, want %q", got, want)
			}
			if got := file.Records[0].Get("source_en"); got != "para1\n\npara2" {
				t.Errorf("原文が変わった: %q", got)
			}
			if got := file.Records[1].Get("key"); got != "3fc4ccfe745870e2" {
				t.Errorf("後ろのレコードのキー = %q", got)
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

		// 版の照合は読むだけなので、書けなくしても通って再試行の経路まで届く。
		forbidSaving(t, path)

		start := time.Now()
		saveErr := f.Save()
		elapsed := time.Since(start)

		if saveErr == nil {
			// 書けないことは forbidSaving が確かめてある。それでも通るのは、
			// 一時ファイルを経ずに path を直接書き換えたときである（POSIX では
			// 0o555 のディレクトリの中でも、既にあるファイルの上書きは通る）。
			t.Fatalf("書けない状態で保存が通った。一時ファイルを経ていない（中身: %q）", readFile(t, path))
		}
		// 10ms + 30ms + 100ms を空けて4回試す。
		if elapsed < 140*time.Millisecond {
			t.Errorf("再試行していない: %v しか掛かっていない（誤り: %v）", elapsed, saveErr)
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

// forbidSaving は path への保存が失敗する状態にする。止められたことを
// 確かめられなければ、呼んだ試験を飛ばす。
//
// 閉じる相手が OS で違う。[publish.WriteBytes] は同じディレクトリに一時ファイルを
// 作ってから rename で置き換えるので、Windows はファイルの読み取り専用属性で止まり、
// POSIX はディレクトリの書き込み権で止まる。ファイルだけを 0444 にしていたころは、
// POSIX では置き換えが通ってしまい、この節は毎回飛ばされていた。
//
// 止められたかは、調べる対象（[File.Save]）の成否ではなく、ここで実際に書いてみて
// 確かめる。POSIX ではディレクトリに新しいファイルを作れないこと、Windows では
// ファイルを書き込み用に開けないことを見る。対象の成否で代用していたころは、
// 対象が一時ファイルを経ずに path を直接書き換える形へ戻ったときも「止められ
// なかった環境」と読んで飛ばしていた。go test は飛ばした試験を成功として数えるので、
// 再試行の経路が CI で1度も通らなくなっても誰も気づけない。
//
// 飛ばすのは、閉じても書けてしまう環境（root で走っている、など）だけである。
func forbidSaving(t *testing.T, path string) {
	t.Helper()

	target := path
	var closed, open os.FileMode = 0o444, 0o644
	if runtime.GOOS != "windows" {
		target, closed, open = filepath.Dir(path), 0o555, 0o755
	}
	if err := os.Chmod(target, closed); err != nil {
		t.Fatal(err)
	}
	// 後始末で消せるように戻す。t.Cleanup は後入れ先出しなので、呼び出し側が
	// 先に作った t.TempDir の削除より先に走る。
	t.Cleanup(func() { _ = os.Chmod(target, open) })

	if runtime.GOOS == "windows" {
		probe, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err == nil {
			_ = probe.Close()
			t.Skipf("%s を読み取り専用にしても書き込み用に開けたので飛ばす", path)
		}
		return
	}
	probe, err := os.CreateTemp(target, "probe*")
	if err == nil {
		_ = probe.Close()
		_ = os.Remove(probe.Name())
		t.Skipf("%s に新しいファイルを作れたので飛ばす。root で走っていると効かない", target)
	}
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
