#!/usr/bin/env bats
# guard の入口 (hooks/guard-entry.sh) と hooks.json の配線のテスト。
#
# PreToolUse の hook が起動できない・時間切れ・非 0 で終わると、onFailure が無ければツール呼び出しは通る。
# 入口は、バイナリが無いときにビルドを試みずに deny を返し、guard が無い状態で通さない。
bats_require_minimum_version 1.5.0

PLUGIN_ROOT="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"
ENTRY="$PLUGIN_ROOT/hooks/guard-entry.sh"
HOOKS_JSON="$PLUGIN_ROOT/hooks/hooks.json"
BASH_INPUT='{"tool_name":"Bash","tool_input":{"command":"ls"},"cwd":"/workspace"}'

# 入口の shebang は #!/bin/bash (macOS では 3.2)。bash 4 以降の機能の混入を検知できるよう、/bin/bash で動かす。
run_entry() {
  run "${HOOK_BASH:-/bin/bash}" "$ENTRY" "$@"
}

assert_deny_json() {
  [ "$status" -eq 0 ]
  printf '%s' "$output" | jq -e '.hookSpecificOutput.hookEventName == "PreToolUse"
    and .hookSpecificOutput.permissionDecision == "deny"' >/dev/null
}

# =============================================================================
# hooks.json の配線
# =============================================================================

# pre_tool_use_hook MATCHER は、その matcher の PreToolUse の hook を 1 つ JSON で出す。
pre_tool_use_hook() {
  jq -c --arg m "$1" '[.hooks.PreToolUse[] | select(.matcher == $m) | .hooks[]] | if length == 1 then .[0] else error("want 1 hook") end' "$HOOKS_JSON"
}

@test "hooks.json: guard-home-dir は Read|Edit|Write|Glob|Grep|NotebookEdit|Bash に入口経由で付く" {
  run pre_tool_use_hook 'Read|Edit|Write|Glob|Grep|NotebookEdit|Bash'
  [ "$status" -eq 0 ]
  [[ "$output" == *'guard-entry.sh\" guard-home-dir'* ]]
}

@test "hooks.json: guard-bash は Bash に入口経由で付く" {
  run pre_tool_use_hook 'Bash'
  [ "$status" -eq 0 ]
  [[ "$output" == *'guard-entry.sh\" guard-bash'* ]]
}

@test "hooks.json: guard-optin-env.sh は Write|Edit|Bash に付く" {
  run pre_tool_use_hook 'Write|Edit|Bash'
  [ "$status" -eq 0 ]
  [[ "$output" == *'hooks/guard-optin-env.sh'* ]]
}

@test "hooks.json: PreToolUse の hook はすべて timeout 10 秒で、失敗したら止める" {
  run jq -e '[.hooks.PreToolUse[].hooks[]] | length == 3
    and all(.type == "command" and .timeout == 10 and .onFailure == "block")' "$HOOKS_JSON"
  [ "$status" -eq 0 ]
}

@test "hooks.json: 入口には CLAUDE_PLUGIN_DATA を明示して渡す" {
  run jq -e '[.hooks.PreToolUse[].hooks[] | .command | select(test("guard-entry.sh"))]
    | length == 2 and all(startswith("CLAUDE_PLUGIN_DATA=\"${CLAUDE_PLUGIN_DATA}\" "))' "$HOOKS_JSON"
  [ "$status" -eq 0 ]
}

@test "hooks.json: SessionStart で guard をビルドする" {
  run jq -e '[.hooks.SessionStart[].hooks[].command] | any(test("scripts/build-guards.sh\" --if-stale$"))' "$HOOKS_JSON"
  [ "$status" -eq 0 ]
}

@test "hooks.json が指すスクリプトはどれも実行できる" {
  local rel
  for rel in hooks/guard-entry.sh hooks/guard-optin-env.sh scripts/build-guards.sh scripts/build-analyze-tokens.sh; do
    [ -x "$PLUGIN_ROOT/$rel" ]
  done
}

# =============================================================================
# バイナリが無い → すぐ deny
# =============================================================================

@test "guard-bash のバイナリが無ければ、10 秒以内に deny を返す" {
  local data="$BATS_TEST_TMPDIR/data" start end
  mkdir -p "$data"
  start=$(date +%s)
  CLAUDE_PLUGIN_DATA="$data" run_entry guard-bash <<<"$BASH_INPUT"
  end=$(date +%s)

  assert_deny_json
  [ $((end - start)) -lt 10 ]
}

@test "guard-home-dir のバイナリが無ければ、10 秒以内に deny を返す" {
  local data="$BATS_TEST_TMPDIR/data" start end
  mkdir -p "$data"
  start=$(date +%s)
  CLAUDE_PLUGIN_DATA="$data" run_entry guard-home-dir <<<'{"tool_name":"Read","tool_input":{"file_path":"/etc/hosts"},"cwd":"/workspace"}'
  end=$(date +%s)

  assert_deny_json
  [ $((end - start)) -lt 10 ]
}

@test "バイナリが無いときの deny は、ビルドの log と開き直しとビルドの script を案内する" {
  local data="$BATS_TEST_TMPDIR/data"
  mkdir -p "$data"
  CLAUDE_PLUGIN_DATA="$data" run_entry guard-bash <<<"$BASH_INPUT"

  assert_deny_json
  local reason
  reason=$(printf '%s' "$output" | jq -r '.hookSpecificOutput.permissionDecisionReason')
  [[ "$reason" == *"$data/bin/guard-bash"* ]]
  [[ "$reason" == *"$data/build-guards.log"* ]]
  [[ "$reason" == *"セッションを開き直す"* ]]
  [[ "$reason" == *"! CLAUDE_PLUGIN_DATA='$data' '$PLUGIN_ROOT/scripts/build-guards.sh'"* ]]
  [[ "$reason" == *"harness:sandbox-troubleshooting"* ]]
}

@test "バイナリが無いときはビルドしない" {
  local data="$BATS_TEST_TMPDIR/data"
  mkdir -p "$data"
  CLAUDE_PLUGIN_DATA="$data" run_entry guard-bash <<<"$BASH_INPUT"

  assert_deny_json
  [ ! -e "$data/bin/guard-bash" ]
  [ ! -e "$data/build-guards.log" ]
}

@test "CLAUDE_PLUGIN_DATA が空なら deny" {
  CLAUDE_PLUGIN_DATA="" run_entry guard-bash <<<"$BASH_INPUT"

  assert_deny_json
  [[ "$output" == *"CLAUDE_PLUGIN_DATA"* ]]
}

@test "知らない guard の名前なら deny" {
  local data="$BATS_TEST_TMPDIR/data"
  mkdir -p "$data/bin"
  CLAUDE_PLUGIN_DATA="$data" run_entry no-such-guard <<<"$BASH_INPUT"

  assert_deny_json
}

@test "パスに引用符を含む置き場でも、deny は JSON として読める" {
  local data="$BATS_TEST_TMPDIR/da\"ta\\x"
  mkdir -p "$data"
  CLAUDE_PLUGIN_DATA="$data" run_entry guard-bash <<<"$BASH_INPUT"

  assert_deny_json
}

# =============================================================================
# バイナリがある → stdin をそのまま渡して実行する
# =============================================================================

@test "バイナリがあれば、stdin を渡して実行し、出力と終了コードをそのまま返す" {
  local data="$BATS_TEST_TMPDIR/data"
  mkdir -p "$data/bin"
  printf '#!/bin/sh\ncat\nexit 3\n' >"$data/bin/guard-bash"
  chmod +x "$data/bin/guard-bash"
  CLAUDE_PLUGIN_DATA="$data" run_entry guard-bash <<<"$BASH_INPUT"

  [ "$status" -eq 3 ]
  [ "$output" = "$BASH_INPUT" ]
}
