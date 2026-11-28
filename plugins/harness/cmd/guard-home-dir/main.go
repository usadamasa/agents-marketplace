package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/usadamasa/agents-marketplace/plugins/harness/internal/pathutil"
)

// alwaysAllowedSubdirs は YAML に書かなくても許すホーム配下の置き場。
var alwaysAllowedSubdirs = []string{
	".claude",
}

// hookInput は PreToolUse フックの入力 JSON 構造
type hookInput struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
	CWD       string         `json:"cwd"`
}

// denyResponse は deny 時の出力 JSON 構造
type denyResponse struct {
	HookSpecificOutput hookOutput `json:"hookSpecificOutput"`
}

type hookOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

func main() {
	// --help フラグ対応 (実行可能性チェック用)
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Fprintf(os.Stderr, "使い方: guard-home-dir < JSON\nPreToolUse フック: ホームディレクトリ走査を防止する\n")
		os.Exit(0)
	}

	// stdin から JSON 入力を読み取り
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stdin 読み取りエラー: %v\n", err)
		os.Exit(1)
	}

	var input hookInput
	if err := json.Unmarshal(data, &input); err != nil {
		fmt.Fprintf(os.Stderr, "JSON パースエラー: %v\n", err)
		os.Exit(1)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ホームディレクトリ取得エラー: %v\n", err)
		os.Exit(1)
	}
	// macOS の /var → /private/var 等の symlink を解決
	if resolved, err := pathutil.ResolveRealpath(home); err == nil {
		home = resolved
	}

	cwd := input.CWD
	if cwd != "" {
		if resolved, err := pathutil.ResolveRealpath(cwd); err == nil {
			cwd = resolved
		}
	}

	paths := extractToolPaths(input, home)
	if len(paths) == 0 {
		os.Exit(0)
	}

	resolvedPaths := resolvePaths(paths, cwd)

	// 許可パスチェック
	cfg := loadConfig(home, os.Getenv)
	deniedPath := checkPaths(resolvedPaths, home, cwd, cfg.dirs)
	if deniedPath == "" {
		os.Exit(0)
	}

	// deny JSON 出力
	writeDeny(denyReason(deniedPath, cfg))
}

// extractToolPaths はツール名に応じてチェック対象パスを抽出する。
// パスがない場合は nil を返す (→ 通過)。
func extractToolPaths(input hookInput, home string) []string {
	switch input.ToolName {
	case "Read", "Edit", "Write", "NotebookEdit":
		p := extractFilePath(input.ToolName, input.ToolInput)
		if p != "" {
			return []string{p}
		}
		return nil
	case "Glob", "Grep":
		p := extractFilePath(input.ToolName, input.ToolInput)
		if p == "" {
			return nil
		}
		return []string{p}
	case "Bash":
		command, ok := input.ToolInput["command"].(string)
		if !ok || command == "" {
			return nil
		}
		return extractScanTargets(command, home)
	default:
		return nil
	}
}

// resolvePaths は各パスを絶対パスに変換し、シンボリックリンクを解決する。
func resolvePaths(paths []string, cwd string) []string {
	var resolved []string
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		r, err := pathutil.ResolveRealpath(p)
		if err != nil {
			r = filepath.Clean(p)
		}
		resolved = append(resolved, r)
	}
	return resolved
}

// writeDeny は deny JSON を stdout に出力する。
func writeDeny(reason string) {
	resp := denyResponse{
		HookSpecificOutput: hookOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "deny",
			PermissionDecisionReason: reason,
		},
	}
	out, _ := json.Marshal(resp)
	out = append(out, '\n')
	_, _ = os.Stdout.Write(out)
}

// extractFilePath はツール名に応じたファイルパスを tool_input から抽出する。
func extractFilePath(toolName string, input map[string]any) string {
	var key string
	switch toolName {
	case "Read", "Edit", "Write":
		key = "file_path"
	case "NotebookEdit":
		key = "notebook_path"
	case "Glob", "Grep":
		key = "path"
	default:
		return ""
	}

	v, ok := input[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// checkPaths はパスリストを検証し、禁止パスがあればそのパスを返す。
// 全て許可なら空文字列を返す。dirs は YAML で許したホームからの相対パス。
func checkPaths(paths []string, home string, cwd string, dirs []string) string {
	if len(paths) == 0 {
		return ""
	}

	// 許可パスリストを構築
	allowed := make([]string, 0, len(alwaysAllowedSubdirs)+len(dirs)+1)
	for _, sub := range alwaysAllowedSubdirs {
		allowed = append(allowed, filepath.Join(home, sub))
	}
	for _, sub := range dirs {
		allowed = append(allowed, filepath.Join(home, sub))
	}
	if cwd != "" {
		allowed = append(allowed, cwd)
	}

	for _, p := range paths {
		// ホームディレクトリ配下でなければ通過
		if !strings.HasPrefix(p, home+"/") && p != home {
			continue
		}

		// 許可パスに前方一致するか
		isAllowed := false
		for _, a := range allowed {
			if p == a || strings.HasPrefix(p, a+"/") {
				isAllowed = true
				break
			}
		}

		if !isAllowed {
			return p
		}
	}

	return ""
}
