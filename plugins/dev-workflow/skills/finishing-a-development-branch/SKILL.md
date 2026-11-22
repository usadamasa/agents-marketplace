---
name: finishing-a-development-branch
description: 実装が終わってテストが通り、作業ブランチを PR にするとき、PR のマージを指示されたときに使う。
---

# finishing-a-development-branch

作業ブランチを PR にして、指示されたらマージするまでの手順を扱う。統合は PR だけで行い、ローカルでマージしない。

## 1. base ブランチを確かめる

base ブランチは、この作業が分岐した元のブランチになる。たいていは計画、会話、ブランチの upstream に
書いてある。分からなければ「このブランチは <推測> から分岐したもので合っていますか?」と聞く。
`main` を前提にしない。stacked PR では下の PR のブランチが base になる。

## 2. base に追いつく

作業中に別のセッションのマージで base が進んでいることがある。CI は branch と base を merge した commit でテストを
実行するので、古い base の上で通したテストは CI での結果を保証しない。テストの前に base を取り込む。

```bash
git fetch origin <base-branch>
git merge-base --is-ancestor origin/<base-branch> HEAD
```

exit 0 なら base は取り込み済みで、手順 3 へ進む。exit 1 なら取り込む。やり方は branch を push 済みかで分ける。

| 状態 | 取り込み方 |
| ---- | ---- |
| upstream が無い (未 push) | `git rebase origin/<base-branch>` |
| upstream がある (push 済み) | `git merge origin/<base-branch>` |

push 済みの branch を rebase すると force push が要る。force push はユーザーがはっきり求めたときだけなので、
push 済みなら merge で取り込む。競合したら解消し、解消の仕方に迷う箇所はユーザーに聞く。

## 3. テストを確かめる

プロジェクトのテスト一式 (`npm test` / `cargo test` / `pytest` / `go test ./...`) と lint を、これから push する
ツリーで実行する。手順 2 で取り込んだなら、取り込む前に通した結果は使わず実行し直す。
落ちたら失敗を報告して止まる。PR を作るのはテストが通ってから。

## 4. やり残しの扱いを決める

PR を作る前に、やり残しを挙げる。やり残しは、直していない箇所、確かめていない挙動、決めていない判断を指す。
挙げた件は 1 件ずつ扱いを決め、PR の先へ持ち越す件を無くす。扱いの決めごとが利用者の指示にあれば従う。
ここでツリーを変えたら、手順 3 のテストと lint を実行し直す。

## 5. commit をまとめて PR を作る

upstream が無い (未 push) なら、push の前に commit を 1 つにまとめる。手順 2 を通した後なので、
`origin/<base-branch>` は HEAD の祖先になっている。

```bash
git reset --soft origin/<base-branch>
git commit
git push -u origin <feature-branch>
```

commit message は変更の全体を表すものにする。まとめないのは、変更が大きく commit ごとに読む必要がある PR だけで、
迷ったらまとめる。upstream が既にある (push 済み) なら、まとめると force push が要るので、そのまま push し、
まとめるのは手順 6 に回す。

続けて、`<base-branch>` に向けた PR を `gh pr create` で作る。本文は「変更」と「検証」で組む。
リポジトリに PR のテンプレートや決めごとがあれば従う。ユーザーへの報告は PR の URL、変更、検証結果で組む。

push が拒否されたら、原因 (リモートが進んでいる、保護ブランチ、権限) を調べる。force push はユーザーが
はっきり求めたときだけ。

push の後に `gh pr edit` で本文を直すと、`edited` でも起動する CI が push の run を取り消す。`gh pr checks` は
取り消された job を fail と出すので、報告の前に `gh run list --branch <feature-branch>` で最新の run の
conclusion を見る。

## 6. PR をマージする

マージはユーザーが指示したときだけ行う。それまでに PR へ届いたフィードバックは、今の worktree で直す。

1. PR の commit を書き換える必要があるかを確かめる。

   ```bash
   git fetch origin <base-branch> <feature-branch>
   git merge-base --is-ancestor origin/<base-branch> HEAD
   git rev-list --count origin/<base-branch>..HEAD
   git rev-parse HEAD origin/<feature-branch>
   ```

   表は上の行から順に当てはめる。

   | 状態 | すること |
   | ---- | ---- |
   | 変更が大きく、commit ごとに読ませるためにまとめていない PR | 書き換えない。未 push の commit があれば push する |
   | exit 0、count が 1、rev-parse の 2 行が同じ | 書き換えない。CI で通った commit がそのままマージされる |
   | それ以外 (base が進んだ、PR の後に commit が増えた、PR の前に push 済みでまとめていない) | 手順 2 (push 済みなので merge)、手順 3 を通し、手順 5 と同じ `git reset --soft origin/<base-branch>` と `git commit` でまとめ、`git push --force-with-lease` する |

   この force push はマージの指示に含まれるので、改めて確かめない。CI の待ち方は手順 6 の 5 で決める。
2. cwd が worktree なら `ExitWorktree` の `remove` で抜ける。未マージのコミットを理由に拒否されたら、
   push 済みであることを確かめて `discard_changes: true` で抜ける。自分のセッションの worktree を
   `git worktree remove` で手で消さない (セッションがその worktree を追い続け、ロックが残る)。
3. 別のセッションの worktree が同じブランチを checkout していれば、マージ先のブランチから
   `git worktree remove <path>` で外す。残っていると `--delete-branch` のローカルのブランチ削除で落ちる。
4. `gh pr list --base <branch> --state open` で、このブランチを base にしている PR を探す。あれば
   `gh pr edit <num> --base <マージ先>` で付け替える。stacked PR を畳むなら `gh stack merge --merge` を使う。
5. Draft なら `gh pr ready <num>` で外す。Draft 解除はマージの指示に含まれる。Draft のままでは auto-merge を
   付けられない (`Pull request is a draft (enablePullRequestAutoMerge)`)。続けて、リポジトリの設定と
   base の必須チェックを見る。

   ```bash
   gh api repos/{owner}/{repo} --jq .allow_auto_merge,.delete_branch_on_merge
   gh api repos/{owner}/{repo}/rules/branches/<base-branch>
   ```

   後のコマンドの出力に `type` が `required_status_checks` の要素があれば、base に必須チェックがある。
   旧来の branch protection のリポジトリでは `gh api repos/{owner}/{repo}/branches/<base-branch>/protection` の
   `required_status_checks` を見る (`Branch not protected` の 404 なら無い)。
   マージ方式のフラグ (`<方式>`) はリポジトリの指示に従う。

   | 状態 | 打つもの |
   | ---- | ---- |
   | 2 つの設定が `true` で、base に必須チェックがある | `gh pr merge <num> --auto <方式>` |
   | それ以外 | マージ先のブランチで `git pull --ff-only` し、`gh pr checks <num> --watch` で最新の run が通るのを待ってから `gh pr merge <num> --delete-branch <方式>` |

   `--auto` は、必須チェックが通っていなければ auto-merge を付けて CI 待ちを GitHub に任せ、通っていれば
   その場でマージする。必須チェックが無い base や `allow_auto_merge` が無効のリポジトリでは、CI の途中でも
   その場でマージするので、`--auto` を打たない (cli/cli#8792)。`--auto` では gh がブランチを消さず、リモートのブランチは
   `delete_branch_on_merge` によってマージ時に GitHub が消す。
6. `--auto` を打ったら、`gh pr view <num> --json state,autoMergeRequest` で結果を見る。

   | 結果 | 報告 |
   | ---- | ---- |
   | `state` が `MERGED` | マージした。worktree を使っていなければ、マージ先のブランチで `git pull --ff-only` し、ローカルのブランチを `git branch -d` で消す |
   | `autoMergeRequest.mergeMethod` が入っている | auto-merge を設定し、必須チェックの通過を待っている。マージ済みとは書かない |

   待っている間に CI が落ちても auto-merge は外れない。直して push すれば、通った時点でマージされる。
   PR を Draft に戻すと auto-merge は外れる。auto-merge が待つのは必須チェックだけで、必須でないチェックが
   落ちてもマージは止まらない。

手順 6 の 4 を飛ばして、ほかの PR が base にしているブランチを消すと、GitHub はその PR を retarget せずに閉じる。
閉じた PR は base を変えられず、reopen もできない (cli/cli#14223、platform 側の bug で未修正)。
