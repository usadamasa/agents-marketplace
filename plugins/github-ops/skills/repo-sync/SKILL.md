---
name: repo-sync
description: >-
  ghq 配下の作業中リポジトリを main へ追従させる。main を FF 更新して worktree の各ブランチを
  rebase し、増えた差分のうち今の作業トピックに関連するものだけを subagent 経由で蒸留して受け取る。
  push はしない。作業の区切り、久しぶりに触るリポジトリ、「<repo> を pull して」と言われたときに使う。
---

# リポジトリ追従スキル (repo-sync)

## 役割の分担

github-ops に同梱した Go の CLI `repo-scan` が、検出 (`repo-scan`) と同期の実行 (`repo-scan sync`) を担う。
Claude は repo-scan を打って JSON を読み、トピックの確認・キャッチアップ・報告を受け持つ。
fetch・merge・rebase・stash を Claude が git で直接打つことはしない。

前提コマンドは Go (repo-scan のビルド)、ghq、git 2.31 以降。
repo-scan が sandbox の外で動く設定が要る。設定の形は github-ops の README の「利用者の設定」にある。

## repo-scan の打ち方

- `${CLAUDE_PLUGIN_DATA}/bin/repo-scan` は置換された絶対パスのまま、クォートせずに打つ
- 1 回の Bash 呼び出しに repo-scan の 1 文だけを書く。パイプ、リダイレクト、`cd` や変数の前置き、`$(...)` を付けない
- 付けると sandbox の外で動く設定に当たらず、sandbox の中で git の書き込みが落ちる

## repo-scan が守る安全ルール

- push しない。`reset --hard`・`push --force`・`git config` の書き込みをしない
- default branch が FF できなければ触らず、`skipped` で返す
- rebase が止まったら、その場で abort して元の状態に戻し、`needs_manual` で返す。コンフリクトは解決しない
- dirty な変更は stash へ退避して戻す。default branch 側の変更と重なるパスがあれば、そのブランチに手を付けない
- stray ブランチ (どの worktree にも出ていないローカルブランチ) は名前を返すだけで触らない

## repo-scan を確かめる

repo-scan は、github-ops の SessionStart hook がソースの変わったときだけ
`${CLAUDE_PLUGIN_DATA}/bin/repo-scan` へビルドする。
始める前にバイナリがあるかを見る。

```bash
test -x ${CLAUDE_PLUGIN_DATA}/bin/repo-scan
```

無ければ (exit 1) ここで止め、git を直接打つ手順で代わりに進めない。
利用者には次を伝える。

- Go が要る。入っていなければ <https://go.dev/doc/install> から入れる
- 再ビルドは次のコマンドで行う。Claude Code の中なら先頭に `!` を付けて打つ

  ```bash
  CLAUDE_PLUGIN_DATA="${CLAUDE_PLUGIN_DATA}" "${CLAUDE_PLUGIN_ROOT}/scripts/build-repo-scan.sh"
  ```

- hook でのビルドが失敗していれば、原因は `${CLAUDE_PLUGIN_DATA}/build-repo-scan.log` にある

## ワークフロー

対象が 1 本のリポジトリ (「agents-daemon を pull して」) なら、Step 1 から Step 3 を飛ばし、
Step 4 の `sync` に ghq のリポジトリ名 (`name`・`owner/name`・`host/owner/name`) かパスを渡す。
キャッチアップのトピックは、利用者が挙げたときだけ使う。

### Step 1: 同期プランを取得

```bash
${CLAUDE_PLUGIN_DATA}/bin/repo-scan
```

既定は ghq 配下の全リポジトリが対象になる。特定の owner だけに絞るときは
`--owner <name>` を繰り返し指定する (例: `--owner <owner-a> --owner <owner-b>`)。

出力 JSON の `repos[]` が対象のリポジトリ。`repo-scan` は fetch をせず、数字は手元にある
`origin/<default_branch>` との比較になる。

- リポジトリ: `path`・`default_branch`・`main_behind`・`worktrees[]`・`branches[]`
- `branches[]` の要素: `name`・`checked_out_in`・`dirty`・`is_stray`・`behind_main`・`ahead_main`

### Step 2: 概要を提示

対象のリポジトリの一覧を、main の遅れ・各ブランチの dirty/stray 状態とともに利用者へ提示する。
ここでは実行しない (何をするかの予告のみ)。

### Step 3: トピックを推定して確認

作業中 worktree のブランチ名・直近コミット (`git -C <wt> log --oneline -5`)・変更ファイルから
「今のトピック」を推定し、「このトピックで合ってる?」と **一度だけ** 利用者に確認する。

### Step 4: 同期を実行

Step 1 と同じ `--owner` を付けて打つ。全件の同期は fetch を対象の数だけ行うので、
Bash の timeout を 600000 (10 分) にする。途中で打ち切られると、退避した stash が戻らずに残りうる。

```bash
${CLAUDE_PLUGIN_DATA}/bin/repo-scan sync
```

1 本に絞るときは `sync <repo>` と打つ。`<repo>` は scan の対象でなくてよい。

出力 JSON の `repos[]` の要素は次のキーを持つ。

| キー | 中身 |
| ---- | ---- |
| `error` | 空でなければそのリポジトリは何も変えずに終えている (`no_origin`、`fetch: ...`) |
| `main.status` | `fast_forwarded`・`up_to_date`・`skipped` |
| `main.reason` | skip の理由。`diverged`・`ff_failed`・`operation_in_progress`・`no_local_branch`・`no_remote_branch` |
| `main.detail` | `ff_failed` のときの git の出力 |
| `main.old` / `main.new` | FF の前後の SHA。`fast_forwarded` のときだけ入る |
| `branches[]` | worktree に出ているブランチ。`name`・`worktree`・`status`・`reason`・`detail`・`stash` |
| `stray_branches[]` | 触らなかった stray ブランチの名前 |

`branches[].status` は `rebased`・`up_to_date`・`skipped`・`needs_manual` のどれかになる。
`skipped` の `reason` は `operation_in_progress` (rebase などが途中)・`worktree_missing`・`no_local_branch` で、どれも触っていない。
`needs_manual` の `reason` ごとの扱いは次のとおり。

| reason | 状態 | 次の手 |
| ---- | ---- | ---- |
| `rebase_failed` | rebase を abort し、ブランチと dirty は sync の前のまま | コンフリクトの解決を渡す |
| `dirty_overlap` | 何もしていない。`detail` が重なったパス | dirty を commit か片付けてから打ち直すよう利用者に伝える |
| `stash_failed` | 何もしていない | `detail` を添えて利用者に上げる |
| `stash_restore_failed` / `restore_failed` | 元に戻しきれていない。`stash` に退避した変更の SHA がある | worktree のパスと `stash` の SHA を添えて利用者に上げる |

次のどれかに当たったら、repo-scan が sandbox の中で動いている疑いがある。

- `error` や `detail` に `Operation not permitted` や `could not lock` が出る
- 全リポジトリの `error` が同じ `fetch: ...` になる

打ち方を「repo-scan の打ち方」と見比べ、合っていれば README の「利用者の設定」が済んでいないことを利用者に伝えて止める。

### Step 5: 要手動対応を渡す

`rebase_failed` のブランチは、メインのセッションで解決しない。次のどちらかで渡す。

- その worktree を cwd にした別の Claude Code セッションへ、リポジトリ・ブランチ・`default_branch` を
  伝えて rebase とコンフリクトの解決を頼む
- 利用者に worktree のパスとブランチを示し、解決を任せる

### Step 6: キャッチアップ

`main.status` が `fast_forwarded` のリポジトリごとに行う。

1. 変更 path を取得する。

   ```bash
   ${CLAUDE_PLUGIN_DATA}/bin/repo-scan changed --range <old>..<new> <path>
   ```

2. subagent (Explore か general-purpose) に委譲する。渡す情報:
   - リポジトリ名、`<old>..<new>` の範囲、変更 path 一覧、Step 3 で確定したトピック。
   - 指示: 「この範囲の変更のうち今のトピックに関連するものだけを、ファイルパス付きで
     200 語以内に要約して。関連がなければ『関連なし』とだけ返して。」
3. メインでは蒸留サマリだけを受け取る (生 diff はメインに展開しない)。

### Step 7: 最終レポート

- FF したリポジトリ (旧 → 新)、rebase したブランチ、skip したリポジトリとその理由、
  要手動対応と渡した先、stray ブランチ、残った stash の SHA を一覧にする。
- 関連が薄いものも含む全体のキャッチアップを利用者が求めたら、メインが `./tmp` に置くファイルのパスを決める。
  そのパスと Step 6 で渡した情報を新しい subagent に渡し、範囲の変更を全部要約してファイルへ書かせる。
  書き終えたら、そのパスを利用者に提示する。
