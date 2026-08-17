package reflektor_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sliverarmory/reflektor"
)

const goRuntimeHelperLibraryEnv = "REFLEKTOR_GO_HELPER_LIBRARY"
const goRuntimeHelperRecursiveEnv = "REFLEKTOR_GO_HELPER_RECURSIVE"

func runGoRuntimeFixtureSubprocess(t *testing.T, libraryPath string) {
	runGoRuntimeFixtureSubprocessMode(t, libraryPath, false)
}

func runGoRuntimeFixtureSubprocessMode(t *testing.T, libraryPath string, recursive bool) {
	t.Helper()

	stateDir := t.TempDir()
	markerPath := filepath.Join(stateDir, "complete")
	readyPath := filepath.Join(stateDir, "ready")
	releasePath := filepath.Join(stateDir, "release")
	closedPath := filepath.Join(stateDir, "closed")
	afterClosePath := filepath.Join(stateDir, "after-close")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGoRuntimeFixtureSubprocess$", "-test.count=1")
	recursiveValue := ""
	if recursive {
		recursiveValue = "1"
	}
	cmd.Env = overrideEnv(os.Environ(), map[string]string{
		goRuntimeHelperLibraryEnv:   libraryPath,
		goRuntimeHelperRecursiveEnv: recursiveValue,
		"REFLEKTOR_MARKER":          markerPath,
		"REFLEKTOR_READY":           readyPath,
		"REFLEKTOR_RELEASE":         releasePath,
		"REFLEKTOR_CLOSED":          closedPath,
		"REFLEKTOR_AFTER_CLOSE":     afterClosePath,
	})
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Go runtime fixture helper: %v", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if got, err := os.ReadFile(readyPath); err == nil && bytes.Equal(got, []byte("ready")) {
			break
		}
		select {
		case err := <-waitCh:
			t.Fatalf("Go runtime fixture helper exited before readiness: %v\n%s", err, output.String())
		case <-ctx.Done():
			t.Fatalf("Go runtime fixture helper did not become ready: %v\n%s", ctx.Err(), output.String())
		case <-ticker.C:
		}
	}

	if err := os.WriteFile(releasePath, []byte("release"), 0o600); err != nil {
		t.Fatalf("release Go runtime fixture helper: %v", err)
	}

	select {
	case err := <-waitCh:
		if err != nil {
			t.Fatalf("Go runtime fixture helper failed: %v\n%s", err, output.String())
		}
	case <-ctx.Done():
		t.Fatalf("Go runtime fixture helper did not return after release: %v\n%s", ctx.Err(), output.String())
	}

	got, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read Go runtime completion marker: %v\n%s", err, output.String())
	}
	if !bytes.Equal(got, []byte("ok")) {
		t.Fatalf("unexpected Go runtime completion marker: got=%q want=%q", got, []byte("ok"))
	}
	afterClose, err := os.ReadFile(afterClosePath)
	if err != nil {
		t.Fatalf("read Go runtime post-Close marker: %v\n%s", err, output.String())
	}
	if !bytes.Equal(afterClose, []byte("alive")) {
		t.Fatalf("unexpected Go runtime post-Close marker: got=%q want=%q", afterClose, []byte("alive"))
	}
}

func TestGoRuntimeFixtureSubprocess(t *testing.T) {
	libraryPath := os.Getenv(goRuntimeHelperLibraryEnv)
	if libraryPath == "" {
		return
	}

	var library *reflektor.Library
	var err error
	if os.Getenv(goRuntimeHelperRecursiveEnv) == "1" {
		library, err = reflektor.LoadLibraryFileRecursive(libraryPath)
	} else {
		library, err = reflektor.LoadLibraryFile(libraryPath)
	}
	if err != nil {
		t.Fatalf("LoadLibraryFile(%s): %v", libraryPath, err)
	}
	if err := library.CallExport("StartW"); err != nil {
		t.Fatalf("CallExport(StartW): %v", err)
	}
	if err := library.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	closedPath := os.Getenv("REFLEKTOR_CLOSED")
	afterClosePath := os.Getenv("REFLEKTOR_AFTER_CLOSE")
	if err := os.WriteFile(closedPath, []byte("closed"), 0o600); err != nil {
		t.Fatalf("signal Close(): %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got, err := os.ReadFile(afterClosePath); err == nil && bytes.Equal(got, []byte("alive")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Go runtime did not remain alive after Close")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
