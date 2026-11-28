package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/usadamasa/agents-config/internal/pathutil"
)

const openBin = "/usr/bin/open"

// 画像の拡張子は CleanShot の screenshotFormat 設定 (jpg / png) で変わるため、
// 出力側では履歴が返した実ファイルの拡張子をそのまま使う｡
var (
	// recordingExts は録画の待ち受けで受け付ける拡張子｡
	recordingExts = []string{".mp4", ".gif", ".mov"}
	// pollInterval は履歴を見に行く間隔｡
	pollInterval = 200 * time.Millisecond
)

// cleanShotURL は cleanshot:// の URL を組み立てる｡
func cleanShotURL(command string, params map[string]string) string {
	u := url.URL{Scheme: "cleanshot", Host: command}
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// rectParams は矩形指定のパラメータを組み立てる｡
func rectParams(x, y, width, height int) map[string]string {
	return map[string]string{
		"x":      strconv.Itoa(x),
		"y":      strconv.Itoa(y),
		"width":  strconv.Itoa(width),
		"height": strconv.Itoa(height),
	}
}

// openURL は URL scheme を CleanShot X に渡す｡
// background が true なら -g を付けて CleanShot を前面に出さない｡撮影対象の
// ウィンドウを前面に保つため、静止画の撮影では必ず background で開く｡
func openURL(target string, background bool) error {
	args := []string{}
	if background {
		args = append(args, "-g")
	}
	args = append(args, target)
	cmd := exec.Command(openBin, args...) // #nosec G204 -- target は cleanShotURL が組み立てた cleanshot:// URL
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("CleanShot X に %s を渡せませんでした: %w", target, err)
	}
	return nil
}

// waitForNewMedia は before に無い新しい履歴要素が現れるまで待つ｡
func waitForNewMedia(before map[string]bool, exts []string, timeout time.Duration) (mediaEntry, error) {
	deadline := time.Now().Add(timeout)
	for {
		entries, err := readMediaHistory()
		if err != nil {
			return mediaEntry{}, err
		}
		if e, ok := newestNewEntry(before, entries, exts); ok {
			return e, nil
		}
		if time.Now().After(deadline) {
			return mediaEntry{}, fmt.Errorf("%s 以内に新しいキャプチャが CleanShot X の履歴に現れませんでした", timeout)
		}
		time.Sleep(pollInterval)
	}
}

// resolveOutPath は --out の指定から最終的な出力パスを決める｡
// 空、またはディレクトリを指す指定なら defaultFileName で補う｡
func resolveOutPath(out, dir, srcPath string, now time.Time) string {
	name := defaultFileName(srcPath, now)
	switch {
	case out == "":
		return filepath.Join(dir, uniqueName(dir, name))
	case isDirLike(out):
		return filepath.Join(out, uniqueName(out, name))
	default:
		return out
	}
}

// isDirLike は out がディレクトリを指しているかを判定する｡
func isDirLike(out string) bool {
	if out[len(out)-1] == filepath.Separator {
		return true
	}
	info, err := os.Stat(out)
	return err == nil && info.IsDir()
}

// defaultFileName は元ファイルの拡張子を引き継いだ既定のファイル名を作る｡
// 名前は秒単位なので、同じ秒に撮ると衝突する｡既存を上書きしないよう連番を足す｡
func defaultFileName(srcPath string, now time.Time) string {
	ext := filepath.Ext(srcPath)
	if ext == "" {
		ext = ".png"
	}
	return "cleanshot-" + now.Format("20060102-150405") + ext
}

// uniqueName は dir の中で衝突しない名前を返す｡
func uniqueName(dir, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	candidate := name
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, candidate)); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d%s", base, i, ext)
	}
}

// ensureWithin は出力先が作業ディレクトリ配下に収まることを確かめ、絶対パスを返す｡
//
// このコマンドは sandbox.excludedCommands 経由で sandbox の外で動く｡OS 側の保護は
// 効かず、guard-home-dir はスキャン系コマンド (find / du / tree / ls -R) しか見ないため、
// --out の行き先を縛るのはここだけになる｡
//
// symlink 経由の脱出を防ぐため、実在する最も深い祖先を解決してから判定する｡
// 出力先そのものはまだ存在しないのが普通なので、パス全体は解決できない｡
func ensureWithin(dst, cwd string) (string, error) {
	root, err := pathutil.ResolveRealpath(cwd)
	if err != nil {
		return "", fmt.Errorf("作業ディレクトリを解決できません: %w", err)
	}

	abs := dst
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}

	var unresolved []string
	dir := filepath.Clean(abs)
	for {
		resolved, rerr := filepath.EvalSymlinks(dir)
		if rerr == nil {
			for i := len(unresolved) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, unresolved[i])
			}
			abs = resolved
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("出力先のパスを解決できません: %s", dst)
		}
		unresolved = append(unresolved, filepath.Base(dir))
		dir = parent
	}

	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", fmt.Errorf("--out は作業ディレクトリ配下を指定してください (指定: %s)", dst)
	}
	return abs, nil
}
