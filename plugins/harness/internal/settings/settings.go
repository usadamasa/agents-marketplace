// Package settings は Claude Code の settings.json のうち、guard が読む項目だけを読む。
package settings

import (
	"encoding/json"
	"fmt"
	"os"
)

// Settings は settings.json のうち sandbox.excludedCommands だけを表す。
type Settings struct {
	Sandbox struct {
		ExcludedCommands []string `json:"excludedCommands"`
	} `json:"sandbox"`
}

// Load は settings.json を読み込んでパースする。
func Load(path string) (*Settings, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- 読むのは settings.json の決まった置き場だけ
	if err != nil {
		return nil, err
	}

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("settings JSON のパースに失敗: %w", err)
	}

	return &s, nil
}
