//go:build (darwin || linux || windows) && (386 || amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sliverarmory/reflektor"
)

const (
	recursiveHelperModeEnv   = "REFLEKTOR_RECURSIVE_HELPER_MODE"
	recursiveHelperRootEnv   = "REFLEKTOR_RECURSIVE_HELPER_ROOT"
	recursiveHelperHiddenEnv = "REFLEKTOR_RECURSIVE_HELPER_HIDDEN"
)

func TestLoadLibraryRecursiveDependencies(t *testing.T) {
	requireCommand(t, "zig")

	for _, mode := range []string{"file", "bytes"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			stateDir := t.TempDir()
			graphDir := filepath.Join(stateDir, "graph")
			if err := os.MkdirAll(graphDir, 0o755); err != nil {
				t.Fatalf("create recursive graph directory: %v", err)
			}
			libraries := buildRecursiveSharedLibs(t, graphDir, runtime.GOOS, runtime.GOARCH)
			hiddenDir := filepath.Join(stateDir, "graph-hidden")
			markerPath := filepath.Join(stateDir, "recursive-marker.txt")

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecursiveFixtureSubprocess$", "-test.count=1", "-test.v")
			cmd.Env = overrideEnv(os.Environ(), map[string]string{
				recursiveHelperModeEnv:   mode,
				recursiveHelperRootEnv:   libraries.root,
				recursiveHelperHiddenEnv: hiddenDir,
				"REFLEKTOR_MARKER":       markerPath,
			})
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("recursive fixture helper timed out: %v\n%s", ctx.Err(), output)
			}
			if err != nil {
				t.Fatalf("recursive fixture helper failed in %s mode: %v\n%s", mode, err, output)
			}

			got, err := os.ReadFile(markerPath)
			if err != nil {
				t.Fatalf("read recursive marker: %v\n%s", err, output)
			}
			if !bytes.Equal(got, []byte("ok")) {
				t.Fatalf("unexpected recursive marker: got=%q want=%q\n%s", got, []byte("ok"), output)
			}
		})
	}
}

func TestRecursiveFixtureSubprocess(t *testing.T) {
	mode := os.Getenv(recursiveHelperModeEnv)
	if mode == "" {
		return
	}

	rootPath := os.Getenv(recursiveHelperRootEnv)
	hiddenDir := os.Getenv(recursiveHelperHiddenEnv)
	if rootPath == "" || hiddenDir == "" {
		t.Fatal("recursive helper paths are not configured")
	}

	var (
		library *reflektor.Library
		err     error
	)
	switch mode {
	case "file":
		library, err = reflektor.LoadLibraryFileRecursive(rootPath)
	case "bytes":
		rootData, readErr := os.ReadFile(rootPath)
		if readErr != nil {
			t.Fatalf("read recursive root bytes: %v", readErr)
		}
		if err := os.Chdir(filepath.Dir(rootPath)); err != nil {
			t.Fatalf("change to recursive fixture directory: %v", err)
		}
		library, err = reflektor.LoadLibraryRecursive(rootData)
	default:
		t.Fatalf("unknown recursive helper mode %q", mode)
	}
	if err != nil {
		t.Fatalf("load recursive fixture in %s mode: %v", mode, err)
	}
	defer func() {
		if closeErr := library.Close(); closeErr != nil {
			t.Errorf("close recursive fixture: %v", closeErr)
		}
	}()

	assertRecursiveDependenciesNotNativeLoaded(t, filepath.Dir(rootPath))
	if err := os.Rename(filepath.Dir(rootPath), hiddenDir); err != nil {
		t.Fatalf("hide recursive dependency files after load: %v", err)
	}
	if err := library.CallExport("StartW"); err != nil {
		t.Fatalf("call recursive StartW after hiding dependencies: %v", err)
	}
	if err := library.CallExport("StartW"); err != nil {
		t.Fatalf("repeat recursive StartW after hiding dependencies: %v", err)
	}

	if marker := os.Getenv("REFLEKTOR_MARKER"); marker == "" {
		t.Fatal("REFLEKTOR_MARKER is not configured")
	} else if _, err := os.Stat(marker); err != nil {
		t.Fatalf("recursive fixture did not write marker %s: %v", marker, err)
	}
	t.Logf("recursive %s mode repeatedly called an export after source dependencies were renamed", mode)
}
