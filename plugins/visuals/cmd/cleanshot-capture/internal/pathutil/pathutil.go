// Package pathutil はパスの正規化を扱う｡
package pathutil

import (
	"path/filepath"
)

// ResolveRealpath はパスを正規化する｡
// GNU realpath -m と同等: パスが存在しなくてもエラーにならず、
// 存在する部分までシンボリックリンクを解決し、残りを正規化する｡
func ResolveRealpath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = abs
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}

	dir := path
	var tail []string
	for {
		parent := filepath.Dir(dir)
		tail = append([]string{filepath.Base(dir)}, tail...)
		dir = parent

		if dir == "/" || dir == "." {
			break
		}

		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			result := resolved
			for _, part := range tail {
				result = filepath.Join(result, part)
			}
			return filepath.Clean(result), nil
		}
	}

	return filepath.Clean(path), nil
}
