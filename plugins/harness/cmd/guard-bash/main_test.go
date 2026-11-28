package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/ghauthor"
)

// excludedCommands の照合ガードは端末の ~/.claude/settings.json を読む。ここの結合テストは
// 別の段 (rules、opt-in) を見るもので、端末の設定が混ざると手元と CI で結果が変わる。
// 存在しないパスを指して「excludedCommands 無し」に固定する。照合ガード自体のテストは
// internal/bashguard、バイナリへの配線は tests/guard-bash.bats が持つ。
func TestMain(m *testing.M) {
	if err := os.Setenv("CLAUDE_SETTINGS_JSON", filepath.Join(os.TempDir(), "guard-bash-test-no-settings.json")); err != nil {
		panic(err)
	}
	// opt-in の対象リポジトリの判定は GH_REPO / GH_HOST を読む。端末で立っていても結果を変えない。
	for _, k := range []string{"GH_REPO", "GH_HOST"} {
		if err := os.Unsetenv(k); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}

// stubAuthorship は PR / issue の作成者照会を差し替える。差し替えないと判定のたびに
// 実際の gh が走り、テストがネットワークと手元の認証に依存する。
// authors のキーは "<kind> <番号>" (例 "pr 5")、未登録は「照会できない」扱い。
func stubAuthorship(t *testing.T, self string, authors map[string]string) {
	t.Helper()
	t.Cleanup(ghauthor.SetForTest(ghauthor.Stub{Self: self, Authors: authors}))
}

func TestRun(t *testing.T) {
	// opt-in は環境変数に依存する。開発者の環境で立っていても既定の判定を検証できるよう明示的に外す。
	t.Setenv(ghPRWritesKey, "")
	t.Setenv(ghIssueWritesKey, "")
	stubAuthorship(t, "octocat", map[string]string{"pr 5": "octocat"})

	tests := []struct {
		name string
		in   string
		// wantContains が空なら「出力なし = 通過」を期待する
		wantContains []string
	}{
		{
			name: "Bash 以外のツールは通過",
			in:   `{"tool_name":"Read","tool_input":{"file_path":"/tmp/test"}}`,
		},
		{
			name: "command が空でも通過",
			in:   `{"tool_name":"Bash","tool_input":{}}`,
		},
		{
			name:         "deny は理由付きの JSON を返す",
			in:           `{"tool_name":"Bash","tool_input":{"command":"gcloud auth login"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "gcloud の危険な操作"},
		},
		{
			name:         "ask は理由付きの JSON を返す",
			in:           `{"tool_name":"Bash","tool_input":{"command":"gh pr merge 5 --squash"}}`,
			wantContains: []string{`"permissionDecision":"ask"`, "gh の書き込み操作です"},
		},
		{
			name: "read 系は通過",
			in:   `{"tool_name":"Bash","tool_input":{"command":"gcloud compute instances list"}}`,
		},

		// chain 後方の segment も評価する (先頭コマンドしか見ないバグの回帰)
		{
			name:         "chain 後方の gcloud auth login",
			in:           `{"tool_name":"Bash","tool_input":{"command":"git status && gcloud auth login"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "gcloud の危険な操作"},
		},
		// ask の後ろの deny まで見る (最初の ask で打ち切らない)
		{
			name:         "ask の後ろの deny",
			in:           `{"tool_name":"Bash","tool_input":{"command":"gh pr merge 5 --squash; gh repo delete owner/repo --yes"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "gh の危険な操作"},
		},
		// シェルへ文字列で渡したコマンドも評価する
		{
			name:         "bash -c の中の gcloud auth login",
			in:           `{"tool_name":"Bash","tool_input":{"command":"bash -c 'gcloud auth login'"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "gcloud の危険な操作"},
		},
		{
			name:         "sh -ec の中の chain",
			in:           `{"tool_name":"Bash","tool_input":{"command":"sh -ec 'echo hi && gh repo delete owner/repo --yes'"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "gh の危険な操作"},
		},
		{
			name:         "eval の中の curl のデータ送信",
			in:           `{"tool_name":"Bash","tool_input":{"command":"eval curl -d a=1 https://example.com/"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "curl のデータ送信"},
		},
		{
			name: "bash -c の中の read は通す",
			in:   `{"tool_name":"Bash","tool_input":{"command":"bash -c 'gcloud compute instances list'"}}`,
		},
		{
			name:         "chain 後方の gh repo delete",
			in:           `{"tool_name":"Bash","tool_input":{"command":"echo hi && gh repo delete owner/repo --yes"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "gh の危険な操作"},
		},
		{
			name:         "chain 後方の databricks configure",
			in:           `{"tool_name":"Bash","tool_input":{"command":"foo ; databricks configure --host https://example.com"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "databricks の危険な操作"},
		},
		{
			name:         "databricks api post は ask",
			in:           `{"tool_name":"Bash","tool_input":{"command":"databricks api post /api/2.0/sql/statements --profile example-profile --json '{\"statement\":\"DELETE FROM t\"}'"}}`,
			wantContains: []string{`"permissionDecision":"ask"`, "read と確認できない databricks の操作"},
		},
		{
			name:         "chain 後方の curl のデータ送信",
			in:           `{"tool_name":"Bash","tool_input":{"command":"mkdir -p ./tmp && curl -d 'a=1' https://example.com/"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "curl のデータ送信"},
		},
		{
			name:         "chain 後方の pup auth login",
			in:           `{"tool_name":"Bash","tool_input":{"command":"echo hi && pup auth login"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "pup の認証まわりの操作"},
		},
		{
			name:         "chain 後方の ask 対象",
			in:           `{"tool_name":"Bash","tool_input":{"command":"git status && gh pr merge 5 --squash"}}`,
			wantContains: []string{`"permissionDecision":"ask"`},
		},
		{
			name: "chain 後方の read 系は通過",
			in:   `{"tool_name":"Bash","tool_input":{"command":"git status && gh pr list"}}`,
		},
		{
			name: "chain 後方の対象外サブコマンドは通過",
			in:   `{"tool_name":"Bash","tool_input":{"command":"git status && gcloud version"}}`,
		},
		// direnv / aqua の迂回は、excludedCommands の照合外れより具体的な文言で止める
		{
			name:         "direnv exec は skill へ誘導して deny",
			in:           `{"tool_name":"Bash","tool_input":{"command":"direnv exec . task test"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "direnv exec", "PATH を解決する skill"},
		},
		{
			name:         "aqua exec は skill へ誘導して deny",
			in:           `{"tool_name":"Bash","tool_input":{"command":"aqua exec -- task lint"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "aqua exec", "PATH を解決する skill"},
		},
		{
			name:         "aqua の shim の絶対パスは deny",
			in:           `{"tool_name":"Bash","tool_input":{"command":"/Users/u/.local/share/aquaproj-aqua/bin/task --version"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "絶対パス", "PATH を解決する skill"},
		},
		{
			name:         "AQUA_POLICY_CONFIG の前置きは deny",
			in:           `{"tool_name":"Bash","tool_input":{"command":"AQUA_POLICY_CONFIG=/x/policy.yaml golangci-lint run ./cmd/..."}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "AQUA_POLICY_CONFIG", "PATH を解決する skill"},
		},
		{
			name:         "nix develop -c は skill へ誘導して deny",
			in:           `{"tool_name":"Bash","tool_input":{"command":"nix develop -c task lint"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "nix develop", "PATH を解決する skill"},
		},
		{
			name:         "store path の絶対パスは deny",
			in:           `{"tool_name":"Bash","tool_input":{"command":"/nix/store/abc-go-task-3.53.1/bin/task --version"}}`,
			wantContains: []string{`"permissionDecision":"deny"`, "絶対パス", "PATH を解決する skill"},
		},
		{
			name: "store を温める nix develop --command true は通過",
			in:   `{"tool_name":"Bash","tool_input":{"command":"nix develop --command true"}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			if err := run(strings.NewReader(tc.in), &out); err != nil {
				t.Fatalf("run() = %v, want nil", err)
			}
			got := out.String()
			if len(tc.wantContains) == 0 {
				if got != "" {
					t.Errorf("出力なしを期待したが %q", got)
				}
				return
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("出力に %q が含まれない: %q", want, got)
				}
			}
		})
	}
}

// opt-in が有効なプロジェクトでは gh の PR 書き込み操作が ask をスキップする。
// deny 側のルールは opt-in の影響を受けない。
func TestRunWithGHPRWritesOptIn(t *testing.T) {
	stubAuthorship(t, "octocat", map[string]string{
		"pr 5":  "octocat",
		"pr 77": "someone-else",
	})

	tests := []struct {
		name    string
		command string
		// wantContains が空なら「出力なし = 通過」を期待する
		wantContains []string
	}{
		{name: "pr merge は通過", command: "gh pr merge 5 --squash"},
		// 文面の変更は opt-in と無関係に allow で通るので、opt-in に頼る形 (label) で確かめる
		{name: "pr edit は通過", command: "gh pr edit 5 --add-label bug"},
		{name: "pr ready は通過", command: "gh pr ready 5"},
		{name: "pr close は通過", command: "gh pr close 5"},
		{name: "chain 後方の pr merge も通過", command: "git status && gh pr merge 5 --squash"},

		{
			name:         "--admin は opt-in の対象外で ask のまま",
			command:      "gh pr merge 5 --admin",
			wantContains: []string{`"permissionDecision":"ask"`},
		},
		{
			name:         "他人の PR は opt-in で覆せず deny",
			command:      "gh pr edit 77 --body changed",
			wantContains: []string{`"permissionDecision":"deny"`, "他人が作成した"},
		},
		{
			name:         "他人の PR は merge も deny",
			command:      "gh pr merge 77 --squash",
			wantContains: []string{`"permissionDecision":"deny"`},
		},
		{
			name:         "作成者を照会できない PR は ask のまま",
			command:      "gh pr edit 999 --body changed",
			wantContains: []string{`"permissionDecision":"ask"`},
		},
		{
			name:         "対象を特定できない形は ask のまま",
			command:      "gh pr edit --body changed",
			wantContains: []string{`"permissionDecision":"ask"`},
		},
		{
			// cd の後ろはどのリポジトリで動くか分からない。自分の PR 5 が
			// あっても、別リポジトリの PR 5 かもしれないので照会しない。
			name:         "cd を挟んだ後ろは照会せず ask",
			command:      "cd /other-repo && gh pr edit 5 --body changed",
			wantContains: []string{`"permissionDecision":"ask"`},
		},
		{
			name:         "opt-in の対象でない ask はそのまま",
			command:      "gh release create v1.0.0",
			wantContains: []string{`"permissionDecision":"ask"`},
		},
		{
			name:         "deny は opt-in で覆らない (repo delete)",
			command:      "gh repo delete owner/repo --yes",
			wantContains: []string{`"permissionDecision":"deny"`},
		},
		{
			name:         "deny は opt-in で覆らない (非 GET api)",
			command:      "gh api -X PATCH repos/o/r/pulls/5 -f title=x",
			wantContains: []string{`"permissionDecision":"deny"`},
		},
		{
			name:         "opt-in は gh 以外の deny にも影響しない",
			command:      "gcloud auth login",
			wantContains: []string{`"permissionDecision":"deny"`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			initRepo(t, projectDir, map[string]string{"origin": projectRemote})
			writeSettings(t, projectDir, "settings.json", enabledSettings(ghPRWritesKey, "1"))
			t.Setenv(ghPRWritesKey, "1")
			t.Setenv("CLAUDE_PROJECT_DIR", projectDir)
			t.Setenv("HOME", t.TempDir())

			in := `{"tool_name":"Bash","cwd":"` + projectDir + `","tool_input":{"command":"` + tc.command + `"}}`
			var out strings.Builder
			if err := run(strings.NewReader(in), &out); err != nil {
				t.Fatalf("run() = %v, want nil", err)
			}
			got := out.String()
			if len(tc.wantContains) == 0 {
				if got != "" {
					t.Errorf("出力なしを期待したが %q", got)
				}
				return
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("出力に %q が含まれない: %q", want, got)
				}
			}
		})
	}
}

// opt-in が有効なプロジェクトでは gh の issue 書き込み操作が ask をスキップする。
// PR 用のキーでは issue が通らないこと (キーの独立性) もここで押さえる。
func TestRunWithGHIssueWritesOptIn(t *testing.T) {
	stubAuthorship(t, "octocat", map[string]string{
		"issue 42": "octocat",
		"issue 77": "someone-else",
		"pr 5":     "octocat",
	})

	tests := []struct {
		name string
		// enabledKey は環境変数と settings に設定する opt-in キー
		enabledKey string
		command    string
		// wantContains が空なら「出力なし = 通過」を期待する
		wantContains []string
	}{
		{name: "issue create は通過", enabledKey: ghIssueWritesKey, command: "gh issue create --title t --body b"},
		{name: "issue edit は通過", enabledKey: ghIssueWritesKey, command: "gh issue edit 42 --add-label bug"},
		{name: "issue close は通過", enabledKey: ghIssueWritesKey, command: "gh issue close 42"},
		{name: "issue reopen は通過", enabledKey: ghIssueWritesKey, command: "gh issue reopen 42"},
		{name: "chain 後方の issue close も通過", enabledKey: ghIssueWritesKey, command: "git status && gh issue close 42"},

		{
			name:         "issue delete は opt-in の対象外で ask のまま",
			enabledKey:   ghIssueWritesKey,
			command:      "gh issue delete 42 --yes",
			wantContains: []string{`"permissionDecision":"ask"`},
		},
		{
			name:         "issue transfer は opt-in の対象外で ask のまま",
			enabledKey:   ghIssueWritesKey,
			command:      "gh issue transfer 42 owner/other",
			wantContains: []string{`"permissionDecision":"ask"`},
		},
		{
			name:         "他人の issue は opt-in で覆せず deny",
			enabledKey:   ghIssueWritesKey,
			command:      "gh issue edit 77 --body changed",
			wantContains: []string{`"permissionDecision":"deny"`, "他人が作成した"},
		},
		{
			name:         "他人の issue は delete も deny",
			enabledKey:   ghIssueWritesKey,
			command:      "gh issue delete 77 --yes",
			wantContains: []string{`"permissionDecision":"deny"`},
		},
		{
			name:         "作成者を照会できない issue は ask のまま",
			enabledKey:   ghIssueWritesKey,
			command:      "gh issue close 999",
			wantContains: []string{`"permissionDecision":"ask"`},
		},
		{
			name:         "issue キーでは PR の書き込みは通らない",
			enabledKey:   ghIssueWritesKey,
			command:      "gh pr merge 5 --squash",
			wantContains: []string{`"permissionDecision":"ask"`},
		},
		{
			name:         "PR キーでは issue の書き込みは通らない",
			enabledKey:   ghPRWritesKey,
			command:      "gh issue close 42",
			wantContains: []string{`"permissionDecision":"ask"`},
		},
		{
			name:         "deny は opt-in で覆らない (非 GET api)",
			enabledKey:   ghIssueWritesKey,
			command:      "gh api -X POST repos/o/r/issues -f title=t",
			wantContains: []string{`"permissionDecision":"deny"`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			initRepo(t, projectDir, map[string]string{"origin": projectRemote})
			writeSettings(t, projectDir, "settings.json", enabledSettings(tc.enabledKey, "1"))
			t.Setenv(ghPRWritesKey, "")
			t.Setenv(ghIssueWritesKey, "")
			t.Setenv(tc.enabledKey, "1")
			t.Setenv("CLAUDE_PROJECT_DIR", projectDir)
			t.Setenv("HOME", t.TempDir())

			in := `{"tool_name":"Bash","cwd":"` + projectDir + `","tool_input":{"command":"` + tc.command + `"}}`
			var out strings.Builder
			if err := run(strings.NewReader(in), &out); err != nil {
				t.Fatalf("run() = %v, want nil", err)
			}
			got := out.String()
			if len(tc.wantContains) == 0 {
				if got != "" {
					t.Errorf("出力なしを期待したが %q", got)
				}
				return
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("出力に %q が含まれない: %q", want, got)
				}
			}
		})
	}
}

// opt-in が成立しないプロジェクトでは gh の PR 書き込み操作は ask のまま。
func TestRunWithoutValidOptIn(t *testing.T) {
	stubAuthorship(t, "octocat", map[string]string{"pr 5": "octocat"})

	tests := []struct {
		name string
		// setup は環境と settings を用意する。projectDir を返す。
		setup func(t *testing.T) string
	}{
		{
			name: "環境変数だけでプロジェクトに定義が無い",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				t.Setenv(ghPRWritesKey, "1")
				t.Setenv("HOME", t.TempDir())
				return dir
			},
		},
		{
			name: "user スコープにだけ定義がある",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				home := t.TempDir()
				writeSettings(t, home, "settings.json", enabledSettings(ghPRWritesKey, "1"))
				t.Setenv(ghPRWritesKey, "1")
				t.Setenv("HOME", home)
				return dir
			},
		},
		{
			name: "user スコープとプロジェクト両方に定義がある",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				home := t.TempDir()
				writeSettings(t, dir, "settings.json", enabledSettings(ghPRWritesKey, "1"))
				writeSettings(t, home, "settings.json", enabledSettings(ghPRWritesKey, "1"))
				t.Setenv(ghPRWritesKey, "1")
				t.Setenv("HOME", home)
				return dir
			},
		},
		{
			name: "プロジェクトに定義があるが環境変数が無い",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				writeSettings(t, dir, "settings.json", enabledSettings(ghPRWritesKey, "1"))
				t.Setenv(ghPRWritesKey, "")
				t.Setenv("HOME", t.TempDir())
				return dir
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := tc.setup(t)
			t.Setenv("CLAUDE_PROJECT_DIR", projectDir)

			in := `{"tool_name":"Bash","cwd":"` + projectDir + `","tool_input":{"command":"gh pr merge 5 --squash"}}`
			var out strings.Builder
			if err := run(strings.NewReader(in), &out); err != nil {
				t.Fatalf("run() = %v, want nil", err)
			}
			if got := out.String(); !strings.Contains(got, `"permissionDecision":"ask"`) {
				t.Errorf("ask を期待したが %q", got)
			}
		})
	}
}

// 入力を読めないときに素通しすると guard が丸ごと無効になる。
// エラー終了は Claude Code が non-blocking として扱うため、ask を出して判断を渡す。
func TestRunInvalidJSON(t *testing.T) {
	var out strings.Builder
	if err := run(strings.NewReader("not json"), &out); err != nil {
		t.Fatalf("エラーではなく ask を期待したが %v", err)
	}
	if got := out.String(); !strings.Contains(got, `"permissionDecision":"ask"`) {
		t.Errorf("ask を期待したが %q", got)
	}
}

// 段を分けてあるのは、後段の ask より前段の deny を優先させるため。
// 1 つのループにまとめると、先に現れた segment の ask で判定が確定してしまう。
func TestRunDenyBeatsAsk(t *testing.T) {
	t.Chdir(t.TempDir())
	var out strings.Builder
	input := `{"tool_name":"Bash","tool_input":{"command":"gh pr merge 5 --squash && git config user.name x"}}`
	if err := run(strings.NewReader(input), &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := out.String(); !strings.Contains(got, `"permissionDecision":"deny"`) {
		t.Errorf("後段の ask ではなく前段の deny を期待したが %q", got)
	}
}

// gh api graphql の query=@<path> は単独のコマンドのときだけ読む。複合コマンドでは
// 前段がファイルを書き換えられるので、中身が Project の mutation でも deny のまま残す。
func TestRunGraphQLQueryFileOnlyStandalone(t *testing.T) {
	dir := t.TempDir()
	q := `mutation { addProjectV2ItemById(input: {projectId: \"P\", contentId: \"I\"}) { item { id } } }`
	if err := os.WriteFile(filepath.Join(dir, "q.graphql"), []byte(strings.ReplaceAll(q, `\"`, `"`)), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		command  string
		wantDeny bool
	}{
		{"単独", "gh api graphql -F query=@q.graphql", false},
		{"複合", "gh pr view 1; gh api graphql -F query=@q.graphql", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			in := `{"tool_name":"Bash","cwd":"` + dir + `","tool_input":{"command":"` + tc.command + `"}}`
			if err := run(strings.NewReader(in), &out); err != nil {
				t.Fatalf("run: %v", err)
			}
			if got := strings.Contains(out.String(), `"permissionDecision":"deny"`); got != tc.wantDeny {
				t.Errorf("deny = %v, want %v (出力: %q)", got, tc.wantDeny, out.String())
			}
		})
	}
}

// Go のモジュール取得で照合を外す形は、照合外れの deny より先に、迂回を名指しして deny する。
func TestRunGoModuleTrust(t *testing.T) {
	t.Chdir(t.TempDir())
	var out strings.Builder
	input := `{"tool_name":"Bash","tool_input":{"command":"cd cmd && GOPROXY=direct go get example.com/m"}}`
	if err := run(strings.NewReader(input), &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"permissionDecision":"deny"`) || !strings.Contains(got, "GOPROXY") {
		t.Errorf("GOPROXY を名指しした deny を期待したが %q", got)
	}
}

// git の --no-verify は、シェルへ文字列で渡す形でも hook の入口から deny に届く。
func TestRunGitNoVerify(t *testing.T) {
	t.Chdir(t.TempDir())
	var out strings.Builder
	input := `{"tool_name":"Bash","tool_input":{"command":"bash -c 'git commit --no-verify -m x'"}}`
	if err := run(strings.NewReader(input), &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"permissionDecision":"deny"`) || !strings.Contains(got, "--no-verify") {
		t.Errorf("--no-verify を名指しした deny を期待したが %q", got)
	}
}

// bash 自身も受け付けない構文は、guard が読めないまま通すより確認を挟む。
func TestRunUnparsableCommand(t *testing.T) {
	var out strings.Builder
	input := `{"tool_name":"Bash","tool_input":{"command":"echo 'unclosed"}}`
	if err := run(strings.NewReader(input), &out); err != nil {
		t.Fatalf("エラーではなく ask を期待したが %v", err)
	}
	if got := out.String(); !strings.Contains(got, `"permissionDecision":"ask"`) {
		t.Errorf("ask を期待したが %q", got)
	}
}
