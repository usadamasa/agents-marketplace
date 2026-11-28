# sandboxexcluded-denies.sh の集計本体。transcript の各行を生の文字列で受け (jq -nR)、
# 週ごとの行を `<week>\t<metric>\t<key>\t<value>` で出す。
#
# 入力の引数: $from / $to (JST の YYYY-MM-DD、空なら制限なし)

# 壊れた行はファイル名と行番号を添えて止める。
def parse_line:
  . as $line
  | try fromjson catch error("invalid JSON: \(input_filename):\(input_line_number)");

# tool_result の content は文字列か text ブロックの配列。
def result_text:
  if type == "array" then map(.text? // "") | join("") elif type == "string" then . else "" end;

# 小数秒付きの ISO 8601 を epoch 秒へ。fromdateiso8601 は小数秒を受けない。
def epoch:
  (capture("^(?<base>[^.Z]+)(?<frac>\\.[0-9]+)?Z$") // error("unexpected timestamp: \(.)"))
  | (.base + "Z" | fromdateiso8601) + ((.frac // "0") | tonumber);

def jst_days: (. + 9 * 3600) / 86400 | floor;
def jst_date: jst_days * 86400 | strftime("%Y-%m-%d");
# 1970-01-01 は木曜なので、月曜を 0 とした曜日は (days + 3) % 7 になる。
def jst_week: jst_days | (. - ((. + 3) % 7)) * 86400 | strftime("%Y-%m-%d");

def fmt2:
  (. * 100 | round) as $n
  | "\($n / 100 | floor).\($n % 100 | tostring | if length < 2 then "0" + . else . end)";

# deny の文言 → {family, command, cause}。deny でなければ null。
# 原因のキーは guard-bash の shapeBreak が返す文言の書き出しで決める。
def classify_deny:
  if test("sandbox\\.excludedCommands の対象ですが") then
    {
      family: "shape",
      command: (capture("「(?<c>[^」]+)」 は sandbox").c // "unknown"),
      cause: (
        (capture("の対象ですが、(?<x>.+?)があるため照合が外れ").x // "") as $x
        | if $x | startswith("パイプ") then "pipe"
          elif $x | startswith("excludedCommands に無い") then "chain"
          elif $x | startswith("ループ・subshell") then "compound"
          elif $x | startswith("リダイレクト") then "redirect"
          elif $x | startswith("変数の前置き") then "assign"
          elif $x | startswith("引数の中のコマンド置換") then "subst"
          elif $x | startswith("`||`") then "or-list"
          elif $x | startswith("cwd の字面") then "cd"
          elif $x | startswith("`!` や `&`") then "negate-bg"
          else "unknown" end
      )
    }
  elif test("値を取る git のグローバルオプション") then
    {family: "git-global-opt", command: "git", cause: "git-global-opt"}
  else null end;

# 改善の候補 (A1〜A4、C) が効く形か。1 件に複数付く。
def tags($command):
  [
    (select(test("(echo|printf)\\b[^;&|\\n]*\\$(\\?|\\{?(PIPESTATUS|pipestatus))")) | "A1-exit-status-echo"),
    (select(test("^\\s*mkdir\\s+-p\\s+\\./tmp/?\\s*&&")) | "A2-mkdir-tmp"),
    (select(test("(^|[^|])\\|\\s*(head|tail|rg|grep|jq|wc|base64|sort|uniq|cut)\\b")) | "A3-pipe-to-filter"),
    (select($command == "task") | "A4-task"),
    (select(test("(^|[;&|]\\s*)(until|while|for)\\s")) | "C-wait-loop")
  ];

def in_range: (($from == "" or .date >= $from) and ($to == "" or .date <= $to));

# count_rows($week; $metric) — キーの配列を件数の行へ。
def count_rows($week; $metric):
  group_by(.) | map([$week, $metric, .[0], (length | tostring)]) | .[];

# 1 パス目: Bash の tool_use と、全 tool_result を拾う。resume / fork で同じ記録が
# 別ファイルに写ることがあるので、どちらも tool_use_id の初出だけを残す。
# api-request-blob などの写しは timestamp を持たないので、assistant / user の記録だけを見る。
reduce (inputs | parse_line | . as $rec | .message.content? | arrays | .[] | [$rec, .]) as [$rec, $block] (
  {uses: {}, results: {}};
  if $rec.type == "assistant" and $block.type == "tool_use" and $block.name == "Bash" then
    .uses[$block.id] //= {id: $block.id, session: $rec.sessionId, t: ($rec.timestamp | epoch), cmd: ($block.input.command // "")}
  elif $rec.type == "user" and $block.type == "tool_result" and ($block.tool_use_id | type) == "string" then
    .results[$block.tool_use_id] //= (
      if $block.is_error == true then ($block.content | result_text | classify_deny) else null end
    )
  else . end
)

# 2 パス目: Bash の呼び出しに deny の判定を付け、セッションごとに時刻順へ並べる。
| .results as $results
| [.uses[] | . + {deny: $results[.id], week: (.t | jst_week), date: (.t | jst_date)}]
| group_by(.session) | map(sort_by(.t)) as $sessions

# 実行し直し: 連続する deny を 1 件 (incident) にまとめ、deny でない Bash 呼び出しに届くまでの数を数える。
| [
    $sessions[]
    | reduce .[] as $c ({out: [], cur: null};
        if $c.deny != null then
          if .cur == null then .cur = {week: $c.week, date: $c.date, retries: 1} else .cur.retries += 1 end
        elif .cur != null then .out += [.cur] | .cur = null
        else . end)
    | .out + (if .cur != null then [.cur | .retries = null] else [] end)
    | .[]
    | select(in_range)
  ] as $incidents
| [$sessions[][] | select(.deny != null and in_range)] as $denies

| ([$denies[].week] + [$incidents[].week] | unique)[] as $week
| ($denies | map(select(.week == $week))) as $wd
| ($wd | map(select(.deny.family == "shape"))) as $shape
| ($incidents | map(select(.week == $week))) as $wi
| ($wi | map(select(.retries != null))) as $resolved
| (
    [$week, "denies", "total", ($shape | length | tostring)],
    ($wd | map(.deny.cause) | count_rows($week; "cause")),
    ($shape | map(.deny.command) | count_rows($week; "command")),
    ($shape | map(.deny.command as $command | .cmd | tags($command)[]) | count_rows($week; "tag")),
    [$week, "retries", "incidents", ($wi | length | tostring)],
    [$week, "retries", "unresolved", ($wi | map(select(.retries == null)) | length | tostring)],
    (if ($resolved | length) > 0 then
      [$week, "retries", "mean", ($resolved | map(.retries) | add / length | fmt2)]
    else empty end)
  )
| join("\t")
