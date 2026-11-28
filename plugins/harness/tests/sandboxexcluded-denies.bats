#!/usr/bin/env bats
# scripts/sandboxexcluded-denies.sh のテスト
#
# guard-bash の sandboxexcluded deny を transcript から拾い、週ごとに原因・候補タグ・実行し直した回数を
# 数えられるかを確かめる。guard を変える前後の比較に使うので、数え方の境界 (期間・週・
# 実行し直しの区切り・数えない記録) を固定する。
# shellcheck disable=SC2016  # 引数はモデルが実行したコマンドと deny の文言で、展開させない
bats_require_minimum_version 1.5.0

PLUGIN_ROOT="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"
DENIES="$PLUGIN_ROOT/scripts/sandboxexcluded-denies.sh"

setup() {
  TEST_TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/sandboxexcluded-denies-test.XXXXXX")
  export TEST_TMPDIR
  PROJECTS="$TEST_TMPDIR/projects"
  mkdir -p "$PROJECTS/proj"
}

teardown() {
  [ -n "$TEST_TMPDIR" ] && rm -rf "$TEST_TMPDIR"
}

# shape_deny <cmd> <原因> — guard-bash の excludedShapeReason と同じ形の文言
shape_deny() {
  printf 'PreToolUse:Bash hook error: 「%s」 は sandbox.excludedCommands の対象ですが、%sがあるため照合が外れ、コマンド全体が sandbox 内で動きます。%s を素の 1 文で実行し直してください。' "$1" "$2" "$1"
}

GITOPT_DENY='PreToolUse:Bash hook error: 値を取る git のグローバルオプション (`-C`) を前置した書き込み系の呼び出しは、素の 1 文でも sandbox.excludedCommands の照合から外れ、コマンド全体が sandbox 内で動きます。'

# bash_call <file> <session> <UTC timestamp> <command> <ok|error|deny 文言>
# Bash の tool_use と tool_result の 2 行を transcript に足す。
bash_call() {
  local file="$1" session="$2" ts="$3" cmd="$4" result="$5"
  local id
  id="toolu_$(printf '%s' "$file$session$ts$cmd" | shasum | cut -c1-16)"
  local is_error=true text="$result"
  case "$result" in
    ok) is_error=false text="done" ;;
    error) text="Exit code 1" ;;
  esac
  jq -nc --arg s "$session" --arg t "$ts" --arg id "$id" --arg cmd "$cmd" \
    '{type: "assistant", sessionId: $s, timestamp: $t,
      message: {content: [{type: "tool_use", id: $id, name: "Bash", input: {command: $cmd}}]}}' >>"$file"
  jq -nc --arg s "$session" --arg t "$ts" --arg id "$id" --arg text "$text" --argjson err "$is_error" \
    '{type: "user", sessionId: $s, timestamp: $t,
      message: {content: [{type: "tool_result", tool_use_id: $id, is_error: $err, content: $text}]}}' >>"$file"
}

run_denies() {
  run --separate-stderr "$DENIES" --projects-dir "$PROJECTS" "$@"
}

# row <week> <metric> <key> <value> — 出力の 1 行
row() {
  printf '%s\t%s\t%s\t%s' "$@"
}

# has_row <行> — 出力にその行がそのまま含まれるか
has_row() {
  local line
  for line in "${lines[@]}"; do
    [ "$line" = "$1" ] && return 0
  done
  printf 'missing row: %s\n--- output ---\n%s\n' "$1" "$output" >&2
  return 1
}

# =============================================================================
# 拾う記録と拾わない記録
# =============================================================================

@test "shape の deny を原因とコマンド名で数える" {
  local f="$PROJECTS/proj/s1.jsonl"
  bash_call "$f" s1 2026-10-16T01:00:00Z 'ghq list | rg x' "$(shape_deny ghq パイプ)"
  bash_call "$f" s1 2026-10-16T01:01:00Z 'ghq list x' ok
  bash_call "$f" s1 2026-10-16T02:00:00Z 'GIT_EDITOR=true git rebase --continue' "$(shape_deny git '変数の前置き (静的な値でも外れる)')"
  bash_call "$f" s1 2026-10-16T02:01:00Z 'git rebase --continue' ok
  run_denies
  [ "$status" -eq 0 ]
  has_row "$(row 2026-10-12 denies total 2)"
  has_row "$(row 2026-10-12 cause pipe 1)"
  has_row "$(row 2026-10-12 cause assign 1)"
  has_row "$(row 2026-10-12 command ghq 1)"
  has_row "$(row 2026-10-12 command git 1)"
}

@test "原因の文言を短いキーへ写す" {
  local f="$PROJECTS/proj/s1.jsonl"
  bash_call "$f" s1 2026-10-16T01:00:00Z 'pwd; gh pr list' "$(shape_deny gh 'excludedCommands に無い「pwd」との連結')"
  bash_call "$f" s1 2026-10-16T01:01:00Z 'until gh pr checks 1; do sleep 5; done' "$(shape_deny gh 'ループ・subshell・ブロックなどの複合構文')"
  bash_call "$f" s1 2026-10-16T01:02:00Z 'gh issue view 1 > ./tmp/a.md' "$(shape_deny gh 'リダイレクトや heredoc (`2>&1` 以外)')"
  bash_call "$f" s1 2026-10-16T01:03:00Z 'gh run view --job $(x)' "$(shape_deny gh '引数の中のコマンド置換 (`$(...)` や backtick)')"
  bash_call "$f" s1 2026-10-16T01:04:00Z 'gh pr view 1 || true' "$(shape_deny gh '`||` でのつなぎ')"
  bash_call "$f" s1 2026-10-16T01:05:00Z 'cd .. && gh pr view 1' "$(shape_deny gh 'cwd の字面と違う cd (同じ場所でも末尾スラッシュや相対表記では外れる)')"
  bash_call "$f" s1 2026-10-16T01:06:00Z 'gh pr view 1 &' "$(shape_deny gh '`!` や `&` の付いた文')"
  bash_call "$f" s1 2026-10-16T01:07:00Z 'gh pr view 1' ok
  run_denies
  [ "$status" -eq 0 ]
  has_row "$(row 2026-10-12 denies total 7)"
  local cause
  for cause in chain compound redirect subst or-list cd negate-bg; do
    has_row "$(row 2026-10-12 cause "$cause" 1)"
  done
}

@test "今の型に合わない文言は unknown として total に含める" {
  # 文言を変えた後も、旧い文言の deny を落とさずに数える。
  local f="$PROJECTS/proj/s1.jsonl"
  bash_call "$f" s1 2026-10-16T01:00:00Z 'git log | head' \
    'PreToolUse:Bash hook error: 「git」 は sandbox.excludedCommands の対象ですが、heredoc をパイプの上流に置く形では照合が成立せず、sandbox 内で実行されます。'
  bash_call "$f" s1 2026-10-16T01:01:00Z 'git log' ok
  run_denies
  [ "$status" -eq 0 ]
  has_row "$(row 2026-10-12 denies total 1)"
  has_row "$(row 2026-10-12 cause unknown 1)"
}

@test "git のグローバルオプションの deny は別の原因として数え、total に入れない" {
  # issue の 99 件はこの deny を含まない。比較の基準をずらさない。
  local f="$PROJECTS/proj/s1.jsonl"
  bash_call "$f" s1 2026-10-16T01:00:00Z 'git -C ../x switch -c b' "$GITOPT_DENY"
  bash_call "$f" s1 2026-10-16T01:01:00Z 'ghq list | rg x' "$(shape_deny ghq パイプ)"
  bash_call "$f" s1 2026-10-16T01:02:00Z 'ghq list x' ok
  run_denies
  [ "$status" -eq 0 ]
  has_row "$(row 2026-10-12 denies total 1)"
  has_row "$(row 2026-10-12 cause git-global-opt 1)"
  has_row "$(row 2026-10-12 cause pipe 1)"
}

@test "deny 以外のエラーと、assistant 本文に引用された文言は数えない" {
  local f="$PROJECTS/proj/s1.jsonl"
  bash_call "$f" s1 2026-10-16T01:00:00Z 'false' error
  # deny の文言を含むが成功した結果 (rg で transcript を検索した出力など)。
  jq -nc --arg text "$(shape_deny gh パイプ)" '{type: "user", sessionId: "s1", timestamp: "2026-10-16T01:01:00Z",
    message: {content: [{type: "tool_result", tool_use_id: "toolu_x", is_error: false, content: $text}]}}' >>"$f"
  jq -nc --arg text "$(shape_deny gh パイプ)" '{type: "assistant", sessionId: "s1", timestamp: "2026-10-16T01:02:00Z",
    message: {content: [{type: "text", text: $text}]}}' >>"$f"
  run_denies
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "assistant / user 以外の記録に入った tool ブロックは読まない" {
  # api-request-blob は送った履歴の写しで、timestamp を持たない。
  local f="$PROJECTS/proj/s1.jsonl"
  bash_call "$f" s1 2026-10-16T01:00:00Z 'ghq list | rg x' "$(shape_deny ghq パイプ)"
  jq -nc '{type: "api-request-blob", sessionId: "s1", timestamp: null,
    message: {content: [{type: "tool_use", id: "toolu_blob", name: "Bash", input: {command: "ghq list | rg y"}}]}}' >>"$f"
  run_denies
  [ "$status" -eq 0 ]
  has_row "$(row 2026-10-12 denies total 1)"
}

@test "subagent の記録 (projects/<proj>/<session>/subagents/) は数えない" {
  mkdir -p "$PROJECTS/proj/s1/subagents"
  local f="$PROJECTS/proj/s1/subagents/agent-a.jsonl"
  bash_call "$f" s1 2026-10-16T01:00:00Z 'ghq list | rg x' "$(shape_deny ghq パイプ)"
  run_denies
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "同じ tool_use_id の結果が 2 つのファイルにあれば 1 件として数える" {
  # resume / fork で履歴が新しいファイルへ写ることがある。
  bash_call "$PROJECTS/proj/s1.jsonl" s1 2026-10-16T01:00:00Z 'ghq list | rg x' "$(shape_deny ghq パイプ)"
  cp "$PROJECTS/proj/s1.jsonl" "$PROJECTS/proj/s2.jsonl"
  run_denies
  [ "$status" -eq 0 ]
  has_row "$(row 2026-10-12 denies total 1)"
}

# =============================================================================
# 期間と週
# =============================================================================

@test "週は JST の月曜始まりで、ラベルは月曜の日付にする" {
  local f="$PROJECTS/proj/s1.jsonl"
  # 2026-10-18 (日) 23:59 JST は 10-12 の週、2026-10-19 (月) 00:00 JST は 10-19 の週
  bash_call "$f" s1 2026-10-18T14:59:00Z 'ghq list | rg a' "$(shape_deny ghq パイプ)"
  bash_call "$f" s1 2026-10-18T15:00:00Z 'ghq list | rg b' "$(shape_deny ghq パイプ)"
  run_denies
  [ "$status" -eq 0 ]
  has_row "$(row 2026-10-12 denies total 1)"
  has_row "$(row 2026-10-19 denies total 1)"
}

@test "--from と --to は JST の日付で両端を含む" {
  local f="$PROJECTS/proj/s1.jsonl"
  bash_call "$f" s1 2026-10-15T14:59:59.999Z 'ghq list | rg a' "$(shape_deny ghq パイプ)" # 10-15 JST
  bash_call "$f" s1 2026-10-15T15:00:00.000Z 'ghq list | rg b' "$(shape_deny ghq パイプ)" # 10-16 JST
  bash_call "$f" s1 2026-10-18T14:59:59.000Z 'ghq list | rg c' "$(shape_deny ghq パイプ)" # 10-18 JST
  bash_call "$f" s1 2026-10-18T15:00:00.000Z 'ghq list | rg d' "$(shape_deny ghq パイプ)" # 10-19 JST
  run_denies --from 2026-10-16 --to 2026-10-18
  [ "$status" -eq 0 ]
  has_row "$(row 2026-10-12 denies total 2)"
  [[ "$output" != *2026-10-19* ]]
}

# =============================================================================
# 候補タグ
# =============================================================================

@test "候補ごとのタグを、1 件に複数付けて数える" {
  local f="$PROJECTS/proj/s1.jsonl"
  bash_call "$f" s1 2026-10-16T01:00:00Z 'task test 2>&1 | rg -v "^ok " | head -40; echo "exit=${PIPESTATUS[0]}"' "$(shape_deny task パイプ)"
  bash_call "$f" s1 2026-10-16T01:01:00Z 'bin/tool status; printf '"'"'exit=%s\n'"'"' "$?"' "$(shape_deny tool'excludedCommands に無い「printf」との連結')"
  bash_call "$f" s1 2026-10-16T01:02:00Z 'mkdir -p ./tmp && task ci > ./tmp/ci.log 2>&1' "$(shape_deny task 'excludedCommands に無い「mkdir」との連結')"
  bash_call "$f" s1 2026-10-16T01:03:00Z 'until gh pr checks 94 --json bucket | rg -q true; do sleep 20; done' "$(shape_deny gh 'ループ・subshell・ブロックなどの複合構文')"
  bash_call "$f" s1 2026-10-16T01:04:00Z 'gh api x --jq .content | base64 -d' "$(shape_deny gh パイプ)"
  bash_call "$f" s1 2026-10-16T01:05:00Z 'if [ -n "$X" ]; then gh pr list; fi' "$(shape_deny gh 'ループ・subshell・ブロックなどの複合構文')"
  # ./tmp の下を作る形は、./tmp を先に作っておく A2 では消えない
  bash_call "$f" s1 2026-10-16T01:06:00Z 'mkdir -p ./tmp/x && gh pr list' "$(shape_deny gh 'excludedCommands に無い「mkdir」との連結')"
  bash_call "$f" s1 2026-10-16T01:07:00Z 'gh pr list' ok
  run_denies
  [ "$status" -eq 0 ]
  has_row "$(row 2026-10-12 denies total 7)"
  has_row "$(row 2026-10-12 tag A1-exit-status-echo 2)"
  has_row "$(row 2026-10-12 tag A2-mkdir-tmp 1)"
  # until の中の `| rg -q` もフィルタへのパイプとして拾う
  has_row "$(row 2026-10-12 tag A3-pipe-to-filter 3)"
  has_row "$(row 2026-10-12 tag A4-task 2)"
  # if はループではない
  has_row "$(row 2026-10-12 tag C-wait-loop 1)"
}

# =============================================================================
# 実行し直し
# =============================================================================

@test "連続する deny を 1 件にまとめ、通るまでの呼び出し数を数える" {
  local f="$PROJECTS/proj/s1.jsonl"
  # 1 件目: deny → ok で 1 回
  bash_call "$f" s1 2026-10-16T01:00:00Z 'ghq list | rg a' "$(shape_deny ghq パイプ)"
  bash_call "$f" s1 2026-10-16T01:01:00Z 'ghq list a' ok
  # 2 件目: deny → deny → ok で 2 回 (git-global-opt の deny も実行し直しに数える)
  bash_call "$f" s1 2026-10-16T02:00:00Z 'git -C x switch b' "$GITOPT_DENY"
  bash_call "$f" s1 2026-10-16T02:01:00Z 'ghq list | rg b' "$(shape_deny ghq パイプ)"
  bash_call "$f" s1 2026-10-16T02:02:00Z 'ghq list b' ok
  # 3 件目: deny → 別のエラー で 1 回 (コマンドは走った)
  bash_call "$f" s1 2026-10-16T03:00:00Z 'ghq list | rg c' "$(shape_deny ghq パイプ)"
  bash_call "$f" s1 2026-10-16T03:01:00Z 'ghq list c' error
  # 4 件目: セッションの最後の deny は unresolved
  bash_call "$f" s1 2026-10-16T04:00:00Z 'ghq list | rg d' "$(shape_deny ghq パイプ)"
  # 別セッションの呼び出しは s1 の実行し直しに数えない
  bash_call "$PROJECTS/proj/s2.jsonl" s2 2026-10-16T04:01:00Z 'ghq list d' ok
  run_denies
  [ "$status" -eq 0 ]
  has_row "$(row 2026-10-12 retries incidents 4)"
  has_row "$(row 2026-10-12 retries unresolved 1)"
  has_row "$(row 2026-10-12 retries mean 1.33)"
}

@test "実行し直しは Bash 以外のツールを挟んでも Bash の呼び出しだけで数える" {
  local f="$PROJECTS/proj/s1.jsonl"
  bash_call "$f" s1 2026-10-16T01:00:00Z 'ghq list | rg a' "$(shape_deny ghq パイプ)"
  jq -nc '{type: "assistant", sessionId: "s1", timestamp: "2026-10-16T01:00:30Z",
    message: {content: [{type: "tool_use", id: "toolu_read", name: "Read", input: {file_path: "/x"}}]}}' >>"$f"
  bash_call "$f" s1 2026-10-16T01:01:00Z 'ghq list a' ok
  run_denies
  [ "$status" -eq 0 ]
  has_row "$(row 2026-10-12 retries mean 1.00)"
}

# =============================================================================
# 入力の不備
# =============================================================================

@test "projects-dir が無ければ 2 で終了する" {
  run --separate-stderr "$DENIES" --projects-dir "$TEST_TMPDIR/nope"
  [ "$status" -eq 2 ]
  [[ "$stderr" == *"nope"* ]]
}

@test "日付の形が違えば usage で 2 を返す" {
  run_denies --from 2026/10/16
  [ "$status" -eq 2 ]
}

@test "壊れた JSONL があれば 2 で終了し、ファイル名を出す" {
  printf '{not json\n' >"$PROJECTS/proj/broken.jsonl"
  run_denies
  [ "$status" -eq 2 ]
  [[ "$stderr" == *"broken.jsonl"* ]]
}
