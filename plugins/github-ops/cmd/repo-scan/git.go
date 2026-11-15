package main

import (
	"os/exec"
	"strings"
)

// git は dir で git コマンドを実行し、trim 済み stdout を返す。読み取り専用の用途のみ。
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- args は本パッケージ内の固定サブコマンドだけ
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// gitOK は git コマンドの成否 (exit 0 か) だけを返す。
func gitOK(dir string, args ...string) bool {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- args は本パッケージ内の固定サブコマンドだけ
	return cmd.Run() == nil
}

// ghqListPaths は `ghq list -p` の出力をパスのスライスで返す。
func ghqListPaths() ([]string, error) {
	out, err := exec.Command("ghq", "list", "-p").Output()
	if err != nil {
		return nil, err
	}
	var paths []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

// ghqRoot は `ghq root` の出力を返す。
func ghqRoot() (string, error) {
	out, err := exec.Command("ghq", "root").Output()
	return strings.TrimSpace(string(out)), err
}
