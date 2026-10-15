package reflektor_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sliverarmory/reflektor/bof"
)

func TestIsolatedBOFWorker(t *testing.T) {
	target, ok := nativeBOFTarget()
	if !ok {
		t.Fatalf("missing BOF fixture target for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	imagePath := buildBOFSource(t, t.TempDir(), target, "isolation_fixture", "isolation_fixture.c")
	image, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	workerPath := filepath.Join(t.TempDir(), "reflektor-bof-worker")
	if runtime.GOOS == "windows" {
		workerPath += ".exe"
	}
	buildArgs := []string{"build"}
	if runtime.GOOS == "freebsd" {
		buildArgs = append(buildArgs, "-gcflags=github.com/ebitengine/purego/internal/fakecgo=-std")
	}
	buildArgs = append(buildArgs, "-o", workerPath, "../cmd/reflektor-bof-worker")
	build := exec.Command("go", buildArgs...)
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build worker: %v\n%s", buildErr, output)
	}

	assertPrefix := func(t *testing.T, outputs []bof.Output) {
		t.Helper()
		if len(outputs) < 2 || outputs[0].Type != bof.OutputOEM || !bytes.Equal(outputs[0].Data, []byte{0xff, 0, 0x80}) || outputs[1].Type != -7 || len(outputs[1].Data) != 0 {
			t.Fatalf("worker output prefix = %#v", outputs)
		}
	}
	run := func(argument []byte) ([]bof.Output, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return bof.ExecuteIsolated(ctx, workerPath, image, argument, bof.LoadOptions{})
	}

	t.Run("success and owned typed output", func(t *testing.T) {
		outputs, err := run(nil)
		if err != nil {
			t.Fatal(err)
		}
		assertPrefix(t, outputs)
		if len(outputs) != 3 || outputs[2].Type != bof.OutputUTF8 || string(outputs[2].Data) != "worker-finished" {
			t.Fatalf("worker outputs = %#v", outputs)
		}
	})

	t.Run("hard deadline and partial output", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		start := time.Now()
		outputs, err := bof.ExecuteIsolated(ctx, workerPath, image, []byte{'H'}, bof.LoadOptions{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline error = %v", err)
		}
		if time.Since(start) > 6*time.Second {
			t.Fatalf("deadline took %s", time.Since(start))
		}
		assertPrefix(t, outputs)
		if len(outputs) != 2 {
			t.Fatalf("deadline outputs = %#v", outputs)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		time.AfterFunc(200*time.Millisecond, cancel)
		start := time.Now()
		_, err := bof.ExecuteIsolated(ctx, workerPath, image, []byte{'H'}, bof.LoadOptions{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v", err)
		}
		if time.Since(start) > 4*time.Second {
			t.Fatalf("cancellation took %s", time.Since(start))
		}
	})

	t.Run("crash and partial output", func(t *testing.T) {
		outputs, err := run([]byte{'C'})
		var exitErr *bof.WorkerExitError
		if !errors.As(err, &exitErr) || exitErr.Detail == "" || exitErr.ExitCode == 0 {
			t.Fatalf("crash error = %T %v, want WorkerExitError", err, err)
		}
		assertPrefix(t, outputs)
		if len(outputs) != 2 {
			t.Fatalf("crash outputs = %#v", outputs)
		}
	})

	t.Run("later worker remains usable", func(t *testing.T) {
		outputs, err := run(nil)
		if err != nil || len(outputs) != 3 {
			t.Fatalf("later worker outputs = %#v, error = %v", outputs, err)
		}
	})

	t.Run("process-local callbacks rejected", func(t *testing.T) {
		_, err := bof.ExecuteIsolated(context.Background(), workerPath, image, nil, bof.LoadOptions{
			ResolveSymbol: func(bof.Import) (uintptr, bool, error) { return 1, true, nil },
		})
		if !errors.Is(err, bof.ErrWorkerCallbacksUnsupported) {
			t.Fatalf("resolver error = %v", err)
		}
		_, err = bof.ExecuteIsolated(context.Background(), workerPath, image, nil, bof.LoadOptions{
			ValidateImports: func([]bof.Import) error { return nil },
		})
		if !errors.Is(err, bof.ErrWorkerCallbacksUnsupported) {
			t.Fatalf("validator error = %v", err)
		}
	})

	t.Run("load error returned without crashing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		outputs, err := bof.ExecuteIsolated(ctx, workerPath, []byte("not-an-object"), nil, bof.LoadOptions{})
		if err == nil || !strings.Contains(err.Error(), "load BOF") || len(outputs) != 0 {
			t.Fatalf("load error = %v, outputs = %#v", err, outputs)
		}
	})
}
