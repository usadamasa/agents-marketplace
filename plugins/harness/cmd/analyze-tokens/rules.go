package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/analyze-tokens/internal/pricing"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/analyze-tokens/internal/transcript"
)

// thresholds は検出の閾値。--threshold NAME=VALUE で上書きできる。
type thresholds map[string]float64

func defaultThresholds() thresholds {
	return thresholds{
		"r1_min_cache_write":  20000,
		"r2_min_cache_write":  50000,
		"r3_min_tokens":       8000,
		"r4_min_reads":        3,
		"r5_min_tokens":       4000,
		"r5_min_writes":       2,
		"r6_min_turns":        150,
		"r6_context_fraction": 0.75,
		"chars_per_token":     4,
	}
}

// parseThresholds は NAME=VALUE の列を既定値に重ねる。
func parseThresholds(items []string) (thresholds, error) {
	th := defaultThresholds()
	for _, item := range items {
		k, v, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("--threshold は NAME=VALUE の形: %q", item)
		}
		if _, known := th[k]; !known {
			return nil, fmt.Errorf("未知の閾値 %q (使えるもの: %s)", k, strings.Join(thresholdNames(), ", "))
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			return nil, fmt.Errorf("--threshold %s の値は正の数: %q", k, v)
		}
		th[k] = f
	}
	return th, nil
}

func thresholdNames() []string {
	th := defaultThresholds()
	names := make([]string, 0, len(th))
	for k := range th {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Finding は検出した浪費 1 件。Evidence の値はルールごとに決まっている (skill の references/rules.md)。
type Finding struct {
	Rule        string         `json:"rule"`
	SessionID   string         `json:"session_id"`
	SessionName string         `json:"session_name"`
	Project     string         `json:"project"`
	TS          string         `json:"ts"`
	WasteUSD    float64        `json:"waste_usd"`
	Evidence    map[string]any `json:"evidence"`
}

func (th thresholds) estTokens(chars int) int {
	return int(float64(chars) / th["chars_per_token"])
}

func (th thresholds) i(key string) int64 { return int64(th[key]) }

func newFinding(s *transcript.Session, rule string, ts time.Time, waste float64, ev map[string]any) Finding {
	if s.Subagent {
		ev["subagent"] = true
	}
	tsStr := ""
	if !ts.IsZero() {
		tsStr = ts.UTC().Format(time.RFC3339)
	}
	return Finding{
		Rule: rule, SessionID: s.SessionID, SessionName: s.DisplayName(), Project: s.Project,
		TS: tsStr, WasteUSD: math.Round(waste*1e4) / 1e4, Evidence: ev,
	}
}

// writeMultiplier はこのセッションの cache write 倍率 (1h を使ったことがあれば 1h)。
func writeMultiplier(s *transcript.Session, r pricing.Rates) float64 {
	if s.UsedOneHourTTL() {
		return r.W1
	}
	return r.W5
}

// detect は 1 セッションに全ルールを当てる。
func detect(s *transcript.Session, p *pricing.Pricing, th thresholds) []Finding {
	var out []Finding
	out = append(out, detectCacheRewrites(s, p, th)...)
	out = append(out, detectLargeResults(s, p, th)...)
	out = append(out, detectRepeatedReads(s, p, th)...)
	out = append(out, detectLargeWrites(s, p, th)...)
	out = append(out, detectLongSession(s, p, th)...)
	return out
}

// detectCacheRewrites は R1 (TTL 超えの gap の後の書き直し) と R2 (TTL 以内の大きな書き直し)。
func detectCacheRewrites(s *transcript.Session, p *pricing.Pricing, th thresholds) []Finding {
	var out []Finding
	for i := 1; i < len(s.Turns); i++ {
		prev, t := s.Turns[i-1], s.Turns[i]
		u := t.Usage
		if u.CacheWrite() == 0 {
			continue
		}
		r := p.RatesFor(t.Model, u.Context())
		gap := t.TS.Sub(prev.TS).Minutes()
		ttlMin := 5
		if u.Cache1h > 0 {
			ttlMin = 60
		}
		waste := (float64(u.Cache5m)*(r.W5-r.Read) + float64(u.Cache1h)*(r.W1-r.Read)) * r.Input
		switch {
		case gap > float64(ttlMin) && u.CacheWrite() >= th.i("r1_min_cache_write"):
			out = append(out, newFinding(s, "R1", t.TS, waste, map[string]any{
				"gap_min": int(math.Round(gap)), "ttl_min": ttlMin,
				"cache_write_tokens": u.CacheWrite(), "model": t.Model,
			}))
		case u.CacheWrite() >= th.i("r2_min_cache_write"):
			out = append(out, newFinding(s, "R2", t.TS, waste, map[string]any{
				"cache_write_tokens": u.CacheWrite(), "gap_min": int(math.Round(gap)),
				"context_tokens": u.Context(), "causes": rewriteCauses(s, i, prev, t), "model": t.Model,
			}))
		}
	}
	return out
}

// rewriteCauses は transcript から見える prefix の変化。見えない原因 (MCP の増減、hook による
// system prompt の変化) は空のまま。effort の変更は今のモデル (Opus 5.5 / Sonnet 5.5 / Haiku 5.5 /
// Fable 5.1) では cache を切らないので原因に挙げない。
func rewriteCauses(s *transcript.Session, i int, prev, t *transcript.Turn) []string {
	causes := []string{}
	for _, c := range s.Compactions {
		if c.TurnIndex == i {
			causes = append(causes, "compaction:"+c.Trigger)
		}
	}
	if prev.Model != t.Model {
		causes = append(causes, "model_change:"+prev.Model+"->"+t.Model)
	}
	if prev.Version != "" && t.Version != "" && prev.Version != t.Version {
		causes = append(causes, "version_change:"+prev.Version+"->"+t.Version)
	}
	if s.ModeEvents[i] {
		causes = append(causes, "mode_change")
	}
	return causes
}

// detectLargeResults は R3 (大きな tool_result が以後のターンで毎回読まれる)。
func detectLargeResults(s *transcript.Session, p *pricing.Pricing, th thresholds) []Finding {
	uses := map[string]transcript.ToolUse{}
	for _, t := range s.Turns {
		for _, u := range t.ToolUses {
			uses[u.ID] = u
		}
	}
	n := len(s.Turns)
	var out []Finding
	for _, res := range s.ToolResults {
		tokens := th.estTokens(res.Chars)
		if int64(tokens) < th.i("r3_min_tokens") || res.TurnIndex < 0 || res.TurnIndex >= n {
			continue
		}
		turn := s.Turns[res.TurnIndex]
		r := p.RatesFor(turn.Model, turn.Usage.Context())
		remaining := n - 1 - res.TurnIndex
		waste := float64(tokens) * (float64(remaining)*r.Read + writeMultiplier(s, r)) * r.Input
		ev := map[string]any{"tool": "?", "est_tokens": tokens, "remaining_turns": remaining}
		if use, ok := uses[res.ToolUseID]; ok {
			ev["tool"] = use.Name
			if use.FilePath != "" {
				ev["file_path"] = use.FilePath
			}
			if use.CommandHead != "" {
				ev["command"] = use.CommandHead
			}
		}
		ts := res.TS
		if ts.IsZero() {
			ts = turn.TS
		}
		out = append(out, newFinding(s, "R3", ts, waste, ev))
	}
	return out
}

type readRef struct {
	turn int
	id   string
}

// detectRepeatedReads は R4 (同じファイルを同じセッションで何度も Read)。
func detectRepeatedReads(s *transcript.Session, p *pricing.Pricing, th thresholds) []Finding {
	reads := map[string][]readRef{}
	var paths []string
	for i, t := range s.Turns {
		for _, u := range t.ToolUses {
			if u.Name == "Read" && u.FilePath != "" {
				if _, seen := reads[u.FilePath]; !seen {
					paths = append(paths, u.FilePath)
				}
				reads[u.FilePath] = append(reads[u.FilePath], readRef{i, u.ID})
			}
		}
	}
	sizes := map[string]int{}
	for _, r := range s.ToolResults {
		sizes[r.ToolUseID] = r.Chars
	}
	var out []Finding
	for _, path := range paths {
		lst := reads[path]
		if int64(len(lst)) < th.i("r4_min_reads") {
			continue
		}
		extra := 0
		for _, ref := range lst[1:] {
			extra += th.estTokens(sizes[ref.id])
		}
		last := s.Turns[lst[len(lst)-1].turn]
		r := p.RatesFor(last.Model, last.Usage.Context())
		out = append(out, newFinding(s, "R4", last.TS, float64(extra)*writeMultiplier(s, r)*r.Input, map[string]any{
			"file_path": path, "reads": len(lst), "extra_est_tokens": extra,
		}))
	}
	return out
}

type writeRef struct {
	turn   int
	tokens int
}

// detectLargeWrites は R5 (大きな Write と同じパスへの繰り返し Write)。両方に当たる Write は 2 回数える。
func detectLargeWrites(s *transcript.Session, p *pricing.Pricing, th thresholds) []Finding {
	var out []Finding
	perPath := map[string][]writeRef{}
	var paths []string
	for i, t := range s.Turns {
		r := p.RatesFor(t.Model, t.Usage.Context())
		for _, u := range t.ToolUses {
			if u.Name != "Write" {
				continue
			}
			tokens := th.estTokens(u.ContentChars)
			if u.FilePath != "" {
				if _, seen := perPath[u.FilePath]; !seen {
					paths = append(paths, u.FilePath)
				}
				perPath[u.FilePath] = append(perPath[u.FilePath], writeRef{i, tokens})
			}
			if int64(tokens) >= th.i("r5_min_tokens") {
				ev := map[string]any{"kind": "large_write", "est_tokens": tokens}
				if u.FilePath != "" {
					ev["file_path"] = u.FilePath
				}
				out = append(out, newFinding(s, "R5", t.TS, float64(tokens)*r.Output, ev))
			}
		}
	}
	for _, path := range paths {
		lst := perPath[path]
		if int64(len(lst)) < th.i("r5_min_writes") {
			continue
		}
		extra := 0
		for _, w := range lst[1:] {
			extra += w.tokens
		}
		last := s.Turns[lst[len(lst)-1].turn]
		r := p.RatesFor(last.Model, last.Usage.Context())
		out = append(out, newFinding(s, "R5", last.TS, float64(extra)*r.Output, map[string]any{
			"kind": "repeated_write", "file_path": path, "writes": len(lst), "extra_est_tokens": extra,
		}))
	}
	return out
}

// detectLongSession は R6 (ターン数か context がモデルの窓の r6_context_fraction を超えたセッション)。
// 額は閾値を超えた context を毎ターン cache read し続けた分で、参考実装の 150k 固定ではなく
// モデルの窓に比例させる (1M 窓のモデルで 150k は窓の 15% に過ぎず、cache read も安い)。
func detectLongSession(s *transcript.Session, p *pricing.Pricing, th thresholds) []Finding {
	n := len(s.Turns)
	if n == 0 {
		return nil
	}
	last := s.Turns[n-1]
	limit := func(t *transcript.Turn) int64 {
		return int64(float64(p.ContextWindow(t.Model)) * th["r6_context_fraction"])
	}
	final := last.Usage.Context()
	if int64(n) < th.i("r6_min_turns") && final < limit(last) {
		return nil
	}
	waste := 0.0
	for _, t := range s.Turns {
		if over := t.Usage.Context() - limit(t); over > 0 {
			r := p.RatesFor(t.Model, t.Usage.Context())
			waste += float64(over) * r.Read * r.Input
		}
	}
	return []Finding{newFinding(s, "R6", last.TS, waste, map[string]any{
		"turns": n, "final_context_tokens": final, "context_window": p.ContextWindow(last.Model),
		"threshold_tokens": limit(last), "model": last.Model,
	})}
}
