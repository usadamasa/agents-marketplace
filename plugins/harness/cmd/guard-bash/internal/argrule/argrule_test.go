package argrule

import (
	"slices"
	"testing"
)

func noSpec(string) FlagSpec { return nil }

// Describe は方針表とログに出る。形を固定する。
func TestCmdAtDescribe(t *testing.T) {
	cases := []struct {
		name string
		rule CmdAt
		want string
	}{
		{
			"noun 1 つ・前方一致",
			CmdAt{Tool: "a", Nouns: NounOne, Verbs: []string{"create", "delete"}},
			"a <noun> {create*, delete*}",
		},
		{
			"path と選択肢・完全一致",
			CmdAt{Tool: "a", Path: []string{"x|y"}, Exact: true, Verbs: []string{"close", "merge"}},
			"a x | y {close, merge}",
		},
		{
			"位置を固定しない",
			CmdAt{Tool: "a", Nouns: NounLoose, Verbs: []string{"create"}},
			"a ... {create*}",
		},
		{
			"verb なし",
			CmdAt{Tool: "a", Path: []string{"auth"}},
			"a auth",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rule.Describe(); got != tc.want {
				t.Errorf("Describe() = %q, want %q", got, tc.want)
			}
		})
	}
}

// CmdAt は位置引数の決まった位置で verb を見る。コマンド名は架空のもの。
func TestCmdAtMatch(t *testing.T) {
	cases := []struct {
		name string
		rule CmdAt
		args []string
		want bool
	}{
		{"noun の直後の verb", CmdAt{Tool: "a", Nouns: NounOne, Exact: true, Verbs: []string{"del"}}, []string{"a", "n", "del"}, true},
		{"フラグの値に verb", CmdAt{Tool: "a", Nouns: NounOne, Exact: true, Verbs: []string{"del"}}, []string{"a", "n", "ls", "--x", "del"}, false},
		{"前方一致", CmdAt{Tool: "a", Nouns: NounLoose, Verbs: []string{"add"}}, []string{"a", "n", "m", "add-x"}, true},
		{"完全一致は前方一致しない", CmdAt{Tool: "a", Nouns: NounOne, Exact: true, Verbs: []string{"add"}}, []string{"a", "n", "add-x"}, false},
		{"ラップ形", CmdAt{Tool: "a", Path: []string{"p"}}, []string{"w", "--", "a", "p"}, true},
		{"Head はラップ形に当たらない", CmdAt{Tool: "a", Head: true, Path: []string{"p"}}, []string{"w", "--", "a", "p"}, false},
		{"下位 resource", CmdAt{Tool: "a", Path: []string{"r:*"}, Exact: true, Verbs: []string{"del"}}, []string{"a", "r:s", "del"}, true},
		{"名前が前方一致するだけの resource", CmdAt{Tool: "a", Path: []string{"r:*"}, Exact: true, Verbs: []string{"del"}}, []string{"a", "rx", "del"}, false},
		{"数字を含む noun", CmdAt{Tool: "a", Nouns: NounOne, Exact: true, Verbs: []string{"get"}}, []string{"a", "settings-v2", "get"}, true},
		{"数字始まりの noun は対象外", CmdAt{Tool: "a", Nouns: NounOne, Exact: true, Verbs: []string{"get"}}, []string{"a", "2settings", "get"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rule.match(NewCmdline(tc.args, noSpec)); got != tc.want {
				t.Errorf("Match(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// When は Rule が当たっても事前条件が false なら一致としない。
// 条件は Rule が当たった argv に対してだけ評価する (外部を叩く述語を
// 置いても、当たらないコマンドでは走らない)。
func TestWhenPrecond(t *testing.T) {
	var calls [][]string
	r := When{
		Rule: CmdAt{Tool: "a", Head: true, Path: []string{"p"}, Exact: true, Verbs: []string{"m"}},
		Cond: func(cl *Cmdline) bool {
			calls = append(calls, cl.Args)
			return !slices.Contains(cl.Args, "5")
		},
		Note: "test",
	}

	if r.match(NewCmdline([]string{"a", "p", "m", "5"}, noSpec)) {
		t.Error("事前条件が false なのに一致した")
	}
	if !r.match(NewCmdline([]string{"a", "p", "m", "7"}, noSpec)) {
		t.Error("事前条件が true なのに一致しなかった")
	}
	if r.match(NewCmdline([]string{"a", "p", "v", "5"}, noSpec)) {
		t.Error("Rule が当たらないのに一致した")
	}
	want := [][]string{{"a", "p", "m", "5"}, {"a", "p", "m", "7"}}
	if !slices.EqualFunc(calls, want, slices.Equal) {
		t.Errorf("Cond の呼び出し = %q, want %q", calls, want)
	}
}

// Hinted の案内は When に包んでも FirstMatchRule の結果から引ける。
// 案内の無いルールが先に当たったら空文字になる。
func TestHintOfThroughWhen(t *testing.T) {
	hinted := When{
		Rule: Hinted{Rule: CmdAt{Tool: "a", Path: []string{"p"}}, Hint: "代わりに q を使う"},
		Cond: func(*Cmdline) bool { return true },
		Note: "test",
	}
	plain := CmdAt{Tool: "a", Path: []string{"r"}}
	rs := []Rule{hinted, plain}

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"a", "p"}, "代わりに q を使う"},
		{[]string{"a", "r"}, ""},
	}
	for _, tc := range cases {
		r, ok := FirstMatchRule(rs, NewCmdline(tc.args, noSpec))
		if !ok {
			t.Fatalf("%q に一致するルールが無い", tc.args)
		}
		if got := HintOf(r); got != tc.want {
			t.Errorf("HintOf(%q に一致したルール) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
