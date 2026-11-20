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
	root.AddCommand(newChangedCmd(), newSyncCmd())
	return root
}

func newSyncCmd() *cobra.Command {
	var owners []string
	cmd := &cobra.Command{
		Use:   "sync [repo]",
		Short: "fetch・default branch の FF・worktree のブランチの rebase を行い、結果を JSON で出す",
		Long: "repo を省くと、scan の対象のリポジトリを全件同期する。\n" +
			"repo には ghq のリポジトリ名 (name、owner/name、host/owner/name) かパスを渡す。scan の対象でなくても同期する。",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoArg := ""
			if len(args) == 1 {
				repoArg = args[0]
			}
			return runSync(cmd.OutOrStdout(), owners, repoArg)
		},
	}
	cmd.Flags().StringArrayVar(&owners, "owner", nil, "repo を省いたときの対象を ghq パスの owner で絞り込む (繰り返し指定可)")
	return cmd
}

func newChangedCmd() *cobra.Command {
	var rangeSpec string
	cmd := &cobra.Command{
		Use:   "changed [repo]",
		Short: "指定したリポジトリ (省略時はカレントディレクトリ) で、range の変更 path 一覧を JSON で出す",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := changedTarget(args)
			if err != nil {
				return err
			}
			paths, err := changedPaths(dir, rangeSpec)
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

// changedTarget は changed の対象のディレクトリを返す。repo の指定が無ければカレントディレクトリになる。
func changedTarget(args []string) (string, error) {
	if len(args) == 0 {
		return os.Getwd()
	}
	return resolveRepoArg(args[0])
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
