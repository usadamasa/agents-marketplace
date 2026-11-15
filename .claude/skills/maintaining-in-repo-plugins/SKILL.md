---
name: maintaining-in-repo-plugins
description: >-
  この marketplace の `plugins/<name>/` に置いた plugin を変えるとき、
  skill を `plugins/<name>/skills/` へ移すとき、plugin を足すときに使う。
---

# maintaining-in-repo-plugins

このリポジトリ内の plugin (`marketplace.json` の `source` が `./plugins/<name>`) を変える PR の手順。
外部リポジトリの plugin と、plugin 一般の作り方 (変数の置換、名前解決) は packaging-claude-plugins skill の担当。

## version を毎回上げる

`plugins/<name>/` 配下を 1 文字でも変えた PR では、その plugin の `plugin.json` の `version` を同じ PR で上げる。
`version` が変わらないと、`claude plugin update` も自動更新も install 済みのコピーを置き換えない。
skill の本文・references・scripts・`plugin.json` の description のどれを変えても同じ。

- 形式は calver の `YYYY.0M0D.MICRO`。月日は 0 埋めし、MICRO は 0 埋めしない (例 `2026.1115.0`)。
- 今の値の日付が今日より前なら、今日の日付の `.0` にする。今日の日付なら MICRO を 1 足す。
- 上げるのは変えた plugin だけ。変えていない plugin の `version` は触らない。
- `marketplace.json` のエントリには `version` を書かない (`plugin.json` の値が優先され、食い違うと validate が落ちる)。

## skill を移すとき

1. 中身を公開してよいか確かめる。このリポジトリは公開されている。
   仕事用の skill (勤務先のサービス名・社内の手順・社内 URL を含むもの) は移さず、agents-config の
   private marketplace に残す。個人の情報 (1Password の vault や item の名前、メールアドレス、端末のパス) は
   一般化してから移す。
2. `plugins/<name>/skills/<skill>/` に置く。`~/.claude/skills/<skill>` を直書きしたパスは `${CLAUDE_SKILL_DIR}` へ替える。
3. 同じ plugin の skill や agent を呼ぶ箇所は `<plugin>:<name>` で書く (bare 名だと `~/.claude/skills/` の旧コピーに当たる)。
4. 上の「version を毎回上げる」に従って `version` を上げる。
5. 移した元 (agents-config) の参照の書き換えと旧コピーの削除は、agents-config 側の PR で行う。

## plugin を足すとき

1. `plugins/<name>/.claude-plugin/plugin.json` を置く。`name`・`version` (今日の `YYYY.0M0D.0`)・`description`・
   `author`・`repository`・`license` を書く。`version` が無いと `validate --strict` が落ちる。
2. `.claude-plugin/marketplace.json` の `plugins` に `{"name", "source": "./plugins/<name>", "description", "category"}` を足す。
3. README の「plugin 一覧」の表に行を足す。置き場の列は `[plugins/<name>](plugins/<name>)` にする。
4. 名前が他の marketplace の plugin と重ならないか確かめる (`github` は公式 marketplace に同名がある)。
   同名の plugin を両方有効にすると、skill の `<plugin>:` 名前空間がぶつかる。

## 検査

PR を出す前に両方を通す。

```sh
claude plugin validate --strict .
./scripts/validate-plugins.sh
```

- 1 つ目は marketplace.json と、そこから辿れる plugin.json を検査する。`source` のディレクトリが無くても通る。
- 2 つ目は相対パスの `source` を 1 つずつ `validate --strict` にかけ、ディレクトリが無ければ落ちる。
- `ICON_MISSING` の警告は結果に影響しない。
- `version` を上げ忘れても検査は落ちない。差分に `plugins/<name>/` があるなら、その `plugin.json` の差分も
  あるかを自分で見る。
