# visuals

図・画像・PDF を作る、読む、見せる skill をまとめた plugin。
画面の撮影 (cleanshot-capture skill) は同梱の Go の CLI `cleanshot-capture` が行う。
cleanshot-capture は SessionStart hook が `${CLAUDE_PLUGIN_DATA}/bin/cleanshot-capture` へビルドする (Go が要る)。
撮影には macOS と CleanShot X が要る。

```text
/plugin install visuals --marketplace usadamasa/agents-marketplace
```

## 利用者の設定

Claude Code を sandbox で動かしているなら、cleanshot-capture を `sandbox.excludedCommands` に入れる。
cleanshot-capture は `open` で CleanShot X に URL scheme を渡し、`osascript` でウィンドウを列挙する。
sandbox の中ではどちらも Launch Services と Apple Events に届かず、撮影が落ちる。

cleanshot-capture skill は cleanshot-capture を
`~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture` の字面で実行する。
excludedCommands の照合はコマンドの字面で行われ、パターンの中の `~` は展開されないので、
パターンには skill が実行する字面をそのまま書く。
確認なしで撮れるようにするなら、同じ字面で `permissions.allow` にも入れる。

```json
{
  "permissions": {
    "allow": [
      "Bash(~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture:*)"
    ]
  },
  "sandbox": {
    "excludedCommands": [
      "~/.claude/plugins/data/visuals-usadamasa/bin/cleanshot-capture *"
    ]
  }
}
```

- パスは `${CLAUDE_PLUGIN_DATA}` の値 (`~/.claude/plugins/data/<plugin>-<marketplace>/`) に `bin/cleanshot-capture` を足したもの
- 絶対パスや `$HOME` で書いたパターンは、`~` で実行する呼び出しに当たらない
- 末尾の ` *` は、引数なしの `cleanshot-capture` にも当たる

sandbox の外で動くコマンドに OS の保護は無い。cleanshot-capture は `--out` を作業ディレクトリ配下に限り、
macOS の標準コマンドと Google Chrome を絶対パスで起動する。`rex` サブコマンドの `rex` だけは PATH から引く。
