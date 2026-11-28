# harness

Claude Code のハーネス自体を運用する skill をまとめた plugin。
ハーネスは、skill・hook・settings など、Claude Code の動きを決める周りの仕組みを指す。

| skill | 中身 |
| --- | --- |
| writing-skills | skill の書き方と、配布する前のテスト |
| auditing-token-usage | 過去のセッションを横断した token の浪費の監査 |

token の監査は同梱の Go の CLI `analyze-tokens` が行う。auditing-token-usage を使うには Go が要る。
analyze-tokens は SessionStart hook が `${CLAUDE_PLUGIN_DATA}/bin/analyze-tokens` へビルドする。

取り込んだ skill と移植したコードの出典は [NOTICE.md](NOTICE.md) にある。

```text
claude plugin install harness@usadamasa
```

## 利用者の設定

analyze-tokens に sandbox の設定は要らない。
読むのは `~/.claude/projects` の transcript だけで、書き込みとネットワークへの接続をしない。
