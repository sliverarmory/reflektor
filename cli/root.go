package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sliverarmory/reflektor"
	"github.com/spf13/cobra"
)

func newRootCommand() *cobra.Command {
	var (
		callExport  string
		recursive   bool
		printResult bool
		rawArgs     []string
	)
	cmd := &cobra.Command{
		Use:          "reflektor <shared library>",
		Short:        "Load a shared library and call an exported function without writing to disk",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			exportArgs, err := parseMachineWordArgs(rawArgs)
			if err != nil {
				return err
			}

			var library *reflektor.Library
			if recursive {
				library, err = reflektor.LoadLibraryFileRecursive(args[0])
			} else {
				library, err = reflektor.LoadLibraryFile(args[0])
			}
			if err != nil {
				return err
			}
			defer library.Close()

			if printResult || len(exportArgs) != 0 {
				result, err := library.CallExportWithArgs(callExport, exportArgs...)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "result: %#x\n", result)
				return err
			}

			if err := library.CallExport(callExport); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "ok")
			return err
		},
	}
	cmd.Flags().StringVar(&callExport, "call-export", "StartW", "Entry symbol to resolve in the shared library")
	cmd.Flags().BoolVar(&recursive, "recursive", false, "Load non-system dependencies from bytes before calling the export")
	cmd.Flags().StringArrayVar(&rawArgs, "arg", nil, "Machine-word integer argument (repeat up to three times; decimal or 0x hex)")
	cmd.Flags().BoolVar(&printResult, "print-result", false, "Call a native export and print its machine-word result, including with zero arguments")
	return cmd
}

func parseMachineWordArgs(raw []string) ([]uintptr, error) {
	if len(raw) > reflektor.MaxExportArguments {
		return nil, fmt.Errorf("--arg accepts at most %d values, got %d", reflektor.MaxExportArguments, len(raw))
	}
	args := make([]uintptr, len(raw))
	for index, value := range raw {
		base := 10
		parseValue := value
		if strings.HasPrefix(strings.ToLower(value), "0x") {
			parseValue = value[2:]
			base = 16
		}
		parsed, err := strconv.ParseUint(parseValue, base, strconv.IntSize)
		if err != nil {
			return nil, fmt.Errorf("parse --arg %d (%q) as a machine-word integer: %w", index+1, value, err)
		}
		args[index] = uintptr(parsed)
	}
	return args, nil
}
