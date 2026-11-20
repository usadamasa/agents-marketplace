package main

import (
	"os"
	"os/exec"
	"strings"
)

// gitCmd は dir を作業ディレクトリにした git の *exec.Cmd を返す。
// 対象は -C ではなく作業ディレクトリで渡す。認証の入力を待って止まらないよう、端末からの入力は切る。
func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...) // #nosec G204 -- args は本パッケージ内の固定サブコマンドだけ
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd
}

// git は dir で git コマンドを実行し、trim 済み stdout を返す。
func git(dir string, args ...string) (string, error) {
	out, err := gitCmd(dir, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// gitRun は dir で git コマンドを実行し、trim 済みの stdout と stderr をまとめて返す。失敗の理由を報告に載せる用途。
func gitRun(dir string, args ...string) (string, error) {
	out, err := gitCmd(dir, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// gitOK は git コマンドの成否 (exit 0 か) だけを返す。
func gitOK(dir string, args ...string) bool {
	return gitCmd(dir, args...).Run() == nil
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
