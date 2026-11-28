package bashguard

import (
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// resolveCdTarget は簡単コマンドが単純な `cd <dir>` なら current 起点で解決した
// 絶対パスを、そうでなければ current をそのまま返す。
//
// chain 内で cd した先のリポジトリを guard の判定対象にするために使う。ネストした
// クローンでは、プロセスの cwd と実際に操作されるリポジトリが食い違うため、cwd だけで
// 判定すると誤検知と見逃しの両方が起きる。
//
// 引数なし cd / cd - / 変数展開やチルダを含む cd は、静的に行き先を決められないので
// 追跡しない (current のまま = 従来の挙動へフォールバック)。
func resolveCdTarget(call *syntax.CallExpr, current string) string {
	args := CallArgs(call)
	if len(args) != 2 || args[0] != "cd" {
		return current
	}
	arg := args[1]
	if arg == "" || arg == "-" || strings.HasPrefix(arg, "~") {
		return current
	}
	target := arg
	if !filepath.IsAbs(target) {
		target = filepath.Join(current, target)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		return current
	}
	return filepath.Clean(target)
}
