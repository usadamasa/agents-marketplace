package imgfmt

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    Format
		wantErr bool
	}{
		{name: "png", path: "./tmp/shot.png", want: PNG},
		{name: "jpg", path: "./tmp/shot.jpg", want: JPEG},
		{name: "jpeg", path: "./tmp/shot.jpeg", want: JPEG},
		{name: "大文字も同じ形式", path: "./tmp/shot.PNG", want: PNG},
		{name: "拡張子が無ければ png", path: "./tmp/shot", want: PNG},
		{name: "録画の拡張子は静止画にできない", path: "./tmp/shot.mp4", wantErr: true},
		{name: "画像でない拡張子はエラー", path: "./tmp/shot.txt", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ForPath(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("エラーを期待したが %q が返った", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if got != tt.want {
				t.Errorf("形式 = %q, want %q", got, tt.want)
			}
		})
	}
}

// samplePNG はテスト用の PNG バイト列を返す｡
func samplePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 12, 8))
	img.Set(0, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("PNG を書けません: %v", err)
	}
	return buf.Bytes()
}

// sampleJPEG はテスト用の JPEG バイト列を返す｡
func sampleJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 12, 8))
	img.Set(0, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("JPEG を書けません: %v", err)
	}
	return buf.Bytes()
}

// writeTemp はバイト列を一時ファイルへ書いてパスを返す｡
func writeTemp(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("ファイルを書けません: %v", err)
	}
	return path
}

// formatOnDisk は書き出したファイルの実形式を返す｡
// 拡張子ではなく中身を見るので、名前と実体の食い違いを検出できる｡
func formatOnDisk(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("ファイルを開けません: %v", err)
	}
	defer func() { _ = f.Close() }()
	_, format, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("画像を読めません (%s): %v", path, err)
	}
	return format
}

func TestWriteAs(t *testing.T) {
	dir := t.TempDir()
	pngSrc := writeTemp(t, dir, "src.png", samplePNG(t))
	jpegSrc := writeTemp(t, dir, "src.jpg", sampleJPEG(t))
	// CleanShot が .jpg の名前で吐いたファイルでも、判定は中身で行う｡
	mislabeled := writeTemp(t, dir, "mislabeled.jpg", samplePNG(t))

	tests := []struct {
		name string
		src  string
		dst  string
		want string
	}{
		{name: "png を png へ", src: pngSrc, dst: "out1.png", want: "png"},
		{name: "jpeg を jpeg へ", src: jpegSrc, dst: "out2.jpg", want: "jpeg"},
		{name: "jpeg を png へ変換する", src: jpegSrc, dst: "out3.png", want: "png"},
		{name: "png を jpeg へ変換する", src: pngSrc, dst: "out4.jpg", want: "jpeg"},
		{name: "拡張子が中身と食い違う入力も中身で判定する", src: mislabeled, dst: "out5.jpg", want: "jpeg"},
		{name: "拡張子が無ければ png で書く", src: jpegSrc, dst: "out6", want: "png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := filepath.Join(dir, tt.dst)
			if err := WriteAs(tt.src, dst); err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if got := formatOnDisk(t, dst); got != tt.want {
				t.Errorf("実形式 = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("形式が同じならバイト列をそのまま渡す", func(t *testing.T) {
		dst := filepath.Join(dir, "same.png")
		if err := WriteAs(pngSrc, dst); err != nil {
			t.Fatalf("予期しないエラー: %v", err)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("読めません: %v", err)
		}
		if !bytes.Equal(got, samplePNG(t)) {
			t.Error("再エンコードされています (バイト列が元と違う)")
		}
	})

	t.Run("jpeg から変換した png は 8bit で書く", func(t *testing.T) {
		dst := filepath.Join(dir, "depth.png")
		if err := WriteAs(jpegSrc, dst); err != nil {
			t.Fatalf("予期しないエラー: %v", err)
		}
		f, err := os.Open(dst)
		if err != nil {
			t.Fatalf("開けません: %v", err)
		}
		defer func() { _ = f.Close() }()
		cfg, _, err := image.DecodeConfig(f)
		if err != nil {
			t.Fatalf("画像を読めません: %v", err)
		}
		// JPEG のデコード結果は YCbCr で、png.Encode はこれを 16bit RGB で書く｡
		// 元が 8bit なので情報は増えず、ファイルだけが倍になる｡
		if cfg.ColorModel == color.RGBA64Model || cfg.ColorModel == color.NRGBA64Model {
			t.Error("16bit で書かれています")
		}
	})

	t.Run("出力先が画像の拡張子でなければエラー", func(t *testing.T) {
		if err := WriteAs(pngSrc, filepath.Join(dir, "out.txt")); err == nil {
			t.Error("エラーを期待したが nil")
		}
	})

	t.Run("画像でない入力はエラー", func(t *testing.T) {
		src := writeTemp(t, dir, "notimage.png", []byte("not an image"))
		if err := WriteAs(src, filepath.Join(dir, "out7.png")); err == nil {
			t.Error("エラーを期待したが nil")
		}
	})
}

func TestCopyRecording(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, dir, "clip.mp4", []byte("fake mp4"))

	t.Run("拡張子が同じならコピーする", func(t *testing.T) {
		dst := filepath.Join(dir, "out.mp4")
		if err := CopyRecording(src, dst); err != nil {
			t.Fatalf("予期しないエラー: %v", err)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("読めません: %v", err)
		}
		if string(got) != "fake mp4" {
			t.Errorf("中身 = %q", got)
		}
	})

	t.Run("拡張子が違えばエラーにし、元ファイルの場所を伝える", func(t *testing.T) {
		err := CopyRecording(src, filepath.Join(dir, "out.gif"))
		if err == nil {
			t.Fatal("エラーを期待したが nil")
		}
		// 撮り直しが高くつくため、録画の実体がどこに残っているかを必ず示す｡
		if !strings.Contains(err.Error(), src) {
			t.Errorf("メッセージに元ファイルのパスがありません: %v", err)
		}
	})
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, dir, "src.jpg", []byte("image-bytes"))

	t.Run("存在しない中間ディレクトリを作る", func(t *testing.T) {
		dst := filepath.Join(dir, "nested", "out.jpg")
		if err := copyFile(src, dst); err != nil {
			t.Fatalf("予期しないエラー: %v", err)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("出力を読めない: %v", err)
		}
		if string(got) != "image-bytes" {
			t.Errorf("内容 = %q, want image-bytes", got)
		}
		// 元ファイルは履歴として残るのでコピー元も残っていること｡
		if _, err := os.Stat(src); err != nil {
			t.Errorf("コピー元が失われた: %v", err)
		}
	})

	t.Run("コピー元が無ければエラー", func(t *testing.T) {
		if err := copyFile(filepath.Join(dir, "missing.jpg"), filepath.Join(dir, "out2.jpg")); err == nil {
			t.Error("エラーを期待したが nil")
		}
	})
}
