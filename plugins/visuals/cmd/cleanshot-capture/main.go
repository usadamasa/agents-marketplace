package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/usadamasa/agents-marketplace/plugins/visuals/cmd/cleanshot-capture/internal/displaypower"
	"github.com/usadamasa/agents-marketplace/plugins/visuals/cmd/cleanshot-capture/internal/imgfmt"
	"github.com/usadamasa/agents-marketplace/plugins/visuals/cmd/cleanshot-capture/internal/macwin"
	"github.com/usadamasa/agents-marketplace/plugins/visuals/cmd/cleanshot-capture/internal/screenlock"
)

// wakeTimeout はディスプレイを起こしてから点灯を待つ上限｡実測では 1 秒以内に点く｡
const wakeTimeout = 10 * time.Second

// defaultOutDir は出力先の既定｡--out は作業ディレクトリ配下しか受け付けないため、
// 作業ディレクトリ配下の ./tmp に置く｡
const defaultOutDir = "./tmp"

// commonOpts は全サブコマンドで共通のフラグ｡
type commonOpts struct {
	out     string
	asJSON  bool
	timeout time.Duration
}

// targetOpts はウィンドウ指定のフラグ｡
type targetOpts struct {
	app           string
	title         string
	index         int
	noRaise       bool
	allowOccluded bool
	delay         time.Duration
}

// result は --json で出力する構造｡
type result struct {
	Path   string `json:"path"`
	Source string `json:"source,omitempty"`
	App    string `json:"app,omitempty"`
	Window string `json:"window,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "cleanshot-capture: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "cleanshot-capture",
		Short: "CleanShot X の URL scheme を使ってウィンドウや画面を非対話でキャプチャする",
		Long: `CleanShot X の URL scheme を使ってウィンドウや画面を非対話でキャプチャする｡

撮影したファイルは CleanShot の履歴 (mediaHistory) から回収して --out へコピーする｡
strict sandbox 内では open も osascript も LaunchServices に到達できないため、
このコマンドは sandbox.excludedCommands 経由で使う｡`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newListCmd(), newWindowCmd(), newAreaCmd(), newFullscreenCmd(), newRecordCmd(), newRexCmd())
	return root
}

// addCommonFlags は出力先・出力形式・待ち時間のフラグを登録する｡
func addCommonFlags(cmd *cobra.Command, o *commonOpts, timeout time.Duration) {
	cmd.Flags().StringVar(&o.out, "out", "", "出力先パス (既定: "+defaultOutDir+"/cleanshot-<timestamp>.<ext>)")
	cmd.Flags().BoolVar(&o.asJSON, "json", false, "結果を JSON で出力する")
	cmd.Flags().DurationVar(&o.timeout, "timeout", timeout, "キャプチャ完了を待つ上限")
}

// addSelectFlags はウィンドウの選び方だけを決めるフラグを登録する｡
func addSelectFlags(cmd *cobra.Command, o *targetOpts) {
	cmd.Flags().StringVar(&o.app, "app", "", "対象アプリ名 (完全一致を優先し、無ければ部分一致)")
	cmd.Flags().StringVar(&o.title, "title", "", "ウィンドウタイトルの部分一致で絞り込む")
	cmd.Flags().IntVar(&o.index, "index", 0, "該当ウィンドウが複数あるときの選択位置 (0 が最前面)")
}

// addTargetFlags は画面の矩形を撮るサブコマンド向けに、選択フラグへ前面化の
// 制御を足す｡ウィンドウ ID で撮る window では前面化そのものが要らないため、
// これらのフラグは録画側にしか付かない｡
func addTargetFlags(cmd *cobra.Command, o *targetOpts) {
	addSelectFlags(cmd, o)
	cmd.Flags().BoolVar(&o.noRaise, "no-raise", false, "対象アプリを前面に出さない")
	cmd.Flags().BoolVar(&o.allowOccluded, "allow-occluded", false, "対象が手前のウィンドウに隠れていても撮影する")
	cmd.Flags().DurationVar(&o.delay, "delay", 600*time.Millisecond, "前面化してから撮影するまでの待ち時間")
}

func newListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "画面上のウィンドウを前面順に列挙する",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			windows, err := macwin.List()
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(windows)
			}
			for i, w := range windows {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%d\t%s\n", i, w)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "結果を JSON で出力する")
	return cmd
}

func newWindowCmd() *cobra.Command {
	var (
		common commonOpts
		target targetOpts
	)
	cmd := &cobra.Command{
		Use:   "window",
		Short: "指定したアプリのウィンドウをキャプチャする",
		Long: `指定したアプリのウィンドウをキャプチャする｡

撮影は macOS 標準の screencapture にウィンドウ ID を渡して行う｡ウィンドウ自身の
バッファを読むため、手前に別のウィンドウが重なっていても中身が写り、対象を前面に
出す必要も無い｡ユーザーのフォーカスもクリップボードも触らない｡`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if target.app == "" && target.title == "" {
				return fmt.Errorf("--app か --title のどちらかを指定してください")
			}
			w, err := findTarget(target)
			if err != nil {
				return err
			}
			return captureWindow(cmd, w, common)
		},
	}
	addSelectFlags(cmd, &target)
	addCommonFlags(cmd, &common, 20*time.Second)
	// timeout は CleanShot の履歴を待つためのフラグで、screencapture 経路では使わない｡
	_ = cmd.Flags().MarkHidden("timeout")
	return cmd
}

// captureWindow は対象ウィンドウを --out へ書き出して結果を報告する｡
func captureWindow(cmd *cobra.Command, target macwin.Window, o commonOpts) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("作業ディレクトリを取得できません: %w", err)
	}
	dst, err := ensureWithin(resolveOutPath(o.out, defaultOutDir, "capture.png", time.Now()), cwd)
	if err != nil {
		return err
	}
	if err := ensureDisplayAwake(cmd); err != nil {
		return err
	}
	if err := captureWindowImage(target, dst); err != nil {
		return err
	}
	// Source は CleanShot の履歴にある元ファイルを指す欄で、この経路では発生しない｡
	res := result{Path: dst, App: target.Owner, Window: target.Title}
	// 報告するのは書き出した画像の実ピクセル数｡Retina では point の 2 倍になる｡
	if w, h, serr := imageSize(dst); serr == nil {
		res.Width, res.Height = w, h
	}
	return report(cmd, res, o.asJSON)
}

func newAreaCmd() *cobra.Command {
	var (
		opts                commonOpts
		x, y, width, height int
	)
	cmd := &cobra.Command{
		Use:   "area",
		Short: "座標で指定した矩形をキャプチャする",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if width <= 0 || height <= 0 {
				return fmt.Errorf("--width と --height は正の値を指定してください")
			}
			if err := ensureDisplayAwake(cmd); err != nil {
				return err
			}
			if err := ensureScreenUnlocked(); err != nil {
				return err
			}
			params, err := areaParams(x, y, width, height)
			if err != nil {
				return err
			}
			params["action"] = "copy"
			res, err := captureAndCollect(cmd, "capture-area", params, opts, nil)
			if err != nil {
				return err
			}
			res.Width, res.Height = width, height
			return report(cmd, res, opts.asJSON)
		},
	}
	cmd.Flags().IntVar(&x, "x", 0, "矩形の左上 X 座標 (主ディスプレイ左上が原点、単位は point)")
	cmd.Flags().IntVar(&y, "y", 0, "矩形の左上 Y 座標")
	cmd.Flags().IntVar(&width, "width", 0, "矩形の幅")
	cmd.Flags().IntVar(&height, "height", 0, "矩形の高さ")
	addCommonFlags(cmd, &opts, 20*time.Second)
	return cmd
}

func newFullscreenCmd() *cobra.Command {
	var (
		opts    commonOpts
		display int
	)
	cmd := &cobra.Command{
		Use:   "fullscreen",
		Short: "画面全体をキャプチャする",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ensureDisplayAwake(cmd); err != nil {
				return err
			}
			if err := ensureScreenUnlocked(); err != nil {
				return err
			}
			params := map[string]string{"action": "copy"}
			if display > 0 {
				params["display"] = strconv.Itoa(display)
			}
			res, err := captureAndCollect(cmd, "capture-fullscreen", params, opts, nil)
			if err != nil {
				return err
			}
			return report(cmd, res, opts.asJSON)
		},
	}
	cmd.Flags().IntVar(&display, "display", 0, "対象ディスプレイ (1 が主ディスプレイ、0 で指定なし)")
	addCommonFlags(cmd, &opts, 20*time.Second)
	return cmd
}

func newRecordCmd() *cobra.Command {
	var (
		opts   commonOpts
		target targetOpts
		noWait bool
	)
	cmd := &cobra.Command{
		Use:   "record",
		Short: "録画ツールを対象ウィンドウの矩形で開き、録画の完成を待つ",
		Long: `録画ツールを対象ウィンドウの矩形で開き、録画の完成を待つ｡

CleanShot X の URL scheme には録画の開始・停止コマンドが無い｡開始と停止は
ツール上のボタン (または CleanShot 側のショートカット) で行う必要がある｡
このコマンドは矩形を合わせて録画ツールを開くところまでを担い、その後は
新しい録画ファイルが履歴に現れるまで待って --out へコピーする｡

GIF と MP4 の切り替えは録画ツール UI の設定が引き継がれる｡API からは指定できない｡`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ensureDisplayAwake(cmd); err != nil {
				return err
			}
			if err := ensureScreenUnlocked(); err != nil {
				return err
			}
			params := map[string]string{}
			if target.app != "" || target.title != "" {
				w, err := resolveTarget(target)
				if err != nil {
					return err
				}
				if params, err = areaParams(w.X, w.Y, w.Width, w.Height); err != nil {
					return err
				}
			}
			if noWait {
				return openURL(cleanShotURL("record-screen", params), false)
			}
			res, err := captureAndCollect(cmd, "record-screen", params, opts, recordingExts)
			if err != nil {
				return err
			}
			return report(cmd, res, opts.asJSON)
		},
	}
	addTargetFlags(cmd, &target)
	addCommonFlags(cmd, &opts, 5*time.Minute)
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "録画ツールを開くだけで完成を待たない")
	return cmd
}

// ensureDisplayAwake はディスプレイがスリープしていれば起こし、点くまで待つ｡
//
// 全ての撮り方に共通｡消えている間はフレームバッファが無く、ウィンドウ ID で撮る
// window でも "could not create image" になる｡ロックとは独立した状態で、
// ロックの検査より先に行う (ロック中でも起こせるし、ロック画面が写る問題はそちらで止める)｡
//
// 起こすのはユーザーの操作があったと宣言するだけで、ロックを解除するわけではない｡
func ensureDisplayAwake(cmd *cobra.Command) error {
	asleep, err := displaypower.Asleep()
	if err != nil {
		return err
	}
	if !asleep {
		return nil
	}
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "ディスプレイがスリープしているので起こします")
	return displaypower.Wake(wakeTimeout)
}

// ensureScreenUnlocked は画面がロックされているときに撮影を止める｡
//
// ディスプレイの内容を撮る経路 (area / fullscreen / record) 専用｡ロック中でも撮影は
// 成功してしまい、写るのはロック画面になる｡ウィンドウ ID で撮る window はウィンドウ
// 自身のバッファを読むため、ロック中でも中身が写る (実測)｡
//
// 強行するフラグは用意しない｡ロック画面の写った画像に使い道が無いため｡
func ensureScreenUnlocked() error {
	locked, err := screenlock.Locked()
	if err != nil {
		return err
	}
	if locked {
		return errors.New(
			"画面がロックされています｡いま撮るとロック画面が写ります\n" +
				"ロックを解除してから撮り直してください｡ウィンドウの中身だけが要るなら、" +
				"window サブコマンドはロック中でも撮れます")
	}
	return nil
}

// findTarget はフラグからキャプチャ対象のウィンドウを 1 つ選ぶ｡画面には触らない｡
func findTarget(o targetOpts) (macwin.Window, error) {
	windows, err := macwin.List()
	if err != nil {
		return macwin.Window{}, err
	}
	return macwin.Select(windows, o.app, o.title, o.index)
}

// resolveTarget はキャプチャ対象を決め、必要なら前面化して遮蔽を検査する｡
//
// 画面の矩形を撮る経路 (録画) 専用｡ウィンドウ ID で撮る window はウィンドウ自身の
// バッファを読むため、前面化も遮蔽の検査も要らない｡
func resolveTarget(o targetOpts) (macwin.Window, error) {
	windows, err := macwin.List()
	if err != nil {
		return macwin.Window{}, err
	}
	w, err := macwin.Select(windows, o.app, o.title, o.index)
	if err != nil {
		return macwin.Window{}, err
	}
	if !o.noRaise {
		if err := macwin.Activate(w.PID); err != nil {
			return macwin.Window{}, err
		}
		time.Sleep(o.delay)

		// 前面化でウィンドウ位置と重なり順が変わるため、状態を取り直す｡
		if refreshed, rerr := macwin.List(); rerr == nil {
			windows = refreshed
			for _, r := range windows {
				if r.Number == w.Number {
					w = r
					break
				}
			}
		}
	}
	if o.allowOccluded {
		return w, nil
	}
	if blocking := macwin.Occluders(windows, w); len(blocking) > 0 {
		return macwin.Window{}, fmt.Errorf(
			"対象ウィンドウが手前のウィンドウに隠れています｡そのまま撮ると手前の内容が写ります\n"+
				"  対象: %s\n  手前: %s\n"+
				"対象ウィンドウを手前に出してから撮り直すか、承知のうえなら --allow-occluded を付けてください",
			w, macwin.Describe(blocking))
	}
	return w, nil
}

// areaParams は上原点の矩形を cleanshot://capture-area 用のパラメータに直す｡
// list サブコマンドと --x/--y の座標系を CGWindowList (主ディスプレイ左上が原点) に
// 揃え、CleanShot が期待する下原点への変換はここだけで行う｡
func areaParams(x, y, width, height int) (map[string]string, error) {
	screenHeight, err := macwin.MainDisplayHeight()
	if err != nil {
		return nil, err
	}
	return rectParams(x, macwin.FlipY(y, height, screenHeight), width, height), nil
}

// captureAndCollect は URL scheme を叩き、履歴に現れた新しいファイルを --out へコピーする｡
func captureAndCollect(cmd *cobra.Command, command string, params map[string]string, o commonOpts, exts []string) (result, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return result{}, fmt.Errorf("作業ディレクトリを取得できません: %w", err)
	}
	// 撮ってから弾くと無駄なキャプチャが履歴に残るため、--out は先に検証する｡
	if o.out != "" {
		if _, err := ensureWithin(o.out, cwd); err != nil {
			return result{}, err
		}
	}

	entries, err := readMediaHistory()
	if err != nil {
		return result{}, err
	}
	before := urlSet(entries)

	// 静止画は CleanShot を前面に出さずに撮る (撮影対象を前面に保つため)｡
	// 録画ツールは操作が要るので前面に出す｡
	background := len(exts) == 0
	if err := openURL(cleanShotURL(command, params), background); err != nil {
		return result{}, err
	}
	if !background {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"録画ツールを開きました｡Record を押して録画し、終わったら停止してください (最大 %s 待ちます)\n", o.timeout)
	}

	entry, err := waitForNewMedia(before, exts, o.timeout)
	if err != nil {
		return result{}, err
	}
	src, err := entry.Path()
	if err != nil {
		return result{}, err
	}
	dst, err := ensureWithin(resolveOutPath(o.out, defaultOutDir, src, entry.CreatedAt()), cwd)
	if err != nil {
		return result{}, err
	}
	// 静止画は --out の拡張子どおりの形式で書き出す｡録画は変換できないので、
	// 食い違えばエラーにして元ファイルの場所を伝える｡
	write := imgfmt.WriteAs
	if !background {
		write = imgfmt.CopyRecording
	}
	if err := write(src, dst); err != nil {
		return result{}, err
	}
	return result{
		Path:   dst,
		Source: src,
		App:    entry.AppMetadata.App,
		Window: entry.AppMetadata.Window,
	}, nil
}

// report は結果を stdout に書く｡--json でなければ最終パスだけを出す｡
func report(cmd *cobra.Command, res result, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(res)
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), res.Path)
	return nil
}
