#!/bin/bash
set -euo pipefail
# guard-optin-env.sh
# opt-in 用の環境変数をプロジェクトスコープの settings へ書き込む操作をブロックする。
#
# CLAUDE_ALLOW_GH_PR_WRITES のような opt-in キーは、guard-bash の ask を
# スキップさせる。Claude Code 自身がこれを書き込めると自分にかかったガードを自分で
# 外せてしまい、opt-in の意味が無くなる。有効化はユーザーが手で行う。
#
# 対象は .claude/settings.json と .claude/settings.local.json への書き込みだけ。
# ドキュメントやソースコードではキー名に言及できる
# (user スコープの ~/.claude/settings.json での定義は guard-bash 側が無効化するため、
# そこに書かれても opt-in は成立しない)。
#
# Bash はコマンド文字列から書き込みを完全には判別できない。宛先が settings に
# なっている明らかな形 (リダイレクト・tee/mv/cp・sed -i) だけを deny し、網羅は
# 狙わない。取りこぼしは sandbox が拾う: .claude/settings.json /
# settings.local.json は sandbox の書き込み拒否対象で、Bash からの touch すら
# Operation not permitted になる。網羅を狙って条件を広げる方が、言及しただけの
# コマンドを止める害の方が大きい。

readonly HOOK_NAME="guard-optin-env"

# log_info MSG はログを stderr へ出す。
log_info() {
  printf '[%s] INFO   %s\n' "$HOOK_NAME" "$1" >&2
}

# deny_tool REASON は PreToolUse の deny を stdout へ出す。jq で JSON エスケープする。
deny_tool() {
  jq -cn --arg r "$1" \
    '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":$r}}'
}

# OPT_IN_KEYS は Claude Code が settings へ書き込んではいけない opt-in 用の環境変数名。
OPT_IN_KEYS=(
  CLAUDE_ALLOW_GH_PR_WRITES
  CLAUDE_ALLOW_GH_ISSUE_WRITES
)

# MATCHED_KEY は matched_opt_in_key が見つけたキー名を返すための変数。
MATCHED_KEY=""

# matched_opt_in_key TEXT
#   0 = opt-in キーを含む (MATCHED_KEY に設定), 1 = 含まない
matched_opt_in_key() {
  local text="$1" key
  for key in "${OPT_IN_KEYS[@]}"; do
    case "$text" in
      *"$key"*)
        MATCHED_KEY="$key"
        return 0
        ;;
    esac
  done
  return 1
}

# is_project_settings_path PATH
#   0 = プロジェクトスコープの settings ファイル
is_project_settings_path() {
  case "$1" in
    */.claude/settings.json | */.claude/settings.local.json) return 0 ;;
    .claude/settings.json | .claude/settings.local.json) return 0 ;;
    *) return 1 ;;
  esac
}

# SETTINGS_PATH_RE はプロジェクトスコープの settings を指すパスの正規表現。
SETTINGS_PATH_RE='\.claude/settings(\.local)?\.json'

# writes_to_project_settings TEXT
#   0 = プロジェクトスコープの settings へ書き込む形を含む
#
# 「対象ファイル名を含む」だけで判定すると、コミットメッセージや PR 本文で
# settings に言及しただけで deny してしまう (guard-bash が
# selectGroups で避けているのと同じ誤検知)。宛先として現れる形だけを見る。
writes_to_project_settings() {
  local text="$1"
  # リダイレクト先
  if printf '%s' "$text" | grep -qE '>[[:space:]]*[^[:space:]|;&]*'"$SETTINGS_PATH_RE"; then
    return 0
  fi
  # ファイルを置き換えるコマンドの引数
  if printf '%s' "$text" | grep -qE '(^|[[:space:]])(tee|mv|cp)[[:space:]].*'"$SETTINGS_PATH_RE"; then
    return 0
  fi
  # sed による in-place 編集
  if printf '%s' "$text" | grep -qE '(^|[[:space:]])sed[[:space:]].*-i.*'"$SETTINGS_PATH_RE"; then
    return 0
  fi
  return 1
}

INPUT=$(cat)

TSV=$(printf '%s' "$INPUT" | jq -r '[
  .tool_name,
  (.tool_input.file_path // ""),
  ((.tool_input.content // "") + (.tool_input.new_string // "") + (.tool_input.command // ""))
] | @tsv')

# @tsv は制御文字をエスケープするので 1 行に収まる。read は IFS に含まれる空白類
# (タブを含む) の連続を 1 つのデリミタに畳むため、file_path が無い Bash 入力で
# フィールドがずれる。パラメータ展開で分割して空フィールドを保つ。
TOOL_NAME="${TSV%%$'\t'*}"
REST="${TSV#*$'\t'}"
FILE_PATH="${REST%%$'\t'*}"
PAYLOAD="${REST#*$'\t'}"

case "$TOOL_NAME" in
  Write | Edit)
    is_project_settings_path "$FILE_PATH" || exit 0
    ;;
  Bash)
    writes_to_project_settings "$PAYLOAD" || exit 0
    ;;
  *)
    exit 0
    ;;
esac

if matched_opt_in_key "$PAYLOAD"; then
  log_info "BLOCKED: $TOOL_NAME が $MATCHED_KEY を settings へ書き込もうとしました"
  deny_tool "$MATCHED_KEY はユーザーが手で設定する opt-in キーです。Claude Code からプロジェクトの .claude/settings.json / settings.local.json へ書き込むことはできません。有効化が必要ならユーザーに依頼してください。現在の設定を確認したいだけなら Read ツールでファイルを読んでください。"
  exit 0
fi

exit 0
