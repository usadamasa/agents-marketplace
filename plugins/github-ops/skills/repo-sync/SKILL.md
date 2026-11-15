---
name: repo-sync
description: >-
  ghq 配下の作業中リポジトリを main へ追従させる。main を FF 更新して全ブランチを
  rebase 試行し、増えた差分のうち今の作業トピックに関連するものだけを subagent 経由で
  蒸留して受け取る。push はしない。作業の区切りや、久しぶりに触るリポジトリで main との差が
  気になったときに使う。
---

# リポジトリ追従スキル (repo-sync)

## 役割の分担

作業の区切りで手動起動し、作業中のリポジトリを安全に main へ追従させる。検出は github-ops に同梱した Go の CLI
`repo-scan` が行い、実行 (FF→rebase)・コンフリクト解決・キャッチアップは Claude が駆動する。

前提コマンドは Go (repo-scan のビルド)、ghq、git 2.24 以降。

## 安全ルール (厳守)

- push は一切しない (ローカル更新のみ)。
- `git push --force` / `git reset --hard` は使わない。main が FF 不可なら skip して報告。
- `git config` の書き込みはしない。
- stash した変更は必ず元の worktree に pop して戻す。pop でコンフリクトしたら止まって一緒に解決。
- stray ブランチ (どの worktree にも出ていないローカルブランチ) は触らず報告のみ。

## repo-scan を確かめる

repo-scan は、github-ops の SessionStart hook がソースの変わったときだけ
`${CLAUDE_PLUGIN_DATA}/bin/repo-scan` へビルドする。
始める前にバイナリがあるかを見る。

```bash
test -x "${CLAUDE_PLUGIN_DATA}/bin/repo-scan"
```

無ければ (exit 1) ここで止め、手作業の git の手順で代わりに進めない。
手作業では、repo-scan が見る stray と worktree の判定を飛ばし、触らないはずのブランチを書き換えるおそれがある。
ユーザーには次を伝える。

- Go が要る。入っていなければ <https://go.dev/doc/install> から入れる
- 再ビルドは次のコマンドで行う。Claude Code の中なら先頭に `!` を付けて打つ

  ```bash
  CLAUDE_PLUGIN_DATA="${CLAUDE_PLUGIN_DATA}" "${CLAUDE_PLUGIN_ROOT}/scripts/build-repo-scan.sh"
  ```

- hook でのビルドが失敗していれば、原因は `${CLAUDE_PLUGIN_DATA}/build-repo-scan.log` にある

## ワークフロー

### Step 1: 同期プランを取得

```bash
"${CLAUDE_PLUGIN_DATA}/bin/repo-scan"
```

既定は ghq 配下の全リポジトリが対象になる。特定の owner だけに絞るときは
`--owner <name>` を繰り返し指定する (例: `--owner <owner-a> --owner <owner-b>`)。

出力 JSON の `repos[]` が対象のリポジトリ。手順で使うキーは次のとおり。

- リポジトリ: `path` (本体 worktree のパス。以下の `<repo>`)・`default_branch`・`main_behind`・`worktrees[]`・`branches[]`
- `branches[]` の要素: `name`・`checked_out_in`・`dirty`・`is_stray`・`behind_main`・`ahead_main`

repo-scan は fetch をしない。数字は手元にある `origin/<default_branch>` との比較になる。

### Step 2: 概要を提示

対象のリポジトリの一覧を、main の遅れ・各ブランチの dirty/stray 状態とともにユーザーへ提示する。
ここでは実行しない (何をするかの予告のみ)。

### Step 3: トピックを推定して確認

作業中 worktree のブランチ名・直近コミット (`git -C <wt> log --oneline -5`)・変更ファイルから
「今のトピック」を推定し、「このトピックで合ってる?」と **一度だけ** ユーザーに確認する。

### Step 4: 各リポジトリをループ

対象のリポジトリごとに以下を実行する。

1. **fetch**: `git -C <repo> fetch origin`
   - 打つ前に、旧 main を `git -C <repo> rev-parse <default_branch>` で控える (キャッチアップ用)。
   - 成否は exit code で判定する。credential helper による認証情報の書き戻しに失敗する環境では、
     fetch に成功しても stderr に `fatal` で始まる行を出す場合もある。
2. **main を FF 更新**:
   - fetch の後に、FF できるかを `git -C <repo> merge-base --is-ancestor <default_branch> origin/<default_branch>` で確かめる。
   - exit 0 なら `git -C <repo> merge --ff-only origin/<default_branch>` を打ち、新 main を
     `git -C <repo> rev-parse <default_branch>` で控える。
   - FF 不可なら skip し、「main が diverged」と報告する (安全ルール)。
3. **各ブランチを rebase**:
   - `is_stray` のブランチは報告だけにする (安全ルール)。
   - `checked_out_in` があるブランチは、その worktree で in-place に rebase:
     - `dirty` なら `git -C <wt> stash push -m "repo-sync auto-stash"`。
     - `git -C <wt> rebase <default_branch>`。
     - コンフリクトしたら **止まって** 差分と解決案を示し、一緒に解決してから
       `git -C <wt> rebase --continue` を打つ。
     - stash していたら `git -C <wt> stash pop` で戻す。pop でコンフリクトしたら同様に一緒に解決。
     - ここまで済んだら次のブランチへ進む。
4. **キャッチアップ** (FF で main が進んだリポジトリのみ):
   - 変更 path を取得する。`changed` はカレントディレクトリのリポジトリを見るので、subshell で移って打つ。

     ```bash
     (cd <repo> && "${CLAUDE_PLUGIN_DATA}/bin/repo-scan" changed --range <old_main>..<new_main>)
     ```

   - subagent (Explore か general-purpose) に委譲する。渡す情報:
     - リポジトリ名、`<old_main>..<new_main>` の範囲、変更 path 一覧、Step 3 で確定したトピック。
     - 指示: 「この範囲の変更のうち今のトピックに関連するものだけを、ファイルパス付きで
       200 語以内に要約して。関連がなければ『関連なし』とだけ返して。」
   - メインでは蒸留サマリだけを受け取る (生 diff はメインに展開しない)。

### Step 5: 最終レポート

- 更新済みのリポジトリ/ブランチ、FF skip したリポジトリ、解決したコンフリクト、stray ブランチ、要手動対応を一覧化。
- 関連が薄いものも含む全体のキャッチアップをユーザーが求めたら、メインが
  `mktemp "${TMPDIR:-/tmp}/catchup.XXXXXX"` でファイルを作る。そのパスと Step 4 で渡した情報を新しい subagent に渡し、
  範囲の変更を全部要約してファイルへ書かせる。書き終えたら、そのパスをユーザーに提示する。
