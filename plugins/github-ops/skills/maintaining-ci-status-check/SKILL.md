---
name: maintaining-ci-status-check
description: >-
  GitHub Actions の集約ジョブ `ci-status-check` を読む・足す・直すとき、CI にジョブを追加・削除・改名するとき、
  新しいリポジトリの CI と Branch Ruleset を組むときに使う。
---

# maintaining-ci-status-check

Branch Ruleset の required status check には `ci-status-check` だけを登録する。
チェックに使うジョブを `needs` に並べた集約ジョブで、本体のジョブを足す・消す・改名しても Ruleset は触らない。
ジョブを個別に登録すると、追加や改名のたびに Ruleset の更新が要り、漏れたジョブは失敗してもマージを止めない。

出典は [GitHub Actions の Required Status Check の管理方法][zenn]。
この skill の集約ジョブは記事の形と違い、全ジョブ成功時に skipped ではなく success を返す。
`gh pr checks` に `skipping` と出ると、読み手 (エージェント、人) が「まだ通っていない」と読み違えるため。

## 採用する形

```yaml
  ci-status-check:
    needs:
      - actions-lock
      - bats-test
      - go-test
      - lint
      - shellcheck
    if: always()
    runs-on: ubuntu-slim
    timeout-minutes: 10
    steps:
      - if: contains(needs.*.result, 'failure') || contains(needs.*.result, 'cancelled')
        run: exit 1
```

- `always()` を `!cancelled()` に変えない。GitHub の [expressions ドキュメント][gh-expr]は `always()` を避けるよう書いているが、
  ここで `!cancelled()` にすると、run のキャンセル時にこのジョブ自体が skipped になる。skipped の required check は
  success 扱いなので、キャンセルされた PR がマージ可能になる。`always()` で起動して `cancelled` を `exit 1` に落とす。
- `timeout-minutes` を付ける。`always()` のジョブが詰まったときの保険。
- ジョブ名は `name:` を付けず、ジョブ id `ci-status-check` のまま使う。Ruleset が照合するのはこの名前。

判定の中身は末尾の APPENDIX。

## 守ること

**チェックに使うジョブを `needs` に並べる。** `needs` に無いジョブは失敗してもマージを止めない。ジョブを足したら
`needs` にも足す。消したら `needs` からも消す (存在しないジョブ名が残ると workflow が無効になり、
required check が Expected のまま止まる)。他のジョブに `needs` で依存するジョブを並べるときは、その上流も並べる。
上流が失敗するとそのジョブは `skipped` になり、集約ジョブは `skipped` を通すため、上流が `needs` に無いと失敗が消える。
確認は次のコマンドで行う。出力には `needs` に無いジョブ id が並ぶ。出たものが全てチェック以外のジョブなら漏れなし。

```sh
yq -r '(.jobs | keys | map(select(. != "ci-status-check"))) - .jobs.ci-status-check.needs | .[]' .github/workflows/ci.yaml
```

**workflow レベルの `paths` / `paths-ignore` を付けない。** workflow ごと skip されると `ci-status-check` が
結果を報告しない。PR は "Waiting for status to be reported" で止まる (GitHub docs の
[Troubleshooting required status checks][gh-docs])。
絞るならジョブ単位の `if:` で行う (「変更されたパスでジョブを絞る」)。
skipped になった本体ジョブは集約ジョブを通る。

**pull_request の workflow は 1 本にする。** 複数の workflow に分けると、片方の `ci-status-check` が成功した時点で
マージ可能になる。分けたいときは出典記事の `workflow_call` 構成に寄せる。

**失敗を無視したいジョブには `continue-on-error: true` を付ける。** result が success になり、集約ジョブを
赤くしない。

## 変更されたパスでジョブを絞る

判定には dorny/paths-filter を使う (merge_group にも対応し、pull_request では変更の一覧を REST API から読む)。
checkout は `filters:` に渡す定義ファイルを読むために要る。
判定を ubuntu-slim の `changes` ジョブに出し、本体ジョブはその outputs をジョブ単位の `if:` 1 つで受ける。
step ごとに `if:` を並べない。フィルタの定義は `.github/filters.yaml` に置き、`filters:` でパスを渡す。

```yaml
  changes:
    runs-on: ubuntu-slim
    outputs:
      checks: ${{ steps.filter.outputs.code != 'false' || steps.filter.outputs.instructions != 'false' }}
    steps:
      - uses: actions/checkout@v6.0.3
      - uses: dorny/paths-filter@v4.0.3
        id: filter
        with:
          predicate-quantifier: some-with-excludes
          filters: .github/filters.yaml

  checks:
    needs:
      - changes
    if: needs.changes.outputs.checks == 'true'
```

```yaml
# .github/filters.yaml
code:
  - '**'
  - '!**/*.md'
instructions:
  - '**/AGENTS.md'
  - '**/CLAUDE.md'
```

- outputs の式は `!= 'false'` で書く。フィルタ名の打ち間違いや出力の欠落があっても、本体ジョブを全て実行する側に倒れる。
- `changes` も集約ジョブの `needs` に並べる (判定の失敗で本体が skipped になっても赤にする)。
- 除外 (`!`) は最終的な判定になり、別のパターンで含め直せない。md のうち検証したいもの (AGENTS.md と CLAUDE.md の
  symlink の対を bats が見る、など) は別のフィルタにして OR で受ける。md を skip する前に、md を読むテストが無いかを
  見る。
- dorny/paths-filter の pin は github-ops:using-gh-actions-lock skill に従う。

## 新しいリポジトリへの導入

1. ci.yaml に上の形で `ci-status-check` を置き、チェックに使うジョブを `needs` に並べる。
2. PR を 1 本出し、`gh pr checks` で `ci-status-check` が `pass` になることを見る。
3. Branch Ruleset の required status checks に `ci-status-check` を登録する。workflow が一度でも実行されていれば
   候補に出る。登録は次のコマンドで確かめる。

```sh
gh api repos/<owner>/<repo>/rulesets --jq '.[] | {id, name, enforcement}'
gh api repos/<owner>/<repo>/rulesets/<id> --jq '.rules[] | select(.type=="required_status_checks")'
```

## 非推奨の形 (見つけたら直す)

| `if:` の形 | 全ジョブ成功時 | 問題 |
| ---- | ---- | ---- |
| `failure()` / `failure() \|\| cancelled()` | skipped | GitHub Actions のバグで意図せず skip され、CI が赤いのにマージできる (出典記事の 2025-07-12 追記) |
| `always() && (contains(needs.*.result, 'failure') \|\| ...)` (記事の現行の形) | skipped | 穴は無いが、`gh pr checks` に `skipping` と出て読み違える |

判別は `yq '.jobs.ci-status-check.if' .github/workflows/ci.yaml`。`always()` 以外が出たら非推奨の形。
直し方は `if:` を `always()` に変え、`exit 1` の step に `if: contains(...)` を付ける (上の形)。
変更は集約ジョブの中で済み、Ruleset 側は触らない。

## 症状から引く

| `gh pr checks` の見え方 | 読み方 |
| ---- | ---- |
| `ci-status-check  skipping  0` で他は全て `pass` | 非推奨の形。CI は通っている。マージ後に形を直す PR を出す |
| `ci-status-check  fail` で他は全て `pass` | `needs` のどれかが `cancelled` か、失敗したジョブが一覧に出ていない。`gh run view <run id>` で見る |
| `ci-status-check` が pending のまま、他のジョブも出ない | workflow が起動していない。`needs` の不明なジョブ名、workflow レベルの `paths`、YAML の構文エラーを疑う。`gh run list --workflow ci.yaml --branch <branch>` |
| 新しく足したジョブが `fail` なのにマージ可能 | `needs` 漏れ。上の yq で確かめる |

## APPENDIX: 判定の中身

`needs.<job_id>.result` は `success` / `failure` / `cancelled` / `skipped` のいずれかで、`needs.*.result` はその一覧。
集約ジョブは `always()` で毎回起動し、一覧に `failure` か `cancelled` があるときだけ `exit 1` の step を実行する。
どちらも無ければ step が skip され、ジョブは success で終わる。

`skipped` を通すのは、ジョブ単位の paths 絞り込みで本体ジョブが skip される形を許すため。
上流の失敗で skip されたジョブも同じ `skipped` に見えるので、上流を `needs` に並べて失敗を拾う (「守ること」)。

GitHub は条件式で skip されたジョブを required check として success と報告する。
一方、workflow レベルのフィルタで workflow ごと起動しなかった場合は結果が報告されず、PR は pending で止まる。
前者が `!cancelled()` を使わない理由、後者が workflow レベルの `paths` を付けない理由になっている。

[zenn]: https://zenn.dev/shunsuke_suzuki/articles/how-to-manage-github-actions-required-status-check
[gh-expr]: https://docs.github.com/en/actions/learn-github-actions/expressions#always
[gh-docs]: https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/defining-the-mergeability-of-pull-requests/troubleshooting-required-status-checks
