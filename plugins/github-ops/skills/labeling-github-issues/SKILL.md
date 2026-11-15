---
name: labeling-github-issues
description: >-
  自分の GitHub リポジトリで issue と PR にラベル (種類、機能の area、優先度、epic) を付ける・付け直すとき、
  リポジトリにラベル運用を新しく入れるとき、release note の分類がおかしいときに使う。
  起票先の判断は github-ops:filing-issues、Project の Priority と運用は github-ops:managing-github-projects の担当。
---

# labeling-github-issues

issue と PR を次の軸のラベルで分け、PR のラベルから release note の category を作る。

| 軸 | ラベル | 付け方 |
| ---- | ---- | ---- |
| 種類 | `bug` `enhancement` `documentation` `refactor` `investigation` | issue に 1 つ |
| 機能 | `area: <機能>` | 1 つ以上。廃止した機能の issue には付けない |
| 優先度 | `priority: high` `priority: medium` `priority: low` | 利用者が付ける |
| epic | `epic` | sub-issue を束ねる親 issue に付ける |

ラベルを作る経路は「ラベル運用を入れる」の手順 2 で決め、付ける判断と PR への写し方をこの skill が持つ。

適用するのは利用者が自分で運用するリポジトリに限る。
業務や organization のリポジトリには当てはめず、その repo の決めごとに従う。

`priority:` ラベルは repo の中での着手の順で、GitHub Project の Priority (P0/P1/P2) は repo をまたいだ選定に使う。
別物なので、片方からもう片方を埋めない。

## repo 固有の決めごとを読む

area の表と廃止した機能の列挙は repo ごとに違う。repo の `.claude/skills/*/SKILL.md` か `AGENTS.md` に
area の表があれば、それに従う。無ければ「ラベル運用を入れる」の手順 1 で決める。

```sh
git grep -l 'area: ' -- AGENTS.md '.claude/skills/*/SKILL.md'
```

何も出なければ (exit 1) area の表はまだ無い。

## issue にラベルを付ける

### 1. ラベルの足りない issue を出す

```sh
gh issue list --state all --limit 1000 --json number,state,title,labels --jq '.[] | select(([.labels[].name | select(test("^(bug|enhancement|documentation|refactor|investigation)$"))] | length) == 0 or ([.labels[].name | select(startswith("area: "))] | length) == 0) | "\(.number)\t\(.state)\t\([.labels[].name] | join(","))\t\(.title)"'
```

area が無い closed issue のうち、廃止した機能のものはそのままでよい。題で決めきれない issue は
`gh issue view <番号>` で本文を読む。

### 2. 種類を決める

| ラベル | 当てる issue |
| ---- | ---- |
| `bug` | 今ある振る舞いが期待と違う。誤検知、取り違え、落ちる、競合 |
| `enhancement` | 新しい機能か、使い勝手と性能の改善 (結果は同じで、呼び出しや待ちを減らすものを含む) |
| `refactor` | 振る舞いを変えない整理。パッケージ構成、重複の除去、lint の違反、廃止した機能の撤去 |
| `documentation` | docs、skill、AGENTS.md だけを変える |
| `investigation` | 結論を issue のコメントに残して閉じ、実装は別の issue に切るもの。検討、triage、前提の裏取り |

### 3. area を決める

repo 固有の area の表から選ぶ。複数の機能に掛かる issue には当たる area をすべて付ける。
見た目が変わっても機能が続いているなら同じ area に入れる。

- 廃止した機能の issue が open で残っていたら、area を付けずに close の候補として利用者に知らせる
- 今ある機能で当たる行が無いときは、近い area を付けて、area を足すかを利用者に確かめる

### 4. ラベルを付ける

issue ごとではなく、ラベルごとに番号をまとめて付ける。打つコマンドがラベルの数で済む。

```sh
gh issue edit 392 393 395 --add-label "area: statusline"
```

### 5. epic を組む

親とする issue に `epic` を付け、子を sub-issue でつなぐ。

```sh
gh issue edit 88 --add-label epic
gh issue edit 88 --add-sub-issue 90,91
```

親の issue を新しく起票するのは、利用者が指示したときだけにする。close した epic に open の子が残っているときは、
付け替え先の候補を添えて利用者に知らせる。

### 6. 確かめる

手順 1 のコマンドを打ち直す。close の候補として知らせたものを除き、open の issue が出ないことを見る。

## PR にラベルを付ける

PR の本文に、閉じる issue を `Closes #N` で 1 行ずつ書く。
PR のラベル同期の workflow (「ラベル運用を入れる」の手順 5) が入っている repo では、
PR の open・edit・push のたびに issue の種類と area が PR へ足される。epic と priority は写らない。

issue を閉じない PR は、作るときに種類と area を付ける。

```sh
gh pr create --draft --label refactor --label "area: dev" ...
```

作った後に足すなら `gh pr edit <番号> --add-label ...` を使う。

tagpr の Release PR の本文で「Other」に入った PR は種類が付いていないので、`gh pr edit` で足す。
Release PR の本文は次に main へ push されたときに作り直され、GitHub Release の note は tag を打つときに作られる。
足したラベルはそのどちらかで反映されるので、Release PR の本文を手で直さなくてよい。

## ラベル運用を入れる

### 1. area を決める

README の役割の節か、実装の単位 (パッケージ、サブコマンド、plugin の skill) から機能を拾い、1 機能 1 area にする。
CI・lint・テスト・保守用の docs は `area: dev` にまとめる。
README と実装のどちらにも残っていない機能は廃止した機能として扱い、area を作らない。
area の表は「area」と「含むもの」の列で作り、案を利用者に見せて合意を取る。

決めた area の表と廃止した機能の列挙は、repo の `AGENTS.md` か `.claude/skills/<name>/SKILL.md` に置く。
この skill の「issue にラベルを付ける」の手順 3 が、そこを読む。

### 2. ラベルを宣言する

ラベルを作る経路は 1 つにする。利用者がラベルを宣言的に管理するツール (リポジトリ設定を YAML で配るものなど) を
使っているなら、そこに宣言して apply する。使っていなければ `gh label create` で作る。
どちらを使うかは、利用者のグローバル CLAUDE.md (`~/.claude/CLAUDE.md`) へ書いたものに従う。

```markdown
## GitHub のラベル

- 宣言の置き場: `<owner>/<repo>` の `<path>` (`gh label create` で作るなら「なし」)
```

書いていなければ利用者に確かめ、CLAUDE.md に書くかを聞く。

どちらの経路でも、ラベルの置き分けと見た目は次のようにそろえる。

- 種類・優先度・`epic` と `duplicate` `wontfix` は全 repo で共通にする。宣言的に管理するなら、共通の設定に置いて全 repo へ配る
- repo ごとに置くのは `area:` のラベルと、ツールが付けるラベル (`dependencies` `tagpr` など)
- `area:` の色は揃え、description は area の表の「含むもの」を 1 句にする

```sh
gh label create "area: statusline" --color 1d76db --description "statusline の描画と state file への集約"
```

### 3. 使われていない既定ラベルを整理する

GitHub が repo 作成時に入れる既定ラベルのうち、共通のラベルに入れていないものを見る
(`good first issue` `help wanted` `invalid` `question`)。そのうち、付いている issue と PR が無いものを出す。

```sh
gh issue list --state all --label invalid --limit 1 --json number
gh pr list --state all --label invalid --limit 1 --json number
```

消すかどうかを利用者に確かめ、消すと決まったら `gh label delete` を打つ。消すと付いていた issue と PR からも外れる。

### 4. 過去の issue に付ける

手順 2 のラベルが repo にできてから、「issue にラベルを付ける」の手順を、closed の issue も含めて回す。
release note は過去の PR に遡らないが、
area ごとの issue の検索と、廃止した機能の issue を拾うのに closed の issue も使う。

### 5. PR と release note へ写す

次を repo に置く。

| 置き場 | 中身 |
| ---- | ---- |
| `.github/workflows/pr-labels.yaml` | PR の open・edit・push で、閉じる issue の種類と `area:` のラベルを PR へ足す workflow |
| `.github/release.yml` | `${CLAUDE_SKILL_DIR}/release.yml` の写し。Dependencies の labels を dependabot が付けるラベルに揃える |

ラベル同期の workflow は、PR の closing issue を引いてラベルを足すだけにし、PR に手で付けたラベルを消さない。
fork と dependabot の PR は token に書き込み権限が無いので対象から外す。
公開されている実装例に、gh-manage の reusable workflow がある。呼び出し側の雛形と、写すラベルを変える `copy-pattern` は
[gh-manage の README の「PR のラベル同期」](https://github.com/usadamasa/gh-manage#pr-のラベル同期) にある。
`release.yml` は GitHub Release の自動生成 note が読む。tagpr を使う repo では Release PR の本文にも効く。

### 6. PR の決めごとを repo に書く

repo の `AGENTS.md` に次の行を置く。

```markdown
- 本文に、閉じる issue を `Closes #N` で 1 行ずつ書く。pr-labels workflow が issue の種類と area のラベルを PR へ写す。
- issue を閉じない PR は、`gh pr create --label` で種類と area のラベルを付けて作る。選び方は `github-ops:labeling-github-issues` skill。
```

## 機能を足す・廃止する

機能を足したら、`area:` のラベルを「ラベル運用を入れる」の手順 2 の経路で作り、repo 側の area の表に行を足す。

機能を廃止したら、repo 側の表から行を外して廃止した機能の列挙へ足す。ラベルの宣言から外すかと、
ラベルを消すかは利用者に確かめる (消すと closed issue からも外れる)。

## APPENDIX: なぜこの形にするか

優先度はいつ着手するかの判断で、題と本文からは決まらないので利用者に残す。

release note の category は PR のラベルで決まり、issue のラベルは見ない。PR ごとに手で付けると漏れるので、
closing issue から写す。

ラベルを `gh label create` と宣言的な管理ツールの両方で作ると、色と説明の正がどちらか分からなくなり、
ツールの plan に差分が出続ける。作る経路を 1 つにする。
