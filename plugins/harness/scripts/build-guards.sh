#!/bin/bash
# build-guards.sh [--if-stale]
# guard-bash と guard-home-dir を ${CLAUDE_PLUGIN_DATA}/bin/ へ建てる。
#
# 置き場を plugin の data にするのは、plugin の update で ${CLAUDE_PLUGIN_ROOT} が変わっても残り、
# SessionStart hook (Bash tool の sandbox の外で動く) から書けるため。
# 建て直しの判定は guard ごとに、入力の hash を stamp と比べて行う。入力は plugin 直下の go.mod と go.sum、
# cmd/<guard> と internal の下の *.go。
#
# --if-stale は SessionStart hook 用。
# - 入力が前回のビルドと同じなら何もしない (go が無くても黙って終わる)
# - go が無ければ 1 行知らせて 0 で終わる。guard のバイナリが無い間、PreToolUse の入口は deny を返すので、
#   会話の中で原因が分かるようにする (SessionStart の stdout は会話に入る)
# - ビルドに失敗しても 0 で終わり、log の場所を stdout に 1 行出す
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
readonly SCRIPT_DIR
PLUGIN_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
readonly PLUGIN_ROOT
readonly GUARDS="guard-bash guard-home-dir"

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
  printf 'build-guards: CLAUDE_PLUGIN_DATA is not set\n' >&2
  exit 1
fi
case "$CLAUDE_PLUGIN_DATA" in
  /*) ;;
  *)
    printf 'build-guards: CLAUDE_PLUGIN_DATA must be an absolute path: %s\n' "$CLAUDE_PLUGIN_DATA" >&2
    exit 1
    ;;
esac
readonly BIN_DIR="$CLAUDE_PLUGIN_DATA/bin"
readonly LOG="$CLAUDE_PLUGIN_DATA/build-guards.log"

# hash_inputs GUARD は GUARD の入力を決まった順に連結して stdout へ出す。
hash_inputs() {
  find "$PLUGIN_ROOT" -maxdepth 1 -type f \( -name go.mod -o -name go.sum \) -print0 | LC_ALL=C sort -z | xargs -0 cat
  find "$PLUGIN_ROOT/cmd/$1" "$PLUGIN_ROOT/internal" -type f -name '*.go' -print0 | LC_ALL=C sort -z | xargs -0 cat
}

# stamp_of GUARD は GUARD の入力の hash を出す。
stamp_of() {
  local stamp
  if ! stamp=$(hash_inputs "$1" | git hash-object --stdin); then
    printf 'build-guards: cannot hash the sources of %s under %s\n' "$1" "$PLUGIN_ROOT" >&2
    return 1
  fi
  printf '%s\n' "$stamp"
}

# is_fresh GUARD STAMP はバイナリがあり、前回のビルドの stamp が STAMP と同じなら 0 を返す。
is_fresh() {
  local built
  [ -x "$BIN_DIR/$1" ] && [ -f "$BIN_DIR/$1.stamp" ] || return 1
  built=$(cat "$BIN_DIR/$1.stamp") || return 1
  [ "$built" = "$2" ]
}

stale=""
for guard in $GUARDS; do
  stamp=$(stamp_of "$guard")
  if [ "$if_stale" = true ] && is_fresh "$guard" "$stamp"; then
    continue
  fi
  stale="$stale $guard"
done
if [ -z "$stale" ]; then
  exit 0
fi

if ! command -v go >/dev/null 2>&1; then
  if [ "$if_stale" = true ]; then
    printf 'harness: go が PATH に無いため guard (%s) をビルドできない。バイナリが無い間、guard の対象のツール呼び出しは deny される。Go を入れてセッションを開き直す\n' "${stale# }"
    exit 0
  fi
  printf 'build-guards: go not found in PATH. Install Go (https://go.dev/doc/install) and run this script again\n' >&2
  exit 1
fi

if ! mkdir -p "$BIN_DIR"; then
  printf 'build-guards: mkdir failed: %s\n' "$BIN_DIR" >&2
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

: >"$LOG"
for guard in $stale; do
  stamp=$(stamp_of "$guard")
  if ! tmp_bin=$(mktemp "$BIN_DIR/.$guard.XXXXXX"); then
    printf 'build-guards: mktemp failed in %s\n' "$BIN_DIR" >&2
    exit 1
  fi
  # GOTOOLCHAIN=auto はこの呼び出しにだけ渡す。PATH の go が go.mod より古くても toolchain を取りに行ける。
  if ! GOTOOLCHAIN=auto go build -C "$PLUGIN_ROOT" -o "$tmp_bin" "./cmd/$guard" >>"$LOG" 2>&1; then
    if [ "$if_stale" = true ]; then
      printf 'harness: %s のビルドに失敗した。原因は %s にある。バイナリが無い間、guard の対象のツール呼び出しは deny される\n' "$guard" "$LOG"
      exit 0
    fi
    cat "$LOG" >&2
    printf 'build-guards: go build failed for %s (log: %s)\n' "$guard" "$LOG" >&2
    exit 1
  fi
  if ! chmod 755 "$tmp_bin"; then
    printf 'build-guards: chmod failed: %s\n' "$tmp_bin" >&2
    exit 1
  fi
  if ! mv "$tmp_bin" "$BIN_DIR/$guard"; then
    printf 'build-guards: mv failed: %s -> %s\n' "$tmp_bin" "$BIN_DIR/$guard" >&2
    exit 1
  fi
  tmp_bin=""

  # stamp はバイナリを置いた後に書く。逆順だと、途中で落ちたとき古いバイナリが新しい入力を名乗る。
  if ! tmp_stamp=$(mktemp "$BIN_DIR/.$guard.stamp.XXXXXX"); then
    printf 'build-guards: mktemp failed in %s\n' "$BIN_DIR" >&2
    exit 1
  fi
  if ! printf '%s\n' "$stamp" >"$tmp_stamp"; then
    printf 'build-guards: write failed: %s\n' "$tmp_stamp" >&2
    exit 1
  fi
  if ! mv "$tmp_stamp" "$BIN_DIR/$guard.stamp"; then
    printf 'build-guards: mv failed: %s -> %s\n' "$tmp_stamp" "$BIN_DIR/$guard.stamp" >&2
    exit 1
  fi
  tmp_stamp=""

  if [ "$if_stale" = false ]; then
    printf 'built %s\n' "$BIN_DIR/$guard"
  fi
done
