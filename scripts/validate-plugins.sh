#!/usr/bin/env bash
# marketplace.json の相対パス source を 1 つずつ claude plugin validate --strict にかける。
# marketplace の validate は source のディレクトリが無くても通るため、ここで存在も確かめる。
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
readonly ROOT
readonly MANIFEST="$ROOT/.claude-plugin/marketplace.json"

for cmd in jq claude; do
  if ! command -v "$cmd" >/dev/null; then
    printf 'ERROR: command not found: %s\n' "$cmd" >&2
    exit 1
  fi
done

if ! err=$(jq empty "$MANIFEST" 2>&1); then
  printf 'ERROR: invalid JSON: %s: %s\n' "$MANIFEST" "$err" >&2
  exit 1
fi

sources=$(jq -r '.plugins[].source | strings' "$MANIFEST")
readonly sources

if [ -z "$sources" ]; then
  printf 'no relative-path plugins in %s\n' "$MANIFEST"
  exit 0
fi

rc=0
while IFS= read -r src; do
  dir="$ROOT/${src#./}"
  if [ ! -f "$dir/.claude-plugin/plugin.json" ]; then
    printf 'ERROR: plugin.json not found for source %s: %s\n' "$src" "$dir" >&2
    rc=1
    continue
  fi
  printf '== %s\n' "$src"
  if ! claude plugin validate --strict "$dir"; then
    rc=1
  fi
done <<<"$sources"

exit "$rc"
