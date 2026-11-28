package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// configState は guard-home-dir.yaml を読んだ結果。
type configState int

const (
	configMissing configState = iota // ファイルが無い
	configInvalid                    // 読めない・壊れている・不正な値がある
	configOK
)

// homeConfig は guard-home-dir.yaml から読んだ、許すホーム配下の置き場。
type homeConfig struct {
	path  string // YAML の置き場
	state configState
	dirs  []string // ホームからの相対パス。state が configOK のときだけ入る
	err   error    // configInvalid の理由
}

// configExample は deny の文言で示す YAML の書き方。
const configExample = "allowed_home_directories:\n  - src\n  - workspace"

// configPath は guard-home-dir.yaml の置き場を返す。
// XDG Base Directory の決まりに従い、XDG_CONFIG_HOME が絶対パスでなければ ~/.config を使う。
func configPath(home string, getenv func(string) string) string {
	base := getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(base) {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "claude-harness", "guard-home-dir.yaml")
}

// loadConfig は guard-home-dir.yaml を読む。
// 不正な値が 1 つでもあれば全体を読めないものとして扱い、正しい値も使わない。
func loadConfig(home string, getenv func(string) string) homeConfig {
	cfg := homeConfig{path: configPath(home, getenv)}
	data, err := os.ReadFile(cfg.path)
	if errors.Is(err, fs.ErrNotExist) {
		cfg.state = configMissing
		return cfg
	}
	if err != nil {
		return invalid(cfg, err)
	}

	var raw struct {
		AllowedHomeDirectories []string `yaml:"allowed_home_directories"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return invalid(cfg, err)
	}
	for _, d := range raw.AllowedHomeDirectories {
		if err := validateDir(d); err != nil {
			return invalid(cfg, err)
		}
	}
	cfg.state = configOK
	cfg.dirs = raw.AllowedHomeDirectories
	return cfg
}

func invalid(cfg homeConfig, err error) homeConfig {
	cfg.state = configInvalid
	cfg.err = err
	return cfg
}

// validateDir は allowed_home_directories の値がホームの下の 1 つの置き場を指すかを確かめる。
func validateDir(d string) error {
	switch {
	case d == "":
		return errors.New("空の値がある")
	case strings.HasPrefix(d, "/"), strings.HasPrefix(d, "~"):
		return fmt.Errorf("%q はホームからの相対パスではない", d)
	case containsDotDot(d):
		return fmt.Errorf("%q は .. を含む", d)
	case filepath.Clean(d) == ".":
		return fmt.Errorf("%q はホームそのものを指す", d)
	}
	return nil
}

func containsDotDot(d string) bool {
	for _, part := range strings.Split(d, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// denyReason は deniedPath を止めた理由と、YAML の置き場と書き方を返す。
func denyReason(deniedPath string, cfg homeConfig) string {
	head := fmt.Sprintf("ホームディレクトリ走査防止: %s は cwd と ~/.claude と許可した置き場の外にあるためアクセスできません。", deniedPath)
	switch cfg.state {
	case configOK:
		return head + fmt.Sprintf("ホーム配下の置き場を許すには、ユーザーが %s の allowed_home_directories にホームからの相対パスを足してください。", cfg.path)
	case configInvalid:
		return head + fmt.Sprintf("%s を読めないため (%v)、cwd と ~/.claude だけを許しています。ユーザーが次の形に直してください (値はホームからの相対パス):\n%s", cfg.path, cfg.err, configExample)
	default:
		return head + fmt.Sprintf("%s が無いため、cwd と ~/.claude だけを許しています。ホーム配下の置き場を許すには、ユーザーが次の形で置いてください (値はホームからの相対パス):\n%s", cfg.path, configExample)
	}
}
