package golang

import (
	"strings"
	"testing"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/bashguard"
)

func TestModuleTrust(t *testing.T) {
	cases := []struct {
		name     string
		cmd      string
		wantDeny bool
	}{
		// 通す形
		{"素の go get", "go get example.com/m@latest", false},
		{"既定の GOPROXY", "GOPROXY=https://proxy.golang.org,direct go get example.com/m", false},
		{"GOPROXY=off (キャッシュだけ)", "GOPROXY=off go mod tidy", false},
		{"既定の GOSUMDB", "GOSUMDB=sum.golang.org go mod download", false},
		{"空の GOPROXY (既定値になる)", "GOPROXY= go get example.com/m", false},
		{"GOPRIVATE (非公開モジュールの取得)", "GOPRIVATE=github.com/x go get github.com/x/m", false},
		{"go env -w GOPRIVATE", "go env -w GOPRIVATE=github.com/x", false},
		{"値の無い export", "export GOPROXY", false},
		{"無関係の変数", "GOFLAGS=-mod=mod go build ./...", false},
		{"go env の読み取り", "go env GOPROXY", false},
		{"go の外での語", "printf '%s\\n' GOPROXY=direct", false},

		// proxy を外す
		{"前置きの GOPROXY=direct", "GOPROXY=direct go get example.com/m", true},
		{"direct を先頭に置く", "GOPROXY=direct,https://proxy.golang.org go get example.com/m", true},
		{"別の proxy", "GOPROXY=https://goproxy.example go get example.com/m", true},
		{"単独の代入", "GOPROXY=direct; go get example.com/m", true},
		{"export", "export GOPROXY=direct && go get example.com/m", true},
		{"declare -x", "declare -x GOPROXY=direct", true},
		{"env 経由", "env GOPROXY=direct go get example.com/m", true},
		{"env のオプション付き", "env -u GOFLAGS GOPROXY=direct go get example.com/m", true},
		{"go env -w", "go env -w GOPROXY=direct", true},
		{"go env -w の複数指定", "go env -w GOFLAGS=-mod=mod GONOSUMDB=example.com", true},
		{"bash -c の中", "bash -c 'GOPROXY=direct go get example.com/m'", true},
		{"値が展開で決まらない", "GOPROXY=$P go get example.com/m", true},
		{"クォートした値", "GOPROXY='direct' go get example.com/m", true},

		// 照合を外す
		{"GOSUMDB=off", "GOSUMDB=off go get example.com/m", true},
		{"別の sumdb", "GOSUMDB=sum.example.com go get example.com/m", true},
		{"GONOSUMDB", "GONOSUMDB=github.com/x go get github.com/x/m", true},
		{"GONOSUMCHECK", "GONOSUMCHECK=1 go get example.com/m", true},
		{"GONOPROXY", "GONOPROXY=github.com/x go get github.com/x/m", true},
		{"GOINSECURE", "GOINSECURE=example.com go get example.com/m", true},
		{"cd の後ろ", "cd cmd && GOPROXY=direct GONOSUMDB=github.com/x go get github.com/x/m@latest", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := bashguard.Parse(tc.cmd)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.cmd, err)
			}
			v := ModuleTrust(f)
			if got := v != nil && v.Decision == "deny"; got != tc.wantDeny {
				t.Errorf("ModuleTrust(%q) deny = %v, want %v (verdict: %+v)", tc.cmd, got, tc.wantDeny, v)
			}
		})
	}
}

// deny の理由は、止めた変数と、代わりに実行する形を名指しする。
func TestModuleTrustReasonGuides(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want []string
	}{
		{"proxy を外す", "cd cmd && GOPROXY=direct go get example.com/m", []string{"GOPROXY", "go.mod のあるディレクトリ", "素の 1 文"}},
		{"照合を外す", "GONOSUMDB=example.com go get example.com/m", []string{"GONOSUMDB", "素の 1 文"}},
		{"go env -w", "go env -w GOSUMDB=off", []string{"GOSUMDB", "go env -u GOSUMDB"}},
		{"値が展開で決まらない", "GOPROXY=$P go get example.com/m", []string{"GOPROXY", "字面"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := bashguard.Parse(tc.cmd)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.cmd, err)
			}
			v := ModuleTrust(f)
			if v == nil {
				t.Fatalf("ModuleTrust(%q) が通過した", tc.cmd)
			}
			for _, w := range tc.want {
				if !strings.Contains(v.Reason, w) {
					t.Errorf("理由に %q が無い: %s", w, v.Reason)
				}
			}
		})
	}
}
