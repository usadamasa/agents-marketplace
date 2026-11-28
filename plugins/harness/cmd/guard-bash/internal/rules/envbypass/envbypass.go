// Package envbypass は direnv / aqua の解決を手で曲げる形 (`direnv exec`、`aqua exec`、`AQUA_*=` / `PATH=` の
// 前置き、shim の絶対パス) を deny する。どれも Bash から direnv の env が外れたときに PATH を手で補う形で、
// 対処は PATH を解決する skill が持つ。
package envbypass

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/bashguard"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/envwalk"
)

const (
	skillHint        = "コマンドの PATH を解決する skill に従ってください。"
	direnvExecReason = "`direnv exec` は使いません。" + skillHint
	aquaExecReason   = "`aqua exec` は使いません。" + skillHint
	aquaVarReason    = "`%s=` の前置きは使いません。" + skillHint
	aquaPathReason   = "PATH に aqua の bin を手で足す形は使いません。" + skillHint
	aquaShimReason   = "aqua の shim や実体を絶対パスで呼ぶ形 (「%s」) は使いません。" + skillHint
)

// aquaResolveVars は aqua の解決先 (設定・policy・root) を変える環境変数。
// AQUA_LOG_LEVEL のように解決を変えないものは対象にしない。
var aquaResolveVars = map[string]bool{
	"AQUA_CONFIG": true, "AQUA_GLOBAL_CONFIG": true, "AQUA_POLICY_CONFIG": true, "AQUA_ROOT_DIR": true,
}

// aquaRootMarker は aqua の root 配下 (shim の bin と pkgs の実体) を字面で見分ける断片。
const aquaRootMarker = "/aquaproj-aqua/"

// Check は direnv / aqua の解決を手で曲げる形を見つけたら deny を返す。`bash -c STR` の中も見る。
func Check(src string, file *syntax.File) *bashguard.Verdict {
	return envwalk.Walk(src, file, envwalk.Visitor{Assign: assignBypass, Call: callBypass})
}

func callBypass(head string, args []string) *bashguard.Verdict {
	if strings.Contains(head, aquaRootMarker) {
		return bashguard.Deny(aquaShimReason, strings.Trim(head, `"'`))
	}
	if len(args) < 2 || args[1] != "exec" {
		return nil
	}
	switch args[0] {
	case "direnv":
		return bashguard.Deny(direnvExecReason)
	case "aqua":
		return bashguard.Deny(aquaExecReason)
	}
	return nil
}

// assignBypass は代入 1 つが aqua の解決を曲げる形か。value は字面 (展開は未評価)。
func assignBypass(name, value string) *bashguard.Verdict {
	switch {
	case aquaResolveVars[name]:
		return bashguard.Deny(aquaVarReason, name)
	case name == "PATH" && strings.Contains(value, aquaRootMarker):
		return bashguard.Deny(aquaPathReason)
	}
	return nil
}
