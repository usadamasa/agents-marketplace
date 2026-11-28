package rules

import (
	"github.com/spf13/pflag"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
)

// opFlags はルールが見る書き出し先のフラグと、値を伴う短縮フラグ。
func opFlags(fs *pflag.FlagSet) {
	fs.String("out-file", "", "")
	fs.StringP("output", "o", "", "")
	fs.StringP("in-file", "i", "", "")
}

// opWritesFile は op が解決した値をファイルへ書き出す形か。
//
// op が秘匿値を masking するのは stdout/stderr だけで、ファイルへ書けば平文で残る。
func opWritesFile(cl *argrule.Cmdline) bool {
	return argrule.AnyView(cl, "op", func(v *argrule.View) bool {
		reads := len(v.Pos) > 0 && (v.Pos[0] == "read" || v.Pos[0] == "inject") ||
			len(v.Pos) > 1 && v.Pos[0] == "document" && v.Pos[1] == "get"
		return reads && v.Changed("out-file", "output")
	})
}

var opRules = &argrule.Group{
	Tool:  "op",
	Flags: opFlags,
	// op (1Password) の更新・削除・権限変更。
	// noun 1〜2 個の直後に来る位置引数だけを verb として判定する。
	// create / get / list / read / run / inject は列挙しないので通過する。
	Deny: []argrule.Rule{
		argrule.CmdAt{Tool: "op", Nouns: argrule.NounUpTo2, Exact: true, Verbs: []string{
			"confirm",
			"delete",
			"edit",
			"grant",
			"provision",
			"reactivate",
			"revoke",
			"share",
			"suspend",
		}},
		argrule.CmdAt{Tool: "op", Exact: true, Verbs: []string{
			"signin",
			"signout",
			"update",
		}},
		argrule.CmdAt{Tool: "op", Path: []string{"account"}, Exact: true, Verbs: []string{
			"add",
			"forget",
		}},
		argrule.CmdAt{Tool: "op", Path: []string{"plugin"}, Exact: true, Verbs: []string{
			"clear",
			"import",
			"init",
		}},
	},
	// 秘匿値をディスクに落とす形だけプロンプトする。
	Ask: []argrule.Rule{
		argrule.Check{Desc: "op {read, inject, document get} --out-file | --output | -o", Fn: opWritesFile},
	},
	DenyReason: "op の危険な操作 (更新・削除・権限変更・認証状態の変更) をブロックしました",
	AskReason:  "op が秘匿値をファイルへ書き出そうとしています。実行してよいか確認してください",
}
