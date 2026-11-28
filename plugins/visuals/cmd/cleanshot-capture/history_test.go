package main

import (
	"encoding/base64"
	"testing"
	"time"
)

// sampleHistory は実際の mediaHistory から形を写した最小の JSON｡
const sampleHistory = `[
  {"tempURL":null,"appMetadata":{"bundleURL":"file:///Applications/Firefox.app/","bundleID":"org.mozilla.firefox","app":"Firefox","window":"ホーム"},
   "projectURL":null,"creationDate":808233212.416476,"cloudMedia":null,
   "url":"file:///Users/u/Library/Application%20Support/CleanShot/media/media_a/CleanShot-1%402x.jpg"},
  {"tempURL":"file:///tmp/x.mp4","appMetadata":{"bundleURL":"file:///Applications/Finder.app/","bundleID":"com.apple.finder","app":"Finder","window":"src"},
   "projectURL":null,"creationDate":809091173.8766,"cloudMedia":null,
   "url":"file:///Users/u/Library/Application%20Support/CleanShot/media/media_b/CleanShot-2.mp4"}
]`

func TestParseMediaHistory(t *testing.T) {
	raw := []byte(base64.StdEncoding.EncodeToString([]byte(sampleHistory)) + "\n")
	entries, err := parseMediaHistory(raw)
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("件数 = %d, want 2", len(entries))
	}
	if entries[1].AppMetadata.App != "Finder" {
		t.Errorf("App = %q, want Finder", entries[1].AppMetadata.App)
	}
	if entries[0].TempURL != nil {
		t.Errorf("TempURL = %v, want nil", *entries[0].TempURL)
	}
}

func TestParseMediaHistoryInvalid(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "base64 ではない", raw: []byte("!!! not base64 !!!")},
		{name: "base64 だが JSON ではない", raw: []byte(base64.StdEncoding.EncodeToString([]byte("plain text")))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseMediaHistory(tt.raw); err == nil {
				t.Error("エラーを期待したが nil")
			}
		})
	}
}

func TestMediaEntryPath(t *testing.T) {
	raw := []byte(base64.StdEncoding.EncodeToString([]byte(sampleHistory)))
	entries, err := parseMediaHistory(raw)
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	got, err := entries[0].Path()
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	// パーセントエンコードされた空白と @ が戻ること｡
	want := "/Users/u/Library/Application Support/CleanShot/media/media_a/CleanShot-1@2x.jpg"
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestFileURLToPath(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "通常のパス", raw: "file:///tmp/a.png", want: "/tmp/a.png"},
		{name: "日本語を含む", raw: "file:///tmp/%E5%9B%B3.png", want: "/tmp/図.png"},
		{name: "file 以外は拒否", raw: "https://example.com/a.png", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fileURLToPath(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMediaEntryCreatedAt(t *testing.T) {
	// Apple 基準時刻 (2001-01-01 UTC) の 0 秒｡
	e := mediaEntry{CreationDate: 0}
	if got := e.CreatedAt().UTC(); !got.Equal(time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("CreatedAt = %v, want 2001-01-01T00:00:00Z", got)
	}
}

func TestNewestNewEntry(t *testing.T) {
	before := map[string]bool{"file:///old.jpg": true}
	entries := []mediaEntry{
		{URL: "file:///old.jpg", CreationDate: 100},
		{URL: "file:///new1.jpg", CreationDate: 200},
		{URL: "file:///new2.jpg", CreationDate: 300},
		{URL: "file:///rec.mp4", CreationDate: 250},
		{URL: "", CreationDate: 999},
	}
	tests := []struct {
		name   string
		exts   []string
		want   string
		wantOK bool
	}{
		{name: "拡張子を絞らなければ最新", want: "file:///new2.jpg", wantOK: true},
		{name: "録画だけに絞る", exts: recordingExts, want: "file:///rec.mp4", wantOK: true},
		{name: "該当拡張子が無い", exts: []string{".heic"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := newestNewEntry(before, entries, tt.exts)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got.URL != tt.want {
				t.Errorf("URL = %q, want %q", got.URL, tt.want)
			}
		})
	}
}

func TestNewestNewEntryIgnoresKnown(t *testing.T) {
	// 履歴が上限に達して件数が変わらない場合でも、URL 集合の差分で検出できること｡
	before := urlSet([]mediaEntry{{URL: "file:///a.jpg"}, {URL: "file:///b.jpg"}})
	after := []mediaEntry{{URL: "file:///b.jpg", CreationDate: 2}, {URL: "file:///c.jpg", CreationDate: 3}}
	got, ok := newestNewEntry(before, after, nil)
	if !ok {
		t.Fatal("新着を検出できなかった")
	}
	if got.URL != "file:///c.jpg" {
		t.Errorf("URL = %q, want file:///c.jpg", got.URL)
	}
}

func TestHasAnyExt(t *testing.T) {
	tests := []struct {
		name string
		url  string
		exts []string
		want bool
	}{
		{name: "指定なしは常に真", url: "file:///a.jpg", want: true},
		{name: "一致する", url: "file:///a.mp4", exts: recordingExts, want: true},
		{name: "大文字でも一致する", url: "file:///a.GIF", exts: recordingExts, want: true},
		{name: "一致しない", url: "file:///a.jpg", exts: recordingExts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasAnyExt(tt.url, tt.exts); got != tt.want {
				t.Errorf("hasAnyExt = %v, want %v", got, tt.want)
			}
		})
	}
}
