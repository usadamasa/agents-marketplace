#!/usr/bin/env bash
# guard-bash の sandboxexcluded deny (excludedCommands の照合外れ) を transcript から拾い、
# 週ごとに原因・候補タグ・実行し直した回数を出す。guard の文言や判定を変える前後の比較に使う。
#
#   sandboxexcluded-denies.sh [--projects-dir <path>] [--from YYYY-MM-DD] [--to YYYY-MM-DD]
#
# 入力: <projects-dir>/*/*.jsonl (メインセッションの記録)。subagent の記録
# (<projects-dir>/<proj>/<session>/subagents/) は読まない。--from / --to は JST の日付で両端を含む。
#
# 出力 (stdout、タブ区切り): <week> <metric> <key> <value>
#   week は JST の月曜始まりの週で、その月曜の日付
#   denies total       shape の deny (「の対象ですが」の文言) の件数。git のグローバルオプションの deny は含めない
#   cause <key>        原因別の件数 (排他)。git-global-opt はここにだけ出る。今の文言の型に合わなければ unknown
#   command <name>     deny されたコマンド名別の件数 (shape のみ)
#   tag <candidate>    改善の候補 (A1〜A4、C) が効く形の件数 (非排他、shape のみ)。A3 のパイプ判定は正規表現による近似
#   retries incidents  連続する deny を 1 件にまとめた件数 (git-global-opt を含む)
#   retries unresolved そのセッションで deny でない Bash 呼び出しに届かなかった件数
#   retries mean       届いた件について、最初の deny の後に実行した Bash 呼び出しの数 (届いた 1 回を含む) の平均。
#                      結果の記録が無い呼び出し (中断など) も届いた 1 回に数える
#
# 終了コード: 0 = 集計できた、2 = 入力が読めない・引数が不正
set -euo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)
filter="${script_dir}/sandboxexcluded-denies.jq"

projects_dir="${HOME}/.claude/projects"
from=""
to=""

usage() {
  printf 'usage: %s [--projects-dir <path>] [--from YYYY-MM-DD] [--to YYYY-MM-DD]\n' "$(basename "$0")" >&2
  exit 2
}

date_arg() {
  [[ "${1:-}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || usage
  printf '%s' "$1"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --projects-dir)
      projects_dir="${2:-}"
      [ -n "$projects_dir" ] || usage
      shift 2
      ;;
    --from)
      from=$(date_arg "${2:-}")
      shift 2
      ;;
    --to)
      to=$(date_arg "${2:-}")
      shift 2
      ;;
    *)
      usage
      ;;
  esac
done

if [ ! -f "$filter" ]; then
  printf 'sandboxexcluded-denies: not found: %s\n' "$filter" >&2
  exit 2
fi
if [ ! -d "$projects_dir" ]; then
  printf 'sandboxexcluded-denies: not a directory: %s\n' "$projects_dir" >&2
  exit 2
fi

shopt -s nullglob
files=("$projects_dir"/*/*.jsonl)
shopt -u nullglob
[ ${#files[@]} -gt 0 ] || exit 0

if ! jq -nr --arg from "$from" --arg to "$to" -f "$filter" -R "${files[@]}"; then
  printf 'sandboxexcluded-denies: 集計に失敗した: %s\n' "$projects_dir" >&2
  exit 2
fi
