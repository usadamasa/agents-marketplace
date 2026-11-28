package sandboxexcluded

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/bashguard"
	"mvdan.cc/sh/v3/syntax"
)

// lines は複数行のコマンド文字列を組み立てる (heredoc を含む形の再現用)。
func lines(l ...string) string { return strings.Join(l, "\n") }

// parseForTest はコマンド文字列を AST へ読む。解析できない形はテストの前提が壊れている。
func parseForTest(t *testing.T, cmd string) *syntax.File {
	t.Helper()
	f, err := bashguard.Parse(cmd)
	if err != nil {
		t.Fatalf("Parse(%q): %v", cmd, err)
	}
	return f
}

// withExcluded は settings.json の代わりに excludedCommands を差し込む。
func withExcluded(t *testing.T, names ...string) {
	t.Helper()
	orig := excludedBases
	bases := make(map[string]bool, len(names))
	for _, n := range names {
		bases[n] = true
	}
	excludedBases = func() map[string]bool { return bases }
	t.Cleanup(func() { excludedBases = orig })
}

// withRegistryKeyring は環境変数 UV_KEYRING_PROVIDER の代わりに、uv が内部レジストリの
// keyring 認証を使うかを差し込む。
func withRegistryKeyring(t *testing.T, enabled bool) {
	t.Helper()
	orig := usesRegistryKeyring
	usesRegistryKeyring = func() bool { return enabled }
	t.Cleanup(func() { usesRegistryKeyring = orig })
}

// 通過・deny の線引きは Claude Code 2.1.278 で `gh api rate_limit` をプローブに実測した
// (harness:sandbox-troubleshooting skill の表)。deny 側には 2026-09-19 に実際に sandbox へ落ちた
// コマンドをそのまま置いている。
func TestSandboxExcluded(t *testing.T) {
	cwd := t.TempDir()
	other := t.TempDir()

	tests := []struct {
		name string
		cmd  string
		deny bool
	}{
		{name: "素のコマンド", cmd: "docker system info"},
		{name: "excluded 同士を ; と && でつなぐ", cmd: "gh api a; gh api b && git push -u origin x"},
		{name: "fd の複製だけのリダイレクト", cmd: "gh api rate_limit 2>&1"},
		{name: "引数の中の変数展開", cmd: `gh api "repos/$OWNER/x"`},
		{name: "cwd と同じ場所への cd", cmd: "cd " + cwd + " && gh api x"},
		{name: "クォート内の演算子・${}・改行は照合を壊さない", cmd: "git commit -m 'a | b; c && d > e' -m \"${X}\nline2\""},
		{name: "引数に = を含むだけ", cmd: `git commit -m 'a=b'`},
		{name: "特殊構文を含まない", cmd: "crit comments --json"},

		{name: "excluded 外なら形が崩れていても通過", cmd: `FOO=$HOME npm test 2>&1 | tail`},
		{name: "excluded 外のコマンド置換", cmd: `out=$(npm test)`},
		{name: "代入のみでコマンドが無い", cmd: `A=$HOME`},
		{
			name: "heredoc の body に書かれた文字列はコマンドとして読まない",
			cmd:  lines("cat <<'EOF' > ./tmp/note.md", "docker ps を実行する", "gh pr create も実行する", "EOF"),
		},
		{
			name: "コマンド置換の中の heredoc: body はコマンドとして読まない",
			cmd:  lines(`echo "$(cat <<'EOF'`, "gh pr create を実行した", "EOF", `)"`),
		},

		{name: "sandbox 内でも動くコマンドのパイプ", cmd: "go test ./... 2>&1 | tail -20"},
		{name: "sandbox 内でも動くコマンドと excluded 外の連結", cmd: "echo start && go test ./... | tail -15"},
		{name: "git の read-only verb のパイプ", cmd: "git log --oneline -5 | head"},
		{name: "git のグローバルオプション越しの read-only verb", cmd: "git -C /x --no-pager diff --stat main | tail -40"},
		{name: "git の read-only verb をコマンド置換で受ける", cmd: `out=$(git rev-parse HEAD)`},
		{name: "引数で read-only と分かる git: branch の表示", cmd: "pwd; git branch --show-current; git branch -a -vv | head"},
		{name: "引数で read-only と分かる git: tag の一覧", cmd: "git tag -l '2026.09*' | tail -3; git tag | wc -l"},
		{name: "引数で read-only と分かる git: worktree と stash の一覧", cmd: "git worktree list && ls skills; git stash list | head"},
		{name: "引数で read-only と分かる git: remote の表示", cmd: "git remote -v | head -2"},
		{name: "remote を読むだけの git", cmd: "git ls-remote --heads origin x | wc -l"},
		{name: "値を取らない git のグローバルオプション越しの書き込み verb", cmd: "git --no-replace-objects --literal-pathspecs switch -c feat/x origin/main"},
		{name: "read-only の -C git を他の excluded と連結する", cmd: "git -C /x log --oneline -3; docker system info"},
		{name: "bash -c の中が sandbox 内でも動くものだけ", cmd: `bash -c 'git log --oneline | head; go vet ./...'`},

		{
			name: "実例: gh pr create --body にコマンド置換で heredoc を渡す",
			cmd:  lines(`gh pr create --draft --assignee octocat --title "fix: x" --body "$(cat <<'EOF'`, "## Summary", "EOF", `)"`),
			deny: true,
		},
		{name: "実例: git commit の出力を tail で受ける", cmd: `git commit -q -m "chore: x" 2>&1 | tail -15; git log --oneline -1`, deny: true},
		{name: "実例: git push の出力を tail で受ける", cmd: "git push -u origin worktree-x 2>&1 | tail -20", deny: true},
		{name: "実例: crit へ入力リダイレクトで JSON を渡す", cmd: "crit comment --json --author 'Claude Code' < ./tmp/crit-replies.json", deny: true},
		{name: "実例: cargo に静的な値の変数を前置きする", cmd: "CARGO_BUILD_BUILD_DIR=./tmp/build-probe cargo run -- --fixture fixtures/compose.json", deny: true},
		{name: "実例: cargo clippy の出力を tail で受ける", cmd: "cargo clippy --all-targets -- -D warnings 2>&1 | tail -150", deny: true},
		{name: "実例: aqua i の出力を tail で受ける", cmd: "aqua i 2>&1 | tail -3", deny: true},
		{name: "実例: excluded でない grep を後ろにつなぐ", cmd: "gh api a --jq .status; gh release view v1 -R o/r; grep -rn x .", deny: true},
		// task は sandbox に落ちると、ネットワークへの TLS 接続や ~/.claude への書き込みを伴う
		// タスクが壊れる。2026-09-19 から 09-26 の transcript で x509 に落ちた task はどれもこの形。
		{name: "実例: task lint の出力を tail で受ける", cmd: "task lint 2>&1 | tail -n 15", deny: true},
		{name: "実例: task に PATH を前置きする", cmd: `PATH="$HOME/.local/share/aquaproj-aqua/bin:$PATH" task lint`, deny: true},
		{name: "実例: excluded 外の echo と task の連結", cmd: "echo start && task lint | tail -15", deny: true},
		{name: "実例: 別ディレクトリへ cd して task を実行する", cmd: "cd " + other + " && task go:lint 2>&1 | grep -E 'Failed|error'", deny: true},
		{name: "実例: direnv exec 越しの task", cmd: "direnv exec . task lint 2>&1 | tail -12", deny: true},
		{name: "direnv exec 越しの素の task", cmd: "direnv exec . task lint", deny: true},

		{name: "heredoc を対象コマンド自身が受ける", cmd: lines("crit comment --json <<'EOF'", `[{"reply_to":"c_1"}]`, "EOF"), deny: true},
		{name: "パイプの下流に置く", cmd: "cat ./tmp/replies.json | crit comment --json", deny: true},
		{name: "heredoc をパイプの上流に置く", cmd: lines("cat <<'EOF' | crit comment --json", `[{"reply_to":"c_1"}]`, "EOF"), deny: true},
		{name: "静的な値の代入", cmd: "A=1 docker system info", deny: true},
		{name: "代入値に $VAR 展開", cmd: `A=$HOME docker ps`, deny: true},
		{name: "env ラッパー", cmd: "env A=1 docker ps", deny: true},
		// KUBECONFIG / KUBECTL_EXTERNAL_DIFF の前置きは、kubeconfig の exec credential
		// plugin や diff ツールとして任意のコマンドを実行させうる。変数の前置きである
		// 時点で既に照合から外れて deny になる (kubectl 固有の対処は rules/kubectl.go)。
		{name: "KUBECONFIG の前置き", cmd: "KUBECONFIG=./tmp/evil.yaml kubectl diff -f x.yaml", deny: true},
		{name: "KUBECTL_EXTERNAL_DIFF の前置き", cmd: "KUBECTL_EXTERNAL_DIFF=./tmp/evil.sh kubectl diff -f x.yaml", deny: true},
		{name: "stderr をファイルへ捨てる", cmd: "gh api x 2>/dev/null", deny: true},
		{name: "出力リダイレクト", cmd: "gh api x > ./tmp/x.json", deny: true},
		{name: "|| でつなぐ", cmd: "gh api x || true", deny: true},
		{name: "subshell", cmd: "( gh api x )", deny: true},
		{name: "ブロック", cmd: "{ gh api x; }", deny: true},
		{name: "ループ", cmd: `while read -r x; do gh api "$x"; done < ./tmp/ids`, deny: true},
		{name: "別ディレクトリへの cd", cmd: "cd " + other + " && gh api x", deny: true},
		{name: "行き先を静的に決められない cd", cmd: "cd ~ && gh api x", deny: true},
		// Claude Code は cd の行き先を cwd の字面と比べる。同じ場所を指していても
		// 末尾スラッシュが付くと照合が外れる (2026-09-20 に crit で実測)。
		{name: "cwd と同じ場所でも末尾スラッシュ付きの cd", cmd: "cd " + cwd + "/ && gh api x", deny: true},
		{name: "double quote 内の backtick", cmd: "gh pr create --body \"a `x` b\"", deny: true},
		// Claude Code の照合はシェルの構文ではなく文字の並びを見る。置換として働かない位置でも外れる。
		{name: "single quote 内の backtick", cmd: "git commit -m 'see `foo`'", deny: true},
		{name: "single quote 内の $(", cmd: `git commit -m 'a $(b) c'`, deny: true},
		{name: "エスケープした backtick", cmd: "gh pr create --body \"a \\`x\\` b\"", deny: true},
		{name: "コマンド置換の中", cmd: `out=$(crit comments --json)`, deny: true},
		{name: "double quote 内のコマンド置換の中", cmd: `echo "$(docker ps)"`, deny: true},
		{name: "chain の後方でも検出する", cmd: `true && docker ps`, deny: true},
		{name: "絶対パス指定も basename で判定する", cmd: `/usr/local/bin/docker ps | head`, deny: true},
		{name: "git の書き込み verb はグローバルオプション越しでも strict", cmd: "git -C /x commit -m y | tail", deny: true},
		// 値を取るグローバルオプションを前置した書き込み系は、素の 1 文でも照合から外れる
		// (2026-09-21 に `git -C <repo> switch -c` が `.git/config` の EPERM で落ちて実測)。
		{name: "-C を前置した素の書き込み verb", cmd: "git -C /x switch -c feat/y origin/main", deny: true},
		{name: "-c を前置した素の書き込み verb", cmd: "git -c init.defaultBranch=main init /x", deny: true},
		{name: "--git-dir を密着形で前置した素の書き込み verb", cmd: "git --git-dir=/x/.git commit -F ./tmp/msg", deny: true},
		// シェルへ文字列で渡す形は先頭トークンが bash になり、中の excluded コマンドは必ず sandbox 内で動く。
		{name: "bash -c の中", cmd: `bash -c 'gh api rate_limit'`, deny: true},
		{name: "束ねたオプションの bash -lc の中", cmd: `bash -lc "cd /x && crit comments --json"`, deny: true},
		{name: "eval の中", cmd: `eval "gh pr list"`, deny: true},
		{name: "branch の削除は strict", cmd: "git branch -d chore/x | tail -1", deny: true},
		{name: "branch の作成は strict", cmd: "git branch feat/x | tail -1", deny: true},
		{name: "tag の作成は strict", cmd: "git tag v1 2>/dev/null", deny: true},
		{name: "worktree の追加は strict", cmd: "git worktree add ./tmp/wt main | tail -1", deny: true},
		{name: "stash の退避は strict", cmd: "git stash | tail -1", deny: true},
		{name: "read-only の git を strict なコマンドの引数に置換で渡す", cmd: `gh pr create --title "$(git log -1 --format=%s)"`, deny: true},
	}

	withExcluded(t, "aqua", "cargo", "crit", "docker", "gh", "git", "go", "kubectl", "task")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertVerdict(t, Check(parseForTest(t, tt.cmd), cwd), tt.deny)
		})
	}
}

// deny 文言は外れた原因を名指しする。原因が分からないと、パイプを外さずに別の回避策
// (変数前置き、ユーザーへの `!` 依頼) へ進んでしまう。
func TestSandboxExcludedReasonNamesCause(t *testing.T) {
	withExcluded(t, "gh", "cargo", "git", "task")
	tests := []struct {
		cmd  string
		want string
	}{
		{cmd: "gh api x | tail -1", want: "パイプ"},
		{cmd: `PATH="$HOME/x:$PATH" task lint`, want: "変数の前置き"},
		{cmd: "direnv exec . task lint", want: "「direnv」"},
		{cmd: "git -C /x switch -c feat/y origin/main", want: "グローバルオプション"},
		{cmd: "gh api x < ./tmp/in.json", want: "リダイレクト"},
		{cmd: "A=1 cargo run", want: "変数の前置き"},
		{cmd: `gh pr create --body "$(cat ./tmp/b.md)"`, want: "コマンド置換"},
		{cmd: "echo hi && gh api x", want: "「echo」"},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			v := Check(parseForTest(t, tt.cmd), "")
			if v == nil {
				t.Fatal("deny を期待したが通過した")
			}
			if !strings.Contains(v.Reason, tt.want) {
				t.Fatalf("理由に %q が無い: %s", tt.want, v.Reason)
			}
		})
	}
}

// excludedCommands はプロジェクトスコープの settings にも書ける (twigpui の `cargo *`)。
// user スコープしか読まないと、そのプロジェクトの照合外れを guard が見落とす。
func TestLoadExcludedBasesMergesProjectScope(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "settings.json")
	writeSettings(t, userPath, `{"sandbox":{"excludedCommands":["gh *"]}}`)
	project := t.TempDir()
	writeSettings(t, filepath.Join(project, ".claude", "settings.json"), `{"sandbox":{"excludedCommands":["cargo *"]}}`)
	writeSettings(t, filepath.Join(project, ".claude", "settings.local.json"), `{"sandbox":{"excludedCommands":["scripts/coverage.sh *"]}}`)
	t.Setenv("CLAUDE_SETTINGS_JSON", userPath)
	t.Setenv("CLAUDE_PROJECT_DIR", project)

	bases := loadExcludedBases()
	for _, want := range []string{"gh", "cargo", "scripts/coverage.sh"} {
		if !bases[want] {
			t.Errorf("%q が excluded に入っていない: %v", want, bases)
		}
	}
}

// uv は内部レジストリの keyring 認証 (UV_KEYRING_PROVIDER=subprocess) が設定された環境でだけ、
// 照合外れで sandbox に落ちると gcloud の ADC を denyRead が塞いで 401 になる
// (harness:sandbox-troubleshooting skill)。それ以外の環境は公開 PyPI だけを触るので、落ちても困らない。
func TestSandboxExcludedUV(t *testing.T) {
	cwd := t.TempDir()
	other := t.TempDir()
	withExcluded(t, "uv")

	t.Run("keyring 認証が無ければ別ディレクトリへの cd も通過", func(t *testing.T) {
		withRegistryKeyring(t, false)
		assertVerdict(t, Check(parseForTest(t, "cd "+other+" && uv lock"), cwd), false)
	})

	t.Run("keyring 認証があれば別ディレクトリへの cd は deny", func(t *testing.T) {
		withRegistryKeyring(t, true)
		v := Check(parseForTest(t, "cd "+other+" && uv lock"), cwd)
		assertVerdict(t, v, true)
		if !strings.Contains(v.Reason, "--project") {
			t.Errorf("--project を使う直し方が無い: %s", v.Reason)
		}
		if !metricPattern.MatchString(v.Reason) {
			t.Errorf("1 文目が計測の正規表現に合わない: %s", v.Reason)
		}
	})

	t.Run("keyring 認証があっても cwd と同じ場所への cd は通過", func(t *testing.T) {
		withRegistryKeyring(t, true)
		assertVerdict(t, Check(parseForTest(t, "cd "+cwd+" && uv lock"), cwd), false)
	})

	t.Run("keyring 認証があっても素の 1 文は通過", func(t *testing.T) {
		withRegistryKeyring(t, true)
		assertVerdict(t, Check(parseForTest(t, "uv lock --project "+other), cwd), false)
	})
}

func writeSettings(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// settings.json を読めないときは判定材料が無い。ここで deny すると
// clone 直後や CI で全てのパイプ付きコマンドが止まる。
func TestSandboxExcludedWithoutSettings(t *testing.T) {
	withExcluded(t)
	assertVerdict(t, Check(parseForTest(t, `docker ps | head`), ""), false)
}

func assertVerdict(t *testing.T, v *bashguard.Verdict, wantDeny bool) {
	t.Helper()
	switch {
	case wantDeny && v == nil:
		t.Fatal("deny を期待したが通過した")
	case !wantDeny && v != nil:
		t.Fatalf("通過を期待したが %s: %s", v.Decision, v.Reason)
	case v != nil && v.Decision != "deny":
		t.Fatalf("deny を期待したが %s", v.Decision)
	}
}
