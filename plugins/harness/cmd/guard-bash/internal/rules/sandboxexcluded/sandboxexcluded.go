// Package sandboxexcluded は sandbox excludedCommands のマッチ外れガード。
//
// settings.json の sandbox.excludedCommands (`docker *` `git *` 等) は
// コマンド文字列の静的マッチで判定される。Claude Code 2.1.278 で実測した限り、
// sandbox の外で動くのは次の形だけで、1 つでも外れるとコマンド全体が sandbox 内で動く。
//
//   - 文のつなぎは `;` と `&&` だけ
//   - 各文は単純コマンドで、コマンド名が excludedCommands の対象か、cwd と同じ場所への cd
//   - 変数の前置きが無い (静的な値でも外れる)
//   - リダイレクトは fd の複製 (`2>&1`) だけ
//   - 引数にコマンド置換が無い (`$VAR` の展開は通る)
//   - git なら、値を取るグローバルオプション (`-C` `-c` `--git-dir=` 等) の前置きが無い
//     (値を取らない `--no-pager` 等は通る。2.1.277 の照合変更による回帰で
//     anthropics/claude-code#95455 として報告済み)
//
// 外れる形を数え上げると Claude Code の更新に追従できない (入力リダイレクトと heredoc は
// 2.1.245 では通り 2.1.278 では外れる) ので、通る形のほうを書き、そこから外れたら deny する。
//
// sandbox に落ちると docker.sock やツールのデータディレクトリへの書き込み、
// loopback 接続、TLS の証明書検証が拒否され、「設定やコードの問題」に見える失敗になる。
// たとえば crit は起動のたびに 127.0.0.1 の daemon へ health probe を投げるが、
// sandbox 内では拒否されるため生きている daemon を死んだと誤判定し、
// ~/.crit/sessions/ の session file を消してしまう。症状から原因を辿るのが
// 難しいので、実行前にこの形を止める。
package sandboxexcluded

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/bashguard"
	"github.com/usadamasa/agents-marketplace/plugins/harness/internal/settings"
	"mvdan.cc/sh/v3/syntax"
)

// 文言は外れた原因と直し方だけにする。sandbox に落ちたときの壊れ方や通る形の一覧は
// harness:sandbox-troubleshooting skill が持つ。1 文目は週次の計測 (scripts/sandboxexcluded-denies.jq) が
// 原因とコマンド名を読むので変えない。2 文目の直し方は shapeHint が形ごとに選ぶ。
const excludedShapeReason = "「%s」 は sandbox.excludedCommands の対象ですが、%sがあるため照合が外れ、コマンド全体が sandbox 内で動きます。%s"

// 他の照合外れと違い、この形には「素の 1 文で実行し直す」書き直しが無い。
// 対象リポジトリを cwd にしたセッションへ移る以外に外で動かす手段が無いので、文言を分ける。
const gitGlobalOptReason = "値を取る git のグローバルオプション (`%s`) を前置した書き込み系の呼び出しは、素の 1 文でも " +
	"sandbox.excludedCommands の照合から外れ、コマンド全体が sandbox 内で動きます " +
	"(Claude Code 2.1.277 の回帰。anthropics/claude-code#95455)。" +
	"`.git/config` の書き込みが EPERM で落ち、ブランチだけ作られた状態が残ります。" +
	"別リポジトリならそれを cwd にした新セッション、同じリポジトリの別 worktree なら `EnterWorktree` で実行し直してください。"

// sandboxTolerant は sandbox 内でも普段の用途が成立するコマンド。
// `go test ./... | tail` のように出力をパイプで受ける形が大半で、止めると得るものより摩擦が大きい。
// task は入れない。task のタスクには、sandbox の外で動くことを前提にしたもの
// (ネットワークへの TLS 接続や ~/.claude への書き込み) があり、
// sandbox に落ちると形ではなくコードの失敗に見える。
// uv はここに入れない。プライベートなパッケージ index の keyring 認証が絡むかどうかで
// sandbox 耐性が変わるため、worksInSandbox で usesRegistryKeyring を見て個別に判定する。
var sandboxTolerant = map[string]bool{"go": true, "node": true}

// usesRegistryKeyring は uv がプライベートなパッケージ index の認証で keyring CLI を呼ぶ設定か。
// この設定が無い uv は公開 PyPI だけを触るので、sandbox 内に落ちても普段どおり成立する。
// ある場合は、照合が外れて sandbox 内に落ちると keyring が ADC を読めず 401 になる
// (harness:sandbox-troubleshooting skill)。
var usesRegistryKeyring = sync.OnceValue(func() bool {
	return os.Getenv("UV_KEYRING_PROVIDER") == "subprocess"
})

// gitReadOnlyVerbs は作業ツリーにも remote にも書かない git の verb。
// sandbox 内で動いても状態を壊さないので、パイプやコマンド置換で受けてよい。
var gitReadOnlyVerbs = map[string]bool{
	"blame": true, "cat-file": true, "check-ignore": true, "describe": true, "diff": true,
	"for-each-ref": true, "grep": true, "log": true, "ls-files": true, "ls-tree": true,
	"ls-remote": true, "merge-base": true, "name-rev": true, "rev-list": true, "rev-parse": true,
	"shortlog": true, "show": true, "show-ref": true, "status": true,
}

// gitBranchListFlags は `git branch` を表示だけに留めるフラグ。これ以外の引数
// (ブランチ名、-d、-m 等) があれば書き込みとみなす。
var gitBranchListFlags = map[string]bool{
	"--show-current": true, "--list": true, "-l": true, "-a": true, "--all": true,
	"-r": true, "--remotes": true, "-v": true, "-vv": true,
}

// gitGlobalOptsWithValue は値を 1 つ取る git のグローバルオプション。
var gitGlobalOptsWithValue = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true, "--exec-path": true,
}

// projectSettingsFiles はプロジェクトスコープの settings ファイル名。
var projectSettingsFiles = []string{"settings.json", "settings.local.json"}

// excludedBases は settings の sandbox.excludedCommands から取り出した
// コマンド名の集合。プロセスにつき 1 回だけ読む。
var excludedBases = sync.OnceValue(loadExcludedBases)

// loadExcludedBases は user スコープとプロジェクトスコープの excludedCommands を合わせる。
// 読めないファイルは飛ばす (プロジェクトスコープは無いのが普通)。
func loadExcludedBases() map[string]bool {
	var paths []string
	if path := os.Getenv("CLAUDE_SETTINGS_JSON"); path != "" {
		paths = append(paths, path)
	} else if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".claude", "settings.json"))
	}
	if project := os.Getenv("CLAUDE_PROJECT_DIR"); project != "" {
		for _, name := range projectSettingsFiles {
			paths = append(paths, filepath.Join(project, ".claude", name))
		}
	}

	bases := map[string]bool{}
	for _, path := range paths {
		s, err := settings.Load(path)
		if err != nil {
			continue
		}
		for _, pattern := range s.Sandbox.ExcludedCommands {
			if name, _, _ := strings.Cut(pattern, " "); name != "" {
				bases[name] = true
			}
		}
	}
	return bases
}

// isExcluded は名前が excludedCommands の対象か。
func isExcluded(name string) bool {
	return name != "" && excludedBases()[name]
}

// Check は、sandbox 内では成立しない excludedCommands 対象を、
// 照合が外れる形で呼ぶのを deny する。cwd は cd の行き先の判定に使う。
func Check(file *syntax.File, cwd string) *bashguard.Verdict {
	name := firstStrict(file, 0)
	if name == "" {
		return nil
	}
	if v := gitGlobalOptBreak(file); v != nil {
		return v
	}
	for _, stmt := range file.Stmts {
		if cause := shapeBreak(stmt, cwd); cause != "" {
			return bashguard.Deny(excludedShapeReason, name, cause, shapeHint(file, stmt, name))
		}
	}
	return nil
}

// firstStrict は sandbox 内では成立しない excludedCommands 対象のうち、最初のものの名前を返す。
// コマンド置換やループの中、`bash -c STR` / `eval STR` の文字列の中も見る
// (シェルへ文字列で渡す形は先頭トークンが bash になり、中のコマンドは必ず sandbox 内で動く)。
func firstStrict(node syntax.Node, depth int) string {
	var hit string
	bashguard.WalkCallExprs(node, func(call *syntax.CallExpr) {
		if hit != "" {
			return
		}
		switch name := bashguard.CommandName(call); {
		case name == "env", name == "direnv":
			// ラッパー越しの git は verb を見ずに strict とする (read-only でも素で実行すれば済む)。
			if wrapped := wrappedName(name, call); isExcluded(wrapped) && !sandboxTolerant[wrapped] {
				hit = wrapped
			}
		case isExcluded(name) && !worksInSandbox(name, call):
			hit = name
		case depth < bashguard.MaxNest:
			if script, ok := bashguard.NestedScript(bashguard.CallArgs(call)); ok {
				if f, err := bashguard.Parse(script); err == nil {
					hit = firstStrict(f, depth+1)
				}
			}
		}
	})
	return hit
}

// gitGlobalOptBreak は、値を取るグローバルオプションを前置した書き込み系の git を deny する。
// read-only な verb は sandbox 内でも成立するので見送る。
// 通る形ではなく外れる形を書いた唯一の例外で、根拠は上流のバグ (anthropics/claude-code#95455)。
// これが閉じたら照合を測り直して、この関数ごと外す (references/maintenance.md)。
func gitGlobalOptBreak(node syntax.Node) *bashguard.Verdict {
	if !isExcluded("git") {
		return nil
	}
	var verdict *bashguard.Verdict
	bashguard.WalkCallExprs(node, func(call *syntax.CallExpr) {
		if verdict != nil || bashguard.CommandName(call) != "git" || gitReadOnly(call) {
			return
		}
		if opt := gitValueGlobalOpt(call); opt != "" {
			verdict = bashguard.Deny(gitGlobalOptReason, opt)
		}
	})
	return verdict
}

// gitValueGlobalOpt は verb より前に置かれた、値を取るグローバルオプションの名前を返す。
// 値が別トークンの形 (`-C <path>`) と密着した形 (`--git-dir=<path>`) の両方を見る。
func gitValueGlobalOpt(call *syntax.CallExpr) string {
	args := bashguard.CallArgs(call)
	for i := 1; i < len(args); i++ {
		name, _, attached := strings.Cut(args[i], "=")
		switch {
		case gitGlobalOptsWithValue[args[i]]:
			return args[i]
		case attached && gitGlobalOptsWithValue[name]:
			return name
		case strings.HasPrefix(args[i], "-"):
		default:
			return ""
		}
	}
	return ""
}

// worksInSandbox は sandbox 内で動いても困らない呼び出しか。
func worksInSandbox(name string, call *syntax.CallExpr) bool {
	if name == "uv" {
		return !usesRegistryKeyring()
	}
	return sandboxTolerant[name] || (name == "git" && gitReadOnly(call))
}

// gitReadOnly は作業ツリーにも remote にも書かない git の呼び出しか。
// verb だけでは決まらないもの (branch / tag / worktree / stash / remote) は引数まで見る。
// 迷う形は false (strict) に倒す。
func gitReadOnly(call *syntax.CallExpr) bool {
	verb, rest := gitVerb(call)
	if gitReadOnlyVerbs[verb] {
		return true
	}
	switch verb {
	case "worktree", "stash":
		return len(rest) > 0 && rest[0] == "list"
	case "tag":
		return len(rest) == 0 || rest[0] == "-l" || rest[0] == "--list"
	case "remote":
		return len(rest) == 0 || rest[0] == "-v"
	case "branch":
		for _, a := range rest {
			if !gitBranchListFlags[a] {
				return false
			}
		}
		return true
	}
	return false
}

// gitVerb はグローバルオプションを読み飛ばして、git の verb とそれ以降の引数を返す。
func gitVerb(call *syntax.CallExpr) (string, []string) {
	args := bashguard.CallArgs(call)
	for i := 1; i < len(args); i++ {
		switch {
		case gitGlobalOptsWithValue[args[i]]:
			i++
		case strings.HasPrefix(args[i], "-"):
		default:
			return args[i], args[i+1:]
		}
	}
	return "", nil
}

// wrappedName はラッパー (`env A=1 cmd` / `direnv exec <dir> cmd`) の実コマンド名を返す。
// どちらも先頭トークンがラッパーになり、中のコマンドは excluded でも sandbox 内で動く。
func wrappedName(wrapper string, call *syntax.CallExpr) string {
	if wrapper == "direnv" {
		args := bashguard.CallArgs(call)
		if len(args) < 4 || args[1] != "exec" {
			return ""
		}
		return args[3][strings.LastIndex(args[3], "/")+1:]
	}
	for _, arg := range call.Args[1:] {
		lit := bashguard.WordLit(arg)
		if lit == "" {
			return ""
		}
		if strings.HasPrefix(lit, "-") || strings.Contains(lit, "=") {
			continue
		}
		return lit[strings.LastIndex(lit, "/")+1:]
	}
	return ""
}

// shapeBreak は文が「sandbox の外で動く形」から外れる原因を返す。外れなければ空文字。
func shapeBreak(stmt *syntax.Stmt, cwd string) string {
	if stmt.Negated || stmt.Background || stmt.Coprocess {
		return "`!` や `&` の付いた文"
	}
	for _, r := range stmt.Redirs {
		if r.Op != syntax.DplOut && r.Op != syntax.DplIn {
			return "リダイレクトや heredoc (`2>&1` 以外)"
		}
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		switch cmd.Op {
		case syntax.AndStmt:
			if cause := shapeBreak(cmd.X, cwd); cause != "" {
				return cause
			}
			return shapeBreak(cmd.Y, cwd)
		case syntax.OrStmt:
			return "`||` でのつなぎ"
		default:
			return "パイプ"
		}
	case *syntax.CallExpr:
		return callBreak(cmd, cwd)
	default:
		return "ループ・subshell・ブロックなどの複合構文"
	}
}

func callBreak(call *syntax.CallExpr, cwd string) string {
	if len(call.Assigns) > 0 {
		return "変数の前置き (静的な値でも外れる)"
	}
	if hasSubst(call) {
		return "引数の中のコマンド置換 (`$(...)` や backtick。single quote の中やエスケープした形でも外れる)"
	}
	name := bashguard.CommandName(call)
	if name == "cd" {
		if cdStaysAt(call, cwd) {
			return ""
		}
		return "cwd の字面と違う cd (同じ場所でも末尾スラッシュや相対表記では外れる)"
	}
	if !isExcluded(name) {
		return fmt.Sprintf("excludedCommands に無い「%s」との連結", name)
	}
	return ""
}

// hasSubst は引数にコマンド置換・プロセス置換か、その字面があるか。
// Claude Code の照合はシェルの構文ではなく文字の並びを見るので、single quote の中や
// エスケープした backtick のように置換として働かない位置でも外れる (2.1.278 で実測)。
func hasSubst(call *syntax.CallExpr) bool {
	found := false
	syntax.Walk(call, func(n syntax.Node) bool {
		switch node := n.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst:
			found = true
		case *syntax.Lit:
			found = found || looksLikeSubst(node.Value)
		case *syntax.SglQuoted:
			found = found || looksLikeSubst(node.Value)
		}
		return !found
	})
	return found
}

func looksLikeSubst(s string) bool {
	return strings.Contains(s, "`") || strings.Contains(s, "$(")
}

// cdStaysAt は `cd <dir>` の行き先が cwd の字面をそのまま指しているか。
// Claude Code の照合はパスを正規化しないので、同じ場所を指していても末尾スラッシュや
// 相対表記では外れる。ハーネスが自動で前置きする形 (`cd <cwd の絶対パス>`) だけが通る。
func cdStaysAt(call *syntax.CallExpr, cwd string) bool {
	args := bashguard.CallArgs(call)
	return cwd != "" && len(args) == 2 && args[1] == cwd
}
