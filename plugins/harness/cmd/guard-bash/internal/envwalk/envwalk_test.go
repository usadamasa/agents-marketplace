package envwalk

import (
	"slices"
	"testing"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/bashguard"
)

// TestWalk は代入の 3 つの形と bash -c の中が同じ visitor に届くことを見る。
func TestWalk(t *testing.T) {
	tests := []struct {
		name        string
		cmd         string
		wantAssigns []string // name=value の列
		wantCalls   []string // コマンド名の列
	}{
		{
			name:        "前置き・export・env の代入を同じ形で渡す",
			cmd:         `A=1 cmd1; export B="$HOME/x"; env C=3 cmd2`,
			wantAssigns: []string{"A=1", `B="$HOME/x"`, "C=3"},
			wantCalls:   []string{"cmd1", "env"},
		},
		{
			name:        "bash -c の中も歩く",
			cmd:         `bash -c 'D=4 inner --flag'`,
			wantAssigns: []string{"D=4"},
			wantCalls:   []string{"bash", "inner"},
		},
		{
			name:        "名前が展開の export は代入として渡さず落ちない",
			cmd:         `export "$name"; export E=`,
			wantAssigns: []string{"E="},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file, err := bashguard.Parse(tc.cmd)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.cmd, err)
			}
			var assigns, calls []string
			got := Walk(tc.cmd, file, Visitor{
				Assign: func(name, value string) *bashguard.Verdict {
					assigns = append(assigns, name+"="+value)
					return nil
				},
				Call: func(_ string, args []string) *bashguard.Verdict {
					calls = append(calls, args[0])
					return nil
				},
			})
			if got != nil {
				t.Fatalf("visitor が nil を返すのに Verdict: %s", got.Reason)
			}
			if !slices.Equal(assigns, tc.wantAssigns) {
				t.Errorf("assigns = %q, want %q", assigns, tc.wantAssigns)
			}
			if !slices.Equal(calls, tc.wantCalls) {
				t.Errorf("calls = %q, want %q", calls, tc.wantCalls)
			}
		})
	}
}

func TestWalkStopsAtFirstVerdict(t *testing.T) {
	cmd := "a; b; c"
	file, err := bashguard.Parse(cmd)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	got := Walk(cmd, file, Visitor{Call: func(_ string, args []string) *bashguard.Verdict {
		seen = append(seen, args[0])
		if args[0] == "b" {
			return bashguard.Deny("stop at %s", args[0])
		}
		return nil
	}})
	if got == nil || got.Reason != "stop at b" {
		t.Fatalf("Verdict = %+v, want stop at b", got)
	}
	if !slices.Equal(seen, []string{"a", "b"}) {
		t.Errorf("seen = %q, want [a b]", seen)
	}
}
