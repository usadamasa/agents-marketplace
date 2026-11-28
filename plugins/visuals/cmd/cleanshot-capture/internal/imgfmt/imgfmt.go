// Package imgfmt は撮影物を --out の拡張子が指す形式で書き出す｡
//
// 静止画は PNG と JPEG を相互に変換し、録画は変換せずにコピーする｡撮り方
// (screencapture / CleanShot) にも、出力先の検証にも依存しない｡
package imgfmt

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Format は静止画の出力形式｡値は screencapture の -t が受け付ける名前｡
type Format string

const (
	PNG  Format = "png"
	JPEG Format = "jpg"
)

// jpegQuality は PNG から JPEG へ変換するときの品質｡
const jpegQuality = 90

// ForPath は出力先の拡張子から静止画の形式を決める｡
// 拡張子が無いときは screencapture と CleanShot の既定に合わせて PNG とする｡
func ForPath(path string) (Format, error) {
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".png", "":
		return PNG, nil
	case ".jpg", ".jpeg":
		return JPEG, nil
	default:
		return "", fmt.Errorf(
			"静止画の出力先にできない拡張子です (%s): --out には .png か .jpg を指定してください", ext)
	}
}

// WriteAs は src の画像を dst の拡張子が指す形式で書き出す｡
//
// CleanShot が書き出す形式は本体の screenshotFormat 設定で決まり、URL scheme から
// 指定できない｡--out の拡張子を要求された形式とみなし、食い違うときだけ変換する｡
// 形式が一致していればバイト列をそのまま渡し、無駄な再エンコードをしない｡
func WriteAs(src, dst string) error {
	want, err := ForPath(dst)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src) // #nosec G304 -- src は CleanShot の履歴が返したパス
	if err != nil {
		return fmt.Errorf("キャプチャファイルを読めません (%s): %w", src, err)
	}
	// 形式は拡張子ではなく中身で判定する｡CleanShot は @2x.jpg のような名前を付けるが、
	// 名前と実体が食い違っていても正しく変換できるようにするため｡
	img, decoded, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("キャプチャファイルを画像として読めません (%s): %w", src, err)
	}
	if decodedFormat(decoded) == want {
		return writeOutput(dst, data)
	}
	var buf bytes.Buffer
	if err := encodeImage(&buf, img, want); err != nil {
		return err
	}
	return writeOutput(dst, buf.Bytes())
}

// decodedFormat は image.Decode が返す形式名を Format へ写す｡
// image/jpeg は "jpeg" を名乗るが、screencapture の -t は "jpg" を取る｡
func decodedFormat(decoded string) Format {
	if decoded == "jpeg" {
		return JPEG
	}
	return Format(decoded)
}

// encodeImage は img を format で書き出す｡
func encodeImage(w io.Writer, img image.Image, format Format) error {
	if format == JPEG {
		if err := jpeg.Encode(w, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return fmt.Errorf("JPEG に変換できません: %w", err)
		}
		return nil
	}
	if err := png.Encode(w, to8Bit(img)); err != nil {
		return fmt.Errorf("PNG に変換できません: %w", err)
	}
	return nil
}

// to8Bit は YCbCr の画像を 8bit RGBA へ写す｡
//
// JPEG のデコード結果は YCbCr で、png.Encode はこれを 16bit RGB として書く｡
// 元が 8bit なので階調は増えず、ファイルサイズだけが倍になる｡
func to8Bit(img image.Image) image.Image {
	if _, ok := img.(*image.YCbCr); !ok {
		return img
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, img.Bounds(), img, img.Bounds().Min, draw.Src)
	return rgba
}

// CopyRecording は録画ファイルを dst へコピーする｡
//
// 静止画と違い mp4 と gif は相互に変換できない｡どちらになるかは CleanShot の録画
// ツール UI の設定で決まるため、--out の拡張子と食い違ったらエラーにする｡撮り直しは
// ユーザーの操作と時間を要するので、録画の実体がどこに残っているかを必ず伝える｡
func CopyRecording(src, dst string) error {
	if !strings.EqualFold(filepath.Ext(src), filepath.Ext(dst)) {
		return fmt.Errorf(
			"録画の形式を変換できません: CleanShot は %s を書き出しましたが --out は %s です｡"+
				"録画はこの場所に残っています: %s",
			filepath.Ext(src), filepath.Ext(dst), src)
	}
	return copyFile(src, dst)
}

// copyFile は CleanShot が書いたファイルを出力先へ複製する｡
// 元ファイルは CleanShot の履歴として残るため移動ではなくコピーする｡
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src) // #nosec G304 -- src は CleanShot の履歴が返したパス
	if err != nil {
		return fmt.Errorf("キャプチャファイルを読めません (%s): %w", src, err)
	}
	return writeOutput(dst, data)
}

// writeOutput は data を dst へ書き出す｡途中のディレクトリは作る｡
func writeOutput(dst string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("出力先ディレクトリを作れません: %w", err)
	}
	// #nosec G703 G306 -- dst は呼び出し側が作業ディレクトリ配下に限定済み
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return fmt.Errorf("出力先へ書き込めません (%s): %w", dst, err)
	}
	return nil
}
