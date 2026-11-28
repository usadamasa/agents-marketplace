// Package screenlock は画面がロックされているかを IO Registry から読む｡
//
// ロック中にディスプレイの内容を撮るとロック画面が写る｡撮影自体は成功するため、
// 呼び出し側が撮る前に止める必要がある｡どのサブコマンドを止めるかはここでは決めない｡
package screenlock

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ioregBin は IO Registry を読む｡PATH 探索を避けて絶対パスで起動する｡
const ioregBin = "/usr/sbin/ioreg"

// consoleLockedKey は Root 直下にあるコンソールのロック状態｡
const consoleLockedKey = `"IOConsoleLocked" = `

// consoleUsersKey は Root 直下にあるログインセッションの辞書｡1 行で出力される｡
const consoleUsersKey = `"IOConsoleUsers" = `

// screenIsLockedKey は consoleUsersKey の行の中にあるスクリーンロックの状態｡
// 解除中はキーごと現れない (macOS 26.5.2 で実測)｡
const screenIsLockedKey = `"CGSSessionScreenIsLocked"=`

// Locked は画面がロックされていれば true を返す｡
func Locked() (bool, error) {
	// -n Root -d1 で Root のプロパティだけを取る｡-a (plist 出力) は /dev/stdout を
	// パスで開くため sandbox 下では失敗する｡テキスト形式なら経路を選ばない｡
	out, err := exec.Command(ioregBin, "-n", "Root", "-d1").Output()
	if err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			if stderr := strings.TrimSpace(string(ee.Stderr)); stderr != "" {
				return false, fmt.Errorf("ioreg の実行に失敗しました: %s", stderr)
			}
		}
		return false, fmt.Errorf("ioreg の実行に失敗しました: %w", err)
	}
	return parseLocked(out)
}

// parseLocked は ioreg の出力からロック状態を読む｡
//
// 手がかりは 2 つあり、どちらかが Yes ならロック中とみなす｡どちらも見つからない
// ときは判定できないためエラーにする (解除中と決め打つとロック画面を撮ってしまう)｡
// キーの有無ではなく値で判定するのは、解除時にキーが残る macOS があっても
// 誤検知しないようにするため｡
func parseLocked(out []byte) (bool, error) {
	found := false
	locked := false

	for line := range strings.Lines(string(out)) {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, consoleLockedKey):
			found = true
			locked = locked || strings.TrimSpace(strings.TrimPrefix(trimmed, consoleLockedKey)) == "Yes"
		case strings.HasPrefix(trimmed, consoleUsersKey):
			// IOKitDiagnostics のような巨大な辞書にも同じ文字列が現れうるため、
			// 探索は IOConsoleUsers の行に限る｡
			found = true
			locked = locked || sessionValue(trimmed, screenIsLockedKey) == "Yes"
		}
	}

	if !found {
		return false, errors.New("ioreg の出力に IOConsoleLocked も IOConsoleUsers もありません｡画面のロック状態を判定できません")
	}
	return locked, nil
}

// sessionValue は IOConsoleUsers の 1 行から key の値を取り出す｡
// 行は {"キー"=値,"キー"=値} 形式で、値は , か } で終わる｡
func sessionValue(line, key string) string {
	_, rest, ok := strings.Cut(line, key)
	if !ok {
		return ""
	}
	fields := strings.FieldsFunc(rest, func(r rune) bool {
		return r == ',' || r == '}'
	})
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[0])
}
