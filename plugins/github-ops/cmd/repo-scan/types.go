package main

type Worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	IsMain bool   `json:"is_main"`
	Dirty  bool   `json:"dirty"`
}

type Branch struct {
	Name         string `json:"name"`
	CheckedOutIn string `json:"checked_out_in"`
	Dirty        bool   `json:"dirty"`
	BehindMain   int    `json:"behind_main"`
	AheadMain    int    `json:"ahead_main"`
	IsStray      bool   `json:"is_stray"`
}

type Repo struct {
	Name           string     `json:"name"`
	Path           string     `json:"path"`
	DefaultBranch  string     `json:"default_branch"`
	Remote         string     `json:"remote"`
	IsTarget       bool       `json:"is_target"`
	Reason         string     `json:"reason"`
	MainBehind     int        `json:"main_behind"`
	MainFFPossible bool       `json:"main_ff_possible"`
	Worktrees      []Worktree `json:"worktrees"`
	Branches       []Branch   `json:"branches"`
}

type Plan struct {
	ScannedAt string `json:"scanned_at"`
	GhqRoot   string `json:"ghq_root"`
	Repos     []Repo `json:"repos"`
}

// SyncResult は `repo-scan sync` の出力。
type SyncResult struct {
	SyncedAt string     `json:"synced_at"`
	Repos    []RepoSync `json:"repos"`
}

// RepoSync は 1 リポジトリの同期結果。Error が空でなければ、そのリポジトリは何も変えずに終えている。
type RepoSync struct {
	Name          string       `json:"name"`
	Path          string       `json:"path"`
	DefaultBranch string       `json:"default_branch"`
	Error         string       `json:"error,omitempty"`
	Main          MainSync     `json:"main"`
	Branches      []BranchSync `json:"branches"`
	StrayBranches []string     `json:"stray_branches"`
}

// MainSync は default branch の FF の結果。Old と New はキャッチアップの range に使う。
type MainSync struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
	Old    string `json:"old,omitempty"`
	New    string `json:"new,omitempty"`
}

// BranchSync は worktree に出ているブランチの rebase の結果。
// Stash が空でなければ、退避した変更を戻せずに stash へ残している。
type BranchSync struct {
	Name     string `json:"name"`
	Worktree string `json:"worktree"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Stash    string `json:"stash,omitempty"`
}
