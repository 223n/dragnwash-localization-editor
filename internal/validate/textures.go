package validate

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
)

const (
	// TexturesDir は、訳した絵（差し替える画像）を置くフォルダーの名前。
	// Translations/<ロケール>/ の直下に置く。無くてもよい。
	TexturesDir = "textures"

	// TexturesCredits は、絵ごとに作った人と何をしたかを書く CSV の名前。
	TexturesCredits = "credits.csv"

	// TexturesFallback は、足りない絵を借りるほかのロケールを並べるファイルの名前。
	TexturesFallback = "fallback.txt"
)

const (
	// maxPictureSide は絵の幅と高さの上限（ピクセル）。
	maxPictureSide = 4096
	// maxPictureBytes は絵のファイルの大きさの上限（8MB）。
	maxPictureBytes = 8 * 1024 * 1024
	// pngHeadSize は PNG の見出しとして読むバイト数。シグネチャ8、IHDR の長さ4、
	// 型4、幅4、高さ4。
	pngHeadSize = 24
)

// pngSignature は PNG ファイルの先頭8バイト。
var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// texturesCreditsHeader は credits.csv の見出し。前後の空白を落として比べる。
var texturesCreditsHeader = []string{"file", "author", "note"}

// picture は textures/ にある .png 1枚。
type picture struct {
	// name はフォルダーの中の名前。credits.csv の file 列と比べる。
	name string
	// path は絶対パスを含む実際の場所。
	path string
}

// checkTextures は Translations/<ロケール>/textures/ を検査する。上流 dev の
// check_textures（cc01bfc）に当たり、文面・順番・件数をそろえてある。
//
// 見る順番:
//
//  1. フォルダーの中を名前順に1つずつ。フォルダーがあれば問題。拡張子が .png
//     （大文字小文字を問わない）なら絵として覚える。fallback.txt ならその場で
//     中身を見る。credits.csv でもなければ、置いてはいけないものとして問題。
//  2. 覚えた絵を名前順に。拡張子が小文字の .png か、8MB 以下か、PNG の
//     シグネチャと IHDR があるか、4096x4096 以下か。
//  3. credits.csv。無ければ、絵が1枚でもあるときだけ問題にして終える。
//     見出しが違えばその1件で終える。
//  4. credits.csv の行が無い絵。
//
// translations は Translations の場所、locale はロケールのフォルダーの名前、
// textures は textures フォルダーの場所。
//
// エラーを返すのは、ファイルやフォルダーが読めないときと、credits.csv の
// フィールドが長すぎて CSV として読めないとき。上流はどれも捕まえずに
// トレースバックで終わる。ここでは [CheckTree] と同じく「検査できなかった」として
// 呼び出し側へ返す。
func checkTextures(show display, translations, locale, textures string) ([]Problem, error) {
	entries, err := os.ReadDir(textures)
	if err != nil {
		return nil, fmt.Errorf("%s が読めない: %w", show.of(textures), err)
	}

	problems := []Problem{}
	var pictures []picture
	// os.ReadDir は名前のバイト順に並べる。上流の sorted() は Linux（CI）では
	// 同じ順になる。Windows の Python は大文字小文字を無視して並べるので、
	// 大文字で始まる名前が混ざると出る順だけがずれる（合否は変わらない）。
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(textures, name)
		// 上流の is_dir() はシンボリックリンクを辿るので os.Stat で見る。
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			problems = append(problems, Problem{Path: show.of(path), Message: "no folders inside textures/"})
			continue
		}
		switch {
		case asciiLower(pythonSuffix(name)) == ".png":
			pictures = append(pictures, picture{name: name, path: path})
		case name == TexturesFallback:
			found, err := checkFallback(show, translations, locale, path)
			if err != nil {
				return nil, err
			}
			problems = append(problems, found...)
		case name != TexturesCredits:
			problems = append(problems, Problem{
				Path:    show.of(path),
				Message: "only .png files, credits.csv and fallback.txt belong in textures/",
			})
		}
	}

	names := make(map[string]bool, len(pictures))
	for _, pic := range pictures {
		names[pic.name] = true
		found, err := checkPicture(show, pic)
		if err != nil {
			return nil, err
		}
		problems = append(problems, found...)
	}

	credits := filepath.Join(textures, TexturesCredits)
	// 上流の exists() もリンクを辿る。フォルダーでも「ある」ことになり、
	// 次の読み込みで失敗する（上流はそこで異常終了する）。
	if _, err := os.Stat(credits); err != nil {
		if len(pictures) > 0 {
			problems = append(problems, Problem{
				Path:    show.of(textures),
				Message: "credits.csv is missing (file,author,note - one row per picture)",
			})
		}
		return problems, nil
	}
	data, err := os.ReadFile(credits)
	if err != nil {
		return nil, fmt.Errorf("%s が読めない: %w", show.of(credits), err)
	}
	found, credited, err := checkTexturesCredits(show.of(credits), data, names)
	if err != nil {
		return nil, fmt.Errorf("%s を CSV として読めない: %w", show.of(credits), err)
	}
	problems = append(problems, found...)
	if credited == nil {
		// 見出しが違うときは、行の無い絵を数えずに終える（上流と同じ）。
		return problems, nil
	}
	for _, pic := range pictures {
		if !credited[pic.name] {
			problems = append(problems, Problem{Path: show.of(pic.path), Message: "no row in credits.csv"})
		}
	}
	return problems, nil
}

// checkPicture は絵1枚を検査する。上流は大きさの問題があっても続けて中身を見るので、
// 1枚で「8MB を超える」と「PNG ではない」の2件が出ることがある。
func checkPicture(show display, pic picture) ([]Problem, error) {
	shown := show.of(pic.path)
	var problems []Problem
	if pythonSuffix(pic.name) != ".png" {
		problems = append(problems, Problem{Path: shown, Message: "use a lowercase .png extension"})
	}

	info, err := os.Stat(pic.path)
	if err != nil {
		return nil, fmt.Errorf("%s が読めない: %w", shown, err)
	}
	if size := info.Size(); size > maxPictureBytes {
		problems = append(problems, Problem{
			Path:    shown,
			Message: fmt.Sprintf("%d KB, more than %d MB", size/1024, maxPictureBytes/(1024*1024)),
		})
	}

	head, err := readHead(pic.path, pngHeadSize)
	if err != nil {
		return nil, fmt.Errorf("%s が読めない: %w", shown, err)
	}
	if len(head) < pngHeadSize || !bytes.Equal(head[:8], pngSignature) || string(head[12:16]) != "IHDR" {
		return append(problems, Problem{Path: shown, Message: "not a PNG file"}), nil
	}
	width := binary.BigEndian.Uint32(head[16:20])
	height := binary.BigEndian.Uint32(head[20:24])
	if width > maxPictureSide || height > maxPictureSide {
		problems = append(problems, Problem{
			Path: shown,
			Message: fmt.Sprintf("%dx%d, larger than %dx%d",
				width, height, maxPictureSide, maxPictureSide),
		})
	}
	return problems, nil
}

// readHead はファイルの先頭 n バイトを読む。短いファイルなら読めた分だけ返す。
// 上流は read_bytes() で丸ごと読んでから先頭を切るが、見るのは先頭だけなので
// 結果は同じになる。
func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, int64(n)))
}

// checkTexturesCredits は textures/credits.csv の中身を検査する。
//
// pictures は textures/ にある絵の名前。戻り値の credited は credits.csv に行の
// ある file 列の値の集合で、見出しが違って行を見なかったときは nil になる。
//
// 報告の行番号は物理行ではなく「何番目のレコードか」。上流が
// enumerate(rows[1:], start=2) で数えるためで、引用値が複数行にまたがる行の
// あとでは物理行とずれる。CI の報告と同じ番号を出すことを優先して、そのまま写す。
// 空行も1レコードとして数える。
//
// 上流はこのファイルを list(csv.reader(fh)) で読み、コメントも扱わない。
// フィールドが長すぎるときの csv.Error は捕まえずに異常終了するので、ここでは
// エラーとして返す。
func checkTexturesCredits(name string, data []byte, pictures map[string]bool) ([]Problem, map[string]bool, error) {
	records, err := csvfile.ParsePythonRecords(csvfile.SplitPythonLines(data))
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 || !slices.Equal(trimFields(records[0].Fields), texturesCreditsHeader) {
		// 1行目が空行でも、それが見出しとして比べられて外れる。
		return []Problem{{Path: name, Line: 1, Message: "the header must be file,author,note"}}, nil, nil
	}

	var problems []Problem
	credited := make(map[string]bool)
	for i, record := range records[1:] {
		n := i + 2
		row := record.Fields
		// 上流の `if not row or not "".join(row).strip(): continue`。
		// " , " のようにカンマと空白だけの行も飛ばす。
		if len(row) == 0 || csvfile.IsPythonBlank(strings.Join(row, "")) {
			continue
		}
		if len(row) != len(texturesCreditsHeader) {
			problems = append(problems, Problem{
				Path: name, Line: n,
				Message: fmt.Sprintf("expected 3 columns (file,author,note), found %d", len(row)),
			})
			continue
		}
		fields := trimFields(row)
		file, author, note := fields[0], fields[1], fields[2]
		if credited[file] {
			problems = append(problems, Problem{Path: name, Line: n, Message: file + " is listed twice"})
		}
		credited[file] = true
		if !pictures[file] {
			problems = append(problems, Problem{Path: name, Line: n, Message: file + " is not in textures/"})
		}
		if author == "" {
			problems = append(problems, Problem{
				Path: name, Line: n,
				Message: "who made " + file + "? (author is empty)",
			})
		}
		if note == "" {
			problems = append(problems, Problem{
				Path: name, Line: n,
				Message: "say what was done for " + file +
					" (note is empty), for example: drawn from scratch, or game texture repainted",
			})
		}
	}
	return problems, credited, nil
}

// trimFields は各フィールドの前後の空白を Python の str.strip() と同じく落とす。
func trimFields(fields []string) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = csvfile.TrimPythonSpace(f)
	}
	return out
}

// checkFallback は textures/fallback.txt を検査する。上流の check_fallback に当たる。
//
// 1行に1つ、足りない絵を借りるロケールの名前を書く。前後の空白を落として、
// 空の行と '#' で始まる行は飛ばす。自分自身の名前と、Translations に strings.csv の
// あるロケールとして見つからない名前が問題になる。
//
// 行の分け方は Python の str.splitlines() に合わせる（[pythonSplitLines]）。
// 上流は read_text() で読んでから splitlines() で分けるので、U+2028 や \v でも
// 行が割れ、報告の行番号もその数え方になる。strings.csv の comment_lines と違い、
// ここでは数え方が1通りしかないので、ずれは生まれない。そのまま写す。
func checkFallback(show display, translations, locale, path string) ([]Problem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s が読めない: %w", show.of(path), err)
	}
	shown := show.of(path)
	var problems []Problem
	for i, line := range pythonSplitLines(csvfile.TrimBOMString(string(data))) {
		name := csvfile.TrimPythonSpace(line)
		if name == "" || strings.HasPrefix(name, "#") {
			continue
		}
		n := i + 1
		switch {
		case name == locale:
			problems = append(problems, Problem{Path: shown, Line: n, Message: name + " is this language itself"})
		case strings.HasPrefix(name, "_") || strings.ContainsAny(name, `/\`) ||
			!isRegularFile(filepath.Join(translations, name, PublishedFile)):
			// '_' で始まる名前は _discovered のようにロケールではないフォルダー。
			// 区切り文字を含む名前は Translations の外や奥を指せるので、
			// 中を見る前に外す（上流と同じ順の判定）。
			problems = append(problems, Problem{
				Path: shown, Line: n,
				Message: name + " is not a language in Translations/",
			})
		}
	}
	return problems, nil
}

// isRegularFile は path が通常のファイルかを返す。上流の is_file() と同じく
// シンボリックリンクを辿る。
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// pythonSuffix は pathlib の PurePath.suffix と同じく、名前の最後の '.' から後ろを
// 返す。'.' が先頭にしか無い名前（".png"）と、'.' で終わる名前は "" になる。
// ".png" という名前のファイルは絵ではなく、置いてはいけないものとして扱われる。
//
// 上流の CI の Python 3.12 に合わせてある。3.14 は "a." に "." を返すが、
// ここで比べる相手は ".png" だけなので、どちらでも判定は変わらない。
func pythonSuffix(name string) string {
	i := strings.LastIndexByte(name, '.')
	if 0 < i && i < len(name)-1 {
		return name[i:]
	}
	return ""
}

// pythonSplitLines は Python の str.splitlines() と同じ区切りで行に分ける。
//
// 区切りは \n、\r、\r\n、\v、\f、\x1c、\x1d、\x1e、U+0085、U+2028、U+2029。
// 区切り文字は行に含めない。末尾が区切りで終わるときに空の行を足さないところも
// 同じ（"a\n" は ["a"]、"" は []）。
func pythonSplitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '\r':
			lines = append(lines, s[start:i])
			i += size
			if i < len(s) && s[i] == '\n' {
				i++
			}
			start = i
		case '\n', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			lines = append(lines, s[start:i])
			i += size
			start = i
		default:
			i += size
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
