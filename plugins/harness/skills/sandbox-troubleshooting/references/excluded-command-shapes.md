# excludedCommands の照合: 形ごとの判定

SKILL.md の「excludedCommands の照合」の 5 条件を、形ごとに確かめた結果。
例は `gh` だが、どの excluded のコマンドでも同じ。

| 形 | 例 | 外で動くか |
| ---- | ---- | ---- |
| 素のコマンド | `gh api rate_limit` | ✅ |
| excluded 同士の連結 | `gh api ...; gh api ...` / `gh api ... && gh api ...` | ✅ |
| fd の複製 | `gh api ... 2>&1` | ✅ |
| 引数の中の変数展開 | `-H "X-A: $HOME"` / `-H "X: ${HOME}"` | ✅ |
| クォート内の演算子・改行 | `-H 'X: a \| b; c && d > e'` / 複数行の `--jq` | ✅ |
| cwd への `cd` を前置き | `cd <cwd の絶対パス> && gh api ...` | ✅ (ハーネスが自動で前置きする形) |
| 末尾スラッシュ付きの `cd` | `cd <cwd の絶対パス>/ && gh api ...` | ❌ (行き先は同じでも字面が違う) |
| 別ディレクトリへの `cd` | `cd /tmp && gh api ...` | ❌ |
| excluded でない文との連結 | `echo hi && gh api ...` | ❌ |
| `\|\|` でつなぐ | `gh api ... \|\| true` | ❌ |
| パイプ | `gh api ... \| tail -1` | ❌ |
| ファイルへのリダイレクト | `> ./tmp/x` / `< /dev/null` / `2>/dev/null` | ❌ |
| heredoc | `gh api ... <<'EOF'` | ❌ |
| subshell・ブロック・ループ | `( gh ... )` / `{ gh ...; }` / `while read x; do gh ...; done` | ❌ |
| コマンド置換への代入 | `out=$(gh api ...)` | ❌ |
| 引数の中のコマンド置換 | `--body "$(cat <<'EOF' ...)"` / `--jq "$(printf ...)"` | ❌ |
| クォート内のバッククォート・`$(` | `-H 'X: $(x)'` / `--body "... \`x\` ..."` | ❌ (シングルクォート内でも、`\` でエスケープしても外れる) |
| 変数の前置き | `GH_PAGER=cat gh api ...` / `A=$HOME gh api ...` | ❌ |
| ラッパー越し | `env A=1 gh ...` / `direnv exec . task lint` / `bash -c 'gh ...'` / `eval 'gh ...'` | ❌ (先頭トークンが別のコマンドになる) |
| パスの形のパターンを同じ字面で呼ぶ | パターン `/abs/x.sh *` に `/abs/x.sh a` / 引数なしの `/abs/x.sh` | ✅ (末尾の ` *` は引数なしにも当たる) |
| パスの形のパターンをクォートして呼ぶ | パターン `/abs/x.sh *` に `"/abs/x.sh" a` | ❌ |
| パターン側に変数や `~` を書く | パターン `${HOME}/x.sh *` / `$HOME/x.sh *` / `~/x.sh *` に `/Users/<name>/x.sh a` | ❌ (展開されない。`~/x.sh *` は呼ぶ側も `~/x.sh` と書いたときだけ当たる) |
| git の値を取らないグローバルオプション | `git --no-pager init x` / `git --literal-pathspecs switch -c x` | ✅ |
| git の値を取るグローバルオプション | `git -C <path> ...` / `git -c k=v ...` / `git --git-dir=<path> ...` | ❌ (2.1.277 の回帰。read-only な verb なら sandbox 内でも成立する) |

判定は Claude Code のバージョンで変わるため、表の ❌ / ✅ は更新のたびに確かめ直す。
公式の [settings リファレンス](https://code.claude.com/docs/en/settings-reference#sandbox-excludedcommands) は
「compound command のどこか 1 か所が一致すれば外で動く」と書くが、実際の挙動は逆なので、頼りにしない。
表と挙動が食い違ったときの測り直しは [maintenance.md](maintenance.md) に従う。
