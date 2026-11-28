package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/usadamasa/agents-config/cleanshot-capture/internal/imgfmt"
	"github.com/usadamasa/agents-config/cleanshot-capture/internal/rexshot"
)

func newRexCmd() *cobra.Command {
	var (
		common commonOpts
		target rexshot.Target
		scale  int
	)
	cmd := &cobra.Command{
		Use:   "rex",
		Short: "rex の block (裏のタブを含む) の画面を PNG にする",
		Long: `rex の block (裏のタブを含む) の画面を PNG にする｡

window サブコマンドが撮る OS のウィンドウには、Rex アプリが前面に出しているタブしか写らない｡
このサブコマンドは rex から画面の大きさと HTML を取り、headless Chrome で PNG に描く｡
ユーザーが見ている画面は切り替えない｡`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scale <= 0 {
				return fmt.Errorf("--scale は正の値を指定してください")
			}
			return captureRex(cmd, target, common, scale)
		},
	}
	cmd.Flags().StringVarP(&target.Block, "block", "b", "", "対象の block (rex の -b と同じ)")
	cmd.Flags().StringVarP(&target.Session, "session", "s", "", "対象の session (rex の -s と同じ)")
	cmd.Flags().StringVarP(&target.Window, "window", "w", "", "対象の window (rex の -w と同じ)")
	cmd.Flags().IntVar(&scale, "scale", 2, "画素密度の倍率")
	addCommonFlags(cmd, &common, 0)
	// timeout は CleanShot の履歴を待つためのフラグで、この経路では使わない｡
	_ = cmd.Flags().MarkHidden("timeout")
	return cmd
}

// captureRex は出力先を検証してから rex の block の画面を PNG で書き出す｡
func captureRex(cmd *cobra.Command, target rexshot.Target, o commonOpts, scale int) error {
	sel, err := target.Args()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("作業ディレクトリを取得できません: %w", err)
	}
	dst, err := ensureWithin(resolveOutPath(o.out, defaultOutDir, "capture.png", time.Now()), cwd)
	if err != nil {
		return err
	}
	// headless Chrome の --screenshot は PNG しか書かない
	if format, ferr := imgfmt.ForPath(dst); ferr != nil || format != imgfmt.PNG {
		return fmt.Errorf("rex サブコマンドの出力先は .png だけを受け付けます: %s", dst)
	}
	if err := rexshot.Capture(target, dst, scale); err != nil {
		return err
	}
	res := result{Path: dst, App: "rex", Window: strings.Join(sel, " ")}
	if w, h, serr := imageSize(dst); serr == nil {
		res.Width, res.Height = w, h
	}
	return report(cmd, res, o.asJSON)
}
