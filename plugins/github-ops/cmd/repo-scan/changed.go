package main

import "strings"

// changedPaths は range (例 "old..new") で変更されたファイル path の重複排除済み一覧を返す。
func changedPaths(dir, rangeSpec string) ([]string, error) {
	// --end-of-options で、`-` で始まる rangeSpec を git log のオプションとして受けないようにする。
	out, err := git(dir, "log", "--name-only", "--pretty=format:", "--end-of-options", rangeSpec)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	paths := []string{} // 空結果でも JSON で null ではなく [] を返す
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		paths = append(paths, line)
	}
	return paths, nil
}
