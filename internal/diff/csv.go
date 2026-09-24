package diff

import (
	"io"
	"strings"

	"github.com/223n/dragnwash-localization-editor/internal/csvfile"
)

// CSVHeader は csv 形式の1行目。
//
// 表計算に貼って並べ替えたり絞り込んだりするための形式なので、列は固定にする。
// category と status は ASCII の識別子で、note だけが日本語。
const CSVHeader = "locale,category,status,key,section,node,order,speaker,source_en,translation,note"

// WriteCSV は報告を CSV で書く。
//
// 書き方は publish と同じ csvfile.EscapeField と LF で、BOM は付けない。
// Go の csv.Writer を使わないのは引用の条件が違うためで、同じ理由が
// internal/csvfile のパッケージコメントに書いてある。
//
// source_en 列に値が入るのは作業コピーを読んだときだけ。作業コピーはコミット
// されないので、CI のログへ英語原文が出ることはない。手元の記録
// （logs/dwloc_<日付>.log）へは、cmd/dwloc がこの出力を写さない。
//
// 出力の並びは Report.Findings のまま（ロケール順 → カテゴリ順 → キー順）。
// 参考のカテゴリも含めて全件書く。--all と --limit は text 形式の指定で、
// CSV には効かない。表計算側で絞れるものを、道具の側で先に落とす必要が無いため。
func (r *Report) WriteCSV(w io.Writer) error {
	var b strings.Builder
	b.WriteString(CSVHeader)
	b.WriteString(csvfile.LineTerminator)
	for _, f := range r.Findings {
		b.WriteString(csvfile.JoinFields(
			f.Locale,
			f.Category.ID(),
			f.Category.Status().id(),
			f.Key,
			f.Section,
			f.Node,
			f.OrderText,
			f.Speaker,
			f.SourceEn,
			f.Translation,
			f.Note,
		))
		b.WriteString(csvfile.LineTerminator)
	}

	_, err := io.WriteString(w, b.String())
	return err
}
