package rules

import (
	"github.com/spf13/pflag"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
)

// gcloudFlags は verb の手前に置かれうる、値を取らない global flag。
// 宣言しないと直後の verb を値として読み飛ばされる。
//
// ponytail: 列挙は global flag だけ。サブコマンド固有の値を取らないフラグを verb の
// 手前に置いた形 (`gcloud compute instances --dry-run delete vm`) は verb を取りこぼして
// 通る。gcloud の慣習ではフラグは verb の後ろに置くので実害は小さい。取りこぼしが
// 見つかったらここへ足す。
func gcloudFlags(fs *pflag.FlagSet) {
	fs.BoolP("quiet", "q", false, "")
	fs.Bool("async", false, "")
	fs.Bool("log-http", false, "")
	fs.Bool("user-output-enabled", false, "")
	fs.Bool("no-user-output-enabled", false, "")
}

var gcloudRules = &argrule.Group{
	Tool:  "gcloud",
	Flags: gcloudFlags,
	Allow: []argrule.Rule{
		// 認証情報を読むだけのサブコマンド。token の発行は権限の拡大にあたるが、
		// token を使うパッケージの取得のたびに人手を挟むのを避けるため通す。
		//
		// allow は group 横断で deny を追い越すため、先頭の gcloud だけを見る。
		// 引数に埋め込んだコマンド置換は、AST の走査で内側のコマンドとして
		// 別に評価されるのでここでは気にしなくてよい。
		argrule.CmdAt{Tool: "gcloud", Head: true, Path: []string{"auth"}, Exact: true, Verbs: []string{
			"describe",
			"list",
			"print-access-token",
			"print-identity-token",
		}},
		argrule.CmdAt{Tool: "gcloud", Head: true, Path: []string{"auth", "application-default"}, Exact: true, Verbs: []string{
			"print-access-token",
		}},
	},
	Deny: []argrule.Rule{
		// 認証・クレデンシャル操作
		argrule.CmdAt{Tool: "gcloud", Path: []string{"auth"}},
		// 設定変更
		argrule.CmdAt{Tool: "gcloud", Path: []string{"config"}, Exact: true, Verbs: []string{
			"set",
			"unset",
		}},
		// 書き込み操作 (create / add / update / delete 等)。
		// read 系 (list, get, describe, show, logs, events, info, version, help) のみ通過させる。
		argrule.CmdAt{Tool: "gcloud", Nouns: argrule.NounLoose, Verbs: []string{
			"add",
			"create",
			"delete",
			"deploy",
			"disable",
			"enable",
			"export",
			"import",
			"move",
			"remove",
			"reset",
			"resize",
			"restart",
			"set-iam-policy",
			"start",
			"stop",
			"update",
		}},
		argrule.CmdAt{Tool: "gcloud", Path: []string{"storage"}, Verbs: []string{
			"cp",
			"mv",
			"rm",
		}},
	},
	DenyReason: "gcloud の危険な操作 (認証・設定変更・リソース書き込み) をブロックしました",
}
