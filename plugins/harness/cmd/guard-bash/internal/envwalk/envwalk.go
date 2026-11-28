// Package envwalk は AST 中の代入と簡単コマンドを `bash -c` の中まで歩き、visitor に渡す。
// 環境 (PATH や policy の変数) を手で曲げる形を見る rule が、歩き方を持たずに済むようにする。
package envwalk

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/bashguard"
)

// Visitor は Walk が代入とコマンドのそれぞれに渡すコールバック。nil の側は呼ばれない。
type Visitor struct {
	// Assign は代入 1 つ。`A=b cmd` の前置き、`export A=b`、`env A=b cmd` を同じ形で受ける。
	// value は字面 (展開は未評価、`A=` なら空文字)。
	Assign func(name, value string) *bashguard.Verdict
	// Call は簡単コマンド 1 つ。head はコマンド語の字面 (クォートを含む)、args は bashguard.CallArgs。
	Call func(head string, args []string) *bashguard.Verdict
}

// Walk は AST 中の全ての代入と簡単コマンドを出現順に visitor へ渡し、最初に返った Verdict で止まる。
// `bash -c STR` / `eval STR` の中も bashguard.CommandArgs と同じ深さまで辿る
// (中のコマンドは必ず sandbox 内で動く)。
func Walk(src string, file *syntax.File, v Visitor) *bashguard.Verdict {
	return walk(src, file, v, 0)
}

func walk(src string, node syntax.Node, v Visitor, depth int) *bashguard.Verdict {
	var verdict *bashguard.Verdict
	syntax.Walk(node, func(n syntax.Node) bool {
		if verdict != nil {
			return false
		}
		switch cmd := n.(type) {
		case *syntax.DeclClause:
			// `export A=b` は bash では宣言文になり、CallExpr としては現れない。
			for _, as := range cmd.Args {
				if as.Name == nil {
					continue // `export "$name"` のように名前が展開のとき
				}
				if verdict = v.assign(as.Name.Value, assignValueText(src, as)); verdict != nil {
					return false
				}
			}
		case *syntax.CallExpr:
			if verdict = v.visitCall(src, cmd); verdict != nil {
				return false
			}
			if depth >= bashguard.MaxNest {
				return true
			}
			if script, ok := bashguard.NestedScript(bashguard.CallArgs(cmd)); ok {
				if f, err := bashguard.Parse(script); err == nil {
					verdict = walk(script, f, v, depth+1)
				}
			}
		}
		return verdict == nil
	})
	return verdict
}

func (v Visitor) visitCall(src string, call *syntax.CallExpr) *bashguard.Verdict {
	for _, as := range call.Assigns {
		if r := v.assign(as.Name.Value, assignValueText(src, as)); r != nil {
			return r
		}
	}
	if len(call.Args) == 0 {
		return nil
	}
	args := bashguard.CallArgs(call)
	if args[0] == "env" {
		// `env A=b cmd` は引数に代入の字面を持つ。
		for _, arg := range call.Args[1:] {
			name, value, ok := strings.Cut(bashguard.WordText(src, arg), "=")
			if !ok {
				continue
			}
			if r := v.assign(strings.Trim(name, `"'`), value); r != nil {
				return r
			}
		}
	}
	return v.call(bashguard.WordText(src, call.Args[0]), args)
}

func (v Visitor) assign(name, value string) *bashguard.Verdict {
	if v.Assign == nil {
		return nil
	}
	return v.Assign(name, value)
}

func (v Visitor) call(head string, args []string) *bashguard.Verdict {
	if v.Call == nil {
		return nil
	}
	return v.Call(head, args)
}

// assignValueText は代入の右辺の字面を返す。`A=` のように値が無ければ空文字。
func assignValueText(src string, as *syntax.Assign) string {
	if as.Value == nil {
		return ""
	}
	return bashguard.WordText(src, as.Value)
}
