package main

import (
	"sort"
	"time"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/analyze-tokens/internal/pricing"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/analyze-tokens/internal/transcript"
)

const (
	shortSessionTurns = 10
	topSessions       = 5
)

// report は analyze-tokens の出力。鍵名は参考実装 (tatsuo48/claude-token-audit) と揃え、
// skill の references/rules.md がこの鍵で引く。
type report struct {
	Period   periodInfo   `json:"period"`
	Sessions int          `json:"sessions"`
	Totals   totalsReport `json:"totals"`
	ByModel  []modelCost  `json:"by_model"`
	Findings []Finding    `json:"findings"`
	ByRule   []ruleTotal  `json:"by_rule"`
	Info     infoReport   `json:"info"`
}

type periodInfo struct {
	Days int    `json:"days,omitempty"` // --all のときは省く
	From string `json:"from"`
	To   string `json:"to"`
}

type totalsReport struct {
	pricing.Cost
	Total         float64 `json:"total"`
	CacheHitRatio float64 `json:"cache_hit_ratio"` // cache_read / (cache_read + cache_write) トークン比
}

type modelCost struct {
	Model    string  `json:"model"`
	USD      float64 `json:"usd"`
	Share    float64 `json:"share"`
	Sessions int     `json:"sessions"`
}

type ruleTotal struct {
	Rule  string  `json:"rule"`
	Count int     `json:"count"`
	USD   float64 `json:"usd"`
}

type sessionCost struct {
	Name               string  `json:"name"`
	Project            string  `json:"project"`
	USD                float64 `json:"usd"`
	Turns              int     `json:"turns"`
	FinalContextTokens int64   `json:"final_context_tokens"`
}

type outputRow struct {
	Name         string  `json:"name"`
	Project      string  `json:"project"`
	USDOutput    float64 `json:"usd_output"`
	OutputTokens int64   `json:"output_tokens"`
}

type subagentInfo struct {
	Count     int     `json:"count"`
	USD       float64 `json:"usd"`
	Share     float64 `json:"share"`
	TTL5mRuns int     `json:"ttl_5m_runs"` // 5 分 TTL の cache write だけで終えた subagent の数
}

type compactionInfo struct {
	Count  int `json:"count"`
	Auto   int `json:"auto"`
	Manual int `json:"manual"`
}

type infoReport struct {
	TopCostSessions   []sessionCost  `json:"top_cost_sessions"`
	TopOutputSessions []outputRow    `json:"top_output_sessions"`
	Subagents         subagentInfo   `json:"subagents"`
	Compactions       compactionInfo `json:"compactions"`
	// ShortSessionsOnExpensiveModels は 10 ターン以下で終わった、input $5/M 以上のモデルのセッション数。
	ShortSessionsOnExpensiveModels int      `json:"short_sessions_on_expensive_models"`
	UnknownModels                  []string `json:"unknown_models"`
	DeduplicatedMessages           int      `json:"deduplicated_messages"`
}

type reportOptions struct {
	Top  int
	Rule string // 空なら全ルール
	Days int    // 0 なら --all
}

// selectFindings は findings の一覧を rule で絞り、top 件に切る。by_rule の集計には使わない。
func selectFindings(findings []Finding, opts reportOptions) []Finding {
	out := findings
	if opts.Rule != "" {
		out = nil
		for _, f := range findings {
			if f.Rule == opts.Rule {
				out = append(out, f)
			}
		}
	}
	return out[:min(max(opts.Top, 0), len(out))]
}

// accumulator はセッションを順に足していく途中の状態。
type accumulator struct {
	p             *pricing.Pricing
	totals        pricing.Cost
	byModel       map[string]pricing.Cost
	modelSessions map[string]map[string]bool
	readTokens    int64
	writeTokens   int64
	info          infoReport
	costRows      []sessionCost
	outputRows    []outputRow
	starts, ends  []time.Time
}

func addCost(a, b pricing.Cost) pricing.Cost {
	return pricing.Cost{Input: a.Input + b.Input, CacheWrite: a.CacheWrite + b.CacheWrite, CacheRead: a.CacheRead + b.CacheRead, Output: a.Output + b.Output}
}

func (a *accumulator) addTurn(sessionID string, t *transcript.Turn) pricing.Cost {
	c := a.p.Cost(t.Model, t.Usage)
	a.byModel[t.Model] = addCost(a.byModel[t.Model], c)
	if a.modelSessions[t.Model] == nil {
		a.modelSessions[t.Model] = map[string]bool{}
	}
	a.modelSessions[t.Model][sessionID] = true
	a.readTokens += t.Usage.CacheRead
	a.writeTokens += t.Usage.CacheWrite()
	return c
}

func (a *accumulator) addSession(s *transcript.Session) {
	var sc pricing.Cost
	var outTokens int64
	for _, t := range s.Turns {
		sc = addCost(sc, a.addTurn(s.SessionID, t))
		outTokens += t.Usage.Output
	}
	for _, sub := range s.Subagents {
		a.info.Subagents.Count++
		if !sub.UsedOneHourTTL() {
			a.info.Subagents.TTL5mRuns++
		}
		for _, t := range sub.Turns {
			c := a.addTurn(s.SessionID, t)
			sc = addCost(sc, c)
			a.info.Subagents.USD += c.Total()
		}
	}
	for _, c := range s.Compactions {
		a.info.Compactions.Count++
		switch c.Trigger {
		case "auto":
			a.info.Compactions.Auto++
		case "manual":
			a.info.Compactions.Manual++
		}
	}
	a.totals = addCost(a.totals, sc)
	a.costRows = append(a.costRows, sessionCost{
		Name: s.DisplayName(), Project: s.Project, USD: sc.Total(), Turns: len(s.Turns),
		FinalContextTokens: s.Turns[len(s.Turns)-1].Usage.Context(),
	})
	a.outputRows = append(a.outputRows, outputRow{Name: s.DisplayName(), Project: s.Project, USDOutput: sc.Output, OutputTokens: outTokens})
	if len(s.Turns) <= shortSessionTurns && a.p.IsExpensive(s.Turns[0].Model) {
		a.info.ShortSessionsOnExpensiveModels++
	}
	a.starts = append(a.starts, s.Start())
	a.ends = append(a.ends, s.End())
}

// buildReport は読み込んだセッションと findings からレポートを組む。findings は waste 降順で渡す。
func buildReport(loaded *transcript.Loaded, findings []Finding, p *pricing.Pricing, opts reportOptions) report {
	a := &accumulator{p: p, byModel: map[string]pricing.Cost{}, modelSessions: map[string]map[string]bool{}}
	for _, s := range loaded.Sessions {
		if len(s.Turns) > 0 {
			a.addSession(s)
		}
	}
	grand := a.totals.Total()
	if grand == 0 {
		grand = 1
	}

	models := make([]modelCost, 0, len(a.byModel))
	for m, c := range a.byModel {
		models = append(models, modelCost{Model: m, USD: c.Total(), Share: c.Total() / grand, Sessions: len(a.modelSessions[m])})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].USD != models[j].USD {
			return models[i].USD > models[j].USD
		}
		return models[i].Model < models[j].Model
	})

	byRule := map[string]*ruleTotal{}
	for _, f := range findings {
		if byRule[f.Rule] == nil {
			byRule[f.Rule] = &ruleTotal{Rule: f.Rule}
		}
		byRule[f.Rule].Count++
		byRule[f.Rule].USD += f.WasteUSD
	}
	rules := make([]ruleTotal, 0, len(byRule))
	for _, r := range byRule {
		rules = append(rules, *r)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Rule < rules[j].Rule })

	sort.Slice(a.costRows, func(i, j int) bool { return a.costRows[i].USD > a.costRows[j].USD })
	sort.Slice(a.outputRows, func(i, j int) bool { return a.outputRows[i].USDOutput > a.outputRows[j].USDOutput })

	info := a.info
	info.TopCostSessions = a.costRows[:min(topSessions, len(a.costRows))]
	info.TopOutputSessions = a.outputRows[:min(topSessions, len(a.outputRows))]
	info.Subagents.Share = info.Subagents.USD / grand
	info.UnknownModels = p.UnknownModels()
	info.DeduplicatedMessages = loaded.Deduplicated

	hit := 0.0
	if a.readTokens+a.writeTokens > 0 {
		hit = float64(a.readTokens) / float64(a.readTokens+a.writeTokens)
	}
	return report{
		Period:   periodInfo{Days: opts.Days, From: formatBound(a.starts, true), To: formatBound(a.ends, false)},
		Sessions: len(a.costRows),
		Totals:   totalsReport{Cost: a.totals, Total: a.totals.Total(), CacheHitRatio: hit},
		ByModel:  models,
		Findings: selectFindings(findings, opts),
		ByRule:   rules,
		Info:     info,
	}
}

// formatBound は最小 (earliest) か最大の時刻を RFC3339 で返す。空なら空文字。
func formatBound(ts []time.Time, earliest bool) string {
	if len(ts) == 0 {
		return ""
	}
	b := ts[0]
	for _, t := range ts[1:] {
		if (earliest && t.Before(b)) || (!earliest && t.After(b)) {
			b = t
		}
	}
	return b.UTC().Format(time.RFC3339)
}
