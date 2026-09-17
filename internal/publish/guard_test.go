package publish

import (
	"path/filepath"
	"strings"
	"testing"

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
