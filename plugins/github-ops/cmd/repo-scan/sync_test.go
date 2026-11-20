package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateGit は本番コード側の git 呼び出しにも効くよう、利用者の git 設定と ID をプロセスの環境で切り離す。
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
}

// fixture は origin (bare)、origin を進める seed、同期対象の work を持つ。
type fixture struct {
	origin, seed, work string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	isolateGit(t)
	f := fixture{origin: t.TempDir(), seed: t.TempDir(), work: t.TempDir()}
	gitT(t, f.origin, "init", "--bare", "-b", "main")
	gitT(t, f.seed, "clone", f.origin, ".")
	writeCommit(t, f.seed, "f.txt", "a\n", "init")
	gitT(t, f.seed, "push", "origin", "main")
	gitT(t, f.work, "clone", f.origin, ".")
	return f
}

// advanceOrigin は seed で 1 コミット作って origin の main へ push する。
func (f fixture) advanceOrigin(t *testing.T, name, content string) string {
	t.Helper()
	writeCommit(t, f.seed, name, content, "advance "+name)
	gitT(t, f.seed, "push", "origin", "main")
	return rev(t, f.seed, "HEAD")
}

func rev(t *testing.T, dir, ref string) string {
	t.Helper()
	return strings.TrimSpace(gitT(t, dir, "rev-parse", ref))
}

func stashList(t *testing.T, dir string) []string {
	t.Helper()
	out := strings.TrimSpace(gitT(t, dir, "stash", "list", "--format=%H"))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// addWorktree は repo に branch を新しく切った linked worktree を作り、symlink を解決したパスを返す。
func addWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), "wt")
	gitT(t, repo, "worktree", "add", "-b", branch, wt)
	resolved, err := filepath.EvalSymlinks(wt)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findBranchSync(t *testing.T, r RepoSync, name string) BranchSync {
	t.Helper()
	for _, b := range r.Branches {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("branch %s not in result: %+v", name, r.Branches)
	return BranchSync{}
}

func assertNoOperationInProgress(t *testing.T, dir string) {
	t.Helper()
	for _, p := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD"} {
		gp := strings.TrimSpace(gitT(t, dir, "rev-parse", "--path-format=absolute", "--git-path", p))
		if _, err := os.Stat(gp); err == nil {
			t.Errorf("%s remains in %s", p, dir)
		}
	}
}

func TestSync_FastForwardsMainCheckedOutInMainWorktree(t *testing.T) {
	f := newFixture(t)
	old := rev(t, f.work, "main")
	newSHA := f.advanceOrigin(t, "g.txt", "b\n")

	r := syncRepo(f.work)

	if r.Error != "" {
		t.Fatalf("error = %q", r.Error)
	}
	if r.Main.Status != statusFastForwarded {
		t.Fatalf("main status = %q (%s), want %q", r.Main.Status, r.Main.Reason, statusFastForwarded)
	}
	if r.Main.Old != old || r.Main.New != newSHA {
		t.Errorf("old/new = %s/%s, want %s/%s", r.Main.Old, r.Main.New, old, newSHA)
	}
	if got := rev(t, f.work, "main"); got != newSHA {
		t.Errorf("local main = %s, want %s", got, newSHA)
	}
	if got := readFile(t, filepath.Join(f.work, "g.txt")); got != "b\n" {
		t.Errorf("worktree was not updated: g.txt = %q", got)
	}
}

func TestSync_MainUpToDate(t *testing.T) {
	f := newFixture(t)

	r := syncRepo(f.work)

	if r.Main.Status != statusUpToDate {
		t.Errorf("main status = %q (%s), want %q", r.Main.Status, r.Main.Reason, statusUpToDate)
	}
}

func TestSync_SkipsDivergedMain(t *testing.T) {
	f := newFixture(t)
	writeCommit(t, f.work, "local.txt", "x\n", "local only")
	local := rev(t, f.work, "main")
	f.advanceOrigin(t, "g.txt", "b\n")

	r := syncRepo(f.work)

	if r.Main.Status != statusSkipped || r.Main.Reason != reasonDiverged {
		t.Errorf("main = %q/%q, want %q/%q", r.Main.Status, r.Main.Reason, statusSkipped, reasonDiverged)
	}
	if got := rev(t, f.work, "main"); got != local {
		t.Errorf("local main moved: %s, want %s", got, local)
	}
}

func TestSync_FastForwardsMainCheckedOutNowhere(t *testing.T) {
	f := newFixture(t)
	gitT(t, f.work, "switch", "-c", "feat/here")
	newSHA := f.advanceOrigin(t, "g.txt", "b\n")

	r := syncRepo(f.work)

	if r.Main.Status != statusFastForwarded {
		t.Fatalf("main status = %q (%s), want %q", r.Main.Status, r.Main.Reason, statusFastForwarded)
	}
	if got := rev(t, f.work, "refs/heads/main"); got != newSHA {
		t.Errorf("local main = %s, want %s", got, newSHA)
	}
	// main の FF が、本体 worktree に出ている別ブランチへ merge されていない
	b := findBranchSync(t, r, "feat/here")
	if b.Status != statusRebased {
		t.Errorf("feat/here status = %q (%s), want %q", b.Status, b.Reason, statusRebased)
	}
	if got := strings.TrimSpace(gitT(t, f.work, "rev-list", "--count", "main..feat/here")); got != "0" {
		t.Errorf("feat/here has %s commits not in main, want 0 (feat/here had no own commits)", got)
	}
}

func TestSync_FastForwardsMainCheckedOutInLinkedWorktree(t *testing.T) {
	f := newFixture(t)
	gitT(t, f.work, "switch", "-c", "feat/here")
	wt := filepath.Join(t.TempDir(), "main-wt")
	gitT(t, f.work, "worktree", "add", wt, "main")
	newSHA := f.advanceOrigin(t, "g.txt", "b\n")

	r := syncRepo(f.work)

	if r.Main.Status != statusFastForwarded {
		t.Fatalf("main status = %q (%s), want %q", r.Main.Status, r.Main.Reason, statusFastForwarded)
	}
	if got := rev(t, wt, "HEAD"); got != newSHA {
		t.Errorf("main worktree HEAD = %s, want %s", got, newSHA)
	}
	if got := readFile(t, filepath.Join(wt, "g.txt")); got != "b\n" {
		t.Errorf("linked worktree was not updated: g.txt = %q", got)
	}
	// FF は main の worktree で行い、本体 worktree に出ている feat/here へ merge しない
	if got := strings.TrimSpace(gitT(t, f.work, "symbolic-ref", "--short", "HEAD")); got != "feat/here" {
		t.Errorf("main worktree HEAD = %q, want feat/here", got)
	}
	if got := strings.TrimSpace(gitT(t, f.work, "rev-list", "--merges", "--count", "feat/here")); got != "0" {
		t.Errorf("feat/here has %s merge commits, want 0", got)
	}
}

func TestSync_RebasesWorktreeBranch(t *testing.T) {
	f := newFixture(t)
	wt := addWorktree(t, f.work, "feat/a")
	writeCommit(t, wt, "a.txt", "a\n", "feat a")
	newSHA := f.advanceOrigin(t, "g.txt", "b\n")

	r := syncRepo(f.work)

	b := findBranchSync(t, r, "feat/a")
	if b.Status != statusRebased {
		t.Fatalf("feat/a = %q (%s), want %q", b.Status, b.Reason, statusRebased)
	}
	if b.Worktree != wt {
		t.Errorf("worktree = %q, want %q", b.Worktree, wt)
	}
	gitT(t, f.work, "merge-base", "--is-ancestor", newSHA, "feat/a")
	if got := readFile(t, filepath.Join(wt, "g.txt")); got != "b\n" {
		t.Errorf("g.txt in worktree = %q", got)
	}
}

func TestSync_BranchAlreadyContainingMainIsUpToDate(t *testing.T) {
	f := newFixture(t)
	wt := addWorktree(t, f.work, "feat/a")
	writeCommit(t, wt, "a.txt", "a\n", "feat a")
	before := rev(t, wt, "HEAD")

	r := syncRepo(f.work)

	b := findBranchSync(t, r, "feat/a")
	if b.Status != statusUpToDate {
		t.Errorf("feat/a = %q (%s), want %q", b.Status, b.Reason, statusUpToDate)
	}
	if got := rev(t, wt, "HEAD"); got != before {
		t.Errorf("HEAD moved: %s, want %s", got, before)
	}
}

func TestSync_RebasesDirtyWorktreeAndRestoresChanges(t *testing.T) {
	f := newFixture(t)
	// 別の作業が積んだ stash。sync の後もそのまま残っていなければならない。
	writeFile(t, filepath.Join(f.work, "f.txt"), "someone else\n")
	gitT(t, f.work, "stash", "push", "-m", "someone else")
	foreign := stashList(t, f.work)

	wt := addWorktree(t, f.work, "feat/a")
	writeCommit(t, wt, "a.txt", "a\n", "feat a")
	writeFile(t, filepath.Join(wt, "a.txt"), "a modified\n")
	writeFile(t, filepath.Join(wt, "untracked.txt"), "u\n")
	newSHA := f.advanceOrigin(t, "g.txt", "b\n")

	r := syncRepo(f.work)

	b := findBranchSync(t, r, "feat/a")
	if b.Status != statusRebased {
		t.Fatalf("feat/a = %q (%s), want %q", b.Status, b.Reason, statusRebased)
	}
	gitT(t, f.work, "merge-base", "--is-ancestor", newSHA, "feat/a")
	if got := readFile(t, filepath.Join(wt, "a.txt")); got != "a modified\n" {
		t.Errorf("tracked change lost: a.txt = %q", got)
	}
	if got := readFile(t, filepath.Join(wt, "untracked.txt")); got != "u\n" {
		t.Errorf("untracked file lost: %q", got)
	}
	if b.Stash != "" {
		t.Errorf("stash = %q, want empty after a clean restore", b.Stash)
	}
	got := stashList(t, f.work)
	if len(got) != 1 || got[0] != foreign[0] {
		t.Errorf("stash list = %v, want only the foreign entry %v", got, foreign)
	}
}

func TestSync_DirtyPathsOverlappingMainAreLeftAlone(t *testing.T) {
	f := newFixture(t)
	wt := addWorktree(t, f.work, "feat/a")
	writeCommit(t, wt, "a.txt", "a\n", "feat a")
	before := rev(t, wt, "HEAD")
	// main 側が足す g.txt を、worktree は未追跡のまま持っている
	writeFile(t, filepath.Join(wt, "g.txt"), "mine\n")
	f.advanceOrigin(t, "g.txt", "b\n")

	r := syncRepo(f.work)

	b := findBranchSync(t, r, "feat/a")
	if b.Status != statusNeedsManual || b.Reason != reasonDirtyOverlap {
		t.Errorf("feat/a = %q/%q, want %q/%q", b.Status, b.Reason, statusNeedsManual, reasonDirtyOverlap)
	}
	if got := rev(t, wt, "HEAD"); got != before {
		t.Errorf("HEAD moved: %s, want %s", got, before)
	}
	if got := readFile(t, filepath.Join(wt, "g.txt")); got != "mine\n" {
		t.Errorf("g.txt = %q, want the untouched local content", got)
	}
	if got := stashList(t, f.work); len(got) != 0 {
		t.Errorf("stash list = %v, want empty", got)
	}
}

func TestSync_RebaseConflictIsAbortedAndRestored(t *testing.T) {
	f := newFixture(t)
	wt := addWorktree(t, f.work, "feat/a")
	writeCommit(t, wt, "f.txt", "branch\n", "feat edits f")
	before := rev(t, wt, "HEAD")
	writeFile(t, filepath.Join(wt, "dirty.txt"), "d\n")
	f.advanceOrigin(t, "f.txt", "main\n")

	r := syncRepo(f.work)

	b := findBranchSync(t, r, "feat/a")
	if b.Status != statusNeedsManual || b.Reason != reasonRebaseFailed {
		t.Errorf("feat/a = %q/%q, want %q/%q", b.Status, b.Reason, statusNeedsManual, reasonRebaseFailed)
	}
	if got := rev(t, wt, "HEAD"); got != before {
		t.Errorf("HEAD = %s, want %s", got, before)
	}
	assertNoOperationInProgress(t, wt)
	if got := readFile(t, filepath.Join(wt, "dirty.txt")); got != "d\n" {
		t.Errorf("dirty change lost: %q", got)
	}
	if got := readFile(t, filepath.Join(wt, "f.txt")); got != "branch\n" {
		t.Errorf("f.txt = %q, want the branch content", got)
	}
	if got := stashList(t, f.work); len(got) != 0 {
		t.Errorf("stash list = %v, want empty", got)
	}
}

func TestSync_RebaseRefusedBeforeStartingRestoresDirtyChanges(t *testing.T) {
	f := newFixture(t)
	hook := filepath.Join(f.work, ".git", "hooks", "pre-rebase")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wt := addWorktree(t, f.work, "feat/a")
	writeCommit(t, wt, "a.txt", "a\n", "feat a")
	before := rev(t, wt, "HEAD")
	writeFile(t, filepath.Join(wt, "a.txt"), "dirty\n")
	f.advanceOrigin(t, "g.txt", "b\n")

	r := syncRepo(f.work)

	b := findBranchSync(t, r, "feat/a")
	if b.Status != statusNeedsManual || b.Reason != reasonRebaseFailed {
		t.Errorf("feat/a = %q/%q (%s), want %q/%q", b.Status, b.Reason, b.Detail, statusNeedsManual, reasonRebaseFailed)
	}
	if got := rev(t, wt, "HEAD"); got != before {
		t.Errorf("HEAD = %s, want %s", got, before)
	}
	if got := readFile(t, filepath.Join(wt, "a.txt")); got != "dirty\n" {
		t.Errorf("dirty change not restored: a.txt = %q", got)
	}
	if got := stashList(t, f.work); len(got) != 0 {
		t.Errorf("stash list = %v, want empty", got)
	}
}

func TestSync_MissingLocalDefaultBranchSkipsBranches(t *testing.T) {
	f := newFixture(t)
	gitT(t, f.work, "switch", "-c", "feat/x")
	writeCommit(t, f.work, "x.txt", "x\n", "feat x")
	gitT(t, f.work, "branch", "-D", "main")
	before := rev(t, f.work, "HEAD")
	f.advanceOrigin(t, "g.txt", "b\n")

	r := syncRepo(f.work)

	if r.Main.Status != statusSkipped || r.Main.Reason != reasonNoLocalBranch {
		t.Errorf("main = %q/%q, want %q/%q", r.Main.Status, r.Main.Reason, statusSkipped, reasonNoLocalBranch)
	}
	b := findBranchSync(t, r, "feat/x")
	if b.Status != statusSkipped || b.Reason != reasonNoLocalBranch {
		t.Errorf("feat/x = %q/%q (%s), want %q/%q", b.Status, b.Reason, b.Detail, statusSkipped, reasonNoLocalBranch)
	}
	if got := rev(t, f.work, "HEAD"); got != before {
		t.Errorf("HEAD moved: %s, want %s", got, before)
	}
}

func TestSync_SkipsWorktreeWithOperationInProgress(t *testing.T) {
	f := newFixture(t)
	wt := addWorktree(t, f.work, "feat/a")
	writeCommit(t, wt, "f.txt", "branch\n", "feat edits f")
	f.advanceOrigin(t, "f.txt", "main\n")
	gitT(t, f.work, "fetch", "origin")
	gitT(t, f.work, "merge", "--ff-only", "origin/main")
	// 利用者が途中で止めている rebase
	cmdErr := gitCmd(wt, "rebase", "main").Run()
	if cmdErr == nil {
		t.Fatal("setup: expected the rebase to stop on a conflict")
	}
	f.advanceOrigin(t, "g.txt", "b\n")

	r := syncRepo(f.work)

	b := findBranchSync(t, r, "feat/a")
	if b.Status != statusSkipped || b.Reason != reasonOperationInProgress {
		t.Errorf("feat/a = %q/%q, want %q/%q", b.Status, b.Reason, statusSkipped, reasonOperationInProgress)
	}
	gp := strings.TrimSpace(gitT(t, wt, "rev-parse", "--path-format=absolute", "--git-path", "rebase-merge"))
	if _, err := os.Stat(gp); err != nil {
		t.Error("the user's rebase in progress was aborted")
	}
}

func TestSync_StrayBranchIsReportedAndUntouched(t *testing.T) {
	f := newFixture(t)
	gitT(t, f.work, "branch", "stray/x")
	before := rev(t, f.work, "stray/x")
	f.advanceOrigin(t, "g.txt", "b\n")

	r := syncRepo(f.work)

	if len(r.StrayBranches) != 1 || r.StrayBranches[0] != "stray/x" {
		t.Errorf("stray = %v, want [stray/x]", r.StrayBranches)
	}
	if got := rev(t, f.work, "stray/x"); got != before {
		t.Errorf("stray/x moved: %s, want %s", got, before)
	}
}

func TestSync_FetchFailureIsReported(t *testing.T) {
	f := newFixture(t)
	gitT(t, f.work, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	before := rev(t, f.work, "main")

	r := syncRepo(f.work)

	if !strings.HasPrefix(r.Error, "fetch") {
		t.Errorf("error = %q, want a fetch error", r.Error)
	}
	if got := rev(t, f.work, "main"); got != before {
		t.Errorf("main moved: %s", got)
	}
}

func TestSync_RepoWithoutOrigin(t *testing.T) {
	isolateGit(t)
	dir := newRepo(t)

	r := syncRepo(dir)

	if r.Error != errNoOrigin {
		t.Errorf("error = %q, want %q", r.Error, errNoOrigin)
	}
}

func TestResolveRepo(t *testing.T) {
	paths := []string{
		"/root/github.com/alice/agents-daemon",
		"/root/github.com/alice/tool",
		"/root/github.com/bob/tool",
		"/root/github.com/alice/.github",
	}
	tests := []struct {
		name    string
		arg     string
		want    string
		wantErr string
	}{
		{name: "リポジトリ名", arg: "agents-daemon", want: "/root/github.com/alice/agents-daemon"},
		{name: "owner/name", arg: "bob/tool", want: "/root/github.com/bob/tool"},
		{name: "host/owner/name", arg: "github.com/alice/tool", want: "/root/github.com/alice/tool"},
		{name: "複数に当たる名前はエラー", arg: "tool", wantErr: "ambiguous"},
		{name: "当たらない名前はエラー", arg: "nothing", wantErr: "not found"},
		{name: "名前の途中には当たらない", arg: "daemon", wantErr: "not found"},
		{name: "ドットで始まる名前はパスとして扱わない", arg: ".github", want: "/root/github.com/alice/.github"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveRepo(tt.arg, paths)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveRepo_PathOfLinkedWorktreeResolvesToMainWorktree(t *testing.T) {
	isolateGit(t)
	dir := newRepo(t)
	main, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	wt := addWorktree(t, dir, "feat/a")

	for _, arg := range []string{dir, wt, filepath.Join(wt, ".")} {
		got, err := resolveRepo(arg, nil)
		if err != nil {
			t.Fatalf("%s: %v", arg, err)
		}
		if got != main {
			t.Errorf("resolveRepo(%s) = %q, want %q", arg, got, main)
		}
	}
}

func TestSync_WritesNothingToGitConfig(t *testing.T) {
	f := newFixture(t)
	wt := addWorktree(t, f.work, "feat/a")
	writeCommit(t, wt, "a.txt", "a\n", "feat a")
	writeFile(t, filepath.Join(wt, "a.txt"), "dirty\n")
	f.advanceOrigin(t, "g.txt", "b\n")
	cfg := filepath.Join(f.work, ".git", "config")
	before := readFile(t, cfg)

	syncRepo(f.work)

	if got := readFile(t, cfg); got != before {
		t.Errorf(".git/config changed:\n%s\n---\n%s", before, got)
	}
}
