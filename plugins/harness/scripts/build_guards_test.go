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

// guards は build-guards.sh が建てるバイナリ。
var guards = []string{"guard-bash", "guard-home-dir"}

// guardsScript は build-guards.sh の絶対パスを返す。
func guardsScript(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("build-guards.sh")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// runGuards は build-guards.sh を env で実行する。
func runGuards(t *testing.T, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runScript(t, guardsScript(t), env, args...)
}

// copyPluginForGuards は build-guards.sh が読む入力 (script、go.mod、go.sum、cmd、internal) を
// 一時ディレクトリへ写し、写した script のパスを返す。
func copyPluginForGuards(t *testing.T) (root, scriptPath string) {
	t.Helper()
	src, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	for _, rel := range []string{"scripts/build-guards.sh", "go.mod", "go.sum"} {
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
	return root, filepath.Join(root, "scripts", "build-guards.sh")
}

func readStamps(t *testing.T, data string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, g := range guards {
		b, err := os.ReadFile(filepath.Join(data, "bin", g+".stamp"))
		if err != nil {
			t.Fatal(err)
		}
		m[g] = string(b)
	}
	return m
}

// go が無いと guard が無いまま Bash などが deny され続けるので、SessionStart の会話に 1 行で知らせる。
func TestGuards_NoGo_IfStale_ReportsOneLine(t *testing.T) {
	data := t.TempDir()
	stdout, stderr, code := runGuards(t, baseEnv(pathWithoutGo(t), data), "--if-stale")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if strings.Count(strings.TrimRight(stdout, "\n"), "\n") != 0 || !strings.Contains(stdout, "go") {
		t.Errorf("stdout = %q, want one line that mentions go", stdout)
	}
	for _, g := range guards {
		if _, err := os.Stat(filepath.Join(data, "bin", g)); err == nil {
			t.Errorf("%s exists, want none", g)
		}
	}
}

func TestGuards_NoGo_Explicit_Fails(t *testing.T) {
	data := t.TempDir()
	_, stderr, code := runGuards(t, baseEnv(pathWithoutGo(t), data))
	if code == 0 {
		t.Fatal("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr, "go") {
		t.Errorf("stderr = %q, want the reason to mention go", stderr)
	}
}

func TestGuards_NoPluginData_Fails(t *testing.T) {
	_, stderr, code := runGuards(t, baseEnv(os.Getenv("PATH"), ""))
	if code == 0 {
		t.Fatal("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr, "CLAUDE_PLUGIN_DATA") {
		t.Errorf("stderr = %q, want it to name CLAUDE_PLUGIN_DATA", stderr)
	}
}

func TestGuards_BuildFailure_IfStale_ReportsLogAndExitsZero(t *testing.T) {
	data := t.TempDir()
	fake := t.TempDir()
	if err := os.WriteFile(filepath.Join(fake, "go"), []byte("#!/bin/sh\necho 'stub build error' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := fake + string(os.PathListSeparator) + pathWithoutGo(t)
	stdout, stderr, code := runGuards(t, baseEnv(path, data), "--if-stale")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	logPath := filepath.Join(data, "build-guards.log")
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
}

func TestGuards_BuildFailure_Explicit_Fails(t *testing.T) {
	data := t.TempDir()
	fake := t.TempDir()
	if err := os.WriteFile(filepath.Join(fake, "go"), []byte("#!/bin/sh\necho 'stub build error' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := fake + string(os.PathListSeparator) + pathWithoutGo(t)
	_, stderr, code := runGuards(t, baseEnv(path, data))
	if code == 0 {
		t.Fatal("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr, "stub build error") {
		t.Errorf("stderr = %q, want the go output", stderr)
	}
}

// 入力のどれか 1 つだけを変えても、それを使うバイナリの stamp が変わり、--if-stale が建て直す。
func TestGuards_IfStale_RebuildsWhenAnInputChanges(t *testing.T) {
	tests := []struct {
		rel     string
		changed []string
	}{
		{"go.sum", guards},
		{"go.mod", guards},
		{"internal/pathutil/pathutil.go", guards},
		{"cmd/guard-bash/main.go", []string{"guard-bash"}},
		{"cmd/guard-bash/internal/rules/rules.go", []string{"guard-bash"}},
		{"cmd/guard-home-dir/main.go", []string{"guard-home-dir"}},
	}
	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			root, scriptPath := copyPluginForGuards(t)
			data := t.TempDir()
			env := baseEnv(stubGoDir(t)+string(os.PathListSeparator)+pathWithoutGo(t), data)
			if _, stderr, code := runScript(t, scriptPath, env); code != 0 {
				t.Fatalf("first build exit = %d (stderr: %s)", code, stderr)
			}
			before := readStamps(t, data)

			appendLine(t, filepath.Join(root, tt.rel), "\n")

			if _, stderr, code := runScript(t, scriptPath, env, "--if-stale"); code != 0 {
				t.Fatalf("--if-stale exit = %d (stderr: %s)", code, stderr)
			}
			after := readStamps(t, data)
			for _, g := range guards {
				want := false
				for _, c := range tt.changed {
					want = want || c == g
				}
				if got := before[g] != after[g]; got != want {
					t.Errorf("%s stamp changed = %v, want %v", g, got, want)
				}
			}
		})
	}
}

// バイナリが新しければ、go が無くても黙って終わる (開き直すたびに警告を出さない)。
func TestGuards_NoGo_UpToDate_IsSilent(t *testing.T) {
	_, scriptPath := copyPluginForGuards(t)
	data := t.TempDir()
	env := baseEnv(stubGoDir(t)+string(os.PathListSeparator)+pathWithoutGo(t), data)
	if _, stderr, code := runScript(t, scriptPath, env); code != 0 {
		t.Fatalf("first build exit = %d (stderr: %s)", code, stderr)
	}
	for _, g := range guards {
		if err := os.Chmod(filepath.Join(data, "bin", g), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, code := runScript(t, scriptPath, baseEnv(pathWithoutGo(t), data), "--if-stale")
	if code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestGuards_Build_ThenIfStaleSkips(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}
	data := t.TempDir()
	env := baseEnv(os.Getenv("PATH"), data)

	stdout, stderr, code := runGuards(t, env)
	if code != 0 {
		t.Fatalf("first build exit = %d (stderr: %s)", code, stderr)
	}
	old := time.Now().Add(-time.Hour)
	for _, g := range guards {
		bin := filepath.Join(data, "bin", g)
		if !strings.Contains(stdout, bin) {
			t.Errorf("stdout = %q, want it to name %s", stdout, bin)
		}
		st, err := os.Stat(bin)
		if err != nil {
			t.Fatalf("%s not built: %v", g, err)
		}
		if st.Mode()&0o111 == 0 {
			t.Errorf("%s mode = %v, want executable", g, st.Mode())
		}
		if out, err := exec.Command(bin, "--help").CombinedOutput(); err != nil {
			t.Fatalf("%s --help: %v\n%s", g, err, out)
		}
		if err := os.Chtimes(bin, old, old); err != nil {
			t.Fatal(err)
		}
	}

	stdout, stderr, code = runGuards(t, env, "--if-stale")
	if code != 0 {
		t.Fatalf("--if-stale exit = %d (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty when up to date", stdout)
	}
	for _, g := range guards {
		st, err := os.Stat(filepath.Join(data, "bin", g))
		if err != nil {
			t.Fatal(err)
		}
		if st.ModTime().After(old.Add(time.Second)) {
			t.Errorf("%s was rebuilt although sources did not change", g)
		}
	}
}
