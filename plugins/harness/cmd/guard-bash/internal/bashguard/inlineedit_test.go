package bashguard

import (
	"strings"
	"testing"
)

func TestInlineFileEdit(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		deny bool
	}{
		// 書き込みを起こさない形
		{name: "ls -la", cmd: "ls -la"},
		{name: "パイプだけ", cmd: "grep -rn foo . | head -30"},
		{name: "sed の読み取り", cmd: "sed -n 1,40p file.md"},
		{name: "-i が別コマンドのフラグ", cmd: "ls -i | sed -n 1,5p"},
		{name: "stderr を捨てる", cmd: "ls foo 2>/dev/null"},
		{name: "fd 複製", cmd: "go build ./... 2>&1 | tail -5"},
		{name: "/dev/null へのリダイレクト", cmd: "echo hi > /dev/null"},
		{name: "算術評価の >", cmd: `while (( i > 0 )); do i=$((i - 1)); done`},
		{name: "算術展開の >", cmd: `n=$(( 5 > 2 )); echo "$n"`},
		{name: "条件式の >", cmd: `[[ "$a" > "$b" ]] && echo yes`},
		{name: "./tmp/ へのリダイレクト", cmd: "mkdir -p ./tmp && task test > ./tmp/test.log 2>&1"},
		{name: "$TMPDIR へのリダイレクト", cmd: `jq . x.json > "$TMPDIR/out.json"`},
		{name: "./tmp/ への heredoc", cmd: lines("cat > ./tmp/script.sh <<'EOF'", "echo hello", "EOF")},
		{name: "./tmp/ を対象にした sed -i", cmd: "sed -i '' 's/a/b/' ./tmp/work.txt"},
		{name: "~/tmp/ へのリダイレクト", cmd: "task test > ~/tmp/test.log 2>&1"},
		{name: "$HOME/tmp/ へのリダイレクト", cmd: `jq . x.json > "$HOME/tmp/out.json"`},
		{name: "${HOME}/tmp/ へのリダイレクト", cmd: `jq . x.json > "${HOME}/tmp/out.json"`},
		{name: "~/tmp/ を対象にした sed -i", cmd: "sed -i '' 's/a/b/' ~/tmp/work.txt"},
		{name: "../tmp/ へのリダイレクト", cmd: "echo x > ../tmp/out.log"},
		{name: "コマンド自身が heredoc を受ける", cmd: lines("crit comment --json <<'EOF'", `{"body":"x"}`, "EOF")},
		{name: "git commit -F - への heredoc", cmd: lines("git commit -F - <<'EOF'", "fix: something", "EOF")},
		{
			name: "コマンド置換の中の heredoc (PR 本文)",
			cmd:  lines(`gh pr create --body "$(cat <<'EOF'`, "## Summary", "", "before > after", "", "EOF", `)"`),
		},
		{
			name: "heredoc の body に sed -i が含まれる",
			cmd:  lines("crit comment --json <<'EOF'", "sed -i '' 's/a/b/' README.md はやめよう", "EOF"),
		},
		{name: "引用符の中の >", cmd: `git commit -m "fix: a > b"`},

		// ファイルを書き換える形
		{name: "条件式の後ろのリダイレクト", cmd: `[[ -f x ]] && echo y > notes.txt`, deny: true},
		{name: "cat > FILE <<EOF", cmd: lines("cat > README.md <<'EOF'", "hello", "EOF"), deny: true},
		{name: "cat <<EOF > FILE", cmd: lines("cat <<'EOF' > docs/note.md", "hello", "EOF"), deny: true},
		{name: "cat <<EOF >> FILE", cmd: lines("cat <<'EOF' >> AGENTS.md", "hello", "EOF"), deny: true},
		{name: "tee FILE <<EOF", cmd: lines("tee AGENTS.md <<'EOF'", "hello", "EOF"), deny: true},
		{name: "heredoc | tee -a FILE", cmd: lines("cat <<'EOF' | tee -a AGENTS.md", "hello", "EOF"), deny: true},
		{name: "ホームディレクトリへの追記", cmd: `echo "export FOO=1" >> ~/.zshrc`, deny: true},
		// ~/tmp の許可はスラッシュ区切りで切る。前方一致だけの兄弟パスは通さない
		{name: "~/tmp の前方一致でしかないパス", cmd: "echo x > ~/tmp-default-acl.sh", deny: true},
		{name: "$HOME/tmp の前方一致でしかないパス", cmd: `echo x > "$HOME/tmpfile"`, deny: true},
		{name: "~/tmp 自体への書き込み", cmd: "echo x > ~/tmp", deny: true},
		// 許可した置き場から .. で抜ける形。一時ファイルを消すコマンド側と判定を揃える
		{name: "~/tmp から .. で抜ける", cmd: "echo x > ~/tmp/../.claude/foo", deny: true},
		{name: "./tmp から .. で抜ける", cmd: "echo x > ./tmp/../AGENTS.md", deny: true},
		{name: "$TMPDIR から .. で抜ける", cmd: `echo x > "$TMPDIR/../foo"`, deny: true},
		{name: "printf でファイル生成", cmd: `printf '%s\n' foo > config.yaml`, deny: true},
		{name: "chain の後段のリダイレクト", cmd: "ls -la && echo x > notes.txt", deny: true},
		{name: "引用符付きのパスへのリダイレクト", cmd: `echo x > "docs/note.md"`, deny: true},
		{name: "jq の出力でファイルを上書き", cmd: `jq '.a = 1' settings.json > settings.json.new`, deny: true},
		{name: "sed -i '' (BSD)", cmd: "sed -i '' 's/a/b/' README.md", deny: true},
		{name: "sed -i.bak", cmd: "sed -i.bak 's/a/b/' README.md", deny: true},
		{name: "sed --in-place", cmd: "sed --in-place 's/a/b/' README.md", deny: true},
		{name: "perl -pi -e", cmd: "perl -pi -e 's/a/b/' README.md", deny: true},
		{name: "chain の後段の sed -i", cmd: "git status && sed -i '' 's/a/b/' README.md", deny: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := parseForTest(t, tt.cmd)
			v := InlineFileEdit(tt.cmd, f)
			if tt.deny && v == nil {
				t.Fatalf("deny を期待したが通過した")
			}
			if !tt.deny && v != nil {
				t.Fatalf("通過を期待したが %s: %s", v.Decision, v.Reason)
			}
			if v != nil && v.Decision != "deny" {
				t.Fatalf("deny を期待したが %s", v.Decision)
			}
		})
	}
}

// deny の文面は「どのツールを使えばよいか」を伝えるためのもの。
// 案内が消えるとエージェントが同じ書き方を繰り返す。
func TestInlineFileEditReasonGuidesToTools(t *testing.T) {
	cmd := "echo x > notes.txt"
	v := InlineFileEdit(cmd, parseForTest(t, cmd))
	if v == nil {
		t.Fatal("deny を期待したが通過した")
	}
	for _, want := range []string{"Edit ツール", "Write ツール", "./tmp/"} {
		if !strings.Contains(v.Reason, want) {
			t.Errorf("deny の文面に %q が無い: %s", want, v.Reason)
		}
	}
}
