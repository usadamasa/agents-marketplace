#!/usr/bin/env bats
# dotclaude/skills/showing-images-in-rex/show-image.sh のテスト。
# kitty graphics の APC の並び (最初だけ a=T,f=100、最後だけ m=0) と、chunk を繋ぐと元の base64 に戻ることを見る。
# skill は /bin/bash で起動する (rex の block は devShell の外)。macOS では bash 3.2 なので、テストも同じ bash で
# 走らせて 4 以降の機能の混入を拾う。Linux の /bin/bash は 5 系なので、この検査が効くのは macOS だけ。

SCRIPT="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/dotclaude/skills/showing-images-in-rex/show-image.sh"
SYSTEM_BASH=/bin/bash

setup() {
  TEST_TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/claude-test.XXXXXX")
  export TEST_TMPDIR
  # 1x1 の PNG (base64 で 92 文字。chunk 1 つに収まる)
  printf 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==' \
    | base64 -d > "$TEST_TMPDIR/one.png"
  # 4096 文字の chunk を 3 つ以上またぐ大きさにする (PNG の署名の後ろに乱数を足す。file は署名で PNG と判定する)
  { cat "$TEST_TMPDIR/one.png"; head -c 9000 /dev/urandom; } > "$TEST_TMPDIR/big.png"
}

teardown() {
  [ -n "$TEST_TMPDIR" ] && rm -rf "$TEST_TMPDIR"
}

# APC を 1 行 1 つに並べ、ESC _ と ESC \ を外す
apcs() {
  tr '\033' '\n' < "$1" | sed -n 's/^_G\(.*\)$/\1/p'
}

@test "引数が無ければ usage を出して exit 2" {
  run "$SYSTEM_BASH" "$SCRIPT"

  [ "$status" -eq 2 ]
  [[ "$output" == usage:* ]]
}

@test "PNG でないファイルは exit 1" {
  printf 'hello\n' > "$TEST_TMPDIR/text.txt"
  run "$SYSTEM_BASH" "$SCRIPT" "$TEST_TMPDIR/text.txt"

  [ "$status" -eq 1 ]
  [[ "$output" == "not a PNG:"* ]]
}

@test "小さい PNG は APC 1 つで、最初から m=0" {
  "$SYSTEM_BASH" "$SCRIPT" "$TEST_TMPDIR/one.png" > "$TEST_TMPDIR/out"
  apcs "$TEST_TMPDIR/out" > "$TEST_TMPDIR/apcs"

  [ "$(wc -l < "$TEST_TMPDIR/apcs")" -eq 1 ]
  expected="a=T,f=100,q=2,m=0;$(base64 < "$TEST_TMPDIR/one.png" | tr -d '\n')"
  [ "$(cat "$TEST_TMPDIR/apcs")" = "$expected" ]
}

@test "大きい PNG は 4096 文字ずつの chunk に割り、最初だけ a=T、最後だけ m=0" {
  "$SYSTEM_BASH" "$SCRIPT" "$TEST_TMPDIR/big.png" > "$TEST_TMPDIR/out"
  apcs "$TEST_TMPDIR/out" > "$TEST_TMPDIR/apcs"

  n=$(wc -l < "$TEST_TMPDIR/apcs")
  [ "$n" -ge 3 ]
  [[ "$(sed -n '1p' "$TEST_TMPDIR/apcs")" == "a=T,f=100,q=2,m=1;"* ]]
  [[ "$(sed -n '2p' "$TEST_TMPDIR/apcs")" == "m=1;"* ]]
  [[ "$(sed -n "${n}p" "$TEST_TMPDIR/apcs")" == "m=0;"* ]]
  [ "$(grep -c 'm=0;' "$TEST_TMPDIR/apcs")" -eq 1 ]
  joined=$(sed 's/^[^;]*;//' "$TEST_TMPDIR/apcs" | tr -d '\n')
  [ "$joined" = "$(base64 < "$TEST_TMPDIR/big.png" | tr -d '\n')" ]
}

@test "最後の行に PNG のパスを出す" {
  run "$SYSTEM_BASH" "$SCRIPT" "$TEST_TMPDIR/one.png"

  [ "$status" -eq 0 ]
  [ "${lines[-1]}" = "$TEST_TMPDIR/one.png" ]
}
