# harness

Claude Code のハーネス自体を運用する skill と hook をまとめた plugin。
ハーネスは、skill・hook・settings など、Claude Code の動きを決める周りの仕組みを指す。

| skill | 中身 |
| --- | --- |
| writing-skills | skill の書き方と、配布する前のテスト |
| auditing-token-usage | 過去のセッションを横断した token の浪費の監査 |
| sandbox-troubleshooting | strict sandbox で Bash が想定外に失敗したときの診断 |
| guard-bash-git-github | guard-bash が git や gh を deny / ask にした理由と、通る書き方 |

| hook | イベント | 中身 |
| --- | --- | --- |
| guard-home-dir | PreToolUse (Read・Edit・Write・Glob・Grep・NotebookEdit・Bash) | ホーム配下の許していない置き場へのアクセスを deny する |
| guard-bash | PreToolUse (Bash) | Bash のコマンドを構文解析し、破壊的な操作と、sandbox の外で動くはずのコマンドが sandbox 内に落ちる形を deny / ask にする |
| guard-optin-env.sh | PreToolUse (Write・Edit・Bash) | gh の書き込みの opt-in キーをプロジェクトの settings へ書く操作を deny する |
| build-guards.sh・build-analyze-tokens.sh | SessionStart | Go のバイナリを、ソースが変わったときだけビルドする |

取り込んだ skill と移植したコードの出典は [NOTICE.md](NOTICE.md) にある。

```text
claude plugin install harness@usadamasa
```

## 前提

Go・jq・git が要る。guard-bash・guard-home-dir・analyze-tokens は、SessionStart hook が
`~/.claude/plugins/data/harness-usadamasa/bin/` へビルドする。

## guard が止めるとき

- guard の PreToolUse hook は、失敗したらツール呼び出しを止める (`onFailure: "block"`、Claude Code v2.1.295 以降で効く)。
  hook が 10 秒の timeout を超えたときも止まる
- guard-bash と guard-home-dir のバイナリが無い間は、対象のツール呼び出しがすべて deny される。
  deny の文言が、ビルドの log の場所と、ビルドし直す手順を示す

## 利用者の設定

### guard-home-dir の許す置き場

guard-home-dir は、ホーム配下のうち、cwd・`~/.claude`・次の YAML に書いたディレクトリだけを許す。

置き場: `${XDG_CONFIG_HOME:-$HOME/.config}/claude-harness/guard-home-dir.yaml`
(`XDG_CONFIG_HOME` が絶対パスでなければ `$HOME/.config` を使う)

```yaml
allowed_home_directories:
  - src
  - workspace
```

- 値はホームからの相対パス。cwd と `~/.claude` は書かなくても許す
- 次の値が 1 つでもあると、YAML 全体を読めないものとして扱う: `/` か `~` で始まる値・`..` を含む値・空の値・
  ホームそのものを指す値 (`.`)。知らないキーと、一覧でない値も同じ
- YAML が無い・読めないときは、cwd と `~/.claude` だけを許す。deny の文言が YAML の置き場と書き方を示す
- YAML はユーザーが手で編集する。YAML のあるディレクトリへの Write・Edit・NotebookEdit は、YAML の中身によらず deny する

### sandbox

analyze-tokens に sandbox の設定は要らない。
読むのは `~/.claude/projects` の transcript だけで、書き込みとネットワークへの接続をしない。

## 保守

guard-bash の照合外れの deny は、`scripts/sandboxexcluded-denies.sh` が transcript から週ごとに数える。
使いどころは
[sandbox-troubleshooting の maintenance.md](skills/sandbox-troubleshooting/references/maintenance.md) にある。
