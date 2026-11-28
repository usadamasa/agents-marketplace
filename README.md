# agents-marketplace

<!-- paths-filter の試し。マージしない -->

usadamasa の Claude Code plugin marketplace。
plugin は専用のリポジトリか、このリポジトリの `plugins/<name>/` に置き、
`.claude-plugin/marketplace.json` で所在を宣言する。
marketplace 名は `usadamasa` で、リポジトリ名 (`agents-marketplace`) とは違う。

## 使い方

```sh
claude plugin marketplace add usadamasa/agents-marketplace
claude plugin install agents-daemon@usadamasa
claude plugin install japanese-writer@usadamasa
claude plugin install go-arch-metrics@usadamasa
```

settings.json で宣言する場合は `extraKnownMarketplaces` と `enabledPlugins` に書く。

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

| plugin | 置き場 | 役目 |
| ---- | ---- | ---- |
| `agents-daemon` | [usadamasa/agents-daemon](https://github.com/usadamasa/agents-daemon) | Claude Code のセッションのそばに常駐し、セッションの中からはできないこと (止まったセッションの再開、セッションを跨いだ状態の集約、時間で動く掃除、rex の block 操作) を背後で担う |
| `japanese-writer` | [usadamasa/japanese-writer](https://github.com/usadamasa/japanese-writer) | 日本語の技術文書の執筆と校正 |
| `go-arch-metrics` | [usadamasa/go-arch-metrics](https://github.com/usadamasa/go-arch-metrics) | Go のアーキテクチャメトリクスの測定と改善 |
| `dev-workflow` | [plugins/dev-workflow](plugins/dev-workflow) | TDD、デバッグ、完了前の検証、コードレビュー、ブランチの仕上げ、subagent への委譲 |
| `shellscript` | [plugins/shellscript](plugins/shellscript) | bash スクリプトの書き方と bats-core でのテスト |
| `toolchain` | [plugins/toolchain](plugins/toolchain) | 開発ツールの置き場所 (Nix devShell、home-manager)、PATH と direnv |
| `github-ops` | [plugins/github-ops](plugins/github-ops) | GitHub の issue・Actions CI とリポジトリ操作 |
| `harness` | [plugins/harness](plugins/harness) | Claude Code ハーネス自体の運用 (skill の書き方、permission、sandbox、token の使い方) |
| `visuals` | [plugins/visuals](plugins/visuals) | 図・画像・PDF とその見せ方 |
| `leadership` | [plugins/leadership](plugins/leadership) | 組織の壁を越えるリーダーシップの実践 |
| `misc` | [plugins/misc](plugins/misc) | 他の plugin に入らないもの (Obsidian の Vault、1Password CLI) |

## plugin を足すとき

外部リポジトリの plugin は次の順で足す。

1. plugin のリポジトリに `.claude-plugin/plugin.json` を置き、`claude plugin validate --strict` を通す
2. `.claude-plugin/marketplace.json` の `plugins` に `{"name", "source": {"source": "github", "repo"}}` を追加する
3. `claude plugin validate --strict .` を通す

このリポジトリ内の plugin の追加と変更は
[maintaining-in-repo-plugins](.claude/skills/maintaining-in-repo-plugins/SKILL.md) に従う。

## ライセンス

MIT
