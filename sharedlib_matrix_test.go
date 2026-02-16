package reflektor_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type sharedLibTarget struct {
	goos      string
	goarch    string
	ext       string
	fileProbe string
}

var sharedLibTargets = []sharedLibTarget{
	{goos: "darwin", goarch: "amd64", ext: "dylib", fileProbe: "x86_64"},
	{goos: "darwin", goarch: "arm64", ext: "dylib", fileProbe: "arm64"},
	{goos: "linux", goarch: "386", ext: "so", fileProbe: "Intel 80386"},
	{goos: "linux", goarch: "amd64", ext: "so", fileProbe: "x86-64"},
	{goos: "linux", goarch: "arm64", ext: "so", fileProbe: "ARM aarch64"},
	{goos: "windows", goarch: "386", ext: "dll", fileProbe: "Intel 80386"},
	{goos: "windows", goarch: "amd64", ext: "dll", fileProbe: "x86-64"},
	{goos: "windows", goarch: "arm64", ext: "dll", fileProbe: "Aarch64"},
}

func TestBuildCSharedLibraryMatrix(t *testing.T) {
	requireCommand(t, "zig")
	requireCommand(t, "file")
	requireCommand(t, "nm")
	requireCommand(t, "objdump")

	outDir := t.TempDir()

	for _, target := range sharedLibTargets {
		target := target
		t.Run(fmt.Sprintf("%s-%s", target.goos, target.goarch), func(t *testing.T) {
			path := buildOneSharedLib(t, outDir, target.goos, target.goarch)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat %s: %v", path, err)
			}
			if info.Size() == 0 {
				t.Fatalf("empty output file: %s", path)
			}

			fileOut := runCmd(t, "file", path)
			if !strings.Contains(fileOut, target.fileProbe) {
				t.Fatalf("unexpected architecture probe for %s: want substring %q, got %q", path, target.fileProbe, fileOut)
			}

			switch target.goos {
			case "windows":
				exportOut := runCmd(t, "objdump", "-p", path)
				if !strings.Contains(exportOut, "StartW") {
					t.Fatalf("expected exported symbol StartW in %s", path)
				}
			case "darwin":
				nmOut := runCmd(t, "nm", path)
				if !strings.Contains(nmOut, "_StartW") {
					t.Fatalf("expected exported symbol _StartW in %s", path)
				}
			default:
				nmOut := runCmd(t, "nm", path)
				if !strings.Contains(nmOut, " StartW") && !strings.Contains(nmOut, "\tStartW") {
					t.Fatalf("expected exported symbol StartW in %s", path)
				}
			}
		})
	}
}

func buildOneSharedLib(t *testing.T, outDir string, goos string, goarch string) string {
	t.Helper()

	var (
		zigTarget string
		ext       string
	)

	switch {
	case goos == "darwin" && goarch == "amd64":
		zigTarget, ext = "x86_64-macos", "dylib"
	case goos == "darwin" && goarch == "arm64":
		zigTarget, ext = "aarch64-macos", "dylib"
	case goos == "linux" && goarch == "386":
		zigTarget, ext = "x86-linux-gnu", "so"
	case goos == "linux" && goarch == "amd64":
		zigTarget, ext = "x86_64-linux-gnu", "so"
	case goos == "linux" && goarch == "arm64":
		zigTarget, ext = "aarch64-linux-gnu", "so"
	case goos == "windows" && goarch == "386":
		zigTarget, ext = "x86-windows-gnu", "dll"
	case goos == "windows" && goarch == "amd64":
		zigTarget, ext = "x86_64-windows-gnu", "dll"
	case goos == "windows" && goarch == "arm64":
		zigTarget, ext = "aarch64-windows-gnu", "dll"
	default:
		t.Fatalf("unsupported target %s/%s", goos, goarch)
	}

	outputPath := filepath.Join(outDir, fmt.Sprintf("basic_%s-%s.%s", goos, goarch, ext))
	sourcePath := filepath.Join("testdata", "c", "basic.c")

	args := []string{"cc", "-target", zigTarget, "-O2", "-g0"}
	switch goos {
	case "darwin":
		args = append(args, "-dynamiclib", "-fPIC")
	case "linux":
		args = append(args, "-shared", "-fPIC")
	case "windows":
		args = append(args, "-shared")
	default:
		t.Fatalf("unsupported target os %s", goos)
	}
	args = append(args, "-o", outputPath, sourcePath)

	cmd := exec.Command("zig", args...)
	cmd.Env = append(
		os.Environ(),
		"ZIG_GLOBAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-global-cache"),
		"ZIG_LOCAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-local-cache"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build shared lib target=%s/%s: %v\n%s", goos, goarch, err, output)
	}

	// Zig emits COFF sidecars for windows builds; keep test temp dirs tidy.
	if goos == "windows" {
		base := strings.TrimSuffix(outputPath, ".dll")
		_ = os.Remove(base + ".pdb")
		_ = os.Remove(filepath.Join(outDir, "basic.lib"))
	}
	return outputPath
}

func runCmd(t *testing.T, name string, args ...string) string {
	t.Helper()

	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not found in PATH", name)
	}
}
