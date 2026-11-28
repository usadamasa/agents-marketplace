# 検出ルールと助言

`analyze-tokens` の `findings[].rule` をここで引く。`evidence` の値をそのまま引用し、数値を作らない。
`waste_usd` はどれも「その行為を避けた場合との差額」の推定で、上限ではなく目安。

共通の単価の見方: cache write は input の 1.25 倍 (5 分 TTL) か 2 倍 (1 時間 TTL)。cache read は
input の 0.1 倍 (Fable 5.1 は 0.025 倍、Opus 5.5 / Sonnet 5.5 は 0.05 倍)、output は input の約 5 倍。
input 単価 (USD / 1M tokens) は、Fable 5.x が 10、Opus 5 と Opus 4.x が 5、Opus 5.5 が 4。
Sonnet は 5.x が 2、4.x が 3。Haiku は 5.5 が 0.10 (prompt 100K 超は 0.50)、4.5 が 1。原本は
`analyze-tokens` に同梱の価格表 (`pricing.json`)。

## R1 — 離席後の cache の書き直し

**起きたこと:** 直前の応答からの間隔 `gap_min` が TTL `ttl_min` (5 か 60) を超え、`cache_write_tokens` の
context 全体を cache read ではなく cache write で送り直した。`gap_min` は assistant の timestamp 同士の差
なので、直前のターンの生成時間も含む。TTL をわずかに超えるものは長いターンの可能性がある。

**助言:**
- まず予防側を確かめる。Claude Code 本体の idle compaction は、1h TTL の端末セッションで 200K tokens 以上の
  会話を約 54 分放置すると、cache が切れる前に圧縮する。R1 が多いなら `/config` の `Idle compaction` が
  off になっていないか、main の TTL が 5 分になっていないか (API key・cloud provider・usage credits の消費中は
  既定が 5 分)、context が `CLAUDE_CODE_IDLE_COMPACT_MIN_TOKENS` (既定 200000) に届いていたかを見る
- 今の cache の状態は `/usage` の `Prompt cache (main):` 行と、statusline に渡る `prompt_cache` object
  (`warm`、`ttl`、`expires_at`、`recache_tokens_if_cold` など) で確かめる。cold のときの書き直し量は
  `recache_tokens_if_cold` が持つ
- `evidence.subagent: true` なら subagent の 5 分 TTL。subagent を続けて使う作業では
  `subagentPromptCacheTtl: "1h"` を検討する (write 2 倍なので read 2 回以上の使い方だけ)
- TTL より長く離れるなら、その前に作業を区切るか手短な引き継ぎを書き、新しいセッションで続ける。
  戻って続けるなら、再開 1 回で `waste_usd` ぶんかかることを承知で送る
- 失効後の `/compact` も全履歴を送り直す。安く再開する手段ではない。以後のターンが多いときだけ元が取れる

## R2 — セッション途中の大きな cache の書き直し

**起きたこと:** TTL 以内なのに `cache_write_tokens` の書き直しが起きた。prefix のどこかが変わった。
`causes` に transcript から見えた原因が入る。

| `causes` | 意味 |
| ---- | ---- |
| `compaction:auto` / `compaction:manual` | 圧縮直後の最初のリクエスト。圧縮後は要約だけを送るので書き直しは 1 回で済む。auto は窓の既定 (1M モデルで約 967k) に達したということ |
| `model_change:A->B` | モデルを切り替えた。cache はモデルごとなので context 全体を B で書き直す |
| `version_change:A->B` | Claude Code のバージョンが変わった (system prompt が変わる) |
| `mode_change` | permission mode を切り替えた |
| 空 | transcript に出ない原因。MCP サーバの増減、plugin の有効化、hook が差し込む system prompt の変化 |

**助言:**
- モデル・permission mode・MCP の切り替えはセッションの頭で済ませる。途中で切り替えるなら
  `context_tokens` ぶんの書き直しを承知で行う (context が大きい途中でモデルを変えると、
  安いモデルに変えても元のモデルで続けるより高くつくことがある)
- `compaction:auto` が並ぶなら、作業の区切りに `/compact` か `/clear` を手で実行する。窓の既定に達するまで待つ
  auto の圧縮より、context が小さいうちに圧縮するので、書き直す量が小さく済む

## R3 — 巨大な tool 出力の取り込み

**起きたこと:** 1 つの tool_result (`tool`、`est_tokens`) が context に入り、以後の `remaining_turns` ターンで
毎回 cache read された。セッションが長いほど膨らむ。`file_path` か `command` (Bash の先頭語) が付く。

**助言 (tool 別):**
- `Read`: `offset` / `limit` で範囲を絞る。先に `rg -n` で当たりを付ける
- `Bash` (`command` は先頭語): `head` / `tail` / `rg` を通す。`cat` でログやビルド出力を流さない。
  長い出力は `./tmp` へ落として必要な行だけ読む
- `Grep` / `Glob`: path や pattern を狭める。`files_with_matches` で済むなら本文を取らない
- どの tool でも、探索的な読み取りは subagent (Explore など) に任せて、
  本体の context には要約だけ戻す
- 大きい結果を 1 度しか使わないなら、使った直後の区切りで `/compact` か `/clear`

## R4 — 同じファイルの繰り返し Read

**起きたこと:** `file_path` を同じセッションで `reads` 回読み、2 回目以降で `extra_est_tokens` ぶんの
重複が context に入った。

**助言:**
- 1 度読んだら context にある写しを使う。Edit の後に読み直すなら変えた範囲だけ `offset` / `limit` で
- 何度も戻るリファレンスなら、要点を CLAUDE.md かメモリに置く

## R5 — 大きい Write、同じパスへの繰り返し Write

**起きたこと:** output は input の約 5 倍の単価。`kind=large_write` は 1 回の Write で `est_tokens`。
`kind=repeated_write` は `file_path` を `writes` 回書き直し、2 回目以降で `extra_est_tokens` の output。
両方に当たる Write は 2 回数えている。

**助言:**
- 部分の変更は Edit で行う。数行のために全体を Write しない
- 定型や繰り返しの多いファイル (fixture、JSON、CSV) はスクリプトやテンプレートで生成する
- 下書きを何度も書き直す作業は、先に構成を決めてから 1 回で書く

## R6 — 長いセッション (carry のコスト)

**起きたこと:** `turns` ターンに達したか、最後の context `final_context_tokens` がモデルの窓
`context_window` の 75% (`threshold_tokens`) を超えた。`waste_usd` は閾値を超えた分を毎ターン
cache read し続けた費用。

**助言:**
- これは浪費というより context を carry するコスト。1M 窓のモデルでは cache read が安く、
  閾値 75% は CLI の既定 (`r6_context_fraction`) によるこの skill の目安で、本体の autocompact より手前に
  ある。R6 だけを理由に細かく compact / 分割しない
- `waste_usd` が大きいセッションに R3 (巨大な tool 出力) が重なっていれば、そちらを直す
- 作業の種類が変わる区切りで `/clear` する。探索の重い段階は subagent に出して本体に溜めない

## info (finding ではない)

- **`totals` の区分:** `cache_write` と `output` が大きいなら R1 / R2 / R5 の習慣、`cache_read` が
  大きいなら長いセッション (R6) か大きい結果 (R3) を carry している。`cache_hit_ratio` は高いほど良いが、
  cache read が安いモデルでは費用への効きは小さい
- **`short_sessions_on_expensive_models`:** 10 ターン以下で終わる input $5/M 以上のモデル (Opus 5 以前、
  Fable) のセッションが多ければ、短い用事は `/model` で安いモデルか低い effort に回す
- **`subagents`:** シェアが高いこと自体は浪費ではない。`ttl_5m_runs` が `count` と同じなら全部 5 分 TTL
  (R1 の助言を参照)。親セッションに R3 が出ていないかも見る
- **`compactions`:** `auto` が多いなら区切りでの手動 `/compact` / `/clear` を提案する
- **`deduplicated_messages`:** resume / fork で複数ファイルに写された同じメッセージを 1 回に数えた件数。
  大きければ `--continue` / fork の多い使い方
- **`unknown_models`:** 価格表に無いモデルを default の単価で計算した。価格表への追加を伝える
