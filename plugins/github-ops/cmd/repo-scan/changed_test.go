package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestChangedPaths_RangeIsNotAnOption(t *testing.T) {
	dir := newRepo(t)
	out := filepath.Join(t.TempDir(), "written")

	// `-` で始まる range を git log のオプションとして受けると、--output で任意のファイルへ書ける。
	if _, err := changedPaths(dir, "--output="+out); err == nil {
		t.Error("err = nil, want an error for a range that starts with '-'")
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s was written; the range was taken as a git option", out)
	}
}

func TestChangedPaths(t *testing.T) {
	dir := newRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	base = base[:len(base)-1] // strip trailing newline
	writeCommit(t, dir, "docs/a.md", "x\n", "add a")
	writeCommit(t, dir, "docs/b.md", "y\n", "add b")
	head := "HEAD"

	got, err := changedPaths(dir, base+".."+head)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"docs/a.md", "docs/b.md"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestChangedPaths_EmptyIsNonNil(t *testing.T) {
	dir := newRepo(t)
	head := gitT(t, dir, "rev-parse", "HEAD")
	head = head[:len(head)-1] // strip trailing newline

	// 変更のない range (HEAD..HEAD) は空だが、JSON で [] になるよう非 nil を返す。
	got, err := changedPaths(dir, head+".."+head)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("got nil, want non-nil empty slice (so JSON encodes as [])")
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty slice", got)
	}
}
