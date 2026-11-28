package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	defaultsBin = "/usr/bin/defaults"
	plutilBin   = "/usr/bin/plutil"

	// cleanShotDomain は CleanShot X の UserDefaults ドメイン｡
	cleanShotDomain = "pl.maketheweb.cleanshotx"

	// mediaHistoryKey はキャプチャ履歴が入っているキー｡値は UTF-8 の JSON を
	// そのまま格納した data 型で、CleanShot が出力先を記録する唯一の経路｡
	// URL scheme には出力先を指定するパラメータが無いため、ここを読んで回収する｡
	mediaHistoryKey = "mediaHistory"

	// appleEpochOffset は Apple 基準時刻 (2001-01-01 UTC) と Unix epoch の差 (秒)｡
	appleEpochOffset = 978307200
)

// mediaEntry は mediaHistory の 1 要素｡
type mediaEntry struct {
	URL          string  `json:"url"`
	TempURL      *string `json:"tempURL"`
	CreationDate float64 `json:"creationDate"`
	AppMetadata  struct {
		App      string `json:"app"`
		BundleID string `json:"bundleID"`
		Window   string `json:"window"`
	} `json:"appMetadata"`
}

// CreatedAt は creationDate を通常の時刻に直す｡
func (e mediaEntry) CreatedAt() time.Time {
	return time.Unix(int64(e.CreationDate)+appleEpochOffset, 0)
}

// Path は url を実ファイルパスに直す｡
func (e mediaEntry) Path() (string, error) {
	return fileURLToPath(e.URL)
}

// fileURLToPath は file:// URL をパーセントデコードしてパスにする｡
func fileURLToPath(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("URL を解析できません (%s): %w", raw, err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("file:// URL ではありません: %s", raw)
	}
	return u.Path, nil
}

// readMediaHistory は CleanShot の履歴を読む｡
// defaults export は cfprefsd 経由で読むため、CleanShot がまだ plist を
// 書き出していない状態でも最新の値が取れる｡
func readMediaHistory() ([]mediaEntry, error) {
	export := exec.Command(defaultsBin, "export", cleanShotDomain, "-")
	extract := exec.Command(plutilBin, "-extract", mediaHistoryKey, "raw", "-o", "-", "-")

	pipe, err := export.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("defaults export のパイプを作れません: %w", err)
	}
	extract.Stdin = pipe

	if err := export.Start(); err != nil {
		return nil, fmt.Errorf("defaults export を起動できません: %w", err)
	}
	out, extractErr := extract.Output()
	waitErr := export.Wait()
	if extractErr != nil {
		return nil, fmt.Errorf("%s の取り出しに失敗しました (CleanShot X の履歴が空の可能性があります): %w",
			mediaHistoryKey, extractErr)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("defaults export が失敗しました: %w", waitErr)
	}
	return parseMediaHistory(out)
}

// parseMediaHistory は plutil が出す base64 を JSON として読む｡
func parseMediaHistory(raw []byte) ([]mediaEntry, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("%s の base64 を復号できません: %w", mediaHistoryKey, err)
	}
	var entries []mediaEntry
	if err := json.Unmarshal(decoded, &entries); err != nil {
		return nil, fmt.Errorf("%s の JSON を解析できません: %w", mediaHistoryKey, err)
	}
	return entries, nil
}

// urlSet は履歴の URL 集合を返す｡件数比較ではなく集合の差分で新着を判定するのは、
// historyCapacity に達した履歴が古い要素を捨てて件数が変わらなくなるため｡
func urlSet(entries []mediaEntry) map[string]bool {
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.URL != "" {
			seen[e.URL] = true
		}
	}
	return seen
}

// newestNewEntry は before に無い要素のうち最も新しいものを返す｡
// exts が空でなければ、その拡張子 (先頭のドットを含む、小文字) に限定する｡
func newestNewEntry(before map[string]bool, after []mediaEntry, exts []string) (mediaEntry, bool) {
	var found mediaEntry
	ok := false
	for _, e := range after {
		if e.URL == "" || before[e.URL] {
			continue
		}
		if !hasAnyExt(e.URL, exts) {
			continue
		}
		if !ok || e.CreationDate > found.CreationDate {
			found, ok = e, true
		}
	}
	return found, ok
}

func hasAnyExt(rawURL string, exts []string) bool {
	if len(exts) == 0 {
		return true
	}
	return slices.Contains(exts, strings.ToLower(filepath.Ext(rawURL)))
}
