#!/usr/bin/env bats
# guard-optin-env.sh のテスト
bats_require_minimum_version 1.5.0

SCRIPT_PATH="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/hooks/guard-optin-env.sh"

# hook の shebang は #!/bin/bash (macOS では 3.2)。PATH の bash で動かすと bash 4 以降の機能の混入を
# 検知できないので、既定で /bin/bash に固定する。別のバージョンで確かめるときは HOOK_BASH を渡す。
run_hook() {
  run "${HOOK_BASH:-/bin/bash}" "$@"
}

# make_input COMMAND は Bash の PreToolUse の JSON 入力を出す。
make_input() {
  jq -cn --arg c "$1" '{tool_name:"Bash",tool_input:{command:$c}}'
}

OPT_IN_KEY="CLAUDE_ALLOW_GH_PR_WRITES"
ISSUE_OPT_IN_KEY="CLAUDE_ALLOW_GH_ISSUE_WRITES"

make_write_input() {
  jq -cn --arg p "$1" --arg c "$2" '{tool_name:"Write",tool_input:{file_path:$p,content:$c}}'
}

make_edit_input() {
  jq -cn --arg p "$1" --arg o "$2" --arg n "$3" \
    '{tool_name:"Edit",tool_input:{file_path:$p,old_string:$o,new_string:$n}}'
}

assert_denied() {
  [ "$status" -eq 0 ]
  [[ "$output" == *'"permissionDecision":"deny"'* ]]
  [[ "$output" == *"$OPT_IN_KEY"* ]]
}

assert_passed() {
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

# =============================================================================
# Write / Edit: プロジェクトスコープの settings への書き込み → deny
# =============================================================================

@test "Write: .claude/settings.json への opt-in キー書き込みは deny" {
  run_hook "$SCRIPT_PATH" <<< "$(make_write_input \
    "/repo/.claude/settings.json" "{\"env\":{\"$OPT_IN_KEY\":\"1\"}}")"

  assert_denied
}

@test "Write: issue 用の opt-in キー書き込みも deny" {
  run_hook "$SCRIPT_PATH" <<< "$(make_write_input \
    "/repo/.claude/settings.json" "{\"env\":{\"$ISSUE_OPT_IN_KEY\":\"1\"}}")"

  [ "$status" -eq 0 ]
  [[ "$output" == *'"permissionDecision":"deny"'* ]]
  [[ "$output" == *"$ISSUE_OPT_IN_KEY"* ]]
}

@test "Write: .claude/settings.local.json への opt-in キー書き込みは deny" {
  run_hook "$SCRIPT_PATH" <<< "$(make_write_input \
    "/repo/.claude/settings.local.json" "{\"env\":{\"$OPT_IN_KEY\":\"1\"}}")"

  assert_denied
}

@test "Write: 相対パスの .claude/settings.json も deny" {
  run_hook "$SCRIPT_PATH" <<< "$(make_write_input \
    ".claude/settings.json" "{\"env\":{\"$OPT_IN_KEY\":\"true\"}}")"

  assert_denied
}

@test "Edit: new_string に opt-in キーを足すのは deny" {
  run_hook "$SCRIPT_PATH" <<< "$(make_edit_input \
    "/repo/.claude/settings.json" '{"env":{}}' "{\"env\":{\"$OPT_IN_KEY\":\"1\"}}")"

  assert_denied
}

# =============================================================================
# Write / Edit: 対象外 → 通過
# =============================================================================

@test "Write: settings 以外のファイルなら opt-in キーを書ける" {
  run_hook "$SCRIPT_PATH" <<< "$(make_write_input \
    "/repo/AGENTS.md" "有効化するには $OPT_IN_KEY を設定する")"

  assert_passed
}

@test "Write: settings 以外の YAML は対象外" {
  run_hook "$SCRIPT_PATH" <<< "$(make_write_input \
    "/repo/docs/example.yaml" "env:\n  $OPT_IN_KEY: \"1\"")"

  assert_passed
}

@test "Write: .claude/settings.json でも opt-in キーが無ければ通過" {
  run_hook "$SCRIPT_PATH" <<< "$(make_write_input \
    "/repo/.claude/settings.json" '{"permissions":{"allow":["Bash(bats *)"]}}')"

  assert_passed
}

@test "Edit: new_string から opt-in キーを消す変更は通過" {
  run_hook "$SCRIPT_PATH" <<< "$(make_edit_input \
    "/repo/.claude/settings.json" "{\"env\":{\"$OPT_IN_KEY\":\"1\"}}" '{"env":{}}')"

  assert_passed
}

@test "Write: 名前が似ているだけの settings は対象外" {
  run_hook "$SCRIPT_PATH" <<< "$(make_write_input \
    "/repo/.claude/settings.backup.json" "{\"env\":{\"$OPT_IN_KEY\":\"1\"}}")"

  assert_passed
}

# =============================================================================
# Bash: settings への書き込み経路 → deny
# =============================================================================

@test "Bash: リダイレクトで settings.json に書くのは deny" {
  run_hook "$SCRIPT_PATH" <<< "$(make_input \
    "echo '{\"env\":{\"$OPT_IN_KEY\":\"1\"}}' > .claude/settings.json")"

  assert_denied
}

@test "Bash: jq で settings.local.json を書き換えるのは deny" {
  run_hook "$SCRIPT_PATH" <<< "$(make_input \
    "jq '.env.$OPT_IN_KEY = \"1\"' .claude/settings.local.json > t && mv t .claude/settings.local.json")"

  assert_denied
}

@test "Bash: sed -i で settings.json を書き換えるのは deny" {
  run_hook "$SCRIPT_PATH" <<< "$(make_input \
    "sed -i '' 's/foo/$OPT_IN_KEY/' .claude/settings.json")"

  assert_denied
}

# =============================================================================
# Bash: 対象外 → 通過
# =============================================================================

@test "Bash: settings を触らないコマンドは opt-in キーを含んでも通過" {
  run_hook "$SCRIPT_PATH" <<< "$(make_input "env | grep $OPT_IN_KEY")"

  assert_passed
}

@test "Bash: settings を読むだけなら通過" {
  run_hook "$SCRIPT_PATH" <<< "$(make_input "grep $OPT_IN_KEY .claude/settings.json")"

  assert_passed
}

@test "Bash: settings を触るが opt-in キーを含まなければ通過" {
  run_hook "$SCRIPT_PATH" <<< "$(make_input "jq '.permissions' .claude/settings.json")"

  assert_passed
}

# コミットメッセージや PR 本文で settings とキー名の両方に言及することはある。
# 書き込みでない以上ここで止めない (guard-bash が selectGroups で
# 避けているのと同じ誤検知)。
@test "Bash: コミットメッセージで settings とキー名に言及しても通過" {
  run_hook "$SCRIPT_PATH" <<< "$(make_input \
    "git commit -m '$OPT_IN_KEY を .claude/settings.json で有効化できるようにする'")"

  assert_passed
}

@test "Bash: heredoc のコミット本文で言及しても通過" {
  run_hook "$SCRIPT_PATH" <<< "$(make_input \
    "git commit -F - <<'EOF'
$OPT_IN_KEY は .claude/settings.json に書く
EOF")"

  assert_passed
}

@test "Bash: ドキュメントへの追記なら通過" {
  run_hook "$SCRIPT_PATH" <<< "$(make_input \
    "printf '%s\\n' '$OPT_IN_KEY を .claude/settings.json に設定する' >> AGENTS.md")"

  assert_passed
}

@test "Bash 以外・Write 以外のツールは通過" {
  run_hook "$SCRIPT_PATH" <<< "$(jq -cn --arg p "/repo/.claude/settings.json" \
    '{tool_name:"Read",tool_input:{file_path:$p}}')"

  assert_passed
}

@test "空の tool_input でも落ちない" {
  run_hook "$SCRIPT_PATH" <<< '{"tool_name":"Write","tool_input":{}}'

  assert_passed
}
