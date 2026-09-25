package edit

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// samplePublished は実データと同じ姿の小さな公開ファイル。
// 見出し・空行・未訳行・引用の要る訳を含む。
const samplePublished = "key,section,node,order,speaker,translation\n" +
	"\n" +
	"# ===== Level 1: Ryan (Sunny) | sets level_1 =====\n" +
	"# --- intro: Ryan_1_intro ---\n" +
	"0da72197e898ebe1,L01 Ryan,Ryan_1_intro,1,Ryan,ああ\n" +
	"334d016f755cd6dc,L01 Ryan,Ryan_1_intro,2,Kobold,\n" +
	"dfba3eb46cb5fc14,L01 Ryan,Ryan_1_intro,3,Ryan,\"a,b\"\n"

// sampleWorking は作業コピーと同じ姿の小さなファイル。7列で source_en を持つ。
const sampleWorking = "key,section,node,order,speaker,source_en,translation\n" +
	"\n" +
	"# ===== Level 1: Ryan (Sunny) =====\n" +
	"# --- intro: Ryan_1_intro ---\n" +
	"0da72197e898ebe1,L01 Ryan,Ryan_1_intro,1,Ryan,Ah,ああ\n" +
	"334d016f755cd6dc,L01 Ryan,Ryan_1_intro,2,Kobold,\"Hi, there\",\n" +
	"\n" +
	"# ===== UI and other text (not part of the dialogue script) =====\n" +
	"9a1b2c3d4e5f6071,UI,,,UI,Start,はじめる\n"

// TestParseClassifiesLines は行の種類分けを固定する。
func TestParseClassifiesLines(t *testing.T) {
	f := Parse([]byte(sampleWorking))
	if f.ReadOnly() {
		t.Fatalf("読み取り専用になった: %s", f.ReadOnlyReason())
	}

	want := []Kind{
		KindHeader,  // 1
		KindBlank,   // 2
		KindComment, // 3
		KindComment, // 4
		KindData,    // 5
		KindData,    // 6
		KindBlank,   // 7
		KindComment, // 8
		KindData,    // 9
	}
	lines := f.Lines()
	if len(lines) != len(want) {
		t.Fatalf("行数が違う: got %d, want %d", len(lines), len(want))
	}
	for i, line := range lines {
		if line.Number != i+1 {
			t.Errorf("%d番目の行番号が %d", i, line.Number)
		}
		if line.Kind != want[i] {
			t.Errorf("%d行目の種類が %v, want %v (%q)", line.Number, line.Kind, want[i], line.Text)
		}
	}

	line, ok := f.Line(6)
	if !ok {
		t.Fatal("6行目が引けない")
	}
	if got, want := line.Key(), "334d016f755cd6dc"; got != want {
		t.Errorf("キーが %q, want %q", got, want)
	}
	if got, want := line.Fields[5], "Hi, there"; got != want {
		t.Errorf("source_en が %q, want %q", got, want)
	}
	// 未訳行。ParsePowerShellRecord は末尾の空フィールドを落とすので、
	// 埋め戻せていなければここで7列にならない。
	if got := len(line.Fields); got != 7 {
		t.Errorf("フィールド数が %d, want 7: %q", got, line.Fields)
	}
	if got := line.Translation(); got != "" {
		t.Errorf("訳が %q, want 空", got)
	}
	if !line.Editable {
		t.Errorf("未訳行が編集不可: %s", line.Reason)
	}

	// コメント行と空行にはフィールドを入れない。
	for _, n := range []int{2, 3, 4, 7, 8} {
		other, _ := f.Line(n)
		if other.Fields != nil {
			t.Errorf("%d行目にフィールドが入っている: %q", n, other.Fields)
		}
		if other.Editable {
			t.Errorf("%d行目が編集可になっている", n)
		}
		if other.Translation() != "" || other.Key() != "" {
			t.Errorf("%d行目から値が取れてしまう", n)
		}
	}
}

// TestAcceptedHeaders は受理される4種と、受理されない例を固定する。
func TestAcceptedHeaders(t *testing.T) {
	accepted := []string{
		"key,section,node,order,speaker,translation",
		"key,speaker,translation",
		"key,translation",
		"key,section,node,order,speaker,source_en,translation",
	}
	for _, header := range accepted {
		t.Run("受理:"+header, func(t *testing.T) {
			f := Parse([]byte(header + "\n"))
			if f.ReadOnly() {
				t.Fatalf("読み取り専用になった: %s", f.ReadOnlyReason())
			}
			if got := strings.Join(f.Header(), ","); got != header {
				t.Errorf("ヘッダーが %q, want %q", got, header)
			}
		})
	}

	rejected := []string{
		"key,section,node,order,speaker,source_en", // translation が最終列でない
		"key,section,node,order,source_en,speaker", // 並びが違う
		"Key,Translation",                          // 大文字小文字は正規化しない
		"key,translation,extra",                    // 余分な列
		"key",                                      // 1列
		"source_en,translation",                    // key が無い
		"",                                         // 空（この行は空行相当なので、そもそもヘッダーにならない）
		// 以下は生テキストの完全一致だから落ちるもの。ConvertFrom-Csv として
		// 解釈すると通ってしまうが、internal/validate は落とす。
		// 厳しい側にそろえるという判断（matchHeader のコメント参照）。
		" key,translation", // 先頭に空白
		"key, translation", // 区切りの後ろに空白
		"key,section,node,order,speaker,translation,", // 末尾に余分な区切り
		`"key",translation`,                           // 引用符で囲まれている
		"key,translation ",                            // 末尾に空白
	}
	for _, header := range rejected {
		t.Run("拒否:"+header, func(t *testing.T) {
			f := Parse([]byte(header + "\na,b\n"))
			if !f.ReadOnly() {
				t.Fatal("読み取り専用にならなかった")
			}
			if f.ReadOnlyReason() == "" {
				t.Error("理由が空")
			}
			if f.Header() != nil {
				t.Errorf("ヘッダーが入っている: %q", f.Header())
			}
		})
	}
}

// TestAcceptedHeadersIsACopy は返り値を書き換えても内部が壊れないことを見る。
func TestAcceptedHeadersIsACopy(t *testing.T) {
	first := AcceptedHeaders()
	if len(first) != 4 {
		t.Fatalf("受理するヘッダーが %d 種, want 4", len(first))
	}
	if last := first[len(first)-1]; !slices.Equal(last, workingHeader) {
		t.Errorf("最後が作業コピーのヘッダーでない: %q", last)
	}
	// どの形でも最終列は translation。編集モデルの前提そのもの。
	for _, h := range first {
		if got := h[len(h)-1]; got != "translation" {
			t.Errorf("%q の最終列が %q", h, got)
		}
	}
	first[0][0] = "壊す"
	if second := AcceptedHeaders(); second[0][0] != "key" {
		t.Errorf("複製でない: %q", second[0])
	}
}

// TestRoundTripBytes は1行も編集しなければ入力とバイト単位で一致することを見る。
// 境界条件（改行の種類、末尾改行の有無、BOM、壊れた行）を1つずつ通す。
func TestRoundTripBytes(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"公開ファイル", samplePublished},
		{"作業コピー", sampleWorking},
		{"空ファイル", ""},
		{"ヘッダーだけ", "key,translation\n"},
		{"ヘッダーだけ改行なし", "key,translation"},
		{"末尾に改行が無い", "key,translation\nabc,訳"},
		{"CRLF", "key,translation\r\nabc,訳\r\n"},
		{"CRLFとLFの混在", "key,translation\r\nabc,訳\nxyz,訳2\r\n"},
		{"CR単独", "key,translation\rabc,訳\r"},
		{"BOM付き", "\xef\xbb\xbfkey,translation\nabc,訳\n"},
		{"BOM付きCRLF", "\xef\xbb\xbfkey,translation\r\nabc,訳\r\n"},
		{"BOM付き作業コピー", "\xef\xbb\xbf" + sampleWorking},
		{"コメントと空行だけ", "# a\n\n# b\n"},
		{"列数が合わない行がある", "key,translation\nabc,訳,余分\nxyz\n"},
		{"空行が途中にある", "key,translation\n\n\nabc,訳\n"},
		{"空白だけの行", "key,translation\n   \nabc,訳\n"},
		{"ヘッダーが受理されない", "foo,bar\n1,2\n"},
		{"訳が空", "key,translation\nabc,\n"},
		{"引用符とカンマを含む訳", "key,translation\nabc,\"a,\"\"b\"\"\"\n"},
		{"閉じていない引用符", "key,translation\nabc,\"訳\n"},
		{"連続する空行と末尾改行なし", "key,translation\n\nabc,訳\n\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := []byte(tt.in)
			f := Parse(in)
			if got := f.Bytes(); !bytes.Equal(got, in) {
				t.Errorf("バイト一致しない:\n got %q\nwant %q", got, in)
			}
			if f.Dirty() {
				t.Error("編集していないのに Dirty")
			}
			if f.Version() != hashBytes(in) {
				t.Error("版が入力のハッシュと違う")
			}
		})
	}
}

// TestSetTranslationTouchesOneLineOnly は「触った行以外は1バイトも変わらない」
// ことを見る。このパッケージの存在理由そのもの。
func TestSetTranslationTouchesOneLineOnly(t *testing.T) {
	inputs := map[string]string{
		"LF":     sampleWorking,
		"CRLF":   strings.ReplaceAll(sampleWorking, "\n", "\r\n"),
		"末尾改行なし": strings.TrimSuffix(sampleWorking, "\n"),
		"BOM付き":  "\xef\xbb\xbf" + sampleWorking,
		"改行の混在": "key,section,node,order,speaker,source_en,translation\r\n" +
			"000000000000000a,,,,,,\n" +
			"000000000000000b,,,,,,古い\r\n" +
			"000000000000000c,,,,,,\r",
	}

	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			f := Parse([]byte(in))
			if f.ReadOnly() {
				t.Fatalf("読み取り専用: %s", f.ReadOnlyReason())
			}

			// 2つ目のデータ行を選ぶ。どれでもよい。
			target := 0
			seen := 0
			for _, line := range f.Lines() {
				if line.Kind == KindData {
					if seen++; seen == 2 {
						target = line.ID
						break
					}
				}
			}
			if target == 0 {
				t.Fatal("データ行が2つ無い")
			}

			before := f.Lines()
			if err := f.SetTranslation(target, "新しい訳"); err != nil {
				t.Fatalf("書き換えに失敗した: %v", err)
			}
			if !f.Dirty() {
				t.Error("Dirty が立っていない")
			}

			after := f.Lines()
			if len(before) != len(after) {
				t.Fatalf("行数が変わった: %d -> %d", len(before), len(after))
			}
			for i := range before {
				if before[i].ID == target {
					continue
				}
				if before[i].Text != after[i].Text || before[i].ID != after[i].ID {
					t.Errorf("ID %d の行が変わった:\n before %q\n after  %q",
						before[i].ID, before[i].Text, after[i].Text)
				}
			}

			got, _ := f.Line(target)
			if v := got.Translation(); v != "新しい訳" {
				t.Errorf("訳が %q, want %q", v, "新しい訳")
			}

			// 改行の種類は元のまま。
			oldBody, oldTerm := splitTerminator(before[target-1].Text)
			newBody, newTerm := splitTerminator(got.Text)
			if newTerm != oldTerm {
				t.Errorf("改行が %q, want %q", newTerm, oldTerm)
			}
			// 最終フィールドより前は1バイトも変わらない。
			offsets := csvfile.FieldOffsets(oldBody)
			prefix := oldBody[:offsets[len(offsets)-1]]
			if !strings.HasPrefix(newBody, prefix) {
				t.Errorf("行の前半が変わった:\n got %q\nwant接頭辞 %q", newBody, prefix)
			}
			if newBody != prefix+"新しい訳" {
				t.Errorf("最終フィールド以外に差がある: %q", newBody)
			}

			// BOM も落ちない。
			if strings.HasPrefix(in, "\xef\xbb\xbf") && !bytes.HasPrefix(f.Bytes(), []byte("\xef\xbb\xbf")) {
				t.Error("BOM が落ちた")
			}
			// 触った行だけの差になっていることを、丸ごとの差でも確かめる。
			wantWhole := strings.Replace(in, before[target-1].Text, got.Text, 1)
			if string(f.Bytes()) != wantWhole {
				t.Errorf("全体が一致しない:\n got %q\nwant %q", f.Bytes(), wantWhole)
			}
		})
	}
}

// TestSetTranslationValues は値ごとの書き戻し結果を固定する。
// 「書いた値」と「読み戻す値」が一致しない場合があることも含めて固定する。
func TestSetTranslationValues(t *testing.T) {
	const header = "key,translation\n"
	tests := []struct {
		name string
		line string
		set  string
		want string // 書き戻したあとの行（改行を除く）
		read string // そのあと読み戻される訳
	}{
		{"普通の訳", "0123456789abcdef,", "こんにちは", "0123456789abcdef,こんにちは", "こんにちは"},
		{"訳を空にする", "0123456789abcdef,古い", "", "0123456789abcdef,", ""},
		{"カンマを含む", "0123456789abcdef,", "a,b", `0123456789abcdef,"a,b"`, "a,b"},
		{"二重引用符を含む", "0123456789abcdef,", `a"b`, `0123456789abcdef,"a""b"`, `a"b`},
		{"引用が要る訳から要らない訳へ", `0123456789abcdef,"a,b"`, "c", "0123456789abcdef,c", "c"},
		{"タブは引用しない", "0123456789abcdef,", "a\tb", "0123456789abcdef,a\tb", "a\tb"},
		{"シャープで始まる訳", "0123456789abcdef,", "#x", "0123456789abcdef,#x", "#x"},
		{"書式タグ", "0123456789abcdef,", `<gradient="gold">金</gradient>`, `0123456789abcdef,"<gradient=""gold"">金</gradient>"`, `<gradient="gold">金</gradient>`},
		// 元実装から引き継ぐ非対称。EscapeField は空白を引用せず、
		// 前後に空白がある訳は引用して書く。引用しないと読み手ごとに値が割れる
		// （escapeTranslation の doc コメント参照）。
		{"先頭の空白は引用して保つ", "0123456789abcdef,", " x", `0123456789abcdef," x"`, " x"},
		{"末尾の空白は引用して保つ", "0123456789abcdef,", "x  ", `0123456789abcdef,"x  "`, "x  "},
		{"空白だけの訳も引用して保つ", "0123456789abcdef,x", "   ", `0123456789abcdef,"   "`, "   "},
		{"前後に空白が無ければ引用しない", "0123456789abcdef,x", "ふつうの訳", "0123456789abcdef,ふつうの訳", "ふつうの訳"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Parse([]byte(header + tt.line + "\n"))
			if err := f.SetTranslation(2, tt.set); err != nil {
				t.Fatalf("書き換えに失敗した: %v", err)
			}
			line, _ := f.Line(2)
			if got := strings.TrimSuffix(line.Text, "\n"); got != tt.want {
				t.Errorf("行が %q, want %q", got, tt.want)
			}
			if got := line.Translation(); got != tt.read {
				t.Errorf("読み戻した訳が %q, want %q", got, tt.read)
			}
			if !line.Editable {
				t.Errorf("書き換えたら編集不可になった: %s", line.Reason)
			}
			// 保存して読み直しても同じ姿になること。
			again := Parse(f.Bytes())
			reloaded, _ := again.Line(2)
			if reloaded.Text != line.Text || reloaded.Translation() != line.Translation() {
				t.Errorf("読み直すと違う: %q/%q vs %q/%q",
					reloaded.Text, reloaded.Translation(), line.Text, line.Translation())
			}
		})
	}
}

// TestSetTranslationSameValueIsNoOp は「差し替えても1バイトも変わらないなら
// 触らない」ことを見る。自動保存で無意味な書き込みを起こさないため。
func TestSetTranslationSameValueIsNoOp(t *testing.T) {
	f := Parse([]byte(sampleWorking))
	before := f.Bytes()
	if err := f.SetTranslation(5, "ああ"); err != nil {
		t.Fatalf("書き換えに失敗した: %v", err)
	}
	if f.Dirty() {
		t.Error("同じ値なのに Dirty が立った")
	}
	if !bytes.Equal(f.Bytes(), before) {
		t.Error("同じ値なのにバイトが変わった")
	}
}

// TestSetTranslationRejects は編集できない場合を固定する。
func TestSetTranslationRejects(t *testing.T) {
	const header = "key,section,node,order,speaker,translation\n"

	t.Run("列が多い行", func(t *testing.T) {
		f := Parse([]byte(header + "abc,,,,,,余分\n"))
		line, _ := f.Line(2)
		if line.Editable {
			t.Fatal("編集可になっている")
		}
		if !strings.Contains(line.Reason, "7列") {
			t.Errorf("理由が %q", line.Reason)
		}
		assertNotEditable(t, f.SetTranslation(2, "x"), 2)
	})

	t.Run("列が少ない行", func(t *testing.T) {
		f := Parse([]byte(header + "abc,訳\n"))
		line, _ := f.Line(2)
		if line.Editable {
			t.Fatal("編集可になっている")
		}
		assertNotEditable(t, f.SetTranslation(2, "x"), 2)
	})

	t.Run("列が合わない行があっても他の行は編集できる", func(t *testing.T) {
		f := Parse([]byte(header + "abc,訳\n0123456789abcdef,,,,,古い\n"))
		if err := f.SetTranslation(3, "新しい"); err != nil {
			t.Fatalf("書き換えに失敗した: %v", err)
		}
		broken, _ := f.Line(2)
		if broken.Text != "abc,訳\n" {
			t.Errorf("壊れた行が変わった: %q", broken.Text)
		}
	})

	t.Run("引用の中のカンマは列数に数えない", func(t *testing.T) {
		f := Parse([]byte(header + `0123456789abcdef,"L01, Ryan",n,1,Ryan,訳` + "\n"))
		line, _ := f.Line(2)
		if !line.Editable {
			t.Fatalf("編集不可になった: %s", line.Reason)
		}
	})

	t.Run("コメント行", func(t *testing.T) {
		f := Parse([]byte(header + "# x\n"))
		assertNotEditable(t, f.SetTranslation(2, "x"), 2)
	})

	t.Run("空行", func(t *testing.T) {
		f := Parse([]byte(header + "\n"))
		assertNotEditable(t, f.SetTranslation(2, "x"), 2)
	})

	t.Run("ヘッダー行", func(t *testing.T) {
		f := Parse([]byte(header))
		assertNotEditable(t, f.SetTranslation(1, "x"), 1)
	})

	t.Run("無い ID", func(t *testing.T) {
		f := Parse([]byte(header))
		for _, n := range []int{99, 0, -1} {
			assertNotEditable(t, f.SetTranslation(n, "x"), n)
		}
	})

	t.Run("改行を含む訳", func(t *testing.T) {
		for _, v := range []string{"a\nb", "a\rb", "a\r\nb", "a\n"} {
			f := Parse([]byte(header + "0123456789abcdef,,,,,\n"))
			var invalid *InvalidValueError
			if err := f.SetTranslation(2, v); !errors.As(err, &invalid) {
				t.Fatalf("SetTranslation(%q) = %v, want *InvalidValueError", v, err)
			}
			if f.Dirty() {
				t.Errorf("拒否したのに Dirty が立った: %q", v)
			}
		}
	})

	t.Run("読み取り専用ファイル", func(t *testing.T) {
		f := Parse([]byte("foo,bar\n1,2\n"))
		if err := f.SetTranslation(2, "x"); !errors.Is(err, ErrReadOnly) {
			t.Fatalf("SetTranslation = %v, want ErrReadOnly", err)
		}
		// 理由はデータ行にも載せておく。画面でそのまま出せるように。
		line, _ := f.Line(2)
		if line.Reason == "" {
			t.Error("データ行に理由が無い")
		}
	})

	t.Run("空ファイル", func(t *testing.T) {
		f := Parse(nil)
		if !f.ReadOnly() {
			t.Fatal("読み取り専用にならなかった")
		}
		if len(f.Lines()) != 0 {
			t.Errorf("行がある: %d", len(f.Lines()))
		}
		if err := f.SetTranslation(1, "x"); !errors.Is(err, ErrReadOnly) {
			t.Errorf("err = %v, want ErrReadOnly", err)
		}
	})

	t.Run("コメントだけのファイル", func(t *testing.T) {
		f := Parse([]byte("# only comments\n\n"))
		if !f.ReadOnly() {
			t.Fatal("読み取り専用にならなかった")
		}
	})
}

// TestLinesIsACopy は [File.Lines] の返り値を書き換えても内部が変わらないことを見る。
func TestLinesIsACopy(t *testing.T) {
	f := Parse([]byte(sampleWorking))
	lines := f.Lines()
	lines[4].Text = "壊す"
	lines[4].Fields[0] = "壊す"
	again := f.Lines()
	if again[4].Text == "壊す" || again[4].Fields[0] == "壊す" {
		t.Error("内部が書き換わった")
	}
}

// TestKindString は種類の名前を固定する。テストの失敗メッセージで使う。
func TestKindString(t *testing.T) {
	want := map[Kind]string{
		KindComment: "comment",
		KindBlank:   "blank",
		KindHeader:  "header",
		KindData:    "data",
		Kind(9):     "Kind(9)",
	}
	for k, s := range want {
		if got := k.String(); got != s {
			t.Errorf("Kind(%d).String() = %q, want %q", int(k), got, s)
		}
	}
}

// assertNotEditable は err が id の行の [NotEditableError] であることを確かめる。
// 行があれば、誤りの行番号がその行の最初の物理行であることも見る。
func assertNotEditable(t *testing.T, err error, id int) {
	t.Helper()
	var e *NotEditableError
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *NotEditableError", err)
	}
	if e.ID != id {
		t.Errorf("ID が %d, want %d", e.ID, id)
	}
	if e.Reason == "" {
		t.Error("理由が空")
	}
}

// TestReadOnlyCause は、読み取り専用にした理由を文面と識別子の両方で持ち、
// それがデータ行にも同じものとして載ることを見る。
//
// 画面はファイルの理由を目録で訳して出し、行の理由も同じ鍵で引く。ファイルと
// 行で別の理由を持つと、同じファイルについて見出しと行で違うことを言う。
func TestReadOnlyCause(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantID   string
		wantArgs []string
	}{
		{
			name:    "ヘッダー行が無い",
			content: "# コメントだけ\n\n",
			wantID:  reason.EditNoHeader,
		},
		{
			// 行番号は物理行で数える。コメントを飛ばした位置ではない。
			// ヘッダー行そのものは引用して渡す（目録の側で引用符を変えさせない）。
			name:     "受理されないヘッダー",
			content:  "# 先頭のコメント\nfoo,bar\n1,2\n",
			wantID:   reason.EditBadHeader,
			wantArgs: []string{"line", "2", "text", `"foo,bar"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Parse([]byte(tt.content))
			if !f.ReadOnly() {
				t.Fatal("読み取り専用にならなかった")
			}
			cause := f.ReadOnlyCause()
			if cause.ID != tt.wantID {
				t.Errorf("識別子が %q、期待 %q", cause.ID, tt.wantID)
			}
			if !slices.Equal(cause.Args, tt.wantArgs) {
				t.Errorf("置換が %q、期待 %q", cause.Args, tt.wantArgs)
			}
			if cause.Text != f.ReadOnlyReason() {
				t.Errorf("文面が2つに割れている: ReadOnlyReason %q / Cause.Text %q",
					f.ReadOnlyReason(), cause.Text)
			}
			for _, line := range f.Lines() {
				if line.Kind == KindData && (line.Cause.ID != cause.ID || line.Reason != cause.Text) {
					t.Errorf("%d行目の理由がファイルの理由と違う: %+v", line.Number, line.Cause)
				}
			}
			// 書き込みの誤りにも同じ理由が載る。CLI はこの文面をそのまま出す。
			err := f.SetTranslation(3, "x")
			if !errors.Is(err, ErrReadOnly) || !strings.Contains(err.Error(), cause.Text) {
				t.Errorf("SetTranslation = %v、ErrReadOnly と理由 %q を期待", err, cause.Text)
			}
		})
	}

	t.Run("編集できるファイルでは空", func(t *testing.T) {
		f := Parse([]byte(sampleWorking))
		if !f.ReadOnlyCause().Empty() {
			t.Errorf("読み取り専用でないのに理由がある: %+v", f.ReadOnlyCause())
		}
	})
}

// TestLineOutOfRange は、無い ID を引いても落ちずに「無い」と返すことを見る。
//
// ID は画面から届く値で、ファイルを読み直したあとの古い ID や、壊れた要求の
// 値も来る。範囲の外で添字を引くと、1つの要求でサーバーごと落ちる。
func TestLineOutOfRange(t *testing.T) {
	f := Parse([]byte(sampleWorking))
	total := len(f.Lines())

	for _, n := range []int{0, -1, total + 1, 1 << 30} {
		line, ok := f.Line(n)
		if ok {
			t.Errorf("Line(%d) が見つかったことになっている: %+v", n, line)
		}
		if line.Number != 0 || line.Text != "" || line.Fields != nil {
			t.Errorf("Line(%d) がゼロ値でない: %+v", n, line)
		}
	}
	// 境目の内側は引ける。
	for _, n := range []int{1, total} {
		if line, ok := f.Line(n); !ok || line.Number != n {
			t.Errorf("Line(%d) = %+v, %v", n, line, ok)
		}
	}
}
