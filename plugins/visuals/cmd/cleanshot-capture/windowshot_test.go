package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usadamasa/agents-config/cleanshot-capture/internal/imgfmt"
	"github.com/usadamasa/agents-config/cleanshot-capture/internal/macwin"
)

func TestScreencaptureArgs(t *testing.T) {
	// screencapture は -t が無いと出力先の名前によらず PNG を書く｡
	// 拡張子どおりの形式にするため、常に -t で明示する｡
	tests := []struct {
		name   string
		format imgfmt.Format
		dst    string
		want   []string
	}{
		{
			name:   "png",
			format: imgfmt.PNG,
			dst:    "/work/tmp/out.png",
			want:   []string{"-x", "-o", "-t", "png", "-l", "17992", "/work/tmp/out.png"},
		},
		{
			name:   "jpg",
			format: imgfmt.JPEG,
			dst:    "/work/tmp/out.jpg",
			want:   []string{"-x", "-o", "-t", "jpg", "-l", "17992", "/work/tmp/out.jpg"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := screencaptureArgs(17992, tt.dst, tt.format)
			if len(got) != len(tt.want) {
				t.Fatalf("引数 = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDescribeScreencaptureFailure(t *testing.T) {
	target := macwin.Window{Owner: "Finder", Title: "src", Width: 992, Height: 599}
	unnamed := macwin.Window{Owner: "Finder", Width: 992, Height: 599}

	// kCGWindowName は画面収録の許可が無いと空になる｡名前が取れているなら、同じ実行の
	// 中で許可があった証拠になるため、設定を見に行かせてはいけない｡
	t.Run("ウィンドウ名が読めていれば設定を案内しない", func(t *testing.T) {
		got := describeScreencaptureFailure(target, "could not create image from window", false)
		if strings.Contains(got, "システム設定") {
			t.Errorf("許可があるのに設定の案内が出ています: %q", got)
		}
		if !strings.Contains(got, "Finder") {
			t.Errorf("メッセージに対象ウィンドウがありません: %q", got)
		}
	})

	t.Run("ウィンドウ名も読めていなければ画面収録の許可を案内する", func(t *testing.T) {
		got := describeScreencaptureFailure(unnamed, "could not create image from window", false)
		if !strings.Contains(got, "システム設定") {
			t.Errorf("メッセージに画面収録の案内がありません: %q", got)
		}
		if !strings.Contains(got, "Finder") {
			t.Errorf("メッセージに対象ウィンドウがありません: %q", got)
		}
	})

	// ディスプレイがスリープしているとフレームバッファが無く、許可があってもウィンドウが
	// 出ていても撮れない｡許可や描画を疑わせる案内はどちらも的外れになる (#305)｡
	t.Run("ディスプレイがスリープ中ならそれだけを伝える", func(t *testing.T) {
		for name, w := range map[string]macwin.Window{"名前あり": target, "名前なし": unnamed} {
			got := describeScreencaptureFailure(w, "could not create image from window", true)
			if !strings.Contains(got, "ディスプレイ") {
				t.Errorf("[%s] ディスプレイスリープの案内がありません: %q", name, got)
			}
			if strings.Contains(got, "システム設定") || strings.Contains(got, "描画") {
				t.Errorf("[%s] 無関係な案内が混ざっています: %q", name, got)
			}
			if !strings.Contains(got, "Finder") {
				t.Errorf("[%s] メッセージに対象ウィンドウがありません: %q", name, got)
			}
		}
	})

	t.Run("それ以外は screencapture の出力をそのまま伝える", func(t *testing.T) {
		got := describeScreencaptureFailure(target, "invalid display specified", false)
		if !strings.Contains(got, "invalid display specified") {
			t.Errorf("screencapture の出力が含まれていません: %q", got)
		}
		if strings.Contains(got, "画面収録") {
			t.Errorf("無関係な権限案内が混ざっています: %q", got)
		}
	})
}

func TestImageSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.png")

	// Retina では point の 2 倍のピクセル数になる｡実寸を報告することの確認も兼ねる｡
	img := image.NewRGBA(image.Rect(0, 0, 120, 80))
	img.Set(0, 0, color.RGBA{R: 1, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("PNG を書けません: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("ファイルを書けません: %v", err)
	}

	w, h, err := imageSize(path)
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	if w != 120 || h != 80 {
		t.Errorf("サイズ = %dx%d, want 120x80", w, h)
	}

	t.Run("読めないファイルはエラー", func(t *testing.T) {
		if _, _, err := imageSize(filepath.Join(dir, "missing.png")); err == nil {
			t.Error("エラーを期待したが nil")
		}
	})
}
