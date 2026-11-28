# github-ops

GitHub の issue・Actions CI と、ローカルのリポジトリ操作の skill をまとめた plugin。
リポジトリの追従 (repo-sync skill) は同梱の Go の CLI `repo-scan` が行う。
repo-scan は SessionStart hook が `${CLAUDE_PLUGIN_DATA}/bin/repo-scan` へビルドする (Go が要る)。

marketplace を[リポジトリの README の「使い方」](../../README.md#使い方)のとおりに登録してから、次のコマンドで install する。

```sh
claude plugin install github-ops@usadamasa
```

## 利用者の設定

### skill が読む値

skill が読む利用者ごとの値は、`.claude-plugin/plugin.json` の `userConfig` が宣言する (キー・型・説明はそこが持つ)。
値は `/config` で入れるか、user スコープの `settings.json` の `pluginConfigs` に plugin の ID
(`github-ops@<marketplace>`) をキーにして書く。値を入れていないキーは skill の本文で置き換わらず、
skill はその役割を未設定として利用者に聞く。

```json
{
  "pluginConfigs": {
    "github-ops@usadamasa": {
      "options": {
        "cross_repo": "<owner>/<repo>",
        "harness_repo": "<owner>/<repo>",
        "labels_declarative": true,
        "shared_ci_owner": "<owner>",
        "shared_ci_topic": "<topic>"
      }
    }
  }
}
```

### sandbox

Claude Code を sandbox で動かしているなら、repo-scan を `sandbox.excludedCommands` に入れる。
`repo-scan sync` は fetch・merge・rebase・stash で作業ツリーと `.git` を書き換え、fetch で認証情報を使う。
sandbox の中では書き込み先と認証情報の読み取りが制限され、途中で落ちうる。

repo-sync skill は repo-scan を `~/.claude/plugins/data/github-ops-usadamasa/bin/repo-scan` の字面で打つ。
excludedCommands の照合はコマンドの字面で行われ、パターンの中の `~` は展開されないので、パターンは skill が打つ字面と同じ形で書く。

```json
{
  "sandbox": {
    "excludedCommands": [
      "~/.claude/plugins/data/github-ops-usadamasa/bin/repo-scan *"
    ]
  }
}
```

- パスは `${CLAUDE_PLUGIN_DATA}` の値 (`~/.claude/plugins/data/<plugin>-<marketplace>/`) に `bin/repo-scan` を足したもの
- 絶対パスや `$HOME` で書いたパターンは、`~` で打つ呼び出しに当たらない
- 末尾の ` *` は、引数なしの `repo-scan` にも当たる
- sandbox の外で動くコマンドに OS の保護は無い。repo-scan は push・`reset --hard`・`git config` の書き込みをしない
