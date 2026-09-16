package diff

import (
	"errors"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
)

// publishedHeader は公開ファイルのヘッダー（6列）。
const publishedHeader = "key,section,node,order,speaker,translation\n"

// workingHeader は作業コピーのヘッダー（7列）。source_en がある点だけが違う。
const workingHeader = "key,section,node,order,speaker,source_en,translation\n"

func TestReadRows(t *testing.T) {
	hash := key.For("Hello")

	tests := []struct {
		name string
		csv  string
		want []Row
	}{
		{
			name: "空ファイル",
			csv:  "",
			want: nil,
		},
		{
			name: "ヘッダーだけ",
			csv:  publishedHeader,
			want: nil,
		},
		{
			name: "ハッシュ行",
			csv:  publishedHeader + hash + ",L01 Ryan,Ryan_1_intro,3,Ryan,こんにちは\n",
			want: []Row{{
				Key: hash, Kind: KindHash,
				Section: "L01 Ryan", Node: "Ryan_1_intro", OrderText: "3",
				Speaker: "Ryan", Translation: "こんにちは",
			}},
		},
		{
			name: "大文字のキーは小文字に直す",
			csv:  publishedHeader + "0DA72197E898EBE1,UI,,,UI,訳\n",
			want: []Row{{
				Key: "0da72197e898ebe1", Kind: KindHash,
				Section: "UI", Speaker: "UI", Translation: "訳",
			}},
		},
		{
			name: "キーの前後の空白は落とす",
			csv:  publishedHeader + "  " + hash + "  ,UI,,,UI,訳\n",
			want: []Row{{Key: hash, Kind: KindHash, Section: "UI", Speaker: "UI", Translation: "訳"}},
		},
		{
			name: "台詞ID行は小文字化しない",
			csv:  publishedHeader + "line:A8779eBF,L01 Ryan,Ryan_1_intro,1,Ryan,訳\n",
			want: []Row{{
				Key: "line:A8779eBF", Kind: KindLineID,
				Section: "L01 Ryan", Node: "Ryan_1_intro", OrderText: "1",
				Speaker: "Ryan", Translation: "訳",
			}},
		},
		{
			name: "16桁hexでも line: でもない行",
			csv:  publishedHeader + "English,,,,,訳\n",
			want: []Row{{Key: "english", Kind: KindBroken, Translation: "訳"}},
		},
		{
			name: "キーが空の行も捨てない",
			csv:  workingHeader + ",,,,,Hello,\n",
			want: []Row{{Key: "", Kind: KindBroken, SourceEn: "Hello"}},
		},
		{
			name: "key 列が無いファイル",
			csv:  "source_en,translation\nHello,こんにちは\n",
			want: []Row{{Key: "", Kind: KindBroken, SourceEn: "Hello", Translation: "こんにちは"}},
		},
		{
			name: "列名の大文字小文字は問わない",
			csv:  "Key,Section,Node,Order,Speaker,Source_EN,Translation\n" + hash + ",UI,,,UI,Hello,訳\n",
			want: []Row{{
				Key: hash, Kind: KindHash, Section: "UI", Speaker: "UI",
				SourceEn: "Hello", Translation: "訳",
			}},
		},
		{
			name: "訳の前後の空白は落とさない",
			csv:  publishedHeader + hash + `,UI,,,UI," 訳 "` + "\n",
			want: []Row{{Key: hash, Kind: KindHash, Section: "UI", Speaker: "UI", Translation: " 訳 "}},
		},
		{
			name: "コメント行は読まない",
			csv:  publishedHeader + "# ===== UI =====\n" + hash + ",UI,,,UI,訳\n",
			want: []Row{{Key: hash, Kind: KindHash, Section: "UI", Speaker: "UI", Translation: "訳"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadRows([]byte(tt.csv))
			if err != nil {
				t.Fatalf("読めない: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("行数が違う: got %d, want %d（%+v）", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("%d 行目が違う:\n got  %+v\n want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestReadRowsDuplicateColumn はヘッダーの列名が重複しているときだけエラーに
// なることを確かめる。publish と同じ扱いで、CLI はこれを終了コード2にする。
func TestReadRowsDuplicateColumn(t *testing.T) {
	_, err := ReadRows([]byte("key,key,translation\na,b,c\n"))
	if err == nil {
		t.Fatal("重複した列名をエラーにしていない")
	}
	var dup *csvfile.DuplicateColumnError
	if !errors.As(err, &dup) {
		t.Fatalf("csvfile.DuplicateColumnError ではない: %v", err)
	}
}

func TestDroppedReason(t *testing.T) {
	hash := key.For("Hello")
	other := key.For("Goodbye")

	tests := []struct {
		name        string
		row         Row
		wantDropped bool
		wantNote    string
	}{
		{
			name: "原文とキーが一致する行は残る",
			row:  Row{Key: hash, Kind: KindHash, SourceEn: "Hello"},
		},
		{
			name:        "原文とキーが食い違う行は捨てられる",
			row:         Row{Key: other, Kind: KindHash, SourceEn: "Hello"},
			wantDropped: true, wantNote: noteDroppedMismatch,
		},
		{
			name: "キーが空で原文がある行は捨てられない",
			// publish はこの行のキーを原文のハッシュから作って採用する。
			// KindBroken を一律に捨てると、ここで publish に対する嘘になる。
			row: Row{Key: "", Kind: KindBroken, SourceEn: "Hello"},
		},
		{
			name:        "キーが形を成さず原文も無い行は捨てられる",
			row:         Row{Key: "english", Kind: KindBroken},
			wantDropped: true, wantNote: noteDroppedBroken,
		},
		{
			name:        "キーが空で原文も無い行は捨てられる",
			row:         Row{Key: "", Kind: KindBroken},
			wantDropped: true, wantNote: noteDroppedBroken,
		},
		{
			name: "原文の無いハッシュ行は残る",
			row:  Row{Key: hash, Kind: KindHash},
		},
		{
			name: "台詞ID行は捨てられない",
			row:  Row{Key: "line:a8779ebf", Kind: KindLineID},
		},
		{
			name:        "形を成さないキーに原文が付いていれば不一致として捨てられる",
			row:         Row{Key: "english", Kind: KindBroken, SourceEn: "Hello"},
			wantDropped: true, wantNote: noteDroppedMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			note, dropped := droppedReason(tt.row)
			if dropped != tt.wantDropped {
				t.Fatalf("捨てるかの判定が違う: got %v, want %v", dropped, tt.wantDropped)
			}
			if note != tt.wantNote {
				t.Errorf("理由が違う: got %q, want %q", note, tt.wantNote)
			}
		})
	}
}
