// Package jsonlscan はセッションの JSONL ファイルを読む補助。
package jsonlscan

import (
	"bufio"
	"io"
)

// NewScanner は JSONL ファイル読み込み用の bufio.Scanner を作成する。
// バッファサイズは最大10MBに設定される。
func NewScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	return s
}
