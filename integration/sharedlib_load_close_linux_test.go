//go:build linux && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64)

package reflektor_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/sliverarmory/reflektor"
)

const closeReentryFixtureEnv = "REFLEKTOR_CLOSE_REENTRY_FIXTURE"

func TestRootLibraryCloseDestructorReentryLinux(t *testing.T) {
	if fixturePath := os.Getenv(closeReentryFixtureEnv); fixturePath != "" {
		runRootLibraryCloseDestructorReentryLinux(t, fixturePath)
		return
	}

	fixturePath := buildNativeLifecycleFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRootLibraryCloseDestructorReentryLinux$", "-test.count=1")
	cmd.Env = overrideEnv(os.Environ(), map[string]string{closeReentryFixtureEnv: fixturePath})
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("destructor reentry helper timed out: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("destructor reentry helper failed: %v\n%s", err, output)
	}
}

func runRootLibraryCloseDestructorReentryLinux(t *testing.T, fixturePath string) {
	t.Helper()
	image, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read lifecycle fixture: %v", err)
	}
	library, err := reflektor.LoadLibraryRecursiveWithResolver(image, fixturePath, func(reflektor.DependencyRequest) (reflektor.Dependency, error) {
		return reflektor.Dependency{}, reflektor.ErrDependencyNotFound
	})
	if err != nil {
		t.Fatalf("load lifecycle fixture: %v", err)
	}

	var callbacks atomic.Int32
	callback := purego.NewCallback(func() uintptr {
		callbacks.Add(1)
		if err := library.Close(); err != nil {
			t.Errorf("reentrant Close: %v", err)
		}
		if err := library.CallExport("ReflektorLifecycleState"); !errors.Is(err, reflektor.ErrLibraryClosed) {
			t.Errorf("reentrant CallExport error = %v, want ErrLibraryClosed", err)
		}
		return 0
	})
	if _, err := library.CallExportWithArgs("ReflektorLifecycleSetCloseCallback", callback); err != nil {
		t.Fatalf("set destructor callback: %v", err)
	}
	if err := library.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	runtime.KeepAlive(callback)
	if got := callbacks.Load(); got != 1 {
		t.Fatalf("destructor callbacks = %d, want 1", got)
	}
	if err := library.Close(); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
}
