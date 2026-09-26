# agents-marketplace

usadamasa の Claude Code plugin marketplace｡コーディングエージェントの運用に使う daemon・hook・skill と、
日本語の執筆・校正の skill 群を plugin として配る｡plugin の実体はそれぞれのリポジトリにあり、
ここは `.claude-plugin/marketplace.json` で所在を宣言するだけ｡

## 使い方

```sh
claude plugin marketplace add usadamasa/agents-marketplace
claude plugin install agents-daemon@agents-marketplace
claude plugin install japanese-writer@agents-marketplace
```

settings.json で宣言する場合は `extraKnownMarketplaces` と `enabledPlugins` に書く｡宣言だけでは
新しい端末に自動 install されないので、端末ごとに `claude plugin install` を打つ｡

```json
{
  "extraKnownMarketplaces": {
    "agents-marketplace": {
      "source": { "source": "github", "repo": "usadamasa/agents-marketplace" },
      "autoUpdate": true
    }
  },
  "enabledPlugins": {
    "agents-daemon@agents-marketplace": true,
    "japanese-writer@agents-marketplace": true
  }
}
```

## plugin 一覧

| plugin | リポジトリ | 役目 |
| ---- | ---- | ---- |
| `agents-daemon` | [usadamasa/agents-daemon](https://github.com/usadamasa/agents-daemon) | 利用上限で止まった Claude Code セッションの自動再開と、context 使用率に応じた compact-prep / compact の投入 (herdr の pane 経由) |
| `japanese-writer` | [usadamasa/japanese-writer](https://github.com/usadamasa/japanese-writer) | 日本語の技術文書を書く・校正する skill 群、proofreader subagent、編集した md を閉じるときに点検する Stop hook (writing-gate) |

## plugin を足すとき

1. plugin のリポジトリに `.claude-plugin/plugin.json` を置き、`claude plugin validate --strict` を通す
2. `.claude-plugin/marketplace.json` の `plugins` に `{"name", "source": {"source": "github", "repo"}}` を追加する
3. `claude plugin validate --strict .` で marketplace manifest を検査する

## ライセンス

MIT
