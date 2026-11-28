# gh の PR / issue の書き込みの opt-in

guard-bash は PR / issue の状態を変える `gh` の書き込みを ask に落とす。
この文書は、プロジェクト単位でこの確認を省く opt-in の条件と、省いても残る ask / deny をまとめる。

## 有効化

キーは PR 用と issue 用で分かれ、片方を有効にしてももう片方は ask のまま。
プロジェクトの `.claude/settings.json` (または `settings.local.json`) にユーザーが手で書く。
キーの書き込みは hook が deny するので、Claude Code からは有効化できない。

```json
{ "env": { "CLAUDE_ALLOW_GH_ISSUE_WRITES": "1", "CLAUDE_ALLOW_GH_PR_WRITES": "1" } }
```

次の条件を全て満たしたときだけ成立し、ひとつでも欠けたら ask に落ちる (fail-closed)。

| 条件 | 理由 |
| ---- | ---- |
| 環境変数が `1` / `true` | 有効化の指定そのもの |
| プロジェクトスコープの settings の `env` に同じキーがある (実ファイルを読む) | シェルの `export` だけで有効化させない |
| `~/.claude/settings.json` の `env` に同じキーが無い | user スコープに書くと全プロジェクトへ効く |

## 判定

opt-in が成立していても、対象ごとに判定が分かれる。

| 対象 | 判定 |
| ---- | ---- |
| 自分が作った、プロジェクトのリポジトリの PR / issue | pass |
| 他人が作った PR / issue | deny (opt-in では覆せない) |
| 作成者を特定できない・照会に失敗 | ask |
| 別のリポジトリ (`-R` / URL / `GH_REPO`) | ask |
| リリース PR (ラベル `tagpr`)、ラベルを取れなかった PR | ask |
| `gh pr merge --admin` | ask |
| `--attach` を伴う `gh pr` / `gh issue` の `comment` / `create` / `edit` | ask |
| `gh pr close --delete-branch` | ask |
| `gh issue delete` / `transfer` | ask |

`gh pr merge --delete-branch` は通る。マージ済みの head を消すだけで、`delete_branch_on_merge` と同じ結果になる。

照会で確かめられずに ask へ落ちる形は、[SKILL.md](../SKILL.md) の「照会で確かめられない形と、照会が効く書き方」節にある。

## 確認が省かれても変わらないこと

確認プロンプトが出なくなるのは操作の許可であって、判断の委譲ではない。
opt-in しても、PR の Draft 解除とマージ、issue の close を進めてよいかの判断は変わらない。
