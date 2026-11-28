// Package graphqldoc は GitHub の GraphQL API へ送る文書を解析し、送ってよい形かを判定する。
//
// 機微フィールドの一覧は GitHub のスキーマの名前で書く。文書の中身だけを見て、
// どのコマンドがどこから文書を読むかは親の gh パッケージが持つ。
package graphqldoc

import (
	"slices"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

// sensitiveFieldParts は名前にこれを含むフィールドを機微とみなす語 (小文字)。
// 将来足されるフィールドにも効くよう部分一致にし、無害なフィールドの巻き添えは許容する。
var sensitiveFieldParts = []string{
	"email",
	"token",
	"secret",
	"password",
	"recoverycode",
	"certificate",
	"identit",
}

// sensitiveFields は名前の完全一致で機微とみなすフィールド。
// `key` は PublicKey / DeployKey の鍵本体で、License.key なども巻き添えで deny になる。
var sensitiveFields = []string{
	"publicKeys",
	"deployKeys",
	"key",
	"ipAllowListEntries",
	"ownerInfo",
	"oidcProvider",
}

// ReadOnlyQuery は文書が機微フィールドを選ばない query operation だけか。
func ReadOnlyQuery(src string) bool {
	doc, err := parser.ParseQuery(&ast.Source{Input: src})
	if err != nil || len(doc.Operations) == 0 {
		return false
	}
	for _, op := range doc.Operations {
		if op.Operation != ast.Query || !safeSelection(doc, op.SelectionSet, nil) {
			return false
		}
	}
	return true
}

// safeSelection は選択木に機微フィールドが無いか。alias ではなくフィールド名を見る。
// named fragment は定義を辿り、未定義と循環は不一致にする。inProgress は辿っている途中の fragment 名。
func safeSelection(doc *ast.QueryDocument, set ast.SelectionSet, inProgress []string) bool {
	for _, sel := range set {
		switch s := sel.(type) {
		case *ast.Field:
			if sensitiveField(s.Name) || !safeSelection(doc, s.SelectionSet, inProgress) {
				return false
			}
		case *ast.InlineFragment:
			if !safeSelection(doc, s.SelectionSet, inProgress) {
				return false
			}
		case *ast.FragmentSpread:
			def := doc.Fragments.ForName(s.Name)
			if def == nil || slices.Contains(inProgress, s.Name) ||
				!safeSelection(doc, def.SelectionSet, append(slices.Clip(inProgress), s.Name)) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func sensitiveField(name string) bool {
	if slices.Contains(sensitiveFields, name) {
		return true
	}
	lower := strings.ToLower(name)
	return slices.ContainsFunc(sensitiveFieldParts, func(p string) bool { return strings.Contains(lower, p) })
}

// OnlyMutationsNamed は文書の全 operation が mutation で、トップレベルの選択が
// 全て names のフィールドか。fragment spread は中身を辿らずに不一致にする。
func OnlyMutationsNamed(src string, names []string) bool {
	doc, err := parser.ParseQuery(&ast.Source{Input: src})
	if err != nil || len(doc.Operations) == 0 {
		return false
	}
	for _, op := range doc.Operations {
		if op.Operation != ast.Mutation || len(op.SelectionSet) == 0 {
			return false
		}
		for _, sel := range op.SelectionSet {
			f, ok := sel.(*ast.Field)
			if !ok || !slices.Contains(names, f.Name) {
				return false
			}
		}
	}
	return true
}
