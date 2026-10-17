# agents-marketplace

usadamasa の Claude Code plugin marketplace｡plugin の実体はそれぞれのリポジトリにあり、
ここは `.claude-plugin/marketplace.json` で所在を宣言するだけ｡
marketplace 名は `usadamasa` で、リポジトリ名 (`agents-marketplace`) とは違う｡

## 使い方

```sh
claude plugin marketplace add usadamasa/agents-marketplace
claude plugin install agents-daemon@usadamasa
claude plugin install japanese-writer@usadamasa
claude plugin install go-arch-metrics@usadamasa
```

settings.json で宣言する場合は `extraKnownMarketplaces` と `enabledPlugins` に書く｡

```json
{
  "extraKnownMarketplaces": {
    "usadamasa": {
      "source": { "source": "github", "repo": "usadamasa/agents-marketplace" },
      "autoUpdate": true
    }
  },
  "enabledPlugins": {
    "agents-daemon@usadamasa": true,
    "japanese-writer@usadamasa": true,
    "go-arch-metrics@usadamasa": true
  }
}
```

## plugin 一覧

| plugin | リポジトリ | 役目 |
| ---- | ---- | ---- |
| `agents-daemon` | [usadamasa/agents-daemon](https://github.com/usadamasa/agents-daemon) | Claude Code のセッションのそばに常駐し、セッションの中からはできないこと (止まったセッションの再開、セッションを跨いだ状態の集約、時間で動く掃除、rex の block 操作) を背後で担う |
| `japanese-writer` | [usadamasa/japanese-writer](https://github.com/usadamasa/japanese-writer) | 日本語の技術文書の執筆と校正 |
| `go-arch-metrics` | [usadamasa/go-arch-metrics](https://github.com/usadamasa/go-arch-metrics) | Go のアーキテクチャメトリクスの測定と改善 |

## plugin を足すとき

1. plugin のリポジトリに `.claude-plugin/plugin.json` を置き、`claude plugin validate --strict` を通す
2. `.claude-plugin/marketplace.json` の `plugins` に `{"name", "source": {"source": "github", "repo"}}` を追加する
3. `claude plugin validate --strict .` で marketplace manifest を検査する

## ライセンス

MIT
