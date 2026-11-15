package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newRootCmd は repo-scan の root cobra コマンドを組み立てる。
func newRootCmd() *cobra.Command {
	var owners []string
	root := &cobra.Command{
		Use:           "repo-scan",
		Short:         "ghq 配下のリポジトリを検査し、同期プラン JSON を出す",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScan(cmd.OutOrStdout(), owners)
		},
	}
	root.Flags().StringArrayVar(&owners, "owner", nil, "ghq パスの owner を絞り込む (繰り返し指定可。省略時は全リポジトリが対象)")
	root.AddCommand(newChangedCmd())
	return root
}

func newChangedCmd() *cobra.Command {
	var rangeSpec string
	cmd := &cobra.Command{
		Use:   "changed",
		Short: "カレントディレクトリのリポジトリで、指定した range の変更 path 一覧を JSON で出す",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			paths, err := changedPaths(cwd, rangeSpec)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), paths)
		},
	}
	cmd.Flags().StringVar(&rangeSpec, "range", "", "変更検出の range (例 old..new)")
	_ = cmd.MarkFlagRequired("range")
	return cmd
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
