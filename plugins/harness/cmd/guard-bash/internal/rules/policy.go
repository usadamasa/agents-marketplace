package rules

import (
	"fmt"
	"strings"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
)

// PolicyMarkdown はルールの宣言から、CLI ごとの方針表を markdown で組み立てる。
// `guard-bash -rules-doc` の出力。ファイルには保存せず、要るときにその場で引く。
func PolicyMarkdown() string {
	var b strings.Builder
	b.WriteString("# guard-bash の CLI ごとの方針\n\n")
	b.WriteString("ルールは allow → deny → ask の順に、CLI をまたいで評価する。最初に当たった判定で確定する。\n")
	b.WriteString("verb の末尾の `*` は前方一致 (`add*` は `add-iam-policy-binding` にも当たる)。\n")
	b.WriteString("`<noun>` は位置引数 1 つ、`...` は位置を固定しないことを表す。\n")

	var tools []string
	byTool := map[string][]*argrule.Group{}
	for _, g := range all {
		if _, ok := byTool[g.Tool]; !ok {
			tools = append(tools, g.Tool)
		}
		byTool[g.Tool] = append(byTool[g.Tool], g)
	}

	for _, tool := range tools {
		fmt.Fprintf(&b, "\n## %s\n", tool)
		for _, sec := range []struct {
			label string
			rules func(*argrule.Group) []argrule.Rule
		}{
			{"通過 (allow)", func(g *argrule.Group) []argrule.Rule { return g.Allow }},
			{"deny", func(g *argrule.Group) []argrule.Rule { return g.Deny }},
			{"ask", func(g *argrule.Group) []argrule.Rule { return g.Ask }},
		} {
			var items []string
			for _, g := range byTool[tool] {
				for _, r := range sec.rules(g) {
					item := "- `" + r.Describe() + "`"
					if h := argrule.HintOf(r); h != "" {
						item += " (代わり: " + h + ")"
					}
					items = append(items, item)
				}
			}
			if len(items) == 0 {
				continue
			}
			fmt.Fprintf(&b, "\n%s:\n\n%s\n", sec.label, strings.Join(items, "\n"))
		}
	}
	return b.String()
}
