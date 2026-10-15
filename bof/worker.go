package bof

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/sliverarmory/reflektor/internal/bofworker"
)

// ErrWorkerCallbacksUnsupported means process-local host callbacks were passed
// to ExecuteIsolated. Native addresses cannot be transferred to a worker.
var ErrWorkerCallbacksUnsupported = errors.New("reflektor: BOF worker cannot use host callbacks or resolver pointers")

// WorkerExitError reports a worker that terminated without a normal result.
// ExitCode is -1 when the OS terminated the worker with a signal. Detail is
// the process-state description, and Stderr contains at most the first 8 KiB
// written by the worker. Output records emitted before termination are still
// returned by ExecuteIsolated.
type WorkerExitError struct {
	ExitCode int
	Detail   string
	Stderr   string
	Err      error
}

func (e *WorkerExitError) Error() string {
	message := fmt.Sprintf("reflektor: BOF worker exited (%s): %v", e.Detail, e.Err)
	if e.Stderr != "" {
		message += ": " + strings.TrimSpace(e.Stderr)
	}
	return message
}

func (e *WorkerExitError) Unwrap() error { return e.Err }

// ExecuteIsolated loads and runs one BOF in a fresh worker process. workerPath
// must name a trusted, same-version, same-platform reflektor-bof-worker binary.
// A context deadline or cancellation forcibly terminates native code in the
// worker; any output captured before termination is returned in callback order
// together with the context error or WorkerExitError. Each call starts and
// reaps its own worker, so a crashed invocation cannot affect later calls.
//
// EntryPoint is the only transferable LoadOptions field. ValidateImports and
// ResolveSymbol are rejected: callback closures and native pointers refer to
// the parent address space and cannot be called in the worker. Built-in and
// ordinary system imports retain Load's default policy, including explicit
// rejection of unprovided privileged Beacon imports. The worker executable
// must be trusted because it receives the BOF image and argument bytes.
func ExecuteIsolated(ctx context.Context, workerPath string, image, args []byte, options LoadOptions) ([]Output, error) {
	if ctx == nil {
		return nil, errors.New("reflektor: nil BOF worker context")
	}
	if options.ValidateImports != nil || options.ResolveSymbol != nil {
		return nil, ErrWorkerCallbacksUnsupported
	}
	if workerPath == "" {
		return nil, errors.New("reflektor: empty BOF worker path")
	}
	request := bofworker.Request{Image: image, Args: args, EntryPoint: options.EntryPoint}
	var encoded bytes.Buffer
	if err := bofworker.WriteRequest(&encoded, request); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	command := exec.CommandContext(ctx, workerPath)
	command.Stdin = &encoded
	command.WaitDelay = time.Second
	stderr := &limitedWorkerStderr{}
	command.Stderr = stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("reflektor: create BOF worker output pipe: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("reflektor: start BOF worker: %w", err)
	}

	type response struct {
		message string
		err     error
	}
	responseDone := make(chan response, 1)
	var outputs []Output
	go func() {
		var totalOutput int64
		message, responseErr := bofworker.ReadResponse(stdout, func(outputType int, data []byte) error {
			if len(outputs) >= 1<<20 {
				return errors.New("reflektor: BOF worker output exceeds 1048576 records")
			}
			totalOutput += int64(len(data))
			if totalOutput > 64<<20 {
				return errors.New("reflektor: BOF worker output exceeds 64 MiB")
			}
			outputs = append(outputs, Output{Type: outputType, Data: data})
			return nil
		})
		responseDone <- response{message: message, err: responseErr}
	}()
	var result response
	select {
	case result = <-responseDone:
	case <-ctx.Done():
		_ = command.Process.Kill()
		_ = stdout.Close()
		result = <-responseDone
	}
	if result.err != nil && !errors.Is(result.err, io.EOF) && !errors.Is(result.err, io.ErrUnexpectedEOF) {
		// An invalid frame may cause the reader to stop while the worker is
		// still writing. Terminate it before waiting on the process.
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if err := ctx.Err(); err != nil {
		return outputs, err
	}
	if result.err != nil && !errors.Is(result.err, io.EOF) && !errors.Is(result.err, io.ErrUnexpectedEOF) {
		return outputs, fmt.Errorf("reflektor: BOF worker protocol: %w", result.err)
	}
	if waitErr != nil {
		return outputs, &WorkerExitError{
			ExitCode: command.ProcessState.ExitCode(),
			Detail:   command.ProcessState.String(),
			Stderr:   stderr.String(),
			Err:      waitErr,
		}
	}
	if result.err != nil {
		return outputs, fmt.Errorf("reflektor: BOF worker protocol: %w", result.err)
	}
	if result.message != "" {
		return outputs, fmt.Errorf("reflektor: BOF worker: %s", result.message)
	}
	return outputs, nil
}

type limitedWorkerStderr struct{ bytes.Buffer }

func (buffer *limitedWorkerStderr) Write(data []byte) (int, error) {
	const maxSize = 8 << 10
	originalLength := len(data)
	remaining := maxSize - buffer.Len()
	if remaining > 0 {
		_, _ = buffer.Buffer.Write(data[:min(len(data), remaining)])
	}
	return originalLength, nil
}

var _ io.Writer = (*limitedWorkerStderr)(nil)
