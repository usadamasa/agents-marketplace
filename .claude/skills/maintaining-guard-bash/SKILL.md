---
name: maintaining-guard-bash
description: >-
  harness plugin の guard-bash に CLI のルールを足す・直すとき、
  settings の broad allow や sandbox.excludedCommands に CLI を足す変更を出すときに使う。
---

# maintaining-guard-bash

guard-bash (harness plugin の Bash の PreToolUse hook) の CLI ごとのルールを足す・直すときの判断と順序。
コードは `plugins/harness/cmd/guard-bash/` にあり、以下のパスはここからの相対で書く。
判定の仕組み (構文解析、評価順、フラグの解釈) はコードのコメントが持つ。
入口は `main.go` の頭と `internal/argrule/argrule.go` になる。
今のルールの一覧は `~/.claude/plugins/data/harness-usadamasa/bin/guard-bash -rules-doc` が出す。

## ルールが要る CLI

次のどれかに当たる変更では、同じ CLI の guard のルールも足す。

- settings の `permissions.allow` に CLI 全体の allow (`Bash(<cli>:*)`) を置く。
  permissions は prefix の字面で照合し、フラグを解析しない。フラグで write を分けて止められるのは guard だけになる。
- `sandbox.excludedCommands` に、ネットワークか認証を伴い write の操作を持つ CLI を置く。
- sandbox 内で動く CLI でも、フラグで送信と取得が分かれるもの (`curl`) は guard でフラグ単位に判定する。
  permissions の deny で丸ごと止めると、取得まで止まる。

## deny と ask を選ぶ

| 判定 | 起きること |
| ---- | ---- |
| deny | 確認を出さずに止める。Claude からは実行できず、要るときはユーザーが `!` を前に付けて自分で実行する |
| ask | 実行前にユーザーへ確認を出す |

## CLI のルールを足す

1. `internal/rules/rules_test.go` に、止める形と通す形のケースを先に書く。
   リポジトリの root で `go -C plugins/harness test ./cmd/guard-bash/...` を実行し、落ちるのを確かめる。
2. `internal/rules/rules.go` の `all` のコメントに従って `<tool>.go` に group を宣言し、`all` に足す。テストを通す。
3. maintaining-in-repo-plugins skill に従って harness plugin の version を上げる (必須)。

## 変更を入れる順序

guard のルールはこのリポジトリに、settings は別のリポジトリにあるので、PR は 2 本になる。次の順に入れる。

1. guard 側の PR をマージする。
2. harness plugin を update する。新しいセッションの SessionStart hook が guard-bash をビルドし直すまでは、古いルールのまま動く。
   新しいセッションで上の `-rules-doc` を実行し、足した CLI の節が出てから 3 へ進む。
3. settings 側の PR をマージする。settings 側の中身 (allow、excludedCommands、auto mode の説明) は、settings を持つリポジトリの手順に従う。

settings 側が先に入ると、broad allow だけが効いて guard の deny / ask の無い期間ができる。
