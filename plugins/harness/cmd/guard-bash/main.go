package main

// guard-bash
// Bash ツール向け PreToolUse ガード。
//
// stdin から PreToolUse JSON を読み、1 プロセスの中で全ての guard を評価する。
// deny/ask に当たった時点で判定 JSON を stdout に出して終了する (first deny wins)。
//
// 評価は 3 段。
//  1. コマンド全体を見る guard: dangerouslyDisableSandbox、direnv / aqua / nix の解決を
//     手で曲げる形、excludedCommands の照合を壊す構文、shell からのファイル書き換え
//  2. 簡単コマンドごとの guard: git config / main への commit / worktree 内の
//     ブランチ操作 / excludedCommands の照合を壊す代入・env ラッパー
//  3. 外部 CLI の破壊的操作 (internal/rules)
//
// 1 と 2 は AST (mvdan.cc/sh) を歩く。3 だけはコマンド文字列を chain 分割して
// テキストで照合する (理由は internal/bashguard の shellast.go の頭)。
// 段を分けてあるのは、後段の ask より前段の deny を優先させるため。
//
// このファイルは hook の入出力と段の順序だけを持つ。コマンド文字列の読み方と
// 判定は internal/bashguard、外部 CLI のルールは internal/rules に閉じる。

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/bashguard"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/envwalk"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/ghauthor"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/rules"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/rules/envbypass"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/rules/golang"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/rules/nix"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/rules/sandboxexcluded"
)

const hookName = "guard-bash"

// hookInput は PreToolUse フックの入力 JSON 構造
type hookInput struct {
	ToolName  string `json:"tool_name"`
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	ToolInput struct {
		Command                   string `json:"command"`
		DangerouslyDisableSandbox bool   `json:"dangerouslyDisableSandbox"`
	} `json:"tool_input"`
}

// decisionResponse は deny/ask 時の出力 JSON 構造
type decisionResponse struct {
	HookSpecificOutput hookOutput `json:"hookSpecificOutput"`
}

type hookOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

func main() {
	rulesDoc := flag.Bool("rules-doc", false, "CLI ごとの方針表を markdown で出力する")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "使い方: %s < JSON\nPreToolUse フック: Bash コマンドをガードする\n\n", hookName)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *rulesDoc {
		fmt.Print(rules.PolicyMarkdown())
		return
	}

	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

// run は PreToolUse JSON を評価し、deny/ask のときだけ判定 JSON を w へ書く。
func run(r io.Reader, w io.Writer) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("stdin 読み取りエラー: %w", err)
	}

	var input hookInput
	if err := json.Unmarshal(data, &input); err != nil {
		// 通してしまうと guard が丸ごと無効になる。判断はユーザーへ渡す。
		return emit(w, bashguard.Ask("PreToolUse 入力の解析に失敗しました (%v)。Bash の guard が評価できていません。", err))
	}

	if input.ToolName != "Bash" {
		return nil
	}

	if v := evaluate(&input); v != nil {
		return emit(w, v)
	}
	return nil
}

// evaluate は 3 段の guard を順に当てる。通過なら nil。
func evaluate(input *hookInput) *bashguard.Verdict {
	if input.ToolInput.DangerouslyDisableSandbox {
		return bashguard.Deny("dangerouslyDisableSandbox は禁止されています。sandbox を無効化せず、必要なら sandbox.network.allowedDomains か excludedCommands への追加を検討してください")
	}

	cmd := input.ToolInput.Command
	file, err := bashguard.Parse(cmd)
	if err != nil {
		// bash 自身も受け付けない形。guard が読めないまま通すより確認を挟む。
		return bashguard.Ask("コマンドの構文を解析できませんでした (%v)。Bash の guard が評価できていないため、実行してよいか確認してください。", err)
	}

	// gh の PR / issue の作成者を照会するときの実行ディレクトリ。--repo を書かない形は
	// ここからリポジトリが解決されるため、判定対象のコマンドが動く cwd を優先する。
	// opt-in を効かせてよいのはプロジェクトのリポジトリへの操作だけなので、プロジェクトも渡す。
	ghauthor.SetWorkdir(resolveWorkdir(input.Cwd))
	ghauthor.SetProjectDir(resolveProjectDir(input.Cwd))
	// git push の名前がタグかも、同じ cwd のリポジトリへ照会する。
	rules.SetGitWorkdir(resolveWorkdir(input.Cwd))
	// コマンドの中で GH_REPO / GH_HOST を代入する形は、gh の対象が hook の照会と食い違う。
	// cd を挟んだ形と同じく照会を止める (作成者も対象リポジトリも確定しないので ask に落ちる)。
	// GIT_DIR などを代入する形も、git の対象が cwd のリポジトリから外れる。
	envwalk.Walk(cmd, file, envwalk.Visitor{Assign: func(name, _ string) *bashguard.Verdict {
		noteAssign(name)
		return nil
	}})
	// Bash のシェルにだけ載る環境変数 (CLAUDE_ENV_FILE) も、コマンドの中の代入と同じに扱う。
	// env ファイルを読めないときは、何が載るか分からないので両方の照会を止める。
	names, ok := sessionEnvAssigns(input.SessionID)
	if !ok {
		ghauthor.DisableLookup()
		rules.DisableGitLookup()
	}
	for _, name := range names {
		noteAssign(name)
	}

	// 破壊的操作の deny は形の deny より先に返す。形を先に返すと、素の形へ実行し直させた後で
	// もう一度 deny することになり、止めた本当の理由が後回しになる。ask は最後に返す
	// (先に返すと、承認された時点で照合の外れた形のまま sandbox 内で走る)。
	ruled := checkRules(input, bashguard.CommandArgs(file))
	if ruled != nil && ruled.Decision == "deny" {
		return ruled
	}
	if v := golang.ModuleTrust(file); v != nil {
		return v
	}
	// direnv / aqua / nix を手で曲げる形は照合外れの一種だが、対処が skill に決まっているので
	// 汎用の「素の 1 文で実行し直す」より先に、その skill へ誘導する。
	if v := envbypass.Check(cmd, file); v != nil {
		return v
	}
	if v := nix.Check(cmd, file); v != nil {
		return v
	}
	// excludedCommands の照合は Claude Code がセッションの cwd を基準に行う (cwd への cd だけ無害)。
	if v := sandboxexcluded.Check(file, resolveWorkdir(input.Cwd)); v != nil {
		return v
	}
	if v := bashguard.InlineFileEdit(cmd, file); v != nil {
		return v
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	if v := bashguard.PerCommand(file, cwd); v != nil {
		return v
	}
	return ruled
}

// noteAssign は環境変数 name の代入を受けて、hook の照会と食い違う対象の照会を止める。
func noteAssign(name string) {
	switch {
	case name == "GH_REPO" || name == "GH_HOST":
		ghauthor.DisableLookup()
	// GIT_CONFIG_COUNT / GIT_CONFIG_KEY_<n> / GIT_CONFIG_GLOBAL などは、hook が読む設定と食い違う。
	case name == "GIT_DIR" || name == "GIT_WORK_TREE" || name == "GIT_COMMON_DIR" || name == "GIT_NAMESPACE",
		strings.HasPrefix(name, "GIT_CONFIG"):
		rules.DisableGitLookup()
	}
}

// checkRules は外部 CLI の破壊的操作を簡単コマンドごとに照合する。
//
// deny が 1 つでもあれば deny、なければ最初の ask を返す。最初の ask で打ち切ると、
// `gh pr merge 5; gh repo delete x` の後ろの deny に届かない。
func checkRules(input *hookInput, calls [][]string) *bashguard.Verdict {
	// gh api graphql の query=@<path> は、単独のコマンドのときだけ cwd から解決して読む。
	// 複合コマンドでは前段がファイルを書き換えたり cd したりできるので読まない。
	queryDir := ""
	if len(calls) == 1 {
		queryDir = resolveWorkdir(input.Cwd)
	}
	rules.SetGHQueryDir(queryDir)

	// opt-in の判定は settings ファイルの読み取りを伴う。大半の Bash 呼び出しは
	// ask にすら当たらないので、最初に必要になるまで遅延させてキーごとに 1 回だけ評価する。
	optInFor := func(key string) func() optInResult {
		return sync.OnceValue(func() optInResult {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return optInResult{Reason: fmt.Sprintf("ホームディレクトリを解決できないため opt-in を無効にしました: %v", err)}
			}
			return ghWritesOptIn(key, resolveProjectDir(input.Cwd), homeDir)
		})
	}
	optIns := []ghOptIn{
		{match: rules.GHPRWriteOptIn, result: optInFor(ghPRWritesKey)},
		{match: rules.GHIssueWriteOptIn, result: optInFor(ghIssueWritesKey)},
	}

	var ask *bashguard.Verdict
	for _, args := range calls {
		cmd := strings.Join(args, " ")
		// cd を挟んだ後ろのコマンドは、どのリポジトリで動くか分からない。
		// カレントリポジトリへ作成者を問い合わせると、同じ番号の別の PR を
		// 見て「自分のもの」と誤判定しうるため、以降は照会を止めて ask に落とす。
		if len(args) > 0 && (args[0] == "cd" || args[0] == "pushd" || args[0] == "popd") {
			ghauthor.DisableLookup()
			rules.DisableGitLookup()
		}
		switch v := rules.Evaluate(args); v.Decision {
		case rules.Deny:
			logInfo("BLOCKED: %s (pattern: %s)", cmd, v.Pattern)
			return &bashguard.Verdict{Decision: "deny", Reason: v.Reason}
		case rules.Ask:
			// deny ではなく ask に落ちた gh の PR / issue 書き込み操作だけ、opt-in で通す。
			// deny を覆す経路は無い (この分岐に来ない)。
			if allowedByOptIn(optIns, args) {
				continue
			}
			logInfo("ASK: %s (pattern: %s)", cmd, v.Pattern)
			if ask == nil {
				ask = &bashguard.Verdict{Decision: "ask", Reason: v.Reason}
			}
		case rules.Pass:
			if v.Pattern != "" {
				logInfo("ALLOWED: %s (pattern: %s)", cmd, v.Pattern)
			}
		}
	}
	return ask
}

// ghOptIn は「opt-in の対象となるコマンドの判定」と「その opt-in キーが有効かの判定」の組。
// キーごとに 1 つ作り、対象のコマンドに当たったときだけ settings を読む。
type ghOptIn struct {
	match  func([]string) bool
	result func() optInResult
}

// allowedByOptIn は ask に落ちたコマンドを opt-in で通してよいかを返す。
func allowedByOptIn(optIns []ghOptIn, args []string) bool {
	for _, o := range optIns {
		if !o.match(args) {
			continue
		}
		r := o.result()
		if r.Enabled {
			logInfo("OPT-IN ALLOWED: %s (%s)", strings.Join(args, " "), r.Reason)
			return true
		}
		if r.Reason != "" {
			logInfo("OPT-IN 不成立: %s", r.Reason)
		}
	}
	return false
}

// emit は PreToolUse の判定 JSON を出力する。
func emit(w io.Writer, v *bashguard.Verdict) error {
	out, err := json.Marshal(decisionResponse{
		HookSpecificOutput: hookOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       v.Decision,
			PermissionDecisionReason: v.Reason,
		},
	})
	if err != nil {
		return fmt.Errorf("JSON 生成エラー: %w", err)
	}
	out = append(out, '\n')
	_, err = w.Write(out)
	return err
}

// logInfo は ほかの hook のログと同じ書式で stderr へ出力する。
func logInfo(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[%s] INFO   %s\n", hookName, fmt.Sprintf(format, args...))
}
