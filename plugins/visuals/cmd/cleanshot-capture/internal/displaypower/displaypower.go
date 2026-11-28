// Package displaypower はディスプレイがスリープしているかを読み、必要なら起こす｡
//
// ディスプレイが消えている間はフレームバッファが無く、screencapture はウィンドウ ID を
// 指定しても画像を作れない｡画面ロックとは別の状態で、ロックだけならディスプレイが
// 点いている限りウィンドウは撮れる｡どのサブコマンドで起こすかはここでは決めない｡
package displaypower

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// pmsetBin は電源管理のログを読む｡PATH 探索を避けて絶対パスで起動する｡
const pmsetBin = "/usr/bin/pmset"

// caffeinateBin はユーザーの操作があったことを宣言してディスプレイを起こす｡
const caffeinateBin = "/usr/bin/caffeinate"

// notificationDomain はディスプレイの点灯・消灯が記録される pmset -g log の Domain 列｡
const notificationDomain = "Notification"

// 点灯・消灯のメッセージ｡Assertions 列にも "display" を含む行が流れるため、
// Domain 列と組で見る｡
const (
	displayOnMessage  = "Display is turned on"
	displayOffMessage = "Display is turned off"
)

// pollInterval は起こした後に状態を読み直す間隔｡pmset -g log 自体に 1 秒強かかる｡
const pollInterval = 500 * time.Millisecond

// Asleep はディスプレイがスリープしていれば true を返す｡
//
// IO Registry の framebuffer (AppleCLCD2) の IOPowerManagement はディスプレイが消えても
// CurrentPowerState が変わらず (macOS 26.5 で実測)、CGDisplayIsAsleep は JXA から型を
// 解決できない｡pmset -g log に残る通知が唯一の確かな手がかりになる｡
func Asleep() (bool, error) {
	out, err := exec.Command(pmsetBin, "-g", "log").Output()
	if err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			if stderr := strings.TrimSpace(string(ee.Stderr)); stderr != "" {
				return false, fmt.Errorf("pmset の実行に失敗しました: %s", stderr)
			}
		}
		return false, fmt.Errorf("pmset の実行に失敗しました: %w", err)
	}
	return parseAsleep(out)
}

// parseAsleep は pmset -g log の出力から、最後に記録された点灯・消灯を読む｡
//
// 行は "<日時> <Domain>\t<メッセージ>\t" のタブ区切りで、Domain 列とメッセージ列は
// 空白で右詰めされている｡通知が 1 つも無いときは判定できないためエラーにする
// (点いていると決め打つと、消えたまま撮りに行って的外れな失敗になる)｡
func parseAsleep(out []byte) (bool, error) {
	found := false
	asleep := false

	for line := range strings.Lines(string(out)) {
		domain, message, ok := strings.Cut(line, "\t")
		if !ok || !strings.HasSuffix(strings.TrimRight(domain, " "), notificationDomain) {
			continue
		}
		message, _, _ = strings.Cut(message, "\t")
		switch strings.TrimSpace(message) {
		case displayOnMessage:
			found, asleep = true, false
		case displayOffMessage:
			found, asleep = true, true
		}
	}

	if !found {
		return false, errors.New("pmset -g log にディスプレイの点灯・消灯の記録がありません｡ディスプレイの状態を判定できません")
	}
	return asleep, nil
}

// Wake はディスプレイを起こし、点くまで待つ｡
//
// caffeinate -u はユーザーの操作があったことを宣言する｡これでディスプレイが点き、
// アイドルタイマーも巻き戻る｡宣言は -t で 1 秒だけ保てば足りる (実測では宣言と同じ秒に
// 点灯が記録され、0.5 秒後には撮れた)｡点灯が記録されるまで timeout を上限に読み直す｡
func Wake(timeout time.Duration) error {
	if out, err := exec.Command(caffeinateBin, "-u", "-t", "1").CombinedOutput(); err != nil {
		return fmt.Errorf("caffeinate の実行に失敗しました: %w: %s", err, strings.TrimSpace(string(out)))
	}
	deadline := time.Now().Add(timeout)
	for {
		asleep, err := Asleep()
		if err != nil {
			return err
		}
		if !asleep {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("ディスプレイを起こせませんでした (%s 待っても消えたまま)｡キーボードやマウスに触れて点けてください", timeout)
		}
		time.Sleep(pollInterval)
	}
}
