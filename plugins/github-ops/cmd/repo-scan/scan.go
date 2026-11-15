package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// repoName はパスから "<owner>/<repo>" を返す。
func repoName(path string) string {
	parent := filepath.Base(filepath.Dir(path))
	return parent + "/" + filepath.Base(path)
}

// filterByOwners は ghq パスの owner で絞り込む。owners が空なら絞り込まず全件を返す。
func filterByOwners(paths []string, owners []string) []string {
	if len(owners) == 0 {
		return paths
	}
	want := make(map[string]bool, len(owners))
	for _, o := range owners {
		want[o] = true
	}
	var out []string
	for _, p := range paths {
		if want[filepath.Base(filepath.Dir(p))] {
			out = append(out, p)
		}
	}
	return out
}

// detectDefaultBranch は origin/HEAD → main → master の順で default ブランチ名を返す。
func detectDefaultBranch(dir string) string {
	if ref, err := git(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		return strings.TrimPrefix(ref, "origin/")
	}
	for _, b := range []string{"main", "master"} {
		if gitOK(dir, "show-ref", "--verify", "--quiet", "refs/heads/"+b) {
			return b
		}
	}
	return "main"
}

// parseWorktrees は `git worktree list --porcelain` を解析する。mainDir は本体リポのパス。
func parseWorktrees(dir, mainDir string) []Worktree {
	out, err := git(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	var wts []Worktree
	var cur Worktree
	flush := func() {
		if cur.Path != "" {
			cur.IsMain = cur.Path == mainDir
			cur.Dirty = isDirty(cur.Path)
			wts = append(wts, cur)
		}
		cur = Worktree{}
	}
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return wts
}

// isDirty は worktree に未コミット変更があるかを返す。
func isDirty(dir string) bool {
	out, err := git(dir, "status", "--porcelain")
	return err == nil && out != ""
}

// countLeftRight は `A...B` の (left, right) コミット数を返す。
func countLeftRight(dir, a, b string) (left, right int) {
	out, err := git(dir, "rev-list", "--left-right", "--count", a+"..."+b)
	if err != nil {
		return 0, 0
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0
	}
	left, _ = strconv.Atoi(fields[0])
	right, _ = strconv.Atoi(fields[1])
	return left, right
}

// inspectRepo は repo path を検査して Repo を返す。fetch はしない (read-only)。
func inspectRepo(dir string) (Repo, error) {
	def := detectDefaultBranch(dir)
	r := Repo{
		Name:          repoName(dir),
		Path:          dir,
		DefaultBranch: def,
		Remote:        "origin",
	}

	wts := parseWorktrees(dir, dir)
	r.Worktrees = wts

	// branch -> worktree path
	checkedOut := map[string]string{}
	for _, w := range wts {
		if w.Branch != "" {
			checkedOut[w.Branch] = w.Path
		}
	}

	// origin/<def> が存在すれば main の追従状況を計算
	originRef := "origin/" + def
	if gitOK(dir, "show-ref", "--verify", "--quiet", "refs/remotes/"+originRef) {
		_, ahead := countLeftRight(dir, def, originRef) // right = origin にあって local main にない
		r.MainBehind = ahead
		r.MainFFPossible = gitOK(dir, "merge-base", "--is-ancestor", def, originRef)
	}

	// ローカルブランチ一覧
	out, err := git(dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return r, err
	}
	hasNonMain := false
	for name := range strings.SplitSeq(out, "\n") {
		if name == "" {
			continue
		}
		b := Branch{Name: name}
		if wtPath, ok := checkedOut[name]; ok {
			b.CheckedOutIn = wtPath
			b.Dirty = isDirty(wtPath)
		} else {
			b.IsStray = true
		}
		behind, ahead := countLeftRight(dir, def, name)
		b.BehindMain = behind
		b.AheadMain = ahead
		if name != def {
			hasNonMain = true
		}
		r.Branches = append(r.Branches, b)
	}

	switch {
	case len(wts) > 1:
		r.IsTarget = true
		r.Reason = "has_worktrees"
	case hasNonMain:
		r.IsTarget = true
		r.Reason = "has_non_main_branch"
	}
	return r, nil
}

// runScan は ghq 配下のリポジトリをスキャンし、対象リポの同期プラン JSON を w に書く。
// owners が空なら ghq 配下の全リポジトリを対象にする。
func runScan(w io.Writer, owners []string) error {
	root, err := ghqRoot()
	if err != nil {
		return fmt.Errorf("ghq root: %w", err)
	}
	paths, err := ghqListPaths()
	if err != nil {
		return fmt.Errorf("ghq list: %w", err)
	}
	paths = filterByOwners(paths, owners)

	plan := Plan{
		ScannedAt: time.Now().Format(time.RFC3339),
		GhqRoot:   root,
	}
	for _, p := range paths {
		r, err := inspectRepo(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: inspect %s: %v\n", p, err)
			continue
		}
		if r.IsTarget {
			plan.Repos = append(plan.Repos, r)
		}
	}
	return writeJSON(w, plan)
}
