package pricing

import (
	"math"
	"slices"
	"testing"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/analyze-tokens/internal/transcript"
)

func nearly(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func mustLoad(t *testing.T) *Pricing {
	t.Helper()
	p, err := Load(DefaultJSON)
	if err != nil {
		t.Fatalf("同梱の pricing.json が読めない: %v", err)
	}
	return p
}

func TestLoadEmbedded(t *testing.T) {
	p := mustLoad(t)
	for _, m := range []string{"claude-fable-5-1", "claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-haiku-4-5"} {
		if p.resolve(m) != m {
			t.Errorf("%s が価格表に無い (resolve = %q)", m, p.resolve(m))
		}
	}
}

func TestResolvePrefix(t *testing.T) {
	p := mustLoad(t)
	tests := []struct {
		model string
		want  string
	}{
		{"claude-haiku-4-5-20251001", "claude-haiku-4-5"},
		{"claude-opus-4-5-20251101", "claude-opus-4-5"},
		{"claude-opus-5-5", "claude-opus-5-5"},
		{"claude-opus-5", "claude-opus-5"},
		{"claude-sonnet-4-6", "claude-sonnet-4-6"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := p.resolve(tt.model); got != tt.want {
				t.Errorf("resolve(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
	if len(p.UnknownModels()) != 0 {
		t.Errorf("既知のモデルが unknown に入っている: %v", p.UnknownModels())
	}
}

func TestResolveUnknownFallsBackToDefault(t *testing.T) {
	p := mustLoad(t)
	if got := p.resolve("claude-nova-9"); got != "claude-opus-5" {
		t.Errorf("未知のモデルは default_model (claude-opus-5) に落ちるべき: got %q", got)
	}
	if got := p.UnknownModels(); !slices.Equal(got, []string{"claude-nova-9"}) {
		t.Errorf("UnknownModels = %v", got)
	}
}

func TestCostUsesPerModelCacheReadMultiplier(t *testing.T) {
	p := mustLoad(t)
	// Fable 5.1: input $10/M, cache_read 0.025x → 1M read tokens = $0.25
	c := p.Cost("claude-fable-5-1", transcript.Usage{CacheRead: 1_000_000})
	if !nearly(c.CacheRead, 0.25) {
		t.Errorf("Fable 5.1 の cache_read = %v, want 0.25", c.CacheRead)
	}
	// Sonnet 5.5: input $2/M, cache_read 0.05x → $0.10
	c = p.Cost("claude-sonnet-5-5", transcript.Usage{CacheRead: 1_000_000})
	if !nearly(c.CacheRead, 0.10) {
		t.Errorf("Sonnet 5.5 の cache_read = %v, want 0.10", c.CacheRead)
	}
	// Opus 5: 既定の 0.1x → $0.50
	c = p.Cost("claude-opus-5", transcript.Usage{CacheRead: 1_000_000})
	if !nearly(c.CacheRead, 0.50) {
		t.Errorf("Opus 5 の cache_read = %v, want 0.50", c.CacheRead)
	}
}

func TestCostCacheWriteMultipliers(t *testing.T) {
	p := mustLoad(t)
	// Opus 5 input $5/M: 5m write 1.25x = $6.25, 1h write 2x = $10
	c := p.Cost("claude-opus-5", transcript.Usage{Cache5m: 1_000_000, Cache1h: 1_000_000, Input: 1_000_000, Output: 1_000_000})
	if !nearly(c.CacheWrite, 16.25) {
		t.Errorf("cache_write = %v, want 16.25", c.CacheWrite)
	}
	if !nearly(c.Input, 5) || !nearly(c.Output, 25) {
		t.Errorf("input/output = %v/%v, want 5/25", c.Input, c.Output)
	}
	if !nearly(c.Total(), 46.25) {
		t.Errorf("total = %v, want 46.25", c.Total())
	}
}

func TestCostHaiku55LongContextTier(t *testing.T) {
	p := mustLoad(t)
	// 100K 以下: input $0.10/M
	short := p.Cost("claude-haiku-5-5", transcript.Usage{Input: 50_000, Output: 1_000})
	if !nearly(short.Input, 0.005) {
		t.Errorf("短い prompt の input = %v, want 0.005", short.Input)
	}
	// 100K 超 (cache_read も context に数える): input $0.50/M、output $2.50/M
	long := p.Cost("claude-haiku-5-5", transcript.Usage{Input: 1_000, CacheRead: 150_000, Output: 1_000})
	if !nearly(long.Input, 0.0005) {
		t.Errorf("長い prompt の input = %v, want 0.0005", long.Input)
	}
	if !nearly(long.Output, 0.0025) {
		t.Errorf("長い prompt の output = %v, want 0.0025", long.Output)
	}
}

func TestContextWindow(t *testing.T) {
	p := mustLoad(t)
	tests := map[string]int64{
		"claude-fable-5-1":          1_000_000,
		"claude-opus-4-7":           1_000_000,
		"claude-opus-4-6":           200_000,
		"claude-sonnet-4-6":         200_000,
		"claude-haiku-4-5-20251001": 200_000,
		"claude-nova-9":             1_000_000, // default_model (opus-5) の窓
	}
	for model, want := range tests {
		if got := p.ContextWindow(model); got != want {
			t.Errorf("ContextWindow(%s) = %d, want %d", model, got, want)
		}
	}
}

func TestIsExpensive(t *testing.T) {
	p := mustLoad(t)
	if !p.IsExpensive("claude-opus-5") || !p.IsExpensive("claude-fable-5-1") {
		t.Error("Opus / Fable は expensive")
	}
	if p.IsExpensive("claude-sonnet-5-5") || p.IsExpensive("claude-opus-5-5") {
		t.Error("input $5/M 未満のモデルは expensive ではない")
	}
}

func TestLoadRejectsBrokenTable(t *testing.T) {
	const mult = `"multipliers":{"cache_write_5m":1.25,"cache_write_1h":2,"cache_read":0.1}`
	tests := []struct {
		name string
		json string
	}{
		{"壊れた JSON", `{"models": `},
		{"default_model が無い", `{"default_context_window":200000,` + mult + `,"models":{"a":{"input":1,"output":2}}}`},
		{"default_model が models に無い", `{"default_model":"x","default_context_window":200000,` + mult + `,"models":{"a":{"input":1,"output":2}}}`},
		{"default_context_window が無い", `{"default_model":"a",` + mult + `,"models":{"a":{"input":1,"output":2}}}`},
		{"multipliers が欠ける", `{"default_model":"a","default_context_window":200000,"multipliers":{"cache_write_5m":1.25},"models":{"a":{"input":1,"output":2}}}`},
		{"単価が 0", `{"default_model":"a","default_context_window":200000,` + mult + `,"models":{"a":{"input":0,"output":2}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load([]byte(tt.json)); err == nil {
				t.Error("エラーになるべき")
			}
		})
	}
}
