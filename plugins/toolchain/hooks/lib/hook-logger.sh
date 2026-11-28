#!/bin/bash
set -euo pipefail
# hook-logger.sh
# hook スクリプトのログ出力ヘルパー。出力先は stderr
#
# 使い方:
#   HOOK_NAME="direnv-env"
#   source "$SCRIPT_DIR/lib/hook-logger.sh"

HOOK_NAME="${HOOK_NAME:-hook}"
_LOG_PREFIX="[$HOOK_NAME]"

# 情報メッセージ (常に出力)
log_info() {
  local msg="$1"
  echo "$_LOG_PREFIX INFO   $msg" >&2
}

# エラーメッセージ (常に出力、重大度: ERROR)
log_error() {
  local msg="$1"
  echo "$_LOG_PREFIX ERROR  $msg" >&2
}
