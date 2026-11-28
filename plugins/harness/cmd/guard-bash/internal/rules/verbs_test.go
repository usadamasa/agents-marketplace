package rules

import (
	"testing"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/rules/gh"
)

// collectCmdAt は宣言済みのルールから CmdAt を全て集める。
// 一覧を手で持つと新しいルールの登録漏れが起きるため、group と opt-in を走査する。
func collectCmdAt(t *testing.T) []argrule.CmdAt {
	t.Helper()

	lists := [][]argrule.Rule{gh.PROptIn, gh.IssueOptIn}
	for _, g := range all {
		lists = append(lists, g.Allow, g.Deny, g.Ask)
	}

	var out []argrule.CmdAt
	for _, l := range lists {
		for _, r := range l {
			// When は Rule を包むので、中身を取り出してから見る。
			if w, ok := r.(argrule.When); ok {
				r = w.Rule
			}
			if c, ok := r.(argrule.CmdAt); ok {
				out = append(out, c)
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("CmdAt のルールが 1 つも集まらなかった (収集経路が壊れている)")
	}
	return out
}

// verb 列は 1 行 1 verb で昇順に並べる。並べ替えは判定に影響しないので、
// diff の読みやすさのために固定する。
func TestVerbListsSortedAndUnique(t *testing.T) {
	for _, c := range collectCmdAt(t) {
		t.Run(c.Describe(), func(t *testing.T) {
			for i, verb := range c.Verbs {
				if i == 0 {
					continue
				}
				switch prev := c.Verbs[i-1]; {
				case prev == verb:
					t.Errorf("verb %q が重複している", verb)
				case prev > verb:
					t.Errorf("verb 列が昇順でない: %q の後ろに %q", prev, verb)
				}
			}
		})
	}
}
