package screenlock

import "testing"

// unlockedRegistry は解除中に ioreg -n Root -d1 が返す Root プロパティ (実測から抜粋)｡
// CGSSessionScreenIsLocked はキーごと現れない｡
const unlockedRegistry = `+-o Root  <class IORegistryEntry, id 0x100000100, retain 35>
    {
      "OS Build Version" = "25F84"
      "IOConsoleLocked" = No
      "IOConsoleUsers" = ({"kCGSSessionOnConsoleKey"=Yes,"kSCSecuritySessionID"=100024,"kCGSSessionIDKey"=257,"kCGSSessionUserNameKey"="user","kCGSSessionUserIDKey"=502})
      "IOKitDiagnostics" = {"Allocations"=0}
    }
`

// lockedRegistry はロック中の実測｡IOConsoleLocked が Yes になり、
// IOConsoleUsers に CGSSessionScreenIsLocked と CGSSessionScreenLockedTime が生える｡
const lockedRegistry = `+-o Root  <class IORegistryEntry, id 0x100000100, retain 35>
    {
      "OS Build Version" = "25F84"
      "IOConsoleLocked" = Yes
      "IOConsoleUsers" = ({"kCGSSessionOnConsoleKey"=Yes,"kSCSecuritySessionID"=100024,"CGSSessionScreenLockedTime"=1787475697,"CGSSessionScreenIsLocked"=Yes,"kCGSSessionUserIDKey"=502})
      "IOKitDiagnostics" = {"Allocations"=0}
    }
`

func TestParseLocked(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    bool
		wantErr bool
	}{
		{name: "解除中", input: unlockedRegistry, want: false},
		{name: "ロック中", input: lockedRegistry, want: true},
		{
			name: "IOConsoleLocked だけがロックを示す",
			input: `      "IOConsoleLocked" = Yes
      "IOConsoleUsers" = ({"kCGSSessionOnConsoleKey"=Yes})
`,
			want: true,
		},
		{
			name: "CGSSessionScreenIsLocked だけがロックを示す",
			input: `      "IOConsoleLocked" = No
      "IOConsoleUsers" = ({"CGSSessionScreenIsLocked"=Yes})
`,
			want: true,
		},
		{
			// 解除時にキーが消えずに残る macOS があっても、値を見るので誤検知しない｡
			name: "CGSSessionScreenIsLocked が No で残る",
			input: `      "IOConsoleLocked" = No
      "IOConsoleUsers" = ({"CGSSessionScreenIsLocked"=No})
`,
			want: false,
		},
		{
			// IOKitDiagnostics のような別プロパティに同名の文字列があっても拾わない｡
			name: "IOConsoleUsers 以外の行は見ない",
			input: `      "IOConsoleLocked" = No
      "IOConsoleUsers" = ({"kCGSSessionOnConsoleKey"=Yes})
      "IOKitDiagnostics" = {"Note"="CGSSessionScreenIsLocked=Yes"}
`,
			want: false,
		},
		{
			name:    "どちらのキーも無ければ判定できない",
			input:   "+-o Root\n    {\n      \"OS Build Version\" = \"25F84\"\n    }\n",
			wantErr: true,
		},
		{name: "空の出力", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLocked([]byte(tt.input))
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
				t.Errorf("parseLocked() = %v, want %v", got, tt.want)
			}
		})
	}
}
