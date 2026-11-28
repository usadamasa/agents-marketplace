package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Run("excludedCommands を読む", func(t *testing.T) {
		tmp := t.TempDir()
		f := filepath.Join(tmp, "settings.json")
		content := `{
			"permissions": {"allow": ["Bash(git status:*)"]},
			"sandbox": {"excludedCommands": ["git *", "gh *"]}
		}`
		if err := os.WriteFile(f, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		s, err := Load(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := s.Sandbox.ExcludedCommands
		if len(got) != 2 || got[0] != "git *" || got[1] != "gh *" {
			t.Errorf("ExcludedCommands: got %v, want [git * gh *]", got)
		}
	})

	t.Run("存在しないファイルはエラーを返す", func(t *testing.T) {
		_, err := Load("/nonexistent/settings.json")
		if err == nil {
			t.Error("エラーが返されるべき")
		}
	})

	t.Run("不正なJSONはエラーを返す", func(t *testing.T) {
		tmp := t.TempDir()
		f := filepath.Join(tmp, "settings.json")
		if err := os.WriteFile(f, []byte("{invalid"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := Load(f)
		if err == nil {
			t.Error("エラーが返されるべき")
		}
	})

	t.Run("フィールドが省略されても正常にパースする", func(t *testing.T) {
		tmp := t.TempDir()
		f := filepath.Join(tmp, "settings.json")
		if err := os.WriteFile(f, []byte(`{}`), 0644); err != nil {
			t.Fatal(err)
		}

		s, err := Load(f)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.Sandbox.ExcludedCommands != nil {
			t.Errorf("ExcludedCommands should be nil, got %v", s.Sandbox.ExcludedCommands)
		}
	})
}
