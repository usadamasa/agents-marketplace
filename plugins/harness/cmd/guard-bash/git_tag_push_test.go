package main

import (
	"strings"
	"testing"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/rules"
)

// タグ名だけを渡す git push は、コマンドが動く cwd のリポジトリにその名前のタグがあるかで決める。
// リポジトリを照会できない cwd では、タグでないと決められないので ask に落とす。
func TestRunGitPushOfLocalTagAsks(t *testing.T) {
	repo := t.TempDir()
	initRepo(t, repo, map[string]string{"origin": projectRemote})
	gitRun(t, repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "init")
	gitRun(t, repo, "tag", "v1.2.3")
	gitRun(t, repo, "branch", "feat/x")
	plain := t.TempDir()

	tests := []struct {
		name    string
		cwd     string
		command string
		// wantPass が true なら「出力なし = 通過」、false なら ask を期待する
		wantPass bool
	}{
		{name: "タグ名だけの push", cwd: repo, command: "git push origin v1.2.3"},
		{name: "ブランチと並べたタグ名", cwd: repo, command: "git push origin feat/x v1.2.3"},
		{name: "ブランチの push は通過", cwd: repo, command: "git push -u origin feat/x", wantPass: true},
		{name: "ローカルに無い名前の push は通過", cwd: repo, command: "git push origin v9.9.9", wantPass: true},
		{name: "git リポジトリでない cwd", cwd: plain, command: "git push origin feat/x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := runOptIn(t, repo, tc.cwd, tc.command)
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

	// cd の後ろはどのリポジトリで動くか分からないので、cwd のリポジトリを照会して通さない。
	// run 経由では照合外れの deny が先に返るので、checkRules を直接呼ぶ。
	t.Run("cd の後ろの push", func(t *testing.T) {
		t.Setenv("CLAUDE_PROJECT_DIR", repo)
		rules.SetGitWorkdir(repo)
		push := []string{"git", "push", "origin", "feat/x"}
		if v := checkRules(&hookInput{Cwd: repo}, [][]string{push}); v != nil {
			t.Fatalf("前提の通過にならない: %+v", v)
		}
		v := checkRules(&hookInput{Cwd: repo}, [][]string{{"cd", "elsewhere"}, push})
		if v == nil || v.Decision != "ask" {
			t.Errorf("ask を期待したが %+v", v)
		}
	})

	// 前の判定で止めた照会は、次の判定へ持ち越さない。
	t.Run("ask の後の判定でも照会する", func(t *testing.T) {
		if got := runOptIn(t, repo, plain, "git push origin feat/x"); !strings.Contains(got, `"permissionDecision":"ask"`) {
			t.Fatalf("前提の ask にならない: %q", got)
		}
		if got := runOptIn(t, repo, repo, "git push -u origin feat/x"); got != "" {
			t.Errorf("出力なしを期待したが %q", got)
		}
	})

	// 引数に現れないタグの送信、リリース PR の head を含む push、commit を失うブランチの削除も、
	// cwd のリポジトリを照会して決める。repo の状態を変えるので最後に置く。
	t.Run("リポジトリの状態で決まる形", func(t *testing.T) {
		commit := []string{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m"}
		gitRun(t, repo, "update-ref", "refs/remotes/origin/feat/x", "refs/heads/feat/x")
		gitRun(t, repo, "branch", "pushed")
		gitRun(t, repo, "checkout", "-q", "-b", "tagpr-from-v2")
		gitRun(t, repo, append(commit, "release")...)
		gitRun(t, repo, "checkout", "-q", "-b", "lonely", "feat/x")
		gitRun(t, repo, append(commit, "only here")...)
		ask := func(command string) bool {
			return strings.Contains(runOptIn(t, repo, repo, command), `"permissionDecision":"ask"`)
		}
		if ask("git branch -D tagpr-from-v2") {
			t.Error("ブランチの削除が ask になった")
		}
		if ask("git push origin lonely") {
			t.Error("リリース PR の head を含まないブランチの push が ask になった")
		}
		gitRun(t, repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "merge", "-q", "--no-edit", "tagpr-from-v2")
		if !ask("git push origin lonely") {
			t.Error("リリース PR の head を含むブランチの push が ask にならない")
		}
		gitRun(t, repo, "checkout", "-q", "feat/x")
		if ask("git push") {
			t.Error("前提の通過にならない")
		}
		gitRun(t, repo, "config", "push.followTags", "true")
		if !ask("git push") {
			t.Error("push.followTags が true の push が ask にならない")
		}
		if ask("git push --no-follow-tags") {
			t.Error("--no-follow-tags の push が ask になった")
		}
	})
}
