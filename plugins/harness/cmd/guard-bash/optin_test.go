package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSettings は <dir>/.claude/<name> に settings JSON を書く。
// body をそのまま書くので、壊れた JSON も渡せる。
func writeSettings(t *testing.T, dir, name, body string) {
	t.Helper()
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// enabledSettings は opt-in キー key を value で定義した settings JSON を返す。
func enabledSettings(key, value string) string {
	return `{"env":{"` + key + `":"` + value + `"},"model":"opus"}`
}

func TestGHPRWritesOptIn(t *testing.T) {
	tests := []struct {
		name string
		env  string
		// user / project / local は書き込む settings の中身。空文字ならファイルを作らない。
		user    string
		project string
		local   string
		// noProjectDir が true なら projectDir に空文字を渡す
		noProjectDir bool
		wantEnabled  bool
		// wantReason が空なら Reason も空 (無音) を期待する
		wantReason string
	}{
		{
			name:    "env 未設定なら project に定義があっても無効 (無音)",
			env:     "",
			project: enabledSettings(ghPRWritesKey, "1"),
		},
		{
			name:    "env が falsy なら無効 (無音)",
			env:     "0",
			project: enabledSettings(ghPRWritesKey, "1"),
		},
		{
			name:        "project settings.json の定義で有効",
			env:         "1",
			project:     enabledSettings(ghPRWritesKey, "1"),
			wantEnabled: true,
			wantReason:  "settings.json",
		},
		{
			name:        "project settings.local.json の定義で有効",
			env:         "1",
			local:       enabledSettings(ghPRWritesKey, "1"),
			wantEnabled: true,
			wantReason:  "settings.local.json",
		},
		{
			name:        "env と settings の値が true でも有効",
			env:         "true",
			project:     enabledSettings(ghPRWritesKey, "true"),
			wantEnabled: true,
			wantReason:  "settings.json",
		},
		{
			name:        "値の大文字小文字・前後空白は無視する",
			env:         " TRUE ",
			project:     enabledSettings(ghPRWritesKey, "True"),
			wantEnabled: true,
			wantReason:  "settings.json",
		},
		{
			name:       "project に定義が無ければ無効",
			env:        "1",
			wantReason: "プロジェクトスコープの settings",
		},
		{
			name:       "project の定義が falsy なら無効",
			env:        "1",
			project:    enabledSettings(ghPRWritesKey, "0"),
			wantReason: "プロジェクトスコープの settings",
		},
		{
			name:       "user スコープに定義があれば project にもあっても無効",
			env:        "1",
			user:       enabledSettings(ghPRWritesKey, "1"),
			project:    enabledSettings(ghPRWritesKey, "1"),
			wantReason: "user スコープ",
		},
		{
			name:       "壊れた JSON は無効",
			env:        "1",
			project:    `{"env":{`,
			wantReason: "プロジェクトスコープの settings",
		},
		{
			name:       "env の値が文字列でない settings は無効",
			env:        "1",
			project:    `{"env":{"` + ghPRWritesKey + `":true}}`,
			wantReason: "プロジェクトスコープの settings",
		},
		{
			name:         "projectDir を解決できなければ無効",
			env:          "1",
			noProjectDir: true,
			wantReason:   "プロジェクトディレクトリ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(ghPRWritesKey, tc.env)

			projectDir := t.TempDir()
			homeDir := t.TempDir()
			for _, f := range []struct{ dir, name, body string }{
				{homeDir, "settings.json", tc.user},
				{projectDir, "settings.json", tc.project},
				{projectDir, "settings.local.json", tc.local},
			} {
				if f.body != "" {
					writeSettings(t, f.dir, f.name, f.body)
				}
			}
			if tc.noProjectDir {
				projectDir = ""
			}

			got := ghWritesOptIn(ghPRWritesKey, projectDir, homeDir)
			if got.Enabled != tc.wantEnabled {
				t.Errorf("Enabled = %v, want %v (Reason: %q)", got.Enabled, tc.wantEnabled, got.Reason)
			}
			if tc.wantReason == "" {
				if got.Reason != "" {
					t.Errorf("Reason = %q, want 空", got.Reason)
				}
				return
			}
			if !strings.Contains(got.Reason, tc.wantReason) {
				t.Errorf("Reason = %q, want %q を含む", got.Reason, tc.wantReason)
			}
		})
	}
}

// opt-in キーは PR 用と issue 用で独立して判定する。片方の定義がもう片方を有効にしない。
func TestGHWritesOptInKeysAreIndependent(t *testing.T) {
	tests := []struct {
		name string
		// defined は settings に書くキー
		defined string
		// checked は判定するキー
		checked     string
		wantEnabled bool
	}{
		{"issue キーの定義で issue が有効", ghIssueWritesKey, ghIssueWritesKey, true},
		{"issue キーの定義では PR は無効", ghIssueWritesKey, ghPRWritesKey, false},
		{"PR キーの定義では issue は無効", ghPRWritesKey, ghIssueWritesKey, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(ghPRWritesKey, "1")
			t.Setenv(ghIssueWritesKey, "1")

			projectDir := t.TempDir()
			writeSettings(t, projectDir, "settings.json", enabledSettings(tc.defined, "1"))

			got := ghWritesOptIn(tc.checked, projectDir, t.TempDir())
			if got.Enabled != tc.wantEnabled {
				t.Errorf("Enabled = %v, want %v (Reason: %q)", got.Enabled, tc.wantEnabled, got.Reason)
			}
		})
	}
}

// projectDir は CLAUDE_PROJECT_DIR を優先し、無ければ PreToolUse JSON の cwd を使う。
func TestResolveProjectDir(t *testing.T) {
	tests := []struct {
		name    string
		envDir  string
		inputCw string
		want    string
	}{
		{"CLAUDE_PROJECT_DIR を優先", "/env/dir", "/cwd/dir", "/env/dir"},
		{"未設定なら cwd", "", "/cwd/dir", "/cwd/dir"},
		{"どちらも無ければ空", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLAUDE_PROJECT_DIR", tc.envDir)
			if got := resolveProjectDir(tc.inputCw); got != tc.want {
				t.Errorf("resolveProjectDir(%q) = %q, want %q", tc.inputCw, got, tc.want)
			}
		})
	}
}
