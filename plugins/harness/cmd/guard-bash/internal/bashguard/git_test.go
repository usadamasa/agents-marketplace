package bashguard

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func TestGitConfig(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		deny bool
	}{
		{name: "git config 以外", cmd: "git status"},
		{name: "git 以外", cmd: "ls -la"},
		{name: "--get", cmd: "git config --get user.name"},
		{name: "--get-all", cmd: "git config --get-all remote.origin.url"},
		{name: "--get-regexp", cmd: `git config --get-regexp 'remote.*'`},
		{name: "--get-urlmatch", cmd: "git config --get-urlmatch http https://example.com"},
		{name: "--list", cmd: "git config --list"},
		{name: "-l", cmd: "git config -l"},
		{name: "--show-origin", cmd: "git config --show-origin user.name"},
		{name: "--show-scope", cmd: "git config --show-scope user.name"},
		{name: "キーだけなら読み取り", cmd: "git config user.name"},
		{name: "worktree 例外 (remote.origin.fetch への書き込み)", cmd: `git config remote.origin.fetch "+refs/heads/*:refs/remotes/origin/*"`},
		{name: "パイプの中の読み取り", cmd: "git config --get remote.origin.fetch | grep refs"},
		{name: "インライン config (git -c) は書き込みではない", cmd: `git -c sequence.editor="sed -i '' '2,$s/^pick/fixup/'" rebase -i abc123`},
		{name: "chain 後方の読み取り", cmd: "git status && git config --get user.name"},

		{name: "キーと値", cmd: `git config user.name "foo"`, deny: true},
		{name: "--global", cmd: "git config --global core.editor vim", deny: true},
		{name: "--unset", cmd: "git config --unset foo", deny: true},
		{name: "--unset-all", cmd: "git config --unset-all foo", deny: true},
		{name: "--replace-all", cmd: "git config --replace-all foo bar", deny: true},
		{name: "--add", cmd: "git config --add foo bar", deny: true},
		{name: "--local", cmd: `git config --local user.email "test@test.com"`, deny: true},
		{name: "chain 前方の書き込み", cmd: `git config user.name "foo" && echo done`, deny: true},
		{name: "chain 後方の書き込み", cmd: `git status && git config --global user.email "evil@x"`, deny: true},
		{name: "; の後ろの書き込み", cmd: `echo hi ; git config user.name "foo"`, deny: true},
		{name: "|| の後ろの書き込み", cmd: "true || git config --unset foo", deny: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertVerdict(t, evalCalls(t, tt.cmd, gitConfig), tt.deny)
		})
	}
}

func TestGitCommitMain(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		cmd    string
		deny   bool
	}{
		{name: "feature ブランチ", branch: "feature-x", cmd: `git commit -m "fix"`},
		{name: "main でも commit 以外", branch: "main", cmd: "git status"},
		{name: "main でも help", branch: "main", cmd: "git commit --help"},
		{name: "main", branch: "main", cmd: `git commit -m "fix"`, deny: true},
		{name: "master", branch: "master", cmd: `git commit -m "fix"`, deny: true},
		{name: "chain 後方の commit", branch: "main", cmd: `git add -A && git commit -m "fix"`, deny: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := initRepo(t, tt.branch)
			assertVerdict(t, evalCalls(t, tt.cmd, func(c *syntax.CallExpr) *Verdict {
				return gitCommitMain(c, dir)
			}), tt.deny)
		})
	}
}

// chain の途中の cd は、以降のコマンドが動くリポジトリを変える。
// ネストしたクローンでは cwd だけで判定すると誤検知と見逃しの両方が起きる。
func TestPerCommandFollowsCd(t *testing.T) {
	outer := initRepo(t, "feature-x")
	inner := filepath.Join(outer, "nested")
	mustRun(t, outer, "mkdir", "nested")
	mustRun(t, inner, "git", "init", "-b", "main")

	got := PerCommand(parseForTest(t, `cd nested && git commit -m "fix"`), outer)
	if got == nil {
		t.Fatal("cd 先の main リポジトリでの commit を deny すべき")
	}
	if !strings.Contains(got.Reason, "main") {
		t.Errorf("deny の理由が main ブランチを指していない: %s", got.Reason)
	}
}

// subshell の中の cd は抜けた後のコマンドには効かない。追跡してしまうと
// 括弧の外の commit を別リポジトリで判定し、cwd 側のブランチを見落とす。
func TestPerCommandIgnoresCdInsideSubshell(t *testing.T) {
	outer := initRepo(t, "main")
	inner := filepath.Join(outer, "nested")
	mustRun(t, outer, "mkdir", "nested")
	mustRun(t, inner, "git", "init", "-b", "feature-x")

	for _, cmd := range []string{
		`(cd nested && true) && git commit -m "fix"`,
		`x=$(cd nested && pwd); git commit -m "fix"`,
	} {
		t.Run(cmd, func(t *testing.T) {
			got := PerCommand(parseForTest(t, cmd), outer)
			if got == nil {
				t.Fatal("cwd 側の main ブランチへの commit を deny すべき")
			}
		})
	}
}

func TestWorktreeBranch(t *testing.T) {
	tests := []struct {
		name   string
		cmd    string
		inTree string // linked worktree の中での判定 (空なら通過)
	}{
		{name: "git status", cmd: "git status"},
		{name: "git branch (引数なし)", cmd: "git branch"},
		{name: "git branch -a", cmd: "git branch -a"},
		{name: "git branch --show-current", cmd: "git branch --show-current"},
		{name: "git branch -d", cmd: "git branch -d feature-xxx"},
		{name: "git checkout --help", cmd: "git checkout --help"},
		{name: "gh stack submit (ブランチ移動を伴わない)", cmd: "gh stack submit"},
		{name: "gh stack add --help", cmd: "gh stack add --help"},

		{name: "git checkout -b", cmd: "git checkout -b feature-xxx", inTree: "deny"},
		{name: "git checkout existing", cmd: "git checkout feature-existing", inTree: "deny"},
		{name: "git switch -c", cmd: "git switch -c feature-xxx", inTree: "deny"},
		{name: "git branch feature-xxx", cmd: "git branch feature-xxx", inTree: "deny"},
		{name: "chain 前方", cmd: "git checkout -b feature-xxx && echo done", inTree: "deny"},
		{name: "chain 後方", cmd: "git status && git checkout -b feature-xxx", inTree: "deny"},
		{name: "; の後ろ", cmd: "echo hi ; git switch -c feature-xxx", inTree: "deny"},
		{name: "|| の後ろ", cmd: "true || git branch new-branch", inTree: "deny"},
		// gh stack は既存ブランチの登録にも使うので、止めずに人へ判断を渡す。
		{name: "gh stack init", cmd: "gh stack init demo/step-1", inTree: "ask"},
		{name: "gh stack up", cmd: "gh stack up", inTree: "ask"},
		{name: "gh stack switch", cmd: "gh stack switch", inTree: "ask"},
	}

	main, linked := initRepoWithWorktree(t)
	for _, tt := range tests {
		t.Run(tt.name+"/通常リポジトリ", func(t *testing.T) {
			t.Chdir(main)
			assertDecision(t, evalCalls(t, tt.cmd, worktreeBranch), "")
		})
		t.Run(tt.name+"/linked worktree", func(t *testing.T) {
			t.Chdir(linked)
			assertDecision(t, evalCalls(t, tt.cmd, worktreeBranch), tt.inTree)
		})
	}
}

// 前の簡単コマンドの ask で打ち切ると、後ろの deny を見落とす。
func TestPerCommandPrefersDenyOverEarlierAsk(t *testing.T) {
	_, linked := initRepoWithWorktree(t)
	t.Chdir(linked)
	got := PerCommand(parseForTest(t, `gh stack up && git config user.name "foo"`), linked)
	assertDecision(t, got, "deny")
}

// bare リポジトリでは main worktree も linked worktree として扱われる。
func TestWorktreeBranchBareRepo(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "repo.git")
	mustRun(t, root, "git", "init", "--bare", "-b", "main", "repo.git")
	work := filepath.Join(root, "main")
	mustRun(t, bare, "git", "worktree", "add", "-b", "work", work)

	t.Chdir(work)
	assertVerdict(t, evalCalls(t, "git checkout -b feature-xxx", worktreeBranch), true)
	assertVerdict(t, evalCalls(t, "git branch -d feature-xxx", worktreeBranch), false)
}

// initRepo は指定ブランチで空のリポジトリを作り、そのパスを返す。
func initRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-b", branch)
	return dir
}

// initRepoWithWorktree は main リポジトリと linked worktree の組を作る。
func initRepoWithWorktree(t *testing.T) (mainDir, linkedDir string) {
	t.Helper()
	root := t.TempDir()
	mainDir = filepath.Join(root, "main")
	mustRun(t, root, "git", "init", "-b", "main", "main")
	mustRun(t, mainDir, "git", "commit", "--allow-empty", "-m", "init")
	linkedDir = filepath.Join(root, "wt")
	mustRun(t, mainDir, "git", "worktree", "add", "-b", "wt", linkedDir)
	return mainDir, linkedDir
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
