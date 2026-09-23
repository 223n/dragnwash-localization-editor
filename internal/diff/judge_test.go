package diff

import (
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// judgeReady は全部の材料がそろった Summary を返す。各試験はここから1つずつ欠く。
func judgeReady() Summary {
	return Summary{
		HasWorking: true, WorkingExists: true,
		OrderKeys: true, OrderLineIDs: true, OrderNorms: true,
		HasLayoutRisks: true, LayoutRisksExist: true,
		OldOrder: true,
	}
}

// TestJudgeBlockReason は、判定しなかった理由の選び方を固定する。
//
// 「作業コピーがありません」と「読んでいません」を分けるのは、後者に「ゲーム内で
// 書き出してください」と促すと済んだ作業をやり直させるため。見る順は canJudge と
// 同じにしてある。引き継ぎ候補は再生順と旧再生順の両方を要るので、順が食い違うと
// 「再生順は読めているのに再生順を読めていません」と書くことになる。
func TestJudgeBlockReason(t *testing.T) {
	tests := []struct {
		name     string
		category Category
		edit     func(*Summary)
		wantID   string
		wantText string
	}{
		{
			name: "作業コピーを読んでいない", category: CatUntranslated,
			edit:   func(s *Summary) { s.HasWorking = false },
			wantID: reason.JudgeWorkingNotRead, wantText: "作業コピーを読んでいません",
		},
		{
			name: "作業コピーが無い", category: CatUntranslated,
			edit:   func(s *Summary) { s.HasWorking, s.WorkingExists = false, false },
			wantID: reason.JudgeWorkingMissing, wantText: "作業コピーがありません",
		},
		{
			name: "再生順のキーが無い", category: CatVanished,
			edit:   func(s *Summary) { s.OrderKeys = false },
			wantID: reason.JudgeOrderUnreadable, wantText: "再生順を読めていません",
		},
		{
			name: "再生順の台詞IDが無い", category: CatStrayLineID,
			edit:   func(s *Summary) { s.OrderLineIDs = false },
			wantID: reason.JudgeOrderUnreadable, wantText: "再生順を読めていません",
		},
		{
			name: "はみ出しの記録を読んでいない", category: CatLayoutRisk,
			edit:   func(s *Summary) { s.HasLayoutRisks = false },
			wantID: reason.JudgeLayoutRisksNotRead, wantText: "はみ出しの記録を読んでいません",
		},
		{
			name: "はみ出しの記録が無い", category: CatLayoutRisk,
			edit:   func(s *Summary) { s.HasLayoutRisks, s.LayoutRisksExist = false, false },
			wantID: reason.JudgeNoLayoutRisks, wantText: "ゲーム内で Check translation layout を走らせた記録がありません",
		},
		{
			name: "norm 列が無い", category: CatCarryFrom,
			edit:   func(s *Summary) { s.OrderNorms = false },
			wantID: reason.JudgeOrderNoNorms, wantText: "再生順に norm 列がありません",
		},
		{
			name: "引き継ぎ元の候補は作業コピーを先に見る", category: CatCarryFrom,
			edit:   func(s *Summary) { s.HasWorking, s.WorkingExists, s.OrderNorms = false, false, false },
			wantID: reason.JudgeWorkingMissing, wantText: "作業コピーがありません",
		},
		{
			name: "旧再生順の理由はそのまま運ぶ", category: CatCarryover,
			edit: func(s *Summary) {
				s.OldOrder, s.OldOrderReason, s.OldOrderReasonID = false, ErrOnlyOneVersion.Error(), reason.OldOrderOnlyOneVersion
			},
			wantID: reason.OldOrderOnlyOneVersion, wantText: ErrOnlyOneVersion.Error(),
		},
		{
			name: "名前の無い理由は識別子を空のまま運ぶ", category: CatCarryover,
			edit:   func(s *Summary) { s.OldOrder, s.OldOrderReason = false, "自前の理由" },
			wantID: "", wantText: "自前の理由",
		},
		{
			name: "旧再生順が無く理由も無ければ一般の理由", category: CatCarryover,
			edit:   func(s *Summary) { s.OldOrder = false },
			wantID: reason.JudgeOldOrderUnreadable, wantText: "1つ前の再生順を読めていません",
		},
		{
			name: "旧再生順を疑ったときもその理由を出す", category: CatCarryover,
			edit: func(s *Summary) {
				s.OldOrderStale, s.OldOrderReason, s.OldOrderReasonID = true, staleOldOrderReason, reason.OldOrderStale
			},
			wantID: reason.OldOrderStale, wantText: staleOldOrderReason,
		},
		{
			name: "引き継ぎ候補はいまの再生順を先に見る", category: CatCarryover,
			edit: func(s *Summary) {
				s.OrderKeys, s.OldOrder, s.OldOrderReason, s.OldOrderReasonID = false, false, ErrNoGit.Error(), reason.OldOrderNoGit
			},
			wantID: reason.JudgeOrderUnreadable, wantText: "再生順を読めていません",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sum := judgeReady()
			tt.edit(&sum)
			if sum.CanJudge(tt.category) {
				t.Fatalf("前提が崩れている: %s を判定できることになっている", tt.category)
			}
			got := sum.JudgeBlockReason(tt.category)
			if got.ID != tt.wantID || got.Text != tt.wantText {
				t.Errorf("got %q (%q), want %q (%q)", got.ID, got.Text, tt.wantID, tt.wantText)
			}
		})
	}
}

// TestJudgeBlockReasonNamesAFailingCondition は、判定しなかったときの理由が、
// 実際に欠けている材料を指すことを、材料の有無の全部の組み合わせで確かめる。
//
// 理由が食い違うと、翻訳者は欠けていない材料を探しに行く（「作業コピーを
// 書き出してください」と言われて、既にある作業コピーを書き出し直す）。
// canJudge と judgeBlockReason は同じ条件を別々に書いているので、どちらかに
// 条件を足したときのずれをここで捕まえる。
func TestJudgeBlockReasonNamesAFailingCondition(t *testing.T) {
	const customID = "test_custom"
	flags := []func(*Summary, bool){
		func(s *Summary, v bool) { s.HasWorking = v },
		func(s *Summary, v bool) { s.WorkingExists = v },
		func(s *Summary, v bool) { s.OrderKeys = v },
		func(s *Summary, v bool) { s.OrderLineIDs = v },
		func(s *Summary, v bool) { s.OrderNorms = v },
		func(s *Summary, v bool) { s.HasLayoutRisks = v },
		func(s *Summary, v bool) { s.LayoutRisksExist = v },
		func(s *Summary, v bool) { s.OldOrder = v },
		func(s *Summary, v bool) { s.OldOrderStale = v },
		func(s *Summary, v bool) {
			if v {
				s.OldOrderReason, s.OldOrderReasonID = "取り出せない理由", customID
			}
		},
	}

	for bits := 0; bits < 1<<len(flags); bits++ {
		var sum Summary
		for i, set := range flags {
			set(&sum, bits&(1<<i) != 0)
		}
		for _, c := range categories {
			if sum.CanJudge(c) {
				continue
			}
			got := sum.JudgeBlockReason(c)
			if got.Text == "" {
				t.Fatalf("%s %+v: 理由の文面が空", c, sum)
			}
			oldMissing := c.needsOldOrder() && (!sum.OldOrder || sum.OldOrderStale)
			var holds bool
			switch got.ID {
			case reason.JudgeWorkingNotRead:
				holds = c.needsWorking() && !sum.HasWorking && sum.WorkingExists
			case reason.JudgeWorkingMissing:
				holds = c.needsWorking() && !sum.HasWorking && !sum.WorkingExists
			case reason.JudgeOrderUnreadable:
				holds = (c.needsOrderKeys() && !sum.OrderKeys) || (c.needsOrderLineIDs() && !sum.OrderLineIDs)
			case reason.JudgeLayoutRisksNotRead:
				holds = c.needsLayoutRisks() && !sum.HasLayoutRisks && sum.LayoutRisksExist
			case reason.JudgeNoLayoutRisks:
				holds = c.needsLayoutRisks() && !sum.HasLayoutRisks && !sum.LayoutRisksExist
			case reason.JudgeOrderNoNorms:
				holds = c.needsOrderNorms() && !sum.OrderNorms
			case reason.JudgeOldOrderUnreadable:
				holds = oldMissing && sum.OldOrderReason == ""
			case customID:
				holds = oldMissing && sum.OldOrderReason != ""
			}
			if !holds {
				t.Fatalf("%s %+v: 欠けていない材料を理由にしている: %q (%q)", c, sum, got.ID, got.Text)
			}
		}
	}
}

// TestStatusID は画面が鍵に使う識別子が、CSV の status 列と同じ値であることを
// 確かめる。日本語名を鍵にすると、表示名を変えた瞬間に対応が切れる。
func TestStatusID(t *testing.T) {
	tests := []struct {
		status Status
		id     string
		name   string
	}{
		{StatusTodo, "todo", "要作業"},
		{StatusReview, "review", "要確認"},
		{StatusInfo, "info", "参考"},
		// 知らない値は最も軽い「参考」に倒す。空の識別子を返すと画面の類名が壊れる。
		{Status(99), "info", "参考"},
	}
	for _, tt := range tests {
		if got := tt.status.ID(); got != tt.id {
			t.Errorf("%d の識別子: got %q, want %q", int(tt.status), got, tt.id)
		}
		if got := tt.status.String(); got != tt.name {
			t.Errorf("%d の表示名: got %q, want %q", int(tt.status), got, tt.name)
		}
	}
	for _, c := range categories {
		if got := c.Status().ID(); got != c.Status().id() {
			t.Errorf("%s: ID と CSV の値が違う: %q / %q", c, got, c.Status().id())
		}
	}
}
