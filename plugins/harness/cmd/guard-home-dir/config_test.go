package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// envOf は getenv の代わりに渡す、map を引くだけの関数を返す。
func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestConfigPath(t *testing.T) {
	home := "/home/tester"
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"XDG_CONFIG_HOME が無ければ ~/.config の下", nil, "/home/tester/.config/claude-harness/guard-home-dir.yaml"},
		{"XDG_CONFIG_HOME が絶対パスならその下", map[string]string{"XDG_CONFIG_HOME": "/xdg"}, "/xdg/claude-harness/guard-home-dir.yaml"},
		{"XDG_CONFIG_HOME が相対パスなら無視する", map[string]string{"XDG_CONFIG_HOME": "rel"}, "/home/tester/.config/claude-harness/guard-home-dir.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := configPath(home, envOf(tt.env)); got != tt.want {
				t.Errorf("configPath = %q, want %q", got, tt.want)
			}
		})
	}
}

// writeConfig は XDG_CONFIG_HOME の下に guard-home-dir.yaml を書き、その XDG_CONFIG_HOME を返す。
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	xdg := t.TempDir()
	p := filepath.Join(xdg, "claude-harness", "guard-home-dir.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return xdg
}

func TestLoadConfig_Missing(t *testing.T) {
	cfg := loadConfig("/home/tester", envOf(map[string]string{"XDG_CONFIG_HOME": t.TempDir()}))
	if cfg.state != configMissing {
		t.Fatalf("state = %v, want configMissing (err: %v)", cfg.state, cfg.err)
	}
	if len(cfg.dirs) != 0 {
		t.Errorf("dirs = %v, want none", cfg.dirs)
	}
}

func TestLoadConfig_Valid(t *testing.T) {
	xdg := writeConfig(t, "allowed_home_directories:\n  - src\n  - workspace\n  - Library/Application Support/Example\n")
	cfg := loadConfig("/home/tester", envOf(map[string]string{"XDG_CONFIG_HOME": xdg}))
	if cfg.state != configOK {
		t.Fatalf("state = %v, want configOK (err: %v)", cfg.state, cfg.err)
	}
	want := []string{"src", "workspace", "Library/Application Support/Example"}
	if strings.Join(cfg.dirs, "|") != strings.Join(want, "|") {
		t.Errorf("dirs = %v, want %v", cfg.dirs, want)
	}
	if cfg.path != filepath.Join(xdg, "claude-harness", "guard-home-dir.yaml") {
		t.Errorf("path = %q", cfg.path)
	}
}

func TestLoadConfig_EmptyListIsValid(t *testing.T) {
	xdg := writeConfig(t, "allowed_home_directories: []\n")
	cfg := loadConfig("/home/tester", envOf(map[string]string{"XDG_CONFIG_HOME": xdg}))
	if cfg.state != configOK {
		t.Fatalf("state = %v, want configOK (err: %v)", cfg.state, cfg.err)
	}
	if len(cfg.dirs) != 0 {
		t.Errorf("dirs = %v, want none", cfg.dirs)
	}
}

// 1 つでも不正な値があれば、YAML 全体を読めないものとして扱う (正しい値も使わない)。
func TestLoadConfig_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"YAML として壊れている", "allowed_home_directories: [src\n"},
		{"/ で始まる値", "allowed_home_directories:\n  - src\n  - /etc\n"},
		{"~ で始まる値", "allowed_home_directories:\n  - ~/src\n"},
		{"~ だけの値", "allowed_home_directories:\n  - \"~\"\n"},
		{".. を含む値", "allowed_home_directories:\n  - src/../Downloads\n"},
		{".. だけの値", "allowed_home_directories:\n  - ..\n"},
		{"空の値", "allowed_home_directories:\n  - \"\"\n"},
		{"ホームそのものを指す . の値", "allowed_home_directories:\n  - .\n"},
		{"ホームそのものを指す ./ の値", "allowed_home_directories:\n  - ./\n"},
		{"知らないキー", "allowed_home_dirs:\n  - src\n"},
		{"一覧でなく文字列", "allowed_home_directories: src\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			xdg := writeConfig(t, tt.content)
			cfg := loadConfig("/home/tester", envOf(map[string]string{"XDG_CONFIG_HOME": xdg}))
			if cfg.state != configInvalid {
				t.Fatalf("state = %v, want configInvalid (dirs: %v)", cfg.state, cfg.dirs)
			}
			if cfg.err == nil {
				t.Error("err = nil, want the reason")
			}
			if len(cfg.dirs) != 0 {
				t.Errorf("dirs = %v, want none", cfg.dirs)
			}
		})
	}
}

func TestLoadConfig_Unreadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root は権限に関係なく読める")
	}
	xdg := writeConfig(t, "allowed_home_directories:\n  - src\n")
	p := filepath.Join(xdg, "claude-harness", "guard-home-dir.yaml")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	cfg := loadConfig("/home/tester", envOf(map[string]string{"XDG_CONFIG_HOME": xdg}))
	if cfg.state != configInvalid {
		t.Fatalf("state = %v, want configInvalid", cfg.state)
	}
}

func TestCheckPaths_WithConfiguredDirs(t *testing.T) {
	home := "/home/tester"
	cwd := "/work/project"
	dirs := []string{"src", "Library/Application Support/Example"}
	tests := []struct {
		name   string
		path   string
		denied bool
	}{
		{"YAML に書いた置き場", home + "/src/a/main.go", false},
		{"YAML に書いた置き場そのもの", home + "/src", false},
		{"空白を含む置き場", home + "/Library/Application Support/Example/x.png", false},
		{"名前が前方一致するだけの置き場", home + "/srcx/a.go", true},
		{"~/.claude は書かなくても許す", home + "/.claude/settings.json", false},
		{"cwd は書かなくても許す", cwd + "/main.go", false},
		{"YAML に無い置き場", home + "/Downloads/a.pdf", true},
		{"ホームそのもの", home, true},
		{"ホームの外", "/tmp/a", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkPaths([]string{tt.path}, home, cwd, dirs)
			if tt.denied && got != tt.path {
				t.Errorf("checkPaths = %q, want deny of %q", got, tt.path)
			}
			if !tt.denied && got != "" {
				t.Errorf("checkPaths = %q, want pass", got)
			}
		})
	}
}

func TestDenyReason(t *testing.T) {
	const denied = "/home/tester/Downloads/a.pdf"
	const cfgPath = "/home/tester/.config/claude-harness/guard-home-dir.yaml"

	t.Run("YAML がある", func(t *testing.T) {
		r := denyReason(denied, homeConfig{path: cfgPath, state: configOK, dirs: []string{"src"}})
		for _, want := range []string{denied, cfgPath, "allowed_home_directories"} {
			if !strings.Contains(r, want) {
				t.Errorf("reason = %q, want it to contain %q", r, want)
			}
		}
	})

	t.Run("YAML が無い", func(t *testing.T) {
		r := denyReason(denied, homeConfig{path: cfgPath, state: configMissing})
		for _, want := range []string{denied, cfgPath, "allowed_home_directories:", "- src", "~/.claude"} {
			if !strings.Contains(r, want) {
				t.Errorf("reason = %q, want it to contain %q", r, want)
			}
		}
	})

	t.Run("YAML を読めない", func(t *testing.T) {
		r := denyReason(denied, homeConfig{path: cfgPath, state: configInvalid, err: os.ErrPermission})
		for _, want := range []string{denied, cfgPath, "読めない", os.ErrPermission.Error(), "allowed_home_directories:"} {
			if !strings.Contains(r, want) {
				t.Errorf("reason = %q, want it to contain %q", r, want)
			}
		}
	})
}

// 許す置き場の YAML は、YAML がその置き場を許していても、ファイルを書くツールからは書かせない。
// Claude Code が自分で許可を広げられると guard の意味が無くなる。
func TestWritesConfig(t *testing.T) {
	const cfgPath = "/home/tester/.config/claude-harness/guard-home-dir.yaml"
	tests := []struct {
		name string
		tool string
		path string
		want bool
	}{
		{"Write で YAML を書く", "Write", cfgPath, true},
		{"Edit で YAML を直す", "Edit", cfgPath, true},
		{"NotebookEdit で YAML の置き場に書く", "NotebookEdit", "/home/tester/.config/claude-harness/x.ipynb", true},
		{"Write で YAML の置き場に別のファイルを書く", "Write", "/home/tester/.config/claude-harness/other.yaml", true},
		{"Read で YAML を読む", "Read", cfgPath, false},
		{"Grep で YAML の置き場を探す", "Grep", "/home/tester/.config/claude-harness", false},
		{"Write で似た名前の置き場に書く", "Write", "/home/tester/.config/claude-harness-other/a.yaml", false},
		{"Write でほかの置き場に書く", "Write", "/home/tester/src/a.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := writesConfig(tt.tool, []string{tt.path}, cfgPath); got != tt.want {
				t.Errorf("writesConfig(%q, %q) = %v, want %v", tt.tool, tt.path, got, tt.want)
			}
		})
	}
}

func TestConfigWriteReason(t *testing.T) {
	const cfgPath = "/home/tester/.config/claude-harness/guard-home-dir.yaml"
	r := configWriteReason(cfgPath)
	for _, want := range []string{cfgPath, "ユーザー"} {
		if !strings.Contains(r, want) {
			t.Errorf("reason = %q, want it to contain %q", r, want)
		}
	}
}
