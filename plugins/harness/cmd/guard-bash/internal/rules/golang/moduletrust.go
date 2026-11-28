// Package golang は Go のツールチェーン (go コマンドと GO* 環境変数) に向けた guard を持つ。
package golang

// モジュール取得で、proxy と sumdb の照合を迂回する設定を止める guard。
//
// go は excludedCommands だが、`cd x && go get` のように照合から外れた形で実行すると sandbox 内で動き、
// trustd を塞がれて proxy.golang.org への TLS 検証に失敗する (x509: OSStatus -26276)。そこで
// GOPROXY=direct や GONOSUMDB で照合を外して取得を通すと、改ざんされたモジュールが go.sum に
// 正しいハッシュとして記録されうる。素の 1 文で実行すれば sandbox の外で proxy に届く。

import (
	"fmt"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/bashguard"
)

// noVerifyVars は空でない値を入れた時点で proxy か照合のどちらかを外す変数。
// GOPRIVATE も同じ効果を持ち、公開モジュールを指せば照合を迂回できる。非公開モジュールの取得に
// 要るので、それを承知で値を問わず通す。
var noVerifyVars = []string{"GONOSUMDB", "GONOSUMCHECK", "GONOPROXY", "GOINSECURE"}

// proxyElems は GOPROXY のリストに置いてよい要素。direct は先頭以外 (proxy が 404/410 を返した
// ときの後段) に限る。
var proxyElems = []string{"https://proxy.golang.org", "https://proxy.golang.org/", "direct", "off"}

// ModuleTrust は Go のモジュール取得で proxy と sumdb の照合を迂回する設定を止める。通過なら nil。
//
// 前置きの代入・単独の代入・export / declare・env・`go env -w` と、`bash -c` の中を見る。
// 値が展開で決まらないときは止める。
func ModuleTrust(file *syntax.File) *bashguard.Verdict {
	return check(file, 0)
}

func check(node syntax.Node, depth int) *bashguard.Verdict {
	var found *bashguard.Verdict
	syntax.Walk(node, func(n syntax.Node) bool {
		if found != nil {
			return false
		}
		switch x := n.(type) {
		case *syntax.Assign:
			// `export GOPROXY` のような値の無い形は既存の値を渡すだけなので見ない。
			if x.Name != nil && !x.Naked {
				found = checkAssign(x.Name.Value, x.Value)
			}
		case *syntax.CallExpr:
			found = checkCall(x, depth)
		}
		return found == nil
	})
	return found
}

// checkCall は env と `go env -w` の NAME=VALUE 引数と、`bash -c` の中を見る。
func checkCall(call *syntax.CallExpr, depth int) *bashguard.Verdict {
	args := bashguard.CallArgs(call)
	if len(args) == 0 {
		return nil
	}
	isEnv := args[0] == "env"
	isGoEnvW := args[0] == "go" && len(args) > 2 && args[1] == "env" && slices.Contains(args[2:], "-w")
	if isEnv || isGoEnvW {
		for _, w := range call.Args[1:] {
			name, ok := assignName(w)
			if !ok {
				continue
			}
			known := !bashguard.HasExpansion(w)
			value, _ := strings.CutPrefix(bashguard.WordLit(w), name+"=")
			if v := checkVar(name, value, known, isGoEnvW); v != nil {
				return v
			}
		}
	}
	if depth >= bashguard.MaxNest {
		return nil
	}
	if script, ok := bashguard.NestedScript(args); ok {
		if f, err := bashguard.Parse(script); err == nil {
			return check(f, depth+1)
		}
	}
	return nil
}

// assignName は語が NAME=VALUE の形なら NAME を返す。先頭の字面だけを見るので、値に展開を含んでもよい。
func assignName(w *syntax.Word) (string, bool) {
	if len(w.Parts) == 0 {
		return "", false
	}
	lit, ok := w.Parts[0].(*syntax.Lit)
	if !ok {
		return "", false
	}
	name, _, ok := strings.Cut(lit.Value, "=")
	return name, ok && name != ""
}

// checkAssign はシェルの代入 (前置き・単独・export / declare) を見る。
func checkAssign(name string, value *syntax.Word) *bashguard.Verdict {
	if value == nil {
		return checkVar(name, "", true, false)
	}
	return checkVar(name, bashguard.WordLit(value), !bashguard.HasExpansion(value), false)
}

// checkVar は変数 1 つの値を判定する。known が false なら値が静的に決まらない。
// persist は `go env -w` で設定ファイルへ書く形か。
func checkVar(name, value string, known, persist bool) *bashguard.Verdict {
	switch {
	case name == "GOPROXY":
		if known && proxyTrusted(value) {
			return nil
		}
	case name == "GOSUMDB":
		if known && (value == "" || value == "sum.golang.org") {
			return nil
		}
	case slices.Contains(noVerifyVars, name):
		if known && value == "" {
			return nil
		}
	default:
		return nil
	}
	return bashguard.Deny("%s", denyReason(name, known, persist))
}

// denyReason は止めた変数と、代わりに実行する形を案内する。
func denyReason(name string, known, persist bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s の書き換えは Go のモジュール取得で proxy か sumdb の照合を迂回するのでブロックしました。", name)
	switch {
	case !known:
		sb.WriteString("値が展開で決まらず判定できません。値を字面で書いてください " +
			"(GOPROXY は proxy.golang.org を経由するリスト、GOSUMDB は sum.golang.org だけを通します)。")
	case persist:
		fmt.Fprintf(&sb, "go env -w で設定に残さないでください。既に書き込んだなら go env -u %s で既定値に戻せます。", name)
	}
	sb.WriteString("取得が sandbox の TLS で落ちるなら、go.mod のあるディレクトリを cwd にして、" +
		"変数も cd も付けず素の 1 文 (例: go get <module>@<version>) で実行してください。" +
		"go は excludedCommands なので、その形なら sandbox の外で proxy.golang.org に届きます。")
	return sb.String()
}

// proxyTrusted は GOPROXY の値が proxy.golang.org を経由する (か off の) リストか。空は既定値になる。
func proxyTrusted(value string) bool {
	if value == "" {
		return true
	}
	elems := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '|' })
	if len(elems) == 0 || elems[0] == "direct" {
		return false
	}
	for _, e := range elems {
		if !slices.Contains(proxyElems, e) {
			return false
		}
	}
	return true
}
