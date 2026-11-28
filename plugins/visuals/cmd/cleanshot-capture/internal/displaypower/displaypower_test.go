package displaypower

import (
	"strings"
	"testing"
)

// 以下は pmset -g log の実測から抜粋した行｡列はタブ区切りで、Domain 列と
// メッセージ列は空白で右詰めされている｡
const (
	headerLine     = "PM ASL data store: /var/log/powermanagement\n"
	assertionLine  = "2026-08-24 07:05:53 +0900 Assertions          \tPID 16698(caffeinate) Created UserIsActive \"caffeinate command-line tool\" 00:00:00  id:0x0x900009d63 [System: PrevIdle DeclUser IPushSrvc kDisp]\n"
	displayOffLine = "2026-08-24 07:05:49 +0900 Notification        \tDisplay is turned off                                                      \t          \n"
	displayOnLine  = "2026-08-24 07:05:53 +0900 Notification        \tDisplay is turned on                                                       \t          \n"
	// Assertions 列には Display を含む別のメッセージも流れる｡状態の判定に使ってはいけない｡
	displayWakeLine = "2026-08-24 06:46:35 +0900 Assertions          \tPID 10850(PowerChime) Created DisplayWake \"com.apple.chimeOnAttach\" 00:00:00  id:0x0xe000098b3 [System: PrevIdle PrevDisp]\n"
	preventLine     = "2026-08-24 06:46:36 +0900 Assertions          \tPID 336(powerd) Summary PreventUserIdleSystemSleep \"Powerd - Prevent sleep while display is on\" 00:00:00  id:0x0x1000098b7\n"
)

func TestParseAsleep(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    bool
		wantErr bool
	}{
		{
			name:  "最後が off ならスリープ中",
			input: headerLine + displayOnLine + assertionLine + displayOffLine,
			want:  true,
		},
		{
			name:  "最後が on なら点いている",
			input: headerLine + displayOffLine + assertionLine + displayOnLine,
			want:  false,
		},
		{
			name:  "off の後に on が続けば点いている (実測の復帰ログ)",
			input: displayOffLine + assertionLine + displayOnLine + preventLine,
			want:  false,
		},
		{
			// Assertions 列の DisplayWake や "display is on" を状態と取り違えない｡
			name:  "Assertions 列の Display は見ない",
			input: displayOffLine + displayWakeLine + preventLine,
			want:  true,
		},
		{
			name:    "Display の通知が無ければ判定できない",
			input:   headerLine + assertionLine + preventLine,
			wantErr: true,
		},
		{name: "空の出力", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAsleep([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("エラーを期待したが nil (got=%v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseAsleep() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseAsleepIgnoresTrailingPadding(t *testing.T) {
	// メッセージ列は右側が空白で埋まる｡末尾の空白やタブの有無で判定が変わってはいけない｡
	line := strings.TrimRight(displayOffLine, " \t\n")
	got, err := parseAsleep([]byte(line))
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	if !got {
		t.Errorf("parseAsleep() = false, want true (input=%q)", line)
	}
}
