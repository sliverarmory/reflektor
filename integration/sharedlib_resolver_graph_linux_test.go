//go:build linux && amd64

package reflektor_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/sliverarmory/reflektor"
)

func TestRecursiveBytesResolverDiamondAndCycleLinux(t *testing.T) {
	requireCommand(t, "zig")
	target, ok := zigTargetFor("linux", runtime.GOARCH)
	if !ok {
		t.Fatalf("unsupported Linux target %s", runtime.GOARCH)
	}

	t.Run("diamond", func(t *testing.T) {
		dir := t.TempDir()
		buildResolverGraphSO(t, target, dir, "libleaf.so", `int LeafValue(void) { return 2; }`)
		buildResolverGraphSO(t, target, dir, "liba.so", `extern int LeafValue(void); int AValue(void) { return LeafValue() + 1; }`, "-lleaf")
		buildResolverGraphSO(t, target, dir, "libb.so", `extern int LeafValue(void); int BValue(void) { return LeafValue() + 2; }`, "-lleaf")
		rootPath := buildResolverGraphSO(t, target, dir, "root.so", `extern int AValue(void); extern int BValue(void); int GraphValue(void) { return AValue() + BValue(); }`, "-la", "-lb")
		loadResolverGraphWithoutFiles(t, rootPath, []string{"liba.so", "libleaf.so", "libb.so"}, 7)
	})

	t.Run("cycle", func(t *testing.T) {
		dir := t.TempDir()
		buildResolverGraphSO(t, target, dir, "libb.so", `int BValue(void) { return 5; }`)
		buildResolverGraphSO(t, target, dir, "liba.so", `int AValue(void) { return 7; }`, "-lb")
		// Relink B against the final A. A's DT_NEEDED still names B's stable
		// SONAME, producing A -> B -> A without recursive function calls.
		buildResolverGraphSO(t, target, dir, "libb.so", `int BValue(void) { return 5; }`, "-la")
		rootPath := buildResolverGraphSO(t, target, dir, "root.so", `extern int AValue(void); int GraphValue(void) { return AValue(); }`, "-la")
		loadResolverGraphWithoutFiles(t, rootPath, []string{"liba.so", "libb.so"}, 7)
	})
}

func buildResolverGraphSO(t *testing.T, target, dir, name, source string, links ...string) string {
	t.Helper()
	sourcePath := filepath.Join(dir, name+".c")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write %s source: %v", name, err)
	}
	path := filepath.Join(dir, name)
	args := []string{
		"cc", "-target", target, "-shared", "-fPIC", "-nostdlib", "-O2", "-g0",
		"-Wl,-z,now", "-Wl,-z,defs", "-Wl,-soname," + name,
		"-o", path, sourcePath, "-L" + dir, "-Wl,--no-as-needed",
	}
	args = append(args, links...)
	cmd := exec.Command("zig", args...)
	cmd.Env = append(os.Environ(),
		"ZIG_GLOBAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-global-cache"),
		"ZIG_LOCAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-local-cache"),
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, output)
	}
	return path
}

func loadResolverGraphWithoutFiles(t *testing.T, rootPath string, wantOrder []string, wantValue uintptr) {
	t.Helper()
	dir := filepath.Dir(rootPath)
	images := make(map[string][]byte, len(wantOrder)+1)
	for _, name := range append([]string{"root.so"}, wantOrder...) {
		if _, exists := images[name]; exists {
			continue
		}
		image, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		images[name] = image
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove dependency files before load: %v", err)
	}

	var order []string
	library, err := reflektor.LoadLibraryRecursiveWithResolver(images["root.so"], rootPath, func(request reflektor.DependencyRequest) (reflektor.Dependency, error) {
		name := filepath.Base(request.Name)
		image, ok := images[name]
		if !ok {
			return reflektor.Dependency{}, reflektor.ErrDependencyNotFound
		}
		order = append(order, name)
		return reflektor.Dependency{Data: image, Path: filepath.Join(dir, name)}, nil
	})
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	t.Cleanup(func() { _ = library.Close() })
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("resolver order = %#v, want %#v", order, wantOrder)
	}
	value, err := library.CallExportWithArgs("GraphValue")
	if err != nil {
		t.Fatalf("call GraphValue: %v", err)
	}
	if value != wantValue {
		t.Fatalf("GraphValue = %d, want %d", value, wantValue)
	}
}
