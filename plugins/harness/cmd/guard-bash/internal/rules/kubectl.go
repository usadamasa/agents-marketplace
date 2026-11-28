package rules

import (
	"github.com/spf13/pflag"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
)

// kubectlFlags は verb の手前に置かれうる global flag。
//
// 値を取る flag を宣言しないと、直後の verb が値として読み飛ばされる。値を取らない
// (bool) flag はそれ以上に危険で、宣言し忘れると直後の verb を値として食ってしまい、
// 書き込み verb が位置引数からそっくり消えて Deny を素通りする (gcloudFlags 同様の罠)。
func kubectlFlags(fs *pflag.FlagSet) {
	// 値を取る。
	fs.String("as", "", "")
	fs.StringArray("as-group", nil, "")
	fs.String("as-uid", "", "")
	fs.String("cache-dir", "", "")
	fs.String("certificate-authority", "", "")
	fs.String("client-certificate", "", "")
	fs.String("client-key", "", "")
	fs.String("cluster", "", "")
	fs.String("context", "", "")
	fs.String("kubeconfig", "", "")
	fs.String("log-backtrace-at", "", "")
	fs.String("log-dir", "", "")
	fs.String("log-file", "", "")
	fs.String("log-file-max-size", "", "")
	fs.StringP("namespace", "n", "", "")
	fs.String("password", "", "")
	fs.String("profile", "", "")
	fs.String("profile-output", "", "")
	fs.String("request-timeout", "", "")
	fs.StringP("server", "s", "", "")
	fs.String("stderrthreshold", "", "")
	fs.String("tls-server-name", "", "")
	fs.String("token", "", "")
	fs.String("user", "", "")
	fs.String("username", "", "")
	fs.StringP("v", "v", "", "")
	fs.String("vmodule", "", "")
	// 値を取らない。
	fs.Bool("add-dir-header", false, "")
	fs.Bool("alsologtostderr", false, "")
	fs.Bool("disable-compression", false, "")
	fs.Bool("insecure-skip-tls-verify", false, "")
	fs.Bool("logtostderr", false, "")
	fs.Bool("match-server-version", false, "")
	fs.Bool("one-output", false, "")
	fs.Bool("skip-headers", false, "")
	fs.Bool("skip-log-headers", false, "")
	fs.Bool("warnings-as-errors", false, "")
	// `kubectl config view` の値を取らない修飾子。read 扱いから `--raw`・`--flatten` を
	// 除く判定に使う。どちらも参照ではなく証明書・token の実データを出力に埋め込む。
	fs.Bool("raw", false, "")
	fs.Bool("flatten", false, "")
}

// kubectlWriteVerbs はクラスタの状態を書き換えるトップレベルの verb。
var kubectlWriteVerbs = []string{
	"annotate",
	"apply",
	"attach",
	"autoscale",
	"cordon",
	"cp",
	"create",
	"debug",
	"delete",
	"drain",
	"edit",
	"exec",
	"expose",
	"label",
	"patch",
	"port-forward",
	"replace",
	"run",
	"scale",
	"taint",
	"uncordon",
}

// kubectlReadVerbs はクラスタの状態を変えないトップレベルの verb。
//
// `diff` は含めない。`KUBECTL_EXTERNAL_DIFF` が設定された環境では、diff ツールとして
// 任意のコマンドが実行される (値は変数の前置きでしか渡せないので sandboxexcluded 側の
// 「変数の前置きは常に照合から外れる」判定が既に deny するが、ここでも read 扱いにしない)。
var kubectlReadVerbs = []string{
	"api-resources",
	"api-versions",
	"cluster-info",
	"completion",
	"describe",
	"events",
	"explain",
	"get",
	"logs",
	"options",
	"top",
	"version",
	"wait",
}

// kubectlReadRules は read と確認できる形。ここに当たらない操作は ask へ落ちる
// (`config set-*`、`config use-context`、`certificate`、`auth reconcile`、
// `proxy`、`plugin` 等。既知の書き込みは上の Deny が先に拾う)。
var kubectlReadRules = []argrule.Rule{
	argrule.CmdAt{Tool: "kubectl", Verbs: kubectlReadVerbs, Exact: true},
	argrule.CmdAt{Tool: "kubectl", Path: []string{"auth"}, Verbs: []string{"can-i", "whoami"}, Exact: true},
	argrule.CmdAt{Tool: "kubectl", Path: []string{"config"}, Verbs: []string{
		"current-context",
		"get-clusters",
		"get-contexts",
		"get-users",
	}, Exact: true},
	// config view の --raw・--flatten は証明書・token の実データを埋め込んで出す。
	// どちらも無いときだけ read 扱いにする。
	argrule.When{
		Rule: argrule.CmdAt{Tool: "kubectl", Path: []string{"config"}, Verbs: []string{"view"}, Exact: true},
		Cond: func(cl *argrule.Cmdline) bool {
			return !argrule.AnyView(cl, "kubectl", func(v *argrule.View) bool {
				return v.Changed("raw") || v.Changed("flatten")
			})
		},
		Note: "--raw・--flatten 無し (kubeconfig の証明書・token を平文で出さない)",
	},
	argrule.CmdAt{Tool: "kubectl", Path: []string{"rollout"}, Verbs: []string{"history", "status"}, Exact: true},
}

// kubectlKubeconfigFlag は `--kubeconfig` の指定か。任意のファイルを指せるため、
// exec credential plugin (kubeconfig の `users[].user.exec`) 経由で任意のコマンドを
// 実行させられる。既定の kubeconfig (gcloud get-credentials が書くもの) 以外を
// 指す正当な理由は通常無いので、read/write を問わず deny する。
func kubectlKubeconfigFlag(cl *argrule.Cmdline) bool {
	return argrule.AnyView(cl, "kubectl", func(v *argrule.View) bool { return v.Changed("kubeconfig") })
}

var kubectlRules = &argrule.Group{
	Tool:  "kubectl",
	Flags: kubectlFlags,
	Deny: []argrule.Rule{
		argrule.CmdAt{Tool: "kubectl", Verbs: kubectlWriteVerbs, Exact: true},
		// rollout は history/status が read、それ以外 (restart/pause/resume/undo) は書き込み。
		argrule.CmdAt{Tool: "kubectl", Path: []string{"rollout"}, Verbs: []string{
			"pause",
			"restart",
			"resume",
			"undo",
		}, Exact: true},
		// set 配下 (image/env/resources 等) は全て書き込み。
		argrule.CmdAt{Tool: "kubectl", Path: []string{"set"}},
		argrule.Check{
			Desc: "kubectl --kubeconfig (任意の kubeconfig ファイルを指す。exec credential plugin 経由の任意コマンド実行になりうる)",
			Fn:   kubectlKubeconfigFlag,
		},
	},
	Ask: []argrule.Rule{
		argrule.Check{
			Desc: "kubectl の read (get/describe/logs/top/explain/version/cluster-info/" +
				"api-resources/api-versions/events/wait/options/completion、auth can-i|whoami、" +
				"config の参照系 (view は --raw・--flatten 無しのみ)、rollout の history|status) 以外の操作",
			Fn: kubectlUnknown,
		},
	},
	DenyReason: "kubectl の書き込み操作 (apply・delete・edit・patch・scale・exec の類) と --kubeconfig の指定を" +
		"ブロックしました。クラスタの変更は GitOps 経由で行ってください",
	AskReason: "read と確認できない kubectl の操作です (書き込みを含みうるため確認します)",
}

// kubectlUnknown は read と確認できない kubectl の操作か。
//
// 書き込みの verb を列挙する形は、列挙から漏れた verb (config の書き込み系サブコマンド、
// certificate、auth reconcile、proxy、plugin 等) や `config view --raw`/`--flatten` がそのまま通る。
// 保守的に倒して、read と確認できた形以外は全て ask にする。
func kubectlUnknown(cl *argrule.Cmdline) bool {
	if _, ok := argrule.FirstMatch(kubectlReadRules, cl); ok {
		return false
	}
	// 位置引数が無い形は対象外。他の CLI の引数に "kubectl" の語が現れただけで
	// (`gh pr create --title kubectl`) 確認を出さない。
	return argrule.AnyView(cl, "kubectl", func(v *argrule.View) bool { return len(v.Pos) > 0 })
}
