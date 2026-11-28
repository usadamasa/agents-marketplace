package rules

import (
	"strings"
	"testing"
)

// 非 GET の gh api を止めた deny は、確認を経て通せる書き込みの形を名指しする。
// 別の理由の deny には付けない。
func TestGHAPIWriteDenyHintsAlternative(t *testing.T) {
	const hint = "pulls/{n}/reviews"
	cases := []struct {
		name     string
		cmd      string
		wantHint bool
	}{
		{"issue の作成", "gh api -X POST repos/o/r/issues -f title=t", true},
		{"review の dismiss", "gh api -X PUT repos/o/r/pulls/5/reviews/123/dismissals -f message=x", true},
		{"gh auth login", "gh auth login", false},
		{"機微情報のエンドポイント", "gh api user/keys", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evalCmd(t, tc.cmd)
			if got.Decision != Deny {
				t.Fatalf("Evaluate(%q).Decision = %v, want Deny (pattern: %q)", tc.cmd, got.Decision, got.Pattern)
			}
			if has := strings.Contains(got.Reason, hint); has != tc.wantHint {
				t.Errorf("Evaluate(%q).Reason に %q を含む = %v, want %v (reason: %q)", tc.cmd, hint, has, tc.wantHint, got.Reason)
			}
		})
	}
}

// review comment の読み取りは確認しない。
func TestGHReviewCommentReadUnchanged(t *testing.T) {
	runEvalCases(t, []evalCase{
		{"review comment の一覧", "gh api repos/o/r/pulls/5/comments", Pass, false},
		{"review comment の一覧 GET 明示", "gh api -X GET repos/o/r/pulls/5/comments -f per_page=100", Pass, true},
	})
}
