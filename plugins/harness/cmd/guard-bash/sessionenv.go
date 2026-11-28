package main

// Bash のシェルにだけ載る環境変数の読み取り。
//
// SessionStart / CwdChanged の hook が CLAUDE_ENV_FILE へ書いた内容は、Bash ツールのシェルが
// コマンドの前に読み込む。PreToolUse の hook のプロセスには渡らないので、ここで代入される
// GH_REPO や GIT_DIR は os.Getenv では見えない。Claude Code はこのファイルを
// <設定ディレクトリ>/session-env/<session_id>/ に置くので、そこから代入される名前を拾う。

import (
	"os"
	"path/filepath"
	"regexp"
)

// envAssignPattern は shell の文の頭にある代入 (`export NAME=`、`NAME=`) の名前に当たる。
// direnv は `export A=$'..';export B=$'..';` と 1 行に並べるので、`;` の後ろも文の頭として扱う。
var envAssignPattern = regexp.MustCompile(`(?m)(?:^|;)\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)=`)

// sessionEnvAssigns は、セッション sessionID の env ファイルが代入する環境変数の名前を返す。
// env ファイルが無いセッションは空を返す。読めないときは ok が false。
func sessionEnvAssigns(sessionID string) (names []string, ok bool) {
	if sessionID == "" {
		return nil, true
	}
	// session_id はディレクトリ名として使う。パスを含む値では置き場を決められない。
	if filepath.Base(sessionID) != sessionID || sessionID == "." || sessionID == ".." {
		return nil, false
	}
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, false
		}
		configDir = filepath.Join(home, ".claude")
	}
	dir := filepath.Join(configDir, "session-env", sessionID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, true
	}
	if err != nil {
		return nil, false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) // #nosec G304 G703 -- 設定ディレクトリ配下の、検証済みの session_id のディレクトリ
		if err != nil {
			return nil, false
		}
		for _, m := range envAssignPattern.FindAllSubmatch(data, -1) {
			names = append(names, string(m[1]))
		}
	}
	return names, true
}
