//go:build linux && (386 || amd64 || arm64)

package memmod

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const linuxCallArgsCSource = `
#include <stdint.h>

__attribute__((visibility("default"))) uintptr_t ReflektorCArgs0(void) {
  return (uintptr_t)0x1234u;
}

__attribute__((visibility("default"))) uintptr_t ReflektorCArgs1(uintptr_t a0) {
  return a0 ^ (uintptr_t)0x55u;
}

__attribute__((visibility("default"))) uintptr_t ReflektorCArgs2(uintptr_t a0, uintptr_t a1) {
  return a0 + (a1 * (uintptr_t)3u) + (uintptr_t)7u;
}

__attribute__((visibility("default"))) uintptr_t ReflektorCArgs3(uintptr_t a0, uintptr_t a1, uintptr_t a2) {
  return a0 + (a1 * (uintptr_t)3u) + (a2 * (uintptr_t)5u) + (uintptr_t)11u;
}
`

func TestCallExportWithArgsLinuxNativeFixtures(t *testing.T) {
	fixtures := []struct {
		name   string
		prefix string
		build  func(*testing.T) string
	}{
		{name: "c", prefix: "ReflektorCArgs", build: buildLinuxCallArgsCFixture},
		{name: "rust", prefix: "ReflektorRustArgs", build: buildLinuxCallArgsRustFixture},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			path := fixture.build(t)
			payload, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s fixture: %v", fixture.name, err)
			}

			modes := []string{"legacy", "recursive"}
			for _, mode := range modes {
				mode := mode
				t.Run(mode, func(t *testing.T) {
					module := loadLinuxCallArgsFixture(t, mode, payload, path)
					t.Cleanup(module.Free)

					tests := []struct {
						name string
						args []uintptr
						want uintptr
					}{
						{name: " " + fixture.prefix + "0 ", want: 0x1234},
						{name: fixture.prefix + "1", args: []uintptr{0x123}, want: 0x123 ^ 0x55},
						{name: fixture.prefix + "2", args: []uintptr{2, 7}, want: 2 + 7*3 + 7},
						{name: "_" + fixture.prefix + "3", args: []uintptr{2, 7, 11}, want: 2 + 7*3 + 11*5 + 11},
					}
					for _, test := range tests {
						got, err := module.CallExportWithArgs(test.name, test.args...)
						if err != nil {
							t.Fatalf("CallExportWithArgs(%q, %v): %v", test.name, test.args, err)
						}
						if got != test.want {
							t.Fatalf("CallExportWithArgs(%q, %v) = %#x, want %#x", test.name, test.args, got, test.want)
						}
					}
				})
			}
		})
	}
}

func TestCallExportWithArgsLinuxRejectsUnsupportedCalls(t *testing.T) {
	t.Run("go-c-shared", func(t *testing.T) {
		module := &Module{goRuntime: true}
		for _, args := range [][]uintptr{nil, {1, 2, 3}} {
			got, err := module.CallExportWithArgs("StartW", args...)
			if got != 0 {
				t.Fatalf("CallExportWithArgs() = %#x, want zero", got)
			}
			if !errors.Is(err, ErrGoExportArgumentsUnsupported) {
				t.Fatalf("CallExportWithArgs() error = %v, want %v", err, ErrGoExportArgumentsUnsupported)
			}
		}
	})

	t.Run("too-many-arguments", func(t *testing.T) {
		module := &Module{}
		got, err := module.CallExportWithArgs("not-resolved", 1, 2, 3, 4)
		if got != 0 {
			t.Fatalf("CallExportWithArgs() = %#x, want zero", got)
		}
		if err == nil || !strings.Contains(err.Error(), "maximum is 3") {
			t.Fatalf("CallExportWithArgs() error = %v, want argument-limit error", err)
		}
	})
}

func loadLinuxCallArgsFixture(t *testing.T, mode string, payload []byte, path string) *Module {
	t.Helper()
	switch mode {
	case "legacy":
		module, err := LoadLibrary(payload)
		if err != nil {
			t.Fatalf("LoadLibrary(%s): %v", path, err)
		}
		return module
	case "recursive":
		module, err := LoadLibraryRecursive(payload, path, func(request DependencyRequest) (Dependency, error) {
			return Dependency{}, fmt.Errorf("%w: %s", ErrDependencyNotFound, request.Name)
		})
		if err != nil {
			t.Fatalf("LoadLibraryRecursive(%s): %v", path, err)
		}
		return module
	default:
		t.Fatalf("unknown Linux call-arguments load mode %q", mode)
		return nil
	}
}

func buildLinuxCallArgsCFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("zig"); err != nil {
		t.Skip("zig not found in PATH")
	}

	dir := t.TempDir()
	source := filepath.Join(dir, "call_args.c")
	output := filepath.Join(dir, "libreflektor_call_args_c.so")
	if err := os.WriteFile(source, []byte(linuxCallArgsCSource), 0o600); err != nil {
		t.Fatalf("write C call-arguments fixture: %v", err)
	}

	cmd := exec.Command("zig", "cc",
		"-target", linuxCallArgsZigTarget(t),
		"-shared", "-fPIC", "-nostdlib",
		"-Wl,-z,now", "-Wl,-z,defs",
		"-O2", "-g0",
		"-o", output,
		source,
	)
	cmd.Env = append(
		os.Environ(),
		"ZIG_GLOBAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-global-cache"),
		"ZIG_LOCAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-local-cache"),
	)
	if buildOutput, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build C call-arguments fixture: %v\n%s", err, buildOutput)
	}
	return output
}

func buildLinuxCallArgsRustFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Skip("rustc not found in PATH")
	}

	dir := t.TempDir()
	source := filepath.Join(dir, "call_args.rs")
	output := filepath.Join(dir, "libreflektor_call_args_rust.so")
	sourceData, err := os.ReadFile(filepath.Join("..", "testdata", "rust", "native_args.rs"))
	if err != nil {
		t.Fatalf("read Rust call-arguments fixture source: %v", err)
	}
	if err := os.WriteFile(source, sourceData, 0o600); err != nil {
		t.Fatalf("write Rust call-arguments fixture: %v", err)
	}

	cmd := exec.Command("rustc",
		"--crate-name", "reflektor_call_args",
		"--crate-type", "cdylib",
		"--edition", "2021",
		"-C", "panic=abort",
		"-C", "opt-level=2",
		"-C", "strip=symbols",
		"-C", "link-arg=-Wl,-z,now",
		"-o", output,
		source,
	)
	if buildOutput, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build Rust call-arguments fixture: %v\n%s", err, buildOutput)
	}
	return output
}

func linuxCallArgsZigTarget(t *testing.T) string {
	t.Helper()
	switch runtime.GOARCH {
	case "386":
		return "x86-linux-gnu"
	case "amd64":
		return "x86_64-linux-gnu"
	case "arm64":
		return "aarch64-linux-gnu"
	default:
		t.Fatalf("unsupported Linux call-arguments GOARCH %s", runtime.GOARCH)
		return ""
	}
}
