package git

import (
	"os/exec"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// 実際の git で、タグが在る・無い・決められないの 3 つを区別する。
func TestLocalTag(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	gitIn(t, repo, "tag", "v1.2.3")
	gitIn(t, repo, "tag", "release/v2")
	gitIn(t, repo, "tag", "dual")
	gitIn(t, repo, "branch", "dual")
	gitIn(t, repo, "branch", "feat/x")

	tests := []struct {
		name string
		dir  string
		tag  string
		want tagState
	}{
		{"タグ", repo, "v1.2.3", tagPresent},
		{"スラッシュを含むタグ", repo, "release/v2", tagPresent},
		{"同名のブランチがあるタグ", repo, "dual", tagPresent},
		{"ブランチ", repo, "feat/x", tagAbsent},
		{"どの ref でもない名前", repo, "nothing", tagAbsent},
		// 前方一致で別のタグ (release/v2) に当たっても、その名前のタグではない
		{"タグ名の前方一致", repo, "release", tagAbsent},
		{"glob", repo, "v1.*", tagAbsent},
		{"git リポジトリでないディレクトリ", t.TempDir(), "v1.2.3", tagUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := localTag(tc.dir, tc.tag); got != tc.want {
				t.Errorf("localTag(%q) = %v, want %v", tc.tag, got, tc.want)
			}
		})
	}
}

// 実行位置が分からないとき、照会を止めた後は、在るタグでも決めない。
func TestTagStateOfUnknown(t *testing.T) {
	t.Cleanup(SetTagsForTest("v1.2.3"))
	if got := tagStateOf("v1.2.3"); got != tagPresent {
		t.Fatalf("stub の前提が崩れている: %v", got)
	}
	DisableLookup()
	if got := tagStateOf("v1.2.3"); got != tagUnknown {
		t.Errorf("DisableLookup の後 = %v, want tagUnknown", got)
	}
	SetWorkdir("stub")
	if got := tagStateOf("v1.2.3"); got != tagPresent {
		t.Errorf("SetWorkdir で照会が再開しなかった: %v", got)
	}
	SetWorkdir("")
	if got := tagStateOf("v1.2.3"); got != tagUnknown {
		t.Errorf("workdir が空 = %v, want tagUnknown", got)
	}
}
