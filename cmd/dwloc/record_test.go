package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/key"
	"github.com/223n/dragnwash-localization-editor/internal/publish"
)

// 記録の試験で使う見本の文。どれも架空の文で、ゲームの台本ではない。
//
// 記録に入ったかどうかを文字列で探すので、ほかの出力（見出しや理由の文）に
// 紛れない言い回しにしてある。
const (
	// recordSource は作業コピーにだけある原文。未翻訳の行として diff に並ぶ。
	recordSource = "The lighthouse keeper hums"
	// recordVanished は再生順に無い行の訳。台本から消えた行として diff に並ぶ。
	recordVanished = "消えた鐘の音色"
	// recordKept はいまも再生される行の訳。
	recordKept = "やかんが歌っている"
	// recordLong は publish で失われる訳。先頭だけが報告に出る長さにしてある。
	recordLong = "とても長い灯台の物語の訳がここに続いています"
	// recordRepoSide と recordGameSide は、ゲーム側と食い違う訳の2つの版。
	recordRepoSide = "港の灯りの新しい歌"
	recordGameSide = "港の灯りの古い歌"
)

// 見本のキー。キーは原文のハッシュなので、原文から計算する。
var (
	recordKeptKey     = key.For("The kettle is singing")
	recordSourceKey   = key.For(recordSource)
	recordVanishedKey = key.For("The bell rang once")
)

// recordOrderCSV は recordKeptKey と recordSourceKey を1回ずつ再生する再生順。
var recordOrderCSV = "section,phase,node,order,line_id,key,speaker,condition\n" +
	"L01 Ryan,intro,Ryan_1_intro,1,line:c0000001," + recordKeptKey + ",Ryan,\n" +
	"L01 Ryan,intro,Ryan_1_intro,2,line:c0000002," + recordSourceKey + ",Ryan,\n"

// recordDiffTree は、diff の本文に原文と訳が並ぶリポジトリを作る。
//
// 未翻訳の行（本文に「原文: 」が出る）と、台本から消えた行（本文に「訳: 」が
// 出る）を1件ずつ持つ。
func recordDiffTree(t *testing.T) string {
	t.Helper()

	return makeTree(t, map[string]string{
		"data/script_order.csv": recordOrderCSV,
		"Translations/ja/strings.csv": publish.HeaderLine + "\n" +
			recordKeptKey + ",L01 Ryan,Ryan_1_intro,1,Ryan," + recordKept + "\n" +
			recordVanishedKey + ",L01 Ryan,Ryan_9_gone,3,Ryan," + recordVanished + "\n",
		"Translations/_discovered/ja.working.csv": "key,source_en,translation\n" +
			recordKeptKey + ",The kettle is singing," + recordKept + "\n" +
			recordSourceKey + "," + recordSource + ",\n",
	})
}

// recordLossTree は、publish が「訳が失われる」で止まるリポジトリとゲームを作る。
//
// 公開ファイルにある recordLong の行が、ゲーム側の作業コピーに無い。
func recordLossTree(t *testing.T) (root, game string) {
	t.Helper()

	root = makeTree(t, map[string]string{
		"data/script_order.csv": recordOrderCSV,
		jaPublishedPath: publish.HeaderLine + "\n" +
			recordKeptKey + ",L01 Ryan,Ryan_1_intro,1,Ryan," + recordLong + "\n",
	})
	game = makeGame(t, map[string]string{
		"Translations/_discovered/ja.working.csv": "key,section,node,order,speaker,source_en,translation\n" +
			recordSourceKey + ",L01 Ryan,Ryan_1_intro,2,Ryan," + recordSource + "," + recordKept + "\n",
	})
	return root, game
}

// recordDriftTree は、publish が「ゲームに入っている翻訳が古い」で止まる
// リポジトリとゲームを作る。
func recordDriftTree(t *testing.T) (root, game string) {
	t.Helper()

	root = makeTree(t, map[string]string{
		"data/script_order.csv": recordOrderCSV,
		jaPublishedPath: publish.HeaderLine + "\n" +
			recordKeptKey + ",L01 Ryan,Ryan_1_intro,1,Ryan," + recordRepoSide + "\n",
	})
	// 見出しをそろえておく。そろえないと、守りが止めた差分と見出しを足した差分が混ざる。
	publishOnce(t, root)
	game = makeGame(t, map[string]string{
		"Translations/ja/strings.csv": publish.HeaderLine + "\n" +
			recordKeptKey + ",L01 Ryan,Ryan_1_intro,1,Ryan," + recordGameSide + "\n",
		"Translations/_discovered/ja.working.csv": "key,section,node,order,speaker,source_en,translation\n" +
			recordKeptKey + ",L01 Ryan,Ryan_1_intro,1,Ryan,The kettle is singing," + recordGameSide + "\n",
	})
	return root, game
}

// TestRecordLeavesOutRowContent は、diff の本文と publish の訳の断片が
// 記録（logs/dwloc_<日付>.log）に入らないことを見る。
//
// 記録は不具合の報告に添えて手元の外へ出る。README と Issue の雛形は、記録を
// そのまま貼ってよいと案内している。そこへ原文（ゲームの台本）が入ると、公開の
// Issue で台本を配り直すことになる。訳の断片も、翻訳者がまだ公開していない
// 作業の中身である。
//
// 画面には今までどおり出す。出ていなければ、この試験は何も確かめていない。
// 記録に残すのは、件数・キー・行番号・理由・見出しと、本文を省いたことの1行である。
func TestRecordLeavesOutRowContent(t *testing.T) {
	tests := []struct {
		name string
		// args は dwloc に渡す引数。
		args     func(t *testing.T) []string
		wantCode int
		// secrets は画面に出て、記録には入ってはいけない文字列。
		secrets []string
		// wantLog は記録に残っていてほしい文字列。
		wantLog []string
	}{
		{
			name: "diff の text は一覧を記録に入れない",
			args: func(t *testing.T) []string {
				return []string{"diff", "--root", recordDiffTree(t), "--no-game"}
			},
			wantCode: exitProblems,
			secrets:  []string{recordSource, recordVanished},
			// 見出しと件数と、判定の結果は残す。報告から何が起きたかを辿れる。
			wantLog: []string{
				"ja  Translations/ja/strings.csv",
				"未翻訳", "台本から消えた行", "1 件",
				"要確認が 1 行あります。",
				"記録しません",
			},
		},
		{
			name: "diff の csv は行を記録に入れない",
			args: func(t *testing.T) []string {
				return []string{"diff", "--root", recordDiffTree(t), "--no-game", "--format", "csv"}
			},
			wantCode: exitProblems,
			secrets:  []string{recordSource, recordVanished, "locale,category,status,key"},
			wantLog:  []string{"記録しません"},
		},
		{
			name: "publish は失われる訳の先頭を記録に入れない",
			args: func(t *testing.T) []string {
				root, game := recordLossTree(t)
				return []string{"publish", "--root", root, "--game", game}
			},
			wantCode: exitProblems,
			secrets:  []string{string([]rune(recordLong)[:6])},
			// どのファイルの何行目のどのキーが、なぜ失われるのかは残す。
			wantLog: []string{
				"訳が失われるので、1バイトも書きませんでした。",
				"2行目 " + recordKeptKey,
				"失われる訳が 1 件あります。",
				"記録しません",
			},
		},
		{
			name: "publish はゲーム側と食い違う訳を記録に入れない",
			args: func(t *testing.T) []string {
				root, game := recordDriftTree(t)
				return []string{"publish", "--root", root, "--game", game}
			},
			wantCode: exitProblems,
			secrets:  []string{recordRepoSide, recordGameSide},
			wantLog: []string{
				"ゲームに入っている翻訳が古いので",
				"ja（1 件）",
				recordKeptKey,
				"記録しません",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetRecord(t)
			args := tt.args(t)
			dir := t.TempDir()
			t.Chdir(dir)
			setArgs(t, args...)

			read := captureStd(t)
			code := mainWithRecord()
			stdout, stderr := read()
			if code != tt.wantCode {
				t.Fatalf("終了コード = %d, 期待 %d\nstdout:\n%s\nstderr:\n%s", code, tt.wantCode, stdout, stderr)
			}

			_, log := readLogs(t, dir)
			screen := stdout + stderr
			for _, secret := range tt.secrets {
				if !strings.Contains(screen, secret) {
					t.Errorf("画面に %q が出ていない（試験の前提が崩れている）\n%s", secret, screen)
				}
				if strings.Contains(log, secret) {
					t.Errorf("記録に %q が入っている\n--- 記録 ---\n%s", secret, log)
				}
			}
			checkContains(t, "記録", log, tt.wantLog)
		})
	}
}

// TestUnrecordedSplitsScreenAndRecord は、画面には全部出し、記録へは選んだ行だけを
// 写して、省いた行の数を1行残すことを見る。
//
// 改行で終わらない最後の行も数える。数え落とすと、記録を読んだ人は省いた行が
// あったことに気づけない。
func TestUnrecordedSplitsScreenAndRecord(t *testing.T) {
	var screen, rec strings.Builder
	body := newUnrecorded(&teeWriter{screen: &screen, record: &rec}, diffHeadingLine)

	// 1行を2回に分けて書く形も混ぜる。fmt.Fprintf が1行を何回かに分けて呼ばれても、
	// 行の単位で振り分けること。
	for _, part := range []string{
		"見出し\n",
		diffListIndent + "原文: 架空の",
		"一文\n",
		"    件数の行\n",
		diffListIndent + "訳: 改行の無い最後の行",
	} {
		if _, err := body.Write([]byte(part)); err != nil {
			t.Fatalf("書けない: %v", err)
		}
	}
	body.Close()

	wantScreen := "見出し\n" + diffListIndent + "原文: 架空の一文\n" + "    件数の行\n" +
		diffListIndent + "訳: 改行の無い最後の行"
	if screen.String() != wantScreen {
		t.Errorf("画面 = %q, 期待 %q", screen.String(), wantScreen)
	}
	wantRecord := "見出し\n    件数の行\n" + fmt.Sprintf(unrecordedText, 2)
	if rec.String() != wantRecord {
		t.Errorf("記録 = %q, 期待 %q", rec.String(), wantRecord)
	}
}

// TestUnrecordedRecordWritesOnlyTheRecord は、Record が記録にだけ書くことを見る。
// publish の失われる訳は、画面には訳の先頭つきの行を、記録には先頭を除いた行を書く。
func TestUnrecordedRecordWritesOnlyTheRecord(t *testing.T) {
	var screen, rec strings.Builder
	samples := newUnrecorded(&teeWriter{screen: &screen, record: &rec}, nil)
	fmt.Fprint(samples, "3行目 abc 「架空の訳」\n")
	samples.Record("3行目 %s\n", "abc")
	samples.Close()

	if got, want := screen.String(), "3行目 abc 「架空の訳」\n"; got != want {
		t.Errorf("画面 = %q, 期待 %q", got, want)
	}
	if got, want := rec.String(), "3行目 abc\n"+fmt.Sprintf(unrecordedText, 1); got != want {
		t.Errorf("記録 = %q, 期待 %q", got, want)
	}
}

// TestUnrecordedWithoutRecord は、記録を取っていないときは受け取った行き先へ
// そのまま書き、Record と Close が何もしないことを見る。
//
// 試験のバッファと、記録を始められなかったときがこれにあたる。ここで数の1行を
// 画面へ出すと、記録していないのに「記録しません」と画面に言うことになる。
func TestUnrecordedWithoutRecord(t *testing.T) {
	var screen strings.Builder
	body := newUnrecorded(&screen, nil)
	fmt.Fprint(body, "原文: 架空の一文\n")
	body.Record("記録にだけ書く行\n")
	body.Close()

	if got, want := screen.String(), "原文: 架空の一文\n"; got != want {
		t.Errorf("画面 = %q, 期待 %q", got, want)
	}
}

// TestUnrecordedLeavesNoCountWhenNothingIsOmitted は、省いた行が無ければ数の1行を
// 書かないことを見る。見出しだけの報告に「0 行は記録しません」は要らない。
func TestUnrecordedLeavesNoCountWhenNothingIsOmitted(t *testing.T) {
	var screen, rec strings.Builder
	body := newUnrecorded(&teeWriter{screen: &screen, record: &rec}, diffHeadingLine)
	fmt.Fprint(body, "見出し\n")
	body.Close()

	if got, want := rec.String(), "見出し\n"; got != want {
		t.Errorf("記録 = %q, 期待 %q", got, want)
	}
}

// TestTeeWriterStopsAtTheScreen は、画面へ書けなければ記録へも写さず、誤りを
// 返すことを見る。画面に無い行が記録にだけ残ると、報告と画面が食い違う。
func TestTeeWriterStopsAtTheScreen(t *testing.T) {
	var rec strings.Builder
	tee := &teeWriter{screen: failWriter{}, record: &rec}
	if _, err := tee.Write([]byte("行\n")); err == nil {
		t.Error("画面へ書けないのに誤りを返していない")
	}
	if rec.Len() != 0 {
		t.Errorf("画面へ書けないのに記録へ写している: %q", rec.String())
	}

	short := &teeWriter{screen: shortWriter{}, record: &rec}
	if _, err := short.Write([]byte("行\n")); !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("画面へ書き切れないのに io.ErrShortWrite を返していない: %v", err)
	}
	if rec.Len() != 0 {
		t.Errorf("画面へ書き切れないのに記録へ写している: %q", rec.String())
	}

	// unrecorded も、画面へ書けなかった分を記録へ写さない。
	body := newUnrecorded(&teeWriter{screen: failWriter{}, record: &rec}, diffHeadingLine)
	if _, err := body.Write([]byte("見出し\n")); err == nil {
		t.Error("画面へ書けないのに誤りを返していない")
	}
	body.Close()
	if rec.Len() != 0 {
		t.Errorf("画面へ書けないのに記録へ写している: %q", rec.String())
	}
}

// shortWriter は、受け取った量より少なく書いたと返す io.Writer。
type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	return len(p) / 2, nil
}
