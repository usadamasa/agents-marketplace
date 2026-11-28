package bashguard

import (
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

// lines は複数行のコマンド文字列を組み立てる (heredoc を含む形の再現用)。
func lines(l ...string) string { return strings.Join(l, "\n") }

// parseForTest はコマンド文字列を AST へ読む。解析できない形はテストの前提が壊れている。
func parseForTest(t *testing.T, cmd string) *syntax.File {
	t.Helper()
	f, err := Parse(cmd)
	if err != nil {
		t.Fatalf("Parse(%q): %v", cmd, err)
	}
	return f
}

func assertVerdict(t *testing.T, v *Verdict, wantDeny bool) {
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

// assertDecision は判定の種類まで照合する。want が空なら通過を期待する。
func assertDecision(t *testing.T, v *Verdict, want string) {
	t.Helper()
	switch {
	case want == "" && v != nil:
		t.Fatalf("通過を期待したが %s: %s", v.Decision, v.Reason)
	case want != "" && v == nil:
		t.Fatalf("%s を期待したが通過した", want)
	case v != nil && v.Decision != want:
		t.Fatalf("%s を期待したが %s", want, v.Decision)
	}
}

// evalCalls は簡単コマンドごとに check を当て、最初の判定を返す。
func evalCalls(t *testing.T, cmd string, check func(*syntax.CallExpr) *Verdict) *Verdict {
	t.Helper()
	var found *Verdict
	WalkCallExprs(parseForTest(t, cmd), func(call *syntax.CallExpr) {
		if found == nil {
			found = check(call)
		}
	})
	return found
}
