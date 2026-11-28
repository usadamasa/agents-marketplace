// Package nix は nix の devShell や store を手で曲げる形 (`nix develop -c`、`nix shell -c`、`nix run`、
// /nix/store の絶対パス、PATH への /nix/store の前置き) を deny する。どれも `.envrc` が未反映なことを
// 隠す形で、対処は PATH を解決する skill が持つ。
package nix

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/bashguard"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/envwalk"
)

const (
	skillHint   = "コマンドの PATH を解決する skill に従ってください。"
	wrapReason  = "`nix %s` でコマンドを包む形は使いません。" + skillHint
	pathReason  = "PATH に /nix/store のパスを手で足す形は使いません。" + skillHint
	storeReason = "/nix/store の実体を絶対パスで呼ぶ形 (「%s」) は使いません。" + skillHint
)

// storeMarker は nix の store path を字面で見分ける断片。
const storeMarker = "/nix/store/"

// Check は nix の解決を手で曲げる形を見つけたら deny を返す。`bash -c STR` の中も見る。
func Check(src string, file *syntax.File) *bashguard.Verdict {
	return envwalk.Walk(src, file, envwalk.Visitor{Assign: pathAssign, Call: call})
}

func call(head string, args []string) *bashguard.Verdict {
	if strings.Contains(head, storeMarker) {
		return bashguard.Deny(storeReason, strings.Trim(head, `"'`))
	}
	if args[0] != "nix" {
		return nil
	}
	return wrap(args)
}

// wrap は `nix develop -c <cmd>`、`nix shell … -c <cmd>`、`nix run …` を deny する。
// `nix develop -c true` だけは通す。cold の store を温めるための形で、包む仕事が無い
// (PATH を解決する skill の、nix の評価のキャッシュの節)。
func wrap(args []string) *bashguard.Verdict {
	if len(args) < 2 {
		return nil
	}
	sub := args[1]
	switch sub {
	case "run":
		return bashguard.Deny(wrapReason, sub)
	case "develop", "shell":
	default:
		return nil
	}
	i := slices.IndexFunc(args[2:], func(a string) bool { return a == "-c" || a == "--command" })
	if i < 0 {
		return nil
	}
	wrapped := args[2+i+1:]
	if sub == "develop" && len(wrapped) == 1 && wrapped[0] == "true" {
		return nil
	}
	return bashguard.Deny(wrapReason, sub)
}

// pathAssign は代入 1 つが PATH に /nix/store を足す形か。value は字面 (展開は未評価)。
func pathAssign(name, value string) *bashguard.Verdict {
	if name != "PATH" || !strings.Contains(value, storeMarker) {
		return nil
	}
	return bashguard.Deny(pathReason)
}
