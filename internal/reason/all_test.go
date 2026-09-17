package reason

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// TestAllListsEveryIdentifier は、宣言した識別子が1つ残らず All() に入っている
// ことを見る。
//
// この設計の安全網は「All() をなぞる試験」に集まっている。目録に鍵があるか、
// 日本語が Go 側と一致するか、英語の画面に日本語が残っていないか——どれも
// All() が返す一覧を端から当たって確かめている。
//
// その入口が手で並べた変数なので、識別子を足して all に足し忘れると、網そのものが
// その識別子を見なくなる。実際に起きたことが確かめられている。all から1つ外し、
// 目録も更新しないまま英語の画面を出すと、英文の途中に日本語が挟まったまま
// 試験は1つも落ちなかった。
//
// だから、ここだけは宣言を読んで突き合わせる。reason.go を構文木として読み、
// このパッケージが持つ文字列の定数を数え上げる。
func TestAllListsEveryIdentifier(t *testing.T) {
	declared := declaredIDs(t)
	if len(declared) == 0 {
		t.Fatal("reason.go から定数を1つも読めていない。読み方が壊れている")
	}

	listed := make(map[string]string, len(All()))
	for _, id := range All() {
		listed[id] = id
	}

	for name, id := range declared {
		if _, ok := listed[id]; !ok {
			t.Errorf("定数 %s（%q）が All() に入っていない。英語の画面にここだけ日本語が出る", name, id)
		}
	}
	for id := range listed {
		found := false
		for _, want := range declared {
			if want == id {
				found = true
			}
		}
		if !found {
			t.Errorf("All() の %q に対応する定数が無い。消した識別子が残っている", id)
		}
	}
}

// declaredIDs は reason.go が宣言している文字列の定数を、名前から値で引ける形で返す。
//
// 数え上げから外すのは、識別子ではない定数だけである。いまは1つも無いが、
// 増えたときに黙って網へ入らないよう、名前ではなく「値が識別子の形か」で見る。
func declaredIDs(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "reason.go", nil, 0)
	if err != nil {
		t.Fatalf("reason.go を読めない: %v", err)
	}

	out := make(map[string]string)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range value.Names {
				if i >= len(value.Values) {
					continue
				}
				lit, ok := value.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				out[name.Name] = text
			}
		}
	}
	return out
}
