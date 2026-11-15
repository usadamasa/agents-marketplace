package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// script は build-repo-scan.sh の絶対パスを返す。
func script(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("build-repo-scan.sh")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// scriptTools は build-repo-scan.sh が go のほかに使うコマンド。
var scriptTools = []string{"bash", "cat", "chmod", "dirname", "git", "mkdir", "mktemp", "mv", "rm"}

// pathWithoutGo は scriptTools だけを symlink で並べたディレクトリを返す。go は入れない。
// 元の PATH から go のディレクトリを外すと、同じディレクトリにある git なども消えるため。
func pathWithoutGo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, tool := range scriptTools {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("%s not in PATH: %v", tool, err)
		}
		if err := os.Symlink(p, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// run は build-repo-scan.sh を env で実行し、stdout と stderr と終了コードを返す。
func run(t *testing.T, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(script(t), args...)
	cmd.Env = env
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run: %v", err)
		}
		code = ee.ExitCode()
	}
	return out.String(), errb.String(), code
}

// baseEnv は HOME と go のキャッシュを引き継ぎ、PATH と CLAUDE_PLUGIN_DATA を差し替えた環境を返す。
func baseEnv(path, data string) []string {
	env := []string{"PATH=" + path, "CLAUDE_PLUGIN_DATA=" + data}
	for _, k := range []string{"HOME", "TMPDIR", "GOCACHE", "GOMODCACHE", "GOPATH", "GOPROXY", "GOFLAGS", "XDG_CACHE_HOME"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func TestNoGo_IfStale_IsSilent(t *testing.T) {
	data := t.TempDir()
	stdout, stderr, code := run(t, baseEnv(pathWithoutGo(t), data), "--if-stale")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (SessionStart の stdout は会話に入る)", stdout)
	}
	if _, err := os.Stat(filepath.Join(data, "bin", "repo-scan")); err == nil {
		t.Error("binary exists, want none")
	}
}

func TestNoGo_Explicit_Fails(t *testing.T) {
	data := t.TempDir()
	_, stderr, code := run(t, baseEnv(pathWithoutGo(t), data))
	if code == 0 {
		t.Fatal("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr, "go") {
		t.Errorf("stderr = %q, want the reason to mention go", stderr)
	}
}

func TestNoPluginData_Fails(t *testing.T) {
	_, stderr, code := run(t, baseEnv(os.Getenv("PATH"), ""))
	if code == 0 {
		t.Fatal("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr, "CLAUDE_PLUGIN_DATA") {
		t.Errorf("stderr = %q, want it to name CLAUDE_PLUGIN_DATA", stderr)
	}
}

func TestBuildFailure_IfStale_ReportsLogAndExitsZero(t *testing.T) {
	data := t.TempDir()
	fake := t.TempDir()
	goStub := filepath.Join(fake, "go")
	if err := os.WriteFile(goStub, []byte("#!/bin/sh\necho 'stub build error' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := fake + string(os.PathListSeparator) + pathWithoutGo(t)
	stdout, stderr, code := run(t, baseEnv(path, data), "--if-stale")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	logPath := filepath.Join(data, "build-repo-scan.log")
	if !strings.Contains(stdout, logPath) {
		t.Errorf("stdout = %q, want it to point at %s", stdout, logPath)
	}
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(b), "stub build error") {
		t.Errorf("log = %q, want the go output", b)
	}
	if _, err := os.Stat(filepath.Join(data, "bin", "repo-scan")); err == nil {
		t.Error("binary exists, want none")
	}
}

func TestIfStale_BuildsIntoEmptyDataSilently(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}
	data := t.TempDir()
	stdout, stderr, code := run(t, baseEnv(os.Getenv("PATH"), data), "--if-stale")
	if code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (SessionStart の stdout は会話に入る)", stdout)
	}
	if _, err := os.Stat(filepath.Join(data, "bin", "repo-scan")); err != nil {
		t.Errorf("binary not built: %v", err)
	}
}

func TestIfStale_RebuildsWhenStampDiffers(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}
	data := t.TempDir()
	env := baseEnv(os.Getenv("PATH"), data)
	if _, stderr, code := run(t, env); code != 0 {
		t.Fatalf("first build exit = %d (stderr: %s)", code, stderr)
	}
	bin := filepath.Join(data, "bin", "repo-scan")
	stamp := filepath.Join(data, "bin", "repo-scan.stamp")
	if err := os.WriteFile(stamp, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(bin, old, old); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := run(t, env, "--if-stale"); code != 0 {
		t.Fatalf("--if-stale exit = %d (stderr: %s)", code, stderr)
	}
	st, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().After(old.Add(time.Second)) {
		t.Error("binary was not rebuilt although the stamp differed")
	}
	b, err := os.ReadFile(stamp)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "stale\n" {
		t.Error("stamp was not rewritten")
	}
}

func TestBuild_ThenIfStaleSkips(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}
	data := t.TempDir()
	env := baseEnv(os.Getenv("PATH"), data)

	_, stderr, code := run(t, env)
	if code != 0 {
		t.Fatalf("first build exit = %d (stderr: %s)", code, stderr)
	}
	bin := filepath.Join(data, "bin", "repo-scan")
	st, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("binary not built: %v", err)
	}
	if st.Mode()&0o111 == 0 {
		t.Errorf("binary mode = %v, want executable", st.Mode())
	}
	if out, err := exec.Command(bin, "--help").CombinedOutput(); err != nil {
		t.Fatalf("built binary --help: %v\n%s", err, out)
	}

	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(bin, old, old); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := run(t, env, "--if-stale")
	if code != 0 {
		t.Fatalf("--if-stale exit = %d (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty when up to date", stdout)
	}
	st2, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}
	if st2.ModTime().After(old.Add(time.Second)) {
		t.Errorf("binary was rebuilt although sources did not change")
	}
}
