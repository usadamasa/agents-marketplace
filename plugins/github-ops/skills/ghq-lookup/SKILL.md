---
name: ghq-lookup
description: リポジトリ URL が提示されたときにローカルの ghq 管理下クローンを優先参照する。GitHub/GitLab 等のリポジトリ URL (HTTPS/SSH) や「このリポジトリのコードを見て」「ソースを調査して」のような依頼時に使用する。WebFetch でリモートを見に行く前にローカルを確認する。
---

# ghq-lookup

リポジトリ URL が提示されたコード調査時に、ghq 管理下のローカルクローンを優先的に参照する。

## Context

- GHQ_ROOT: !`ghq root`

## Step 1: URL 正規化

提示された URL を `host/owner/repo` 形式に正規化する。ホスト部分は GitHub 以外でも残す。

| 入力形式 | 例 | 正規化結果 |
|----------|---|-----------|
| HTTPS | `https://github.com/x-motemen/ghq` | `github.com/x-motemen/ghq` |
| HTTPS (サブパス付き) | `https://github.com/x-motemen/ghq/tree/main/cmd` | `github.com/x-motemen/ghq` |
| HTTPS (.git 付き) | `https://github.com/x-motemen/ghq.git` | `github.com/x-motemen/ghq` |
| SSH | `git@github.com:x-motemen/ghq.git` | `github.com/x-motemen/ghq` |
| GitHub 以外のホスト | `https://gitlab.com/gitlab-org/cli` | `gitlab.com/gitlab-org/cli` |
| 短縮形 | `github.com/x-motemen/ghq` | `github.com/x-motemen/ghq` |

正規化は次のロジックで行う。

```bash
# HTTPS / SSH の URL からホスト/オーナー/リポジトリを抽出
REPO_PATH=$(echo "$URL" | sed -E 's|^https?://||; s|^git@([^:]+):|\1/|; s|\.git$||; s|/tree/.*||; s|/blob/.*||; s|/pull/.*||; s|/issues/.*||; s|/actions/.*||; s|/releases/.*||' | cut -d'/' -f1-3)
```

URL ではなくリポジトリ名だけを渡されたときは、
Step 1 の代わりに「[名前だけで探すとき](#名前だけで探すとき)」の手順を行う。

## Step 2: ローカル存在確認

```bash
GHQ_ROOT=$(ghq root)
test -d "${GHQ_ROOT}/${REPO_PATH}"
```

## Step 3A: ローカルにある場合

ローカルクローンが存在するときは、次のように参照する。

1. Read ツールで `${GHQ_ROOT}/${REPO_PATH}` 配下のファイルを直接読む
2. 継続的に多数のファイルを参照する場合は `/add-dir ${GHQ_ROOT}/${REPO_PATH}` をユーザーに提案する
3. WebFetch は使わない (ローカルから読むほうが速く、コンテキストの消費も少ない)

### 読む前にブランチを確かめる

仕様や実装の根拠として引くのは `origin/main` の内容だけにする。
作業中のブランチの中身を確かめたいときは「[作業中のブランチを読むとき](#作業中のブランチを読むとき)」に従う。
clone は worktree を切らない限り全セッションで共有される。別のセッションが feature ブランチを
checkout したまま置いていることがあり、そのままでは未着地の記述を `origin/main` の内容として読む。
未着地の PR の記述を仕様や実装の根拠にすると、その PR が変わったり落ちたときに実装だけが取り残される。

```bash
git -C "${GHQ_ROOT}/${REPO_PATH}" status --short --branch
git -C "${GHQ_ROOT}/${REPO_PATH}" log --oneline -3
```

feature ブランチが出ていたら、ブランチを切り替えずに `origin/main` から読む。

```bash
git -C "${GHQ_ROOT}/${REPO_PATH}" fetch origin
git -C "${GHQ_ROOT}/${REPO_PATH}" show origin/main:<path>
```

共有の clone では、ブランチを切り替えず、ファイルも書き換えない。
書き換える作業が要るなら、その clone を cwd にしたセッションで `EnterWorktree` を使って隔離する。

## Step 3B: ローカルにない場合

ローカルクローンが存在しないときは、次の手順で取得する。

1. 確認を取らずに `ghq get` でクローンする。
   ```bash
   ghq get "${REPO_PATH}"
   ```
2. クローン完了後、Step 3A と同様にローカル参照に移行する。

## 名前だけで探すとき

`ghq list` に名前の一部を渡すと、部分一致で候補を絞れる。

```bash
ghq list repo-name
```

候補が 1 件ならそのパスで Step 3A に進む。複数ヒットした場合はユーザーに選択を促し、無ければ URL を聞く。

## 作業中のブランチを読むとき

clone の本体とは別に、作業中のブランチが git worktree に出ていることがある。
そのブランチの内容を読むときは、配置先がリポジトリごとに異なるため、パスを推測しない。
`git -C <repo> worktree list` で worktree のパスを引いて、そこを読む。

## WebFetch との使い分け

- コード調査、ファイル内容の確認 → ghq-lookup (ローカル優先)
- ローカルにないリポジトリの README やドキュメントの概要確認 → WebFetch でも可
- API ドキュメント、Issue、PR の内容確認 → `gh` CLI または WebFetch
