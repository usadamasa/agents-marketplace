# hook-test-helpers.bash
# hook のテストの共通ヘルパー

# テストファイルごとの一時ディレクトリと mock 用の bin を作る (setup_file から呼ぶ)
setup_mock_bin() {
  TEST_FILE_TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/claude-test.XXXXXX")
  export TEST_FILE_TMPDIR
  MOCK_BIN="$TEST_FILE_TMPDIR/bin"
  mkdir -p "$MOCK_BIN"
  export MOCK_BIN
}

teardown_mock_bin() {
  [ -n "$TEST_FILE_TMPDIR" ] && rm -rf "$TEST_FILE_TMPDIR"
}
