package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// flag パッケージが返す誤りの文の頭。flag は誤りの種類を型で返さないので、
// 文の形で見分けます。形は Go 1.0 から変わっていません。見分けられない形は
// 英語のまま出すので、flag が文を変えても理由そのものは失われません。
const (
	flagUndefined    = "flag provided but not defined: -"
	flagNeedsValue   = "flag needs an argument: -"
	flagBadSyntax    = "bad flag syntax: "
	flagInvalidBool  = "invalid boolean value "
	flagInvalidValue = "invalid value "
	// flagValueRange は、整数が範囲を超えたときに Set が返す理由です。
	flagValueRange = "value out of range"
)

// flagErrorText は、flag パッケージの解釈の誤りを日本語の1文に言い換えます。
//
// 使い方の説明もほかの誤りの文も日本語なので、ここだけ英語だと1回の実行の出力が
// 2言語に割れます。オプションの名前は、使い方と同じ -- の形で書きます
// （flag は1つ目の - だけで書きます）。
//
// 値を読めなかったときは、どう書けばよいかを添えます。とくに --idle-timeout は
// 単位が要り（30 ではなく 30s）、使い方の中の説明は全文に埋もれて読まれません。
// 値は打ったままの形で出します（[quoteValue]）。
func flagErrorText(fs *flag.FlagSet, err error) string {
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, flagUndefined):
		return "知らないオプションです: --" + strings.TrimPrefix(msg, flagUndefined)
	case strings.HasPrefix(msg, flagNeedsValue):
		return "--" + strings.TrimPrefix(msg, flagNeedsValue) + " には値が要ります"
	case strings.HasPrefix(msg, flagBadSyntax):
		return "オプションの書き方が正しくありません: " + strings.TrimPrefix(msg, flagBadSyntax)
	case strings.HasPrefix(msg, flagInvalidBool):
		// invalid boolean value "<値>" for -<名前>: <理由>
		if value, name, _, ok := splitInvalid(msg, flagInvalidBool, " for -"); ok {
			return fmt.Sprintf("--%s の値%sを読めません（値を付けないか、true か false を書きます）", name, quoteValue(value))
		}
	case strings.HasPrefix(msg, flagInvalidValue):
		// invalid value "<値>" for flag -<名前>: <理由>
		if value, name, cause, ok := splitInvalid(msg, flagInvalidValue, " for flag -"); ok {
			return fmt.Sprintf("--%s の値%sを読めません（%s）", name, quoteValue(value), valueHint(fs, name, cause))
		}
	}
	return msg
}

// quoteValue は、誤りの文に出すために、打たれた値を「」で囲んで返します。
//
// %q で囲まないのは、Windows のパスの \ が \\ に化けるためです。打った値と違う
// 形で画面に出るうえ、記録でホームのパスを ~ に置き換える照合（logfile の
// ShortenPath）にも当たらず、利用者名入りのパスが記録に残ります。パスを整数や
// 時間の指定に打ち間違えることは、ふつうに起きます。
//
// 改行やタブなどの見えない文字と、文字として読めないバイトだけは、%q と同じ形
// （\n や \xff など）で書きます。1つの誤りを1行に収め、値に何が入って
// いたかを見えるようにするためです。\ はそのまま書くので、打った \n の2文字と
// 改行は同じに見えます。見分けやすさより、打ったままに見えることを採ります。
func quoteValue(value string) string {
	var b strings.Builder
	b.WriteString("「")
	for i := 0; i < len(value); {
		r, size := utf8.DecodeRuneInString(value[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, value[i])
		case unicode.IsPrint(r):
			b.WriteRune(r)
		default:
			// QuoteRune は '\n' のように ' で囲んで返すので、囲みを外します。
			quoted := strconv.QuoteRune(r)
			b.WriteString(quoted[1 : len(quoted)-1])
		}
		i += size
	}
	b.WriteString("」")
	return b.String()
}

// splitInvalid は、<prefix>"<値>"<sep><名前>: <理由> の形の文を分けます。
//
// 値は %q で囲まれているので、値の中に sep や ": " があっても取り違えません。
func splitInvalid(msg, prefix, sep string) (value, name, cause string, ok bool) {
	rest := strings.TrimPrefix(msg, prefix)
	quoted, err := strconv.QuotedPrefix(rest)
	if err != nil {
		return "", "", "", false
	}
	// QuotedPrefix が切り出したものは、必ず Unquote できる。
	value, _ = strconv.Unquote(quoted)
	rest, found := strings.CutPrefix(rest[len(quoted):], sep)
	if !found {
		return "", "", "", false
	}
	name, cause, ok = strings.Cut(rest, ": ")
	return value, name, cause, ok
}

// valueHint は、読めなかった値をどう書けばよいかを返します。
//
// 標準の型（整数、時間）は、flag の理由が英語の決まり文句なので、型から書き方を
// 言い換えます。独自の型（--locale など）は、Set が自分の言葉で理由を返すので、
// それをそのまま使います。
func valueHint(fs *flag.FlagSet, name, cause string) string {
	f := fs.Lookup(name)
	if f == nil {
		return cause
	}
	getter, ok := f.Value.(flag.Getter)
	if !ok {
		return cause
	}
	switch getter.Get().(type) {
	case int:
		if cause == flagValueRange {
			return "大きすぎます"
		}
		return "整数で書きます"
	case time.Duration:
		return "30s や 1h30m のように単位を付けて書きます。0 だけは単位なしで書けます"
	}
	return cause
}
