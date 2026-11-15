---
name: introducing-tagpr
description: >-
  Claude Code plugin のリポジトリに Songmu/tagpr を導入するときに使う。
  plugin.json の version (calver) の手動 bump をやめてリリース PR に任せたい、
  tagpr を入れたのにリリース PR の plugin.json が更新されない、base_tag が v0.0.0 になる、
  バージョンが 2026.0927.00 のようにゼロ埋めされない、feature PR が version 検査の CI で落ち続ける、
  リリース PR の branch が tagpr-from-0.0.0 になる、リリース PR が CHANGELOG.md しか変えない、
  バージョンが semver (1.2.0) のリポジトリへ入れたい、といった症状にも引く。
argument-hint: <対象リポジトリの絶対パス>
---

# introducing-tagpr

Claude Code plugin のリポジトリへ tagpr のファイルを置き、ファイルだけでは済まない手作業を案内する。

対象は `.claude-plugin/plugin.json` の `version` が calver (`YYYY.0M0D.MICRO`) のリポジトリ。
バージョンが semver などの別の形なら、手順 2 で calver へ移してから入れる。
calver の書式は maintaining-in-repo-plugins skill に従う。

引数は対象リポジトリの絶対パス。以下 `<repo>` と書き、`<owner>/<name>` は plugin.json の `repository` から取る。

作業は最初から `<repo>` を cwd にしたセッションの worktree で行う。cwd が `<repo>` と別のリポジトリなら、
ファイルを置く前に `<repo>` を cwd にした別のセッションへ丸ごと渡す (渡し方は agents-daemon:peer-sessions skill)。

## 1. 前提を確かめる

tagpr は `calendarVersioning` を設定すると、tag のうち次の条件をすべて満たすものだけを前回のバージョンとして扱う。

- `calendarVersioning` の書式 (`YYYY.0M0D.MICRO`) で parse できる
- 先頭の `v` の有無が `vPrefix` (`false`) と一致する

該当する tag が 1 本も無いと、root commit を起点にして現在のバージョンを `v0.0.0` と見なし、
versionFile の中の `0.0.0` を探す。
置き換え先が見つからないので、リリース PR の branch は `tagpr-from-0.0.0` になり、
変わるファイルは `CHANGELOG.md` だけになる。
semver (`1.2.0`) や v 付き (`v1.2.0`) の tag を打っても、tag が無いのと同じ結果になる。

- version を持つファイルを全部挙げる。`.claude-plugin/plugin.json` のほか、単一 plugin の
  `.claude-plugin/marketplace.json` や Codex 向けの `.codex-plugin/plugin.json` にもバージョンがあることがある。
  ファイルごとにバージョンが違えばユーザーへ上げる。
- バージョンが `YYYY.0M0D.MICRO` の形なら手順 3 へ進む。
- バージョンが semver など別の形のとき、または `plugin.json` に `version` が無いときは手順 2 へ進む。
  そのバージョンを `plugin.json` へ書き写して起点の tag を打つ進め方は、上の理由で必ず失敗する。
  依頼者に「バージョンを `plugin.json` へ移して」と言われていても同じ。
- `<repo>/.tagpr` と `<repo>/.github/workflows/tagpr.yaml` がまだ無い。あれば差分をユーザーに見せて判断を仰ぐ。
- バージョンを手で上げるスクリプトや、バージョンを上げたかを検査する CI があれば、扱いをユーザーに問い合わせる。

## 2. バージョンを calver へ移す (バージョンが calver でないときだけ)

バージョンの付け方を変える作業なので、calver へ移してよいかをユーザーに問い合わせる。同意が無ければ導入を止める。

- 手順 1 で挙げたファイル全部の `version` を、今日の日付の `.0` にする (書式は冒頭の calver)。
  `plugin.json` に `version` が無ければ足す。
- この変更だけで PR を作り、手順 3 のファイルより先に main へ merge する。
  起点の tag はこのバージョンが入った main の commit に打つ。workflow と同じ PR にすると、
  merge した時点で workflow が起点の tag 無しで tagpr を実行し、`tagpr-from-0.0.0` のリリース PR を作る。
- 既存の semver の tag は消さない。tagpr が無視するだけで、害は無い。
- 最初のリリース PR が起点の tag と同じ日に出ると、提案のバージョンは `YYYY.0M0D.1` になる。正常な動き。

## 3. ファイルを置く

`${CLAUDE_SKILL_DIR}/templates/` のファイルを Read し、Write で置く。
中身は書き換えない。ただし `.tagpr` の `versionFile` には、手順 1 で挙げたファイルをカンマ区切りで全部並べる
(例: `.claude-plugin/plugin.json,.codex-plugin/plugin.json`)。並べ忘れたファイルは古いバージョンのまま残る。

| template | 置き先 |
| ---- | ---- |
| `.tagpr` | `<repo>/.tagpr` |
| `tagpr.yaml` | `<repo>/.github/workflows/tagpr.yaml` |
| `release.yml` | `<repo>/.github/release.yml` (既にあれば `exclude.labels` に `tagpr` を足すだけにする) |

action の固定は `github-ops:using-gh-actions-lock` skill に従う。
`actions.lock` があれば `gh actions-lock` で lockfile を更新する。

AGENTS.md / CLAUDE.md / README に tag を手で打つリリース手順があれば、
tagpr がリリース PR を作ること・`version` を手で上げないことの 2 行に置き換える。手順を細かく書き直さない。

## 4. 手作業を一覧で渡す

`<owner>/<name>` とバージョンを埋めて、次の一覧をユーザーへ渡す。

1〜3 は workflow を main へ merge する前に済ませる。
どれも Claude のセッションからは打たない。tag の push は外へ出る操作、secret は guard が止める。
コマンドはユーザーがプロンプトに `! <command>` と打って実行する形で渡す (出力がそのまま会話に載る)。

tagpr 用の GitHub App は、利用者の全リポジトリにインストール済みの前提で進める。
App の client-id と、private key を置いた 1Password の参照 (`op://<vault>/<item>/<field>`) は、
利用者のグローバル CLAUDE.md の「tagpr」節から読む。節が無ければユーザーに尋ねる。

1. Variable を登録する:
   `! gh variable set TAGPR_CLIENT_ID --repo <owner>/<name> --body <client-id>`
2. Secret を登録する:
   `! op read "<private key の参照>" | gh secret set TAGPR_PRIVATE_KEY -R <owner>/<name>`
3. 起点の tag を打つ。plugin.json の現在のバージョンと同じ名前 (v 無し) を、そのバージョンが入った main の commit に付ける。
   手順 2 を通ったなら、その PR が入った commit になる。
   - commit を探す。出力の最後の行が、そのバージョンを入れた commit になる:
     `git log --format='%H %s' -S'"version": "<バージョン>"' origin/main -- .claude-plugin/plugin.json`
   - 何も出ないときは plugin.json の `"version":` の後の空白を確かめ、`-S` の文字列をファイルに合わせる。
     HEAD へ付けて済ませない。
   - `! git tag <バージョン> <commit>` → `! git push origin <バージョン>`
   - calver の書式に合わない tag は無視される (手順 1)。tag の名前が `YYYY.0M0D.MICRO` の形か打つ前に見直す。
4. ファイルを置いた変更を PR にして merge する。
5. 最初のリリース PR で確かめる。
   - 本文の `base_tag` が 3 の tag になっている
   - branch が `tagpr-from-<3 の tag>` になっている (`tagpr-from-0.0.0` なら下の「起点の tag が無視されたとき」へ)
   - 提案のバージョンが `YYYY.0M0D.MICRO` の形 (例: `2026.0927.0`)
   - 変わるファイルが、`versionFile` に並べたファイル全部と `CHANGELOG.md`

## 起点の tag が無視されたとき

リリース PR の branch が `tagpr-from-0.0.0` になり、`CHANGELOG.md` しか変わらないときの回復手順。
起点の tag が calver の書式に合わないまま workflow を merge すると、この状態になる (手順 1)。

- リリース PR の branch に、`versionFile` のファイル全部を提案のバージョンへ書き換える commit を 1 つ足し、そのまま merge する。
  tagpr は merge 時に versionFile のバージョンで tag と Release を作るので、以降は通常どおり回る。
- commit を足してから merge するまで、他の PR を main へ入れない。
  tagpr がリリース PR を作り直すと、足した commit が残るとは限らない。
- `CHANGELOG.md` には root commit からの変更が全部載る。要らなければ同じ commit で削る。

## 設定の根拠

| 箇所 | 理由 |
| ---- | ---- |
| `calendarVersioning = YYYY.0M0D.MICRO` | `true` だけでは書式が既存のバージョンと揃わない。MICRO は `%d` 固定でゼロ埋めできず、`0MICRO` と書くと `0M` (月) と解釈されて弾かれる |
| `vPrefix = false` | tag と plugin.json の `version` を同じ文字列にする。v 付きの tag は起点として読まれなくなる |
| `versionFile` のカンマ区切り | バージョンを持つファイルが複数あると、1 つだけ並べても他のファイルは古いバージョンのまま残る |
| `client-id` と `vars.TAGPR_CLIENT_ID` | create-github-app-token v3 で `app-id` は deprecated |
| `permission-*` | App の権限のうち tagpr が要るものだけをトークンに載せる |
| App のトークン | リポジトリ設定の「Allow GitHub Actions to create and approve pull requests」が要らない。リリース PR から他の workflow も起動する |
| checkout に `fetch-depth` 無し | tagpr が自分で `fetch --unshallow` する |
| `release.yml` の `exclude` | リリースノートからリリース PR 自身を外す |
