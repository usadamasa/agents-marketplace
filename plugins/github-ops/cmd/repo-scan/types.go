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
