package bashguard

// 簡単コマンドごとの guard を出現順に当てる段。

import (
	"mvdan.cc/sh/v3/syntax"
)

// PerCommand は簡単コマンドごとの guard を出現順に当てる。通過なら nil。
// deny があれば最初の deny、無ければ最初の ask を返す (ask で打ち切ると後ろの deny を見落とす)。
//
// chain の途中の cd は、以降のコマンドが動くリポジトリを変える。ネストしたクローンでは
// プロセスの cwd と実際に操作されるリポジトリが食い違うため、cd を追跡して渡す。
func PerCommand(file *syntax.File, cwd string) *Verdict {
	targetDir := cwd
	scopes := cdScopeRanges(file)
	var deny, ask *Verdict
	WalkCallExprs(file, func(call *syntax.CallExpr) {
		if deny != nil {
			return
		}
		if !inRanges(scopes, call.Pos().Offset()) {
			targetDir = resolveCdTarget(call, targetDir)
		}
		for _, check := range []func(*syntax.CallExpr) *Verdict{
			gitConfig,
			func(c *syntax.CallExpr) *Verdict { return gitCommitMain(c, targetDir) },
			worktreeBranch,
		} {
			v := check(call)
			switch {
			case v == nil:
				continue
			case v.Decision == "deny":
				deny = v
				return
			case ask == nil:
				ask = v
			}
		}
	})
	if deny != nil {
		return deny
	}
	return ask
}

// cdScopeRanges は cd を追跡しない範囲を返す。
//
// subshell `( ... )`・コマンド置換・プロセス置換・関数定義の中の cd は、抜けた後の
// コマンドには効かない。追跡すると `(cd nested && true) && git commit` を nested の
// リポジトリで判定してしまい、cwd 側のブランチを見ない分だけ甘くなる。
// 中を追いかけて元へ戻すこともできるが、cd の効果が閉じる位置を静的に決められない
// 形 (`cd $x`、関数呼び出し) が残るため、範囲ごと追跡しない。
func cdScopeRanges(file *syntax.File) [][2]uint {
	var ranges [][2]uint
	syntax.Walk(file, func(n syntax.Node) bool {
		switch n.(type) {
		case *syntax.Subshell, *syntax.CmdSubst, *syntax.ProcSubst, *syntax.FuncDecl:
			ranges = append(ranges, [2]uint{n.Pos().Offset(), n.End().Offset()})
		}
		return true
	})
	return ranges
}

func inRanges(ranges [][2]uint, offset uint) bool {
	for _, r := range ranges {
		if offset >= r[0] && offset < r[1] {
			return true
		}
	}
	return false
}
