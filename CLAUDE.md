# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## このリポジトリの役割

Claude Code plugin の marketplace｡plugin の実体 (skill・hook・daemon のコード) は各 plugin のリポジトリにあり、
ここには置かない｡`.claude-plugin/marketplace.json` で所在を宣言するだけにする｡
plugin 本体の修正依頼が来たら、このリポジトリではなく該当 plugin のリポジトリで作業する｡

## plugin を追加・変更するとき

- `.claude-plugin/marketplace.json` の `plugins` と、README の「plugin 一覧」の表を同じ変更で揃える
- `source` は `{"source": "github", "repo": "<owner>/<repo>"}` とし、`ref` / `sha` でバージョンを固定しない
  (既定ブランチの最新を配る方針)
- 変更後は `claude plugin validate --strict .` を通す
