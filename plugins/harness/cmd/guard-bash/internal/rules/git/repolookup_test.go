package git

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// gitTry は失敗しうる git を dir で実行し、出力と成否を返す。
func gitTry(t *testing.T, dir string, args ...string) (string, bool) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// newRepo は commit を 1 つ持つリポジトリを作る。
func newRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	return repo
}

func TestLocalPushConfig(t *testing.T) {
	t.Run("設定なし", func(t *testing.T) {
		got, ok := localPushConfig(newRepo(t))
		if !ok || len(got) != 0 {
			t.Errorf("localPushConfig = %q, %v, want 空, true", got, ok)
		}
	})
	t.Run("push に関わる設定だけを返す", func(t *testing.T) {
		repo := newRepo(t)
		gitIn(t, repo, "config", "push.followTags", "true")
		gitIn(t, repo, "config", "remote.origin.url", "https://example.com/x.git")
		gitIn(t, repo, "config", "remote.origin.push", "refs/tags/*:refs/tags/*")
		gitIn(t, repo, "config", "remote.backup.mirror", "true")
		gitIn(t, repo, "config", "push.default", "current")
		gitIn(t, repo, "config", "push.autoSetupRemote", "true")
		got, ok := localPushConfig(repo)
		want := []string{
			"push.followtags true", "push.default current",
			"remote.origin.push refs/tags/*:refs/tags/*", "remote.backup.mirror true",
		}
		slices.Sort(got)
		slices.Sort(want)
		if !ok || !slices.Equal(got, want) {
			t.Errorf("localPushConfig = %q, %v, want %q, true", got, ok, want)
		}
	})
	t.Run("git リポジトリでないディレクトリ", func(t *testing.T) {
		// リポジトリの外では local の設定が読めず、どの remote の設定があるかを決められない。
		if got, ok := localPushConfig(t.TempDir()); ok {
			t.Errorf("localPushConfig = %q, true, want false", got)
		}
	})
}

func TestMergedRefs(t *testing.T) {
	repo := newRepo(t)
	gitIn(t, repo, "branch", "tagpr-from-v1")
	gitIn(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "second")
	gitIn(t, repo, "branch", "ahead")
	gitIn(t, repo, "update-ref", "refs/remotes/origin/tagpr-from-v2", "HEAD")
	gitIn(t, repo, "tag", "v1")

	tests := []struct {
		name string
		src  string
		want []string
		ok   bool
	}{
		{"先端のブランチ", "ahead", []string{
			"refs/heads/ahead", "refs/heads/main", "refs/heads/tagpr-from-v1",
			"refs/remotes/origin/main", "refs/remotes/origin/tagpr-from-v2",
		}, true},
		{"手前のブランチ", "tagpr-from-v1", []string{"refs/heads/tagpr-from-v1", "refs/remotes/origin/main"}, true},
		{"HEAD", "HEAD", []string{
			"refs/heads/ahead", "refs/heads/main", "refs/heads/tagpr-from-v1",
			"refs/remotes/origin/main", "refs/remotes/origin/tagpr-from-v2",
		}, true},
		// src を絞らないときは、全てのブランチと remote 追跡 ref (タグは含めない)
		{"絞らない", "", []string{
			"refs/heads/ahead", "refs/heads/main", "refs/heads/tagpr-from-v1",
			"refs/remotes/origin/main", "refs/remotes/origin/tagpr-from-v2",
		}, true},
		// リポジトリに無い名前からは何も辿れない (照会の失敗とは分ける)
		{"無い名前", "nothing", nil, true},
		// フラグに見える名前を git のオプションとして渡さない (全ての ref が返らない)
		{"フラグに見える名前", "--all", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := mergedRefs(repo, tc.src)
			slices.Sort(got)
			if ok != tc.ok || !slices.Equal(got, tc.want) {
				t.Errorf("mergedRefs(%q) = %q, %v, want %q, %v", tc.src, got, ok, tc.want, tc.ok)
			}
		})
	}
	t.Run("git リポジトリでないディレクトリ", func(t *testing.T) {
		if got, ok := mergedRefs(t.TempDir(), "HEAD"); ok {
			t.Errorf("mergedRefs = %q, true, want false", got)
		}
	})
}

func TestContainingRefs(t *testing.T) {
	repo := newRepo(t)
	gitIn(t, repo, "branch", "merged")
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "second")
	gitIn(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitIn(t, repo, "branch", "pushed")
	gitIn(t, repo, "tag", "v1")
	gitIn(t, repo, "checkout", "-q", "-b", "lonely")
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "third")

	tests := []struct {
		name string
		ref  string
		want []string
		ok   bool
	}{
		{"ほかの ref から辿れない", "refs/heads/lonely", []string{"refs/heads/lonely"}, true},
		{"ブランチ・remote 追跡 ref・タグから辿れる", "refs/heads/pushed", []string{
			"refs/heads/lonely", "refs/heads/main", "refs/heads/pushed", "refs/remotes/origin/main", "refs/tags/v1",
		}, true},
		{"無い ref", "refs/heads/nothing", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := containingRefs(repo, tc.ref)
			slices.Sort(got)
			if ok != tc.ok || !slices.Equal(got, tc.want) {
				t.Errorf("containingRefs(%q) = %q, %v, want %q, %v", tc.ref, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// remote にだけ在るタグを照会しない判定が頼る、git の解決規則を固定する。
// git がこの規則を変えたら、強制でない push でも remote のタグに届くようになる。
func TestGitPushResolvesNamesLocally(t *testing.T) {
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare", "-b", "main")
	repo := newRepo(t)
	gitIn(t, repo, "remote", "add", "origin", remote)
	gitIn(t, repo, "push", "-q", "origin", "main")
	// remote にだけ在るタグ v1 を作る。
	gitIn(t, repo, "tag", "v1")
	gitIn(t, repo, "push", "-q", "origin", "refs/tags/v1")
	gitIn(t, repo, "tag", "-d", "v1")
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "second")
	tagBefore, _ := gitTry(t, remote, "rev-parse", "refs/tags/v1")

	t.Run("強制でない push は remote のタグを書き換えない", func(t *testing.T) {
		if out, ok := gitTry(t, repo, "push", "origin", "HEAD:v1"); ok {
			t.Fatalf("push が通った: %s", out)
		}
		if after, _ := gitTry(t, remote, "rev-parse", "refs/tags/v1"); after != tagBefore {
			t.Errorf("remote のタグが動いた: %q → %q", tagBefore, after)
		}
	})
	t.Run("名前だけの refspec は src と同じ種類の ref へ送る", func(t *testing.T) {
		gitIn(t, repo, "branch", "v1")
		gitIn(t, repo, "push", "-q", "origin", "v1")
		if _, ok := gitTry(t, remote, "rev-parse", "--verify", "-q", "refs/heads/v1"); !ok {
			t.Error("remote にブランチ v1 が出来ていない")
		}
		if after, _ := gitTry(t, remote, "rev-parse", "refs/tags/v1"); after != tagBefore {
			t.Errorf("remote のタグが動いた: %q → %q", tagBefore, after)
		}
	})
	t.Run("remote に無い修飾なしの dst はブランチになる", func(t *testing.T) {
		gitIn(t, repo, "push", "-q", "origin", "HEAD:fresh")
		out, _ := gitTry(t, remote, "for-each-ref", "--format=%(refname)")
		if !strings.Contains(out, "refs/heads/fresh\n") || strings.Contains(out, "refs/tags/fresh") {
			t.Errorf("fresh がブランチになっていない:\n%s", out)
		}
	})
}
