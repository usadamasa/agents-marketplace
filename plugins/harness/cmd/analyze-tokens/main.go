// analyze-tokens は ~/.claude/projects の transcript を横断して token の浪費を見つけ、
// 推定 USD の大きい順に並べる。message の本文は出力しない (セッション名、project slug、
// ファイルパス、ツール名、Bash の先頭語、数値だけ)。ネットワークには出ない。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/analyze-tokens/internal/pricing"
	"github.com/usadamasa/agents-marketplace/plugins/harness/cmd/analyze-tokens/internal/transcript"
)

type runOptions struct {
	ProjectsDir string
	Days        int
	All         bool
	Project     string
	Top         int
	Rule        string // findings をこのルール (R1〜R6) だけに絞る。by_rule は絞らない
	PricingPath string
	Thresholds  []string
	Now         time.Time
}

func main() {
	var opts runOptions
	flag.StringVar(&opts.ProjectsDir, "projects-dir", "", "transcript の置き場 (省略時: $CLAUDE_CONFIG_DIR か ~/.claude の projects/)")
	flag.IntVar(&opts.Days, "days", 30, "直近 N 日のセッションを見る")
	flag.BoolVar(&opts.All, "all", false, "--days を無視して全セッションを見る")
	flag.StringVar(&opts.Project, "project", "", "project slug にこの文字列を含むものだけ")
	flag.IntVar(&opts.Top, "top", 10, "findings の件数")
	flag.StringVar(&opts.Rule, "rule", "", "findings をこのルールだけに絞る (R1〜R6)。by_rule の集計は絞らない")
	flag.StringVar(&opts.PricingPath, "pricing", "", "価格表 JSON (省略時: 同梱の pricing.json)")
	flag.Func("threshold", "検出の閾値を NAME=VALUE で上書き (繰り返し可)。使えるもの: "+strings.Join(thresholdNames(), ", "),
		func(s string) error { opts.Thresholds = append(opts.Thresholds, s); return nil })
	flag.Parse()

	if opts.ProjectsDir == "" {
		opts.ProjectsDir = defaultProjectsDir()
	}
	opts.Now = time.Now()
	if err := run(os.Stdout, opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// defaultProjectsDir は Claude Code と同じ規則で projects/ を決める。
func defaultProjectsDir() string {
	base := os.Getenv("CLAUDE_CONFIG_DIR")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ホームディレクトリの取得に失敗: %v\n", err)
			os.Exit(1)
		}
		base = filepath.Join(home, ".claude")
	}
	return filepath.Join(base, "projects")
}

// knownRules は --rule に渡せる値。
var knownRules = []string{"R1", "R2", "R3", "R4", "R5", "R6"}

func run(w io.Writer, opts runOptions) error {
	th, err := parseThresholds(opts.Thresholds)
	if err != nil {
		return err
	}
	if opts.Rule != "" && !slices.Contains(knownRules, opts.Rule) {
		return fmt.Errorf("--rule は %s のどれか: %q", strings.Join(knownRules, " / "), opts.Rule)
	}
	data := pricing.DefaultJSON
	if opts.PricingPath != "" {
		data, err = os.ReadFile(opts.PricingPath) // #nosec G304 -- CLI ツール: パスはフラグ由来
		if err != nil {
			return fmt.Errorf("価格表が読めない: %w", err)
		}
	}
	p, err := pricing.Load(data)
	if err != nil {
		return err
	}
	loaded, err := transcript.Load(opts.ProjectsDir, transcript.Options{Days: opts.Days, All: opts.All, Project: opts.Project, Now: opts.Now})
	if err != nil {
		return err
	}

	var findings []Finding
	for _, s := range loaded.Sessions {
		findings = append(findings, detect(s, p, th)...)
		for _, sub := range s.Subagents {
			findings = append(findings, detect(sub, p, th)...)
		}
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].WasteUSD > findings[j].WasteUSD })

	days := opts.Days
	if opts.All {
		days = 0
	}
	rep := buildReport(loaded, findings, p, reportOptions{Top: opts.Top, Rule: opts.Rule, Days: days})
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		return fmt.Errorf("レポートの出力に失敗: %w", err)
	}
	return nil
}
