# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## このリポジトリの役割

Claude Code plugin の marketplace。plugin は 2 種類ある。

- 外部リポジトリの plugin: 実体 (skill・hook・daemon のコード) は各 plugin のリポジトリに置き、
  ここでは `.claude-plugin/marketplace.json` で所在を宣言するだけにする。本体の修正はそのリポジトリで行う。
- このリポジトリ内の plugin: `plugins/<name>/` に置く。

リポジトリは公開されている。仕事用の plugin と、業務や個人の情報を含む skill は置かない。

## plugin を追加・変更するとき

- `.claude-plugin/marketplace.json` の `plugins` と、README の「plugin 一覧」の表を同じ変更で揃える
- 外部リポジトリの plugin の `source` は `{"source": "github", "repo": "<owner>/<repo>"}` とし、
  `ref` / `sha` でバージョンを固定しない (既定ブランチの最新を配る方針)
- このリポジトリ内の plugin の `source` は `"./plugins/<name>"` とし、`plugin.json` は `plugins/<name>/.claude-plugin/` に置く
- 変更後は `claude plugin validate --strict .` と `./scripts/validate-plugins.sh` を通す

## このリポジトリ内の plugin の version

`plugins/<name>/` を変えた PR では version を毎回上げる。
上げ方と、plugin の追加・skill の移行は maintaining-in-repo-plugins skill に従う。
