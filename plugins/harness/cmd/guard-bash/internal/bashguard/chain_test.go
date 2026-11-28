package bashguard

import (
	"slices"
	"testing"
)

// CommandArgs は chain・subshell・置換の中まで、簡単コマンドを出現順に並べる。
// コマンド名は架空のものにする (判定ルールの変更に左右されないため)。
func TestCommandArgs(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want [][]string
	}{
		{"単独", "a x y", [][]string{{"a", "x", "y"}}},
		{"空文字列", "", nil},
		{"&& と ||", "a && b || c", [][]string{{"a"}, {"b"}, {"c"}}},
		{"; とパイプ", "a ; b | c", [][]string{{"a"}, {"b"}, {"c"}}},
		{"改行", "a\nb", [][]string{{"a"}, {"b"}}},
		{"バックグラウンド", "a &", [][]string{{"a"}}},
		{"subshell", "(cd d && a)", [][]string{{"cd", "d"}, {"a"}}},
		{"クォートの中のセパレータは分けない", "a 'x && y'", [][]string{{"a", "x && y"}}},
		{"コマンド置換の中", "a --opt=$(b c)", [][]string{{"a", ""}, {"b", "c"}}},
		{"プロセス置換の中", "a <(b)", [][]string{{"a", ""}, {"b"}}},
		{"パスはコマンド名に", "/usr/bin/a x", [][]string{{"a", "x"}}},

		// シェルへ文字列で渡したコマンドは再解析する
		{"bash -c", "bash -c 'a && b'", [][]string{{"bash", "-c", "a && b"}, {"a"}, {"b"}}},
		{"束ねた -ec", "sh -ec 'a'", [][]string{{"sh", "-ec", "a"}, {"a"}}},
		{"eval", "eval a x", [][]string{{"eval", "a", "x"}, {"a", "x"}}},
		{"入れ子", `bash -c "sh -c 'a'"`, [][]string{{"bash", "-c", "sh -c 'a'"}, {"sh", "-c", "a"}, {"a"}}},
		{"展開を含む文字列は辿らない", `bash -c "$X"`, [][]string{{"bash", "-c", ""}}},
		{"-c の無いシェル", "bash s.sh", [][]string{{"bash", "s.sh"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file, err := Parse(tc.cmd)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.cmd, err)
			}
			if got := CommandArgs(file); !slices.EqualFunc(got, tc.want, slices.Equal) {
				t.Errorf("CommandArgs(%q) = %q, want %q", tc.cmd, got, tc.want)
			}
		})
	}
}
