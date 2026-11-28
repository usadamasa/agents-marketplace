package main

import (
	"encoding/json"
	"testing"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/analyze-tokens/internal/transcript"
)

func TestBuildReport(t *testing.T) {
	p := mustPricing(t)
	main1 := &transcript.Session{SessionID: "aaaaaaaa-1", Project: "p", Title: "main-one", Turns: []*transcript.Turn{
		turn("m1", at(0), transcript.Usage{Cache1h: 1_000_000, Output: 100_000}),
	}, Compactions: []transcript.Compaction{{TurnIndex: 0, Trigger: "auto"}}}
	sub := &transcript.Session{SessionID: "agent-x", Project: "p", Subagent: true, Turns: []*transcript.Turn{
		{MsgID: "a1", TS: at(1), Model: "claude-sonnet-5", Usage: transcript.Usage{Cache5m: 1_000_000, CacheRead: 1_000_000}},
	}}
	main1.Subagents = []*transcript.Session{sub}
	short := &transcript.Session{SessionID: "bbbbbbbb-2", Project: "q", Turns: []*transcript.Turn{
		turn("m1", at(0), transcript.Usage{Input: 1000}),
	}}
	loaded := &transcript.Loaded{Sessions: []*transcript.Session{main1, short}, Deduplicated: 3}
	findings := []Finding{
		{Rule: "R6", WasteUSD: 2.0, SessionID: "aaaaaaaa-1"},
		{Rule: "R1", WasteUSD: 1.0, SessionID: "aaaaaaaa-1"},
		{Rule: "R1", WasteUSD: 0.5, SessionID: "agent-x"},
	}
	r := buildReport(loaded, findings, p, reportOptions{Top: 2, Days: 30})

	// main: 1h write 1M × 2 × $5 = $10、output 100k × $25/M = $2.5 → $12.5
	// sub (sonnet-5 $2/M): 5m write 1M × 1.25 × 2 = $2.5、read 1M × 0.1 × 2 = $0.2 → $2.7
	// short: 1000 × $5/M = $0.005
	if !nearly(r.Totals.Total, 15.205) {
		t.Errorf("totals.total = %v, want 15.205", r.Totals.Total)
	}
	if r.Sessions != 2 {
		t.Errorf("sessions = %d", r.Sessions)
	}
	if len(r.Findings) != 2 || r.Findings[0].Rule != "R6" {
		t.Errorf("findings は top で切る: %+v", r.Findings)
	}
	if len(r.ByRule) != 2 || r.ByRule[0].Rule != "R1" || r.ByRule[0].Count != 2 || !nearly(r.ByRule[0].USD, 1.5) {
		t.Errorf("by_rule = %+v", r.ByRule)
	}
	if len(r.ByModel) != 2 || r.ByModel[0].Model != "claude-opus-5" || r.ByModel[0].Sessions != 2 {
		t.Errorf("by_model = %+v", r.ByModel)
	}
	if r.Info.Subagents.Count != 1 || !nearly(r.Info.Subagents.USD, 2.7) || r.Info.Subagents.TTL5mRuns != 1 {
		t.Errorf("subagents = %+v", r.Info.Subagents)
	}
	if r.Info.ShortSessionsOnExpensiveModels != 2 {
		t.Errorf("short sessions on expensive = %d, want 2 (両方 opus-5 で 10 ターン以下)", r.Info.ShortSessionsOnExpensiveModels)
	}
	if r.Info.Compactions.Count != 1 || r.Info.Compactions.Auto != 1 {
		t.Errorf("compactions = %+v", r.Info.Compactions)
	}
	if r.Info.DeduplicatedMessages != 3 {
		t.Errorf("deduplicated_messages = %d", r.Info.DeduplicatedMessages)
	}
	// cache hit ratio = read / (read + write) = 1M / (1M + 1M + 1M)
	if !nearly(r.Totals.CacheHitRatio, 1.0/3.0) {
		t.Errorf("cache_hit_ratio = %v", r.Totals.CacheHitRatio)
	}
	if len(r.Info.TopCostSessions) != 2 || r.Info.TopCostSessions[0].Name != "main-one" {
		t.Errorf("top_cost_sessions = %+v", r.Info.TopCostSessions)
	}
	// period は main 会話の Turn で決める (subagent の時刻は含めない)
	if r.Period.From != "2026-10-01T10:00:00Z" || r.Period.To != "2026-10-01T10:00:00Z" || r.Period.Days != 30 {
		t.Errorf("period = %+v", r.Period)
	}

	// JSON の鍵は参考実装と揃える (skill の rules.md が鍵名で引く)
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"period", "sessions", "totals", "by_model", "findings", "by_rule", "info"} {
		if _, ok := m[k]; !ok {
			t.Errorf("JSON に %q が無い", k)
		}
	}

	// --rule は findings の一覧だけを絞り、by_rule は全ルールのまま
	r = buildReport(loaded, findings, p, reportOptions{Top: 10, Rule: "R1", Days: 30})
	if len(r.Findings) != 2 || r.Findings[0].Rule != "R1" || r.Findings[1].Rule != "R1" {
		t.Errorf("--rule R1 の findings = %+v", r.Findings)
	}
	if len(r.ByRule) != 2 {
		t.Errorf("--rule でも by_rule は全ルール: %+v", r.ByRule)
	}
}
