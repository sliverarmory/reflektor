//go:build freebsd && (amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sliverarmory/reflektor"
)

const freeBSDLDLibraryPathDependency = "libreflektor_freebsd_ldpath_dependency.so"

func TestLoadLibraryRecursiveFreeBSDSystemLibraryDelegatesToRTLD(t *testing.T) {
	requireCommand(t, "cc")
	requireCommand(t, "objdump")
	requireCommand(t, "procstat")
	t.Setenv("LD_LIBRARY_PATH", "")

	systemLibrary, err := filepath.EvalSymlinks("/usr/lib/libarchive.so")
	if err != nil {
		t.Fatalf("resolve FreeBSD base-system libarchive: %v", err)
	}
	systemLibrary, err = filepath.Abs(systemLibrary)
	if err != nil {
		t.Fatalf("resolve absolute FreeBSD base-system libarchive path: %v", err)
	}
	if filepath.Dir(systemLibrary) != "/usr/lib" {
		t.Fatalf("FreeBSD base-system libarchive path = %q, want /usr/lib", systemLibrary)
	}
	if freeBSDProcstatContainsPath(t, systemLibrary) {
		t.Fatalf("FreeBSD base-system dependency was already mapped before recursive load: %s", systemLibrary)
	}

	rootPath := filepath.Join(t.TempDir(), "reflektor_freebsd_system_dependency.so")
	buildFreeBSDNativeSharedLibrary(t, rootPath,
		"-Wl,-soname,reflektor_freebsd_system_dependency.so",
		filepath.Join("..", "testdata", "c", "freebsd_system_dependency.c"),
		"-Wl,--no-as-needed", systemLibrary,
	)
	needed := runCmd(t, "objdump", "-p", rootPath)
	if !strings.Contains(needed, filepath.Base(systemLibrary)) {
		t.Fatalf("FreeBSD system-dependency fixture does not import %s:\n%s", filepath.Base(systemLibrary), needed)
	}

	markerPath := filepath.Join(t.TempDir(), "freebsd-system-dependency-marker")
	t.Setenv("REFLEKTOR_MARKER", markerPath)
	library, err := reflektor.LoadLibraryFileRecursive(rootPath)
	if err != nil {
		t.Fatalf("LoadLibraryFileRecursive(%s): %v", rootPath, err)
	}
	t.Cleanup(func() { _ = library.Close() })
	if !freeBSDProcstatContainsPath(t, systemLibrary) {
		t.Fatalf("FreeBSD system dependency was not delegated to rtld at %s", systemLibrary)
	}
	if err := library.CallExport("StartW"); err != nil {
		t.Fatalf("CallExport(StartW): %v", err)
	}
	assertFreeBSDRecursiveMarker(t, markerPath)
}

func TestLoadLibraryRecursiveFreeBSDLDLibraryPathDependency(t *testing.T) {
	requireCommand(t, "cc")
	requireCommand(t, "objdump")
	requireCommand(t, "procstat")

	stateDir := t.TempDir()
	rootDir := filepath.Join(stateDir, "root")
	dependencyDir := filepath.Join(stateDir, "ld-library-path")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root fixture directory: %v", err)
	}
	if err := os.MkdirAll(dependencyDir, 0o755); err != nil {
		t.Fatalf("create LD_LIBRARY_PATH fixture directory: %v", err)
	}

	dependencyPath := filepath.Join(dependencyDir, freeBSDLDLibraryPathDependency)
	buildFreeBSDNativeSharedLibrary(t, dependencyPath,
		"-Wl,-soname,"+freeBSDLDLibraryPathDependency,
		filepath.Join("..", "testdata", "c", "freebsd_ld_library_path_dependency.c"),
	)
	rootPath := filepath.Join(rootDir, "reflektor_freebsd_ldpath_root.so")
	buildFreeBSDNativeSharedLibrary(t, rootPath,
		"-Wl,-soname,reflektor_freebsd_ldpath_root.so",
		filepath.Join("..", "testdata", "c", "freebsd_ld_library_path_root.c"),
		"-L"+dependencyDir, "-Wl,--no-as-needed", "-lreflektor_freebsd_ldpath_dependency",
	)
	dynamic := runCmd(t, "objdump", "-p", rootPath)
	if !strings.Contains(dynamic, freeBSDLDLibraryPathDependency) {
		t.Fatalf("FreeBSD LD_LIBRARY_PATH fixture is missing dependency %s:\n%s", freeBSDLDLibraryPathDependency, dynamic)
	}
	if strings.Contains(dynamic, "RPATH") || strings.Contains(dynamic, "RUNPATH") {
		t.Fatalf("FreeBSD LD_LIBRARY_PATH fixture unexpectedly has an embedded search path:\n%s", dynamic)
	}

	t.Setenv("LD_LIBRARY_PATH", dependencyDir)
	markerPath := filepath.Join(stateDir, "freebsd-ld-library-path-marker")
	t.Setenv("REFLEKTOR_MARKER", markerPath)
	library, err := reflektor.LoadLibraryFileRecursive(rootPath)
	if err != nil {
		t.Fatalf("LoadLibraryFileRecursive(%s) through LD_LIBRARY_PATH: %v", rootPath, err)
	}
	t.Cleanup(func() { _ = library.Close() })
	assertRecursiveDependenciesNotNativeLoaded(t, dependencyDir)

	hiddenDir := filepath.Join(stateDir, "ld-library-path-hidden")
	if err := os.Rename(dependencyDir, hiddenDir); err != nil {
		t.Fatalf("hide LD_LIBRARY_PATH dependency after recursive load: %v", err)
	}
	if err := library.CallExport("StartW"); err != nil {
		t.Fatalf("CallExport(StartW) after hiding LD_LIBRARY_PATH dependency: %v", err)
	}
	assertFreeBSDRecursiveMarker(t, markerPath)
}

func buildFreeBSDNativeSharedLibrary(t *testing.T, outputPath string, args ...string) {
	t.Helper()
	commandArgs := []string{
		"-shared", "-fPIC", "-O2", "-g0",
		"-Wl,-z,now", "-Wl,-z,defs",
		"-o", outputPath,
	}
	commandArgs = append(commandArgs, args...)
	cmd := exec.Command("cc", commandArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build native FreeBSD shared library %s: %v\n%s", outputPath, err, output)
	}
}

func freeBSDProcstatContainsPath(t *testing.T, path string) bool {
	t.Helper()
	output, err := exec.Command("procstat", "-v", strconv.Itoa(os.Getpid())).CombinedOutput()
	if err != nil {
		t.Fatalf("procstat -v current process: %v\n%s", err, output)
	}
	return bytes.Contains(output, []byte(path))
}

func assertFreeBSDRecursiveMarker(t *testing.T, markerPath string) {
	t.Helper()
	marker, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read FreeBSD recursive marker: %v", err)
	}
	if !bytes.Equal(marker, []byte("ok")) {
		t.Fatalf("FreeBSD recursive marker = %q, want ok", marker)
	}
}
