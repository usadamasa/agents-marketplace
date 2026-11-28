#!/usr/bin/env bats
# direnv-env.sh のテスト
# 本物の direnv を使う。許可の記録 (XDG_DATA_HOME) と設定 (XDG_CONFIG_HOME) は
# テストごとの一時ディレクトリへ向け、端末の direnv の状態に触れない。
bats_require_minimum_version 1.5.0

SCRIPT_PATH="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/hooks/direnv-env.sh"

load lib/hook-test-helpers

setup_file() { setup_mock_bin; }
teardown_file() { teardown_mock_bin; }

setup() {
  CASE_DIR="$TEST_FILE_TMPDIR/test-$BATS_TEST_NUMBER"
  NEW_CWD="$CASE_DIR/project"
  mkdir -p "$NEW_CWD"
  # direnv のログ (log_status) は stderr に出る。これが CLAUDE_ENV_FILE に混ざると読み込みで落ちる
  printf '%s\n' 'log_status "noise on stderr"' 'export FROM_ENVRC=loaded' > "$NEW_CWD/.envrc"
  export XDG_DATA_HOME="$CASE_DIR/data" XDG_CONFIG_HOME="$CASE_DIR/config"
  export CLAUDE_ENV_FILE="$CASE_DIR/env.sh"
  # テストを走らせるシェルが direnv を読み込み済みでも、hook の入力環境を揃える
  unset DIRENV_DIFF DIRENV_DIR DIRENV_FILE DIRENV_WATCHES
  printf '%s\n' "export SEEDED_BY_PREVIOUS_EVENT=1" > "$CLAUDE_ENV_FILE"
}

# 第 1 引数が cd の行き先 (new_cwd)、第 2 引数が hook 起動時の cwd (省略時は new_cwd と同じ)。
run_with_input() {
  local new_cwd="${1:-$NEW_CWD}" cwd="${2:-${1:-$NEW_CWD}}"
  jq -n --arg new "$new_cwd" --arg cwd "$cwd" \
    '{hook_event_name: "CwdChanged", cwd: $cwd, old_cwd: "/", new_cwd: $new}' \
    | "${HOOK_BASH:-/bin/bash}" "$SCRIPT_PATH"
}

# SessionStart の入力。new_cwd / old_cwd は無く、cwd だけが入る
run_with_session_start_input() {
  local cwd="${1:-$NEW_CWD}" source="${2:-startup}"
  jq -n --arg cwd "$cwd" --arg source "$source" \
    '{hook_event_name: "SessionStart", cwd: $cwd, source: $source}' \
    | "${HOOK_BASH:-/bin/bash}" "$SCRIPT_PATH"
}

# 別の .envrc を持つディレクトリを作って許可する
make_allowed_dir() {
  local dir="$1" value="$2"
  mkdir -p "$dir"
  printf '%s\n' "export FROM_ENVRC=$value" > "$dir/.envrc"
  direnv allow "$dir"
}

source_env_var() {
  bash -euc 'source "$1"; printf "%s" "${!2}"' _ "$CLAUDE_ENV_FILE" "$1"
}

# direnv は PATH_add や $PWD を symlink を解いた実体のパスで書く (macOS の /tmp は /private/tmp)
real_path() { (cd "$1" && pwd -P); }

# Bash ツールのシェルは hook の PATH の末尾に plugin の bin/ を足してから env file を実行する。
# 第 1 引数をその時点の PATH として env file を実行し、結果の PATH を返す
source_path_from() {
  bash -euc 'PATH="$2"; source "$1"; printf "%s" "$PATH"' _ "$CLAUDE_ENV_FILE" "$1"
}

@test "許可済みの .envrc: 移動先の変数が入り、前回の CwdChanged の内容は置き換わる" {
  direnv allow "$NEW_CWD"
  run run_with_input
  [ "$status" -eq 0 ]
  run source_env_var FROM_ENVRC
  [ "$status" -eq 0 ]
  [ "$output" = "loaded" ]
  # Claude Code は CwdChanged のたびに前回分を捨てる前提なので、追記ではなく置き換える
  run grep -c SEEDED_BY_PREVIOUS_EVENT "$CLAUDE_ENV_FILE"
  [ "$output" = "0" ]
}

@test "未許可の .envrc: CLAUDE_ENV_FILE を変えず、systemMessage に direnv の未許可エラーを載せて exit 0" {
  run --separate-stderr run_with_input
  [ "$status" -eq 0 ]
  # 別の理由 (direnv 自体が起動できない等) で失敗したときに通らないよう、未許可で落ちたことを確かめる
  # shellcheck disable=SC2154 # stderr は run --separate-stderr が設定する
  [[ "$stderr" == *"is blocked"* ]]
  printf '%s' "$output" | jq -e --arg d "$NEW_CWD" \
    '.systemMessage | contains($d) and contains("is blocked") and (contains("exit status") | not)'
  run cat "$CLAUDE_ENV_FILE"
  [ "$output" = "export SEEDED_BY_PREVIOUS_EVENT=1" ]
}

@test "評価に失敗する .envrc: CLAUDE_ENV_FILE を変えず、systemMessage に .envrc の stderr と終了コードを載せて exit 0" {
  # 許可済みでも .envrc 自体が失敗するケース。未許可と同じ文言だと direnv allow を試して空振りする
  printf '%s\n' 'echo "boom from envrc" >&2' 'exit 1' > "$NEW_CWD/.envrc"
  direnv allow "$NEW_CWD"
  run --separate-stderr run_with_input
  [ "$status" -eq 0 ]
  printf '%s' "$output" | jq -e --arg d "$NEW_CWD" \
    '.systemMessage | contains($d) and contains("boom from envrc") and contains("exit status 1") and (contains("is blocked") | not)'
  # direnv は色付きで出すので、エスケープシーケンスを残さない
  printf '%s' "$output" | jq -e '.systemMessage | contains("\u001b") | not'
  # direnv は失敗時も stdout に export を出すので、それを書いていないことを確かめる
  run cat "$CLAUDE_ENV_FILE"
  [ "$output" = "export SEEDED_BY_PREVIOUS_EVENT=1" ]
}

@test "SessionStart: new_cwd が無くても cwd の .envrc を読み、起動時の内容は置き換わる" {
  # claude --worktree や .envrc 未読み込みのシェルからの起動、resume / compact 後の再開で、
  # 起動時の cwd の .envrc を Bash の環境へ載せる。CwdChanged と同じく置き換えで書く
  direnv allow "$NEW_CWD"
  run run_with_session_start_input
  [ "$status" -eq 0 ]
  run source_env_var FROM_ENVRC
  [ "$status" -eq 0 ]
  [ "$output" = "loaded" ]
  run grep -c SEEDED_BY_PREVIOUS_EVENT "$CLAUDE_ENV_FILE"
  [ "$output" = "0" ]
}

@test "SessionStart (compact): cwd が worktree ならその .envrc を読む" {
  # compact 後の SessionStart は cwd が worktree になっている。source によらず cwd で評価する
  make_allowed_dir "$CASE_DIR/worktree" worktree
  run run_with_session_start_input "$CASE_DIR/worktree" compact
  [ "$status" -eq 0 ]
  run source_env_var FROM_ENVRC
  [ "$output" = "worktree" ]
}

@test "Claude Code が cwd を戻した cd: new_cwd ではなく cwd の .envrc を読む" {
  # 作業ディレクトリの外へ cd すると、Claude Code は cwd をプロジェクトへ戻すが CwdChanged は
  # 再発火しない。hook の入力では戻し先が cwd、cd の行き先が new_cwd に入る (2026-10-10 に実測)
  direnv allow "$NEW_CWD"
  make_allowed_dir "$CASE_DIR/outside" outside
  run run_with_input "$CASE_DIR/outside" "$NEW_CWD"
  [ "$status" -eq 0 ]
  run source_env_var FROM_ENVRC
  [ "$output" = "loaded" ]
}

@test "PATH に空白を含む要素があっても source できる" {
  printf '%s\n' 'PATH_add bin' 'export FROM_ENVRC=loaded' > "$NEW_CWD/.envrc"
  mkdir -p "$NEW_CWD/bin"
  direnv allow "$NEW_CWD"
  PATH="/Applications/Rex Beta.app/Contents/MacOS:$PATH" run run_with_input
  [ "$status" -eq 0 ]
  run source_env_var FROM_ENVRC
  [ "$status" -eq 0 ]
  [ "$output" = "loaded" ]
}

@test "PATH_add した分だけを前に足し、Bash 側で hook の PATH より後に足された要素を残す" {
  # plugin の bin/ は hook の PATH には無く、Bash ツールのシェルでだけ末尾に足される (2026-10-17 に実測)
  printf '%s\n' 'PATH_add bin' > "$NEW_CWD/.envrc"
  mkdir -p "$NEW_CWD/bin"
  direnv allow "$NEW_CWD"
  local hook_path=$PATH
  run run_with_input
  [ "$status" -eq 0 ]
  run source_path_from "$hook_path:/plugin/bin"
  [ "$status" -eq 0 ]
  [ "$output" = "$(real_path "$NEW_CWD")/bin:$hook_path:/plugin/bin" ]
}

@test ".envrc が PATH を置き換えても、Bash 側で足された要素を末尾に残す" {
  # shellcheck disable=SC2016 # .envrc の中身なので展開させない
  printf '%s\n' 'export PATH="$PWD/bin:/usr/bin:/bin"' > "$NEW_CWD/.envrc"
  mkdir -p "$NEW_CWD/bin"
  direnv allow "$NEW_CWD"
  local hook_path=$PATH
  run run_with_input
  [ "$status" -eq 0 ]
  run source_path_from "$hook_path:/plugin/bin"
  [ "$status" -eq 0 ]
  [ "$output" = "$(real_path "$NEW_CWD")/bin:/usr/bin:/bin:/plugin/bin" ]
}

@test "claude を起動したシェルが別の .envrc を読み込み済みでも、Bash 側で足された要素を残す" {
  # DIRENV_DIFF があると、direnv は前の .envrc の差分を戻してから足すので、PATH が hook の PATH で終わらない
  local base_path=$PATH
  mkdir -p "$CASE_DIR/prev/bin"
  printf '%s\n' 'PATH_add bin' > "$CASE_DIR/prev/.envrc"
  direnv allow "$CASE_DIR/prev"
  eval "$(cd "$CASE_DIR/prev" && direnv export bash 2>/dev/null)"
  [ -n "${DIRENV_DIFF:-}" ]
  printf '%s\n' 'PATH_add bin' > "$NEW_CWD/.envrc"
  mkdir -p "$NEW_CWD/bin"
  direnv allow "$NEW_CWD"
  local hook_path=$PATH
  run run_with_input
  [ "$status" -eq 0 ]
  run source_path_from "$hook_path:/plugin/bin"
  [ "$status" -eq 0 ]
  [ "$output" = "$(real_path "$NEW_CWD")/bin:$base_path:/plugin/bin" ]
}

@test ".envrc が PATH を変えないときは、Bash 側の PATH をそのまま残す" {
  direnv allow "$NEW_CWD"
  local hook_path=$PATH
  run run_with_input
  [ "$status" -eq 0 ]
  run source_path_from "$hook_path:/plugin/bin"
  [ "$status" -eq 0 ]
  [ "$output" = "$hook_path:/plugin/bin" ]
}

@test "2 つの hook が同時に走っても env file は shell として壊れず、どちらか一方の内容になる" {
  make_allowed_dir "$CASE_DIR/a" a
  make_allowed_dir "$CASE_DIR/b" b
  run_with_input "$CASE_DIR/a" &
  run_with_input "$CASE_DIR/b" &
  wait
  run bash -n "$CLAUDE_ENV_FILE"
  [ "$status" -eq 0 ]
  run source_env_var FROM_ENVRC
  [ "$status" -eq 0 ]
  [[ "$output" == "a" || "$output" == "b" ]]
}

@test "direnv の出力が shell として壊れていたら、書き込まずに systemMessage で知らせる" {
  # 壊れた preamble を残すと以後の Bash が全部失敗するので、direnv の環境が無い方がまし
  # shellcheck disable=SC2016 # mock の中身なので展開させない
  printf '%s\n' '#!/bin/bash' 'if [ "$1" = export ]; then printf "%s\n" "export BROKEN=\$'"'"'abc"; fi' > "$MOCK_BIN/direnv"
  chmod +x "$MOCK_BIN/direnv"
  PATH="$MOCK_BIN:$PATH" run --separate-stderr run_with_input
  [ "$status" -eq 0 ]
  printf '%s' "$output" | jq -e '.systemMessage | contains("壊れ")'
  run cat "$CLAUDE_ENV_FILE"
  [ "$output" = "export SEEDED_BY_PREVIOUS_EVENT=1" ]
  rm -f "$MOCK_BIN/direnv"
}
