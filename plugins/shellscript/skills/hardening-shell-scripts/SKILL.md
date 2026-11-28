---
name: hardening-shell-scripts
description: bash スクリプトを書く・直すとき、Taskfile.yml・.pre-commit-config.yaml・CI 定義にコマンドを書くときに使う。
---

# hardening-shell-scripts

shellcheck が拾えない方針と、
**書いた本人からは成功したように見えて実は壊れている**形、その壊れ方を構造で防ぐ書き方を扱う。

## 基本規則

- `set -euo pipefail` をスクリプト冒頭に記述する
- `|| true` でエラーを握り潰さない。`if !` で捕捉してログ出力する
- 関数の戻り値・`mktemp`/`mv` の失敗を必ずチェックする (`set -e` は if ブロック内で抑制される)
- `echo` より `printf '%s\n'` を優先する (引数解釈の制御)
- 前提条件未充足 (ツール未インストール等) はエラー終了する。警告してスキップする防御的パターンは書かない
- ループ内でリモート呼び出し (`gh` `gcloud` `curl` 等) するときは stderr を捨てない (`2>/dev/null` 禁止)。失敗時に `rc=1` しか残らないと TLS エラーや guard deny を後から特定できなくなる。`out=$(cmd 2>&1)` で捕捉するか stderr をそのまま流す
- 外部入力のパスは絶対パスであることを検証する
- 2 行を超えるロジックを `Taskfile.yml` や `.pre-commit-config.yaml` や CI 定義へ埋め込まない。専用ファイルへ切り出して呼ぶだけにする (YAML のエスケープと混ざって読めなくなり、単体でテストもできない)

## トラブルシュート

| 症状 | ルール |
|------|--------|
| 壊れた JSON を渡すと「キーがありません」と出る | [エラー原因を分離する](#エラー原因を分離する) |
| 配列に数値や null が 1 つ混ざると jq が異常終了する | [jq の型ガード](#jq-の型ガード) |
| ライブラリ不在時に bash の生エラーで死ぬ | [source 前の存在チェック](#source-前の存在チェック) |
| 途中で上書きされた値のまま最後まで動き、結果だけがおかしい | [変えない値を readonly にする](#変えない値を-readonly-にする) |

## エラー原因を分離する

「JSON として壊れている」と「キーが無い」は別の問題。1 つの `jq -e` にまとめると、
**壊れた JSON がキー不在として報告され**、利用者は無関係な箇所を探すことになる。

```bash
# NG: パース失敗もキー不在も同じメッセージに落ちる
if ! jq -e '.tools | type == "array"' "$input" >/dev/null 2>&1; then
  log_warn "no top-level 'tools' array: $input"; exit 1
fi

# OK: 段階的に切り分け、原因ごとに違うことを言う
if ! err=$(jq empty "$input" 2>&1); then
  log_warn "invalid JSON: $input: $err"; exit 1
fi
if ! jq -e 'has("tools")' "$input" >/dev/null; then
  log_warn "key 'tools' not found: $input"; exit 1
fi
```

`>/dev/null 2>&1` で jq の stderr を捨てないこと。捨てると構文エラーの行番号まで失う。

## jq の型ガード

`test()` / `startswith()` / `ascii_downcase` は**文字列以外を渡されると即座に異常終了する**。
手編集されうる JSON の配列は型が混ざりうるので、値を触る前に必ず絞る。

```bash
# NG: 配列に数値が 1 つあるだけで全体が落ちる
jq -r '.tools[] | select(startswith("mcp__"))'

# OK (抽出): strings で文字列だけ通す
jq -r '.tools[] | strings | select(startswith("mcp__"))'

# OK (書き換え): 非文字列は保持したまま、文字列だけ判定する
jq '.tools |= map(if type == "string" then select(test($pat) | not) else . end)'
```

抽出と書き換えで形が違う点に注意。`strings` は非文字列を**捨てる**ので、
元の配列を書き戻す用途では `if type == "string"` を使う。

## source 前の存在チェック

ロガーを読む前にロガーは使えない。`source` が失敗すると `set -e` で即死し、
その時点では `log_warn` も未定義なので、利用者には bash の生エラーしか残らない。

```bash
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
if [ ! -f "$SCRIPT_DIR/lib/logger.sh" ]; then
  printf 'ERROR: logger not found: %s\n' "$SCRIPT_DIR/lib/logger.sh" >&2
  exit 1
fi
source "$SCRIPT_DIR/lib/logger.sh"
```

stderr へ直接書くのはここだけ。ロガーが載ったあとは `log_warn` に切り替える。

## 変えない値を readonly にする

値が確定した時点で `readonly` にする。再代入を残すのは、次のどれかに当たる変数だけ。

- ループや関数呼び出しのたびに更新する累積値 (終了コードの集約、件数)
- 複数回 `source` されうるライブラリの中で宣言する変数 (ロガーの `DRY_RUN`、`HOOK_NAME`)

```bash
# NG: $(cat) の失敗が readonly の終了コード 0 に隠れる (SC2155)
readonly INPUT=$(cat)

# OK: 代入と凍結を分ける。代入の失敗は set -e が拾う
INPUT=$(cat)
readonly INPUT

# OK: 環境変数で上書きさせる値も、決めたあとで凍結する
if [ -z "${SCRIPT_DIR:-}" ]; then
  SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
fi
readonly SCRIPT_DIR
```

関数の中では `local -r` を使う。関数の中の `readonly` はグローバル変数を作るので、
同じ関数を 2 回呼ぶと 2 回目が `readonly variable` で落ちる。

ライブラリの中で `readonly` にすると、同じプロセスが 2 回 `source` したとき
`readonly variable` になって落ちる。ライブラリの定数を凍結するなら、二重読み込みを先頭で弾いてからにする。

## 見落としやすい形

- **jq の終了コードだけ見る**: `if ! jq -e ...` は、false/null が返った (終了コード 1) のも
  実行に失敗した (2 以上) のもまとめて弾く。区別したいなら `jq empty` で妥当性を先に確かめる
- **空結果の改行**: `printf '%s\n' "$result" > out` は結果が空でも 1 行書く。
  0 件と 1 件 (空文字) を取り違えるので、件数で分岐するか `jq -r` の出力を直接リダイレクトする
