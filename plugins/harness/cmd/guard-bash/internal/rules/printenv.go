package rules

import (
	"github.com/spf13/pflag"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/guard-bash/internal/argrule"
)

// printenvFlags は GNU printenv の -0/--null。宣言しないと `-0 NAME` の名前を数え損なう。
func printenvFlags(fs *pflag.FlagSet) {
	fs.BoolP("null", "0", false, "")
}

// printenvManyNames は printenv に名前を 2 つ以上渡す形か。
func printenvManyNames(cl *argrule.Cmdline) bool {
	return argrule.AnyView(cl, "printenv", func(v *argrule.View) bool { return len(v.Pos) >= 2 })
}

// printenvRules は printenv に名前を複数渡す形を止める。
//
// macOS (BSD) の printenv は先頭の名前しか見ず、先頭が未設定なら何も出さずに exit 1 で終わる
// (GNU は全部出す)。後ろの変数の値が見えないまま空の結果を「どれも未設定」と読み違えるため、
// 手順を知っていても短縮して踏む形を機械的に落とす。
var printenvRules = &argrule.Group{
	Tool:  "printenv",
	Flags: printenvFlags,
	Deny: []argrule.Rule{
		argrule.Check{Desc: "printenv <name> <name>...", Fn: printenvManyNames},
	},
	DenyReason: "macOS の printenv は先頭の名前しか見ません。1 つずつ実行するか `printenv | rg '^(A|B)='` で絞ってください",
}
