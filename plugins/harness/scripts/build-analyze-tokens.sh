#!/bin/bash
# build-analyze-tokens.sh [--if-stale]
# analyze-tokens を ${CLAUDE_PLUGIN_DATA}/bin/analyze-tokens へ建てる。
#
# 置き場を plugin の data にするのは、plugin の update で ${CLAUDE_PLUGIN_ROOT} が変わっても残り、
# SessionStart hook (Bash tool の sandbox の外で動く) から書けるため。
# 建て直しの判定は、入力の hash を stamp と比べて行う。入力は plugin 直下の go.mod と go.sum (あれば)、
# cmd/analyze-tokens と internal の下の *.go と *.json。*.json は go:embed で埋め込む価格表を拾うため。
#
# --if-stale は SessionStart hook 用。
# - 入力が前回のビルドと同じなら何もしない
# - go が無ければ黙って終わる。harness の他の skill は go を要らないので、セッションを止めず警告も出さない。
#   auditing-token-usage skill は、バイナリが無いことを呼ぶ前に確かめて利用者に伝える
# - ビルドに失敗しても 0 で終わり、log の場所を stdout に 1 行出す (SessionStart の stdout は会話に入る)
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
readonly SCRIPT_DIR
PLUGIN_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
readonly PLUGIN_ROOT

if_stale=false
case "${1:-}" in
  "") ;;
  --if-stale) if_stale=true ;;
  *)
    printf 'usage: %s [--if-stale]\n' "$0" >&2
    exit 2
    ;;
esac
readonly if_stale

if [ -z "${CLAUDE_PLUGIN_DATA:-}" ]; then
  printf 'build-analyze-tokens: CLAUDE_PLUGIN_DATA is not set\n' >&2
  exit 1
fi
case "$CLAUDE_PLUGIN_DATA" in
  /*) ;;
  *)
    printf 'build-analyze-tokens: CLAUDE_PLUGIN_DATA must be an absolute path: %s\n' "$CLAUDE_PLUGIN_DATA" >&2
    exit 1
    ;;
esac
readonly BIN_DIR="$CLAUDE_PLUGIN_DATA/bin"
readonly BIN="$BIN_DIR/analyze-tokens"
readonly STAMP="$BIN_DIR/analyze-tokens.stamp"
readonly LOG="$CLAUDE_PLUGIN_DATA/build-analyze-tokens.log"

if ! command -v go >/dev/null 2>&1; then
  if [ "$if_stale" = true ]; then
    exit 0
  fi
  printf 'build-analyze-tokens: go not found in PATH. Install Go (https://go.dev/doc/install) and run this script again\n' >&2
  exit 1
fi

# hash_inputs は入力を決まった順に連結して stdout へ出す。
# go.sum は依存を持たない間は無いので、find で拾える分だけを入れる。
hash_inputs() {
  find "$PLUGIN_ROOT" -maxdepth 1 -type f \( -name go.mod -o -name go.sum \) -print0 | LC_ALL=C sort -z | xargs -0 cat
  find "$PLUGIN_ROOT/cmd/analyze-tokens" "$PLUGIN_ROOT/internal" -type f \( -name '*.go' -o -name '*.json' \) -print0 |
    LC_ALL=C sort -z | xargs -0 cat
}

if ! stamp=$(hash_inputs | git hash-object --stdin); then
  printf 'build-analyze-tokens: cannot hash the sources under %s\n' "$PLUGIN_ROOT" >&2
  exit 1
fi
readonly stamp

if [ "$if_stale" = true ] && [ -x "$BIN" ] && [ -f "$STAMP" ]; then
  if ! built=$(cat "$STAMP"); then
    printf 'build-analyze-tokens: cannot read %s\n' "$STAMP" >&2
    exit 1
  fi
  if [ "$built" = "$stamp" ]; then
    exit 0
  fi
fi

if ! mkdir -p "$BIN_DIR"; then
  printf 'build-analyze-tokens: mkdir failed: %s\n' "$BIN_DIR" >&2
  exit 1
fi

tmp_bin=""
tmp_stamp=""
cleanup() {
  if [ -n "$tmp_bin" ] && [ -e "$tmp_bin" ]; then
    rm -f "$tmp_bin"
  fi
  if [ -n "$tmp_stamp" ] && [ -e "$tmp_stamp" ]; then
    rm -f "$tmp_stamp"
  fi
}
trap cleanup EXIT

if ! tmp_bin=$(mktemp "$BIN_DIR/.analyze-tokens.XXXXXX"); then
  printf 'build-analyze-tokens: mktemp failed in %s\n' "$BIN_DIR" >&2
  exit 1
fi

# GOTOOLCHAIN=auto はこの呼び出しにだけ渡す。PATH の go が go.mod より古くても toolchain を取りに行ける。
if ! GOTOOLCHAIN=auto go build -C "$PLUGIN_ROOT" -o "$tmp_bin" ./cmd/analyze-tokens >"$LOG" 2>&1; then
  if [ "$if_stale" = true ]; then
    printf 'harness: analyze-tokens のビルドに失敗した。原因は %s にある\n' "$LOG"
    exit 0
  fi
  cat "$LOG" >&2
  printf 'build-analyze-tokens: go build failed (log: %s)\n' "$LOG" >&2
  exit 1
fi
if ! chmod 755 "$tmp_bin"; then
  printf 'build-analyze-tokens: chmod failed: %s\n' "$tmp_bin" >&2
  exit 1
fi
if ! mv "$tmp_bin" "$BIN"; then
  printf 'build-analyze-tokens: mv failed: %s -> %s\n' "$tmp_bin" "$BIN" >&2
  exit 1
fi
tmp_bin=""

# stamp はバイナリを置いた後に書く。逆順だと、途中で落ちたとき古いバイナリが新しい入力を名乗る。
if ! tmp_stamp=$(mktemp "$BIN_DIR/.analyze-tokens.stamp.XXXXXX"); then
  printf 'build-analyze-tokens: mktemp failed in %s\n' "$BIN_DIR" >&2
  exit 1
fi
if ! printf '%s\n' "$stamp" >"$tmp_stamp"; then
  printf 'build-analyze-tokens: write failed: %s\n' "$tmp_stamp" >&2
  exit 1
fi
if ! mv "$tmp_stamp" "$STAMP"; then
  printf 'build-analyze-tokens: mv failed: %s -> %s\n' "$tmp_stamp" "$STAMP" >&2
  exit 1
fi
tmp_stamp=""

if [ "$if_stale" = false ]; then
  printf 'built %s\n' "$BIN"
fi
