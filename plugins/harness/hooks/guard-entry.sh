#!/bin/bash
# guard-entry.sh <guard-bash|guard-home-dir>
# PreToolUse hook の入口。${CLAUDE_PLUGIN_DATA}/bin/<guard> を stdin ごと exec する。
#
# バイナリが無ければ、ビルドを試みずに deny を返す。ビルドは SessionStart の build-guards.sh に任せる
# (初回は module の取得で hook の timeout を超えうる)。guard が無いまま通すと fail-open になるので、
# deny の JSON は jq に頼らず printf で組む。
set -uo pipefail

# json_string S は S を JSON の文字列リテラルにして出す。パスに混ざりうる \ と " と改行だけを扱う。
json_string() {
  local s="$1"
  s=${s//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\n'/\\n}
  printf '"%s"' "$s"
}

deny() {
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":%s}}\n' \
    "$(json_string "$1")"
  exit 0
}

name="${1:-}"
case "$name" in
  guard-bash | guard-home-dir) ;;
  *) deny "harness: 知らない guard の名前です (${name})。hooks.json の配線を確かめてください。" ;;
esac

data="${CLAUDE_PLUGIN_DATA:-}"
if [ -z "$data" ]; then
  deny "harness: CLAUDE_PLUGIN_DATA が渡っていないため、${name} を起動できません。hooks.json の配線を確かめてください。"
fi

bin="$data/bin/$name"
if [ ! -x "$bin" ]; then
  plugin_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
  deny "harness: ${name} のバイナリ (${bin}) が無いため、このツール呼び出しを止めました。バイナリはセッションの開始時に SessionStart hook がビルドします (Go が要ります)。ビルドの結果は ${data}/build-guards.log にあります。直すには、セッションを開き直すか、ユーザーが ! CLAUDE_PLUGIN_DATA='${data}' '${plugin_root}/scripts/build-guards.sh' を実行してください。切り分けは harness:sandbox-troubleshooting skill にあります。"
fi

exec "$bin"
