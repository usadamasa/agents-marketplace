package transcript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- フィクスチャ (実機の JSONL の形を最小限に写す) ---

type fxUsage struct {
	input, c5, c1, read, output int64
}

func (u fxUsage) json() string {
	return fmt.Sprintf(`{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,"cache_creation":{"ephemeral_5m_input_tokens":%d,"ephemeral_1h_input_tokens":%d}}`,
		u.input, u.c5+u.c1, u.read, u.output, u.c5, u.c1)
}

// assistantLine は assistant 1 行。blocks は content[] の JSON 要素。
func assistantLine(id, ts, model string, u fxUsage, blocks ...string) string {
	return fmt.Sprintf(`{"type":"assistant","isSidechain":false,"timestamp":%q,"version":"2.1.296","effort":"high","message":{"id":%q,"model":%q,"usage":%s,"content":[%s]}}`,
		ts, id, model, u.json(), strings.Join(blocks, ","))
}

func toolUseBlock(id, name string, input map[string]any) string {
	in, _ := json.Marshal(input)
	return fmt.Sprintf(`{"type":"tool_use","id":%q,"name":%q,"input":%s}`, id, name, in)
}

func textBlock(text string) string {
	return fmt.Sprintf(`{"type":"text","text":%q}`, text)
}

// userResultLine は tool_result を 1 つ持つ user 行。content は tool_result の content (JSON リテラル)。
func userResultLine(ts, toolUseID, content string) string {
	return fmt.Sprintf(`{"type":"user","isSidechain":false,"timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":%s}]}}`,
		ts, toolUseID, content)
}

func writeJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

const (
	ts0 = "2026-10-01T10:00:00.000Z"
	ts1 = "2026-10-01T10:01:00.000Z"
	ts2 = "2026-10-01T10:02:00.000Z"
)

var now = time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)

// --- parseFile ---

func TestParseFileDedupesSameMessageIDWithinFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	u := fxUsage{input: 2, c1: 1000, read: 500, output: 40}
	writeJSONL(t, path,
		assistantLine("m1", ts0, "claude-opus-5", u, textBlock("a")),
		assistantLine("m1", ts1, "claude-opus-5", u, toolUseBlock("tu1", "Read", map[string]any{"file_path": "/x"})),
		assistantLine("m2", ts2, "claude-opus-5", u, textBlock("b")),
	)
	s, err := parseFile(path, "s1", "proj", false, newDedup())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Turns) != 2 {
		t.Fatalf("turns = %d, want 2 (同じ message.id は 1 ターン)", len(s.Turns))
	}
	if got := s.Turns[0].TS.Format(time.RFC3339); got != "2026-10-01T10:01:00Z" {
		t.Errorf("同じ id の最後の行の timestamp を採るべき: got %s", got)
	}
	if len(s.Turns[0].ToolUses) != 1 || s.Turns[0].ToolUses[0].FilePath != "/x" {
		t.Errorf("content block の tool_use が集まっていない: %+v", s.Turns[0].ToolUses)
	}
	if s.Turns[0].Usage.Cache1h != 1000 || s.Turns[0].Usage.Context() != 1502 {
		t.Errorf("usage = %+v", s.Turns[0].Usage)
	}
}

func TestParseFileSkipsSyntheticAndSidechainAndBrokenLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	writeJSONL(t, path,
		`{"type":"assistant","timestamp":"`+ts0+`","message":{"id":"syn","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0},"content":[]}}`,
		`{"type":"assistant","isSidechain":true,"timestamp":"`+ts0+`","message":{"id":"side","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":5},"content":[]}}`,
		`not json at all`,
		`{"type":"queue-operation","operation":"enqueue"}`,
		assistantLine("m1", ts1, "claude-opus-5", fxUsage{input: 1, output: 1}, textBlock("ok")),
	)
	s, err := parseFile(path, "s1", "proj", false, newDedup())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Turns) != 1 || s.Turns[0].MsgID != "m1" {
		t.Errorf("synthetic / sidechain / 壊れた行を除いて 1 ターンのはず: %+v", s.Turns)
	}
}

func TestParseFileSubagentReadsSidechainLines(t *testing.T) {
	// subagent の transcript は全行が isSidechain:true。subagent として読むときはそれを採る。
	path := filepath.Join(t.TempDir(), "agent-a1.jsonl")
	writeJSONL(t, path,
		`{"type":"assistant","isSidechain":true,"timestamp":"`+ts0+`","message":{"id":"a1","model":"claude-sonnet-5","usage":{"input_tokens":5,"output_tokens":5},"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","isSidechain":true,"timestamp":"`+ts0+`","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"x"}]}}`,
	)
	s, err := parseFile(path, "agent-a1", "proj", true, newDedup())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Turns) != 1 || len(s.ToolResults) != 1 || !s.Subagent {
		t.Errorf("subagent: turns=%d results=%d subagent=%v", len(s.Turns), len(s.ToolResults), s.Subagent)
	}
}

func TestParseFileUsageWithoutBreakdownIsFiveMinute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	writeJSONL(t, path,
		`{"type":"assistant","timestamp":"`+ts0+`","message":{"id":"m1","model":"claude-opus-5","usage":{"input_tokens":1,"cache_creation_input_tokens":700,"cache_read_input_tokens":0,"output_tokens":1},"content":[]}}`,
	)
	s, err := parseFile(path, "s1", "proj", false, newDedup())
	if err != nil {
		t.Fatal(err)
	}
	if s.Turns[0].Usage.Cache5m != 700 || s.Turns[0].Usage.Cache1h != 0 {
		t.Errorf("内訳が無ければ 5m 扱い: %+v", s.Turns[0].Usage)
	}
}

func TestParseFileCollectsToolResultsTitleCompactionAndModeChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	u := fxUsage{input: 1, c5: 10, output: 1}
	writeJSONL(t, path,
		`{"type":"custom-title","customTitle":"my-task"}`,
		`{"type":"permission-mode","permissionMode":"auto"}`,
		assistantLine("m1", ts0, "claude-opus-5", u, toolUseBlock("tu1", "Bash", map[string]any{"command": "FOO=1 export BAR=2 rg -n x"})),
		userResultLine(ts0, "tu1", `"0123456789"`),
		`{"type":"permission-mode","permissionMode":"auto"}`,
		`{"type":"system","subtype":"compact_boundary","timestamp":"`+ts1+`","compactMetadata":{"trigger":"auto","preTokens":168366,"postTokens":18000}}`,
		`{"type":"permission-mode","permissionMode":"plan"}`,
		assistantLine("m2", ts2, "claude-opus-5", u, toolUseBlock("tu2", "Write", map[string]any{"file_path": "/w", "content": strings.Repeat("x", 100)})),
		userResultLine(ts2, "tu2", `[{"type":"text","text":"ok"},{"type":"image","source":{"data":"AAAA"}}]`),
	)
	s, err := parseFile(path, "s1", "proj", false, newDedup())
	if err != nil {
		t.Fatal(err)
	}
	if s.DisplayName() != "my-task" {
		t.Errorf("title = %q", s.DisplayName())
	}
	if len(s.ToolResults) != 2 {
		t.Fatalf("tool_results = %d, want 2", len(s.ToolResults))
	}
	if s.ToolResults[0].Chars != 12 || s.ToolResults[0].TurnIndex != 0 {
		t.Errorf("文字列の tool_result: %+v (JSON リテラルの長さ 12 を期待)", s.ToolResults[0])
	}
	// text block は JSON 長、image block は定数
	if want := len(`{"type":"text","text":"ok"}`) + ImageResultChars; s.ToolResults[1].Chars != want {
		t.Errorf("配列の tool_result: chars = %d, want %d", s.ToolResults[1].Chars, want)
	}
	if got := s.Turns[0].ToolUses[0].CommandHead; got != "rg" {
		t.Errorf("Bash の先頭語 (環境変数代入と export を飛ばす) = %q, want rg", got)
	}
	if got := s.Turns[1].ToolUses[0].ContentChars; got != 100 {
		t.Errorf("Write の content 長 = %d, want 100", got)
	}
	if len(s.Compactions) != 1 || s.Compactions[0].TurnIndex != 1 || s.Compactions[0].Trigger != "auto" || s.Compactions[0].PreTokens != 168366 {
		t.Errorf("compaction = %+v", s.Compactions)
	}
	// permission-mode は毎 prompt 記録されるので、値が変わったときだけ event にする (auto→plan で 1 回、m2 の直前)
	if len(s.ModeEvents) != 1 || !s.ModeEvents[1] {
		t.Errorf("mode_events = %v, want {1: true}", s.ModeEvents)
	}
}

// --- Load ---

func TestLoadReadsSubagentsAndFiltersByPeriod(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "-home-me-proj")
	u := fxUsage{input: 1, c5: 10, output: 1}
	writeJSONL(t, filepath.Join(proj, "s1.jsonl"), assistantLine("m1", ts0, "claude-opus-5", u))
	writeJSONL(t, filepath.Join(proj, "s1", "subagents", "agent-a1.jsonl"),
		`{"type":"assistant","isSidechain":true,"timestamp":"`+ts1+`","message":{"id":"a1","model":"claude-sonnet-5","usage":{"input_tokens":1,"output_tokens":1},"content":[]}}`)
	writeJSONL(t, filepath.Join(proj, "s1", "subagents", "agent-a1.meta.json"), `{"agentType":"Explore","model":"sonnet"}`)
	writeJSONL(t, filepath.Join(proj, "old.jsonl"), assistantLine("o1", "2026-01-01T00:00:00.000Z", "claude-opus-5", u))
	writeJSONL(t, filepath.Join(proj, "empty.jsonl"), `{"type":"queue-operation"}`)
	other := filepath.Join(root, "-home-me-other")
	writeJSONL(t, filepath.Join(other, "s9.jsonl"), assistantLine("m9", ts0, "claude-opus-5", u))

	got, err := Load(root, Options{Days: 30, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2 (古いものと空のものは除く)", len(got.Sessions))
	}
	var withSub *Session
	for _, s := range got.Sessions {
		if s.SessionID == "s1" {
			withSub = s
		}
	}
	if withSub == nil || len(withSub.Subagents) != 1 {
		t.Fatalf("subagent が読めていない: %+v", withSub)
	}
	sub := withSub.Subagents[0]
	if !sub.Subagent || sub.SessionID != "agent-a1" || sub.AgentType != "Explore" || sub.DisplayName() != "s1 (subagent: Explore)" {
		t.Errorf("subagent = %+v name=%q", sub, sub.DisplayName())
	}

	filtered, err := Load(root, Options{Days: 30, Now: now, Project: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Sessions) != 1 || filtered.Sessions[0].Project != "-home-me-other" {
		t.Errorf("--project の絞り込み: %+v", filtered.Sessions)
	}

	all, err := Load(root, Options{All: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Sessions) != 3 {
		t.Errorf("--all なら古いものも入る: %d", len(all.Sessions))
	}
}

func TestLoadDedupesMessageIDAcrossFiles(t *testing.T) {
	// resume / fork で履歴が別ファイルへ写されると同じ message.id が 2 ファイルに現れる。
	// 2 回目のファイルではその行を数えない (tool_result も付けない)。
	root := t.TempDir()
	proj := filepath.Join(root, "-p")
	u := fxUsage{input: 1, c5: 10, output: 1}
	writeJSONL(t, filepath.Join(proj, "a.jsonl"),
		assistantLine("m1", ts0, "claude-opus-5", u, toolUseBlock("tu1", "Read", map[string]any{"file_path": "/x"})),
		userResultLine(ts0, "tu1", `"0123456789"`),
	)
	writeJSONL(t, filepath.Join(proj, "b.jsonl"),
		assistantLine("m1", ts0, "claude-opus-5", u, toolUseBlock("tu1", "Read", map[string]any{"file_path": "/x"})),
		userResultLine(ts0, "tu1", `"0123456789"`),
		assistantLine("m2", ts1, "claude-opus-5", u),
	)
	got, err := Load(root, Options{All: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 2 {
		t.Fatalf("sessions = %d", len(got.Sessions))
	}
	var a, b *Session
	for _, s := range got.Sessions {
		switch s.SessionID {
		case "a":
			a = s
		case "b":
			b = s
		}
	}
	if len(a.Turns) != 1 || len(a.ToolResults) != 1 {
		t.Errorf("a: turns=%d results=%d", len(a.Turns), len(a.ToolResults))
	}
	if len(b.Turns) != 1 || b.Turns[0].MsgID != "m2" || len(b.ToolResults) != 0 {
		t.Errorf("b は m1 を数えないべき: turns=%+v results=%d", b.Turns, len(b.ToolResults))
	}
	if got.Deduplicated != 1 {
		t.Errorf("deduplicated = %d, want 1", got.Deduplicated)
	}
}

func TestLoadMissingDirIsError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope"), Options{Days: 30, Now: now}); err == nil {
		t.Error("projects ディレクトリが無いならエラー終了する")
	}
}
