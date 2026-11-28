// Package transcript は ~/.claude/projects 配下の transcript (JSONL) を読み、監査に要る形にする。
// 形式は Claude Code の内部形式でバージョンにより変わるため、読めない行は飛ばす。
package transcript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/usadamasa/agents-marketplace/plugins/harness/internal/jsonlscan"
)

// ImageResultChars は tool_result の image block 1 つぶんの見なし文字数。
// 実体は base64 で、文字数 ÷ 4 だとトークン数を大きく見誤る。
const ImageResultChars = 6400

// Usage は 1 ターンの usage。cache_creation の内訳が無い行は 5 分 TTL と見なす。
type Usage struct {
	Input, Cache5m, Cache1h, CacheRead, Output int64
}

// CacheWrite は 5m と 1h の cache write の合計。
func (u Usage) CacheWrite() int64 { return u.Cache5m + u.Cache1h }

// Context はそのターンが送った prompt のおおよその大きさ。
func (u Usage) Context() int64 { return u.Input + u.CacheRead + u.CacheWrite() }

// ToolUse は assistant の content にある tool_use のうち監査に要る部分。
type ToolUse struct {
	ID, Name     string
	FilePath     string
	ContentChars int    // Write の content の長さ (UTF-8 バイト)。Edit は数えない
	CommandHead  string // Bash の先頭語だけ。コマンド全体は出力しない
}

// ToolResult は user 行の tool_result。Chars は transcript に入った JSON の長さ (= context に入った量)。
// 大きい出力は Claude Code が tool-results/ へ退避し、transcript には要約の stub だけが残る。
// context に入ったのはその stub なので、退避先のファイルは読まない。
type ToolResult struct {
	ToolUseID string
	TS        time.Time
	Chars     int
	TurnIndex int // 呼び出した Turn の index。assistant より前に現れた result は -1
}

// Turn は message.id で重複排除した assistant メッセージ 1 つ。
type Turn struct {
	MsgID    string
	TS       time.Time
	Model    string
	Version  string // Claude Code のバージョン (変わると cache が切れる)
	Usage    Usage
	ToolUses []ToolUse
}

// Compaction は compact_boundary。TurnIndex は圧縮後の最初の Turn の index。
type Compaction struct {
	TurnIndex  int
	Trigger    string
	PreTokens  int64
	PostTokens int64
}

// Session は transcript 1 ファイルぶん。subagent の transcript も同じ型で持つ。
type Session struct {
	SessionID   string
	Project     string
	Path        string
	Title       string
	Subagent    bool
	AgentType   string // subagent の種別 (meta.json の agentType)
	Turns       []*Turn
	ToolResults []ToolResult
	ModeEvents  map[int]bool // permission mode が変わった直後の Turn の index
	Compactions []Compaction
	Subagents   []*Session
}

// DisplayName は custom title か session ID の先頭 8 文字。
func (s *Session) DisplayName() string {
	if s.Title != "" {
		return s.Title
	}
	if len(s.SessionID) > 8 {
		return s.SessionID[:8]
	}
	return s.SessionID
}

// Start は最初の Turn の時刻。Turn が無ければゼロ値。
func (s *Session) Start() time.Time {
	if len(s.Turns) == 0 {
		return time.Time{}
	}
	return s.Turns[0].TS
}

// End は最後の Turn の時刻。Turn が無ければゼロ値。
func (s *Session) End() time.Time {
	if len(s.Turns) == 0 {
		return time.Time{}
	}
	return s.Turns[len(s.Turns)-1].TS
}

// UsedOneHourTTL はこのセッションが 1h TTL の cache write を 1 度でもしたか。
func (s *Session) UsedOneHourTTL() bool {
	for _, t := range s.Turns {
		if t.Usage.Cache1h > 0 {
			return true
		}
	}
	return false
}

// dedup はファイルを跨いだ message.id の重複排除。resume / fork は履歴を新しいファイルへ写すので、
// 同じ message.id が複数ファイルに現れる。最初に見たファイルだけが数える。
type dedup struct {
	owner map[string]string // message.id → 最初に見たファイル
	count int               // 他ファイルで数えなかったメッセージ数
}

// newDedup は空の dedup を返す。
func newDedup() *dedup {
	return &dedup{owner: map[string]string{}}
}

type usageJSON struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreation            *struct {
		FiveMinute int64 `json:"ephemeral_5m_input_tokens"`
		OneHour    int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

// line は JSONL 1 行のうち要る部分。
type line struct {
	Type           string `json:"type"`
	Subtype        string `json:"subtype"`
	IsSidechain    bool   `json:"isSidechain"`
	Timestamp      string `json:"timestamp"`
	Version        string `json:"version"`
	CustomTitle    string `json:"customTitle"`
	Mode           string `json:"mode"`
	PermissionMode string `json:"permissionMode"`
	CompactMeta    *struct {
		Trigger    string `json:"trigger"`
		PreTokens  int64  `json:"preTokens"`
		PostTokens int64  `json:"postTokens"`
	} `json:"compactMetadata"`
	Message *struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Usage   *usageJSON      `json:"usage"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

var envAssignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// parser はファイル 1 つを読む間の状態。
type parser struct {
	s        *Session
	path     string
	d        *dedup
	byID     map[string]*Turn
	skipping bool            // 他ファイル由来の重複メッセージの最中 (tool_result も捨てる)
	skipped  map[string]bool // このファイルで数えなかった message.id
	lastMode map[string]string
}

// parseFile は transcript 1 ファイルを Session にする。subagent の transcript は全行が
// isSidechain なので、subagent のときだけ sidechain の行を読む (main の transcript では飛ばす)。
func parseFile(path, sessionID, project string, subagent bool, d *dedup) (*Session, error) {
	f, err := os.Open(path) // #nosec G304 -- 自分の ~/.claude/projects 配下を走査するだけ
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	p := &parser{
		s:        &Session{SessionID: sessionID, Project: project, Path: path, Subagent: subagent, ModeEvents: map[int]bool{}},
		path:     path,
		d:        d,
		byID:     map[string]*Turn{},
		skipped:  map[string]bool{},
		lastMode: map[string]string{},
	}
	sc := jsonlscan.NewScanner(f)
	for sc.Scan() {
		var l line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			continue
		}
		p.handle(&l)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s の読み込みに失敗: %w", path, err)
	}
	return p.s, nil
}

func (p *parser) handle(l *line) {
	switch l.Type {
	case "assistant":
		p.handleAssistant(l)
	case "user":
		if !p.skipping && !p.foreign(l) {
			p.handleUser(l)
		}
	case "custom-title":
		if l.CustomTitle != "" {
			p.s.Title = l.CustomTitle
		}
	case "mode":
		p.handleMode("mode", l.Mode)
	case "permission-mode":
		p.handleMode("permission-mode", l.PermissionMode)
	case "system":
		if l.Subtype == "compact_boundary" && !p.foreign(l) && l.CompactMeta != nil {
			p.s.Compactions = append(p.s.Compactions, Compaction{
				TurnIndex: len(p.s.Turns), Trigger: l.CompactMeta.Trigger,
				PreTokens: l.CompactMeta.PreTokens, PostTokens: l.CompactMeta.PostTokens,
			})
		}
	}
}

// foreign はこの Session に属さない行か。main の transcript にある sidechain の行は
// subagent のもの (古いバージョンでは subagents/ に分かれず main に混ざる)。
func (p *parser) foreign(l *line) bool {
	return l.IsSidechain && !p.s.Subagent
}

// handleMode は mode の記録を見る。Claude Code は値が変わらなくても prompt ごとに記録するので、
// 前回と違うときだけ event にする。
func (p *parser) handleMode(kind, value string) {
	prev, seen := p.lastMode[kind]
	p.lastMode[kind] = value
	if seen && prev != value {
		p.s.ModeEvents[len(p.s.Turns)] = true
	}
}

func (p *parser) handleAssistant(l *line) {
	m := l.Message
	if p.foreign(l) || m == nil || m.Usage == nil || m.Model == "<synthetic>" || l.Timestamp == "" {
		return
	}
	ts, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil {
		return
	}
	if owner, ok := p.d.owner[m.ID]; ok && owner != p.path {
		p.skipping = true
		if !p.skipped[m.ID] {
			p.skipped[m.ID] = true
			p.d.count++
		}
		return
	}
	p.skipping = false
	p.d.owner[m.ID] = p.path

	uses := parseToolUses(m.Content)
	usage := parseUsage(m.Usage)
	if t, ok := p.byID[m.ID]; ok {
		// 同じ id の行は content block ごとの分割。usage と時刻は最後の行で上書きし、tool_use は集める
		t.Usage, t.TS = usage, ts
		known := map[string]bool{}
		for _, u := range t.ToolUses {
			known[u.ID] = true
		}
		for _, u := range uses {
			if !known[u.ID] {
				t.ToolUses = append(t.ToolUses, u)
			}
		}
		return
	}
	model := m.Model
	if model == "" {
		model = "unknown"
	}
	t := &Turn{MsgID: m.ID, TS: ts, Model: model, Version: l.Version, Usage: usage, ToolUses: uses}
	p.byID[m.ID] = t
	p.s.Turns = append(p.s.Turns, t)
}

func (p *parser) handleUser(l *line) {
	if l.Message == nil {
		return
	}
	var blocks []contentBlock
	if err := json.Unmarshal(l.Message.Content, &blocks); err != nil {
		return // content が文字列の行 (利用者の発話) は tool_result を持たない
	}
	var ts time.Time
	if l.Timestamp != "" {
		ts, _ = time.Parse(time.RFC3339Nano, l.Timestamp)
	}
	for _, b := range blocks {
		if b.Type != "tool_result" {
			continue
		}
		p.s.ToolResults = append(p.s.ToolResults, ToolResult{
			ToolUseID: b.ToolUseID, TS: ts, Chars: resultChars(b.Content), TurnIndex: len(p.s.Turns) - 1,
		})
	}
}

func parseUsage(u *usageJSON) Usage {
	out := Usage{Input: u.InputTokens, CacheRead: u.CacheReadInputTokens, Output: u.OutputTokens}
	if cc := u.CacheCreation; cc != nil && cc.FiveMinute+cc.OneHour > 0 {
		out.Cache5m, out.Cache1h = cc.FiveMinute, cc.OneHour
	} else {
		out.Cache5m = u.CacheCreationInputTokens
	}
	return out
}

func parseToolUses(content json.RawMessage) []ToolUse {
	var blocks []contentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil
	}
	var out []ToolUse
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		var in struct {
			FilePath string          `json:"file_path"`
			Content  json.RawMessage `json:"content"`
			Command  string          `json:"command"`
		}
		_ = json.Unmarshal(b.Input, &in)
		u := ToolUse{ID: b.ID, Name: b.Name, FilePath: in.FilePath, CommandHead: commandHead(in.Command)}
		var content string
		if json.Unmarshal(in.Content, &content) == nil {
			u.ContentChars = len(content)
		}
		if u.Name == "" {
			u.Name = "?"
		}
		out = append(out, u)
	}
	return out
}

// commandHead は Bash コマンドの先頭語。環境変数の代入と export は飛ばす。
func commandHead(cmd string) string {
	for tok := range strings.FieldsSeq(cmd) {
		if tok == "export" || envAssignRe.MatchString(tok) {
			continue
		}
		return tok
	}
	return ""
}

// resultChars は tool_result の content の長さ。image block は定数で見なす。
func resultChars(content json.RawMessage) int {
	var blocks []json.RawMessage
	if err := json.Unmarshal(content, &blocks); err != nil {
		return len(content)
	}
	total := 0
	for _, b := range blocks {
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(b, &head) == nil && head.Type == "image" {
			total += ImageResultChars
		} else {
			total += len(b)
		}
	}
	return total
}

// Options は Load の絞り込み。
type Options struct {
	Days    int
	All     bool
	Project string // project slug の部分一致
	Now     time.Time
}

// Loaded は読み込んだセッション群と、読み込み時の統計。
type Loaded struct {
	Sessions     []*Session
	Deduplicated int
}

// Load は projectsDir 配下の transcript を読む。期間はセッションの最後の Turn の時刻で見る。
// subagent の transcript (<project>/<session>/subagents/*.jsonl) は親の Subagents に入れる。
func Load(projectsDir string, opts Options) (*Loaded, error) {
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, fmt.Errorf("projects ディレクトリが読めない (%s): %w", projectsDir, err)
	}
	var cutoff time.Time
	if !opts.All {
		cutoff = opts.Now.Add(-time.Duration(opts.Days) * 24 * time.Hour)
	}
	d := newDedup()
	out := &Loaded{}
	for _, e := range entries {
		if !e.IsDir() || (opts.Project != "" && !strings.Contains(e.Name(), opts.Project)) {
			continue
		}
		files, err := filepath.Glob(filepath.Join(projectsDir, e.Name(), "*.jsonl"))
		if err != nil {
			return nil, err
		}
		sort.Strings(files)
		for _, fp := range files {
			s, err := loadSession(fp, e.Name(), d)
			if err != nil {
				return nil, err
			}
			if len(s.Turns) == 0 || (!opts.All && s.End().Before(cutoff)) {
				continue
			}
			out.Sessions = append(out.Sessions, s)
		}
	}
	out.Deduplicated = d.count
	return out, nil
}

func loadSession(path, project string, d *dedup) (*Session, error) {
	sid := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	s, err := parseFile(path, sid, project, false, d)
	if err != nil {
		return nil, err
	}
	if len(s.Turns) == 0 {
		return s, nil
	}
	subs, err := filepath.Glob(filepath.Join(filepath.Dir(path), sid, "subagents", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(subs)
	for _, ap := range subs {
		sub, err := parseFile(ap, strings.TrimSuffix(filepath.Base(ap), ".jsonl"), project, true, d)
		if err != nil {
			return nil, err
		}
		if len(sub.Turns) == 0 {
			continue
		}
		sub.AgentType = readAgentType(strings.TrimSuffix(ap, ".jsonl") + ".meta.json")
		sub.Title = s.DisplayName() + " (subagent"
		if sub.AgentType != "" {
			sub.Title += ": " + sub.AgentType
		}
		sub.Title += ")"
		s.Subagents = append(s.Subagents, sub)
	}
	return s, nil
}

// readAgentType は subagent の meta.json から agentType を読む。無ければ空。
func readAgentType(path string) string {
	data, err := os.ReadFile(path) // #nosec G304 -- transcript と同じディレクトリの sidecar
	if err != nil {
		return ""
	}
	var meta struct {
		AgentType string `json:"agentType"`
	}
	if json.Unmarshal(data, &meta) != nil {
		return ""
	}
	return meta.AgentType
}
