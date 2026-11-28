package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanShotURL(t *testing.T) {
	tests := []struct {
		name    string
		command string
		params  map[string]string
		want    string
	}{
		{
			name:    "パラメータなし",
			command: "capture-fullscreen",
			want:    "cleanshot://capture-fullscreen",
		},
		{
			name:    "矩形とアクション",
			command: "capture-area",
			params:  map[string]string{"x": "10", "y": "20", "width": "300", "height": "400", "action": "copy"},
			want:    "cleanshot://capture-area?action=copy&height=400&width=300&x=10&y=20",
		},
		{
			name:    "空マップはクエリを付けない",
			command: "record-screen",
			params:  map[string]string{},
			want:    "cleanshot://record-screen",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanShotURL(tt.command, tt.params); got != tt.want {
				t.Errorf("URL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRectParams(t *testing.T) {
	got := rectParams(10, 20, 300, 400)
	want := map[string]string{"x": "10", "y": "20", "width": "300", "height": "400"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("キー数 = %d, want %d", len(got), len(want))
	}
}

func TestDefaultFileName(t *testing.T) {
	now := time.Date(2026, 8, 22, 20, 41, 44, 0, time.UTC)
	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "jpg を引き継ぐ", src: "/x/CleanShot-1@2x.jpg", want: "cleanshot-20260822-204144.jpg"},
		{name: "mp4 を引き継ぐ", src: "/x/CleanShot-1.mp4", want: "cleanshot-20260822-204144.mp4"},
		{name: "拡張子が無ければ png", src: "/x/CleanShot", want: "cleanshot-20260822-204144.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultFileName(tt.src, now); got != tt.want {
				t.Errorf("名前 = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveOutPath(t *testing.T) {
	now := time.Date(2026, 8, 22, 20, 41, 44, 0, time.UTC)
	const src = "/x/CleanShot-1@2x.jpg"
	dir := t.TempDir()

	tests := []struct {
		name string
		out  string
		want string
	}{
		{name: "省略時は既定ディレクトリ", out: "", want: filepath.Join(dir, "cleanshot-20260822-204144.jpg")},
		{name: "ファイル名指定はそのまま", out: "./tmp/shot.png", want: "./tmp/shot.png"},
		{name: "末尾スラッシュはディレクトリ扱い", out: "out/", want: "out/cleanshot-20260822-204144.jpg"},
		{name: "既存ディレクトリもディレクトリ扱い", out: dir, want: filepath.Join(dir, "cleanshot-20260822-204144.jpg")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveOutPath(tt.out, dir, src, now); got != tt.want {
				t.Errorf("出力先 = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsureWithin(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "tmp"), 0o750); err != nil {
		t.Fatalf("準備に失敗: %v", err)
	}
	// t.TempDir() 自体が /var → /private/var の symlink 配下に返るため、
	// 期待値も解決後のパスで比べる｡
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("準備に失敗: %v", err)
	}

	outside := t.TempDir()
	escape := filepath.Join(cwd, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatalf("準備に失敗: %v", err)
	}

	tests := []struct {
		name    string
		dst     string
		want    string
		wantErr bool
	}{
		{name: "相対パスは cwd 起点", dst: "tmp/a.png", want: filepath.Join(root, "tmp", "a.png")},
		{name: "存在しない中間ディレクトリも通す", dst: "tmp/nested/deep/a.png", want: filepath.Join(root, "tmp", "nested", "deep", "a.png")},
		{name: "cwd 配下の絶対パス", dst: filepath.Join(cwd, "a.png"), want: filepath.Join(root, "a.png")},
		{name: "cwd の外は拒否", dst: filepath.Join(outside, "a.png"), wantErr: true},
		{name: "相対パスでの脱出も拒否", dst: "../a.png", wantErr: true},
		{name: "symlink 経由の脱出も拒否", dst: "escape/a.png", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ensureWithin(tt.dst, cwd)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("パス = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveOutPathAvoidsCollision(t *testing.T) {
	now := time.Date(2026, 8, 22, 20, 41, 44, 0, time.UTC)
	const src = "/x/CleanShot-1@2x.jpg"
	dir := t.TempDir()

	first := resolveOutPath("", dir, src, now)
	if err := os.WriteFile(first, []byte("x"), 0o600); err != nil {
		t.Fatalf("準備に失敗: %v", err)
	}
	second := resolveOutPath("", dir, src, now)
	if second == first {
		t.Fatalf("同じ秒の 2 枚目が 1 枚目を上書きする: %s", second)
	}
	if got := filepath.Base(second); got != "cleanshot-20260822-204144-2.jpg" {
		t.Errorf("名前 = %q, want cleanshot-20260822-204144-2.jpg", got)
	}
}
