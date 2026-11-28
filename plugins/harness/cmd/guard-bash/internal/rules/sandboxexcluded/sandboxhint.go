package sandboxexcluded

// 照合外れの deny の 2 文目 (代わりの書き方) を、deny した文の形から選ぶ。
// 原因の名前だけでは実行し直しが 1 往復で済まないことが多いので、形ごとに通る書き方を示す。
// 示すのは意味の変わらない書き方だけにする。理由はモデルへの指示として読まれるので、
// 誤った代替は汎用の文言より高くつく。表に無い形は汎用の文言に落とす。

import (
	"fmt"
	"slices"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/bashguard"
	"mvdan.cc/sh/v3/syntax"
)

const defaultHint = "%s を素の 1 文で実行し直してください (入出力はフラグで渡す。通る形は harness:sandbox-troubleshooting skill)。"

const exitStatusHint = "終了コードは Bash ツールが結果に添えて返すので、`echo \"exit=$?\"` の類は外してください。"

// hintRule は deny した文 (stmt) と excluded のコマンド名から代わりの書き方を返す。当てはまらなければ空文字。
type hintRule func(file *syntax.File, stmt *syntax.Stmt, name string) string

// 先に当たったものを使う。ループの中のパイプはループとして扱うため、loopHint を pipeHint より前に置く。
var hintRules = []hintRule{loopHint, sleepHint, mkdirTmpHint, uvHint, pipeHint, substHint, chainHint}

// pipeFilters は出力を絞るだけのパイプの下流。gh なら `--jq` で代わりが利く。
var pipeFilters = []string{"head", "tail", "rg", "grep", "jq", "wc", "sort", "uniq", "cut"}

// shapeHint は deny の 2 文目を返す。
func shapeHint(file *syntax.File, stmt *syntax.Stmt, name string) string {
	hint := fmt.Sprintf(defaultHint, name)
	for _, rule := range hintRules {
		if h := rule(file, stmt, name); h != "" {
			hint = h
			break
		}
	}
	if hasExitStatusEcho(file) {
		hint += exitStatusHint
	}
	return hint
}

func loopHint(_ *syntax.File, stmt *syntax.Stmt, _ string) string {
	switch stmt.Cmd.(type) {
	case *syntax.WhileClause, *syntax.ForClause:
	default:
		return ""
	}
	if containsCall(stmt, isGHWait) {
		return "`gh pr checks <番号> --watch` と `gh run watch <id>` は完了まで自分で待つので、ループを外して 1 文で実行してください。"
	}
	return "ループを外し、対象を 1 つずつ別の呼び出しに分けて実行してください。"
}

func sleepHint(file *syntax.File, stmt *syntax.Stmt, _ string) string {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || bashguard.CommandName(call) != "sleep" || !containsCall(file, isGHWatch) {
		return ""
	}
	return "`--watch` は完了まで自分で待つので、`sleep` を外して実行してください。"
}

func mkdirTmpHint(_ *syntax.File, stmt *syntax.Stmt, _ string) string {
	if !containsCall(stmt, isMkdirTmp) {
		return ""
	}
	return "`mkdir -p ./tmp` は別の呼び出しに分けて先に実行してください。"
}

// uvHint は `cd <dir> && uv ...` の形を、`--project` へ渡す書き方へ誘導する。
func uvHint(_ *syntax.File, stmt *syntax.Stmt, name string) string {
	if name != "uv" || !containsCall(stmt, isCd) {
		return ""
	}
	return "対象ディレクトリへは `cd` で移らず `--project <絶対パス>` で渡し、`uv` を素の 1 文で実行し直してください。"
}

func isCd(call *syntax.CallExpr) bool {
	return bashguard.CommandName(call) == "cd"
}

func pipeHint(_ *syntax.File, stmt *syntax.Stmt, name string) string {
	downstream, ok := pipeDownstream(stmt)
	switch {
	case !ok:
		return ""
	case name == "ghq" && (downstream == "rg" || downstream == "grep"):
		return "`ghq list <query>` で絞れます (名前にクエリを含むリポジトリだけを出す)。"
	case name == "gh" && downstream == "base64":
		return "`gh api` の contents / readme は `-H 'Accept: application/vnd.github.raw+json'` を付けると中身がそのまま返るので、パイプを外して実行してください。"
	case name == "gh" && slices.Contains(pipeFilters, downstream):
		return "`--jq` で絞るか、パイプを外して出力をそのまま読んでください。"
	}
	return fmt.Sprintf("パイプを外して %s を素で実行し、絞り込みは出力を読んでから行ってください。", name)
}

func substHint(_ *syntax.File, stmt *syntax.Stmt, _ string) string {
	if containsNode(stmt, func(n syntax.Node) bool {
		switch n.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst:
			return true
		}
		return false
	}) {
		return "内側のコマンドを先に単独で実行し、得た値を引数に書いてください。"
	}
	if containsCall(stmt, hasSubst) {
		return "single quote の中でも backtick や `$(` の字面で外れます。本文はファイルに書いて `-F` / `--body-file` で渡し、" +
			"`--jq` の式なら backtick と `$(` を含まない形に書き換えてください。"
	}
	return ""
}

func chainHint(_ *syntax.File, stmt *syntax.Stmt, name string) string {
	var other string
	bashguard.WalkCallExprs(stmt, func(call *syntax.CallExpr) {
		n := bashguard.CommandName(call)
		if other == "" && n != "" && n != "cd" && !isExcluded(n) && !isExitStatusEcho(call) {
			other = n
		}
	})
	if other == "" {
		return ""
	}
	return fmt.Sprintf("「%s」 は別の Bash 呼び出しに分け、%s だけを 1 文で実行してください。", other, name)
}

// pipeDownstream は文の中の最初のパイプについて、下流の最初のコマンド名を返す。
func pipeDownstream(stmt *syntax.Stmt) (string, bool) {
	var downstream string
	found := false
	syntax.Walk(stmt, func(n syntax.Node) bool {
		bin, ok := n.(*syntax.BinaryCmd)
		if found || !ok || (bin.Op != syntax.Pipe && bin.Op != syntax.PipeAll) {
			return !found
		}
		found = true
		bashguard.WalkCallExprs(bin.Y, func(call *syntax.CallExpr) {
			if downstream == "" {
				downstream = bashguard.CommandName(call)
			}
		})
		return false
	})
	return downstream, found
}

func containsNode(node syntax.Node, match func(syntax.Node) bool) bool {
	found := false
	syntax.Walk(node, func(n syntax.Node) bool {
		found = found || match(n)
		return !found
	})
	return found
}

func containsCall(node syntax.Node, match func(*syntax.CallExpr) bool) bool {
	return containsNode(node, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		return ok && match(call)
	})
}

// isGHWait は gh で完了を待つ呼び出し (pr checks、run の参照) か。
func isGHWait(call *syntax.CallExpr) bool {
	args := bashguard.CallArgs(call)
	return len(args) >= 3 && args[0] == "gh" &&
		((args[1] == "pr" && args[2] == "checks") || args[1] == "run")
}

func isGHWatch(call *syntax.CallExpr) bool {
	args := bashguard.CallArgs(call)
	return len(args) >= 3 && args[0] == "gh" &&
		((args[1] == "run" && args[2] == "watch") || slices.Contains(args, "--watch"))
}

// isMkdirTmp は ./tmp そのものを作る呼び出しか。./tmp の下を作る形は別の呼び出しに分ける以外の
// 代わりが無いので、chainHint に任せる。
func isMkdirTmp(call *syntax.CallExpr) bool {
	args := bashguard.CallArgs(call)
	return len(args) == 3 && args[0] == "mkdir" && args[1] == "-p" && (args[2] == "./tmp" || args[2] == "./tmp/")
}

func hasExitStatusEcho(file *syntax.File) bool {
	return containsCall(file, isExitStatusEcho)
}

// isExitStatusEcho は直前の終了コード (`$?`、`${PIPESTATUS[0]}`、zsh の `$pipestatus`) を表示する echo / printf か。
func isExitStatusEcho(call *syntax.CallExpr) bool {
	if n := bashguard.CommandName(call); n != "echo" && n != "printf" {
		return false
	}
	return containsNode(call, func(n syntax.Node) bool {
		p, ok := n.(*syntax.ParamExp)
		return ok && p.Param != nil && slices.Contains([]string{"?", "PIPESTATUS", "pipestatus"}, p.Param.Value)
	})
}
