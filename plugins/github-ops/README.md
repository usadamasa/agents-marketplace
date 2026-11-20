# github-ops

GitHub の issue・Project・Actions CI と、ローカルのリポジトリ操作の skill をまとめた plugin。
リポジトリの追従 (repo-sync skill) は同梱の Go の CLI `repo-scan` が行う。
repo-scan は SessionStart hook が `${CLAUDE_PLUGIN_DATA}/bin/repo-scan` へビルドする (Go が要る)。

```text
/plugin install github-ops --marketplace usadamasa/agents-marketplace
```

## 利用者の設定

Claude Code を sandbox で動かしているなら、repo-scan を `sandbox.excludedCommands` に入れる。
`repo-scan sync` は fetch・merge・rebase・stash で作業ツリーと `.git` を書き換え、fetch で認証情報を使う。
sandbox の中では書き込み先と認証情報の読み取りが制限され、途中で落ちうる。

repo-sync skill は repo-scan を絶対パスで打つ。excludedCommands の照合はコマンドの字面で行われるので、
パターンにはそのパスを展開済みの字面で書く。パターンの中の `~` や `$HOME` などの変数は展開されず、絶対パスの呼び出しに当たらない。

```json
{
  "sandbox": {
    "excludedCommands": [
      "/Users/<user>/.claude/plugins/data/github-ops-usadamasa/bin/repo-scan *"
    ]
  }
}
```

- `<user>` は自分のホームディレクトリに合わせる。パスは `${CLAUDE_PLUGIN_DATA}` の値 (`~/.claude/plugins/data/<plugin>-<marketplace>/`) に `bin/repo-scan` を足したもの
- 末尾の ` *` は、引数なしの `repo-scan` にも当たる
- sandbox の外で動くコマンドに OS の保護は無い。repo-scan は push・`reset --hard`・`git config` の書き込みをしない
