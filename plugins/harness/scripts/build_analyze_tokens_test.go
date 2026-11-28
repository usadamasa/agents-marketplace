package scripts_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// script は build-analyze-tokens.sh の絶対パスを返す。
func script(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("build-analyze-tokens.sh")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// scriptTools は build-analyze-tokens.sh が go のほかに使うコマンド。
var scriptTools = []string{"bash", "cat", "chmod", "dirname", "find", "git", "mkdir", "mktemp", "mv", "rm", "sort", "xargs"}

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

// stubGoDir は、go build の -o の行き先へ空のファイルを置くだけの go を入れたディレクトリを返す。
func stubGoDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	stub := "#!/bin/sh\nout=\nwhile [ $# -gt 0 ]; do\n  if [ \"$1\" = -o ]; then out=$2; fi\n  shift\ndone\n: > \"$out\"\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runScript は path の script を env で実行し、stdout と stderr と終了コードを返す。
func runScript(t *testing.T, path string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(path, args...)
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

// run は build-analyze-tokens.sh を env で実行する。
func run(t *testing.T, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runScript(t, script(t), env, args...)
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

// copyPlugin は script が読む入力 (scripts、go.mod、cmd、internal) を一時ディレクトリへ写し、写した script のパスを返す。
func copyPlugin(t *testing.T) (root, scriptPath string) {
	t.Helper()
	src, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	for _, rel := range []string{"scripts/build-analyze-tokens.sh", "go.mod"} {
		copyFile(t, filepath.Join(src, rel), filepath.Join(root, rel))
	}
	for _, dir := range []string{"cmd", "internal"} {
		err = filepath.WalkDir(filepath.Join(src, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(src, p)
			if err != nil {
				return err
			}
			copyFile(t, p, filepath.Join(root, rel))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return root, filepath.Join(root, "scripts", "build-analyze-tokens.sh")
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, st.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
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
	if _, err := os.Stat(filepath.Join(data, "bin", "analyze-tokens")); err == nil {
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
	logPath := filepath.Join(data, "build-analyze-tokens.log")
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
	if _, err := os.Stat(filepath.Join(data, "bin", "analyze-tokens")); err == nil {
		t.Error("binary exists, want none")
	}
}

// 入力のどれか 1 つだけを変えても stamp が変わり、--if-stale が建て直すことを見る。
// 入力には、埋め込みの価格表 (*.json) と、cmd の外にある internal/ の下を含む。
func TestIfStale_RebuildsWhenAnInputChanges(t *testing.T) {
	inputs := []string{
		"internal/jsonlscan/jsonlscan.go",
		"cmd/analyze-tokens/internal/pricing/pricing.json",
		"cmd/analyze-tokens/internal/transcript/transcript.go",
	}
	for _, rel := range inputs {
		t.Run(rel, func(t *testing.T) {
			root, scriptPath := copyPlugin(t)
			data := t.TempDir()
			env := baseEnv(stubGoDir(t)+string(os.PathListSeparator)+pathWithoutGo(t), data)
			if _, stderr, code := runScript(t, scriptPath, env); code != 0 {
				t.Fatalf("first build exit = %d (stderr: %s)", code, stderr)
			}
			stamp := filepath.Join(data, "bin", "analyze-tokens.stamp")
			before, err := os.ReadFile(stamp)
			if err != nil {
				t.Fatal(err)
			}

			appendLine(t, filepath.Join(root, rel), "\n")

			if _, stderr, code := runScript(t, scriptPath, env, "--if-stale"); code != 0 {
				t.Fatalf("--if-stale exit = %d (stderr: %s)", code, stderr)
			}
			after, err := os.ReadFile(stamp)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) == string(before) {
				t.Errorf("stamp did not change although %s changed", rel)
			}
		})
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
	if _, err := os.Stat(filepath.Join(data, "bin", "analyze-tokens")); err != nil {
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
	bin := filepath.Join(data, "bin", "analyze-tokens")
	stamp := filepath.Join(data, "bin", "analyze-tokens.stamp")
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
	bin := filepath.Join(data, "bin", "analyze-tokens")
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
