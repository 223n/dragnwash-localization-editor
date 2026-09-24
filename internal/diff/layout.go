package diff

import (
	"strconv"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/reason"
)

// このファイルは「はみ出しの恐れがある行」（[CatLayoutRisk]）を作る。
//
// # 出どころ
//
// ゲーム内の F1 → Translation → Check translation layout が
// Translations/_discovered/layout_risks.csv を書く。列は
// source_en,translation,axis,required_px,available_px,ratio,object_path で、
// 翻訳リポジトリの CONTRIBUTING.ja.md が「ratio が大きいものほどはみ出しが
// 大きいので、訳文を短くする等で調整してください」と書いている。
//
// この道具が読むのは、直す作業がこの道具の中で起きるからである。いまは別の CSV を
// 開いて ratio の大きい行を探し、原文か訳の文字を頼りに画面へ戻ることになる。
// 行に結び付けてしまえば、絞り込んで短く直すところまでが1つの画面で済む。
//
// # どのロケールの結果かをどう決めるか
//
// このファイルはロケールの列を持たない。ゲームでそのとき選んでいた言語の結果が
// 1つのファイルに上書きで書かれる。だから「いま読んでいるロケールの結果だ」と
// 決めつけずに、行の translation 列が、そのロケールの同じキーの訳と1字も違わない
// ことを確かめてから結び付ける。
//
// 確かめられなかった行は数にも入れない。別の言語で走らせた結果を、そのまま
// 別の言語の警告として出すと、直しようのない指摘になる。
//
// 訳を直したあとの行も、同じ理由で外れる。測ったのは直す前の訳なので、
// 直した訳がまだはみ出すかどうかは分からない。もう一度ゲームで測ってもらう。

// LayoutRisk は layout_risks.csv の1行。
//
// 数として扱うのは [LayoutRisk.Ratio] だけである。ほかを文字のまま持つのは、
// ゲームが書いた値をそのまま出すためで、単位や桁を書き換えると、翻訳者が
// ファイルと見比べたときに食い違う。
type LayoutRisk struct {
	// SourceEn は測った文字の原文。キーを導くのに使う。
	SourceEn string
	// Translation は測ったときの訳。どのロケールの結果かを確かめるのに使う。
	Translation string
	// Axis はどちら向きにはみ出すか（ゲームが書いた綴りのまま）。
	Axis string
	// RequiredPx は訳を出すのに要る大きさ（同上）。
	RequiredPx string
	// AvailablePx は使える大きさ（同上）。
	AvailablePx string
	// RatioText は ratio 列の生の値。
	RatioText string
	// Ratio は [LayoutRisk.RatioText] を数に直したもの。読めなければ 0。
	//
	// 同じキーに複数の行（別の軸、別の場所）が付くことがあるので、どれを
	// 報告するかを決めるのに使う。
	Ratio float64
	// ObjectPath は画面のどこにある文字か（同上）。注記には入れない。長いので、
	// 読みたいときは layout_risks.csv を直接見てもらう。
	ObjectPath string
}

// ParseLayoutRisks は layout_risks.csv を読む。
//
// 原文が空の行は捨てる。キーを導けないので、どの行の話か決められない。
// 読み方は公開ファイルや作業コピーと同じ（[csvfile.ReadPowerShell]）。原文が行を
// またいでも1つの値として読むので、作業コピーの行と同じキーで結び付く。
func ParseLayoutRisks(data []byte) ([]LayoutRisk, error) {
	f, err := csvfile.ReadPowerShell(data)
	if err != nil {
		return nil, err
	}
	records := f.Rows()
	out := make([]LayoutRisk, 0, len(records))
	for _, rec := range records {
		source := rec.Get("source_en")
		if source == "" {
			continue
		}
		ratioText := rec.Get("ratio")
		ratio, err := strconv.ParseFloat(strings.TrimSpace(ratioText), 64)
		if err != nil {
			ratio = 0
		}
		out = append(out, LayoutRisk{
			SourceEn:    source,
			Translation: rec.Get("translation"),
			Axis:        rec.Get("axis"),
			RequiredPx:  rec.Get("required_px"),
			AvailablePx: rec.Get("available_px"),
			RatioText:   ratioText,
			Ratio:       ratio,
			ObjectPath:  rec.Get("object_path"),
		})
	}
	return out, nil
}

// cause は Finding.Note と Finding.NoteReason に入れる理由を組み立てる。
//
// 比を先に置くのは、翻訳者がまずそこで見る順番を決めるからである
// （CONTRIBUTING.ja.md「ratio が大きいものほどはみ出しが大きい」）。
// 大きさの2つを添えるのは、どれだけ短くすればよいかの見当を付けるため。
func (r LayoutRisk) cause() reason.Reason {
	return reason.New(reason.NoteLayoutRisk,
		"はみ出しの恐れ（比 "+r.RatioText+"、"+r.Axis+" 方向、要 "+r.RequiredPx+
			" / 使える "+r.AvailablePx+"）",
		"ratio", r.RatioText, "axis", r.Axis,
		"required", r.RequiredPx, "available", r.AvailablePx)
}

// layoutRiskFindings は「はみ出しの恐れがある行」を集める。
//
// 結び付ける相手は publish の入力になる側と同じで、作業コピーを読んでいれば
// その行、読んでいなければ公開ファイルの行である（[tagFindings] と同じ）。
// ゲームは作業コピーを書き換えると読み直すので、測られた訳は作業コピーの側に
// あることが多い。
//
// 1つのキーに複数の行が付いたときは、比がいちばん大きい行を報告する。
// 軸ごと・場所ごとに1件ずつ出すと、翻訳者が直すのは1つの訳なのに、同じ行が
// 何件も並ぶ。残りは layout_risks.csv に全部ある。
func layoutRiskFindings(idx *orderIndex, loc Locale) []Finding {
	if !loc.HasLayoutRisks {
		return nil
	}
	// キーごとに、比がいちばん大きい行を選ぶ。
	worst := make(map[string]LayoutRisk, len(loc.LayoutRisks))
	for _, risk := range loc.LayoutRisks {
		k := key.For(risk.SourceEn)
		if got, seen := worst[k]; seen && got.Ratio >= risk.Ratio {
			continue
		}
		worst[k] = risk
	}

	var out []Finding
	seen := make(map[string]struct{})
	report := func(k string, f Finding, translation string) {
		if k == "" || translation == "" {
			return
		}
		if _, dup := seen[k]; dup {
			return
		}
		risk, ok := worst[k]
		if !ok || risk.Translation != translation {
			// 測ったときの訳と、いま読んでいる訳が違う。別の言語で走らせた
			// 結果か、測ったあとに直した行である。どちらも、この行の警告として
			// は出せない。
			return
		}
		seen[k] = struct{}{}
		cause := risk.cause()
		f.Note, f.NoteReason = cause.Text, cause
		out = append(out, f)
	}
	if loc.HasWorking {
		for _, row := range loc.Working {
			k, adopted := PublishKey(row)
			if !adopted {
				continue
			}
			report(k, workingFinding(idx, row), row.Translation)
		}
		return out
	}
	for _, row := range loc.Published {
		if row.Kind == KindBroken {
			continue
		}
		report(row.Key, publishedFinding(row), row.Translation)
	}
	return out
}
