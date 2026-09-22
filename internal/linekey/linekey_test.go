package linekey

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// 見本の値は、元実装（翻訳リポジトリの tools/linekeys.py）を実際に走らせて
// 取ったものである。手で計算していない。文はこの試験のために作ったもので、
// ゲームの台本は1文も使っていない（元実装の見本も同じ方針で作られている）。
var samples = []struct {
	text       string
	normalized string
	normKey    string
	fp         string
}{
	{"", "", "e3b0c44298fc1c14", "0000000000000000"},
	{"Hi", "hi", "8f434346648f6b96", "08ba5f07b55ec3da"},
	{
		"Wash the dragon, then rinse!", "wash the dragon then rinse",
		"2b0163d969143127", "c291a81964e2e55a",
	},
	// 書式タグ、連続する空白、大文字、記号を落とすと上と同じ形になる。
	{
		"<i>Wash</i>   THE   dragon,<size=70%> then rinse!", "wash the dragon then rinse",
		"2b0163d969143127", "c291a81964e2e55a",
	},
	{
		"Kobold: Grab the sponge before the water gets cold.",
		"kobold grab the sponge before the water gets cold",
		"7ca8718e52c17a61", "c29ba61965e1cbb3",
	},
	// 末尾の記号だけが違う文。正規化すると完全に同じになる。
	{
		"Kobold: Grab the sponge before the water gets cold!",
		"kobold grab the sponge before the water gets cold",
		"7ca8718e52c17a61", "c29ba61965e1cbb3",
	},
	// 日本語は英数字として残る。読点と感嘆符は落ちる。
	{"こんにちは、世界！", "こんにちは世界", "c6a304536826fb57", "aa39de1f7cc4c29d"},
	{
		"Numbers 123 and symbols #$% stay apart", "numbers 123 and symbols stay apart",
		"6aea87f148de67bc", "c2578e190552ee7f",
	},
	{"a\tb", "a b", "c8687a08aa5d6ed2", "e63f991904833892"},
	{" leading and trailing ", "leading and trailing", "2f5342d289b88dce", "c3020219257ea321"},
	// 閉じないタグは、そこから先が全部落ちる。
	{"<i>", "", "e3b0c44298fc1c14", "0000000000000000"},
	{"<b>bold", "bold", "e0007a5bca8d9158", "000d281901408056"},
	// ½ は No、Ⅷ は Nl。Python の isalnum() が真を返すので残す。
	// Ä は非ASCIIの大文字なので小文字にしない。
	{"½ Ⅷ Ä", "½ Ⅷ Ä", "267976fa0ab16318", "8fa84fa34873a240"},
}

func TestNormalize(t *testing.T) {
	for _, tt := range samples {
		t.Run(tt.text, func(t *testing.T) {
			if got := Normalize(tt.text); got != tt.normalized {
				t.Errorf("Normalize(%q) = %q, want %q", tt.text, got, tt.normalized)
			}
		})
	}
}

func TestNormalizedKeyAndFingerprint(t *testing.T) {
	for _, tt := range samples {
		t.Run(tt.text, func(t *testing.T) {
			if got := NormalizedKey(tt.text); got != tt.normKey {
				t.Errorf("NormalizedKey(%q) = %q, want %q", tt.text, got, tt.normKey)
			}
			if got := FingerprintText(tt.text); got != tt.fp {
				t.Errorf("FingerprintText(%q) = %q, want %q", tt.text, got, tt.fp)
			}
		})
	}
}

// TestNormalizeIsIdempotent は、正規化した文字列をもう一度正規化しても変わらない
// ことを見る。
//
// 元実装の resolve は、蓄えた fp（生の英文から作ったもの）と、その場で
// 正規化した文字列から作った fp を突き合わせている。冪等でなければ、この2つが
// 別の指紋になって突き合わせが成り立たない。
func TestNormalizeIsIdempotent(t *testing.T) {
	for _, tt := range samples {
		n := Normalize(tt.text)
		if got := Normalize(n); got != n {
			t.Errorf("Normalize(%q) = %q（2度目で変わった）", n, got)
		}
		if got, want := Fingerprint(n), Fingerprint(tt.text); got != want {
			t.Errorf("指紋が変わった: %016x != %016x（%q）", got, want, tt.text)
		}
	}
}

func TestDistance(t *testing.T) {
	// 記号だけが違う文は、正規化が同じなので指紋も同じになる。
	same := Distance(
		Fingerprint("Grab the sponge before the water gets cold."),
		Fingerprint("Grab the sponge before the water gets cold!"),
	)
	if same != 0 {
		t.Errorf("記号だけ違う文の距離が %d", same)
	}
	// まるで別の文は上限より遠い。
	far := Distance(
		Fingerprint("Grab the sponge before the water gets cold."),
		Fingerprint("Ryan flies away and the level ends here"),
	)
	if far <= MaxFuzzyDistance {
		t.Errorf("別の文の距離が %d で、上限 %d 以下になっている", far, MaxFuzzyDistance)
	}
	if got := Distance(0, 0xFFFFFFFFFFFFFFFF); got != 64 {
		t.Errorf("全ビット違う距離が %d", got)
	}
}

func TestParseFingerprint(t *testing.T) {
	tests := []struct {
		in   string
		want uint64
		ok   bool
	}{
		{"0000000000000000", 0, true},
		{"124a73193de90c3e", 0x124a73193de90c3e, true},
		{"124A73193DE90C3E", 0x124a73193de90c3e, true},
		{"", 0, false},
		{"124a73193de90c3", 0, false},
		{"124a73193de90c3ee", 0, false},
		{"zzzzzzzzzzzzzzzz", 0, false},
	}
	for _, tt := range tests {
		got, ok := ParseFingerprint(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseFingerprint(%q) = %016x, %v、%016x, %v を期待", tt.in, got, ok, tt.want, tt.ok)
		}
	}
	// 書いた形をそのまま読み戻せること。
	for _, tt := range samples {
		got, ok := ParseFingerprint(FingerprintText(tt.text))
		if !ok || got != Fingerprint(tt.text) {
			t.Errorf("往復できない: %q", tt.text)
		}
	}
}

// sourceRepoEnv は元実装のリポジトリの場所を上書きする環境変数。
// internal/diff の実データ試験と同じ名前を使う。
const sourceRepoEnv = "DRAGNWASH_SOURCE_REPO"

// sourceRepoCandidates は環境変数が無いときに探す場所。
var sourceRepoCandidates = []string{
	`C:\dev\223n\dragnwash-localization`,
}

// TestUpstreamVectors は、元実装が配っている見本（ci/linekey-vectors.json）と
// 1件も食い違わないことを見る。
//
// 見本のファイルはこのリポジトリへ写していない。写しは上流が定義を変えたときに
// 静かに古くなるし、上流は仕組みそのものを experimental と書いている。翻訳
// リポジトリが手元にあるときだけ突き合わせ、無ければ飛ばす（internal/diff の
// 実データ試験と同じ考え方）。
func TestUpstreamVectors(t *testing.T) {
	root := os.Getenv(sourceRepoEnv)
	if root == "" {
		for _, c := range sourceRepoCandidates {
			if _, err := os.Stat(c); err == nil {
				root = c
				break
			}
		}
	}
	if root == "" {
		t.Skipf("翻訳リポジトリが見つからないので飛ばす（%s で場所を指定できる）", sourceRepoEnv)
	}
	path := filepath.Join(root, "ci", "linekey-vectors.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("見本を読めないので飛ばす: %v", err)
	}
	var vectors []struct {
		Text          string `json:"text"`
		Key           string `json:"key"`
		Normalized    string `json:"normalized"`
		NormalizedKey string `json:"normalized_key"`
		Fingerprint   string `json:"fingerprint"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("見本を読めない: %v", err)
	}
	if len(vectors) == 0 {
		t.Fatal("見本が空")
	}
	for i, v := range vectors {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := Normalize(v.Text); got != v.Normalized {
				t.Errorf("Normalize(%q) = %q, want %q", v.Text, got, v.Normalized)
			}
			if got := NormalizedKey(v.Text); got != v.NormalizedKey {
				t.Errorf("NormalizedKey(%q) = %q, want %q", v.Text, got, v.NormalizedKey)
			}
			if got := FingerprintText(v.Text); got != v.Fingerprint {
				t.Errorf("FingerprintText(%q) = %q, want %q", v.Text, got, v.Fingerprint)
			}
		})
	}
	t.Logf("%s の %d 件と一致した", path, len(vectors))
}
