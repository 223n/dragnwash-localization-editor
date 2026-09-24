package diff

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/linekey"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// 引き継ぎ元の試験に使う英文。ゲームの台本は使わず、この試験のために作る。
//
//	Same    正規化すると旧と新が同じになる（記号と空白だけが変わった）
//	Similar 正規化は違うが指紋が近い（言い回しが変わった）
//	Far     指紋が上限より遠い（別の台詞）
const (
	srcGateOld = "The gate opens, when the crest is clean!"
	srcGateNew = "The gate opens when the crest is clean."
	srcWashOld = "Grab the sponge before the water gets cold."
	srcWashNew = "Grab the sponge before the water turns cold."
	srcFar     = "Ryan flies away and the level ends here now"
	// srcShort は正規化後が足切り（12文字）に届かない文。正規化しても新旧が
	// 別の文字列になるので、当たるとしたら指紋の層しかない（距離は 8）。
	srcShort    = "Yip, yip, yip!"
	srcShortNew = "Yip yip yop"
)

// carryRow は norm / fp / nlen つきの再生順の1行。
type carryRow struct {
	node, orderText, lineID, source, speaker string
}

// carryOrderFile は norm / fp / nlen を計算して script_order.csv を組み立てる。
//
// 値を手で書かないのは、列の作り方そのものを internal/linekey に確かめさせる
// ためである。書き写すと、移植がずれたときに試験も一緒にずれる。
func carryOrderFile(rows ...carryRow) string {
	var b strings.Builder
	b.WriteString("section,phase,node,order,line_id,key,speaker,condition,norm,fp,nlen\n")
	for _, r := range rows {
		n := linekey.Normalize(r.source)
		b.WriteString(strings.Join([]string{
			"L01 Ryan", "intro", r.node, r.orderText, r.lineID, key.For(r.source), r.speaker, "",
			linekey.NormalizedKey(r.source), linekey.FingerprintText(r.source),
			strconv.Itoa(utf8.RuneCountInString(n)),
		}, ","))
		b.WriteString("\n")
	}
	return b.String()
}

// carryOrderWithoutNorms は同じ行を norm / fp / nlen 列なしで組み立てる。
func carryOrderWithoutNorms(rows ...carryRow) string {
	var b strings.Builder
	b.WriteString("section,phase,node,order,line_id,key,speaker,condition\n")
	for _, r := range rows {
		b.WriteString(strings.Join([]string{
			"L01 Ryan", "intro", r.node, r.orderText, r.lineID, key.For(r.source), r.speaker, "",
		}, ","))
		b.WriteString("\n")
	}
	return b.String()
}

var carryOrderRows = []carryRow{
	{node: "Gate_1", orderText: "1", lineID: "line:gate0001", source: srcGateOld, speaker: "Ryan"},
	{node: "Wash_1", orderText: "1", lineID: "line:wash0001", source: srcWashOld, speaker: "Kobold"},
}

// carryPublished は旧キーの訳が残っている公開ファイル。
var carryPublished = publishedHeader +
	key.For(srcGateOld) + ",L01 Ryan,Gate_1,1,Ryan,紋章がきれいになるとゲートが開きます\n" +
	key.For(srcWashOld) + ",L01 Ryan,Wash_1,1,Kobold,お湯が冷める前にスポンジを取ってください\n"

// workingRow は作業コピーの1行を組み立てる。
//
// 値は publish と同じ規則で引用する。srcGateOld のようにカンマを含む原文を
// そのまま連ねると列がずれ、原文の後半が訳の列に入る。そうなると「訳が空の行」を
// 試しているつもりの試験が、訳のある行として別の理由で通ってしまう。
func workingRow(node, orderText, speaker, source, translation string) string {
	return csvfile.JoinFields(
		key.For(source), "L01 Ryan", node, orderText, speaker, source, translation,
	) + "\n"
}

func TestCarryFromCandidates(t *testing.T) {
	t.Run("正規化が一致した行", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       carryOrderFile(carryOrderRows...),
			"Translations/ja/strings.csv": carryPublished,
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Gate_1", "1", "Ryan", srcGateNew, ""),
		}, true)
		rep := Compare(repo, nil)

		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 1 {
			t.Fatalf("件数が違う: got %d, want 1", got)
		}
		f := findingOf(t, rep, CatCarryFrom)
		if f.Key != key.For(srcGateNew) {
			t.Errorf("報告が付く行が違う: got %s, want %s", f.Key, key.For(srcGateNew))
		}
		if f.CarryFrom != key.For(srcGateOld) {
			t.Errorf("引き継ぎ元が違う: got %s, want %s", f.CarryFrom, key.For(srcGateOld))
		}
		if f.NoteReason.ID != reason.NoteCarryFromSame {
			t.Errorf("注記が違う: %q (%q)", f.NoteReason.ID, f.Note)
		}
		// 旧キーの位置まで書く。キーだけでは翻訳者が候補を確かめられない。
		if !strings.Contains(f.Note, "Gate_1") {
			t.Errorf("位置が入っていない: %q", f.Note)
		}
	})

	t.Run("指紋が近い行", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       carryOrderFile(carryOrderRows...),
			"Translations/ja/strings.csv": carryPublished,
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Wash_1", "1", "Kobold", srcWashNew, ""),
		}, true)
		rep := Compare(repo, nil)

		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 1 {
			t.Fatalf("件数が違う: got %d, want 1", got)
		}
		f := findingOf(t, rep, CatCarryFrom)
		if f.CarryFrom != key.For(srcWashOld) {
			t.Errorf("引き継ぎ元が違う: got %s, want %s", f.CarryFrom, key.For(srcWashOld))
		}
		if f.NoteReason.ID != reason.NoteCarryFromSimilar {
			t.Errorf("注記が違う: %q (%q)", f.NoteReason.ID, f.Note)
		}
		// 距離を書くのは、翻訳者が見る順番を自分で決められるようにするため。
		want := linekey.Distance(linekey.Fingerprint(srcWashOld), linekey.Fingerprint(srcWashNew))
		if !strings.Contains(f.Note, strconv.Itoa(want)) {
			t.Errorf("距離 %d が注記に無い: %q", want, f.Note)
		}
	})

	t.Run("遠い行は候補にしない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       carryOrderFile(carryOrderRows...),
			"Translations/ja/strings.csv": carryPublished,
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Wash_1", "1", "Kobold", srcFar, ""),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 0 {
			t.Errorf("別の台詞を候補にした: got %d", got)
		}
	})

	t.Run("短い台詞は指紋で当てない", func(t *testing.T) {
		rows := []carryRow{{node: "Gate_1", orderText: "1", lineID: "line:short001", source: srcShort, speaker: "Ryan"}}
		repo := newRepo(t, map[string]string{
			"data/script_order.csv": carryOrderFile(rows...),
			"Translations/ja/strings.csv": publishedHeader +
				key.For(srcShort) + ",L01 Ryan,Gate_1,1,Ryan,すばらしい！\n",
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Gate_1", "1", "Ryan", srcShortNew, ""),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 0 {
			t.Errorf("足切りが効いていない: got %d", got)
		}
		// 足切りが無ければ当たる距離であることを確かめる。ここが上限より
		// 遠いと、この試験は足切りを何も確かめていないことになる。
		d := linekey.Distance(linekey.Fingerprint(srcShort), linekey.Fingerprint(srcShortNew))
		if d > linekey.MaxFuzzyDistance {
			t.Errorf("見本の距離が %d で、そもそも上限 %d を超えている", d, linekey.MaxFuzzyDistance)
		}
	})

	t.Run("ノードが違えば指紋で当てない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       carryOrderFile(carryOrderRows...),
			"Translations/ja/strings.csv": carryPublished,
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Wash_9", "1", "Kobold", srcWashNew, ""),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 0 {
			t.Errorf("別のノードの行を候補にした: got %d", got)
		}
	})

	t.Run("訳が入っている行は出さない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       carryOrderFile(carryOrderRows...),
			"Translations/ja/strings.csv": carryPublished,
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Gate_1", "1", "Ryan", srcGateNew, "もう訳がある"),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 0 {
			t.Errorf("訳のある行に引き継ぎ元を出した: got %d", got)
		}
	})

	t.Run("引き継ぎ元に訳が無ければ出さない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv": carryOrderFile(carryOrderRows...),
			// 旧キーの行はあるが訳が空。持ってくるものが無い。
			"Translations/ja/strings.csv": publishedHeader +
				key.For(srcGateOld) + ",L01 Ryan,Gate_1,1,Ryan,\n",
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Gate_1", "1", "Ryan", srcGateNew, ""),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 0 {
			t.Errorf("空の訳を引き継ぎ元にした: got %d", got)
		}
	})

	t.Run("キーが変わっていない行は出さない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       carryOrderFile(carryOrderRows...),
			"Translations/ja/strings.csv": carryPublished,
			// 再生順にあるキーそのもの。訳がまだ無いだけで、引き継ぎではない。
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Gate_1", "1", "Ryan", srcGateOld, ""),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 0 {
			t.Errorf("同じキーの行を候補にした: got %d", got)
		}
	})

	t.Run("台詞ID行は出さない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       carryOrderFile(carryOrderRows...),
			"Translations/ja/strings.csv": carryPublished,
			// 空の台詞ID行は正常な状態。埋めるよう促すと誤検出が一気に出る。
			"Translations/_discovered/ja.working.csv": workingHeader +
				"line:gate0001,L01 Ryan,Gate_1,1,Ryan," + srcGateNew + ",\n",
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 0 {
			t.Errorf("台詞ID行を候補にした: got %d", got)
		}
	})

	t.Run("正規化が同じ旧キーが2つあれば黙る", func(t *testing.T) {
		// 記号だけが違う2つの英文が、どちらも再生順にある。どちらの訳を
		// 持ってくるべきか決められない。
		rows := append([]carryRow{}, carryOrderRows...)
		rows = append(rows, carryRow{
			node: "Gate_1", orderText: "2", lineID: "line:gate0002",
			source: "The gate opens when the crest is clean!!", speaker: "Ryan",
		})
		repo := newRepo(t, map[string]string{
			"data/script_order.csv": carryOrderFile(rows...),
			"Translations/ja/strings.csv": carryPublished +
				key.For("The gate opens when the crest is clean!!") + ",L01 Ryan,Gate_1,2,Ryan,もうひとつの訳\n",
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Gate_1", "1", "Ryan", srcGateNew, ""),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 0 {
			t.Errorf("2つの引き継ぎ元から勝手に選んだ: got %d", got)
		}
	})

	t.Run("同じ英文が複数のノードにあってもキーが1つなら出す", func(t *testing.T) {
		// 行は2つだがキーは1つ。訳の出どころは決まる。
		rows := append([]carryRow{}, carryOrderRows...)
		rows = append(rows, carryRow{
			node: "Gate_2", orderText: "5", lineID: "line:gate0005", source: srcGateOld, speaker: "Ryan",
		})
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       carryOrderFile(rows...),
			"Translations/ja/strings.csv": carryPublished,
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Gate_1", "1", "Ryan", srcGateNew, ""),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 1 {
			t.Errorf("行の数で黙ってしまった: got %d, want 1", got)
		}
	})

	t.Run("norm 列が無ければ判定しない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       carryOrderWithoutNorms(carryOrderRows...),
			"Translations/ja/strings.csv": carryPublished,
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Gate_1", "1", "Ryan", srcGateNew, ""),
		}, true)
		rep := Compare(repo, nil)

		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 0 {
			t.Fatalf("norm 無しで判定した: got %d", got)
		}
		sum := rep.Locales[0]
		if sum.OrderNorms {
			t.Error("OrderNorms が立っている")
		}
		why := sum.JudgeBlockReason(CatCarryFrom)
		if why.ID != reason.JudgeOrderNoNorms {
			t.Errorf("理由が違う: %q (%q)", why.ID, why.Text)
		}
	})

	// 再生順が丸ごと読めないときは、norm 列が無いことも理由にしない。norm は
	// キーのある行からしか拾わないので、キーが無ければ norm も無いのは当たり前で、
	// 「norm 列がありません」と書くと、見出しの「再生順を読めていません」と
	// 食い違い、norm 列だけを直しに行かせる。csv の標準エラー（cmd/dwloc）も、
	// この理由を見て再生順の警告に任せる。
	t.Run("再生順を読めなければ、理由は norm 列ではなく再生順", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"Translations/ja/strings.csv": carryPublished,
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Gate_1", "1", "Ryan", srcGateNew, ""),
		}, true)
		rep := Compare(repo, nil)
		sum := rep.Locales[0]
		if !sum.HasWorking || sum.OrderKeys {
			t.Fatalf("前提が崩れている: 作業コピー %v / 再生順のキー %v", sum.HasWorking, sum.OrderKeys)
		}
		if sum.CanJudge(CatCarryFrom) {
			t.Fatal("再生順を読めないのに判定した")
		}
		if why := sum.JudgeBlockReason(CatCarryFrom); why.ID != reason.JudgeOrderUnreadable {
			t.Errorf("理由が違う: %q (%q)", why.ID, why.Text)
		}

		var b strings.Builder
		if err := rep.WriteText(&b, TextOptions{}); err != nil {
			t.Fatalf("WriteText が失敗した: %v", err)
		}
		want := pad(CatCarryFrom.String(), categoryNameWidth()) + "判定していません（再生順を読めていません）"
		if !strings.Contains(b.String(), want) {
			t.Errorf("本文に %q が無い:\n%s", want, b.String())
		}
		if strings.Contains(b.String(), "norm 列がありません") {
			t.Errorf("再生順を読めないのに norm 列のことを書いている:\n%s", b.String())
		}
	})

	t.Run("作業コピーが無ければ判定しない", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       carryOrderFile(carryOrderRows...),
			"Translations/ja/strings.csv": carryPublished,
		}, false)
		rep := Compare(repo, nil)
		if rep.Locales[0].CanJudge(CatCarryFrom) {
			t.Error("作業コピー無しで判定できることになっている")
		}
	})

	t.Run("同じキーが2行あっても1件", func(t *testing.T) {
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       carryOrderFile(carryOrderRows...),
			"Translations/ja/strings.csv": carryPublished,
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Gate_1", "1", "Ryan", srcGateNew, "") +
				workingRow("Gate_1", "1", "Ryan", srcGateNew, ""),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 1 {
			t.Errorf("件数が違う: got %d, want 1", got)
		}
	})

	t.Run("公開ファイルが無いロケールでは出さない", func(t *testing.T) {
		// 引き継ぎ元は同じロケールの公開ファイルの訳だけ。他のロケールに訳が
		// あっても、それはこのロケールへ持ってくる訳ではない。
		repo := newRepo(t, map[string]string{
			"data/script_order.csv":       carryOrderFile(carryOrderRows...),
			"Translations/de/strings.csv": carryPublished,
			"Translations/ja/.keep":       "",
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Gate_1", "1", "Ryan", srcGateNew, ""),
		}, true)
		rep := Compare(repo, []string{"ja"})
		sum := rep.Locales[0]
		if !sum.CanJudge(CatCarryFrom) {
			t.Fatal("作業コピーも norm 列もあるのに判定していない")
		}
		if got := sum.Counts[CatCarryFrom]; got != 0 {
			t.Errorf("他のロケールの訳を引き継ぎ元にした: got %d", got)
		}
	})

	t.Run("正規化すると空になる原文は突き合わせない", func(t *testing.T) {
		// 記号だけの台詞は正規化すると空になり、どれも同じ norm（空文字のハッシュ）
		// を持つ。突き合わせると "..." の訳を "!!!" に持ってくることになる。
		rows := append([]carryRow{}, carryOrderRows...)
		rows = append(rows, carryRow{node: "Gate_1", orderText: "2", lineID: "line:dots0001", source: "...", speaker: "Ryan"})
		if linekey.Normalize("...") != "" || linekey.Normalize("!!!") != "" {
			t.Fatal("見本の作り方が間違っている: 正規化しても空にならない")
		}
		repo := newRepo(t, map[string]string{
			"data/script_order.csv": carryOrderFile(rows...),
			"Translations/ja/strings.csv": carryPublished +
				key.For("...") + ",L01 Ryan,Gate_1,2,Ryan,……\n",
			"Translations/_discovered/ja.working.csv": workingHeader +
				workingRow("Gate_1", "3", "Ryan", "!!!", ""),
		}, true)
		rep := Compare(repo, nil)
		if got := counts(t, rep, "ja")[CatCarryFrom]; got != 0 {
			t.Errorf("記号だけの台詞どうしを結び付けた: got %d", got)
		}
	})
}

// TestPublishedTranslations は、引き継ぎ元の訳を引く表の作り方を固定する。
//
// ハッシュ行だけを入れる。台詞ID行は同じ英文を話者ごとに訳し分けるための行で、
// キーの訳そのものではない。同じキーが2行あれば先に出たほうを採る。
func TestPublishedTranslations(t *testing.T) {
	rows, err := ReadRows([]byte(publishedHeader +
		keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,一つ目\n" +
		keyHello + ",L01 Ryan,Ryan_1_intro,1,Ryan,二つ目\n" +
		"line:aaaa1111,L01 Ryan,Ryan_1_intro,1,Ryan,台詞ID行の訳\n" +
		"English,,,,,壊れた行の訳\n" +
		keyBye + ",L01 Ryan,Ryan_1_intro,2,Ryan,\n"))
	if err != nil {
		t.Fatalf("見本を読めない: %v", err)
	}

	got := publishedTranslations(Locale{Published: rows})
	want := map[string]string{keyHello: "一つ目", keyBye: ""}
	if len(got) != len(want) {
		t.Fatalf("件数が違う: got %v, want %v", got, want)
	}
	for k, v := range want {
		if tr, ok := got[k]; !ok || tr != v {
			t.Errorf("%s: got %q (%v), want %q", k, tr, ok, v)
		}
	}
}

// TestCarryFromSpeakerBreaksTies は、指紋が同じ距離で並んだときに話者で絞ることを見る。
func TestCarryFromSpeakerBreaksTies(t *testing.T) {
	// 同じノードに、同じ英文を別の話者が話す2行を置く。キーが違うので、
	// 話者で絞らなければ決められない。
	const otherOld = "Grab the sponge before the water gets cold!"
	rows := []carryRow{
		{node: "Wash_1", orderText: "1", lineID: "line:wash0001", source: srcWashOld, speaker: "Kobold"},
		{node: "Wash_1", orderText: "2", lineID: "line:wash0002", source: otherOld, speaker: "Ryan"},
	}
	files := map[string]string{
		"data/script_order.csv": carryOrderFile(rows...),
		"Translations/ja/strings.csv": publishedHeader +
			key.For(srcWashOld) + ",L01 Ryan,Wash_1,1,Kobold,コボルドの訳\n" +
			key.For(otherOld) + ",L01 Ryan,Wash_1,2,Ryan,ライアンの訳\n",
		"Translations/_discovered/ja.working.csv": workingHeader +
			workingRow("Wash_1", "1", "Kobold", srcWashNew, ""),
	}
	rep := Compare(newRepo(t, files, true), nil)
	if got := counts(t, rep, "ja")[CatCarryFrom]; got != 1 {
		t.Fatalf("件数が違う: got %d, want 1", got)
	}
	if f := findingOf(t, rep, CatCarryFrom); f.CarryFrom != key.For(srcWashOld) {
		t.Errorf("話者で絞れていない: got %s, want %s", f.CarryFrom, key.For(srcWashOld))
	}

	// 話者が空の行では絞れないので黙る。
	files["Translations/_discovered/ja.working.csv"] = workingHeader +
		workingRow("Wash_1", "1", "", srcWashNew, "")
	rep = Compare(newRepo(t, files, true), nil)
	if got := counts(t, rep, "ja")[CatCarryFrom]; got != 0 {
		t.Errorf("話者が分からないのに選んだ: got %d", got)
	}
}
