package reflektor_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func buildOneGoSharedLib(t *testing.T, outDir string, goos string, goarch string) string {
	t.Helper()

	ext, err := sharedLibExt(goos)
	if err != nil {
		t.Fatalf("build go shared library target=%s/%s: %v", goos, goarch, err)
	}

	outputPath := filepath.Join(outDir, fmt.Sprintf("basic_go_%s-%s.%s", goos, goarch, ext))
	sourcePath := "./testdata/go/basic"

	args := []string{
		"build",
		"-buildmode=c-shared",
		"-trimpath",
		"-o", outputPath,
		sourcePath,
	}

	baseEnv := overrideEnv(os.Environ(), map[string]string{
		"GOOS":        goos,
		"GOARCH":      goarch,
		"CGO_ENABLED": "1",
		"GOCACHE":     filepath.Join(os.TempDir(), "reflektor-go-build-cache"),
	})

	var (
		out []byte
	)
	if _, err := exec.LookPath("zig"); err == nil {
		cmd := exec.Command("go", args...)
		cc := "zig cc"
		cxx := "zig c++"
		if target, ok := zigTargetFor(goos, goarch); ok {
			cc = "zig cc -target " + target
			cxx = "zig c++ -target " + target
		}
		cmd.Env = overrideEnv(baseEnv, map[string]string{
			"CC":  cc,
			"CXX": cxx,
		})
		out, err = cmd.CombinedOutput()
		if err == nil {
			cleanupGoSharedSidecars(outputPath, ext)
			return outputPath
		}
		t.Logf("go build with zig cc failed for %s/%s, retrying with default compiler: %v\n%s", goos, goarch, err, out)
	}

	cmd := exec.Command("go", args...)
	cmd.Env = baseEnv
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build go shared lib target=%s/%s: %v\n%s", goos, goarch, err, out)
	}

	cleanupGoSharedSidecars(outputPath, ext)
	return outputPath
}

func zigTargetFor(goos string, goarch string) (string, bool) {
	switch {
	case goos == "darwin" && goarch == "amd64":
		return "x86_64-macos", true
	case goos == "darwin" && goarch == "arm64":
		return "aarch64-macos", true
	case goos == "linux" && goarch == "386":
		return "x86-linux-gnu", true
	case goos == "linux" && goarch == "amd64":
		return "x86_64-linux-gnu", true
	case goos == "linux" && goarch == "arm64":
		return "aarch64-linux-gnu", true
	case goos == "windows" && goarch == "386":
		return "x86-windows-gnu", true
	case goos == "windows" && goarch == "amd64":
		return "x86_64-windows-gnu", true
	case goos == "windows" && goarch == "arm64":
		return "aarch64-windows-gnu", true
	default:
		return "", false
	}
}

func sharedLibExt(goos string) (string, error) {
	switch goos {
	case "darwin":
		return "dylib", nil
	case "linux":
		return "so", nil
	case "windows":
		return "dll", nil
	default:
		return "", fmt.Errorf("unsupported target os: %s", goos)
	}
}

func cleanupGoSharedSidecars(outputPath string, ext string) {
	base := strings.TrimSuffix(outputPath, "."+ext)
	_ = os.Remove(base + ".h")
	if runtime.GOOS == "windows" || strings.EqualFold(ext, "dll") {
		_ = os.Remove(base + ".lib")
		_ = os.Remove(base + ".exp")
		_ = os.Remove(base + ".pdb")
	}
}

func overrideEnv(base []string, overrides map[string]string) []string {
	block := make(map[string]struct{}, len(overrides))
	for key := range overrides {
		block[key] = struct{}{}
	}

	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		if _, drop := block[kv[:eq]]; drop {
			continue
		}
		out = append(out, kv)
	}

	for key, value := range overrides {
		out = append(out, key+"="+value)
	}
	return out
}
