---
name: packaging-claude-plugins
description: >-
  Claude Code の plugin を作るとき、marketplace として公開するとき、
  `~/.claude/skills/` の skill や agent を別リポジトリの plugin へ切り出すときに使う｡
  plugin.json の `version` の付け方 (calver) もここで決める｡
  plugin の変数が置換されない、plugin の skill や agent を名前で呼べない・旧版に当たる、
  install 先のパスが安定しない、sandbox 内で plugin の install が失敗する、といった症状にも引く｡
---

# packaging-claude-plugins

公式ドキュメントに書かれた仕様は要約とリンクに留め、本体には実機で確かめた挙動を置く｡
記載の挙動は Claude Code 2.1.280 で、local directory marketplace から user scope へ install して確かめたもの｡
バージョンを上げて怪しくなったら、末尾の「挙動の再確認」で確かめ直す｡

## 公式ドキュメントの要点

| ページ | 要点 |
| ---- | ---- |
| [plugins-reference](https://code.claude.com/docs/en/plugins-reference.md) | `.claude-plugin/plugin.json` のフィールド｡skill は `skills/` が推奨で `commands/` は legacy |
| [plugin-marketplaces](https://code.claude.com/docs/en/plugin-marketplaces.md) | `.claude-plugin/marketplace.json` の書式と `source` の種類 |
| [discover-plugins](https://code.claude.com/docs/en/discover-plugins.md) | settings の `extraKnownMarketplaces` (`{autoUpdate, source: {source: github, repo}}`) と `enabledPlugins` (`"<plugin>@<marketplace>": true`) |
| [skills](https://code.claude.com/docs/en/skills.md) | SKILL.md の frontmatter と置換される変数 |
| [hooks](https://code.claude.com/docs/en/hooks.md) | plugin の `hooks/hooks.json`｡Stop hook は matcher を持たない |

ドキュメントから読み落としやすい点が 2 つある｡

- GitHub 由来の plugin は、`enabledPlugins` に書いただけでは新しい端末で自動 install されない｡
  端末ごとに `claude plugin install` が要る｡
- install 時に走る lifecycle hook は無い｡npx や Go バイナリのような外部依存は、
  README に前提として書くか、SessionStart hook で用意する｡

## バージョン

新しく作る plugin は、最初の commit から `plugin.json` の `version` を calver で書く｡

- 形式は `YYYY.MMDD.NN`｡月日と、同じ日の中での通番を 0 埋めする (2026 年 9 月 26 日の 1 本目なら `2026.0926.01`)｡
- `version` は semver として検査されない｡0 埋めの calver も `claude plugin validate --strict` を通る (2.1.283 で確認)｡
- `version` を書くと、利用者はその値が変わるまで同じ版に留まる｡配りたい変更を入れるたびに `version` を上げる｡
- marketplace.json のエントリにも `version` を書けるが、plugin.json の値が優先される｡plugin.json だけに書く｡

## 変数の置換

| 変数 | SKILL.md 本文 | agents/*.md 本文 |
| ---- | ---- | ---- |
| `${CLAUDE_PLUGIN_ROOT}` | 置換される | 置換される |
| `${CLAUDE_SKILL_DIR}` | 置換される | **置換されず字面のまま残る** |
| `${CLAUDE_PLUGIN_DATA}` | 置換される | (未確認) |

- agent から自 plugin の references を指すときは `${CLAUDE_PLUGIN_ROOT}/skills/<skill>/references/...` と書く｡
- `${CLAUDE_PLUGIN_DATA}` の値は `~/.claude/plugins/data/<marketplace>-<plugin>/`｡
- 置換は Claude が読むテキストに対して行われるだけで、Bash の環境変数としては export されない｡
  本文に `"${CLAUDE_PLUGIN_ROOT}/scripts/foo.sh"` と書けば実パスになって Claude に見えるが、
  Claude が組み立てた ad-hoc なコマンドの中で `$CLAUDE_PLUGIN_ROOT` を展開させても空になる｡

## 名前解決

`Skill` と `Agent` で規則が違う｡

| 呼び出し | bare 名 (`proofread`) | namespaced 名 (`<plugin>:proofread`) |
| ---- | ---- | ---- |
| `Skill` ツール | 解決する｡ただし `~/.claude/skills/` を先に探す | 解決する |
| `Agent` ツールの `subagent_type` | **plugin の agent には解決しない** | 解決する |

- `Skill` の bare 名で呼び出せるのは、同じ plugin の skill、別 plugin の skill、`~/.claude/skills/` の user skill のどれか｡
  同じ名前が複数あると `~/.claude/skills/` が勝つので、旧コピーが残っていると plugin 版ではなく旧版が読み込まれる｡
- `Agent` の bare 名は plugin の agent を探さない｡`Agent type 'proofreader' not found` で失敗し、
  同名の `~/.claude/agents/proofreader.md` があればそちらの旧版に当たる｡
  plugin 内の skill から自 plugin の agent を dispatch するときは、常に `<plugin>:<agent>` と書く｡
- `Skill` ツールはファイルパスを受け付けない｡skill 内の `references/*.md` を読ませる手順は `Read` で書く｡

## install 先

- `claude plugin install` は `~/.claude/plugins/cache/<marketplace>/<plugin>/<version>/` にコピーを作る｡
  `<version>` は plugin.json の `version`、無ければ commit hash の先頭 12 桁｡
- パスがバージョンごとに変わるので、他のツール (agy など) から symlink で参照する先には向かない｡
  ghq の clone のような安定したパスを参照する｡
- local directory marketplace (`claude plugin marketplace add <絶対パス>`) から install すると、
  cache にコピーを作りつつ `${CLAUDE_PLUGIN_ROOT}` はソースディレクトリそのものを指す (cwd によらない)｡
  ソースを編集すると install し直さずに反映されるので、開発中の動作確認はこの形で行う｡
- ローカル install のコピーには `.gitignore` 対象の `tmp/` なども含まれる｡GitHub 由来なら git の中身だけになる｡

## 単一リポジトリで marketplace と plugin を兼ねる

```text
<repo>/
  .claude-plugin/
    marketplace.json   # plugins[0].source: "./"
    plugin.json
  skills/<skill>/SKILL.md
  agents/<agent>.md
```

- `marketplace.json` の `plugins[0].source` に `"./"` を書き、`plugin.json` を同じルートに置く｡
- `claude plugin validate --strict <repo>` で両方を検査する (`--strict` を付けると marketplace.json も検査される)｡
- install 名は `<plugin.json の name>@<marketplace.json の name>`｡

## user scope の skill を plugin へ切り出す

1. plugin 内の skill と agent からの呼び出しを、`Skill` も `Agent` も `<plugin>:<name>` に揃える｡
   bare 名のままだと、移行期に `~/.claude/` に残った旧版へ当たる｡
2. agent 本文の `${CLAUDE_SKILL_DIR}` を `${CLAUDE_PLUGIN_ROOT}/skills/<skill>/...` へ書き換える｡
3. `claude plugin validate --strict` を通し、local directory marketplace から install して動作を確かめる｡
4. `~/.claude/skills/<name>` と `~/.claude/agents/<name>.md` の旧コピーを外す｡
   構成管理リポジトリから symlink しているなら、リポジトリ側の実体も消す｡

## sandbox との関係 (このハーネス固有)

- `claude plugin marketplace add` と `claude plugin install` は `~/.claude/plugins/` へ書くため、
  strict sandbox の Bash からは EPERM で失敗する｡ユーザーに `! claude plugin install ...` の形で打ってもらう｡
- `claude -p` での headless 実行も同じ理由で、ユーザーの `!` 経由になる｡
- `claude plugin validate` は読み取りだけなので sandbox 内で通る｡

## 挙動の再確認

上の表が古くなっていないかを確かめる手順｡

1. 使い捨ての plugin を作る｡probe skill の SKILL.md 本文に、3 つの変数を字面のまま並べて
   「見えた値をそのまま出力する」と書く｡probe agent の本文にも同じ変数を並べる｡
2. caller skill を作り、probe skill を `Skill` で bare 名と `<plugin>:probe` の両方で、
   probe agent を `Agent` で bare 名と `<plugin>:probe-agent` の両方で呼ぶ手順を書く｡
3. `claude plugin validate --strict <dir>` を通す｡
4. ユーザーに `! claude plugin marketplace add <dir の絶対パス>` と `! claude plugin install <plugin>@<marketplace>` を打ってもらう｡
5. ユーザーに caller skill を呼ぶ prompt を `! claude -p "..."` で打ってもらい、置換された値と、
   各呼び出しが解決したかを読む｡
6. 確かめ終えたら `claude plugin uninstall` と `claude plugin marketplace remove` で片付ける (これもユーザーの `!` 経由)｡
