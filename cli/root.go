package main

import (
	"fmt"

	"github.com/sliverarmory/reflektor"
	"github.com/spf13/cobra"
)

var (
	callExport string
)

var rootCmd = &cobra.Command{
	Use:          "reflektor <shared library>",
	Short:        "Load a shared library and call an exported function without writing to disk",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		library, err := reflektor.LoadLibraryFile(args[0])
		if err != nil {
			return err
		}
		defer library.Close()

		if err := library.CallExport(callExport); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "ok")
		return nil
	},
}

func init() {
	rootCmd.Flags().StringVar(&callExport, "call-export", "StartW", "Entry symbol to resolve in the shared library")
}
