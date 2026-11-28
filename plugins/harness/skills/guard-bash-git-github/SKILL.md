---
name: guard-bash-git-github
description: Bash の guard (guard-bash) が git や gh のコマンドを deny / ask にしたとき、理由と通る書き方を調べるために使う。gh の PR / issue の書き込みの確認をプロジェクト単位で省く opt-in を扱うときにも引く。GitHub での作業の進め方 (起票、PR の作成とマージ) は対象外。
---

# guard-bash-git-github

guard-bash は Bash の PreToolUse hook で、どのプロジェクトでも git と gh のコマンドを判定する。
字面で決まらない判定は、コマンドが動くリポジトリや GitHub へ照会して決める。
この skill は、deny / ask になった理由の調べ方と、その照会が効く書き方をまとめる。

GitHub での作業の進め方 (issue の起票、ラベル、PR の作成とマージ) は扱わない。

## 判定を読む

今のルールの一覧は、バイナリが自分で出す。
バイナリは、セッションの開始時に harness plugin のデータの置き場へビルドされる。

```sh
~/.claude/plugins/data/harness-usadamasa/bin/guard-bash -rules-doc
```

ルールは allow → deny → ask の順に評価し、最初に当たった判定で確定する。
deny と ask の理由文は、一覧の `gh` と `git` の節の 1 行に対応する。

ask には 2 種類ある。

| 種類 | 対処 |
| ---- | ---- |
| 一覧の ask のルールに当たった (タグの送信、リリース PR への書き込みなど) | 形を変えずに、ユーザーの確認を受ける |
| 照会で確かめられなかった | 「照会で確かめられない形と、照会が効く書き方」の形で実行し直す |

## 照会で決まる判定

照会で確かめられなかったときは ask に落ちる (fail-closed)。

| 判定 | 照会 |
| ---- | ---- |
| PR / issue が自分の作成か、リリース PR か | `gh pr view --json author,labels`、`gh api user` |
| gh の対象がプロジェクトのリポジトリか | cwd とプロジェクトの `git-common-dir` |
| push する名前がタグか | `git for-each-ref refs/tags/<name>` |
| 設定が push にタグを足すか | `git config` (`push.followTags`、`remote.<name>.push`、`remote.<name>.mirror`) |
| push が送る履歴にリリース PR の head があるか | `git for-each-ref --merged` / `--contains` |

リリース PR は、gh の側ではラベル `tagpr`、git の側では head ブランチ名の接頭辞 `tagpr-from-` で見分ける。

## 照会で確かめられない形と、照会が効く書き方

次の形は、照会で確かめられないので ask になる。右の形で実行し直すと、照会の結果で判定が決まる。

| ask になる形 | 照会が効く書き方 |
| ---- | ---- |
| chain の途中に `cd` / `pushd` / `popd` がある | 対象のリポジトリを cwd にして、素の 1 文で実行する |
| `git -C` / `--git-dir` / `--work-tree` 付き、`GIT_DIR` / `GIT_CONFIG_*` の代入 | 同上 |
| `GH_REPO` / `GH_HOST` の代入 | `-R <owner>/<repo>` で対象を渡す |
| 引数に変数やコマンド置換がある (`git push origin "$BRANCH"`) | ブランチ名や番号を字面で書く |
| gh の番号や URL を verb の直後に置いていない (`gh pr merge --squash 5`) | `gh pr merge 5 --squash` |
| ブランチと同名のローカルのタグがある | `refs/heads/<name>` で完全修飾する |
| `push.followTags` が true のリポジトリでの push | `--no-follow-tags` を付ける |

`.envrc` などが `GH_REPO` / `GH_HOST` / `GIT_DIR` を Bash のシェルにだけ載せているセッションでは、
どの書き方でも ask になる。hook から値が見えないためで、実行し直しても通らない。
ユーザーの確認を受けて進め、どの変数が載っているかをユーザーへ伝える。

## gh の PR / issue の書き込みの opt-in

gh の PR / issue の書き込みは ask に落ちる。プロジェクト単位でこの確認を省く opt-in の条件と、
省いても残る ask / deny は [references/gh-write-optin.md](references/gh-write-optin.md) が持つ。

## 実装の所在

ルールと照会の実装は harness plugin の `cmd/guard-bash` にある。
