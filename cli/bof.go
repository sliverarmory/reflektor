package main

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/sliverarmory/reflektor/bof"
	"github.com/spf13/cobra"
)

func newBOFCommand() *cobra.Command {
	var entry string
	var rawArgs []string
	cmd := &cobra.Command{
		Use:          "bof <object file>",
		Short:        "Load and execute a native BOF",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, paths []string) error {
			var arguments bof.Arguments
			for index, raw := range rawArgs {
				if err := appendBOFArgument(&arguments, raw); err != nil {
					return fmt.Errorf("parse --bof-arg %d: %w", index+1, err)
				}
			}
			object, err := bof.LoadFileWithOptions(paths[0], bof.LoadOptions{EntryPoint: entry})
			if err != nil {
				return err
			}
			defer object.Close()
			outputs, executeErr := object.Execute(arguments.Bytes())
			for _, record := range outputs {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "type=%d data=%q\n", record.Type, record.Data); err != nil {
					return err
				}
			}
			return executeErr
		},
	}
	cmd.Flags().StringVar(&entry, "entry", "", "Exact BOF entry symbol (default: go, _go, coffee, or _coffee)")
	cmd.Flags().StringArrayVar(&rawArgs, "bof-arg", nil, "Ordered BOF argument as int32:, int16:, string:, utf16:, or hex: (repeatable)")
	return cmd
}

func appendBOFArgument(arguments *bof.Arguments, raw string) error {
	kind, value, ok := strings.Cut(raw, ":")
	if !ok {
		return fmt.Errorf("argument %q must have a type and colon", raw)
	}
	switch kind {
	case "int32":
		parsed, err := parseBOFInteger(value, 32)
		if err != nil {
			return fmt.Errorf("int32 value %q: %w", value, err)
		}
		return arguments.AddInt32(int32(parsed))
	case "int16":
		parsed, err := parseBOFInteger(value, 16)
		if err != nil {
			return fmt.Errorf("int16 value %q: %w", value, err)
		}
		return arguments.AddInt16(int16(parsed))
	case "string":
		return arguments.AddString(value)
	case "utf16":
		return arguments.AddUTF16String(value)
	case "hex":
		decoded, err := hex.DecodeString(value)
		if err != nil {
			return fmt.Errorf("hex value %q: %w", value, err)
		}
		return arguments.AddBytes(decoded)
	default:
		return fmt.Errorf("unsupported BOF argument type %q", kind)
	}
}

func parseBOFInteger(value string, bits int) (int64, error) {
	parseValue := value
	base := 10
	sign := ""
	if strings.HasPrefix(parseValue, "-") || strings.HasPrefix(parseValue, "+") {
		sign, parseValue = parseValue[:1], parseValue[1:]
	}
	if strings.HasPrefix(strings.ToLower(parseValue), "0x") {
		parseValue = sign + parseValue[2:]
		base = 16
	} else {
		parseValue = sign + parseValue
	}
	return strconv.ParseInt(parseValue, base, bits)
}
