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
//
// 表計算が式と読む値には、頭に ' を付ける（[defuseFormula]）。値を変えずに
// 書くのは [Report.WriteCSVRaw] である。
func (r *Report) WriteCSV(w io.Writer) error {
	return r.writeCSV(w, defuseFormula)
}

// WriteCSVRaw は、値に手を加えずに報告を CSV で書く。
//
// 機械と突き合わせる使い方のためにある。そこでは公開ファイルの訳と1文字も
// 違わない値が要り、頭に ' が付くと一致しなくなる。
func (r *Report) WriteCSVRaw(w io.Writer) error {
	return r.writeCSV(w, nil)
}

// writeCSV は報告を CSV で書く。field が nil でなければ、各値を通してから書く。
func (r *Report) writeCSV(w io.Writer, field func(string) string) error {
	var b strings.Builder
	b.WriteString(CSVHeader)
	b.WriteString(csvfile.LineTerminator)
	for _, f := range r.Findings {
		values := []string{
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
		}
		if field != nil {
			for i, v := range values {
				values[i] = field(v)
			}
		}
		b.WriteString(csvfile.JoinFields(values...))
		b.WriteString(csvfile.LineTerminator)
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// formulaStarts は、表計算がその値を式として読む先頭の文字。
//
// = + - @ は式の始まりとして読まれ、タブと CR は、その後ろの = などを式として
// 読ませる抜け道になる（OWASP の CSV Injection の一覧と同じ）。
const formulaStarts = "=+-@\t\r"

// defuseFormula は、表計算が式と読む値の頭に ' を付ける。
//
// csv は「表計算にそのまま貼れます」と案内している。訳は公開ファイルから来るので、
// ほかの人が Pull Request で入れた訳（=HYPERLINK(...) など）が、開いた人の画面で
// 式として評価される。悪意が無くても、台詞のダッシュで始まる訳は #NAME? などに
// 化ける。
//
// ' は引用の内側に入る（引用は、このあと csvfile.EscapeField が付ける）。
// 表計算は ' で始まる値を文字列として読む。
//
// 全部の列に通す。訳と原文のほかに、ロケール名（フォルダー名）や話者名も
// ゲームやリポジトリから来る文字列で、どの列なら安全かを決めきれない。
//
// publish の公開ファイルと画面の書き出しには通さない。あちらは上流の道具と
// バイト単位で同じ出力が要る。
func defuseFormula(v string) string {
	if v != "" && strings.ContainsRune(formulaStarts, rune(v[0])) {
		return "'" + v
	}
	return v
}
