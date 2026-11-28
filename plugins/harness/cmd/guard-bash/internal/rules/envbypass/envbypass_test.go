package envbypass

import (
	"strings"
	"testing"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/bashguard"
)

// deny 側は 2026-10-10 に ~/.claude/projects の transcript から拾った実例をそのまま置いている。
// どれも direnv の env が Bash から外れた後に、PATH や policy を手で補おうとした形。
func TestCheck(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string // deny の理由に含まれる語。空なら通過
	}{
		{name: "direnv allow は通す", cmd: "direnv allow"},
		{name: "direnv status は通す", cmd: "direnv status"},
		{name: "direnv export は通す", cmd: "direnv export bash"},
		{name: "aqua 自身の呼び出しは通す", cmd: "aqua i; aqua which task; aqua root-dir"},
		{name: "exec を引数に取るだけなら通す", cmd: "rg -n 'aqua exec' skills"},
		{name: "shim のディレクトリを引数に取るだけなら通す", cmd: "ls -la ~/.local/share/aquaproj-aqua/bin/"},
		{name: "変数の参照は通す", cmd: `printf '%s\n' "$AQUA_POLICY_CONFIG"`},
		{name: "変数名を検索するだけなら通す", cmd: "rg -n AQUA_POLICY_CONFIG cmd/Taskfile.yml"},
		{name: "aqua と無関係な PATH の前置きは対象外", cmd: "PATH=/opt/homebrew/bin:$PATH some-tool --version"},
		{name: "aqua と無関係な変数の前置きは対象外", cmd: "GOFLAGS=-mod=mod go build ./..."},
		{name: "ログレベルの前置きは解決を曲げないので通す", cmd: "AQUA_LOG_LEVEL=debug shellcheck --version"},
		{name: "aqua と無関係な export は通す", cmd: "export FOO=bar"},

		{name: "direnv exec で task を包む", cmd: "direnv exec . task test", want: "direnv exec"},
		{name: "direnv exec でスクリプトを包む", cmd: "direnv exec /x/y scripts/run-shellcheck-all.sh", want: "direnv exec"},
		{name: "bash -c の中の direnv exec", cmd: "bash -c 'direnv exec . task lint'", want: "direnv exec"},
		{name: "aqua exec でツールを包む", cmd: "aqua exec -- golangci-lint run --timeout=5m ./cmd/... ./internal/...", want: "aqua exec"},
		{name: "aqua exec で task を包む", cmd: "aqua exec -- task lint", want: "aqua exec"},
		{name: "chain の後方の aqua exec", cmd: "aqua exec -- goimports -l cmd internal; aqua exec -- gosec -quiet -exclude-dir tmp ./...", want: "aqua exec"},
		{name: "policy の前置き", cmd: "AQUA_POLICY_CONFIG=/x/aqua/policy.yaml golangci-lint run ./cmd/...", want: "AQUA_POLICY_CONFIG"},
		{name: "policy の前置き (展開を含む値)", cmd: `AQUA_POLICY_CONFIG="$PWD/aqua/policy.yaml" go-arch-lint check && gosec ./...`, want: "AQUA_POLICY_CONFIG"},
		{name: "policy の前置き (chain の後方)", cmd: "mkdir -p ./tmp && AQUA_POLICY_CONFIG=./aqua/policy.yaml bash ./scripts/baseline.sh", want: "AQUA_POLICY_CONFIG"},
		{name: "policy の export", cmd: `export AQUA_POLICY_CONFIG="$PWD/aqua/policy.yaml"; aqua install -l`, want: "AQUA_POLICY_CONFIG"},
		{name: "env ラッパー越しの policy", cmd: "env AQUA_POLICY_CONFIG=/x/policy.yaml golangci-lint run ./cmd/...", want: "AQUA_POLICY_CONFIG"},
		{name: "aqua.yaml の差し替え", cmd: "AQUA_CONFIG=/x/aqua.yaml aqua which task", want: "AQUA_CONFIG"},
		{name: "PATH に aqua の bin を前置き", cmd: "PATH=/Users/u/.local/share/aquaproj-aqua/bin:$PATH task validate:schema", want: "PATH"},
		{name: "PATH に aqua の bin を前置き (HOME 展開)", cmd: `PATH="$HOME/.local/share/aquaproj-aqua/bin:$PATH" bats tests/`, want: "PATH"},
		{name: "PATH に aqua の bin を export", cmd: `export PATH="$HOME/.local/share/aquaproj-aqua/bin:$PATH" AQUA_POLICY_CONFIG="$PWD/aqua/policy.yaml"`, want: "PATH"},
		{name: "shim を絶対パスで呼ぶ", cmd: "/Users/u/.local/share/aquaproj-aqua/bin/task --version", want: "絶対パス"},
		{name: "shim をチルダで呼ぶ", cmd: "~/.local/share/aquaproj-aqua/bin/golangci-lint run --timeout 5m ./...", want: "絶対パス"},
		{name: "shim を HOME 展開で呼ぶ", cmd: `"$HOME/.local/share/aquaproj-aqua/bin/jv" --version`, want: "絶対パス"},
		{name: "pkgs 配下の実体を直接呼ぶ", cmd: "/Users/u/.local/share/aquaproj-aqua/pkgs/github_release/github.com/go-task/task/v3.54.0/task_darwin_arm64.tar.gz/task --version", want: "絶対パス"},
		{name: "chain の後方の shim 呼び出し", cmd: "go vet ./... && ~/.local/share/aquaproj-aqua/bin/gosec -quiet ./...", want: "絶対パス"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file, err := bashguard.Parse(tc.cmd)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.cmd, err)
			}
			got := Check(tc.cmd, file)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("通過を期待したが deny: %s", got.Reason)
				}
				return
			}
			if got == nil {
				t.Fatalf("deny を期待したが通過")
			}
			if got.Decision != "deny" {
				t.Errorf("Decision = %q, want deny", got.Decision)
			}
			if !strings.Contains(got.Reason, tc.want) {
				t.Errorf("理由に %q が無い: %s", tc.want, got.Reason)
			}
			if !strings.Contains(got.Reason, "skill") {
				t.Errorf("理由が skill を指していない: %s", got.Reason)
			}
		})
	}
}
