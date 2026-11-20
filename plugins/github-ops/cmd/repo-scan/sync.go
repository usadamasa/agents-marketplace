package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	statusFastForwarded = "fast_forwarded"
	statusUpToDate      = "up_to_date"
	statusRebased       = "rebased"
	statusSkipped       = "skipped"
	statusNeedsManual   = "needs_manual"

	reasonNoLocalBranch       = "no_local_branch"
	reasonNoRemoteBranch      = "no_remote_branch"
	reasonDiverged            = "diverged"
	reasonFFFailed            = "ff_failed"
	reasonWorktreeMissing     = "worktree_missing"
	reasonOperationInProgress = "operation_in_progress"
	reasonDirtyOverlap        = "dirty_overlap"
	reasonStashFailed         = "stash_failed"
	reasonRebaseFailed        = "rebase_failed"
	reasonRestoreFailed       = "restore_failed"
	reasonStashRestoreFailed  = "stash_restore_failed"

	errNoOrigin = "no_origin"

	stashTagPrefix = "repo-scan sync auto-stash"
)

// syncRepo は dir (本体 worktree) を fetch し、default branch の FF と worktree に出ているブランチの rebase を行う。
// push・reset --hard・git config への書き込みはしない。止まった操作は abort して元の状態へ戻す。
func syncRepo(dir string) RepoSync {
	r := RepoSync{
		Name:          repoName(dir),
		Path:          dir,
		Branches:      []BranchSync{},
		StrayBranches: []string{},
	}
	if _, err := git(dir, "remote", "get-url", "origin"); err != nil {
		r.Error = errNoOrigin
		return r
	}
	if out, err := gitRun(dir, "fetch", "origin"); err != nil {
		r.Error = "fetch: " + out
		return r
	}
	def := detectDefaultBranch(dir)
	r.DefaultBranch = def

	checkedOut, err := branchWorktrees(dir)
	if err != nil {
		r.Error = "for-each-ref: " + err.Error()
		return r
	}
	r.Main = syncMain(dir, def, checkedOut[def])
	_, hasDef := checkedOut[def]

	names := make([]string, 0, len(checkedOut))
	for name := range checkedOut {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		wt := checkedOut[name]
		switch {
		case name == def:
		case wt == "":
			r.StrayBranches = append(r.StrayBranches, name)
		case !hasDef:
			r.Branches = append(r.Branches, BranchSync{Name: name, Worktree: wt, Status: statusSkipped, Reason: reasonNoLocalBranch})
		default:
			r.Branches = append(r.Branches, syncBranch(wt, name, def))
		}
	}
	return r
}

// branchWorktrees はローカルブランチから、そのブランチが出ている worktree のパスへの対応を返す。
// どこにも出ていないブランチ (stray) は空文字に対応させる。rebase の途中で HEAD が外れた worktree も、
// そのブランチを出しているものとして扱う。
func branchWorktrees(dir string) (map[string]string, error) {
	out, err := git(dir, "for-each-ref", "--format=%(refname:short)%00%(worktreepath)", "refs/heads")
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		name, wt, ok := strings.Cut(line, "\x00")
		if !ok || name == "" {
			continue
		}
		m[name] = wt
	}
	// %(worktreepath) は、rebase の途中で HEAD が外れた worktree を拾わない。rebase の head-name から補う。
	wts, err := git(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	for line := range strings.SplitSeq(wts, "\n") {
		wt, ok := strings.CutPrefix(line, "worktree ")
		if !ok {
			continue
		}
		if name := rebasingBranch(wt); name != "" {
			if cur, known := m[name]; known && cur == "" {
				m[name] = wt
			}
		}
	}
	return m, nil
}

// rebasingBranch は worktree wt で rebase の途中にあるブランチ名を返す。rebase 中でなければ空文字。
func rebasingBranch(wt string) string {
	for _, dir := range []string{"rebase-merge", "rebase-apply"} {
		p, err := git(wt, "rev-parse", "--path-format=absolute", "--git-path", dir+"/head-name")
		if err != nil {
			continue
		}
		b, err := os.ReadFile(p) // #nosec G304 -- git が返した自リポジトリの管理ファイル
		if err != nil {
			continue
		}
		return strings.TrimPrefix(strings.TrimSpace(string(b)), "refs/heads/")
	}
	return ""
}

// syncMain は default branch を origin へ FF する。ブランチが出ている worktree があればそこで merge --ff-only し、
// どこにも出ていなければ旧値を条件にした update-ref で ref だけを進める。
func syncMain(dir, def, wt string) MainSync {
	local, err := git(dir, "rev-parse", "--verify", "-q", "refs/heads/"+def)
	if err != nil {
		return MainSync{Status: statusSkipped, Reason: reasonNoLocalBranch}
	}
	remote, err := git(dir, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+def)
	if err != nil {
		return MainSync{Status: statusSkipped, Reason: reasonNoRemoteBranch}
	}
	if local == remote {
		return MainSync{Status: statusUpToDate}
	}
	if !gitOK(dir, "merge-base", "--is-ancestor", local, remote) {
		return MainSync{Status: statusSkipped, Reason: reasonDiverged}
	}
	if wt != "" {
		if operationInProgress(wt) {
			return MainSync{Status: statusSkipped, Reason: reasonOperationInProgress}
		}
		if out, err := gitRun(wt, "merge", "--ff-only", remote); err != nil {
			return MainSync{Status: statusSkipped, Reason: reasonFFFailed, Detail: out}
		}
	} else if out, err := gitRun(dir, "update-ref", "-m", "repo-scan sync: fast-forward", "refs/heads/"+def, remote, local); err != nil {
		return MainSync{Status: statusSkipped, Reason: reasonFFFailed, Detail: out}
	}
	newSHA, err := git(dir, "rev-parse", "refs/heads/"+def)
	if err != nil {
		return MainSync{Status: statusSkipped, Reason: reasonFFFailed, Detail: err.Error()}
	}
	return MainSync{Status: statusFastForwarded, Old: local, New: newSHA}
}

// syncBranch は worktree wt に出ているブランチ name を def へ rebase する。
// dirty な変更は stash へ退避して戻す。def 側の変更と重なる dirty は、戻すときに衝突しうるので触らない。
func syncBranch(wt, name, def string) BranchSync {
	b := BranchSync{Name: name, Worktree: wt}
	if _, err := os.Stat(wt); err != nil {
		return withStatus(b, statusSkipped, reasonWorktreeMissing, "")
	}
	if operationInProgress(wt) {
		return withStatus(b, statusSkipped, reasonOperationInProgress, "")
	}
	if gitOK(wt, "merge-base", "--is-ancestor", def, "HEAD") {
		return withStatus(b, statusUpToDate, "", "")
	}
	before, err := git(wt, "rev-parse", "HEAD")
	if err != nil {
		return withStatus(b, statusSkipped, reasonRestoreFailed, err.Error())
	}

	dirty, err := dirtyPaths(wt)
	if err != nil {
		return withStatus(b, statusNeedsManual, reasonStashFailed, err.Error())
	}
	stash := ""
	if len(dirty) > 0 {
		overlap, err := overlapWithIncoming(wt, def, dirty)
		if err != nil {
			return withStatus(b, statusNeedsManual, reasonDirtyOverlap, err.Error())
		}
		if len(overlap) > 0 {
			return withStatus(b, statusNeedsManual, reasonDirtyOverlap, strings.Join(overlap, ", "))
		}
		tag := fmt.Sprintf("%s %s %d", stashTagPrefix, name, time.Now().UnixNano())
		if out, err := gitRun(wt, "stash", "push", "--include-untracked", "-m", tag); err != nil {
			return withStatus(b, statusNeedsManual, reasonStashFailed, out)
		}
		stash, err = findStash(wt, tag)
		if err != nil {
			return withStatus(b, statusNeedsManual, reasonStashFailed, err.Error())
		}
	}

	if out, err := gitRun(wt, "rebase", def); err != nil {
		b = withStatus(b, statusNeedsManual, reasonRebaseFailed, out)
		// hook が断ったときなど、rebase が始まらずに失敗したなら abort するものは無い
		if operationInProgress(wt) {
			if abortOut, err := gitRun(wt, "rebase", "--abort"); err != nil || operationInProgress(wt) {
				b.Reason = reasonRestoreFailed
				b.Detail = out + "\n" + abortOut
				b.Stash = stash
				return b
			}
		}
		if head, _ := git(wt, "rev-parse", "HEAD"); head != before {
			b.Reason = reasonRestoreFailed
			b.Detail = fmt.Sprintf("HEAD is %s after rebase --abort, expected %s", head, before)
			b.Stash = stash
			return b
		}
	} else {
		b = withStatus(b, statusRebased, "", "")
	}

	if stash != "" {
		if out, err := gitRun(wt, "stash", "apply", "--index", stash); err != nil {
			b = withStatus(b, statusNeedsManual, reasonStashRestoreFailed, out)
			b.Stash = stash
			return b
		}
		if err := dropStash(wt, stash); err != nil {
			b.Stash = stash
			b.Detail = "applied, but could not drop the stash entry: " + err.Error()
		}
	}
	return b
}

func withStatus(b BranchSync, status, reason, detail string) BranchSync {
	b.Status = status
	b.Reason = reason
	b.Detail = detail
	return b
}

// operationInProgress は worktree で rebase・merge・cherry-pick・revert・bisect が途中かを返す。
func operationInProgress(wt string) bool {
	for _, p := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG"} {
		gp, err := git(wt, "rev-parse", "--path-format=absolute", "--git-path", p)
		if err != nil {
			return true
		}
		if _, err := os.Stat(gp); err == nil {
			return true
		}
	}
	return false
}

// dirtyPaths は worktree の未コミットの変更と未追跡のファイルのパスを返す。rename は新旧の両方を含める。
func dirtyPaths(wt string) ([]string, error) {
	out, err := gitCmd(wt, "status", "--porcelain=v1", "-z", "--untracked-files=all").Output()
	if err != nil {
		return nil, err
	}
	var paths []string
	entries := strings.Split(string(out), "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		paths = append(paths, e[3:])
		if e[0] == 'R' || e[0] == 'C' {
			i++
			if i < len(entries) && entries[i] != "" {
				paths = append(paths, entries[i])
			}
		}
	}
	return paths, nil
}

// overlapWithIncoming は dirty のうち、merge-base から def までに変わったパスと重なるものを返す。
func overlapWithIncoming(wt, def string, dirty []string) ([]string, error) {
	base, err := git(wt, "merge-base", "HEAD", def)
	if err != nil {
		return nil, err
	}
	out, err := gitCmd(wt, "diff", "--name-only", "--no-renames", "-z", base, def).Output()
	if err != nil {
		return nil, err
	}
	incoming := map[string]bool{}
	for p := range strings.SplitSeq(string(out), "\x00") {
		if p != "" {
			incoming[p] = true
		}
	}
	var overlap []string
	for _, p := range dirty {
		if incoming[p] {
			overlap = append(overlap, p)
		}
	}
	return overlap, nil
}

// findStash は message が tag の stash の SHA を返す。stash の積み上げは worktree と他の作業で共有なので、位置ではなく tag で引く。
func findStash(wt, tag string) (string, error) {
	out, err := git(wt, "stash", "list", "--format=%H%x00%gs")
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(out, "\n") {
		sha, subject, ok := strings.Cut(line, "\x00")
		if ok && strings.HasSuffix(subject, ": "+tag) {
			return sha, nil
		}
	}
	return "", fmt.Errorf("stash entry %q not found", tag)
}

// dropStash は SHA が sha の stash を、今の位置を引き直してから消す。
func dropStash(wt, sha string) error {
	out, err := git(wt, "stash", "list", "--format=%H")
	if err != nil {
		return err
	}
	i := 0
	for line := range strings.SplitSeq(out, "\n") {
		if line == sha {
			if out, err := gitRun(wt, "stash", "drop", "stash@{"+strconv.Itoa(i)+"}"); err != nil {
				return errors.New(out)
			}
			return nil
		}
		i++
	}
	return fmt.Errorf("stash %s not found", sha)
}

// resolveRepo は利用者の指定を本体 worktree のパスへ解決する。
// パス (isPathArg) はそのリポジトリの本体 worktree へ、それ以外は ghq のパスの末尾
// (`name`・`owner/name`・`host/owner/name`) と照合する。
func resolveRepo(arg string, ghqPaths []string) (string, error) {
	if isPathArg(arg) {
		return mainWorktreeOf(arg)
	}
	var hits []string
	for _, p := range ghqPaths {
		if strings.HasSuffix(p, "/"+arg) {
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("repository %q not found in ghq list", arg)
	case 1:
		return hits[0], nil
	default:
		return "", fmt.Errorf("repository %q is ambiguous: %s", arg, strings.Join(hits, ", "))
	}
}

// isPathArg は arg がパスの指定かを返す。`.github` のようにドットで始まるリポジトリ名は名前として扱う。
func isPathArg(arg string) bool {
	return filepath.IsAbs(arg) || arg == "." || arg == ".." ||
		strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../")
}

// resolveRepoArg は resolveRepo を、名前の指定のときだけ `ghq list` を引いて呼ぶ。
func resolveRepoArg(arg string) (string, error) {
	var paths []string
	if !isPathArg(arg) {
		var err error
		if paths, err = ghqListPaths(); err != nil {
			return "", fmt.Errorf("ghq list: %w", err)
		}
	}
	return resolveRepo(arg, paths)
}

// mainWorktreeOf は path を含むリポジトリの本体 worktree のパスを返す。linked worktree からも本体へたどる。
func mainWorktreeOf(path string) (string, error) {
	common, err := git(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git repository", path)
	}
	if filepath.Base(common) != ".git" {
		return "", fmt.Errorf("%s has no main worktree (git dir %s)", path, common)
	}
	return filepath.EvalSymlinks(filepath.Dir(common))
}

// runSync は対象のリポジトリを同期し、結果の JSON を w に書く。
// repoArg があればその 1 本だけを (scan の対象でなくても) 同期する。無ければ scan の対象を全件同期する。
func runSync(w io.Writer, owners []string, repoArg string) error {
	var targets []string
	if repoArg != "" {
		if len(owners) > 0 {
			return errors.New("--owner cannot be combined with a repository argument")
		}
		p, err := resolveRepoArg(repoArg)
		if err != nil {
			return err
		}
		targets = []string{p}
	} else {
		paths, err := ghqListPaths()
		if err != nil {
			return fmt.Errorf("ghq list: %w", err)
		}
		for _, p := range filterByOwners(paths, owners) {
			r, err := inspectRepo(p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: inspect %s: %v\n", p, err)
				continue
			}
			if r.IsTarget {
				targets = append(targets, p)
			}
		}
	}

	res := SyncResult{SyncedAt: time.Now().Format(time.RFC3339), Repos: []RepoSync{}}
	for _, p := range targets {
		res.Repos = append(res.Repos, syncRepo(p))
	}
	return writeJSON(w, res)
}
