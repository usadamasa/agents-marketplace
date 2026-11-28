package sandboxexcluded

import (
	"regexp"
	"strings"
	"testing"
)

// metricPattern は scripts/sandboxexcluded-denies.jq が deny の 1 文目から原因とコマンド名を
// 取り出す正規表現と同じもの。文言を変えてここから外れると、週次の計測が cause unknown に落ちて
// 候補の前後比較ができなくなる。
var metricPattern = regexp.MustCompile(`「([^」]+)」 は sandbox\.excludedCommands の対象ですが、(.+?)があるため照合が外れ`)

// deny の 2 文目は、deny した形に合わせた代わりの書き方を示す。
// 例は 2026-10-16〜18 に実際に deny されたコマンドから取った。
func TestSandboxExcludedHint(t *testing.T) {
	withExcluded(t, "cargo", "gh", "ghq", "git", "task")
	tests := []struct {
		name    string
		cmd     string
		want    []string
		notWant []string
	}{
		{name: "ghq の一覧を rg で絞る", cmd: "ghq list | rg -n example", want: []string{"`ghq list <query>`"}},
		{name: "gh api の中身を base64 -d で戻す", cmd: "gh api repos/o/r/readme --jq '.content' | base64 -d", want: []string{"application/vnd.github.raw+json"}},
		{name: "gh の出力を head で切る", cmd: "gh pr view 390 --json body --jq .body | head -60", want: []string{"`--jq`"}},
		{name: "task の出力を tail で切る", cmd: "task lint 2>&1 | tail -40", want: []string{"パイプを外して"}},
		{
			name: "until で gh pr checks を待つ",
			cmd:  "until gh pr checks 94 --json bucket --jq 'all(.bucket != \"pending\")' | rg -q true; do sleep 20; done; gh pr checks 94",
			want: []string{"`gh pr checks <番号> --watch`"},
		},
		{name: "for で複数リポジトリを回す", cmd: `for r in a b; do gh pr view 1 -R "o/$r"; done`, want: []string{"1 つずつ別の呼び出し"}},
		{name: "--watch の前の sleep", cmd: "sleep 20; gh pr checks 71 --watch --interval 20", want: []string{"`sleep` を外して"}},
		{name: "./tmp を作ってから task を実行する", cmd: "mkdir -p ./tmp && task ci", want: []string{"`mkdir -p ./tmp` は別の呼び出し"}},
		{
			name: "引数にコマンド置換で値を渡す",
			cmd:  "gh run view 1 --log --job $(gh run view 1 --json jobs --jq '.jobs[0].databaseId')",
			want: []string{"内側のコマンドを先に単独で"},
		},
		{name: "本文の字面に backtick がある", cmd: "git commit -m 'see `foo`'", want: []string{"`-F`"}},
		{name: "excluded でないコマンドとの連結", cmd: "pwd; git branch --show-current; task --version", want: []string{"「pwd」 は別の Bash 呼び出し"}},
		{
			name:    "終了コードの echo",
			cmd:     `task lint; echo "exit=$?"`,
			want:    []string{"終了コードは Bash ツール"},
			notWant: []string{"「echo」 は別の"},
		},
		{
			name: "パイプと終了コードの echo",
			cmd:  `task test 2>&1 | rg -v '^ok ' | head -40; echo "exit=${PIPESTATUS[0]}"`,
			want: []string{"パイプを外して", "終了コードは Bash ツール"},
		},
		{name: "printf で終了コードを出す", cmd: `task plugins:check; printf 'exit=%s\n' "$?"`, want: []string{"終了コードは Bash ツール"}},
		// 表に無い形は今の文言に落とす。
		{name: "表に無い形", cmd: "A=1 cargo run", want: []string{"素の 1 文"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Check(parseForTest(t, tt.cmd), "")
			if v == nil {
				t.Fatal("deny を期待したが通過した")
			}
			for _, w := range tt.want {
				if !strings.Contains(v.Reason, w) {
					t.Errorf("理由に %q が無い: %s", w, v.Reason)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(v.Reason, w) {
					t.Errorf("理由に %q がある: %s", w, v.Reason)
				}
			}
			if !metricPattern.MatchString(v.Reason) {
				t.Errorf("1 文目が計測の正規表現に合わない: %s", v.Reason)
			}
		})
	}
}
