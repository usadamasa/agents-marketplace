---
name: introducing-tagpr
description: >-
  Claude Code plugin のリポジトリに Songmu/tagpr を導入するときに使う｡
  plugin.json の version (calver) の手動 bump をやめてリリース PR に任せたい､
  tagpr を入れたのにリリース PR の plugin.json が更新されない､base_tag が v0.0.0 になる､
  版が 2026.0927.00 のようにゼロ埋めされない､feature PR が version 検査の CI で落ち続ける､
  といった症状にも引く｡
argument-hint: <対象リポジトリの絶対パス>
---

# introducing-tagpr

Claude Code plugin のリポジトリへ tagpr のファイルを置き､ファイルだけでは済まない手作業を案内する｡

対象は `.claude-plugin/plugin.json` の `version` が calver (`YYYY.MMDD.NN`) のリポジトリ｡
版の付け方そのものは packaging-claude-plugins skill の担当｡

引数は対象リポジトリの絶対パス｡以下 `<repo>` と書き､`<owner>/<name>` は `gh repo view` で得る｡

## 1. 前提を確かめる

- `<repo>/.claude-plugin/plugin.json` の `version` が `YYYY.MMDD.NN` の形になっている｡
  無い・形が違うときは導入を止めてユーザーへ上げる (tagpr の置き換え先の文字列が無い)｡
- `<repo>/.tagpr` と `<repo>/.github/workflows/tagpr.yaml` がまだ無い｡あれば差分をユーザーに見せて判断を仰ぐ｡

## 2. ファイルを置く

`${CLAUDE_SKILL_DIR}/templates/` の 3 つを Read し､Write で置く｡中身は書き換えない｡

| template | 置き先 |
| ---- | ---- |
| `.tagpr` | `<repo>/.tagpr` |
| `tagpr.yaml` | `<repo>/.github/workflows/tagpr.yaml` |
| `release.yml` | `<repo>/.github/release.yml` (既にあれば `exclude.labels` に `tagpr` を足すだけにする) |

`<repo>/.pinact.yaml` があれば､`<repo>` で `pinact run` を打って action を SHA に固定する｡

template の action は版のタグで書いてある｡`Songmu/tagpr@v1` のような major タグへ緩めない｡
pinact の `min_age` が公開 7 日未満の版を弾くため､最新版へ寄せると pin に失敗する｡

## 3. 手動 bump の仕組みを消す

`<repo>` で `rg -n 'version' .github/workflows scripts Taskfile.yml` を引き､次のものを探す｡

- plugin.json の `version` を書き換えるスクリプトや task
- PR で「版を上げたか」を検査する CI job

見つけたものは `git -C <repo> rm <path>` で消す｡残すと､版を上げない feature PR が落ち続ける｡

その job が branch protection の required status checks に入っていれば､外す作業を手順 4 の一覧の 5 の前に足す｡

以後 plugin.json の `version` は手で上げない｡tagpr は最新 tag の版の文字列を plugin.json から探して置き換えるので､
手で上げると見つからずに更新されない｡

## 4. 手作業を一覧で渡す

`<owner>/<name>` と版を埋めて､次の一覧をユーザーへ渡す｡

1〜4 は workflow を main へ merge する前に済ませる｡
どれも Claude のセッションからは打たない｡tag の push は外へ出る操作､secret は guard が止める｡

1. GitHub App `usadamasa-tagpr` を対象リポジトリにインストールする:
   <https://github.com/settings/installations/101558454> の Repository access に足す｡
2. Variable を登録する:
   `gh variable set TAGPR_CLIENT_ID --repo <owner>/<name> --body Iv23liqxSJK4kOrLJFj4`
3. Secret を登録する (手元のターミナルで打つ):
   `op read "op://Personal/usadamasa-tagpr/private key" | gh secret set TAGPR_PRIVATE_KEY -R <owner>/<name>`
4. 起点の tag を打つ｡plugin.json の現在の版と同じ名前 (v 無し) を､その版が入った main の commit に付ける｡
   - commit を探す｡出力の最後の行が､その版を入れた commit になる:
     `git -C <repo> log --format='%H %s' -S'"version": "<版>"' origin/main -- .claude-plugin/plugin.json`
   - 何も出ないときは plugin.json の `"version":` の後の空白を確かめ､`-S` の文字列をファイルに合わせる｡
     HEAD へ付けて済ませない｡
   - `git -C <repo> tag <版> <commit>` → `git -C <repo> push origin <版>`
   - tag が 1 本も無いと tagpr は `v0.0.0` を起点にし､plugin.json の中の `0.0.0` を探すため版が更新されない｡
5. ファイルを置いた変更を PR にして merge する｡
6. 最初のリリース PR で確かめる｡
   - 本文の `base_tag` が手順 4 の tag になっている
   - 提案の版が `YYYY.0M0D.N` の形 (例: `2026.0927.0`)
   - 変わるファイルが `.claude-plugin/plugin.json` と `CHANGELOG.md` だけ

## 設定の根拠

| 箇所 | 理由 |
| ---- | ---- |
| `calendarVersioning = YYYY.0M0D.MICRO` | `true` だけでは書式が既存の版と揃わない｡MICRO は `%d` 固定でゼロ埋めできず､`0MICRO` と書くと `0M` (月) と解釈されて弾かれる |
| `vPrefix = false` | tag と plugin.json の `version` を同じ文字列にする |
| `client-id` と `vars.TAGPR_CLIENT_ID` | create-github-app-token v3 で `app-id` は deprecated |
| `permission-*` | App の権限のうち tagpr が要るものだけをトークンに載せる |
| App のトークン | リポジトリ設定の「Allow GitHub Actions to create and approve pull requests」が要らない｡リリース PR から他の workflow も起動する |
| checkout に `fetch-depth` 無し | tagpr が自分で `fetch --unshallow` する |
| `release.yml` の `exclude` | リリースノートからリリース PR 自身を外す |
