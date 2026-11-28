package main

import (
	"math"
	"testing"
	"time"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/analyze-tokens/internal/pricing"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/analyze-tokens/internal/transcript"
)

func nearly(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func mustPricing(t *testing.T) *pricing.Pricing {
	t.Helper()
	p, err := pricing.Load(pricing.DefaultJSON)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func at(min int) time.Time {
	return time.Date(2026, 10, 1, 10, min, 0, 0, time.UTC)
}

func turn(id string, ts time.Time, u transcript.Usage, uses ...transcript.ToolUse) *transcript.Turn {
	return &transcript.Turn{MsgID: id, TS: ts, Model: "claude-opus-5", Version: "2.1.296", Usage: u, ToolUses: uses}
}

func findRule(fs []Finding, rule string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

func TestDetectR1IdleGapRewrite(t *testing.T) {
	p := mustPricing(t)
	s := &transcript.Session{SessionID: "s", Turns: []*transcript.Turn{
		turn("m1", at(0), transcript.Usage{Cache1h: 50_000, Output: 10}),
		// 1h TTL を超える 90 分の gap の後に 40k を書き直す
		turn("m2", at(90), transcript.Usage{Cache1h: 40_000, Output: 10}),
	}}
	fs := detect(s, p, defaultThresholds())
	r1 := findRule(fs, "R1")
	if len(r1) != 1 {
		t.Fatalf("R1 = %d 件, want 1 (%+v)", len(r1), fs)
	}
	ev := r1[0].Evidence
	if ev["gap_min"] != 90 || ev["ttl_min"] != 60 || ev["cache_write_tokens"] != int64(40_000) {
		t.Errorf("evidence = %v", ev)
	}
	if _, has := ev["subagent"]; has {
		t.Error("main 会話の finding に subagent フラグは付かない")
	}
	// 浪費 = 40k × (2.0 − 0.1) × $5/M = $0.38
	if !nearly(r1[0].WasteUSD, 0.38) {
		t.Errorf("waste = %v, want 0.38", r1[0].WasteUSD)
	}
}

func TestDetectR1UsesFiveMinuteTTLWhenWriteIsFiveMinute(t *testing.T) {
	p := mustPricing(t)
	s := &transcript.Session{SessionID: "s", Subagent: true, Turns: []*transcript.Turn{
		turn("m1", at(0), transcript.Usage{Cache5m: 30_000}),
		turn("m2", at(7), transcript.Usage{Cache5m: 30_000}),
	}}
	r1 := findRule(detect(s, p, defaultThresholds()), "R1")
	if len(r1) != 1 || r1[0].Evidence["ttl_min"] != 5 || r1[0].Evidence["subagent"] != true {
		t.Errorf("R1 (5m, subagent) = %+v", r1)
	}
}

func TestDetectR2MidSessionRewriteWithCauses(t *testing.T) {
	p := mustPricing(t)
	t1 := turn("m1", at(0), transcript.Usage{Cache1h: 10_000})
	t2 := turn("m2", at(1), transcript.Usage{Cache1h: 60_000})
	t2.Model = "claude-sonnet-5"
	t2.Version = "2.1.300"
	s := &transcript.Session{SessionID: "s", Turns: []*transcript.Turn{t1, t2},
		ModeEvents:  map[int]bool{1: true},
		Compactions: []transcript.Compaction{{TurnIndex: 1, Trigger: "auto", PreTokens: 168_000}},
	}
	r2 := findRule(detect(s, p, defaultThresholds()), "R2")
	if len(r2) != 1 {
		t.Fatalf("R2 = %d 件", len(r2))
	}
	causes, _ := r2[0].Evidence["causes"].([]string)
	want := []string{"compaction:auto", "model_change:claude-opus-5->claude-sonnet-5", "version_change:2.1.296->2.1.300", "mode_change"}
	if len(causes) != len(want) {
		t.Fatalf("causes = %v, want %v", causes, want)
	}
	for i := range want {
		if causes[i] != want[i] {
			t.Errorf("causes[%d] = %q, want %q", i, causes[i], want[i])
		}
	}
	// gap が TTL 以内で 50k 以上の write は R2。R1 にはならない
	if len(findRule(detect(s, p, defaultThresholds()), "R1")) != 0 {
		t.Error("TTL 以内の書き直しは R1 ではない")
	}
}

func TestDetectR3LargeToolResult(t *testing.T) {
	p := mustPricing(t)
	s := &transcript.Session{SessionID: "s", Turns: []*transcript.Turn{
		turn("m1", at(0), transcript.Usage{Cache1h: 100}, transcript.ToolUse{ID: "tu1", Name: "Bash", CommandHead: "cat"}),
		turn("m2", at(1), transcript.Usage{CacheRead: 100}),
		turn("m3", at(2), transcript.Usage{CacheRead: 100}),
	}, ToolResults: []transcript.ToolResult{
		{ToolUseID: "tu1", TS: at(0), Chars: 40_000, TurnIndex: 0}, // 10k tokens
		{ToolUseID: "tu9", TS: at(0), Chars: 400, TurnIndex: 0},    // 小さい
	}}
	r3 := findRule(detect(s, p, defaultThresholds()), "R3")
	if len(r3) != 1 {
		t.Fatalf("R3 = %d 件", len(r3))
	}
	ev := r3[0].Evidence
	if ev["tool"] != "Bash" || ev["command"] != "cat" || ev["est_tokens"] != 10_000 || ev["remaining_turns"] != 2 {
		t.Errorf("evidence = %v", ev)
	}
	// 10k × (2 ターン × 0.1 + 2.0 (1h write)) × $5/M = $0.11
	if !nearly(r3[0].WasteUSD, 0.11) {
		t.Errorf("waste = %v, want 0.11", r3[0].WasteUSD)
	}
}

func TestDetectR4RepeatedReads(t *testing.T) {
	p := mustPricing(t)
	read := func(id string) transcript.ToolUse {
		return transcript.ToolUse{ID: id, Name: "Read", FilePath: "/same.go"}
	}
	s := &transcript.Session{SessionID: "s", Turns: []*transcript.Turn{
		turn("m1", at(0), transcript.Usage{Cache5m: 1}, read("a")),
		turn("m2", at(1), transcript.Usage{Cache5m: 1}, read("b")),
		turn("m3", at(2), transcript.Usage{Cache5m: 1}, read("c"), transcript.ToolUse{ID: "d", Name: "Read", FilePath: "/other.go"}),
	}, ToolResults: []transcript.ToolResult{
		{ToolUseID: "a", Chars: 4000, TurnIndex: 0}, {ToolUseID: "b", Chars: 4000, TurnIndex: 1}, {ToolUseID: "c", Chars: 4000, TurnIndex: 2},
	}}
	r4 := findRule(detect(s, p, defaultThresholds()), "R4")
	if len(r4) != 1 {
		t.Fatalf("R4 = %d 件 (3 回以上読んだファイルだけ)", len(r4))
	}
	if r4[0].Evidence["file_path"] != "/same.go" || r4[0].Evidence["reads"] != 3 || r4[0].Evidence["extra_est_tokens"] != 2000 {
		t.Errorf("evidence = %v", r4[0].Evidence)
	}
}

func TestDetectR5LargeAndRepeatedWrites(t *testing.T) {
	p := mustPricing(t)
	s := &transcript.Session{SessionID: "s", Turns: []*transcript.Turn{
		turn("m1", at(0), transcript.Usage{}, transcript.ToolUse{ID: "w1", Name: "Write", FilePath: "/big", ContentChars: 20_000}),
		turn("m2", at(1), transcript.Usage{}, transcript.ToolUse{ID: "w2", Name: "Write", FilePath: "/small", ContentChars: 400}),
		turn("m3", at(2), transcript.Usage{}, transcript.ToolUse{ID: "w3", Name: "Write", FilePath: "/small", ContentChars: 400}),
	}}
	r5 := findRule(detect(s, p, defaultThresholds()), "R5")
	if len(r5) != 2 {
		t.Fatalf("R5 = %d 件 (large_write 1 + repeated_write 1)", len(r5))
	}
	kinds := map[string]Finding{}
	for _, f := range r5 {
		kinds[f.Evidence["kind"].(string)] = f
	}
	if lw, ok := kinds["large_write"]; !ok || lw.Evidence["est_tokens"] != 5000 || !nearly(lw.WasteUSD, 0.125) {
		t.Errorf("large_write = %+v", lw)
	}
	if rw, ok := kinds["repeated_write"]; !ok || rw.Evidence["writes"] != 2 || rw.Evidence["extra_est_tokens"] != 100 {
		t.Errorf("repeated_write = %+v", rw)
	}
}

func TestDetectR6LongSession(t *testing.T) {
	p := mustPricing(t)
	// opus-5 は 1M 窓。既定の 75% = 750k が閾値
	s := &transcript.Session{SessionID: "s"}
	for i := range 3 {
		s.Turns = append(s.Turns, turn("m", at(i), transcript.Usage{CacheRead: 800_000}))
	}
	th := defaultThresholds()
	th["r6_min_turns"] = 3
	r6 := findRule(detect(s, p, th), "R6")
	if len(r6) != 1 {
		t.Fatalf("R6 = %d 件", len(r6))
	}
	ev := r6[0].Evidence
	if ev["turns"] != 3 || ev["final_context_tokens"] != int64(800_000) || ev["context_window"] != int64(1_000_000) || ev["threshold_tokens"] != int64(750_000) {
		t.Errorf("evidence = %v", ev)
	}
	// 超過 50k × 3 ターン × 0.1 × $5/M = $0.075
	if !nearly(r6[0].WasteUSD, 0.075) {
		t.Errorf("waste = %v, want 0.075", r6[0].WasteUSD)
	}
	// ターン数も context も閾値未満なら出ない
	th["r6_min_turns"] = 150
	th["r6_context_fraction"] = 0.9
	if len(findRule(detect(s, p, th), "R6")) != 0 {
		t.Error("閾値未満の R6")
	}
	// 200K 窓のモデルなら同じ 160k でも閾値 (150k) を超える
	old := &transcript.Session{SessionID: "o"}
	for i := range 3 {
		tt := turn("m", at(i), transcript.Usage{CacheRead: 160_000})
		tt.Model = "claude-sonnet-4-6"
		old.Turns = append(old.Turns, tt)
	}
	th = defaultThresholds()
	th["r6_min_turns"] = 3
	if got := findRule(detect(old, p, th), "R6"); len(got) != 1 || got[0].Evidence["threshold_tokens"] != int64(150_000) {
		t.Errorf("200K 窓の R6 = %+v", got)
	}
}

func TestParseThresholds(t *testing.T) {
	th, err := parseThresholds([]string{"r3_min_tokens=4000", "chars_per_token=3.5"})
	if err != nil {
		t.Fatal(err)
	}
	if th["r3_min_tokens"] != 4000 || th["chars_per_token"] != 3.5 || th["r6_min_turns"] != 150 {
		t.Errorf("thresholds = %v", th)
	}
	for _, bad := range []string{"nope=1", "r3_min_tokens", "r3_min_tokens=abc", "r3_min_tokens=0"} {
		if _, err := parseThresholds([]string{bad}); err == nil {
			t.Errorf("%q はエラーになるべき", bad)
		}
	}
}
