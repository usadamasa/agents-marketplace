---
name: managing-github-projects
description: 自分のリポジトリ群を 1 つの GitHub Project で横断して整理するときに使う。 週次の棚卸し、Project に載っていない open な issue / PR の triage、 今週やることの選定、アイデアを draft item で置く、draft を issue にする、 リリース待ちのプラグイン (tagpr の Release PR) の確認が対象。
---

# managing-github-projects

利用者のリポジトリ群を 1 つの GitHub Project に集約し、横断で優先順位を決める。
Project の設定は固定で、この skill は日々の運用 (triage・計画・リリース確認・アイデアの置き方) を扱う。

## 利用者の値を確かめる

Project と対象リポジトリは利用者ごとに違う。利用者のグローバル CLAUDE.md (`~/.claude/CLAUDE.md`) に書いた値を使う。

```markdown
## GitHub Project

- owner: `<owner>`
- Project の番号: `<number>`
- 対象リポジトリと Kind の一覧: `<owner>/<repo>` の `<path>` (タブ区切り。1 列目がリポジトリ名、2 列目が Kind)
```

この skill が扱うのは一覧にあるリポジトリだけになる。
書いていなければ、次のコマンドで番号を調べて利用者に確かめ、CLAUDE.md に書くかを聞く。
対象リポジトリの一覧が無ければ、`gh repo list <owner>` の結果から利用者と決める。

```sh
gh project list --owner <owner>
```

一覧は次のコマンドで読む。

```sh
gh api -H "Accept: application/vnd.github.raw" repos/<owner>/<repo>/contents/<path>
```

以下のコマンドの `<owner>` と `<number>` は、この値に置き換えて打つ。

## Project の構成

| 項目 | 値 |
| ---- | ---- |
| Status | Todo / In Progress / Done。進み具合だけを表す |
| Priority | P0 (壊れている・使う人が困る) / P1 (次にやる) / P2 (いつか)。空は未振り分け (Inbox) |
| Kind | リポジトリの種類 (例: Plugin / Harness / CLI / Other)。リポジトリから決まる |

未振り分けは Priority が空であることで表す。
Project 既定の workflow が追加した item の Status を自動で Todo にするので、Status では表せない。
triage で Priority まで埋めると、plan で中身を見て決めた Priority と、triage で中身を見ずに付けた Priority の
見分けが付かなくなる。

## triage: 未登録の issue / PR を拾う

未登録の issue / PR を定期的に Project へ追加する workflow を置いているなら、手動の triage は
自動追加の漏れを拾うときと、次の実行を待たずに入れたいときだけ行う。
数件の操作は `gh project` サブコマンドで行う。
item は URL で、フィールドは名前で指定する (`--url` と `--field` / `--value`)。

1. Project に未登録の open な issue / PR を検索で直接取る。突き合わせは検索修飾子 `-project:` に任せる。
   `--repo` は一覧の各行から `--repo <owner>/<1 列目>` を作って並べる (`#` で始まる行は飛ばす)。
   自動追加の workflow があるなら、検索条件をそちらとそろえる。

   ```sh
   gh search issues --repo <owner>/<repo-a> --repo <owner>/<repo-b> ... \
     --state open --include-prs --limit 500 \
     --json id,url,title,repository,isPullRequest,updatedAt \
     -- -project:<owner>/<number> -author:app/dependabot -label:tagpr
   ```

   - 否定の修飾子は `-` で始まるので、フラグを全部書いた後に `--` を置いて渡す。
     `--` より後ろにフラグを書くと検索語として扱われ、GitHub 全体を検索してしまう
   - 修飾子を 1 つの引数にまとめると `gh` が全体を引用符で囲み、`Invalid search query` で落ちる
   - 既定の `--limit` は 30 なので、31 件目以降は結果に入らない
   - dependabot の PR はリポジトリ側でまとめて処理し、`tagpr` の Release PR は release-check で扱う。
     どちらも Project に載せない
2. 結果を `gh project item-add <number> --owner <owner> --url <URL>` で追加する。
3. `gh project item-edit <number> --owner <owner> --url <URL> --field Kind --value <Kind>` で Kind だけ埋める。
   値は一覧の 2 列目。Priority は空のまま残す。
4. 追加した件数をリポジトリ別に報告する。

### 件数が多いとき

5 件を超える書き換えは、二次レート制限に当たらないよう、
aliased mutation を 1 リクエストへまとめて `gh api graphql` で送る。リクエストは並列に投げず、順番に送る。

1. ID を取得する。content の node ID は triage の検索結果の `id` を使う。

   ```sh
   gh project view <number> --owner <owner> --format json --jq .id
   gh project field-list <number> --owner <owner> --format json --jq '.fields[] | select(.options) | {name, id, options}'
   ```

2. 追加の mutation を書くファイルを作る。出力されたパスを控え、以降のコマンドの `<file>` にはそのパスを書く
   (shell の変数は次の呼び出しへ持ち越せないことがある)。

   ```sh
   mktemp "${TMPDIR:-/tmp}/triage-add.XXXXXX"
   ```

   そのファイルへ mutation を書く。1 ファイル 50 件までにし、ID は mutation の中に直接書く。

   ```graphql
   mutation {
     a0: addProjectV2ItemById(input: {projectId: "PVT_...", contentId: "I_..."}) { item { id } }
     a1: addProjectV2ItemById(input: {projectId: "PVT_...", contentId: "PR_..."}) { item { id } }
   }
   ```

3. 送って、alias と item ID の対応を受け取る。

   ```sh
   gh api graphql -F query=@<file> --jq '.data | to_entries[] | [.key, .value.item.id] | @tsv'
   ```

4. 同じ要領で `updateProjectV2ItemFieldValue` を並べたファイルを書き、Kind を埋める。

   ```graphql
   mutation {
     k0: updateProjectV2ItemFieldValue(input: {projectId: "PVT_...", itemId: "PVTI_...", fieldId: "PVTSSF_...", value: {singleSelectOptionId: "..."}}) { projectV2Item { id } }
   }
   ```

5. 送り終えたら `rm <file>` で消す。

### 自動で付いてくる sub-issue

Project 既定の workflow が、追加した issue の sub-issue も Project に入れる。
閉じた sub-issue も入り、Status は Done、Kind は空になる。
Done なので view の邪魔にはならず、Kind は埋めずに残す。Kind の抜けを数えるときは Status が Done のものを除く。

## plan: 振り分けと今週の選定

未振り分けは `gh project item-list <number> --owner <owner> --query "no:priority" --limit 500 --format json` で取る。
1 件ずつ Priority の案を作り、利用者に表で見せて合意を取ってから書き込む。
長く動いていない issue は、close するか Project から外すかを利用者に聞く。
対応しないと決めたものは、Priority を付けずに issue を close するか item-archive する。

Priority の案はリポジトリの tier を踏まえて作り、案の表に tier の列を置く。
tier は GitHub topic の `tier-1` (積極的に開発) / `tier-2` (維持) / `tier-3` (放置) で表す。
リポジトリごとの tier は写さず、その都度 topic から取得する。

```sh
gh repo list <owner> --limit 200 --json name,repositoryTopics \
  --jq '.[] | [.name, ([.repositoryTopics[]?.name | select(startswith("tier-"))][0] // "未宣言")] | @tsv'
```

| tier | Priority の案 |
| ---- | ---- |
| 問わない | 壊れている・使う人が困るものは P0 |
| `tier-1` | P1 の候補 |
| `tier-2` | 内容で決める |
| `tier-3` | P2。close / item-archive も選択肢として併記し、利用者がそちらを選んだら Priority を付けない |
| 未宣言 | `tier-2` として扱い、tier の列には「未宣言」と書く |

topic を使っていない利用者では全件が未宣言になり、内容だけで決めることになる。

今週の候補は、Priority を書き込んだ後に次の順で 3 件まで選ぶ。
初回は全件が未振り分けなので、Priority の合意が先になる。

1. P0 はすべて (tier を問わない)
2. release-check でリリース待ちが見つかったプラグイン (Release PR を候補の表に併記するだけで、リリースを勧めない)
3. P1 を tier の順 (`tier-1` → `tier-2`) に、同じ tier 内は Kind の順 (Project の選択肢の並び) に、
   同じ Kind 内は古い順

2 は tier を問わず併記する。

選んだものを In Progress にするのは、利用者が合意してから。

## release-check: リリース待ちのプラグイン

```sh
gh search prs --owner <owner> --state open --label tagpr --json repository,title,url,updatedAt
```

結果には対象外のリポジトリ (fork など) も混ざる。一覧にあるものだけを残す。
リリース待ちとして扱うのは Kind が Plugin の Release PR で、それ以外の Kind は参考として併記する。
マージがそのままリリースになるので、マージは利用者の指示があるときだけ行う。

tagpr の有無は一覧で持たず、その都度 `.tagpr` の有無で判定する。

```sh
gh search code --owner <owner> --filename .tagpr --json repository --jq '.[].repository.nameWithOwner'
```

Kind が Plugin なのにこの結果に出ないリポジトリは、最新タグ以降のコミット数でリリース待ちかを判断する。

```sh
gh api repos/<owner>/<repo>/tags --paginate --jq '.[].name'
gh api repos/<owner>/<repo>/compare/<tag>...main --jq '.ahead_by'
```

tags API の並びは作成順ではないので、取ったタグ名をバージョンの順に並べて最新を選ぶ。
タグが空なら「未リリース」と報告する。`gh release view` はリリースが無いと `release not found` で落ちるので使わない。

## アイデアを置く・issue にする

issue にする前の思いつきは draft item で置く。

```sh
gh project item-create <number> --owner <owner> --title "<やりたいこと>" --body "<背景>"
```

Kind は対象リポジトリが決まっていれば埋め、決まっていなければ Other にする。

着手が決まったら issue に変換する。
`gh` には draft を変換するコマンドが無いので、次の順で置き換える。

1. 立てるリポジトリを決めて issue を作る。**REQUIRED SUB-SKILL:** github-ops:filing-issues
2. その issue を item-add し、draft にあった Priority・Status・Kind を同じ値で付け直す
3. `gh project item-delete <number> --owner <owner> --id <draft の item ID>` で draft を消す。
   item ID は `item-list --format json` の `.items[] | select(.content.type == "DraftIssue") | .id` で取る。

## 対象リポジトリを足す・外す

一覧に行を足す・消す。自動追加の workflow が一覧を読むなら、次の実行から対象が変わる。
外したリポジトリの item は `gh project item-archive` で archive する。

## view (Web UI で 1 回だけ作る)

`gh` に view を作るコマンドは無い。Project の Web 画面で次の view を作る。

| view | レイアウト | フィルタ・並び |
| ---- | ---- | ---- |
| Inbox | Table | `no:priority`、Kind で group |
| This week | Table | `status:"In Progress"`、Priority で sort |
| By kind | Board | Status 列、Kind で group |

`gh project view` の JSON には view が含まれないので、作った view の設定は GraphQL で確かめる。
次の query を「件数が多いとき」と同じ要領で一時ファイルに書き、`gh api graphql -F query=@<file>` で送る。

```graphql
query {
  user(login: "<owner>") { projectV2(number: <number>) { views(first: 20) { nodes { name layout } } } }
}
```

## うまくいかないとき

| 症状 | 原因と対処 |
| ---- | ---- |
| `your authentication token is missing required scopes [read:project]` | 利用者に `gh auth refresh --hostname github.com --scopes project` を実行してもらう。Claude Code の `!` 経由で打つときは `--hostname` が必須 |
| item-edit で選択肢が見つからない | `gh project field-list <number> --owner <owner>` で実際の選択肢名を見る。選択肢の追加・改名は `gh` ではできないので Web UI で行う |
| item-edit で項目が見つからない | 先に item-add していない。triage の手順 2 を先に行う |
| `unknown owner type` (直前に `GraphQL: API rate limit exceeded` が出ている) | 二次レート制限。`gh api rate_limit` の `graphql.reset` まで待ってから「件数が多いとき」の手順で再開する |
| `unknown owner type` (rate limit のエラーが出ていない) | organization の解決が NOT_FOUND 以外のエラーで落ちている (`gh project` は login から user と organization を 1 回のクエリで取得する)。多くは token に `read:org` が無いときで、`--owner @me` で回避する |
| Actions など scope を絞った token で `gh project` が scope 不足で落ちる | `gh project` は Project を読むだけでも `read:org` と `read:discussion` を要求する。scope を `repo` と `project` に抑えるなら `gh api graphql` で必要な項目だけ取得する |
