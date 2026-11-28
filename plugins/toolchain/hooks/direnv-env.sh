#!/bin/bash
# SessionStart / CwdChanged hook: cwd の .envrc を direnv で評価し、差分を CLAUDE_ENV_FILE へ書く。
# 仕組みと発火の契機は toolchain:resolving-path skill の「仕組み」にある。
#
# 評価先は new_cwd ではなく cwd。作業ディレクトリの外へ cd すると Claude Code は cwd を戻すが
# CwdChanged は再発火せず、戻し先が cwd、cd の行き先が new_cwd に入る。
# 書き込みは追記ではなく mv で置き換える。hook が重なると追記はチャンク単位で混ざり、
# 以後の Bash が preamble で全部失敗する。
# fail-open: direnv が失敗しても exit 0 で戻り、systemMessage で知らせる。

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
HOOK_NAME="direnv-env"

if [ ! -f "$SCRIPT_DIR/lib/hook-logger.sh" ]; then
  printf '%s\n' "ERROR: [$HOOK_NAME] hook-logger.sh not found at $SCRIPT_DIR/lib/hook-logger.sh" >&2
  exit 1
fi
# shellcheck disable=SC1091 # Dynamically resolved path
source "$SCRIPT_DIR/lib/hook-logger.sh"

restore_path_snippet="$SCRIPT_DIR/lib/direnv-env-restore-path.sh"
if [ ! -f "$restore_path_snippet" ]; then
  log_error "direnv-env-restore-path.sh not found at $restore_path_snippet"
  exit 1
fi

if ! command -v direnv >/dev/null; then
  log_error "direnv not found in PATH"
  exit 1
fi

[ -n "${CLAUDE_ENV_FILE:-}" ] || exit 0

input=$(cat)
target=$(printf '%s' "$input" | jq -r '.cwd // empty')
new_cwd=$(printf '%s' "$input" | jq -r '.new_cwd // empty')
if [ -z "$target" ]; then
  log_error "cwd is missing in hook input"
  exit 0
fi
if [ -n "$new_cwd" ] && [ "$new_cwd" != "$target" ]; then
  log_info "cwd was reset to $target (cd target: $new_cwd); evaluating direnv there"
fi

env_dir=$(dirname "$CLAUDE_ENV_FILE")
err_file=$(mktemp "${TMPDIR:-/tmp}/$HOOK_NAME.XXXXXX")
staged=$(mktemp "$env_dir/.$HOOK_NAME.XXXXXX")
trap 'rm -f "$err_file" "$staged"' EXIT

if ! exports=$(cd "$target" && direnv export bash 2>"$err_file"); then
  log_error "direnv export failed in $target: $(cat "$err_file")"
  # 未許可と .envrc の失敗を見分けられるよう、direnv の stderr の末尾をそのまま載せる。
  # direnv は出力先によらず色を付けるので、エスケープシーケンスを除く
  err_tail=$(tail -n 3 "$err_file" | sed $'s/\e\\[[0-9;]*m//g')
  jq -n --arg d "$target" --arg e "$err_tail" \
    '{systemMessage: ("direnv の読み込みに失敗しました (" + $d + "):\n" + $e)}'
  exit 0
fi

printf '%s\n' "$exports" >"$staged"
if ! bash -n "$staged" 2>"$err_file"; then
  log_error "direnv export in $target produced output that is not valid shell: $(cat "$err_file")"
  jq -n --arg d "$target" \
    '{systemMessage: ("direnv の出力が shell として壊れているため CLAUDE_ENV_FILE へ書きませんでした (" + $d + ")。.envrc の出力を確認してください")}'
  exit 0
fi

# Bash ツールのシェルは hook の PATH の末尾に plugin の bin/ を足してから env file を実行する。
# direnv の PATH は hook の PATH をもとにした絶対値なので、そのままでは plugin の bin/ が消える。
# export の前の PATH と hook の PATH を残し、末尾の断片で plugin の bin/ を付け直す
# shellcheck disable=SC2016 # env file の中で展開させる
{
  printf '%s\n' '__direnv_env_bash_path=$PATH'
  printf '%s\n' "$exports"
  printf '__direnv_env_hook_path=%q\n' "$PATH"
  cat "$restore_path_snippet"
} >"$staged"

if ! mv -f "$staged" "$CLAUDE_ENV_FILE"; then
  log_error "failed to replace $CLAUDE_ENV_FILE"
  exit 0
fi
