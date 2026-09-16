package diff

import (
	"errors"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/order"
)

// 引き継ぎ候補の試験に使う英文とそのキー。ハッシュは手で書かず key.For で作る。
// srcB2 / srcC2 は「原文が1文字変わってキーが変わった」状態を表す。
var (
	srcA  = "Line A"
	srcB  = "Line B"
	srcB2 = "Line B!"
	srcC  = "Line C"
	srcC2 = "Line C!"
	srcD  = "Line D"
	srcX  = "Line X"
	keyA  = key.For(srcA)
	keyB  = key.For(srcB)
	keyB2 = key.For(srcB2)
	keyC  = key.For(srcC)
	keyC2 = key.For(srcC2)
	keyD  = key.For(srcD)
	keyX  = key.For(srcX)
)

// fixedOldOrder は渡した中身をそのまま返す [OldOrderSource]。
//
// この穴があるおかげで、引き継ぎ候補のテストが git リポジトリを要らなくなる。
// 旧版の取り出しは外部プロセスの呼び出しで、そこを既定に固定してしまうと、
// 判定の規則を試すだけのテストまで git の有無に左右される。
func fixedOldOrder(csv string) OldOrderSource {
	return func(root, orderPath string) ([]byte, error) {
		return []byte(csv), nil
	}
}

// failingOldOrder は必ず失敗する [OldOrderSource]。旧版を取り出せない場合を作る。
func failingOldOrder(reason string) OldOrderSource {
	return func(root, orderPath string) ([]byte, error) {
		return nil, errors.New(reason)
	}
}

// introRow は L01 Ryan / Ryan_1_intro の再生順の1行を作る。
func introRow(orderText, lineID, k string) orderRow {
	return orderRow{"L01 Ryan", "intro", "Ryan_1_intro", orderText, lineID, k, "Ryan", ""}
}

// unusedRow は Unused / Start の再生順の1行を作る。
func unusedRow(orderText, lineID, k string) orderRow {
	return orderRow{"Unused", "", "Start", orderText, lineID, k, "Kobold", ""}
}

// publishedFour は4行ぶんの公開ファイルを組み立てる。
func publishedFour(keys ...string) string {
	out := publishedHeader
	labels := []string{"あ", "い", "う", "え"}
	orders := []string{"1", "2", "3", "4"}
	for i, k := range keys {
		out += k + ",L01 Ryan,Ryan_1_intro," + orders[i] + ",Ryan," + labels[i] + "\n"
	}
	return out
}

// carryOldOrder は「更新前」の再生順。台詞ID 0001〜0004 に A〜D が並ぶ。
// 各試験はここからの差分として「更新後」の再生順を書く。
var carryOldOrder = orderFile(
	introRow("1", "line:0001", keyA),
	introRow("2", "line:0002", keyB),
	introRow("3", "line:0003", keyC),
	introRow("4", "line:0004", keyD),
)

// carryKinds はそのロケールの引き継ぎ候補を「引き継ぎ元 → 移動か複製か」で取り出す。
func carryKinds(rep *Report, locale string) map[string]CarryKind {
	out := make(map[string]CarryKind)
	for _, f := range rep.Findings {
		if f.Locale == locale && f.Category == CatCarryover {
			out[f.Key] = f.CarryKind
		}
	}
	return out
}

// TestCarryoverRule は引き継ぎ候補の規則を、ゲーム更新で起きる形ごとに固定する。
//
// いちばん大事なのは誤検出を出さないこと。間違った引き継ぎ候補は、翻訳者に
// 間違った訳を別の行へ移させることになり、訳を失うより悪い結果になる。
// だから「出すべきものが出るか」と同じ重さで「出さないこと」を並べてある。
func TestCarryoverRule(t *testing.T) {
	published := publishedFour(keyA, keyB, keyC, keyD)

	tests := []struct {
		name string
		// oldOrder は更新前の再生順。空なら carryOldOrder を使う。
		oldOrder string
		// order は更新後の再生順。published は指定が無ければ4行の publishedFour。
		order     string
		published string
		// wantPairs は「引き継ぎ元 → 引き継ぎ先」。空なら候補なし。
		wantPairs map[string]string
		// wantKinds は「引き継ぎ元 → 移動か複製か」。wantPairs と同じ鍵をそろえる。
		wantKinds map[string]CarryKind
		// wantVanished は「台本から消えた行」の件数。候補が付いても減らさない。
		wantVanished int
	}{
		{
			name: "何も変わっていなければ候補は出ない",
			order: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyB),
				introRow("3", "line:0003", keyC),
				introRow("4", "line:0004", keyD),
			),
			wantPairs: map[string]string{},
		},
		{
			name: "同じ台詞IDでキーが変わったら、その1件を移動として候補にする",
			order: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyB2),
				introRow("3", "line:0003", keyC),
				introRow("4", "line:0004", keyD),
			),
			wantPairs:    map[string]string{keyB: keyB2},
			wantKinds:    map[string]CarryKind{keyB: CarryMoved},
			wantVanished: 1,
		},
		{
			name: "旧キーが別の行で生きていれば複製として候補にする",
			// 実データの d12499a3f17512de と同じ形。Ryan_1_intro の台詞が
			// 直されたが、同じ英文が Unused のノードにも置かれていて、
			// そちらは直されていない。
			oldOrder: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyB),
				introRow("3", "line:0003", keyC),
				introRow("4", "line:0004", keyD),
				unusedRow("1", "line:0009", keyB),
			),
			order: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyB2),
				introRow("3", "line:0003", keyC),
				introRow("4", "line:0004", keyD),
				unusedRow("1", "line:0009", keyB),
			),
			wantPairs: map[string]string{keyB: keyB2},
			wantKinds: map[string]CarryKind{keyB: CarryCopied},
			// 旧キーはいまの再生順にあるので「台本から消えた行」には出ない。
			wantVanished: 0,
		},
		{
			name: "行が消えただけなら候補にしない",
			order: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyB),
				// line:0003 の行ごと消えた。新しい台詞IDは1つも増えていない。
				introRow("3", "line:0004", keyD),
			),
			wantPairs:    map[string]string{},
			wantVanished: 1,
		},
		{
			name: "行が足されただけなら引き継ぎ元がない",
			order: orderFile(
				introRow("1", "line:0009", keyX),
				introRow("2", "line:0001", keyA),
				introRow("3", "line:0002", keyB),
				introRow("4", "line:0003", keyC),
				introRow("5", "line:0004", keyD),
			),
			wantPairs: map[string]string{},
		},
		{
			name: "会話の入れ替えは、台詞IDとキーの組が変わらないので候補にしない",
			order: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0003", keyC),
				introRow("3", "line:0002", keyB),
				introRow("4", "line:0004", keyD),
			),
			wantPairs: map[string]string{},
		},
		{
			name: "別のノードへ移っただけなら候補にしない",
			order: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0003", keyC),
				introRow("3", "line:0004", keyD),
				unusedRow("1", "line:0002", keyB),
			),
			wantPairs: map[string]string{},
		},
		{
			name: "同じ旧キーに2つの引き継ぎ先が付いたら、両方取り下げる",
			// 同じ英文の2つの行が、別々の英文に書き換えられた場合。
			// どちらへ移すか決められない。
			oldOrder: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyB),
				introRow("3", "line:0003", keyC),
				introRow("4", "line:0004", keyD),
				introRow("5", "line:0005", keyB),
			),
			order: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyB2),
				introRow("3", "line:0003", keyC),
				introRow("4", "line:0004", keyD),
				introRow("5", "line:0005", keyC2),
			),
			wantPairs:    map[string]string{},
			wantVanished: 1,
		},
		{
			name: "同じ引き継ぎ先に2つの引き継ぎ元が付いたら、両方取り下げる",
			// 別々だった2つの英文が、同じ英文に統一された場合。
			// どちらの訳を残すか決められない。
			order: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyX),
				introRow("3", "line:0003", keyX),
				introRow("4", "line:0004", keyD),
			),
			wantPairs:    map[string]string{},
			wantVanished: 2,
		},
		{
			name: "同じ旧キーの2行が同じ新キーになったら、候補は1件にまとまる",
			oldOrder: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyB),
				introRow("3", "line:0003", keyC),
				introRow("4", "line:0004", keyD),
				introRow("5", "line:0005", keyB),
			),
			order: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyB2),
				introRow("3", "line:0003", keyC),
				introRow("4", "line:0004", keyD),
				introRow("5", "line:0005", keyB2),
			),
			wantPairs:    map[string]string{keyB: keyB2},
			wantKinds:    map[string]CarryKind{keyB: CarryMoved},
			wantVanished: 1,
		},
		{
			name: "引き継ぎ先に既に訳があるなら候補にしない",
			// 移すと、いまある訳を上書きすることになる。
			order: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyC),
				introRow("3", "line:0003", keyC),
				introRow("4", "line:0004", keyD),
			),
			wantPairs:    map[string]string{},
			wantVanished: 1,
		},
		{
			name: "移す訳が無い行は候補にしない",
			published: publishedHeader +
				keyA + ",L01 Ryan,Ryan_1_intro,1,Ryan,あ\n" +
				keyB + ",L01 Ryan,Ryan_1_intro,2,Ryan,\n" +
				keyC + ",L01 Ryan,Ryan_1_intro,3,Ryan,う\n" +
				keyD + ",L01 Ryan,Ryan_1_intro,4,Ryan,え\n",
			order: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyB2),
				introRow("3", "line:0003", keyC),
				introRow("4", "line:0004", keyD),
			),
			wantPairs:    map[string]string{},
			wantVanished: 1,
		},
		{
			name: "旧版で同じ台詞IDに違うキーが並んでいたら、その台詞IDは使わない",
			// 手編集やマージで壊れた旧版。使うと無関係な台詞を結びつけてしまう。
			oldOrder: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyB),
				introRow("3", "line:0002", keyC),
				introRow("4", "line:0004", keyD),
			),
			order: orderFile(
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0002", keyB2),
				introRow("3", "line:0003", keyC),
				introRow("4", "line:0004", keyD),
			),
			wantPairs:    map[string]string{},
			wantVanished: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub := tt.published
			if pub == "" {
				pub = published
			}
			old := tt.oldOrder
			if old == "" {
				old = carryOldOrder
			}
			repo := newRepoWith(t, map[string]string{
				"data/script_order.csv":       tt.order,
				"Translations/ja/strings.csv": pub,
			}, Options{OldOrder: fixedOldOrder(old)})
			rep := Compare(repo, nil)

			sum := rep.Locales[0]
			if !sum.canJudge(CatCarryover) {
				t.Fatalf("旧再生順を渡したのに判定していない: %s", sum.OldOrderReason)
			}

			got := carryPairs(rep, "ja")
			if len(got) != len(tt.wantPairs) {
				t.Errorf("候補の件数が違う: got %d %v, want %d %v",
					len(got), got, len(tt.wantPairs), tt.wantPairs)
			}
			for from, to := range tt.wantPairs {
				if got[from] != to {
					t.Errorf("引き継ぎ先が違う: %s -> got %q, want %q", from, got[from], to)
				}
			}
			kinds := carryKinds(rep, "ja")
			for from, want := range tt.wantKinds {
				if kinds[from] != want {
					t.Errorf("移動と複製の区別が違う: %s -> got %v, want %v", from, kinds[from], want)
				}
			}
			if n := sum.Counts[CatCarryover]; n != len(tt.wantPairs) {
				t.Errorf("件数と Finding の数が合わない: Counts %d, pairs %d", n, len(tt.wantPairs))
			}
			// 内訳の合計が件数と一致すること。表示はこの2つで内訳を書く。
			if sum.CarryMoved+sum.CarryCopied != sum.Counts[CatCarryover] {
				t.Errorf("内訳の合計が件数と合わない: 移動 %d + 複製 %d != %d",
					sum.CarryMoved, sum.CarryCopied, sum.Counts[CatCarryover])
			}
			// 候補が付いても「台本から消えた行」は減らさない。既存の判定は動かさない。
			if n := sum.Counts[CatVanished]; n != tt.wantVanished {
				t.Errorf("台本から消えた行が違う: got %d, want %d", n, tt.wantVanished)
			}
		})
	}
}

// TestCarryoverFinding は候補1件の中身を確かめる。
//
// 引き継ぎ元の行をそのまま写すこと、引き継ぎ先を CarryTo と note の両方に
// 入れることを固定する。CSV は11列のままなので、表計算に貼る人は note 列で読む。
func TestCarryoverFinding(t *testing.T) {
	repo := newRepoWith(t, map[string]string{
		"data/script_order.csv": orderFile(
			introRow("1", "line:0001", keyA),
			introRow("2", "line:0002", keyB2),
		),
		"Translations/ja/strings.csv": publishedHeader +
			keyA + ",L01 Ryan,Ryan_1_intro,1,Ryan,あ\n" +
			keyB + ",L01 Ryan,Ryan_1_intro,2,Ryan,い\n",
	}, Options{OldOrder: fixedOldOrder(orderFile(
		introRow("1", "line:0001", keyA),
		introRow("2", "line:0002", keyB),
	))})
	rep := Compare(repo, nil)

	var f Finding
	for _, x := range rep.Findings {
		if x.Category == CatCarryover {
			f = x
		}
	}
	if f.Key != keyB {
		t.Fatalf("引き継ぎ元が違う: got %q, want %q", f.Key, keyB)
	}
	tests := []struct {
		name      string
		got, want string
	}{
		{"引き継ぎ先", f.CarryTo, keyB2},
		{"訳", f.Translation, "い"},
		{"section", f.Section, "L01 Ryan"},
		{"node", f.Node, "Ryan_1_intro"},
		{"order", f.OrderText, "2"},
		{"speaker", f.Speaker, "Ryan"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s が違う: got %q, want %q", tt.name, tt.got, tt.want)
		}
	}
	if f.CarryKind != CarryMoved {
		t.Errorf("移動のはず: got %v", f.CarryKind)
	}
	// note には引き継ぎ先のキーと、その新しい位置と、移動か複製かが入る。
	for _, want := range []string{keyB2, "L01 Ryan", "Ryan_1_intro", "2", "移動"} {
		if !strings.Contains(f.Note, want) {
			t.Errorf("note に %q が入っていない: %q", want, f.Note)
		}
	}

	// 同じ行が「台本から消えた行」にも残っていること。
	vanished := 0
	for _, x := range rep.Findings {
		if x.Category == CatVanished && x.Key == keyB {
			vanished++
		}
	}
	if vanished != 1 {
		t.Errorf("台本から消えた行に残っていない: %d 件", vanished)
	}

	// 出力の両方に引き継ぎ先が出ること。
	var text strings.Builder
	if err := rep.WriteText(&text, TextOptions{Root: repo.Root}); err != nil {
		t.Fatalf("text で書けない: %v", err)
	}
	for _, want := range []string{"引き継ぎ候補", "引き継ぎ先", keyB2, "移動 1 件"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text に %q が出ていない:\n%s", want, text.String())
		}
	}
	// 同じ1行が2つのカテゴリに出るので、締めは行数とのべ件数の両方を書く。
	if !strings.Contains(text.String(), "要確認が 1 行あります（カテゴリをまたぐ重なりを含めて、のべ 2 件）。") {
		t.Errorf("締めの数え方が違う:\n%s", text.String())
	}

	var csv strings.Builder
	if err := rep.WriteCSV(&csv); err != nil {
		t.Fatalf("csv で書けない: %v", err)
	}
	if !strings.Contains(csv.String(), "ja,carryover,review,"+keyB+",") {
		t.Errorf("csv の行が違う:\n%s", csv.String())
	}
	if lines := strings.Split(strings.TrimSuffix(csv.String(), "\n"), "\n"); lines[0] != CSVHeader {
		t.Errorf("CSV の列を増やしている: %q", lines[0])
	}
}

// TestCarryoverCopiedNote は、複製の候補が「元の訳を残す」と言うことを確かめる。
//
// 移動と複製を取り違えると、いまも再生される行の訳を消させることになる。
// 文面がそこまで踏み込んでいないと、翻訳者は旧行を消してよいか判断できない。
func TestCarryoverCopiedNote(t *testing.T) {
	repo := newRepoWith(t, map[string]string{
		"data/script_order.csv": orderFile(
			introRow("1", "line:0002", keyB2),
			unusedRow("1", "line:0009", keyB),
		),
		"Translations/ja/strings.csv": publishedHeader +
			keyB + ",L01 Ryan,Ryan_1_intro,2,Ryan,い\n",
	}, Options{OldOrder: fixedOldOrder(orderFile(
		introRow("1", "line:0002", keyB),
		unusedRow("1", "line:0009", keyB),
	))})
	rep := Compare(repo, nil)

	sum := rep.Locales[0]
	if sum.CarryCopied != 1 || sum.CarryMoved != 0 {
		t.Fatalf("複製1件になっていない: 移動 %d / 複製 %d", sum.CarryMoved, sum.CarryCopied)
	}
	// 旧キーは生きているので「台本から消えた行」には出ない。
	if n := sum.Counts[CatVanished]; n != 0 {
		t.Errorf("台本から消えた行に出ている: %d 件", n)
	}

	var f Finding
	for _, x := range rep.Findings {
		if x.Category == CatCarryover {
			f = x
		}
	}
	if f.CarryKind != CarryCopied {
		t.Fatalf("複製のはず: got %v", f.CarryKind)
	}
	for _, want := range []string{"複製", "残して"} {
		if !strings.Contains(f.Note, want) {
			t.Errorf("note に %q が入っていない: %q", want, f.Note)
		}
	}

	var text strings.Builder
	if err := rep.WriteText(&text, TextOptions{Root: repo.Root}); err != nil {
		t.Fatalf("text で書けない: %v", err)
	}
	if !strings.Contains(text.String(), "複製 1 件") {
		t.Errorf("内訳が出ていない:\n%s", text.String())
	}
	// カテゴリをまたぐ重なりが無いので、締めは行数だけになる。
	if !strings.Contains(text.String(), "要確認が 1 行あります。") {
		t.Errorf("締めの数え方が違う:\n%s", text.String())
	}
}

// TestCarryoverNeedsOldOrder は、旧再生順を取り出せないときに候補を出さず、
// 「0 件」ではなく理由を書くことを確かめる。
//
// 0 件と書くと「移すべき訳は無い」と読まれる。翻訳者はそれを信じて孤児の行を
// 捨てるので、黙って 0 件にするのがいちばん危ない。
func TestCarryoverNeedsOldOrder(t *testing.T) {
	const reason = "再生順の履歴が1版しかありません"
	repo := newRepoWith(t, map[string]string{
		"data/script_order.csv": orderFile(
			introRow("1", "line:0001", keyA),
			introRow("2", "line:0002", keyB2),
		),
		"Translations/ja/strings.csv": publishedHeader +
			keyA + ",L01 Ryan,Ryan_1_intro,1,Ryan,あ\n" +
			keyB + ",L01 Ryan,Ryan_1_intro,2,Ryan,い\n",
	}, Options{OldOrder: failingOldOrder(reason)})
	rep := Compare(repo, nil)

	sum := rep.Locales[0]
	if sum.canJudge(CatCarryover) {
		t.Error("旧再生順が無いのに判定している")
	}
	if n := sum.Counts[CatCarryover]; n != 0 {
		t.Errorf("候補 = %d 件, want 0", n)
	}
	// 旧版が無くても、旧版に頼らない判定はそのまま動くこと。
	if n := sum.Counts[CatVanished]; n != 1 {
		t.Errorf("台本から消えた行 = %d 件, want 1", n)
	}

	var b strings.Builder
	if err := rep.WriteText(&b, TextOptions{}); err != nil {
		t.Fatalf("WriteText が失敗した: %v", err)
	}
	if !strings.Contains(b.String(), "引き継ぎ候補") {
		t.Errorf("カテゴリが出ていない:\n%s", b.String())
	}
	if !strings.Contains(b.String(), "判定していません（"+reason+"）") {
		t.Errorf("理由が出ていない:\n%s", b.String())
	}
}

// TestCarryoverNeedsOrder は、いまの再生順を読めていないときにも候補を出さない
// ことを確かめる。読めていないと公開行のほぼ全部が孤児に見える。
func TestCarryoverNeedsOrder(t *testing.T) {
	repo := newRepoWith(t, map[string]string{
		"Translations/ja/strings.csv": publishedFour(keyA, keyB, keyC, keyD),
	}, Options{OldOrder: fixedOldOrder(carryOldOrder)})
	rep := Compare(repo, nil)

	sum := rep.Locales[0]
	if sum.canJudge(CatCarryover) {
		t.Error("再生順を読めていないのに判定している")
	}
	if n := sum.Counts[CatCarryover]; n != 0 {
		t.Errorf("候補 = %d 件, want 0", n)
	}

	var b strings.Builder
	if err := rep.WriteText(&b, TextOptions{}); err != nil {
		t.Fatalf("WriteText が失敗した: %v", err)
	}
	// 旧版は渡してあるので、止めている理由はいまの再生順のほう。
	if !strings.Contains(b.String(), "判定していません（再生順を読めていません）") {
		t.Errorf("理由が出ていない:\n%s", b.String())
	}
}

// TestCarryoverEmptyOldOrderIsBlocked は、旧版として取り出したものに台詞IDと
// キーの組が1つも無いときに、0 件ではなく保留になることを確かめる。
//
// 列名が変わった古い版や、取り違えた別のファイルがここへ来る。突き合わせる
// 手がかりが無いまま 0 件と書くと、「移すべき訳は無い」と読まれる。
func TestCarryoverEmptyOldOrderIsBlocked(t *testing.T) {
	repo := newRepoWith(t, map[string]string{
		"data/script_order.csv": orderFile(
			introRow("1", "line:0002", keyB2),
		),
		"Translations/ja/strings.csv": publishedHeader +
			keyB + ",L01 Ryan,Ryan_1_intro,2,Ryan,い\n",
	}, Options{OldOrder: fixedOldOrder("section,phase,node,order,line_id,key,speaker,condition\n")})
	rep := Compare(repo, nil)

	if rep.Locales[0].canJudge(CatCarryover) {
		t.Error("空の旧再生順で判定している")
	}
	if repo.OldOrderReason == "" {
		t.Error("理由が入っていない")
	}
}

// TestKeysByLineID は台詞IDからキーを引く表の作り方を固定する。
func TestKeysByLineID(t *testing.T) {
	tests := []struct {
		name string
		rows []orderRow
		want map[string]string
	}{
		{name: "空", want: map[string]string{}},
		{
			name: "そのまま引ける",
			rows: []orderRow{introRow("1", "line:0001", keyA), introRow("2", "line:0002", keyB)},
			want: map[string]string{"line:0001": keyA, "line:0002": keyB},
		},
		{
			name: "同じ台詞IDに同じキーが並ぶのは問題にしない",
			rows: []orderRow{introRow("1", "line:0001", keyA), unusedRow("1", "line:0001", keyA)},
			want: map[string]string{"line:0001": keyA},
		},
		{
			name: "同じ台詞IDに違うキーが並んだらその台詞IDを落とす",
			rows: []orderRow{
				introRow("1", "line:0001", keyA),
				introRow("2", "line:0001", keyB),
				introRow("3", "line:0002", keyC),
			},
			want: map[string]string{"line:0002": keyC},
		},
		{
			name: "台詞IDかキーが空の行は数えない",
			rows: []orderRow{introRow("1", "", keyA), introRow("2", "line:0002", "")},
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := order.LoadPowerShell([]byte(orderFile(tt.rows...)), nil)
			if err != nil {
				t.Fatalf("再生順を読めない: %v", err)
			}
			got := keysByLineID(data.Entries)
			if len(got) != len(tt.want) {
				t.Fatalf("件数が違う: got %v, want %v", got, tt.want)
			}
			for id, k := range tt.want {
				if got[id] != k {
					t.Errorf("%s: got %q, want %q", id, got[id], k)
				}
			}
		})
	}
}
