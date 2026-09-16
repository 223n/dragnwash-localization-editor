package edit

import (
	"strings"
	"testing"

	"github.com/223n/dragnwash-localization-editor/internal/validate"
)

// TestHeaderNotLooserThanValidate は「編集できるのにコミット前の検証で落ちる」
// ファイルが生まれないことを見る。
//
// 確かめる向きは1つだけ: 形式検証がヘッダーを拒むファイルは、編集モデルも
// 読み取り専用にすること。逆向き（編集モデルが拒み、検証は通す）は許す。
// 生テキストの完全一致で照合しているぶん、編集モデルの方が厳しいのは想定どおり。
//
// 作業コピーの7列だけは例外。検証は公開ファイルだけを見るので source_en 付きを
// 拒むが、それは編集モデルが主に扱う相手である。
func TestHeaderNotLooserThanValidate(t *testing.T) {
	workingJoined := strings.Join(workingHeader, ",")

	headers := []string{
		"key,section,node,order,speaker,translation",
		"key,speaker,translation",
		"key,translation",
		workingJoined,
		"key,section,node,order,speaker,source_en",
		"key,section,node,order,source_en,speaker",
		"Key,Translation",
		"key,translation,extra",
		"key",
		"source_en,translation",
		" key,translation",
		"key, translation",
		"key,translation ",
		"key,section,node,order,speaker,translation,",
		`"key",translation`,
		"key;translation",
		"\tkey,translation",
	}

	for _, header := range headers {
		t.Run(header, func(t *testing.T) {
			// 検証を通すため、キーの形と section / node の形を満たす行を1本入れる。
			data := []byte(header + "\n0da72197e898ebe1,UI,,,UI,訳\n")

			validateRejects := false
			for _, p := range validate.CheckFile("t.csv", data) {
				if strings.HasPrefix(p.Message, "header is ") {
					validateRejects = true
				}
			}
			editRejects := Parse(data).ReadOnly()

			if header == workingJoined {
				if !validateRejects {
					t.Fatal("作業コピーのヘッダーを形式検証が受理してしまった（前提が変わった）")
				}
				if editRejects {
					t.Fatal("作業コピーのヘッダーを編集モデルが拒んだ")
				}
				return
			}
			if validateRejects && !editRejects {
				t.Errorf("形式検証は拒むのに編集モデルは編集させる: %q", header)
			}
		})
	}
}

// TestMatchHeader は照合そのものを固定する。
func TestMatchHeader(t *testing.T) {
	if got := matchHeader("key,translation"); strings.Join(got, ",") != "key,translation" {
		t.Errorf("matchHeader = %q", got)
	}
	if got := matchHeader("key,section,node,order,speaker,source_en,translation"); len(got) != 7 {
		t.Errorf("作業コピーのヘッダーが一致しない: %q", got)
	}
	if got := matchHeader("key,translation\n"); got != nil {
		// 改行は呼び出し側で落としてから渡す約束。
		t.Errorf("改行付きが一致してしまう: %q", got)
	}
	if got := matchHeader(""); got != nil {
		t.Errorf("空文字が一致してしまう: %q", got)
	}
}
