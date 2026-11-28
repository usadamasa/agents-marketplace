package bashguard

import "fmt"

// Verdict は guard の判定。nil が通過を表すので、値ではなくポインタで扱う。
type Verdict struct {
	Decision string // "deny" または "ask"
	Reason   string
}

// Deny は実行を止める判定を作る。
func Deny(format string, args ...any) *Verdict {
	return &Verdict{Decision: "deny", Reason: fmt.Sprintf(format, args...)}
}

// Ask はユーザーへ判断を渡す判定を作る。
func Ask(format string, args ...any) *Verdict {
	return &Verdict{Decision: "ask", Reason: fmt.Sprintf(format, args...)}
}
