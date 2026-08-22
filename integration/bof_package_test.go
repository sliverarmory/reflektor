package reflektor_test

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestBOFPackageDependencyGraphIsIsolated(t *testing.T) {
	const modulePath = "github.com/sliverarmory/reflektor"

	rootDependencies := listPackageDependencies(t, "..")
	for _, dependency := range rootDependencies {
		if dependency == modulePath+"/bof" || strings.HasPrefix(dependency, modulePath+"/bof/") ||
			dependency == modulePath+"/internal/bofloader" || strings.HasPrefix(dependency, modulePath+"/internal/bofloader/") {
			t.Fatalf("root shared-library dependency graph includes BOF package %q", dependency)
		}
	}

	sawBackend := false
	for _, dependency := range listPackageDependencies(t, "../bof") {
		switch {
		case dependency == modulePath:
			t.Fatalf("bof dependency graph includes root shared-library package %q", dependency)
		case dependency == modulePath+"/memmod" || strings.HasPrefix(dependency, modulePath+"/memmod/"):
			t.Fatalf("bof dependency graph includes shared-library backend %q", dependency)
		case dependency == modulePath+"/native" || strings.HasPrefix(dependency, modulePath+"/native/"):
			t.Fatalf("bof dependency graph includes native shared-library package %q", dependency)
		case dependency == modulePath+"/internal/bofloader":
			sawBackend = true
		}
	}
	if !sawBackend {
		t.Fatal("bof dependency graph does not include internal/bofloader")
	}
}

func listPackageDependencies(t *testing.T, packagePath string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", packagePath)
	cmd.Env = overrideEnv(os.Environ(), map[string]string{
		"GOOS":        runtime.GOOS,
		"GOARCH":      runtime.GOARCH,
		"CGO_ENABLED": "0",
		"GOFLAGS":     "",
		"GOWORK":      "off",
	})
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s with CGO_ENABLED=0: %v\n%s", packagePath, err, output)
	}
	return strings.Fields(string(output))
}
