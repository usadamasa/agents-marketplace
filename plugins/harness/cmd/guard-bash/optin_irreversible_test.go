package main

import (
	"strings"
	"testing"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/ghauthor"
)

// opt-in を有効にしても、戻しにくい操作は ask に残す。
// リリース PR (tagpr) はマージでタグと GitHub Release が作られる。GitHub App で動かす tagpr の PR は
// 作成者が bot なので「自分が作成した PR」の条件で元から外れるが、PAT で動かすと作成者が自分になる。
func TestRunGHWritesOptInKeepsIrreversibleAsk(t *testing.T) {
	const releaseURL = "pr https://github.com/owner/repo/pull/7"
	t.Cleanup(ghauthor.SetForTest(ghauthor.Stub{
		Self: "octocat",
		Authors: map[string]string{
			"pr 5":     "octocat",
			"pr 7":     "octocat",
			"pr 8":     "app/tagpr-bot",
			"pr 9":     "octocat",
			releaseURL: "octocat",
		},
		Bots: map[string]bool{"pr 8": true},
		Labels: map[string][]string{
			"pr 7":     {"tagpr"},
			"pr 8":     {"tagpr"},
			releaseURL: {"tagpr"},
		},
		LabelsUnknown: map[string]bool{"pr 9": true},
	}))

	tests := []struct {
		name    string
		command string
		// wantPass が true なら「出力なし = 通過」、false なら ask を期待する
		wantPass bool
	}{
		{name: "普通の PR の merge は通過", command: "gh pr merge 5 --squash", wantPass: true},
		{name: "普通の PR の merge --delete-branch は通過", command: "gh pr merge 5 --squash --delete-branch", wantPass: true},

		{name: "自分が作成したリリース PR の merge", command: "gh pr merge 7 --squash"},
		{name: "リリース PR を URL で指した merge", command: "gh pr merge https://github.com/owner/repo/pull/7 --merge"},
		{name: "bot が作成したリリース PR の merge", command: "gh pr merge 8 --merge"},
		{name: "リリース PR のラベルを外す edit", command: "gh pr edit 7 --remove-label tagpr"},
		{name: "ラベルを取れなかった PR の merge", command: "gh pr merge 9 --squash"},
		{name: "普通の PR と並べたリリース PR の merge", command: "gh pr merge 5 --squash && gh pr merge 7 --squash"},

		{name: "未マージのブランチを消す close --delete-branch", command: "gh pr close 5 --delete-branch"},

		// タグを作る操作は opt-in の対象に無い
		{name: "release create", command: "gh release create v1.0.0"},
		{name: "git tag の作成", command: "git tag v1.0.0"},
		{name: "git push --tags", command: "git push --tags"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GH_REPO", "")
			t.Setenv("GH_HOST", "")
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
