package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// projectRemote はテストのプロジェクトリポジトリの origin。
const projectRemote = "https://github.com/owner/repo.git"

// gitRun は dir で git を実行する。端末の git 設定が混ざらないよう global / system の設定は読ませない。
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

// initRepo は dir を git リポジトリにして remotes (名前 → URL) を登録する。
func initRepo(t *testing.T, dir string, remotes map[string]string) {
	t.Helper()
	gitRun(t, dir, "init", "-q")
	for name, url := range remotes {
		gitRun(t, dir, "remote", "add", name, url)
	}
}

// runOptIn は opt-in を有効にしたプロジェクトで command を評価し、出力を返す。
func runOptIn(t *testing.T, projectDir, cwd, command string) string {
	t.Helper()
	writeSettings(t, projectDir, "settings.json",
		`{"env":{"`+ghPRWritesKey+`":"1","`+ghIssueWritesKey+`":"1"}}`)
	t.Setenv(ghPRWritesKey, "1")
	t.Setenv(ghIssueWritesKey, "1")
	t.Setenv("CLAUDE_PROJECT_DIR", projectDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	in := `{"tool_name":"Bash","cwd":"` + cwd + `","tool_input":{"command":"` + command + `"}}`
	var out strings.Builder
	if err := run(strings.NewReader(in), &out); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
	return out.String()
}

// opt-in はプロジェクト (セッションの cwd) のリポジトリの PR / issue にだけ効く。
// 別リポジトリを -R / --repo / URL / GH_REPO で指した形は、作成者が自分でも ask に残す。
func TestRunGHWritesOptInScopedToProjectRepo(t *testing.T) {
	stubAuthorship(t, "octocat", map[string]string{
		"pr 11": "octocat",
		"pr https://github.com/owner/other-repo/pull/11": "octocat",
		"pr https://github.com/owner/repo/pull/11":       "octocat",
		"issue 42": "octocat",
	})

	tests := []struct {
		name    string
		command string
		// env は追加で設定する環境変数
		env map[string]string
		// wantPass が true なら「出力なし = 通過」、false なら ask を期待する
		wantPass bool
	}{
		// 報告された形
		{name: "別リポジトリの pr merge (-R)", command: "gh pr merge 11 -R owner/other-repo --merge --delete-branch"},
		{name: "別リポジトリの pr ready (-R)", command: "gh pr ready 11 -R owner/other-repo"},
		// 文面と assignee の変更は opt-in と無関係に allow で通るので、opt-in に頼る形 (label) で確かめる
		{name: "別リポジトリの pr edit (--repo)", command: "gh pr edit 11 --repo owner/other-repo --add-label bug"},
		{name: "別リポジトリの pr edit (--repo=)", command: "gh pr edit 11 --repo=owner/other-repo --add-label bug"},
		{name: "別リポジトリの pr close (host 付き -R)", command: "gh pr close 11 -R github.com/owner/other-repo"},
		{name: "別リポジトリの pr merge (URL)", command: "gh pr merge https://github.com/owner/other-repo/pull/11 --merge"},
		{name: "別リポジトリの pr merge (大文字の scheme の URL)", command: "gh pr merge HTTPS://github.com/owner/other-repo/pull/11 --merge"},
		{name: "別リポジトリの pr merge (フラグの後ろの URL)", command: "gh pr merge --merge https://github.com/owner/other-repo/pull/11"},
		{
			name:    "別リポジトリの pr merge (環境変数 GH_REPO)",
			command: "gh pr merge 11 --merge",
			env:     map[string]string{"GH_REPO": "owner/other-repo"},
		},
		{name: "別リポジトリの pr merge (GH_REPO の前置き)", command: "GH_REPO=owner/other-repo gh pr merge 11 --merge"},
		{name: "別リポジトリの pr merge (export GH_REPO)", command: "export GH_REPO=owner/other-repo; gh pr merge 11 --merge"},
		{
			name:    "GH_HOST が設定されている",
			command: "gh pr merge 11 --merge",
			env:     map[string]string{"GH_HOST": "ghe.example.com"},
		},
		{name: "別リポジトリの issue create (-R)", command: "gh issue create -R owner/other-repo --title t --body b"},
		{name: "別リポジトリの issue close (-R)", command: "gh issue close 42 -R owner/other-repo"},

		// 従来どおり通る形
		{name: "リポジトリを明示しない pr merge", command: "gh pr merge 11 --merge", wantPass: true},
		{name: "同じリポジトリを -R で明示", command: "gh pr merge 11 -R owner/repo --merge", wantPass: true},
		{name: "同じリポジトリを大文字違いで明示", command: "gh pr merge 11 -R Owner/Repo --merge", wantPass: true},
		{name: "同じリポジトリを host 付きで明示", command: "gh pr ready 11 -R github.com/owner/repo", wantPass: true},
		{name: "同じリポジトリの URL", command: "gh pr merge https://github.com/owner/repo/pull/11 --merge", wantPass: true},
		{
			name:     "GH_REPO が同じリポジトリ",
			command:  "gh pr merge 11 --merge",
			env:      map[string]string{"GH_REPO": "owner/repo"},
			wantPass: true,
		},
		{name: "リポジトリを明示しない issue create", command: "gh issue create --title t --body b", wantPass: true},
		// URL らしい引数は、対象の URL として読めなければ判定を諦める (本文の中の URL も含む)
		{name: "本文に URL を含む issue create", command: `gh issue create --title t --body 'see https://github.com/owner/other-repo/issues/3'`},
		{name: "同じリポジトリの issue close", command: "gh issue close 42 -R owner/repo", wantPass: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GH_REPO", "")
			t.Setenv("GH_HOST", "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			projectDir := t.TempDir()
			initRepo(t, projectDir, map[string]string{"origin": projectRemote})

			got := runOptIn(t, projectDir, projectDir, tc.command)
			if tc.wantPass {
				if got != "" {
					t.Errorf("出力なしを期待したが %q", got)
				}
				return
			}
			if !strings.Contains(got, `"permissionDecision":"ask"`) {
				t.Errorf("ask を期待したが %q", got)
			}
		})
	}
}

// リポジトリを明示しない形は、コマンドが動く cwd のリポジトリが対象になる。
// cwd がプロジェクトと別のリポジトリ (入れ子の clone を含む) なら opt-in を効かせない。
func TestRunGHWritesOptInFollowsWorkdirRepo(t *testing.T) {
	stubAuthorship(t, "octocat", map[string]string{"pr 11": "octocat"})
	t.Setenv("GH_REPO", "")
	t.Setenv("GH_HOST", "")

	t.Run("プロジェクト内に入れ子になった別リポジトリ", func(t *testing.T) {
		projectDir := t.TempDir()
		initRepo(t, projectDir, map[string]string{"origin": projectRemote})
		nested := filepath.Join(projectDir, "nested")
		if err := os.Mkdir(nested, 0o750); err != nil {
			t.Fatal(err)
		}
		initRepo(t, nested, map[string]string{"origin": "https://github.com/owner/other-repo.git"})

		if got := runOptIn(t, projectDir, nested, "gh pr merge 11 --merge"); !strings.Contains(got, `"permissionDecision":"ask"`) {
			t.Errorf("ask を期待したが %q", got)
		}
	})

	t.Run("プロジェクトの外の別リポジトリ", func(t *testing.T) {
		projectDir := t.TempDir()
		initRepo(t, projectDir, map[string]string{"origin": projectRemote})
		other := t.TempDir()
		initRepo(t, other, map[string]string{"origin": "https://github.com/owner/other-repo.git"})

		if got := runOptIn(t, projectDir, other, "gh pr merge 11 --merge"); !strings.Contains(got, `"permissionDecision":"ask"`) {
			t.Errorf("ask を期待したが %q", got)
		}
	})

	// cwd がプロジェクトと同じパスでも、git リポジトリであることを確かめられなければ通さない。
	t.Run("git 管理外のプロジェクト", func(t *testing.T) {
		projectDir := t.TempDir()

		if got := runOptIn(t, projectDir, projectDir, "gh pr merge 11 --merge"); !strings.Contains(got, `"permissionDecision":"ask"`) {
			t.Errorf("ask を期待したが %q", got)
		}
	})

	t.Run("プロジェクトのサブディレクトリ", func(t *testing.T) {
		projectDir := t.TempDir()
		initRepo(t, projectDir, map[string]string{"origin": projectRemote})
		sub := filepath.Join(projectDir, "sub")
		if err := os.Mkdir(sub, 0o750); err != nil {
			t.Fatal(err)
		}

		if got := runOptIn(t, projectDir, sub, "gh pr merge 11 --merge"); got != "" {
			t.Errorf("出力なしを期待したが %q", got)
		}
	})

	t.Run("プロジェクトの worktree", func(t *testing.T) {
		projectDir := t.TempDir()
		initRepo(t, projectDir, map[string]string{"origin": projectRemote})
		gitRun(t, projectDir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "init")
		wt := filepath.Join(t.TempDir(), "wt")
		gitRun(t, projectDir, "worktree", "add", "-q", wt)

		if got := runOptIn(t, projectDir, wt, "gh pr merge 11 --merge"); got != "" {
			t.Errorf("出力なしを期待したが %q", got)
		}
	})
}

// -R で明示した形は、プロジェクトの remote から対象を決められないときは opt-in を効かせない。
// gh と同じく、gh repo set-default が付ける gh-resolved の remote を優先する。
func TestRunGHWritesOptInProjectRemoteResolution(t *testing.T) {
	stubAuthorship(t, "octocat", map[string]string{"pr 11": "octocat"})
	t.Setenv("GH_REPO", "")
	t.Setenv("GH_HOST", "")

	const fork = "git@github.com:owner/repo.git"
	const upstream = "https://github.com/upstream-org/workspace.git"

	tests := []struct {
		name     string
		remotes  map[string]string
		resolved string // gh-resolved = base を付ける remote 名
		command  string
		wantPass bool
	}{
		{name: "ssh の remote でも一致すれば通す", remotes: map[string]string{"origin": fork}, command: "gh pr merge 11 -R owner/repo", wantPass: true},
		{name: "remote が複数で既定が無ければ ask", remotes: map[string]string{"origin": fork, "upstream": upstream}, command: "gh pr merge 11 -R owner/repo"},
		{name: "既定の remote と一致すれば通す", remotes: map[string]string{"origin": fork, "upstream": upstream}, resolved: "upstream", command: "gh pr merge 11 -R upstream-org/workspace", wantPass: true},
		{name: "既定でない remote を指せば ask", remotes: map[string]string{"origin": fork, "upstream": upstream}, resolved: "upstream", command: "gh pr merge 11 -R owner/repo"},
		{name: "remote が無ければ ask", remotes: map[string]string{}, command: "gh pr merge 11 -R owner/repo"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			initRepo(t, projectDir, tc.remotes)
			if tc.resolved != "" {
				gitRun(t, projectDir, "config", "remote."+tc.resolved+".gh-resolved", "base")
			}

			got := runOptIn(t, projectDir, projectDir, tc.command)
			if tc.wantPass {
				if got != "" {
					t.Errorf("出力なしを期待したが %q", got)
				}
				return
			}
			if !strings.Contains(got, `"permissionDecision":"ask"`) {
				t.Errorf("ask を期待したが %q", got)
			}
		})
	}
}
