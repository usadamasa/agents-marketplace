// Package rules は外部 CLI の破壊的操作を判定するルール集。
package rules

import (
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/rules/gh"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/rules/git"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/rules/pup"
)

// all は各 <tool>.go (gh・git・pup は rules/<tool> パッケージ) で宣言した group を評価順に結合したもの。
//
// コマンドを追加するときは <tool>.go を新設して、ここに 1 行足す。
// argv のトークンに tool が現れた group だけが評価対象になる。
// 評価順は group をまたいで allow → deny → ask で、allow に当たったコマンドは
// deny/ask を評価しない (例外の追加は各 group の allow に足す)。
var all = []*argrule.Group{
	gcloudRules,
	kubectlRules,
	databricksRules,
	pup.Rules,
	gh.AttachRules,
	gh.Rules,
	gh.OthersRules,
	git.Rules,
	opRules,
	curlRules,
	antWorkerRules,
	antRules,
	printenvRules,
}

// GHPRWriteOptIn は argv が opt-in で ask をスキップしてよい gh の PR 操作かを返す。
func GHPRWriteOptIn(args []string) bool { return gh.PRWriteOptIn(args) }

// GHIssueWriteOptIn は argv が opt-in で ask をスキップしてよい gh の issue 操作かを返す。
func GHIssueWriteOptIn(args []string) bool { return gh.IssueWriteOptIn(args) }

// SetGHQueryDir は gh api graphql の `query=@<path>` を解決する基準ディレクトリを設定する。
func SetGHQueryDir(dir string) { gh.SetQueryDir(dir) }

// SetGitWorkdir は git push の名前がタグかを照会するディレクトリ (コマンドが動く cwd) を設定する。
// 1 回の判定の始まりに呼ぶ (DisableGitLookup の効果も解除する)。
func SetGitWorkdir(dir string) { git.SetWorkdir(dir) }

// DisableGitLookup は以降の照会を止める。コマンドがどのリポジトリで動くか分からなくなった時点で呼ぶ。
func DisableGitLookup() { git.DisableLookup() }
