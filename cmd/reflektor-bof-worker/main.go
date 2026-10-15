// reflektor-bof-worker runs one BOF request from stdin and emits framed typed
// output to stdout. It is launched by bof.ExecuteIsolated, not by operators.
package main

import (
	"fmt"
	"os"

	"github.com/sliverarmory/reflektor/bof"
	"github.com/sliverarmory/reflektor/internal/bofworker"
)

func main() {
	if err := serve(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve() error {
	request, err := bofworker.ReadRequest(os.Stdin)
	if err != nil {
		return err
	}
	object, err := bof.LoadWithOptions(request.Image, bof.LoadOptions{EntryPoint: request.EntryPoint})
	if err != nil {
		return bofworker.WriteResult(os.Stdout, err.Error())
	}
	var outputErr error
	executeErr := object.ExecuteWithOutput(request.Args, func(output bof.Output) {
		if outputErr == nil {
			outputErr = bofworker.WriteOutput(os.Stdout, output.Type, output.Data)
		}
	})
	closeErr := object.Close()
	if outputErr != nil {
		return outputErr
	}
	if executeErr != nil {
		return bofworker.WriteResult(os.Stdout, executeErr.Error())
	}
	if closeErr != nil {
		return bofworker.WriteResult(os.Stdout, closeErr.Error())
	}
	return bofworker.WriteResult(os.Stdout, "")
}
