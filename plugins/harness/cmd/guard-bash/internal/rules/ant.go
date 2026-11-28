package rules

import (
	"github.com/spf13/pflag"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
)

// antFlags はサブコマンドの前に置ける global option のうち、値を取らないものと、
// ルールが見る --base-url。値を取らない global option を宣言しないと、直後の
// resource を値として読み飛ばされる (`ant --api-key-stdin auth logout` が抜ける)。
func antFlags(fs *pflag.FlagSet) {
	fs.String("base-url", "", "")
	fs.Bool("api-key-stdin", false, "")
	fs.Bool("debug", false, "")
	fs.Bool("raw-output", false, "")
	fs.Bool("version", false, "")
	fs.Bool("no-browser", false, "")
	fs.Bool("all", false, "")
	fs.Bool("access-token", false, "")
}

// antWorkerRules は self-hosted の environment worker を止める。ant の他の deny と
// group を分けてあるのは、専用の denyReason を返すため。
//
// `ant beta:worker run|poll` は Managed Agents のセッションが発行する tool_use
// イベントをこのマシンで実行する。リモートのエージェントに任意のコマンドを
// 実行させる入口になるので、ask にも落とさず deny する。
var antWorkerRules = &argrule.Group{
	Tool: "ant",
	Deny: []argrule.Rule{
		argrule.CmdAt{Tool: "ant", Path: []string{"beta:worker"}},
	},
	DenyReason: "ant beta:worker はリモートのセッションが発行する tool_use をこのマシンで実行する (self-hosted worker) ためブロックしました",
}

// antRules は ant の書き込み操作を deny / ask に振り分ける。
//
// `ant <resource> <verb>` の構造で、resource は `beta:sessions:events` のように
// コロンで連結した 1 トークン。verb は resource 直後の位置引数でだけ判定する
// (`--transform delete` のような引数値では発火しない)。
//
// ant は sandbox の外で動く (Go 製 CLI は sandbox 内で TLS 検証に落ちるため
// excludedCommands に入れてある) ので、egress 先を sandbox で縛れない。
// `--base-url` を deny するのはそのためで、OAuth token を任意のホストへ
// 送れる形を止める。
//
// 同名の Apache Ant (`ant clean` / `ant -f build.xml compile`) は resource+verb の
// 形を取らないのでどのルールにも当たらない。
var antRules = &argrule.Group{
	Tool:  "ant",
	Flags: antFlags,
	Deny: []argrule.Rule{
		// 認証状態の変更と、credential の書き出し (print-credentials は refresh token を
		// 含む全体を出す。`--access-token` を付けても短命 token を stdout へ出すので
		// 同じ扱いにする)。
		argrule.CmdAt{Tool: "ant", Path: []string{"auth"}, Exact: true, Verbs: []string{
			"login",
			"logout",
			"print-credentials",
		}},
		// プロファイルの切り替え・書き換え (base_url や organization_id もここで変わる)。
		argrule.CmdAt{Tool: "ant", Path: []string{"profile"}, Exact: true, Verbs: []string{
			"activate",
			"set",
		}},
		// 復旧手段の無い操作。resource を問わない。
		argrule.CmdAt{Tool: "ant", Nouns: argrule.NounQualified, Exact: true, Verbs: []string{
			"archive",
			"delete",
			"remove",
		}},
		// 組織管理 (メンバー・招待・API key・workspace・WIF) の書き込み。
		argrule.CmdAt{Tool: "ant", Path: []string{"beta:organization:*"}, Exact: true, Verbs: []string{
			"add",
			"archive",
			"create",
			"delete",
			"remove",
			"update",
		}},
		// vault は MCP 等の資格情報の置き場。
		argrule.CmdAt{Tool: "ant", Path: []string{"beta:vaults:*"}, Exact: true, Verbs: []string{
			"archive",
			"create",
			"delete",
			"update",
		}},
		// tunnel は token を持つ。reveal-token / rotate-token は秘匿値の露出・失効。
		argrule.CmdAt{Tool: "ant", Path: []string{"beta:tunnels:*"}, Exact: true, Verbs: []string{
			"archive",
			"create",
			"reveal-token",
			"rotate-token",
		}},
		// API の宛先を差し替える global option。
		argrule.Check{Desc: "ant --base-url", Fn: func(cl *argrule.Cmdline) bool {
			return argrule.AnyView(cl, "ant", func(v *argrule.View) bool { return v.Changed("base-url") })
		}},
	},
	// API 呼び出し (課金) とリソースの作成・更新。resource 直後の verb で判定する。
	Ask: []argrule.Rule{
		argrule.CmdAt{Tool: "ant", Nouns: argrule.NounQualified, Exact: true, Verbs: []string{
			"ack",
			"add",
			"cancel",
			"create",
			"create-enrollment-url",
			"heartbeat",
			"mcp-oauth-validate",
			"pause",
			"poll",
			"run",
			"send",
			"stop",
			"unpause",
			"update",
			"upload",
		}},
	},
	DenyReason: "ant の危険な操作 (認証・プロファイル変更・delete/archive/remove・組織/vault/tunnel の書き込み・--base-url) をブロックしました",
	AskReason:  "ant の書き込み操作 (API 呼び出し・リソースの作成/更新) です。実行してよいか確認してください",
}
