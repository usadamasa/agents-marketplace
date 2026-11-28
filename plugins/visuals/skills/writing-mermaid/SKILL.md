---
name: writing-mermaid
description: >-
  Mermaid のコードブロックを書く・直すときに使う。
  parse error やラベルの欠落を避ける記法と、図の粒度を扱う。
---

# writing-mermaid

Mermaid 記法のダイアグラムを書くときの規範。

## 記述規範 (レンダリング失敗を避ける)

以下は parse error かラベルの欠落を起こしやすい記法。
ここに挙がっていない通常の文字 (`/` `+` `.` `'` `"` `:` `=` `::`、
quoted rectangle label / subgraph title / sequence message 内の丸カッコ) は普通に使ってよい。

### ラベルの改行

- **改行に `<br/>` を使わず single-line label で書く**。多行にしたいときは `A: B` (名前と説明) か `A / B` (並列) の separator 形式にする

`<br/>` は parse error にならず、**エラーを出さないままラベルが壊れる**。
mermaid v10 以降の `securityLevel` / `htmlLabels` 設定によっては `<br/>` が strip され、`A<br/>B` が `AB` に連結される。
たとえば `VirtualService<br/>sample-app-api` は `VirtualServicesample-app-api` になる。
書いた環境で見えていても、設定の違う別のレンダラでは壊れうる。

### 丸カッコ

- **cylinder `[("...")]` と stadium `(["..."])` のラベル内に丸カッコ `(` `)` を入れない**: 終端パターン (`)]` / `"])`) を greedy に閉じる。`Apps(["Application (Explorer / Agent)"])` は NG → `Apps(["Application - Explorer / Agent"])`
- **flowchart edge label `|...|` 内に丸カッコ `(` `)` を入れない**: edge label は quote できず、`(` が `PS` トークンとして消費される。`A -->|routing (VirtualService)| B` は NG → `A -->|routing via VirtualService| B`

### 記号

- **flowchart node label に裸の `<word>` を使わない**: HTML タグ扱いで strip される。`&lt;word&gt;` と HTML entity で書く
- **flowchart edge label / node ID の先頭に `@<word>` を置かない**: mermaid v11 以降 `@` は edge ID 記法 (`e1@-->`)。`@reboot` 等は `@` を外すか別表記にする
- **stateDiagram-v2 の note description の先頭に `:` を置かない**: parser が `DESCR` トークンとして消費する。`: undeploy 不可` → `undeploy 不可`

### レイアウト制約 (dagre)

Mermaid の既定レンダラは dagre。ELK への切替はレンダリング側の設定変更が要るので、図の書き方で回避するほうが早い。

- **nested subgraph の `direction TB` は、inner node が subgraph 境界をまたぐ edge を持つと無視され、親の方向を継承する** ([Mermaid 公式](https://mermaid.ai/open-source/syntax/flowchart.html))。回避するには次のどれかを使う
  - edge を subgraph ID 起点に置換する
  - 構造を flat 化する
  - 親 `flowchart` を `TB` にする
  - invisible edge `~~~` で sibling を pin する (効かないことも多い)

複雑でネストの深い構成図を Mermaid で再現しようとすると dagre のレイアウトが崩れる。
元図 (Miro 等) が既にあるなら、再現より原本の画像を埋め込んで出典リンクを添えるほうが正確で保守も楽。

### 確認

判断に迷う記法は、図を載せる先 (GitHub の PR、wiki など) のプレビューで描画して確かめる。
parse error ならその場で描画が止まる。
`<br/>` 系のようにエラーを出さずに文字が欠ける問題も、描画された図を見れば気づける。

## シーケンス図の粒度

`sequenceDiagram` は collaborator 間の相互作用を主軸にする。
Service 内部の validate / parse / transform を self-call (`Svc->>Svc:`) で一つずつ並べない。
内部処理ステップを 5〜10 個並べると図がスクロールしないと読めなくなり、
本来見せたい「どの境界をまたいで何が起きるか」が埋もれる。
設計議論で必要なのは collaborator 間の責務分担とラウンドトリップ構造であって、method 名の列挙ではない。

- **まず依頼文の主語を読む**。「A と B のシーケンス」と言われたら participant は A + B の 2 つで描き始め、内部分割 (Controller / Service / Repository 等) は note に圧縮する。最初から詰め込むと差し戻される
- 参加者の目安: cross-service の責務分担を見せたいなら 2〜3、内部協調を見せたい設計議論なら 4〜6。8 を超えたら目的が複数混ざっているサイン
- 同一プロセス内のヘルパクラスや validator は participant にしない。`Note over X: 構文 / 存在 / 整合 / 重複を順次確認` のように束ねる
- Repository への複数 method 呼び出しは 1 メッセージにまとめる
- 外部システムへの副作用 (DDL 実行 / message publish / 外部 API) は note でもよい。ただし副作用の有無・非同期化・リトライが論点なら participant に上げる
- 実装ステップを詳細に追う必要があるときは、シーケンス図ではなく flowchart か箇条書きの処理フローで別に書く。図と本文で粒度を分けるのが基本構成

## 棚卸し・整理文書の要約図

棚卸し・整理の文書で要約の図を描くときは、
既存の棚卸しの中身を数える列挙ではなく、読み手が次にどこへ手を付けるかを示す図にする。

- 件数を並べない。件数は「いくつあるか」は伝えるが「どこから手を付けるか」は伝えない
- 課題はツリー構造 (テーマ → 束 → 個別) で描き、個別の課題がどう束ねられているかを見せる
- ノードの色は、その棚卸しで既に付けている優先度 (重要度) ごとに変える。図のために新しい尺度を作らない
