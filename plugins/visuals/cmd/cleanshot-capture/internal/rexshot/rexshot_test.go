package rexshot

import (
	"slices"
	"strings"
	"testing"
)

func TestTargetArgs(t *testing.T) {
	tests := []struct {
		name    string
		target  Target
		want    []string
		wantErr bool
	}{
		{
			name:   "block だけ",
			target: Target{Block: "pane"},
			want:   []string{"-b", "pane"},
		},
		{
			name:   "session と window",
			target: Target{Session: "session:1", Window: "bgtest"},
			want:   []string{"-s", "session:1", "-w", "bgtest"},
		},
		{
			name:   "全部",
			target: Target{Block: "shell", Session: "session:1", Window: "bgtest"},
			want:   []string{"-b", "shell", "-s", "session:1", "-w", "bgtest"},
		},
		{
			// 指定が無いと rex は呼び出し元の REX_BLOCK を選び、自分の pane が撮れてしまう
			name:    "block も window も無い",
			target:  Target{Session: "session:1"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.target.Args()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Args() = %v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Args() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Args() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    screenSize
		wantErr bool
	}{
		{
			name:  "rex block call size の出力",
			input: `{"columns":180,"rows":56,"cell_width_px":8,"cell_height_px":16,"owner":null,"desired":[]}`,
			want:  screenSize{Columns: 180, Rows: 56, CellWidthPx: 8, CellHeightPx: 16},
		},
		{
			// 0 のまま描くと大きさ 0 の画像になる
			name:    "セルの大きさが 0",
			input:   `{"columns":80,"rows":24,"cell_width_px":0,"cell_height_px":0}`,
			wantErr: true,
		},
		{
			name:    "JSON でない",
			input:   `not json`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSize([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseSize() = %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSize() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("parseSize() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBuildDocument(t *testing.T) {
	size := screenSize{Columns: 180, Rows: 56, CellWidthPx: 8, CellHeightPx: 16}
	fragment := `<div style="font-family: monospace; white-space: pre;">hello</div>`
	doc := buildDocument(fragment, size)

	for _, want := range []string{
		fragment,
		// script と外部取得を止める｡端末の中身は任意の文字列で、sandbox の外の Chrome が読む
		`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">`,
		// 画面の箱は columns × cell_width、rows × cell_height
		"width: 1440px;",
		"height: 896px;",
		"line-height: 16px;",
		// Menlo の送り幅は 0.6021em なので、セル幅 8px に合わせる文字の大きさは 8 / 0.6021
		"font-size: 13.29px;",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document に %q が無い\n%s", want, doc)
		}
	}
}

func TestChromeArgs(t *testing.T) {
	size := screenSize{Columns: 100, Rows: 30, CellWidthPx: 8, CellHeightPx: 16}
	got := chromeArgs("/tmp/x/page.html", "/work/tmp/out.png", "/tmp/x/profile", size, 2)
	want := []string{
		"--headless",
		"--disable-gpu",
		"--hide-scrollbars",
		"--no-first-run",
		"--user-data-dir=/tmp/x/profile",
		"--force-device-scale-factor=2",
		"--window-size=800,480",
		"--screenshot=/work/tmp/out.png",
		"file:///tmp/x/page.html",
	}
	if !slices.Equal(got, want) {
		t.Errorf("chromeArgs() =\n  %v\nwant\n  %v", got, want)
	}
}
