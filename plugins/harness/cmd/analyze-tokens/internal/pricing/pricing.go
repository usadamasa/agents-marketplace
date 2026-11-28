// Package pricing はモデル別の単価表と、usage を USD にする計算。
package pricing

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/analyze-tokens/internal/transcript"
)

// DefaultJSON は同梱の価格表。
//
//go:embed pricing.json
var DefaultJSON []byte

// expensiveInputPerM は「高いモデル」の境目 (input USD / 1M tokens)。短いセッションの数え上げに使う。
const expensiveInputPerM = 5.0

// Cost は USD の内訳。
type Cost struct {
	Input      float64 `json:"input"`
	CacheWrite float64 `json:"cache_write"`
	CacheRead  float64 `json:"cache_read"`
	Output     float64 `json:"output"`
}

// Total は 4 区分の合計。
func (c Cost) Total() float64 {
	return c.Input + c.CacheWrite + c.CacheRead + c.Output
}

// Rates は 1 ターンぶんの単価。Input / Output は 1 トークンあたりの USD、
// W5 / W1 / Read は Input に掛ける倍率。
type Rates struct {
	Input, Output float64
	W5, W1, Read  float64
}

type longContextPrice struct {
	ThresholdTokens int64   `json:"threshold_tokens"`
	Input           float64 `json:"input"`
	Output          float64 `json:"output"`
}

type modelPrice struct {
	Input         float64           `json:"input"`
	Output        float64           `json:"output"`
	CacheWrite5m  *float64          `json:"cache_write_5m"`
	CacheWrite1h  *float64          `json:"cache_write_1h"`
	CacheRead     *float64          `json:"cache_read"`
	ContextWindow *int64            `json:"context_window"`
	LongContext   *longContextPrice `json:"long_context"`
}

// base は prompt の大きさに応じた input / output の単価 (USD / 1M tokens)。
func (m modelPrice) base(context int64) (in, out float64) {
	if lc := m.LongContext; lc != nil && context > lc.ThresholdTokens {
		return lc.Input, lc.Output
	}
	return m.Input, m.Output
}

type multipliers struct {
	CacheWrite5m *float64 `json:"cache_write_5m"`
	CacheWrite1h *float64 `json:"cache_write_1h"`
	CacheRead    *float64 `json:"cache_read"`
}

// table は pricing.json の形。
type table struct {
	DefaultModel         string                `json:"default_model"`
	DefaultContextWindow int64                 `json:"default_context_window"`
	Multipliers          multipliers           `json:"multipliers"`
	Models               map[string]modelPrice `json:"models"`
}

func (t table) validate() error {
	if t.DefaultModel == "" {
		return fmt.Errorf("価格表に default_model が無い")
	}
	if t.DefaultContextWindow <= 0 {
		return fmt.Errorf("価格表に default_context_window が無い")
	}
	if _, ok := t.Models[t.DefaultModel]; !ok {
		return fmt.Errorf("default_model %q が models に無い", t.DefaultModel)
	}
	if t.Multipliers.CacheWrite5m == nil || t.Multipliers.CacheWrite1h == nil || t.Multipliers.CacheRead == nil {
		return fmt.Errorf("価格表の multipliers に cache_write_5m / cache_write_1h / cache_read が揃っていない")
	}
	for name, m := range t.Models {
		if m.Input <= 0 || m.Output <= 0 {
			return fmt.Errorf("モデル %q の input / output 単価が正ではない", name)
		}
	}
	return nil
}

// Pricing はモデル別の単価表。モデル ID は最長一致の前方一致で引く
// (`claude-haiku-4-5-20251001` → `claude-haiku-4-5`)。表に無いモデルは default_model の単価で
// 計算し、UnknownModels に残す。
type Pricing struct {
	t       table
	unknown map[string]bool
}

// Load は価格表の JSON を読む。
func Load(data []byte) (*Pricing, error) {
	var t table
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("価格表の JSON が読めない: %w", err)
	}
	if err := t.validate(); err != nil {
		return nil, err
	}
	return &Pricing{t: t, unknown: map[string]bool{}}, nil
}

// resolve はモデル ID を価格表の鍵に写す。表に無ければ default_model。
func (p *Pricing) resolve(model string) string {
	if _, ok := p.t.Models[model]; ok {
		return model
	}
	if k := p.longestPrefix(model); k != "" {
		return k
	}
	p.unknown[model] = true
	return p.t.DefaultModel
}

func (p *Pricing) longestPrefix(model string) string {
	best := ""
	for k := range p.t.Models {
		if strings.HasPrefix(model, k) && len(k) > len(best) {
			best = k
		}
	}
	return best
}

// UnknownModels は default の単価で計算したモデル ID (昇順)。
func (p *Pricing) UnknownModels() []string {
	out := make([]string, 0, len(p.unknown))
	for m := range p.unknown {
		out = append(out, m)
	}
	slices.Sort(out)
	return out
}

// ContextWindow はモデルの既定の context 窓 (トークン)。
func (p *Pricing) ContextWindow(model string) int64 {
	if w := p.t.Models[p.resolve(model)].ContextWindow; w != nil {
		return *w
	}
	return p.t.DefaultContextWindow
}

func pick(override, base *float64) float64 {
	if override != nil {
		return *override
	}
	return *base
}

// RatesFor は model と prompt の大きさ (context) から単価を返す。
func (p *Pricing) RatesFor(model string, context int64) Rates {
	m := p.t.Models[p.resolve(model)]
	in, out := m.base(context)
	return Rates{
		Input:  in / 1e6,
		Output: out / 1e6,
		W5:     pick(m.CacheWrite5m, p.t.Multipliers.CacheWrite5m),
		W1:     pick(m.CacheWrite1h, p.t.Multipliers.CacheWrite1h),
		Read:   pick(m.CacheRead, p.t.Multipliers.CacheRead),
	}
}

// IsExpensive は input 単価が $5/M 以上か (Opus 5 以前の Opus と Fable)。
func (p *Pricing) IsExpensive(model string) bool {
	return p.t.Models[p.resolve(model)].Input >= expensiveInputPerM
}

// Cost は 1 ターンの usage を USD にする。
func (p *Pricing) Cost(model string, u transcript.Usage) Cost {
	r := p.RatesFor(model, u.Context())
	return Cost{
		Input:      float64(u.Input) * r.Input,
		CacheWrite: (float64(u.Cache5m)*r.W5 + float64(u.Cache1h)*r.W1) * r.Input,
		CacheRead:  float64(u.CacheRead) * r.Read * r.Input,
		Output:     float64(u.Output) * r.Output,
	}
}
