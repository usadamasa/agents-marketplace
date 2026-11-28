#!/usr/bin/env bats
# guard-bash の入出力のテスト。
#
# 判定ロジック本体のテストは cmd/guard-bash 側の Go テストにある。ここで見るのは、
# 実際にビルドしたバイナリが stdin の PreToolUse JSON を読んで判定 JSON を返すことと、
# settings → 判定 → 出力の配線。
bats_require_minimum_version 1.5.0

setup_file() {
  PLUGIN_ROOT="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"
  export PLUGIN_ROOT
  GUARD_BASH="$BATS_FILE_TMPDIR/guard-bash"
  export GUARD_BASH
  go build -C "$PLUGIN_ROOT" -o "$GUARD_BASH" ./cmd/guard-bash
}

setup() {
  # 手元の ~/.claude/settings.json の excludedCommands を判定に混ぜない
  export CLAUDE_SETTINGS_JSON="$BATS_TEST_TMPDIR/no-settings.json"
  unset CLAUDE_PROJECT_DIR
}

# hook の入出力契約: stdin の PreToolUse JSON を読んで、stdout に判定 JSON を出す。
@test "stdin の PreToolUse JSON を読んで deny を stdout へ返す" {
  run "$GUARD_BASH" <<< '{"tool_name":"Bash","tool_input":{"command":"echo x > notes.txt"}}'

  [ "$status" -eq 0 ]
  [[ "$output" == *'"permissionDecision":"deny"'* ]]
}

# excludedCommands の照合ガードは settings を読んで初めて働く。Go の結合テストは端末の設定を
# 切り離しているので、settings → 判定 → 出力の配線はここで確かめる。
write_excluded_settings() {
  printf '%s\n' '{"sandbox":{"excludedCommands":["gh *"]}}' > "$BATS_TEST_TMPDIR/settings.json"
  export CLAUDE_SETTINGS_JSON="$BATS_TEST_TMPDIR/settings.json"
}

@test "excludedCommands の対象をパイプで受ける形は、原因を名指しして deny する" {
  write_excluded_settings
  run "$GUARD_BASH" <<< '{"tool_name":"Bash","tool_input":{"command":"gh api rate_limit | tail -1"}}'

  [ "$status" -eq 0 ]
  [[ "$output" == *'"permissionDecision":"deny"'* ]]
  [[ "$output" == *'パイプ'* ]]
}

@test "破壊的操作の deny は、照合外れの deny より先に返す" {
  write_excluded_settings
  run "$GUARD_BASH" <<< '{"tool_name":"Bash","tool_input":{"command":"echo hi && gh repo delete owner/repo --yes"}}'

  [ "$status" -eq 0 ]
  [[ "$output" == *'gh の危険な操作'* ]]
}

@test "Bash 以外のツールでは何も出力しない" {
  run "$GUARD_BASH" <<< '{"tool_name":"Read","tool_input":{"file_path":"/etc/hosts"}}'

  [ "$status" -eq 0 ]
  [ -z "$output" ]
}
