package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/rules"
)

// runOptInSession は、Bash のシェルにだけ載る環境 (CLAUDE_ENV_FILE の中身) を持つセッションで
// command を評価する。envFile が空ならファイルを置かない。
func runOptInSession(t *testing.T, projectDir, sessionID, envFile, command string) string {
	t.Helper()
	writeSettings(t, projectDir, "settings.json",
		`{"env":{"`+ghPRWritesKey+`":"1","`+ghIssueWritesKey+`":"1"}}`)
	t.Setenv(ghPRWritesKey, "1")
	t.Setenv(ghIssueWritesKey, "1")
	t.Setenv("CLAUDE_PROJECT_DIR", projectDir)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if envFile != "" {
		dir := filepath.Join(home, ".claude", "session-env", sessionID)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sessionstart-hook-0.sh"), []byte(envFile), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	in := `{"tool_name":"Bash","session_id":"` + sessionID + `","cwd":"` + projectDir + `","tool_input":{"command":"` + command + `"}}`
	var out strings.Builder
	if err := run(strings.NewReader(in), &out); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
	return out.String()
}

// SessionStart / CwdChanged の hook が CLAUDE_ENV_FILE へ書いた環境変数は Bash のシェルにだけ載り、
// guard のプロセスには渡らない。セッションの env ファイルを読んで、gh と git の対象を変える変数が
// 在れば照会を止める。
func TestRunSessionEnvStopsLookup(t *testing.T) {
	stubAuthorship(t, "octocat", map[string]string{"pr 11": "octocat"})
	const session = "0f0e0d0c-1111-2222-3333-444455556666"
	const direnvExport = `export PATH=$'/nix/store/x/bin:/usr/bin';export FOO=$'bar';`

	tests := []struct {
		name    string
		envFile string
		command string
		// wantPass が true なら「出力なし = 通過」、false なら ask を期待する
		wantPass bool
	}{
		{name: "env ファイルなし", command: "gh pr merge 11 --merge", wantPass: true},
		{name: "無関係の変数だけ", envFile: direnvExport, command: "gh pr merge 11 --merge", wantPass: true},
		{name: "GH_REPO", envFile: direnvExport + `export GH_REPO=$'owner/other-repo';`, command: "gh pr merge 11 --merge"},
		{name: "GH_HOST", envFile: "export GH_HOST=ghe.example.com\n", command: "gh pr merge 11 --merge"},
		{name: "export の無い代入", envFile: "GH_REPO=owner/other-repo\n", command: "gh pr merge 11 --merge"},
		// 値の中に名前が現れるだけでは止めない
		{name: "値の中の名前", envFile: `export NOTE=$'set GH_REPO later';`, command: "gh pr merge 11 --merge", wantPass: true},
		{name: "unset", envFile: "unset GH_REPO;", command: "gh pr merge 11 --merge", wantPass: true},
		{name: "無関係の変数だけの git push", envFile: direnvExport, command: "git push", wantPass: true},
		{name: "GIT_DIR", envFile: "export GIT_DIR=/elsewhere/.git;", command: "git push"},
		{name: "GIT_CONFIG_COUNT", envFile: "export GIT_CONFIG_COUNT=1;", command: "git push"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			initRepo(t, repo, map[string]string{"origin": projectRemote})
			gitRun(t, repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "init")
			got := runOptInSession(t, repo, session, tc.envFile, tc.command)
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

	// session_id がパスを含む形は、env ファイルを読めないので照会を止める。
	t.Run("パスを含む session_id", func(t *testing.T) {
		repo := t.TempDir()
		initRepo(t, repo, map[string]string{"origin": projectRemote})
		if got := runOptInSession(t, repo, "../x", "", "gh pr merge 11 --merge"); !strings.Contains(got, `"permissionDecision":"ask"`) {
			t.Errorf("ask を期待したが %q", got)
		}
	})
}

// コマンドの中で git の設定を環境変数から差し込む形は、hook が読む設定と食い違う。
func TestNoteAssignStopsGitLookup(t *testing.T) {
	repo := t.TempDir()
	initRepo(t, repo, map[string]string{"origin": projectRemote})
	gitRun(t, repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "init")
	t.Setenv("CLAUDE_PROJECT_DIR", repo)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	push := [][]string{{"git", "push"}}

	for _, name := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_PARAMETERS", "GIT_DIR"} {
		t.Run(name, func(t *testing.T) {
			rules.SetGitWorkdir(repo)
			if v := checkRules(&hookInput{Cwd: repo}, push); v != nil {
				t.Fatalf("前提の通過にならない: %+v", v)
			}
			noteAssign(name)
			if v := checkRules(&hookInput{Cwd: repo}, push); v == nil || v.Decision != "ask" {
				t.Errorf("ask を期待したが %+v", v)
			}
		})
	}
	t.Run("無関係の変数", func(t *testing.T) {
		rules.SetGitWorkdir(repo)
		noteAssign("GIT_AUTHOR_NAME")
		if v := checkRules(&hookInput{Cwd: repo}, push); v != nil {
			t.Errorf("通過を期待したが %+v", v)
		}
	})
}
