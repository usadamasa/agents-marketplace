package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestFilterByOwners(t *testing.T) {
	paths := []string{
		"/root/github.com/alice/tool-a",
		"/root/github.com/example-org/service-b",
		"/root/github.com/example-org/service-c",
		"/root/github.com/bob/tool-d",
	}
	tests := []struct {
		name   string
		owners []string
		want   []string
	}{
		{
			name:   "owners が空なら全件そのまま",
			owners: nil,
			want:   paths,
		},
		{
			name:   "1 owner を指定",
			owners: []string{"alice"},
			want:   []string{"/root/github.com/alice/tool-a"},
		},
		{
			name:   "複数 owner を指定",
			owners: []string{"alice", "example-org"},
			want: []string{
				"/root/github.com/alice/tool-a",
				"/root/github.com/example-org/service-b",
				"/root/github.com/example-org/service-c",
			},
		},
		{
			name:   "一致する owner が無ければ空",
			owners: []string{"nobody"},
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterByOwners(paths, tt.owners)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInspectRepo_NonMainBranch(t *testing.T) {
	dir := newRepo(t)
	gitT(t, dir, "branch", "feat/x")

	r, err := inspectRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.DefaultBranch != "main" {
		t.Errorf("default branch = %q, want main", r.DefaultBranch)
	}
	if !r.IsTarget {
		t.Error("expected IsTarget=true (has non-main branch)")
	}
	if r.Reason != "has_non_main_branch" {
		t.Errorf("reason = %q, want has_non_main_branch", r.Reason)
	}
	// feat/x はどの worktree にも出ていないので stray
	var feat *Branch
	for i := range r.Branches {
		if r.Branches[i].Name == "feat/x" {
			feat = &r.Branches[i]
		}
	}
	if feat == nil {
		t.Fatal("feat/x not found in branches")
	}
	if !feat.IsStray {
		t.Error("expected feat/x to be stray")
	}
}

func TestInspectRepo_OnlyMain(t *testing.T) {
	dir := newRepo(t)
	r, err := inspectRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.IsTarget {
		t.Error("expected IsTarget=false (only main, single worktree)")
	}
}

func TestInspectRepo_Worktree(t *testing.T) {
	dir := newRepo(t)
	wt := t.TempDir() + "-wt"
	gitT(t, dir, "worktree", "add", "-b", "feat/y", wt)
	// git は worktree path の symlink を解決して記録する (macOS の /var -> /private/var 等)。
	// 比較対象も同様に解決しておく。
	if resolved, err := filepath.EvalSymlinks(wt); err == nil {
		wt = resolved
	}

	r, err := inspectRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !r.IsTarget {
		t.Error("expected IsTarget=true (has worktree)")
	}
	if r.Reason != "has_worktrees" {
		t.Errorf("reason = %q, want has_worktrees", r.Reason)
	}
	var feat *Branch
	for i := range r.Branches {
		if r.Branches[i].Name == "feat/y" {
			feat = &r.Branches[i]
		}
	}
	if feat == nil {
		t.Fatal("feat/y not found")
	}
	if feat.IsStray {
		t.Error("feat/y is checked out in a worktree, should not be stray")
	}
	if feat.CheckedOutIn != wt {
		t.Errorf("CheckedOutIn = %q, want %q", feat.CheckedOutIn, wt)
	}
}

func TestInspectRepo_MainBehindFF(t *testing.T) {
	// bare リポを origin として用意する。
	origin := t.TempDir()
	gitT(t, origin, "init", "--bare", "-b", "main")

	// origin を初期化するための seed クローンを作り、初期コミットを push する。
	seed := t.TempDir()
	gitT(t, seed, "clone", origin, ".")
	writeCommit(t, seed, "f.txt", "a\n", "init")
	gitT(t, seed, "push", "origin", "main")

	// 検査対象となる作業クローン。
	work := t.TempDir()
	gitT(t, work, "clone", origin, ".")

	// origin 側を 1 コミット進める (seed から push)。
	writeCommit(t, seed, "g.txt", "b\n", "advance")
	gitT(t, seed, "push", "origin", "main")

	// 作業クローンで fetch すると origin/main が local main より進む (FF 可能)。
	gitT(t, work, "fetch", "origin")

	r, err := inspectRepo(work)
	if err != nil {
		t.Fatal(err)
	}
	if r.DefaultBranch != "main" {
		t.Errorf("default branch = %q, want main", r.DefaultBranch)
	}
	if r.MainBehind <= 0 {
		t.Errorf("MainBehind = %d, want > 0", r.MainBehind)
	}
	if !r.MainFFPossible {
		t.Error("expected MainFFPossible=true (local main is ancestor of origin/main)")
	}
}
