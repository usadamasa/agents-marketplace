---
name: typst-infographic
description: >-
  Typst でインフォグラフィック・ポスター・図解資料を PDF/PNG に生成する。
  ヘッドレス Chrome 不要で軽量・高速。固定サイズの1枚もの (4:3 等) に向く。
  可変長やインタラクティブな成果物が要るなら HTML 系の手段を選ぶ。
---

# typst-infographic

Typst を使ってインフォグラフィック/図解資料を PDF・PNG で生成する。
HTML とヘッドレス Chrome (puppeteer) の組み合わせより軽い。
単一バイナリ (約 30MB) でブラウザが要らず、コンパイルは 1 秒未満で終わる。

## Context

- Working directory: !`pwd`

## 前提ツール

```bash
# Typst (aqua 管理のリポジトリなら aqua.yaml に typst/typst を追加して `aqua i`)
typst --version           # PATH にある場合
aqua exec -- typst --version  # aqua 管理の場合

# 日本語フォント (環境非依存にするため Noto を第一候補にする)
brew install --cask font-noto-sans-cjk-jp
aqua exec -- typst fonts | grep -i "noto sans cjk"   # ファミリ名確認: "Noto Sans CJK JP"
```

- 未インストールなら `brew install typst` または aqua へ追加する (警告してスキップしない)。

## 基本ワークフロー

### Step 1: `main.typ` を書く

`references/template.typ` を雛形にして、次の点を押さえる。

- **ページは固定サイズ**。HTML モックの px 値を **そのまま pt に 1:1 移植**すると見た目を再現しやすい
  (例: 1600×1200px の 4:3 → `#set page(width: 1600pt, height: 1200pt)`)。
- フォントは **Noto Sans CJK JP を第一候補**にする (Hiragino 等が無い環境でも同じ見た目)。
- レイアウトは `grid` で組む。`columns`/`rows` に固定値と `1fr` を混在でき、`colspan`/`rowspan` も使える。
- **ページ高さを埋める**: トップレベルを `grid(rows: (auto, 1fr, auto, ...))` にして主役ブロックを `1fr` 行に置く。
- 装飾には `block(fill:, stroke:, radius:, inset:)` を使う。グラデーションは `gradient.linear(c1, c2, angle: ..deg)`、
  縦書き風のラベルは `rotate(-90deg, reflow: true)` で作る。

### Step 2: アイコン/図は SVG を埋め込む

- 自己完結した SVG ファイルを `image("icons/foo.svg", width: 96pt)` で読み込む。
- **入れ子 SVG (SVG が別 SVG を href 参照) は Typst 非対応**。1 ファイルに完結させる。
- Mermaid 図を載せたいときは mermaid-cli で SVG/PNG にしてから `image()` で読む。

### Step 3: コンパイル

```bash
# PDF (ベクター・印刷可)
aqua exec -- typst compile main.typ out.pdf

# 確認用 PNG (96dpi 相当)
aqua exec -- typst compile main.typ preview.png --ppi 96
```

### Step 4: 出力確認

- `preview.png` を Read ツールで開いて、レイアウト崩れ・はみ出し・文字化けが無いか目視する。
- フォント警告 (`unknown font family`) が出たら、フォント名を `typst fonts` の表記に合わせるか、未導入フォントを候補から外す。

## トラブルシューティング

- **`unknown font family`**: `typst fonts` で正式名を確認。Noto は `Noto Sans CJK JP`。
- **太字が効かない**: `text(weight: "black")` (900) / `"bold"` (700) を使う。Noto Sans CJK JP は Thin〜Black を持つ。
- **1fr 行が伸びない**: 親に固定高さが無いと `fr` は効かない。ページか上位の `grid` の行で高さを確定させる。
- **PDF が複数ページに割れる**: ページ高さに対し中身が多い。フォント/余白を詰めるか、主役を `1fr` 行に収める。

## 詳細

`references/template.typ` に 4:3 インフォグラフィックの最小雛形を置いている。
