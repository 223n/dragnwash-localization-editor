package publish

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// publishedCSV は公開ファイルを模した中身を作る。行は key,translation の組で渡す。
// 列は公開ファイルと同じ6列にする（[HeaderLine]）。
func publishedCSV(rows ...[2]string) string {
	var b strings.Builder
	b.WriteString(HeaderLine)
	b.WriteString("\n")
	for _, r := range rows {
		b.WriteString(r[0])
		b.WriteString(",UI,,,UI,")
		b.WriteString(r[1])
		b.WriteString("\n")
	}
	return b.String()
}

func TestCheckLoss(t *testing.T) {
	const keyA = "aaaaaaaaaaaaaaaa"
	const keyB = "bbbbbbbbbbbbbbbb"

	t.Run("行が消えると失われる", func(t *testing.T) {
		current := publishedCSV([2]string{keyA, "ある訳"}, [2]string{keyB, "もう1つ"})
		next := publishedCSV([2]string{keyB, "もう1つ"})

		got, err := CheckLoss("ja", []byte(current), []byte(next))
		if err != nil {
			t.Fatalf("CheckLoss: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("1件でない: %+v", got)
		}
		if got[0].Key != keyA || got[0].Locale != "ja" {
			t.Errorf("中身が違う: %+v", got[0])
		}
		if got[0].Why.ID != reason.PublishRowGone {
			t.Errorf("理由が %q", got[0].Why.ID)
		}
		// 行番号はいまの公開ファイルのもの。ヘッダーが1行目なので2行目。
		if got[0].Line != 2 {
			t.Errorf("行番号が %d", got[0].Line)
		}
	})

	t.Run("訳が空になると失われる", func(t *testing.T) {
		// いまの [Build] は訳が空の行を書かないので、この形は Build からは
		// 出てこない。守りの条件を「キーがあるかどうか」だけに狭めていないことを、
		// バイト列を直に渡して確かめる。
		current := publishedCSV([2]string{keyA, "ある訳"})
		next := publishedCSV([2]string{keyA, ""})

		got, err := CheckLoss("ja", []byte(current), []byte(next))
		if err != nil {
			t.Fatalf("CheckLoss: %v", err)
		}
		if len(got) != 1 || got[0].Why.ID != reason.PublishTranslationCleared {
			t.Fatalf("空になったことを見ていない: %+v", got)
		}
	})

	t.Run("訳が変わっただけなら失われない", func(t *testing.T) {
		// これがふつうの保存である。ここで止めると道具として使えない。
		current := publishedCSV([2]string{keyA, "まえの訳"})
		next := publishedCSV([2]string{keyA, "あとの訳"})

		got, err := CheckLoss("ja", []byte(current), []byte(next))
		if err != nil {
			t.Fatalf("CheckLoss: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("止めてはいけない: %+v", got)
		}
	})

	t.Run("行が増えるだけなら失われない", func(t *testing.T) {
		current := publishedCSV([2]string{keyA, "ある訳"})
		next := publishedCSV([2]string{keyA, "ある訳"}, [2]string{keyB, "あたらしい訳"})

		got, err := CheckLoss("ja", []byte(current), []byte(next))
		if err != nil {
			t.Fatalf("CheckLoss: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("止めてはいけない: %+v", got)
		}
	})

	t.Run("もともと訳が無い行は失うものが無い", func(t *testing.T) {
		current := publishedCSV([2]string{keyA, ""})
		next := publishedCSV()

		got, err := CheckLoss("ja", []byte(current), []byte(next))
		if err != nil {
			t.Fatalf("CheckLoss: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("止めてはいけない: %+v", got)
		}
	})

	t.Run("同じキーの行が2行あっても報告は1件", func(t *testing.T) {
		// publish は2行目以降を捨てる（先勝ち、R18）ので、2行が別々に
		// 失われるわけではない。
		current := publishedCSV([2]string{keyA, "ひとつめ"}, [2]string{keyA, "ふたつめ"})
		next := publishedCSV()

		got, err := CheckLoss("ja", []byte(current), []byte(next))
		if err != nil {
			t.Fatalf("CheckLoss: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("1件でない: %+v", got)
		}
	})

	t.Run("台詞ID行の大文字小文字は区別しない", func(t *testing.T) {
		// 引き当ては lineTable と同じ畳み方にそろえる。ここだけ区別すると、
		// 綴りの大小が違う台詞ID行を「失われた」と誤って報せる。
		current := publishedCSV([2]string{"line:AAAAAAAA", "台詞の訳"})
		next := publishedCSV([2]string{"line:aaaaaaaa", "台詞の訳"})

		got, err := CheckLoss("ja", []byte(current), []byte(next))
		if err != nil {
			t.Fatalf("CheckLoss: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("止めてはいけない: %+v", got)
		}
	})

	t.Run("キーを決められない行も失われる", func(t *testing.T) {
		// 16桁キーでも台詞IDでもない行。publish はこの行を捨てるので、
		// 訳は消える。報告に出す名前は key 列の値そのままにする。
		current := "key,section,node,order,speaker,translation\nnot-a-key,UI,,,UI,消える訳\n"
		next := publishedCSV()

		got, err := CheckLoss("ja", []byte(current), []byte(next))
		if err != nil {
			t.Fatalf("CheckLoss: %v", err)
		}
		if len(got) != 1 || got[0].Key != "not-a-key" {
			t.Fatalf("中身が違う: %+v", got)
		}
	})

	t.Run("いまの公開ファイルが空なら失うものが無い", func(t *testing.T) {
		got, err := CheckLoss("ja", nil, []byte(publishedCSV()))
		if err != nil {
			t.Fatalf("CheckLoss: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("止めてはいけない: %+v", got)
		}
	})

	t.Run("読めないヘッダーは誤りにする", func(t *testing.T) {
		// 列名が重複していると、どの列が訳なのかを決められない。
		// 「確かめられなかった」を「失われない」に落とさない。
		current := "key,key,node,order,speaker,translation\n" +
			keyA + ",UI,,,UI,ある訳\n"
		if _, err := CheckLoss("ja", []byte(current), []byte(publishedCSV())); err == nil {
			t.Error("誤りを返していない")
		}
	})
}

func TestCheckLossKeepsOnlyTheHeadOfTheTranslation(t *testing.T) {
	// 報告に訳を丸ごと持たせない。長い訳は先頭だけにして印を付ける。
	const long = "いちにさんしごろくしちはちきゅうじゅうじゅういちじゅうに"
	current := publishedCSV([2]string{"aaaaaaaaaaaaaaaa", long})

	got, err := CheckLoss("ja", []byte(current), []byte(publishedCSV()))
	if err != nil {
		t.Fatalf("CheckLoss: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("1件でない: %+v", got)
	}
	want := string([]rune(long)[:lossHeadRunes]) + lossHeadEllipsis
	if got[0].Head != want {
		t.Errorf("先頭が %q、期待 %q", got[0].Head, want)
	}
}

func TestCheckTargetLossWithoutOutputFile(t *testing.T) {
	// 出力先がまだ無いロケール。失うものが無いので何も返さない。
	root := t.TempDir()
	target := Target{
		Locale: "ja",
		Input:  filepath.Join(root, "in.csv"),
		Output: filepath.Join(root, "out.csv"),
	}

	got, err := CheckTargetLoss(target, []byte(publishedCSV()))
	if err != nil {
		t.Fatalf("CheckTargetLoss: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("止めてはいけない: %+v", got)
	}
}

func TestPublishedFileRoundTripLosesNothing(t *testing.T) {
	// 公開ファイル自身を入力にしたとき（作業コピーが無いときの既定）。
	// 冪等なので1件も失われない。ここが守りの「邪魔をしない」側の土台になる。
	root := t.TempDir()
	body := publishedCSV(
		[2]string{"aaaaaaaaaaaaaaaa", "ある訳"},
		[2]string{"line:aaaaaaaa", "台詞の訳"},
	)
	path := filepath.Join(root, TranslationsDir, "ja", StringsFile)
	writeFile(t, path, body)

	target := Target{Locale: "ja", Input: path, Output: path}
	out, _, err := BuildTarget(nil, target)
	if err != nil {
		t.Fatalf("BuildTarget: %v", err)
	}
	got, err := CheckTargetLoss(target, out)
	if err != nil {
		t.Fatalf("CheckTargetLoss: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("止めてはいけない: %+v", got)
	}
}

// TestCheckLossReadsTheOutputItWillWrite は、書き出そうとしている中身の側の
// 読み方を固定する。
//
// いまの公開ファイルの側は [TestCheckLoss] が見ている。こちらは「何が残るか」を
// 決める側で、ここを緩く読むと、残らない訳を残ると数えて守りを素通りさせる。
func TestCheckLossReadsTheOutputItWillWrite(t *testing.T) {
	const keyA = "aaaaaaaaaaaaaaaa"
	const keyB = "bbbbbbbbbbbbbbbb"
	current := []byte(publishedCSV([2]string{keyA, "ある訳"}, [2]string{keyB, "もう1つ"}))

	t.Run("書き出す中身のヘッダーが読めなければ誤りにする", func(t *testing.T) {
		// 列名が重複していると、どの列が訳なのかを決められない。
		// 「確かめられなかった」を「失われない」に落とさない。
		next := "key,Key,node,order,speaker,translation\n" + keyA + ",UI,,,UI,ある訳\n"
		got, err := CheckLoss("ja", current, []byte(next))
		if err == nil {
			t.Fatalf("誤りを返していない: %+v", got)
		}
		if got != nil {
			t.Errorf("誤りと一緒に結果を返している: %+v", got)
		}
	})

	t.Run("書き出す中身が空なら訳はすべて失われる", func(t *testing.T) {
		// 長さ0とヘッダーだけ。どちらも、公開ファイルを空にする書き出しである。
		for _, next := range [][]byte{nil, []byte(HeaderLine + "\n")} {
			got, err := CheckLoss("ja", current, next)
			if err != nil {
				t.Fatalf("CheckLoss: %v", err)
			}
			if len(got) != 2 {
				t.Fatalf("書き出す中身 %q で %d 件、2件を期待: %+v", next, len(got), got)
			}
			for i, want := range []struct {
				key  string
				line int
			}{{keyA, 2}, {keyB, 3}} {
				if got[i].Key != want.key || got[i].Line != want.line {
					t.Errorf("%d 件目が %+v、期待 キー %s・%d行目", i+1, got[i], want.key, want.line)
				}
				if got[i].Why.ID != reason.PublishRowGone {
					t.Errorf("%d 件目の理由が %q", i+1, got[i].Why.ID)
				}
			}
		}
	})

	t.Run("同じキーが2行あれば先の行を見る", func(t *testing.T) {
		// 先勝ち（R18）。後ろの空の行で上書きしたと見ると、残る訳を失われると報せる。
		next := publishedCSV(
			[2]string{keyA, "ある訳"},
			[2]string{keyA, ""},
			[2]string{keyB, "もう1つ"},
		)
		got, err := CheckLoss("ja", current, []byte(next))
		if err != nil {
			t.Fatalf("CheckLoss: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("止めてはいけない: %+v", got)
		}
	})

	t.Run("同じ訳でもキーの無い行に移ったものは残らない", func(t *testing.T) {
		// 残るかどうかは訳の中身ではなくキーで引く。キーの無い行は誰にも
		// 引かれないので、そこに同じ訳が書かれていても、ゲームには届かない。
		next := publishedCSV([2]string{"", "ある訳"}, [2]string{keyB, "もう1つ"})
		got, err := CheckLoss("ja", current, []byte(next))
		if err != nil {
			t.Fatalf("CheckLoss: %v", err)
		}
		if len(got) != 1 || got[0].Key != keyA {
			t.Errorf("キー %s だけが失われるはず: %+v", keyA, got)
		}
	})
}

// TestCheckTargetLossFailsWhenOutputIsUnreadable は、いまの公開ファイルが
// あるのに読めないとき、誤りを返すことを見る。
//
// 「ファイルが無い」は失うものが無いので黙ってよい。「あるのに読めない」は
// 確かめられていないので、同じ扱いにすると確かめられないほうが素通りする
// （dwloc publish はこの誤りで終了コード 2 を返し、書かない）。
func TestCheckTargetLossFailsWhenOutputIsUnreadable(t *testing.T) {
	root := t.TempDir()
	target := Target{
		Locale: "ja",
		Input:  filepath.Join(root, "in.csv"),
		// ディレクトリは読めない。
		Output: root,
	}

	got, err := CheckTargetLoss(target, []byte(publishedCSV()))
	if err == nil {
		t.Fatalf("誤りを返していない: %+v", got)
	}
	if got != nil {
		t.Errorf("誤りと一緒に結果を返している: %+v", got)
	}
}

// TestCheckTargetLossCatchesABrokenWorkingCopy は、壊れた作業コピーを入力に
// したときに、組み立てた結果を書く前に訳の消失を捕まえることを見る。
//
// doc.go の実測表と同じ形を、組み立て（[BuildTarget]）から通して確かめる。
// 守りを入れる前は、ヘッダーの引用符が閉じていない作業コピーが終了コード 0 で
// 通り、公開ファイルがヘッダー1行だけになっていた。壊れた入力ほど静かに消す。
func TestCheckTargetLossCatchesABrokenWorkingCopy(t *testing.T) {
	const keyA = "aaaaaaaaaaaaaaaa"
	const keyB = "bbbbbbbbbbbbbbbb"
	const keyC = "cccccccccccccccc"
	published := publishedCSV(
		[2]string{keyA, "ひとつめ"},
		[2]string{keyB, "ふたつめ"},
		[2]string{keyC, "みっつめ"},
	)
	const workingHeader = "key,section,node,order,speaker,source_en,translation\n"
	whole := workingHeader +
		keyA + ",UI,,,UI,,ひとつめ\n" +
		keyB + ",UI,,,UI,,ふたつめ\n" +
		keyC + ",UI,,,UI,,みっつめ\n"

	tests := []struct {
		name    string
		working string
		// wantLost は失われると報せるキー。空なら書き出してよい。
		wantLost []string
		// wantUnclosed は、組み立てそのものが閉じない引用符の誤りで止まることを期待するか。
		wantUnclosed bool
	}{
		{
			name:     "長さ0",
			working:  "",
			wantLost: []string{keyA, keyB, keyC},
		},
		{
			name:     "ヘッダーだけ",
			working:  workingHeader,
			wantLost: []string{keyA, keyB, keyC},
		},
		{
			name:     "途中まで",
			working:  workingHeader + keyA + ",UI,,,UI,,ひとつめ\n",
			wantLost: []string{keyB, keyC},
		},
		{
			// 行単位で読んでいたときは、source_en と translation が1つの列名に融合し、
			// translation 列を引けなくなって、全行が「訳が空」と見なされて落ちていた
			// （doc.go の実測表）。全体を解釈すると、ヘッダーがファイルの終わりまでを
			// 飲み込むので、読み手が閉じない引用符の誤りを返し、組み立てそのものが止まる。
			// cmd/dwloc と画面の書き出しは、その前に形の確かめ（CheckTargetShape）で止める。
			name:         "ヘッダーの引用符が閉じていない",
			working:      strings.Replace(whole, ",source_en,", `,"source_en,`, 1),
			wantUnclosed: true,
		},
		{
			name:     "公開ファイルにある行の訳を1つ空にした",
			working:  strings.Replace(whole, ",ふたつめ\n", ",\n", 1),
			wantLost: []string{keyB},
		},
		{
			// ふつうの編集。ここで止めると publish が使えない。
			name:    "訳を書き換えただけ",
			working: strings.Replace(whole, "ふたつめ", "二つ目", 1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, TranslationsDir, "ja", StringsFile), published)
			writeFile(t, filepath.Join(root, TranslationsDir, DiscoveredDir, "ja"+WorkingSuffix), tt.working)

			targets, err := DiscoverTargets(root)
			if err != nil {
				t.Fatalf("DiscoverTargets: %v", err)
			}
			if len(targets) != 1 || filepath.Base(targets[0].Input) != "ja"+WorkingSuffix {
				t.Fatalf("入力が作業コピーになっていない: %+v", targets)
			}
			out, _, err := BuildTarget(nil, targets[0])
			if tt.wantUnclosed {
				var unclosed *csvfile.UnclosedQuoteError
				if !errors.As(err, &unclosed) || unclosed.Line != 1 {
					t.Errorf("組み立てが閉じない引用符で止まらない: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildTarget: %v", err)
			}

			losses, err := CheckTargetLoss(targets[0], out)
			if err != nil {
				t.Fatalf("CheckTargetLoss: %v", err)
			}
			var got []string
			for _, l := range losses {
				got = append(got, l.Key)
			}
			if !slices.Equal(got, tt.wantLost) {
				t.Errorf("失われると報せたキーが %q、期待 %q", got, tt.wantLost)
			}
		})
	}
}
