#!/usr/bin/env bash
# PNG を kitty graphics protocol で端末に描く。rex の block の中で実行する前提 (Claude Code の Bash の stdout は
# 端末へ届かないので、`rex split -- /bin/bash show-image.sh <png>` の形で呼ぶ)。block は devShell の外で動き、
# /bin/bash は macOS の bash 3.2 なので、4 以降の機能 (mapfile、連想配列) は使わない。
# 転送は直接 (base64 を APC に載せる)。APC 1 つの payload は 4096 byte までなので chunk に割り、
# 最後の chunk だけ m=0 にする。
set -euo pipefail

if [ $# -ne 1 ]; then
  printf 'usage: %s <png>\n' "$0" >&2
  exit 2
fi
png=$1
if [ ! -f "$png" ]; then
  printf 'not a file: %s\n' "$png" >&2
  exit 1
fi
case $(file -b --mime-type "$png") in
  image/png) ;;
  *)
    printf 'not a PNG: %s (%s)\n' "$png" "$(file -b --mime-type "$png")" >&2
    exit 1
    ;;
esac

apc_start=$'\033_G'
apc_end=$'\033\\'
first=1

# emit は chunk を APC 1 つにして出す。more は後に chunk が続くなら 1。
emit() {
  if [ "$first" -eq 1 ]; then
    printf '%sa=T,f=100,q=2,m=%d;%s%s' "$apc_start" "$2" "$1" "$apc_end"
    first=0
  else
    printf '%sm=%d;%s%s' "$apc_start" "$2" "$1" "$apc_end"
  fi
}

# 最後の chunk だけ m=0 にするため、1 つ遅らせて出す (bash 3.2 には mapfile が無い)。
# fold の最後の行は改行で終わらないので、read が失敗しても chunk が残っていれば回す。
pending=
while IFS= read -r chunk || [ -n "$chunk" ]; do
  if [ -n "$pending" ]; then
    emit "$pending" 1
  fi
  pending=$chunk
done < <(base64 < "$png" | tr -d '\n' | fold -w 4096)
emit "$pending" 0
printf '\n%s\n' "$png"
