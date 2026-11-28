package ghauthor

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestParseRemoteURL(t *testing.T) {
	want := repository{Host: "github.com", Owner: "o", Name: "r"}
	cases := []struct {
		url  string
		ok   bool
		want repository
	}{
		{"https://github.com/o/r.git", true, want},
		{"https://github.com/o/r", true, want},
		{"https://user@github.com/o/r.git", true, want},
		{"ssh://git@github.com/o/r.git", true, want},
		{"ssh://git@github.com:22/o/r.git", true, want},
		{"git@github.com:o/r.git", true, want},
		{"git@ghe.example.com:o/r.git", true, repository{Host: "ghe.example.com", Owner: "o", Name: "r"}},
		{"/local/path/r.git", false, repository{}},
		{"file:///local/o/r.git", false, repository{}},
		{"https://github.com/o/r/extra", false, repository{}},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			got, ok := parseRemoteURL(tc.url)
			if ok != tc.ok || got != tc.want {
				t.Errorf("parseRemoteURL(%q) = %v, %v; want %v, %v", tc.url, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// explicitRepos はフラグが値を取るかに依らず、明示の形を字面で拾う。
// 取りこぼすと cwd で判定され、別リポジトリに opt-in が効いてしまう。
func TestExplicitRepos(t *testing.T) {
	gh := func(s string) repository { r, _ := parseFullName(s); return r }
	cases := []struct {
		cmd  string
		ok   bool
		want []repository
	}{
		{"gh pr merge 1 --merge", true, nil},
		{"gh pr merge 1 -R o/x", true, []repository{gh("o/x")}},
		{"gh pr merge 1 --repo o/x", true, []repository{gh("o/x")}},
		{"gh pr merge 1 --repo=o/x", true, []repository{gh("o/x")}},
		{"gh pr merge 1 -Ro/x", true, []repository{gh("o/x")}},
		{"gh pr merge 1 -R ghe.example.com/o/x", true, []repository{{Host: "ghe.example.com", Owner: "o", Name: "x"}}},
		{"gh pr merge --squash https://github.com/o/x/pull/1", true, []repository{gh("o/x")}},
		{"gh issue close https://github.com/o/x/issues/3", true, []repository{gh("o/x")}},
		// gh が受け付けても読めない URL は判定を諦める
		{"gh pr merge https://github.com/o/x/pull/1/files", false, nil},
		{"gh pr merge https://github.com/o/x/pull/1?x=y", false, nil},
		{"gh pr edit 1 --body https://example.com", false, nil},
		{"gh pr edit 1 --body=see:https://example.com", false, nil},
		// scheme の大文字小文字は gh と同じく区別しない
		{"gh pr merge HTTPS://github.com/o/x/pull/1", true, []repository{gh("o/x")}},
		{"gh pr merge Https://github.com/o/x/pull/1", true, []repository{gh("o/x")}},
		{"gh pr merge ftp://github.com/o/x/pull/1", false, nil},
		// 値を読めない明示は判定を諦める
		{"gh pr merge 1 -R", false, nil},
		{"gh pr merge 1 -R ../x", false, nil},
		{"gh pr merge 1 -dR o/x", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			got, ok := explicitRepos(strings.Fields(tc.cmd))
			if ok != tc.ok || !slices.Equal(got, tc.want) {
				t.Errorf("explicitRepos(%q) = %v, %v; want %v, %v", tc.cmd, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// 判定の材料が無いときは opt-in を効かせない。
func TestInProjectFailClosed(t *testing.T) {
	t.Setenv("GH_REPO", "")
	t.Setenv("GH_HOST", "")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	args := strings.Fields("gh pr merge 1")
	t.Cleanup(func() { SetProjectDir(""); SetWorkdir("") })

	// git 管理外は、cwd とプロジェクトが同じパスでも通さない
	plain := t.TempDir()
	SetProjectDir(plain)
	SetWorkdir(plain)
	if InProject(args) {
		t.Error("git 管理外のプロジェクトで true")
	}

	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	SetProjectDir("")
	SetWorkdir(dir)
	if InProject(args) {
		t.Error("プロジェクトが無いのに true")
	}
	SetProjectDir(dir)
	if !InProject(args) {
		t.Error("cwd = プロジェクトで false")
	}
	DisableLookup()
	if InProject(args) {
		t.Error("DisableLookup の後に true")
	}
	SetWorkdir(dir)
	t.Setenv("GH_REPO", "not a repo")
	if InProject(args) {
		t.Error("読めない GH_REPO で true")
	}
}
