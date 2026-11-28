package macwin

import "testing"

func TestFilterWindows(t *testing.T) {
	all := []Window{
		{Owner: "Ghostty", Layer: 0, Width: 1470, Height: 923},
		{Owner: "Control Center", Layer: 25, Width: 34, Height: 33},
		{Owner: "Window Server", Layer: 24, Width: 1470, Height: 33},
		{Owner: "", Layer: 0, Width: 800, Height: 600},
		{Owner: "Dock", Layer: 0, Width: 10, Height: 10},
		{Owner: "Finder", Layer: 0, Width: 992, Height: 599},
	}
	got := filterWindows(all)
	if len(got) != 2 {
		t.Fatalf("残ったウィンドウ数 = %d, want 2 (%+v)", len(got), got)
	}
	if got[0].Owner != "Ghostty" || got[1].Owner != "Finder" {
		t.Errorf("残ったウィンドウ = %+v, want Ghostty と Finder", got)
	}
}

func TestMatchWindows(t *testing.T) {
	candidates := []Window{
		{Owner: "Code", Title: "main.go — agents-config", Number: 1},
		{Owner: "Visual Studio Code", Title: "README", Number: 2},
		{Owner: "Finder", Title: "src", Number: 3},
		{Owner: "Finder", Title: "Downloads", Number: 4},
	}
	tests := []struct {
		name  string
		app   string
		title string
		want  []int
	}{
		{name: "指定なしは全件", want: []int{1, 2, 3, 4}},
		{name: "完全一致を部分一致より優先", app: "Code", want: []int{1}},
		{name: "完全一致が無ければ部分一致", app: "Visual", want: []int{2}},
		{name: "大文字小文字を無視", app: "finder", want: []int{3, 4}},
		{name: "タイトルの部分一致で絞る", app: "Finder", title: "down", want: []int{4}},
		{name: "タイトル単独でも絞れる", title: "agents-config", want: []int{1}},
		{name: "該当なし", app: "Safari", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchWindows(candidates, tt.app, tt.title)
			if len(got) != len(tt.want) {
				t.Fatalf("件数 = %d, want %d (%+v)", len(got), len(tt.want), got)
			}
			for i, w := range got {
				if w.Number != tt.want[i] {
					t.Errorf("[%d] Number = %d, want %d", i, w.Number, tt.want[i])
				}
			}
		})
	}
}

func TestSelectWindow(t *testing.T) {
	candidates := []Window{
		{Owner: "Finder", Title: "src", Number: 3},
		{Owner: "Finder", Title: "Downloads", Number: 4},
	}
	t.Run("index 省略時は最前面", func(t *testing.T) {
		got, err := Select(candidates, "Finder", "", 0)
		if err != nil {
			t.Fatalf("予期しないエラー: %v", err)
		}
		if got.Number != 3 {
			t.Errorf("Number = %d, want 3", got.Number)
		}
	})
	t.Run("index で背面を選ぶ", func(t *testing.T) {
		got, err := Select(candidates, "Finder", "", 1)
		if err != nil {
			t.Fatalf("予期しないエラー: %v", err)
		}
		if got.Number != 4 {
			t.Errorf("Number = %d, want 4", got.Number)
		}
	})
	t.Run("該当なしはエラー", func(t *testing.T) {
		if _, err := Select(candidates, "Safari", "", 0); err == nil {
			t.Error("エラーを期待したが nil")
		}
	})
	t.Run("index が範囲外はエラー", func(t *testing.T) {
		if _, err := Select(candidates, "Finder", "", 5); err == nil {
			t.Error("エラーを期待したが nil")
		}
	})
}

func TestOccluders(t *testing.T) {
	front := Window{Number: 1, X: 0, Y: 0, Width: 100, Height: 100}
	side := Window{Number: 2, X: 500, Y: 500, Width: 100, Height: 100}
	target := Window{Number: 3, X: 50, Y: 50, Width: 200, Height: 200}
	behind := Window{Number: 4, X: 50, Y: 50, Width: 200, Height: 200}

	tests := []struct {
		name       string
		candidates []Window
		want       int
	}{
		{name: "手前で重なるものを検出", candidates: []Window{front, target}, want: 1},
		{name: "手前でも重ならなければ無視", candidates: []Window{side, target}, want: 0},
		{name: "背面は無視", candidates: []Window{target, behind}, want: 0},
		{name: "手前と背面が混在", candidates: []Window{front, side, target, behind}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Occluders(tt.candidates, target); len(got) != tt.want {
				t.Errorf("遮蔽数 = %d, want %d (%+v)", len(got), tt.want, got)
			}
		})
	}
}

func TestIntersects(t *testing.T) {
	base := Window{X: 100, Y: 100, Width: 100, Height: 100}
	tests := []struct {
		name  string
		other Window
		want  bool
	}{
		{name: "完全に重なる", other: base, want: true},
		{name: "一部が重なる", other: Window{X: 150, Y: 150, Width: 100, Height: 100}, want: true},
		{name: "辺で接するだけは重ならない", other: Window{X: 200, Y: 100, Width: 100, Height: 100}},
		{name: "離れている", other: Window{X: 400, Y: 400, Width: 100, Height: 100}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := intersects(tt.other, base); got != tt.want {
				t.Errorf("intersects = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFlipY(t *testing.T) {
	// 主ディスプレイ 1470x956 で実測した対応関係｡
	// CGWindowList が返す Finder ウィンドウ (y=284, height=599) は
	// cleanshot://capture-area では y=73 になる｡
	const screenHeight = 956
	tests := []struct {
		name   string
		y      int
		height int
		want   int
	}{
		{name: "実測したウィンドウ", y: 284, height: 599, want: 73},
		{name: "画面上端", y: 0, height: 100, want: 856},
		{name: "画面下端", y: 856, height: 100, want: 0},
		{name: "全画面", y: 0, height: 956, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FlipY(tt.y, tt.height, screenHeight); got != tt.want {
				t.Errorf("flipY = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFlipYIsInvolution(t *testing.T) {
	// 反転を 2 回かけると元に戻る｡
	const screenHeight = 956
	for _, y := range []int{0, 33, 284, 500, 856} {
		const height = 100
		if got := FlipY(FlipY(y, height, screenHeight), height, screenHeight); got != y {
			t.Errorf("FlipY(FlipY(%d)) = %d, want %d", y, got, y)
		}
	}
}
