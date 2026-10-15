//go:build (darwin && (amd64 || arm64)) || (freebsd && (amd64 || arm64)) || (linux && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64)) || (windows && (386 || amd64 || arm64))

package reflektor_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCLIArgumentsAndRecursiveLoad(t *testing.T) {
	requireCommand(t, "zig")

	outDir := t.TempDir()
	cliName := "reflektor"
	if runtime.GOOS == "windows" {
		cliName += ".exe"
	}
	cliPath := filepath.Join(outDir, cliName)
	build := exec.Command("go", "build", "-o", cliPath, "../cli")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}

	argsLibrary := buildCSharedLib(t, outDir, runtime.GOOS, runtime.GOARCH, "cli_args", filepath.Join("..", "testdata", "c", "cli_args.c"))
	if output := runCLI(t, cliPath, nil,
		argsLibrary, "--call-export", "ReflektorCLIAdd", "--arg", "0x2", "--arg", "3", "--arg", "4"); output != "result: 0x1b0" {
		t.Fatalf("CLI argument result = %q, want %q", output, "result: 0x1b0")
	}
	if output := runCLI(t, cliPath, nil,
		argsLibrary, "--call-export", "ReflektorCLIZero", "--print-result"); output != "result: 0x7" {
		t.Fatalf("CLI zero-argument result = %q, want %q", output, "result: 0x7")
	}
	graphDir := filepath.Join(outDir, "graph")
	if err := os.Mkdir(graphDir, 0o755); err != nil {
		t.Fatalf("create recursive graph directory: %v", err)
	}
	libraries := buildRecursiveSharedLibs(t, graphDir, runtime.GOOS, runtime.GOARCH)
	markerPath := filepath.Join(outDir, "marker.txt")
	if output := runCLI(t, cliPath, []string{"REFLEKTOR_MARKER=" + markerPath},
		libraries.root, "--recursive"); output != "ok" {
		t.Fatalf("CLI recursive result = %q, want %q", output, "ok")
	}
	marker, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read CLI recursive marker: %v", err)
	}
	if string(marker) != "ok" {
		t.Fatalf("CLI recursive marker = %q, want %q", marker, "ok")
	}
}

func TestCLIBOFExecution(t *testing.T) {
	target, ok := nativeBOFTarget()
	if !ok {
		t.Fatalf("missing BOF fixture target for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	outDir := t.TempDir()
	cliName := "reflektor"
	if runtime.GOOS == "windows" {
		cliName += ".exe"
	}
	cliPath := filepath.Join(outDir, cliName)
	build := exec.Command("go", "build", "-o", cliPath, "../cli")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	bofPath := buildBOFFixture(t, outDir, target)
	output := runCLI(t, cliPath, nil, "bof", bofPath,
		"--bof-arg", "int32:0x12345678", "--bof-arg", "int16:0x1234", "--bof-arg", "string:bof")
	if !strings.Contains(output, "type=0 data=\"bof-e2e-ok\"") || strings.Contains(output, "bof-e2e-invalid-arguments") {
		t.Fatalf("CLI BOF output = %q", output)
	}
}

func runCLI(t *testing.T, path string, env []string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("CLI timed out: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("CLI failed: %v\n%s", err, output)
	}
	return strings.TrimSpace(string(output))
}
