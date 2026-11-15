package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitT は t.TempDir 内で git を実行する。失敗したら fatal。
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// writeFile はリポ内にファイルを書いて add+commit する。name はサブディレクトリを含んでよい。
func writeCommit(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-m", msg)
}

// newRepo は main ブランチと初期コミットを持つ一時リポを作って返す。
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitT(t, dir, "init", "-b", "main")
	writeCommit(t, dir, "f.txt", "a\n", "init")
	return dir
}
