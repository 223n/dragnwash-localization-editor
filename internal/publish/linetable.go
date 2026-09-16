package publish

import "github.com/223n/dragnwash-localization-editor/internal/csvfile"

// lineEntry は台詞ID（line:xxxxxxxx）1件ぶんの訳。
type lineEntry struct {
	// id は入力ファイルにあった綴り。R26 の出力はこちらを書く。
	id string
	// translation は訳。トリムしない生の値。
	translation string
	// removed は再生順の中に置けたかどうか。元実装の $lineRows.Remove($lid) に対応する。
	removed bool
}

// lineTable は台詞ID→訳の表。挿入順を保ち、引き当ては大文字小文字を区別しない。
//
// 元実装は `[ordered]@{}`（OrderedDictionary）で、挿入順を保ちつつ照合は
// OrdinalIgnoreCase になる（移植仕様「公開CSV生成 / 内部集約構造」）。挿入順は
// R26 の出力順に、大小無視は R12 の先勝ち判定と R24 の引き当てに効く。
//
// 台詞IDの判定（key.LooksLikeLineID）が接頭辞 "line:" を小文字に固定しているため、
// 大小が食い違いうるのはコロンより後ろの英字だけ。実データでは食い違わない。
type lineTable struct {
	entries []lineEntry
	// index は csvfile.FoldASCII で畳んだIDから entries の添字への対応。
	index map[string]int
	// removedCount は removed が立った件数。Remaining の件数を数え直さずに済ませる。
	removedCount int
}

func newLineTable() *lineTable {
	return &lineTable{index: make(map[string]int)}
}

// Add は台詞ID行を足す。同じIDが既にあれば何もしない（先勝ち、移植仕様 R12）。
//
// 訳が空かどうかの判定は呼び出し側で行う。元実装も「訳が非空」と
// 「まだ表に無い」の2条件を並べており、空の訳は表に入らない。
func (t *lineTable) Add(id, translation string) {
	folded := csvfile.FoldASCII(id)
	if _, dup := t.index[folded]; dup {
		return
	}
	t.index[folded] = len(t.entries)
	t.entries = append(t.entries, lineEntry{id: id, translation: translation})
}

// Lookup は台詞IDの訳を返す。表に無い場合と、既に [lineTable.Remove] 済みの場合は
// 第2戻り値が false になる。元実装の $lineRows.Contains($lid) に対応する。
func (t *lineTable) Lookup(id string) (string, bool) {
	i, ok := t.index[csvfile.FoldASCII(id)]
	if !ok || t.entries[i].removed {
		return "", false
	}
	return t.entries[i].translation, true
}

// Remove は台詞IDを「再生順の中に置けた」印を付けて表から外す。
// 元実装の $lineRows.Remove($lid) に対応し、残ったものが R26 の対象になる。
func (t *lineTable) Remove(id string) {
	i, ok := t.index[csvfile.FoldASCII(id)]
	if !ok || t.entries[i].removed {
		return
	}
	t.entries[i].removed = true
	t.removedCount++
}

// RemainingCount は外していない件数を返す。元実装の $lineRows.Count に対応する。
func (t *lineTable) RemainingCount() int {
	return len(t.entries) - t.removedCount
}

// Remaining は外していない項目を挿入順に返す。
func (t *lineTable) Remaining() []lineEntry {
	out := make([]lineEntry, 0, t.RemainingCount())
	for _, e := range t.entries {
		if e.removed {
			continue
		}
		out = append(out, e)
	}
	return out
}
