package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func assistantLine(id, ts string, cache1h int64) string {
	return `{"type":"assistant","timestamp":"` + ts + `","message":{"id":"` + id + `","model":"claude-opus-5","usage":{"input_tokens":1,"cache_creation_input_tokens":` + itoa(cache1h) + `,"cache_read_input_tokens":0,"output_tokens":10,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":` + itoa(cache1h) + `}},"content":[]}}`
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestRunEndToEnd(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "-home-me-proj")
	if err := os.MkdirAll(proj, 0o750); err != nil {
		t.Fatal(err)
	}
	// 1h TTL を超える 2 時間の gap の後に 50k を書き直す → R1
	lines := assistantLine("m1", "2026-10-01T10:00:00.000Z", 50_000) + "\n" + assistantLine("m2", "2026-10-01T12:00:00.000Z", 50_000) + "\n"
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := run(&out, runOptions{
		ProjectsDir: root, Days: 30, Top: 10,
		Now: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	var r report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("stdout が JSON ではない: %v\n%s", err, out.String())
	}
	if r.Sessions != 1 || len(r.Findings) != 1 || r.Findings[0].Rule != "R1" {
		t.Errorf("report = %+v", r)
	}
}

func TestRunRejectsMissingPricingFile(t *testing.T) {
	err := run(&bytes.Buffer{}, runOptions{ProjectsDir: t.TempDir(), Days: 30, Top: 10, PricingPath: filepath.Join(t.TempDir(), "none.json"), Now: time.Now()})
	if err == nil {
		t.Error("無い pricing ファイルはエラー")
	}
}

func TestRunRejectsUnknownRule(t *testing.T) {
	err := run(&bytes.Buffer{}, runOptions{ProjectsDir: t.TempDir(), Days: 30, Top: 10, Rule: "R9", Now: time.Now()})
	if err == nil {
		t.Error("R1〜R6 以外の --rule はエラー")
	}
}

func TestRunRejectsBadThreshold(t *testing.T) {
	err := run(&bytes.Buffer{}, runOptions{ProjectsDir: t.TempDir(), Days: 30, Top: 10, Thresholds: []string{"nope=1"}, Now: time.Now()})
	if err == nil {
		t.Error("未知の閾値はエラー")
	}
}
