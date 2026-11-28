package rules

import (
	"github.com/spf13/pflag"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
)

// databricksFlags は verb の手前に置かれうる、値を取らない global flag。
func databricksFlags(fs *pflag.FlagSet) {
	fs.Bool("debug", false, "")
}

var databricksRules = &argrule.Group{
	Tool:  "databricks",
	Flags: databricksFlags,
	Deny: []argrule.Rule{
		// 認証・クレデンシャル操作
		argrule.CmdAt{Tool: "databricks", Path: []string{"auth"}, Verbs: []string{
			"env",
			"login",
			"switch",
			"token",
		}},
		argrule.CmdAt{Tool: "databricks", Path: []string{"configure"}},
		// 書き込み操作。read 系 (list, get, describe 等) のみ通過させる。
		//
		// "set" は "set-" にして前方一致の対象を絞る。`settings-v2` のような
		// noun が「set」に前方一致して誤って Deny に落ちるため (`account settings-v2
		// list-account-settings-metadata` 等)。裸の "set" は下の Exact ルールで別に拾う。
		argrule.CmdAt{Tool: "databricks", Nouns: argrule.NounLoose, Verbs: []string{
			"add",
			"cancel",
			"create",
			"delete",
			"destroy",
			"import",
			"install",
			"patch",
			"pin",
			"remove",
			"reset",
			"restart",
			"run-now",
			"set-",
			"start",
			"stop",
			"submit",
			"uninstall",
			"unpin",
			"update",
		}},
		argrule.CmdAt{Tool: "databricks", Nouns: argrule.NounLoose, Exact: true, Verbs: []string{"set"}},
		argrule.CmdAt{Tool: "databricks", Path: []string{"fs"}, Verbs: []string{
			"cp",
			"mkdir",
			"rm",
		}},
		argrule.CmdAt{Tool: "databricks", Path: []string{"workspace"}, Verbs: []string{
			"delete",
			"import",
			"mkdirs",
		}},
	},
	Ask: []argrule.Rule{
		argrule.Check{
			Desc: "databricks の read (cat, describe, export*, get*, list*, ls, plan, profiles, schema, spark-versions, summary, validate, version, current-user me) 以外の操作",
			Fn:   databricksUnknown,
		},
	},
	DenyReason: "databricks の危険な操作 (認証・設定変更・ワークスペース書き込み) をブロックしました",
	AskReason:  "read と確認できない databricks の操作です (書き込みや任意の SQL 実行を含みうるため確認します)",
}

// databricksUnknown は read と確認できない databricks の操作か。
//
// 書き込みの verb を列挙する形は、列挙から漏れた verb がそのまま通る。
// `api post /api/2.0/sql/statements` (任意の SQL)、`bundle deploy`、`sync`、
// `secrets put-secret` が実際に素通りしていた。保守的に倒して、read と確認できた
// 形以外は全て ask にする。既知の破壊的操作は上の Deny が先に拾う。
func databricksUnknown(cl *argrule.Cmdline) bool {
	if _, ok := argrule.FirstMatch(databricksReadRules, cl); ok {
		return false
	}
	// 位置引数が無い形は対象外。他の CLI の引数に "databricks" の語が現れただけで
	// (`gh pr create --title databricks`) 確認を出さない。
	return argrule.AnyView(cl, "databricks", func(v *argrule.View) bool { return len(v.Pos) > 0 })
}

// databricksReadVerbs は read の verb。前方一致なので `get-run` `list-runs`
// `export-dir` `get-permissions` も含む。
var databricksReadVerbs = []string{
	"cat",
	"describe",
	"export",
	"get",
	"list",
	"ls",
	"plan",
	"profiles",
	"schema",
	"spark-versions",
	"summary",
	"validate",
}

// databricksReadRules は read と確認できる形。ここに当たらない操作は ask へ落ちる。
var databricksReadRules = []argrule.Rule{
	// verb が 1 つだけの形。
	argrule.CmdAt{Tool: "databricks", Nouns: argrule.NounNone, Verbs: []string{
		"help",
		"version",
	}, Exact: true},
	// `databricks <noun> <verb>` と、account モードの `databricks account <noun> <verb>`。
	//
	// verb の位置は固定する (NounLoose で後ろの位置引数まで走査すると、
	// `bundle run get_data` のように read の verb に前方一致する引数を持つ
	// 書き込みが read 扱いになる)。
	argrule.CmdAt{Tool: "databricks", Nouns: argrule.NounOne, Verbs: databricksReadVerbs},
	argrule.CmdAt{Tool: "databricks", Path: []string{"account"}, Nouns: argrule.NounOne, Verbs: databricksReadVerbs},
	// `current-user me` の verb は read の語彙に無いので個別に通す。前方一致へ "me" を
	// 足すと `account metastores <未知の verb>` まで read 扱いになる。
	argrule.CmdAt{Tool: "databricks", Path: []string{"current-user"}, Verbs: []string{"me"}, Exact: true},
}
