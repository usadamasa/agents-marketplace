package main

import (
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/usadamasa/agents-marketplace/plugins/visuals/cmd/cleanshot-capture/internal/displaypower"
	"github.com/usadamasa/agents-marketplace/plugins/visuals/cmd/cleanshot-capture/internal/imgfmt"
	"github.com/usadamasa/agents-marketplace/plugins/visuals/cmd/cleanshot-capture/internal/macwin"
)

// screencaptureBin は macOS 標準のキャプチャコマンド｡PATH 探索を避けて絶対パスで起動する｡
const screencaptureBin = "/usr/sbin/screencapture"

// captureWindowImage は対象ウィンドウを dst へ書き出す｡
//
// CleanShot の capture-area ではなく screencapture を使うのは、ウィンドウ ID を
// 指定するとウィンドウ自身のバッファを撮れるため｡画面の矩形を撮る方式と違って
// 手前に別のウィンドウが重なっていても中身が写り、前面化も要らない｡
func captureWindowImage(target macwin.Window, dst string) error {
	format, err := imgfmt.ForPath(dst)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("出力先ディレクトリを作れません: %w", err)
	}
	// #nosec G204 -- 引数はウィンドウ ID (整数) と ensureWithin 済みの出力先だけ
	cmd := exec.Command(screencaptureBin, screencaptureArgs(target.Number, dst, format)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(describeScreencaptureFailure(target, string(out), displayAsleepNow()))
	}
	// screencapture は撮れなかったときでも終了コード 0 を返すことがある｡
	if _, serr := os.Stat(dst); serr != nil {
		return errors.New(describeScreencaptureFailure(target, string(out), displayAsleepNow()))
	}
	return nil
}

// screencaptureArgs は screencapture の引数を組み立てる｡
// -x はシャッター音を鳴らさない、-o はウィンドウの影を含めない、
// -t は書き出す形式、-l は撮影対象をウィンドウ ID で指定する｡
//
// -t を省くと screencapture は出力先の名前によらず PNG を書くため、
// --out に .jpg を渡しても中身が PNG のファイルができる｡常に明示する｡
func screencaptureArgs(windowNumber int, dst string, format imgfmt.Format) []string {
	return []string{"-x", "-o", "-t", string(format), "-l", strconv.Itoa(windowNumber), dst}
}

// describeScreencaptureFailure は screencapture の失敗を読める文にする｡
// displayAsleep は失敗した時点でディスプレイがスリープしていたか｡
func describeScreencaptureFailure(target macwin.Window, output string, displayAsleep bool) string {
	output = strings.TrimSpace(output)
	if strings.Contains(output, "could not create image") {
		return describeImageFailure(target, displayAsleep)
	}
	if output == "" {
		return fmt.Sprintf("ウィンドウを撮影できませんでした: %s", target)
	}
	return fmt.Sprintf("ウィンドウを撮影できませんでした: %s\n  screencapture: %s", target, output)
}

// describeImageFailure は "could not create image" の原因を切り分けて案内する｡
//
// このメッセージは画面収録の許可が無いときに出るが、許可があっても出る｡権限だと
// 決め打つと、別の原因なのに設定を見に行かせることになる｡切り分けにはウィンドウ名を
// 使う｡kCGWindowName は許可が無いと空になるため、名前が取れていれば、同じ実行の中で
// 許可があった証拠になる｡
//
// 名前が空でも許可があるとは限らない (名前を持たないウィンドウもある) ため、
// そちらは可能性として案内するにとどめる｡
//
// ディスプレイがスリープしているときはフレームバッファが無く、許可があってもウィンドウが
// 出ていても撮れない｡許可の有無だけで分岐すると「描画されていない」と決め打って
// しまうため、先にそちらを見る｡
func describeImageFailure(target macwin.Window, displayAsleep bool) string {
	if displayAsleep {
		return fmt.Sprintf(
			"ウィンドウを撮影できませんでした: %s\n"+
				"ディスプレイがスリープしています｡消えている間はウィンドウ指定でも撮れません｡"+
				"撮り直せば起こしてから撮ります｡それでも点かなければキーボードやマウスに触れて"+
				"ディスプレイを点けてください", target)
	}
	if target.Title != "" {
		return fmt.Sprintf(
			"ウィンドウを撮影できませんでした: %s\n"+
				"ウィンドウ名が読めているので画面収録の許可はあります｡対象がまだ描画されていないか、"+
				"撮影の直前に閉じられた可能性があります｡ウィンドウが出ていることを確かめて"+
				"撮り直してください", target)
	}
	return fmt.Sprintf(
		"ウィンドウを撮影できませんでした: %s\n"+
			"ウィンドウ名も読めていません｡ターミナル (Claude Code を起動しているアプリ) に"+
			"画面収録の許可が無い可能性があります｡システム設定 > プライバシーとセキュリティ > "+
			"画面収録 で許可してから、そのアプリを再起動してください", target)
}

// displayAsleepNow は失敗の診断用にディスプレイの状態を読む｡読めなければ false を返す｡
// ここで判定できないことを理由に元の失敗を隠すと、何が起きたのか分からなくなるため｡
func displayAsleepNow() bool {
	asleep, err := displaypower.Asleep()
	return err == nil && asleep
}

// imageSize は書き出した画像の実ピクセル数を返す｡
// Retina では point の 2 倍になるため、ウィンドウの論理サイズは報告に使えない｡
func imageSize(path string) (int, int, error) {
	f, err := os.Open(path) // #nosec G304 -- path は ensureWithin 済みの出力先
	if err != nil {
		return 0, 0, fmt.Errorf("画像を開けません (%s): %w", path, err)
	}
	defer func() { _ = f.Close() }()

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, fmt.Errorf("画像のサイズを読めません (%s): %w", path, err)
	}
	return cfg.Width, cfg.Height, nil
}
