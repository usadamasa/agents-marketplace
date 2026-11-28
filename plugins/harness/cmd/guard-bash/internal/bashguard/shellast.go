// Package bashguard は Bash コマンドの読み取りと判定を持つ。
//
// 構文解析 (AST)・AST から語やコマンド名を取り出すヘルパー・chain 分割と、
// それらの上に載る guard (ファイル書き換え / git config /
// main への commit / worktree 内のブランチ操作 / excludedCommands の照合外れ)
// までをここに閉じる。hook の入出力と段の順序は main が持つ。
package bashguard

// コマンド文字列の構文解析。
//
// guard は「シェルが解釈する前のテキスト」を受け取る。quote・heredoc・
// コマンド置換・リダイレクトの構造を自前の文字ループで追うと、判定ごとに
// 別々の近似パーサが増える。ここでは mvdan.cc/sh の
// bash パーサへ寄せ、各 guard は AST を歩くだけにする。
//
// ルール照合 (internal/rules) も簡単コマンドごとの argv で行う (CommandArgs)。
// `bash -c STR` は AST 上では「bash に文字列を 1 つ渡す」でしかないので、
// シェルへ文字列で渡す形だけは文字列を再解析して中のコマンドも並べる。
// heredoc やパイプでシェルへ流し込む形 (`bash <<EOF`) は辿らない。

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Parse は Bash のコマンド文字列を AST へ読む。
// 解析に失敗するのは bash 自身も受け付けない形なので、呼び出し側は ask に落とす。
//
// ponytail: bash 方言だけを読む。Bash ツールが zsh を起動する環境では zsh 固有の
// 構文 (`${(f)x}` / `=cmd` / `repeat`) が解析に失敗して ask になる。素通りではなく
// 確認が増える側へ倒れるので実害は小さい。頻度が問題になったら
// syntax.Variant の切り替えを足す。
func Parse(cmd string) (*syntax.File, error) {
	return syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(cmd), "")
}

// WalkCallExprs は AST 中の全ての簡単コマンドを出現順に渡す。
func WalkCallExprs(node syntax.Node, fn func(*syntax.CallExpr)) {
	syntax.Walk(node, func(n syntax.Node) bool {
		if call, ok := n.(*syntax.CallExpr); ok {
			fn(call)
		}
		return true
	})
}

// CommandName は簡単コマンドの実行されるコマンド名 (basename) を返す。
// 変数展開などでコマンド名が静的に決まらないときは空文字を返す。
func CommandName(call *syntax.CallExpr) string {
	if len(call.Args) == 0 {
		return ""
	}
	lit := WordLit(call.Args[0])
	if lit == "" {
		return ""
	}
	return lit[strings.LastIndex(lit, "/")+1:]
}

// WordLit は展開を含まない語の文字列を返す。クォートは剥がす。
// 展開 (コマンド置換・変数展開) を含む語では空文字を返す。
func WordLit(w *syntax.Word) string {
	var sb strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			sb.WriteString(p.Value)
		case *syntax.SglQuoted:
			sb.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok {
					return ""
				}
				sb.WriteString(lit.Value)
			}
		default:
			return ""
		}
	}
	return sb.String()
}

// WordText は語をソース上の字面のまま返す (クォートを剥がさない)。
func WordText(src string, w *syntax.Word) string {
	return src[w.Pos().Offset():w.End().Offset()]
}

// CallArgs は簡単コマンドの引数を文字列で返す。args[0] はコマンド名 (basename)。
// 展開を含む引数は空文字になる。
func CallArgs(call *syntax.CallExpr) []string {
	if len(call.Args) == 0 {
		return nil
	}
	args := make([]string, 0, len(call.Args))
	args = append(args, CommandName(call))
	for _, a := range call.Args[1:] {
		args = append(args, WordLit(a))
	}
	return args
}

// shells は `-c` で文字列をコマンドとして実行するシェル。
var shells = map[string]bool{"bash": true, "sh": true, "zsh": true, "dash": true, "ksh": true}

// MaxNest は `bash -c` / `eval` の再解析を辿る深さの上限。
const MaxNest = 4

// CommandArgs は AST 中の全ての簡単コマンドの argv を出現順に返す。
//
// `bash -c STR` と `eval STR...` は、AST 上では「シェルに文字列を渡す」でしかなく、
// 中の危険操作が見えない。STR を再解析して、中の簡単コマンドも同じ列に並べる。
// 再解析できない文字列 (展開を含む・構文エラー) は辿らない。
func CommandArgs(file *syntax.File) [][]string {
	var out [][]string
	collectArgs(file, 0, &out)
	return out
}

func collectArgs(node syntax.Node, depth int, out *[][]string) {
	WalkCallExprs(node, func(call *syntax.CallExpr) {
		args := CallArgs(call)
		*out = append(*out, args)
		if depth >= MaxNest {
			return
		}
		script, ok := NestedScript(args)
		if !ok {
			return
		}
		if f, err := Parse(script); err == nil {
			collectArgs(f, depth+1, out)
		}
	})
}

// NestedScript は argv が文字列をコマンドとして実行する形なら、その文字列を返す。
func NestedScript(args []string) (string, bool) {
	if len(args) < 2 {
		return "", false
	}
	if args[0] == "eval" {
		return strings.Join(args[1:], " "), true
	}
	if !shells[args[0]] {
		return "", false
	}
	// `-c` は `-ec` `-lc` のように束ねられる。最初の非オプション引数が文字列になる。
	for i, a := range args[1:] {
		if !strings.HasPrefix(a, "-") || strings.HasPrefix(a, "--") {
			continue
		}
		if strings.Contains(a, "c") && i+2 < len(args) && args[i+2] != "" {
			return args[i+2], true
		}
	}
	return "", false
}

// HasExpansion は語が展開 (変数展開・コマンド置換・算術展開) を含むか。
func HasExpansion(w *syntax.Word) bool {
	found := false
	syntax.Walk(w, func(n syntax.Node) bool {
		switch n.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ProcSubst:
			found = true
			return false
		}
		return true
	})
	return found
}
