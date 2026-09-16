package diff

import (
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
	"github.com/223n/dragnwash-localization-editor/internal/key"
)

// 入力の列名。照合は大文字小文字を区別しない（csvfile.Row の性質）。
// 公開ファイルの6列と作業コピーの7列の両方を同じ名前で引く。
const (
	colKey         = "key"
	colSection     = "section"
	colNode        = "node"
	colOrder       = "order"
	colSpeaker     = "speaker"
	colSourceEn    = "source_en"
	colTranslation = "translation"
)

// Kind は key 列の値の種別。
type Kind int

const (
	// KindHash は16桁の小文字16進。英文のハッシュキー。
	KindHash Kind = iota
	// KindLineID は "line:xxxx" の形の台詞ID。
	KindLineID
	// KindBroken はどちらでもない値。空文字もここに入る。
	//
	// 公開ファイルには本来現れない（実データの13ロケールで0件を実測済み）。
	// 作業コピーには現れうる。作業コピーを書く WorkingCopy.Export は key の形を
	// 検証せず、翻訳者が末尾に足した "English,訳" のような行をそのまま写す
	// （移植仕様「作業コピー生成 R5」）。
	KindBroken
)

// Row は公開ファイルまたは作業コピーの1行。
//
// Key だけが正規化される。順序も publish.collect と同じで、
// トリム → 台詞ID判定 → 小文字化。台詞IDを先に判定するので、台詞IDは
// 小文字化されずトリム後の綴りのまま残る（再生順の line_id 列と同じ綴りで
// 突き合わせるため）。
//
// ほかの列は一切トリムしない。publish が訳の前後の空白をそのまま公開している
// 以上、ここで削ると「公開ファイルにある値」と違うものを表示することになる。
type Row struct {
	Key  string
	Kind Kind

	Section     string
	Node        string
	OrderText   string
	Speaker     string
	SourceEn    string
	Translation string
}

// ReadRows はCSVのバイト列を [Row] の並びに直す。
//
// 読み方は publish と同じ csvfile.ReadPowerShellRows に固定する。この道具の
// 主張は「publish を回すとどうなるか」なので、読み方が publish とずれた瞬間に
// 主張が嘘になる。引用フィールド内の改行を値として読む csvfile.ReadCSharpRows を
// 使うと、同じファイルから別のレコード集合が出てくる。
//
// エラーを返すのはヘッダーの列名が重複しているときだけ（csvfile.DuplicateColumnError）。
// 空ファイルとヘッダーだけのファイルは0行として返し、エラーにしない。
func ReadRows(csvBytes []byte) ([]Row, error) {
	records, err := csvfile.ReadPowerShellRows(csvBytes)
	if err != nil {
		return nil, err
	}

	rows := make([]Row, 0, len(records))
	for _, rec := range records {
		// key 列だけトリムする。列が無ければ空文字になり KindBroken に落ちる。
		// 空文字を key.For へ渡す経路はこのパッケージに無いので、
		// key.HashOfEmpty が本物のキーに化けることはない。
		raw := strings.TrimSpace(rec.Get(colKey))

		row := Row{
			Section:     rec.Get(colSection),
			Node:        rec.Get(colNode),
			OrderText:   rec.Get(colOrder),
			Speaker:     rec.Get(colSpeaker),
			SourceEn:    rec.Get(colSourceEn),
			Translation: rec.Get(colTranslation),
		}
		if key.LooksLikeLineID(raw) {
			row.Key, row.Kind = raw, KindLineID
		} else {
			row.Key = strings.ToLower(raw)
			row.Kind = KindBroken
			if key.LooksLike(row.Key) {
				row.Kind = KindHash
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// PublishKey は publish がこの行を採用するかどうかと、採用した場合のキーを返す。
//
// 判定は publish.collect の分岐をそのまま写したもので、条件の順序も変えない。
// publish と同じ行を同じキーで拾うことがこのパッケージの存在意義なので、
// 「16桁hexの行だけを見る」のような読みやすい近似に置き換えない。
//
//	台詞ID行            採用しない（別経路で扱う）
//	source_en あり      key が空なら source_en のハッシュで採用
//	                    key が非空でハッシュと一致すれば採用、違えば捨てる
//	source_en なし      key が16桁hexなら採用、でなければ捨てる
//
// key 列が無い（あるいは空の）行を採用するのが、素朴な「KindBroken は全部捨てる」
// との違い。ゲーム内のUIエクスポートは `key,source_en,translation,object_path` で、
// key 列が空の行を普通に含む（移植仕様「作業コピー生成」）。この行を見落とすと、
// 訳を書けば公開される行を「未翻訳 0 件」と報告することになる。
func PublishKey(r Row) (string, bool) {
	if r.Kind == KindLineID {
		return "", false
	}
	if r.SourceEn != "" {
		hashed := key.For(r.SourceEn)
		if r.Key != "" && r.Key != hashed {
			return "", false
		}
		return hashed, true
	}
	if r.Kind == KindHash {
		return r.Key, true
	}
	return "", false
}

// droppedReason は publish がこの行を捨てる理由を返す。捨てないなら第2戻り値が false。
//
// [PublishKey] の裏返しだが、捨てた理由を分けて伝えるために別に持つ。
// 台詞ID行は捨てられないので、ここでも対象外になる。
func droppedReason(r Row) (string, bool) {
	if r.Kind == KindLineID {
		return "", false
	}
	if _, adopted := PublishKey(r); adopted {
		return "", false
	}
	if r.SourceEn != "" {
		// key と source_en のハッシュが食い違う。source_en を書き換えたのに
		// 古い key が残っている行で、publish はこれを黙って捨てる。
		return noteDroppedMismatch, true
	}
	return noteDroppedBroken, true
}
