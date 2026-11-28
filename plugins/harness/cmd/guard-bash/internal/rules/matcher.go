package rules

import (
	"slices"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
)

// Decision は簡単コマンド 1 つに対する判定結果。
type Decision int

const (
	Pass Decision = iota
	Ask
	Deny
)

// Verdict は簡単コマンド 1 つの判定と、その根拠。
type Verdict struct {
	Decision Decision
	// Reason は deny/ask 時にユーザーへ返す説明。どのコマンドが止まったかを名指しする。
	Reason string
	// Pattern は一致したルールの説明 (ログ用)。Pass でも allow に当たった場合は入る。
	Pattern string
}

// specs は tool ごとの FlagSpec。group の宣言から init で組み立てる。
// ルールの関数が解析を呼ぶので、all を直接参照すると初期化が循環する。
var specs = map[string]argrule.FlagSpec{}

func init() {
	for _, g := range all {
		if g.Flags != nil {
			specs[g.Tool] = g.Flags
		}
	}
}

// specFor は tool の FlagSpec を返す。宣言が無い tool は nil。
func specFor(tool string) argrule.FlagSpec { return specs[tool] }

// isHelp は help フラグを持つか。--help は位置を問わないが、-h は gh auth の
// --hostname 短縮形でもあるため、末尾のトークンのときだけ help とみなす。
func isHelp(args []string) bool {
	return slices.Contains(args, "--help") || args[len(args)-1] == "-h"
}

// Evaluate は簡単コマンド 1 つの argv を判定する。
//
// argv は mvdan.cc/sh が切り出した簡単コマンドの引数で、args[0] はコマンド名。
// コマンドライン全体の文字列は見ない。PR 本文やコミットメッセージは 1 トークンに
// 収まるので、そこに危険なコマンドを書いても照合されない。
func Evaluate(args []string) Verdict {
	if len(args) == 0 || isHelp(args) {
		return Verdict{Decision: Pass}
	}

	groups := selectGroups(args)
	if len(groups) == 0 {
		return Verdict{Decision: Pass}
	}

	cl := argrule.NewCmdline(args, specFor)
	// 引数を解釈できないと、どのルールにも当たらずに素通りしてしまう。
	if cl.HeadView() == nil {
		return Verdict{
			Decision: Ask,
			Reason:   args[0] + " の引数を解釈できませんでした。実行してよいか確認してください",
			Pattern:  "(引数の解析に失敗)",
		}
	}

	// allow → deny → ask の順に group 横断で評価する。
	for _, g := range groups {
		if p, ok := argrule.FirstMatch(g.Allow, cl); ok {
			return Verdict{Decision: Pass, Pattern: p}
		}
	}
	for _, g := range groups {
		if r, ok := argrule.FirstMatchRule(g.Deny, cl); ok {
			return Verdict{Decision: Deny, Reason: withHint(g.DenyReason, r), Pattern: r.Describe()}
		}
	}
	for _, g := range groups {
		if r, ok := argrule.FirstMatchRule(g.Ask, cl); ok {
			return Verdict{Decision: Ask, Reason: withHint(g.AskReason, r), Pattern: r.Describe()}
		}
	}
	return Verdict{Decision: Pass}
}

// withHint は group の理由文の後ろに、一致したルールの案内 (argrule.Hinted) を付ける。
func withHint(reason string, r argrule.Rule) string {
	if h := argrule.HintOf(r); h != "" {
		return reason + "。" + h
	}
	return reason
}

// selectGroups は評価対象の group を宣言順で返す。
//
// ゲートは「コマンド名がガード対象の CLI か」だけを見る。引数のどこかに CLI 名が
// 現れたら対象にする方式だと、`echo gcloud auth login` のような言及で止まる。
//
// コマンド名が対象なら、argv に tool 名がトークンとして現れた group を全て評価する。
// `op run -- gcloud auth login` のようにラップされた形でも内側の CLI を拾うため。
func selectGroups(args []string) []*argrule.Group {
	head := args[0]
	if !slices.ContainsFunc(all, func(g *argrule.Group) bool { return g.Tool == head }) {
		return nil
	}
	var out []*argrule.Group
	for _, g := range all {
		if slices.Contains(args, g.Tool) {
			out = append(out, g)
		}
	}
	return out
}
