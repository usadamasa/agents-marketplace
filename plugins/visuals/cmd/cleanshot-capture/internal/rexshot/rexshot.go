// Package rexshot は rex の block の画面を、rex の HTML と headless Chrome で PNG にする｡
//
// OS のウィンドウを撮ると Rex アプリが前面に出しているタブしか写らない｡rex から画面の
// 大きさと HTML を取って描けば、裏のタブもユーザーの画面を切り替えずに撮れる｡
package rexshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// chromeBin は HTML を PNG にする headless Chrome｡
const chromeBin = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

// menloAdvance は Menlo の 1 文字の送り幅 (em)｡セル幅からフォントの大きさを決めるのに使う｡
const menloAdvance = 0.6021

// Target は撮る rex の block の指定｡
type Target struct {
	Block   string
	Session string
	Window  string
}

// Args は rex のセレクタ引数を組み立てる｡
//
// block も window も無いと rex は呼び出し元の REX_BLOCK を選ぶ｡撮りたいのは別のタブなので、
// 黙って自分の pane を撮らないようエラーにする｡
func (t Target) Args() ([]string, error) {
	if t.Block == "" && t.Window == "" {
		return nil, errors.New("--block か --window のどちらかを指定してください")
	}
	var args []string
	if t.Block != "" {
		args = append(args, "-b", t.Block)
	}
	if t.Session != "" {
		args = append(args, "-s", t.Session)
	}
	if t.Window != "" {
		args = append(args, "-w", t.Window)
	}
	return args, nil
}

// screenSize は rex block call size の出力｡
type screenSize struct {
	Columns      int `json:"columns"`
	Rows         int `json:"rows"`
	CellWidthPx  int `json:"cell_width_px"`
	CellHeightPx int `json:"cell_height_px"`
}

// pixels は画面の大きさを CSS px で返す｡
func (s screenSize) pixels() (int, int) {
	return s.Columns * s.CellWidthPx, s.Rows * s.CellHeightPx
}

// parseSize は rex block call size の出力を読む｡
func parseSize(data []byte) (screenSize, error) {
	var s screenSize
	if err := json.Unmarshal(data, &s); err != nil {
		return screenSize{}, fmt.Errorf("rex の size の出力を読めません: %w", err)
	}
	if s.Columns <= 0 || s.Rows <= 0 || s.CellWidthPx <= 0 || s.CellHeightPx <= 0 {
		return screenSize{}, fmt.Errorf("rex の size に 0 が含まれています: %+v", s)
	}
	return s, nil
}

// buildDocument は rex capture --format=html の断片を PNG 化用の文書で包む｡
//
// 断片には文書の殻も地の色も無く、画面より上の scrollback も含む｡画面の大きさの箱に
// 下寄せで入れ、はみ出した上側を切ると、末尾の rows 行 (いまの画面) だけが残る｡
//
// CJK は Menlo に無く、代わりのフォントでは 2 セルの幅にならない｡CJK の範囲だけ
// ヒラギノを size-adjust で 2 × menloAdvance em に伸ばして桁を合わせる｡
//
// 反転は filter: invert で書かれ、背景が透明だと白地に白い文字になる｡地の色を塗って
// から反転させる｡
func buildDocument(fragment string, s screenSize) string {
	width, height := s.pixels()
	fontSize := float64(s.CellWidthPx) / menloAdvance
	head := fmt.Sprintf(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">
<style>
@font-face {
  font-family: "term-cjk";
  src: local("HiraginoSans-W3"), local("Hiragino Sans W3");
  size-adjust: %.2f%%;
  unicode-range: U+1100-115F, U+2E80-A4CF, U+AC00-D7A3, U+F900-FAFF, U+FE30-FE4F, U+FF00-FF60, U+FFE0-FFE6;
}
html, body { margin: 0; background: #ffffff; color: #000000; }
.term {
  box-sizing: border-box;
  width: %dpx;
  height: %dpx;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  font-family: Menlo, "term-cjk", monospace;
  font-size: %.2fpx;
  line-height: %dpx;
}
.term div { font-family: inherit !important; }
.term div[style*="invert"] { background-color: #ffffff; }
</style>
</head>
<body>
<div class="term">
`, 2*menloAdvance*100, width, height, fontSize, s.CellHeightPx)
	return head + fragment + "\n</div>\n</body>\n</html>\n"
}

// chromeArgs は headless Chrome の引数を組み立てる｡
// profile を一時ディレクトリに分けるのは、ユーザーが開いている Chrome の profile に触らないため｡
func chromeArgs(htmlPath, dst, profileDir string, s screenSize, scale int) []string {
	width, height := s.pixels()
	return []string{
		"--headless",
		"--disable-gpu",
		"--hide-scrollbars",
		"--no-first-run",
		"--user-data-dir=" + profileDir,
		fmt.Sprintf("--force-device-scale-factor=%d", scale),
		fmt.Sprintf("--window-size=%d,%d", width, height),
		"--screenshot=" + dst,
		"file://" + htmlPath,
	}
}

// Capture は rex の block の画面を dst へ PNG で書き出す｡dst の検証は呼び出し側が行う｡
func Capture(t Target, dst string, scale int) error {
	sel, err := t.Args()
	if err != nil {
		return err
	}
	if _, err := os.Stat(chromeBin); err != nil {
		return fmt.Errorf("headless で使う Google Chrome が見つかりません (%s): %w", chromeBin, err)
	}

	sizeOut, err := runRex(append(append([]string{"block", "call"}, sel...), "size"))
	if err != nil {
		return err
	}
	size, err := parseSize(sizeOut)
	if err != nil {
		return err
	}
	// trim しない｡画面の下端の空行を落とすと、下寄せで画面がずれる
	fragment, err := runRex(append(append([]string{"capture"}, sel...), "--format=html"))
	if err != nil {
		return err
	}

	work, err := os.MkdirTemp("", "cleanshot-rex-")
	if err != nil {
		return fmt.Errorf("作業用の一時ディレクトリを作れません: %w", err)
	}
	defer func() { _ = os.RemoveAll(work) }()
	page := filepath.Join(work, "page.html")
	if err := os.WriteFile(page, []byte(buildDocument(string(fragment), size)), 0o600); err != nil {
		return fmt.Errorf("HTML を書けません: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("出力先ディレクトリを作れません: %w", err)
	}

	// #nosec G204 -- 実行ファイルは固定、引数は一時ディレクトリと検証済みの出力先
	out, err := exec.Command(chromeBin, chromeArgs(page, dst, filepath.Join(work, "profile"), size, scale)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("headless Chrome で PNG にできませんでした: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(dst); err != nil {
		return fmt.Errorf("headless Chrome が PNG を書きませんでした: %s\n%s", dst, strings.TrimSpace(string(out)))
	}
	return nil
}

// runRex は rex を実行して stdout を返す｡
func runRex(args []string) ([]byte, error) {
	// #nosec G204 -- 引数はセレクタのフラグと固定のサブコマンドだけ
	cmd := exec.Command("rex", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("rex %s が失敗しました: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
