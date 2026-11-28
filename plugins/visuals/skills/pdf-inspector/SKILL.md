---
name: pdf-inspector
description: >-
  ローカルの PDF からテキスト・表・Markdown を取り出すとき、
  その PDF が text-based かスキャン画像かを判定したいときに使う。
  PDF を読む・引用する・要約する・表を抜き出す依頼が対象。
---

# pdf-inspector

[firecrawl/pdf-inspector](https://github.com/firecrawl/pdf-inspector) は Rust 製の
PDF 分類・テキスト抽出ライブラリ。OCR を使わず、text-based PDF をローカルで
Markdown 化する。CLI として `pdf2md` (抽出) と `detect-pdf` (分類のみ) を提供する。

Read ツールに PDF を渡すと全ページが画像として context に載る。`pdf2md` は
テキストだけを返すので、数十ページの PDF でも context 消費が桁違いに小さい。
ただし後述のとおり **抽出できない PDF でも成功したように見える** ので、
検証をスキップしてはいけない。

## インストール

```bash
cargo install pdf-inspector   # pdf2md と detect-pdf が入る
```

`--help` は無い。引数なしで実行すると usage が出る。

## 抽出のワークフロー

### Step 1: 必ず `--json` で開ける

いきなり `--raw` を叩かない。まず JSON でメタデータを取り、抽出結果を
信用してよいか判定する。

```bash
pdf2md document.pdf --json | jq 'del(.markdown)'
```

```json
{
  "pdf_type": "text_based",
  "page_count": 1,
  "markdown_length": 0,
  "pages_needing_ocr": [1],
  "ocr_reasons_by_page": [{"page": 1, "reasons": ["suspected_garbled_text"]}],
  "has_encoding_issues": true,
  "pages_with_tables": [],
  "pages_with_columns": []
}
```

`--detect-only` で代用しない。抽出を走らせないので `has_encoding_issues` が常に
`false` になる。本文が `???` に化ける PDF で、`--detect-only --json` は
`pages_needing_ocr: []` / `has_encoding_issues: false` を返した。同じ PDF をフル抽出の `--json` にかけると
`[1]` / `true` が返った。フィールドが存在するぶん誤判定に気づきにくい。

### Step 2: 3つのフィールドで合否を判定する

以下のいずれかに該当したら、抽出結果は使わない。

| フィールド | 不合格の条件 |
| ---- | ---- |
| `has_encoding_issues` | `true` — フォントエンコーディングが壊れている |
| `pages_needing_ocr` | 空でない — そのページはテキストが取れていない |
| `markdown_length` | `0` または本文量に対して不自然に小さい |

`pdf_type` と `confidence` は判定に使わない。文字化けした日本語 PDF でも
`text_based` / `confidence: 1.00` を返す。

### Step 3: 合格なら本文を取る

```bash
pdf2md document.pdf --raw               # Markdown のみ
pdf2md document.pdf --raw --compact     # 目次のドットリーダー等を畳んで節約
pdf2md document.pdf --select-pages 1,3,5-10 --raw
pdf2md document.pdf --password PW --raw # 暗号化 PDF
```

### Step 4: 不合格のときの退避先

`pages_needing_ocr` の件数で切り替える。

- **10 ページ以下**: Read ツールで該当ページのみ直読みする。`Read` の `pages`
  パラメータでページを指定できるので、PDF 全体ではなく不合格ページだけを渡す。
- **10 ページ超**: 抽出できない旨と `ocr_reasons_by_page` を報告し、Read で読むか
  外部 OCR に出すかをユーザーに確認する。

全ページを勝手に Read へ流さない。Read は PDF を画像として読むため、
ページ数ぶんの画像が context に載る。

文字化けの程度を確かめたいときは `--items-json` を見る。Markdown が空でも
テキストアイテム自体は返る (空出力の PDF からも 71 items を取れた実例がある)。
アイテムのテキストが読めるなら座標つきで部分回収でき、`???` ばかりなら
デコード不能なので Read へ退避する。

## 分類だけしたいとき

ページ数が多く、抽出せずスキャンかどうかだけ知りたいとき。

```bash
detect-pdf document.pdf --json
detect-pdf document.pdf --analyze --json   # 表・段組みのレイアウト解析つき
```

本文を取り出すつもりなら分類だけで済ませない。`detect-pdf --json` に
`has_encoding_issues` は無く、`pdf2md --detect-only --json` にはあるが常に `false`。
どちらも `pages_needing_ocr: []` を返した PDF が、フル抽出では `[1]` /
`has_encoding_issues: true` だった実例がある。

## 落とし穴

| 症状 | 原因と対処 |
| ---- | ---- |
| 出力が空なのに exit 0 | 文字化け PDF の既定動作。Step 1-2 の JSON 判定で捕まえる |
| CJK が `???` になる | Identity-H / CID フォントのデコード失敗。Step 4 へ |
| `Error: PDF is encrypted` (exit 1) | `--password PW` を付ける |
| `--items-json` が password を無視する | `--password` と併用しても `{"error":"PDF is encrypted"}` で exit 1。暗号化 PDF に `--items-json` は使えない |
| 本文が全部 `##` 見出しになる | 見出し判定はフォントサイズ比。単一サイズの PDF では誤検出する |
| `--pages` のマーカーがページ数と合わない | ページ境界の挿入は best-effort。ページ単位で扱うなら `--select-pages` を1ページずつ回す |

## 出力フォーマット

| フラグ | 用途 |
| ---- | ---- |
| (なし) | ヘッダ付きの人間向け出力。ファイル名・型・処理時間を含む |
| `--raw` | Markdown のみ。パイプ・ファイル保存向け |
| `--json` | メタデータ + markdown。判定に使う |
| `--items-json` | 座標・フォント情報つきテキストアイテム。`{items, total_items, underlined_count}` |
| `--detect-only` / `--analyze` | 抽出せず分類 / レイアウト解析まで |
