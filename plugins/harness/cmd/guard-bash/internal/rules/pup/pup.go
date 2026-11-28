// Package pup は Datadog の CLI (pup) のルール。
package pup

import (
	"slices"
	"strings"

	"github.com/spf13/pflag"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
)

// pupFlags は noun の手前にも置ける global flag と、ルールが見る `pup api` の flag。
// 値を取るものを宣言しないと、値が noun や verb として読まれる (`pup -o json monitors list`)。
func pupFlags(fs *pflag.FlagSet) {
	fs.StringP("output", "o", "", "")
	fs.String("org", "", "")
	fs.String("jq", "", "")
	fs.String("site", "", "")
	fs.BoolP("yes", "y", false, "")
	fs.Bool("no-envelope", false, "")
	fs.Bool("read-only", false, "")
	fs.BoolP("verbose", "v", false, "")
	fs.BoolP("version", "V", false, "")
	fs.Bool("trust-site", false, "")
	fs.StringP("method", "X", "GET", "")
	fs.StringArrayP("field", "F", nil, "")
	fs.StringArrayP("raw-field", "f", nil, "")
	fs.StringArrayP("header", "H", nil, "")
	fs.String("input", "", "")
	fs.BoolP("include", "i", false, "")
	fs.Bool("silent", false, "")
}

// Rules は pup の認証まわりを deny し、read と確認できない操作を ask にする。
//
// pup は sandbox の外で動くので、書き込みを止めるのはこの guard だけになる。
// 書き込みの verb を列挙して deny する形は、列挙から漏れた verb (`auth logout`、
// 非 GET の `pup api`、`runbooks run` 等) がそのまま通る。read と確認できた形以外は
// ask に倒す。
var Rules = &argrule.Group{
	Tool:  "pup",
	Flags: pupFlags,
	// allow は deny より先に評価されるので、deny の --trust-site をここでも外す。
	Allow: []argrule.Rule{
		argrule.When{
			Rule: argrule.CmdAt{Tool: "pup", Path: []string{"notebooks"}, Verbs: []string{
				"create",
				"update",
			}},
			Cond: func(cl *argrule.Cmdline) bool { return !trustSite(cl) },
			Note: "--trust-site なし",
		},
	},
	Deny: []argrule.Rule{
		// 認証状態の変更と、OAuth の access token の書き出し。
		argrule.CmdAt{Tool: "pup", Path: []string{"auth"}, Exact: true, Verbs: []string{
			"login",
			"logout",
			"refresh",
			"token",
		}},
		// 資格情報 (API key、application key、IdP の対応付け) の書き込み。
		argrule.CmdAt{Tool: "pup", Path: []string{"api-keys|app-keys|authn-mappings"}, Verbs: []string{
			"create",
			"delete",
			"update",
		}},
		// Datadog 以外のホストを信頼させ、token をそこへ送れる形。
		argrule.Check{Desc: "pup --trust-site", Fn: trustSite},
	},
	Ask: []argrule.Rule{
		argrule.Check{
			Desc: "pup の read (get*, list*, search, query 等の verb、auth list|status|test、GET の api、docs、version) 以外の操作",
			Fn: func(cl *argrule.Cmdline) bool {
				return argrule.AnyView(cl, "pup", func(v *argrule.View) bool { return !pupRead(v) })
			},
		},
	},
	DenyReason: "pup の認証まわりの操作 (auth の login/logout/refresh/token・API key/application key/AuthN mapping の書き込み・--trust-site) をブロックしました",
	AskReason:  "read と確認できない pup の操作です (Datadog リソースの作成・更新・削除を含みうるため確認します)",
}

// pupReadVerbPrefixes は前方一致で read とみなす verb (`get-trace`、`list-versions`)。
var pupReadVerbPrefixes = []string{
	"get",
	"list",
}

// pupNounLikeReadVerbs は noun としても使われる read の verb (`rum metrics delete`
// の metrics)。後ろに noun の形の語が続くなら noun として読み進める。
var pupNounLikeReadVerbs = []string{
	"events",
	"interfaces",
	"metrics",
	"patterns",
	"query",
	"resources",
	"schema",
	"tracers",
	"types",
}

// pupReadVerbs はトークン全体で比べる read の verb。
var pupReadVerbs = []string{
	"affecting",
	"aggregate",
	"artifacts",
	"attribution",
	"branch-summary",
	"broker-configs",
	"by-org",
	"callgraph",
	"channel-get",
	"client-configs",
	"columns",
	"commit-summary",
	"context",
	"count",
	"coverage",
	"coverage-ts",
	"currency",
	"deps",
	"describe",
	"diagnostics",
	"diff",
	"dimension-values",
	"events",
	"expand",
	"facet-keys",
	"facet-values",
	"facets",
	"find",
	"find-errors",
	"flamegraph",
	"flow-map",
	"guide",
	"hotspots",
	"hourly",
	"interfaces",
	"investigation-queries",
	"ip-ranges",
	"metric-values",
	"metrics",
	"model-pricing",
	"onboarding-status",
	"operations",
	"owner",
	"path",
	"patterns",
	"points",
	"poll-results",
	"preview",
	"projected",
	"query",
	"read-messages",
	"records",
	"records-all",
	"records-full",
	"resources",
	"results",
	"revision",
	"revisions",
	"savings",
	"savings-ts",
	"scalar",
	"schema",
	"search",
	"space-get",
	"spec",
	"stats",
	"status",
	"subject-schemas",
	"suggested-actions",
	"summary",
	"table",
	"tables",
	"tag-sources",
	"timeline",
	"timeseries",
	"to-terraform",
	"topic-configs",
	"topics",
	"topics-with-points",
	"tracers",
	"traffic-summary",
	"types",
	"url",
	"utilization",
	"utilization-ts",
	"validate",
	"versions",
}

// pupWriteVerbPrefixes は前方一致で書き込みとみなす verb。noun の並びの途中に
// 現れたら、後ろに read の語があっても read 扱いにしない (`monitors delete list`)。
var pupWriteVerbPrefixes = []string{
	"attach-",
	"batch-",
	"bulk-",
	"create",
	"delete",
	"detach-",
	"migrate-",
	"override-",
	"patch-",
	"refresh",
	"set-",
	"submit",
	"update",
	"write-",
}

// pupWriteVerbs はトークン全体で比べる書き込みの verb。read の一覧に無い葉の verb は
// 全てここか前方一致の一覧に入れる (入れ漏れた verb は noun として読み飛ばされる)。
var pupWriteVerbs = []string{
	"activate",
	"add",
	"analyze",
	"archive",
	"ask",
	"assign",
	"assist",
	"cancel",
	"check",
	"clone",
	"conclude",
	"configure",
	"convert",
	"deactivate",
	"disable",
	"download",
	"edit",
	"enable",
	"export",
	"generate",
	"import",
	"install",
	"link",
	"login",
	"logout",
	"mitigate",
	"move",
	"mute",
	"offboard",
	"onboard",
	"pause",
	"pin",
	"post",
	"publish",
	"records-add",
	"register",
	"remove",
	"restore",
	"resume",
	"run",
	"score",
	"send",
	"serve",
	"set",
	"star",
	"start",
	"stop",
	"surface",
	"token",
	"trigger",
	"unarchive",
	"unassign",
	"unlink",
	"unmute",
	"unpin",
	"unpublish",
	"unstar",
	"upgrade",
	"upload",
	"upsert",
	"watch",
}

func trustSite(cl *argrule.Cmdline) bool {
	return argrule.AnyView(cl, "pup", func(v *argrule.View) bool { return v.Changed("trust-site") })
}

func hasAnyPrefix(tok string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(tok, p) })
}

func isWriteVerb(tok string) bool {
	return hasAnyPrefix(tok, pupWriteVerbPrefixes) || slices.Contains(pupWriteVerbs, tok)
}

func isReadVerb(tok string) bool {
	return hasAnyPrefix(tok, pupReadVerbPrefixes) || slices.Contains(pupReadVerbs, tok)
}

// pupMaxNouns は verb の手前に並ぶ noun の最大の段数
// (`integrations aws cloud-auth persona-mappings list`)。
const pupMaxNouns = 4

// pupRead は pup の read と確認できる形か。
//
// noun の深さが 1〜4 段で揃わないので、verb の位置は固定せず、先頭の noun から
// 順に見て最初に現れた verb で決める。
func pupRead(v *argrule.View) bool {
	pos := v.Pos
	if len(pos) == 0 {
		return true
	}
	switch pos[0] {
	case "completions", "docs", "fmt", "format", "help", "version":
		return true
	case "api":
		m, _ := v.FS.GetString("method")
		return strings.EqualFold(m, "GET") || strings.EqualFold(m, "HEAD")
	case "auth":
		return len(pos) > 1 && slices.Contains([]string{"list", "status", "test"}, pos[1])
	}
	if !argrule.IsNoun(pos[0]) {
		return false
	}
	for q := 1; q < len(pos) && q <= pupMaxNouns; q++ {
		tok := pos[q]
		if isWriteVerb(tok) || !argrule.IsNoun(tok) {
			return false
		}
		nounLike := slices.Contains(pupNounLikeReadVerbs, tok)
		if nounLike && q+1 < len(pos) && argrule.IsNoun(pos[q+1]) {
			continue
		}
		if isReadVerb(tok) {
			return true
		}
	}
	return false
}
