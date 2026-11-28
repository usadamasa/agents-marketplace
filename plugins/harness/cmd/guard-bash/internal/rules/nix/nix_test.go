package nix

import (
	"strings"
	"testing"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/bashguard"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string // deny の理由に含まれる語。空なら通過
	}{
		{name: "nix 自身の呼び出しは通す", cmd: "nix flake lock; nix flake update; nix --version"},
		{name: "daemon の疎通確認は通す", cmd: "nix store info"},
		{name: "store へ落とすだけの nix build は通す", cmd: "nix build --no-link --print-out-paths nixpkgs#hello"},
		{name: "-c の無い nix develop は通す", cmd: "nix develop"},
		{name: "store を温める nix develop --command true は通す", cmd: "nix develop --command true"},
		{name: "store を温める nix develop -c true は通す", cmd: "nix develop -c true"},
		{name: "store path を引数に取るだけなら通す", cmd: "ls -la /nix/store/abc-go-1.26.8/bin/"},
		{name: "nix develop の形を検索するだけなら通す", cmd: "rg -n 'nix develop -c' skills"},
		{name: "store と無関係な PATH の前置きは通す", cmd: "PATH=/opt/homebrew/bin:$PATH some-tool --version"},
		{name: "PATH 以外の変数に store path があっても通す", cmd: "GOROOT=/nix/store/abc-go-1.26.8/share/go go version"},

		{name: "nix develop -c で task を包む", cmd: "nix develop -c task lint", want: "nix develop"},
		{name: "nix develop --command で包む", cmd: "nix develop --command golangci-lint run ./...", want: "nix develop"},
		{name: "flake 参照付きの nix develop -c", cmd: "nix develop .#default -c task test", want: "nix develop"},
		{name: "nix develop -c true の後に引数が続く形は通さない", cmd: "nix develop -c true task", want: "nix develop"},
		{name: "chain の後方の nix develop -c", cmd: "nix develop -c true; nix develop -c task test", want: "nix develop"},
		{name: "bash -c の中の nix develop", cmd: "bash -c 'cd cmd && nix develop -c go test ./...'", want: "nix develop"},
		{name: "nix shell -c でツールを包む", cmd: "nix shell nixpkgs#golangci-lint -c golangci-lint run ./...", want: "nix shell"},
		{name: "nix shell --command で包む", cmd: "nix shell nixpkgs#go-task --command task lint", want: "nix shell"},
		{name: "nix shell -c true も通さない (develop だけの例外)", cmd: "nix shell nixpkgs#hello -c true", want: "nix shell"},
		{name: "nix run でツールを起こす", cmd: "nix run nixpkgs#gosec -- ./...", want: "nix run"},
		{name: "store path を絶対パスで呼ぶ", cmd: "/nix/store/mp3m86aziykrwshmqfr7i6w4ak4c8qz4-go-task-3.53.1/bin/task test", want: "絶対パス"},
		{name: "引用符で囲んだ store path", cmd: `"/nix/store/abc-go-1.26.8/bin/go" version`, want: "絶対パス"},
		{name: "chain の後方の store path 呼び出し", cmd: "go vet ./... && /nix/store/abc-gosec-2.29.0/bin/gosec ./...", want: "絶対パス"},
		{name: "PATH に store path を前置き", cmd: "PATH=/nix/store/abc-go-task-3.53.1/bin:$PATH task lint", want: "PATH"},
		{name: "PATH に store path を export", cmd: `export PATH="/nix/store/abc-go-1.26.8/bin:$PATH"`, want: "PATH"},
		{name: "env ラッパー越しの PATH 前置き", cmd: "env PATH=/nix/store/abc-go-1.26.8/bin:/usr/bin go test ./...", want: "PATH"},
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
