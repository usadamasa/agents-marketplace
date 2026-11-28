package bashguard

// git まわりの guard 3 本。
//   - git config の書き込み (読み取りと worktree 用 remote.origin.fetch だけ許す)
//   - main / master への直接 commit
//   - worktree 内でのブランチ操作 (checkout / switch / branch 作成。gh stack は ask)

import (
	"os/exec"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

const gitConfigReason = "git config の書き込みはフックによりブロックされました。読み取り (--get, --list) と remote.origin.fetch の設定のみ許可されています。"

const worktreeBranchReason = "worktree 内でのブランチ操作はブロックされました。`EnterWorktree` ツールで新しい worktree を作成してください。ファイル復元には `git restore` を使用してください。"

const worktreeGhStackReason = "worktree 内の gh stack はブランチの作成や checkout を伴うことがあります。この worktree のブランチを切り替えないかを確かめてから承認してください。"

// gitConfigReadFlags は読み取り専用のオプション。
var gitConfigReadFlags = map[string]bool{
	"--get": true, "--get-all": true, "--get-regexp": true, "--get-urlmatch": true,
	"--list": true, "--show-origin": true, "--show-scope": true, "-l": true,
}

// gitConfigWriteFlags は書き込みを起こすオプション。
var gitConfigWriteFlags = map[string]bool{
	"--global": true, "--local": true, "--system": true, "--worktree": true,
	"--file": true, "--blob": true, "--unset": true, "--unset-all": true,
	"--replace-all": true, "--add": true, "--rename-section": true,
	"--remove-section": true, "--edit": true,
}

// gitConfig は git config の書き込みを deny する。
func gitConfig(call *syntax.CallExpr) *Verdict {
	args := gitSubcommandArgs(call, "config")
	if args == nil {
		return nil
	}
	for _, a := range args {
		if gitConfigReadFlags[a] {
			return nil
		}
	}
	// worktree 例外: remote.origin.fetch への設定は許可する
	if len(args) > 0 && args[0] == "remote.origin.fetch" {
		return nil
	}
	for _, a := range args {
		if gitConfigWriteFlags[a] {
			return Deny("%s", gitConfigReason)
		}
	}
	// フラグなしの形は引数の数で読み書きを分ける。
	// git config <key> は読み取り、git config <key> <value> は書き込み。
	if len(args) >= 2 {
		return Deny("%s", gitConfigReason)
	}
	return nil
}

// gitCommitMain は main / master ブランチへの直接 commit を deny する。
func gitCommitMain(call *syntax.CallExpr, targetDir string) *Verdict {
	args := gitSubcommandArgs(call, "commit")
	if args == nil || hasHelpFlag(args) {
		return nil
	}
	out, err := exec.Command("git", "-C", targetDir, "branch", "--show-current").Output() // #nosec G204 -- targetDir は cd の追跡結果
	if err != nil {
		return nil
	}
	switch branch := strings.TrimSpace(string(out)); branch {
	case "main", "master":
		return Deny("%s ブランチへの直接コミットはフックによりブロックされました。feature ブランチを作成してください。", branch)
	}
	return nil
}

// worktreeBranchGhStackSubs は gh stack のうちブランチ作成・checkout を行うもの。
var worktreeBranchGhStackSubs = map[string]bool{
	"init": true, "add": true, "adopt": true, "up": true, "down": true,
	"top": true, "bottom": true, "trunk": true, "switch": true, "view": true,
}

// gitBranchReadFlags は git branch のうち作成を伴わない形。
var gitBranchReadFlags = map[string]bool{
	"-a": true, "-v": true, "--list": true, "--show-current": true,
	"-d": true, "-D": true, "--delete": true, "--merged": true, "--no-merged": true,
	"--contains": true, "--sort": true, "--format": true, "-r": true, "--remotes": true,
}

// worktreeBranch は worktree 内でのブランチ操作を deny し、gh stack は ask にする。
func worktreeBranch(call *syntax.CallExpr) *Verdict {
	args, sub := branchOperation(call)
	if args == nil || hasHelpFlag(args) {
		return nil
	}
	if sub == "branch" {
		if len(args) == 0 {
			return nil
		}
		for _, a := range args {
			if gitBranchReadFlags[a] {
				return nil
			}
		}
	}
	if !inLinkedWorktree() {
		return nil
	}
	// gh stack は既存ブランチを積むだけの使い方もあり、checkout を伴うかを引数から決められない。
	if sub == "stack" {
		return Ask("%s", worktreeGhStackReason)
	}
	return Deny("%s", worktreeBranchReason)
}

// branchOperation はブランチ操作なら (サブコマンド以降の引数, サブコマンド名) を返す。
func branchOperation(call *syntax.CallExpr) ([]string, string) {
	args := CallArgs(call)
	if len(args) < 2 {
		return nil, ""
	}
	switch args[0] {
	case "git":
		switch args[1] {
		case "checkout", "switch", "branch":
			return args[2:], args[1]
		}
	case "gh":
		if len(args) >= 3 && args[1] == "stack" && worktreeBranchGhStackSubs[args[2]] {
			return args[3:], "stack"
		}
	}
	return nil, ""
}

// inLinkedWorktree はプロセスの cwd が linked worktree の中か。
// 判定は cd の追跡結果ではなく cwd で行う。
func inLinkedWorktree() bool {
	out, err := exec.Command("git", "rev-parse", "--git-dir", "--git-common-dir").Output()
	if err != nil {
		return false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return len(lines) == 2 && lines[0] != lines[1]
}

// gitSubcommandArgs は `git <sub>` の形なら、それ以降の引数を返す。
// git のグローバルオプション (`git -c x=y config ...`) を挟む形は対象外。
func gitSubcommandArgs(call *syntax.CallExpr, sub string) []string {
	args := CallArgs(call)
	if len(args) < 2 || args[0] != "git" || args[1] != sub {
		return nil
	}
	return args[2:]
}

func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}
