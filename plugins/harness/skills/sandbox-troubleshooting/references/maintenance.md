# この skill の更新方法

sandbox の挙動は Claude Code の更新で変わる。記述と挙動が食い違ったら、推測で直さずに測り直してから書き換える。

## 誰が更新するか

食い違いに気づいたセッションは、進行中の作業を止めない。回避して先へ進み、skill の更新は別セッションか subagent に渡す。
渡すときは、実行したコマンド、返ってきたエラー、食い違った記述の場所を添える。
同じセッションで直すのは、食い違いが進行中の作業のブロッカーになっているときだけにする。

## 更新するとき

| きっかけ | 直すもの |
| ---- | ---- |
| guard が通した形が sandbox 内に落ちた、または表で ❌ の形が外で動いた | 「照合の形を測り直す」 |
| 記載の無いパスで `Operation not permitted` が出た、または記載のパスに書けた | 「組込み deny を測り直す」 |
| 記載の無い経路 (IPC、socket、デバイス) が塞がれていた | 「行を足す前に」 |

## 行を足す前に

新しい症状は、まず既存の行と同根かどうかを確かめる。遮断されている機構 (Mach IPC、`connect(2)`、bind、
書き込み先のパス、照合外れ) が同じなら、行は足さずに既存の行の症状や例を広げる。
照合外れが原因なら、どのコマンドで起きても「excludedCommands の照合」の 1 件で、症状の行を増やす理由にならない。
行を足すのは、遮断の機構か対処が既存のどの行とも違うときだけにする。

## 照合の形を測り直す

sandbox の内外で結果が分かれるコマンドをプローブにする。
`gh api rate_limit` は sandbox 内なら `x509: OSStatus -26276` で落ち、外なら通る。
確かめたい形にプローブを組み込んで 1 回の Bash 呼び出しとして実行し、どちらの結果になるかを見る。
guard が deny する形は実行まで届かない。その形を測るときは、guard の外し方をユーザーと決めてから進める。

結果が変わったら、次の箇所を同じ変更で揃える。

1. `references/excluded-command-shapes.md` の表と、SKILL.md の 5 条件
2. harness plugin の `cmd/guard-bash/internal/rules/sandboxexcluded/sandboxexcluded.go` の `shapeBreak` / `callBreak` とテスト。
   代わりの書き方の表 (`sandboxhint.go`) も、通らなくなった形を勧めていないか見る
3. plugin のバージョンを上げて配る (hook が呼ぶのはビルドしたバイナリなので、更新したセッションの SessionStart が
   ビルドし直すまで、古い判定と文言のまま動く)

deny の文言や判定を変えた効果は、harness plugin の `scripts/sandboxexcluded-denies.sh` で比べる。
transcript から照合外れの deny を拾い、週ごとに原因・コマンド名・実行し直した回数を出す。
集計は deny の 1 文目の形に頼るので、1 文目を変えるときは `sandboxhint_test.go` の `metricPattern` と集計の jq も揃える。

## 組込み deny を測り直す

そのセッションで実際に効いている一覧は、システムプロンプトの sandbox 節にある `denyWithinAllow` で読める。
名前で塞がれるものは一覧に出ないので、`./tmp/probe/` の下に同じ名前を作って確かめる。

| 確かめたいこと | プローブ |
| ---- | ---- |
| 新規作成できるか | `touch <path>` / `mkdir <path>` |
| 既存ファイルを書き込みで開けるか | `dd if=/dev/null of=<path> conv=notrunc` (中身を変えずに開く。`>>` は guard-bash が deny する) |
| Edit / Write ツールで書けるか | Edit で空行を 1 つ足し、すぐ戻す |

プローブで作った `./tmp/probe` は消し、`git status --short` で残骸が無いことを確かめる。
結果は SKILL.md の Appendix に反映する。

## 書き方

- SKILL.md は症状 → 原因 → 対処の表を中心にし、形ごと・パスごとの詳細は `references/` と Appendix に置く。
- 書くのは挙動と対処だけにする。発見の経緯、測定したバージョン、どのセッションで起きたかは commit message とテストのコメントに書く。
- `excludedCommands` に何が登録されているかを前提にしない。登録状況は端末とプロジェクトで変わるので、
  「excluded 登録済みのコマンド」と書き、コマンド名は症状の例として必要なときだけ出す。
- frontmatter の `name` は、他の skill や CLAUDE.md、guard の deny の文言から名前で参照されうる。変えるなら参照元も直す。
