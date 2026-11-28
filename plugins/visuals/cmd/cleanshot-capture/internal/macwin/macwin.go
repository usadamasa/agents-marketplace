// Package macwin は macOS の画面上にあるウィンドウを列挙し、選び、前面に出す｡
//
// CleanShot にも screencapture にも依存しない｡撮り方を決めるのは呼び出し側で、
// このパッケージは「どのウィンドウが、どこに、どの重なり順で出ているか」だけを扱う｡
package macwin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// osascriptBin は JXA を実行する｡PATH 探索を避けて絶対パスで起動する｡
const osascriptBin = "/usr/bin/osascript"

// minWindowSize より小さいウィンドウは列挙から除く｡ドロップシャドウ用の
// ダミーウィンドウや 1x1 のプレースホルダを拾わないため｡
const minWindowSize = 40

// listWindowsJXA は画面上のウィンドウを前面順に返す JXA｡
// CGWindowListCopyWindowInfo は Accessibility も Apple events も要求しない｡
// オプション 1 は kCGWindowListOptionOnScreenOnly、16 は kCGWindowListExcludeDesktopElements｡
const listWindowsJXA = `
ObjC.import('CoreGraphics');
ObjC.import('Foundation');
var raw = ObjC.deepUnwrap(ObjC.castRefToObject($.CGWindowListCopyWindowInfo(1 | 16, 0)));
JSON.stringify(raw.map(function (w) {
  var b = w.kCGWindowBounds || {};
  return {
    owner: w.kCGWindowOwnerName || '',
    title: w.kCGWindowName || '',
    pid: w.kCGWindowOwnerPID || 0,
    number: w.kCGWindowNumber || 0,
    layer: w.kCGWindowLayer,
    x: b.X || 0,
    y: b.Y || 0,
    width: b.Width || 0,
    height: b.Height || 0
  };
}));
`

// activateJXA は pid のアプリを前面に出す｡NSRunningApplication 経由なので
// System Events (Accessibility) を経由せず、TCC の追加許可が要らない｡
// 引数 2 は NSApplicationActivateIgnoringOtherApps｡
const activateJXA = `
ObjC.import('AppKit');
var app = $.NSRunningApplication.runningApplicationWithProcessIdentifier(%d);
if (!app) { throw new Error('pid %[1]d のアプリが見つかりません'); }
JSON.stringify({ activated: app.activateWithOptions(2) });
`

// mainDisplayJXA は主ディスプレイの矩形を返す｡NSScreen は GUI セッションに
// 属さないプロセスから空を返すことがあるため CoreGraphics 側を使う｡
const mainDisplayJXA = `
ObjC.import('CoreGraphics');
var b = $.CGDisplayBounds($.CGMainDisplayID());
JSON.stringify({ width: b.size.width, height: b.size.height });
`

// displaySize は主ディスプレイの論理サイズ (point)｡
type displaySize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Window は CGWindowList が返す 1 ウィンドウ分の情報｡
type Window struct {
	Owner  string `json:"owner"`
	Title  string `json:"title"`
	PID    int    `json:"pid"`
	Number int    `json:"number"`
	Layer  int    `json:"layer"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func (w Window) String() string {
	return fmt.Sprintf("%s [%s] %dx%d @ (%d,%d)", w.Owner, w.Title, w.Width, w.Height, w.X, w.Y)
}

// runJXA は JavaScript for Automation のソースを実行して stdout を返す｡
func runJXA(src string) ([]byte, error) {
	cmd := exec.Command(osascriptBin, "-l", "JavaScript", "-e", src) // #nosec G204 -- src は本ファイル内の定数から組み立てる
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			if stderr := strings.TrimSpace(string(ee.Stderr)); stderr != "" {
				return nil, fmt.Errorf("osascript の実行に失敗しました: %s", stderr)
			}
		}
		return nil, fmt.Errorf("osascript の実行に失敗しました: %w", err)
	}
	return out, nil
}

// List は画面上のウィンドウを前面順に返す｡
func List() ([]Window, error) {
	out, err := runJXA(listWindowsJXA)
	if err != nil {
		return nil, err
	}
	var raw []Window
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("ウィンドウ一覧の JSON を解析できません: %w", err)
	}
	return filterWindows(raw), nil
}

// filterWindows は通常のアプリケーションウィンドウだけを残す｡
// レイヤー 0 以外はメニューバーやシステム UI で、キャプチャ対象にならない｡
func filterWindows(all []Window) []Window {
	kept := make([]Window, 0, len(all))
	for _, w := range all {
		if w.Layer != 0 || w.Owner == "" {
			continue
		}
		if w.Width < minWindowSize || w.Height < minWindowSize {
			continue
		}
		kept = append(kept, w)
	}
	return kept
}

// Select は app / title の条件に合うウィンドウを 1 つ選ぶ｡
// candidates は前面順に並んでいる前提で、index を省いたときは最前面を返す｡
// owner の完全一致 (大文字小文字を無視) を部分一致より優先する｡
func Select(candidates []Window, app, title string, index int) (Window, error) {
	matched := matchWindows(candidates, app, title)
	if len(matched) == 0 {
		return Window{}, fmt.Errorf("条件に合うウィンドウがありません (app=%q title=%q)\n%s",
			app, title, availableSummary(candidates))
	}
	if index < 0 || index >= len(matched) {
		return Window{}, fmt.Errorf("--index %d は範囲外です (該当ウィンドウは %d 件)", index, len(matched))
	}
	return matched[index], nil
}

// matchWindows は app / title でウィンドウを絞り込む｡どちらも空なら全件返す｡
func matchWindows(candidates []Window, app, title string) []Window {
	var exact, partial []Window
	for _, w := range candidates {
		if title != "" && !containsFold(w.Title, title) {
			continue
		}
		switch {
		case app == "":
			partial = append(partial, w)
		case strings.EqualFold(w.Owner, app):
			exact = append(exact, w)
		case containsFold(w.Owner, app):
			partial = append(partial, w)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

// availableSummary は選択に失敗したときに出す、いま開いているアプリの一覧｡
func availableSummary(candidates []Window) string {
	if len(candidates) == 0 {
		return "画面上にアプリケーションウィンドウがありません"
	}
	seen := map[string]bool{}
	owners := make([]string, 0, len(candidates))
	for _, w := range candidates {
		if !seen[w.Owner] {
			seen[w.Owner] = true
			owners = append(owners, w.Owner)
		}
	}
	sort.Strings(owners)
	return "いま開いているアプリ: " + strings.Join(owners, ", ")
}

// Describe は複数のウィンドウを 1 行にまとめる｡
func Describe(windows []Window) string {
	descs := make([]string, 0, len(windows))
	for _, w := range windows {
		descs = append(descs, w.String())
	}
	return strings.Join(descs, " / ")
}

// Activate は pid のアプリを前面に出す｡
func Activate(pid int) error {
	_, err := runJXA(fmt.Sprintf(activateJXA, pid))
	return err
}

// MainDisplayHeight は主ディスプレイの論理的な高さ (point) を返す｡
// 上原点と下原点を変換する FlipY に渡すためだけに使う｡
func MainDisplayHeight() (int, error) {
	out, err := runJXA(mainDisplayJXA)
	if err != nil {
		return 0, err
	}
	var d displaySize
	if err := json.Unmarshal(out, &d); err != nil {
		return 0, fmt.Errorf("ディスプレイ情報の JSON を解析できません: %w", err)
	}
	if d.Height <= 0 {
		return 0, fmt.Errorf("主ディスプレイの高さを取得できませんでした")
	}
	return d.Height, nil
}

// FlipY は CGWindowList の上原点 Y を CleanShot が期待する下原点 Y に直す｡
// CGWindowListCopyWindowInfo は主ディスプレイ左上を原点とするのに対し、
// cleanshot://capture-area の y は左下を原点として数える (実測)｡
func FlipY(y, height, screenHeight int) int {
	return screenHeight - (y + height)
}

// Occluders は target より手前にあって矩形が重なるウィンドウを返す｡
// 画面の矩形を撮る方式ではウィンドウのバッファを読まないため、遮蔽されたまま撮ると
// 手前のウィンドウが写る｡前面化はアプリ単位でしか効かず、同じアプリの別ウィンドウが
// 手前に残ることがあるのでここで検出する｡candidates は前面順に並んでいる前提｡
func Occluders(candidates []Window, target Window) []Window {
	var found []Window
	for _, w := range candidates {
		if w.Number == target.Number {
			break // target まで到達したら、それ以降は背面なので見なくてよい
		}
		if intersects(w, target) {
			found = append(found, w)
		}
	}
	return found
}

// intersects は 2 つのウィンドウ矩形が重なるかを返す｡
func intersects(a, b Window) bool {
	return a.X < b.X+b.Width && b.X < a.X+a.Width &&
		a.Y < b.Y+b.Height && b.Y < a.Y+a.Height
}
