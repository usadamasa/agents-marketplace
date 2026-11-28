package main

// プロジェクト単位の opt-in 判定。
//
// gh の PR 書き込み操作 (edit / ready / merge / close / reopen) と issue 書き込み操作
// (create / edit / close / reopen / pin / lock 系) は既定で ask に落ちる。信頼できる
// プロジェクトに限ってこの ask をスキップするための opt-in をここで判定する。
// PR と issue は別々の環境変数で、片方を有効にしてももう片方は ask のまま。
// opt-in を有効にしても ask に残る形がある。条件は internal/rules/gh の PROptIn / IssueOptIn が持つ。
//
// 環境変数が立っているだけでは有効にしない。シェルの export や user スコープの
// settings.json で全プロジェクトに効いてしまうのを防ぐため、プロジェクトスコープの
// settings ファイルに定義されていることを実ファイルから確認する。
// 判定は fail-closed で、少しでも条件を満たさなければ従来どおり ask に落とす。
//
// opt-in が効くのはプロジェクトのリポジトリへの操作だけ (ghauthor.InProject)。
// .envrc などが GH_REPO / GH_HOST を Bash のシェルにだけ載せているセッションでは、
// 対象リポジトリを決められないので opt-in を効かせない (sessionenv.go)。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ghPRWritesKey / ghIssueWritesKey は gh の書き込み操作の ask スキップを有効にする環境変数名。
const (
	ghPRWritesKey    = "CLAUDE_ALLOW_GH_PR_WRITES"
	ghIssueWritesKey = "CLAUDE_ALLOW_GH_ISSUE_WRITES"
)

// projectSettingsFiles は opt-in の定義元として認めるプロジェクトスコープの settings。
var projectSettingsFiles = []string{"settings.json", "settings.local.json"}

// optInResult は opt-in の判定結果と、その根拠。
type optInResult struct {
	Enabled bool
	// Reason は判定の根拠 (ログ用)。環境変数が立っていないとき、つまり opt-in を
	// そもそも使っていないときは空にして無音にする。
	Reason string
}

// settingsEnv は settings.json のうち env セクションだけを読む。
type settingsEnv struct {
	Env map[string]string `json:"env"`
}

// ghWritesOptIn は opt-in キー key による ask スキップが有効かを判定する。
func ghWritesOptIn(key, projectDir, homeDir string) optInResult {
	if !truthy(os.Getenv(key)) {
		return optInResult{}
	}
	if projectDir == "" {
		return optInResult{Reason: fmt.Sprintf(
			"%s が設定されていますが、プロジェクトディレクトリを解決できないため無効にしました", key)}
	}
	// user スコープでの定義は認めない。プロジェクトスコープにも定義があるかによらず、
	// 全プロジェクトへ効く書き方をしている時点で無効にする。
	if userPath := filepath.Join(homeDir, ".claude", "settings.json"); settingsEnables(userPath, key) {
		return optInResult{Reason: fmt.Sprintf(
			"%s が user スコープの %s に定義されています。opt-in はプロジェクトスコープ限定のため無効にしました",
			key, userPath)}
	}
	for _, name := range projectSettingsFiles {
		path := filepath.Join(projectDir, ".claude", name)
		if settingsEnables(path, key) {
			return optInResult{Enabled: true, Reason: fmt.Sprintf("%s を %s で有効化", key, path)}
		}
	}
	return optInResult{Reason: fmt.Sprintf(
		"%s が設定されていますが、プロジェクトスコープの settings (%s の %s) に定義が無いため無効にしました",
		key, filepath.Join(projectDir, ".claude"), strings.Join(projectSettingsFiles, " / "))}
}

// resolveProjectDir はプロジェクトルートを返す。hook には CLAUDE_PROJECT_DIR が渡るが、
// 取れない場合に備えて PreToolUse JSON の cwd にも落とす。
func resolveProjectDir(inputCwd string) string {
	if dir := os.Getenv("CLAUDE_PROJECT_DIR"); dir != "" {
		return dir
	}
	return inputCwd
}

// resolveWorkdir は判定対象のコマンドが動くディレクトリを返す。PreToolUse JSON の
// cwd を優先し、取れない場合だけ CLAUDE_PROJECT_DIR に落とす
// (opt-in の判定と違い、プロジェクトルートではなくコマンドの実行位置が要る)。
func resolveWorkdir(inputCwd string) string {
	if inputCwd != "" {
		return inputCwd
	}
	return os.Getenv("CLAUDE_PROJECT_DIR")
}

// settingsEnables は settings ファイルの env で opt-in キー key が有効化されているかを返す。
// 読めない・壊れている・値が文字列でない場合は false (fail-closed)。
func settingsEnables(path, key string) bool {
	data, err := os.ReadFile(path) // #nosec G304 -- hook: パスは CLAUDE_PROJECT_DIR / HOME 由来
	if err != nil {
		return false
	}
	var s settingsEnv
	if err := json.Unmarshal(data, &s); err != nil {
		return false
	}
	return truthy(s.Env[key])
}

// truthy は opt-in を有効とみなす値かを返す。
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true":
		return true
	default:
		return false
	}
}
