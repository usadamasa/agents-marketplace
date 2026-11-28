// ============================================================
//  4:3 インフォグラフィック 最小雛形 (Typst)
//  生成: typst compile template.typ out.pdf
//  プレビュー: typst compile template.typ preview.png --ppi 96
//
//  設計のコツ:
//   - HTML モックの px 値を pt に 1:1 移植 (1600x1200px → 1600pt x 1200pt)
//   - トップレベル grid の `1fr` 行で主役を伸ばしページ高さを埋める
//   - 色は変数化、装飾は block(fill/stroke/radius/inset)、グラデは gradient.linear
// ============================================================

#let ink   = rgb("#0b1f30")
#let sub   = rgb("#5b6b7d")
#let accent1 = rgb("#2563eb")
#let accent2 = rgb("#dc2626")
#let line  = rgb("#e2e8f0")

#set page(
  width: 1600pt, height: 1200pt,
  margin: (x: 32pt, top: 24pt, bottom: 20pt),
  fill: rgb("#eef2f6"),
)
// Noto を第一候補にして環境非依存に (`typst fonts | grep -i noto` で名前確認)
#set text(font: ("Noto Sans CJK JP", "Hiragino Sans"), fill: ink, lang: "ja")

// ---- ヘッダー ----
#let header = block(
  width: 100%, fill: gradient.linear(rgb("#15294d"), rgb("#1f3a5f"), angle: 35deg),
  radius: 18pt, inset: (x: 32pt, y: 18pt),
)[
  #text(fill: rgb("#9fc0ff"), size: 13pt, weight: "bold", tracking: 2pt)[KICKER / SUBTITLE]
  #v(4pt, weak: true)
  #text(fill: white, size: 37pt, weight: "black")[ここにメインタイトル]
]

// ---- カード (主役グリッドのセル) ----
#let card(accent, icbg, iconpath, title, body) = block(
  width: 100%, height: 100%, fill: white, radius: 18pt,
  stroke: (top: 8pt + accent, rest: 1pt + line), inset: (x: 30pt, y: 22pt),
)[
  #grid(columns: (auto, 1fr), column-gutter: 26pt, align: horizon,
    block(fill: icbg, radius: 24pt, inset: 18pt)[#image(iconpath, width: 88pt)],
    [
      #text(fill: accent, size: 28pt, weight: "black")[#title]
      #v(8pt, weak: true)
      #text(fill: rgb("#3a4d61"), size: 16pt)[#body]
    ]
  )
]

// ---- 主役: 2x2 グリッド (1fr 行/列で領域を埋める) ----
#let grid-main = grid(
  columns: (1fr, 1fr), rows: (1fr, 1fr),
  column-gutter: 14pt, row-gutter: 14pt,
  card(accent1, rgb("#eaf2ff"), "icons/a.svg", [見出しA], [本文を簡潔に。キーワードは #text(weight: "bold")[太字] で強調]),
  card(accent1, rgb("#eaf2ff"), "icons/b.svg", [見出しB], [本文]),
  card(accent2, rgb("#fdecec"), "icons/c.svg", [見出しC], [本文]),
  card(accent2, rgb("#fdecec"), "icons/d.svg", [見出しD], [本文]),
)

// ---- フッター ----
#let footer = align(right)[#text(size: 10.5pt, fill: sub)[出典 / 補足をここに]]

// ---- レイアウト (1fr 行でページ高さを埋める) ----
#grid(rows: (auto, 1fr, auto), row-gutter: 13pt,
  header,
  grid-main,
  footer,
)
