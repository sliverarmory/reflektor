//go:build darwin && (amd64 || arm64)

package memmod

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

type darwinTestLinkedDylib struct {
	command uint32
	name    string
}

func TestDarwinRecursiveLoadGraphFromCapturedBytes(t *testing.T) {
	if runtime.GOARCH == "amd64" {
		if translated, err := unix.SysctlUint32("sysctl.proc_translated"); err == nil && translated == 1 {
			t.Skip("darwin/amd64 under Rosetta is not supported by the dyld4-only in-memory loader")
		}
	}
	if os.Getenv("REFLEKTOR_DARWIN_RECURSIVE_HELPER") == "1" {
		runDarwinRecursiveLoadGraphHelper(t)
		return
	}
	compiler, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang is required for the native recursive Mach-O integration test")
	}

	directory := t.TempDir()
	dependencyPath := filepath.Join(directory, "librecursive_child.dylib")
	rootPath := filepath.Join(directory, "librecursive_root.dylib")
	markerPath := filepath.Join(directory, "recursive-marker.txt")
	childSource := filepath.Join(directory, "child.c")
	rootSource := filepath.Join(directory, "root.c")
	if err := os.WriteFile(childSource, []byte(`
__attribute__((visibility("default"))) int RecursiveChildValue(void) {
    return 1337;
}
`), 0o600); err != nil {
		t.Fatalf("write child fixture: %v", err)
	}
	if err := os.WriteFile(rootSource, []byte(`
#include <stdio.h>
#include <stdlib.h>

extern int RecursiveChildValue(void);

__attribute__((visibility("default"))) void StartW(void) {
    const char *path = getenv("REFLEKTOR_MARKER");
    if ( path == NULL || RecursiveChildValue() != 1337 )
        return;
    FILE *file = fopen(path, "wb");
    if ( file == NULL )
        return;
    fwrite("ok", 1, 2, file);
    fclose(file);
}
`), 0o600); err != nil {
		t.Fatalf("write root fixture: %v", err)
	}

	runDarwinFixtureCompiler(t, compiler,
		"-dynamiclib", "-fPIC", "-O2", "-g0",
		"-Wl,-install_name,"+dependencyPath,
		"-o", dependencyPath, childSource,
	)
	runDarwinFixtureCompiler(t, compiler,
		"-dynamiclib", "-fPIC", "-O2", "-g0", "-Wl,-bind_at_load",
		"-o", rootPath, rootSource, dependencyPath,
	)

	command := exec.Command(os.Args[0], "-test.run=^TestDarwinRecursiveLoadGraphFromCapturedBytes$", "-test.count=1", "-test.v")
	command.Env = append(os.Environ(),
		"REFLEKTOR_DARWIN_RECURSIVE_HELPER=1",
		"REFLEKTOR_RECURSIVE_ROOT="+rootPath,
		"REFLEKTOR_RECURSIVE_DEPENDENCY="+dependencyPath,
		"REFLEKTOR_MARKER="+markerPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("recursive Mach-O helper failed: %v\n%s", err, output)
	}
	marker, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read recursive marker: %v\nhelper output:\n%s", err, output)
	}
	if !bytes.Equal(marker, []byte("ok")) {
		t.Fatalf("unexpected recursive marker: got=%q want=%q\nhelper output:\n%s", marker, []byte("ok"), output)
	}
}

func runDarwinRecursiveLoadGraphHelper(t *testing.T) {
	rootPath := os.Getenv("REFLEKTOR_RECURSIVE_ROOT")
	dependencyPath := os.Getenv("REFLEKTOR_RECURSIVE_DEPENDENCY")
	rootImage, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatalf("read recursive root: %v", err)
	}
	reader := func(request DependencyRequest) (Dependency, error) {
		if request.Name != dependencyPath {
			return Dependency{}, fmt.Errorf("unexpected dependency request %q", request.Name)
		}
		image, readErr := os.ReadFile(dependencyPath)
		if readErr != nil {
			return Dependency{}, readErr
		}
		return Dependency{Data: image, Path: dependencyPath}, nil
	}
	module, err := LoadLibraryRecursive(rootImage, rootPath, reader)
	if err != nil {
		t.Fatalf("LoadLibraryRecursive: %v", err)
	}
	defer module.Free()

	// Once planning returns, remove the only disk path dyld could use. A passing
	// export proves the root bound to the pre-registered anonymous child image.
	capturedPath := dependencyPath + ".captured"
	if err := os.Rename(dependencyPath, capturedPath); err != nil {
		t.Fatalf("hide captured dependency: %v", err)
	}
	if err := module.CallExport("StartW"); err != nil {
		t.Fatalf("CallExport(StartW): %v", err)
	}
	if err := module.CallExport("StartW"); err != nil {
		t.Fatalf("repeat CallExport(StartW): %v", err)
	}

	// Darwin mappings are process-resident, so also verify that a fresh Module
	// can reuse an identical install-name graph without remapping on every call.
	module.Free()
	if err := os.Rename(capturedPath, dependencyPath); err != nil {
		t.Fatalf("restore captured dependency for reload: %v", err)
	}
	reloaded, err := LoadLibraryRecursive(rootImage, rootPath, reader)
	if err != nil {
		t.Fatalf("reload identical recursive graph: %v", err)
	}
	defer reloaded.Free()
	if err := os.Rename(dependencyPath, capturedPath+"-reload"); err != nil {
		t.Fatalf("hide reloaded dependency: %v", err)
	}
	if err := reloaded.CallExport("StartW"); err != nil {
		t.Fatalf("CallExport(StartW) after identical graph reload: %v", err)
	}
}

func runDarwinFixtureCompiler(t *testing.T, compiler string, arguments ...string) {
	t.Helper()
	command := exec.Command(compiler, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("compile recursive fixture: %v\n%s", err, output)
	}
}

func TestDarwinRecursivePlanResolvesRPathAndCapturesDependency(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "root.dylib")
	dependencyPath := filepath.Join(filepath.Dir(rootPath), "deps", "libchild.dylib")
	root := darwinTestMachO(t, "@rpath/libroot.dylib", []string{"@loader_path/deps"}, []darwinTestLinkedDylib{
		{command: darwinLCLoadDylib, name: "@rpath/libchild.dylib"},
	})
	child := darwinTestMachO(t, "@rpath/libchild.dylib", nil, nil)

	var request DependencyRequest
	module, err := LoadLibraryRecursive(root, rootPath, func(got DependencyRequest) (Dependency, error) {
		request = got
		return Dependency{Data: child, Path: dependencyPath}, nil
	})
	if err != nil {
		t.Fatalf("LoadLibraryRecursive: %v", err)
	}
	t.Cleanup(module.Free)

	wantSearchPath := filepath.Join(filepath.Dir(rootPath), "deps")
	if request.Name != "@rpath/libchild.dylib" || request.ImporterPath != rootPath {
		t.Fatalf("unexpected dependency request: %#v", request)
	}
	if !reflect.DeepEqual(request.SearchPaths, []string{wantSearchPath}) {
		t.Fatalf("unexpected rpath search list: got=%q want=%q", request.SearchPaths, []string{wantSearchPath})
	}
	if module.recursive == nil || len(module.recursive.dependencies) != 1 {
		t.Fatalf("unexpected recursive plan: %#v", module.recursive)
	}
	if got := module.recursive.dependencies[0].path; got != dependencyPath {
		t.Fatalf("unexpected dependency path: got=%q want=%q", got, dependencyPath)
	}
	if len(module.recursive.dependencies[0].image) == 0 {
		t.Fatal("recursive plan did not retain dependency bytes")
	}
}

func TestDarwinRecursivePlanHandlesCycleAndDeduplicatesDiamond(t *testing.T) {
	base := t.TempDir()
	paths := map[string]string{
		"a": filepath.Join(base, "liba.dylib"),
		"b": filepath.Join(base, "libb.dylib"),
		"c": filepath.Join(base, "libc.dylib"),
		"d": filepath.Join(base, "libd.dylib"),
	}
	images := map[string][]byte{
		paths["a"]: darwinTestMachO(t, paths["a"], nil, []darwinTestLinkedDylib{
			{command: darwinLCLoadDylib, name: paths["b"]},
			{command: darwinLCLoadDylib, name: paths["c"]},
		}),
		paths["b"]: darwinTestMachO(t, paths["b"], nil, []darwinTestLinkedDylib{
			{command: darwinLCLoadDylib, name: paths["a"]},
			{command: darwinLCLoadDylib, name: paths["d"]},
		}),
		paths["c"]: darwinTestMachO(t, paths["c"], nil, []darwinTestLinkedDylib{
			{command: darwinLCLoadDylib, name: paths["d"]},
		}),
		paths["d"]: darwinTestMachO(t, paths["d"], nil, nil),
	}

	reads := make(map[string]int)
	module, err := LoadLibraryRecursive(images[paths["a"]], paths["a"], func(request DependencyRequest) (Dependency, error) {
		reads[request.Name]++
		image, ok := images[request.Name]
		if !ok {
			return Dependency{}, fmt.Errorf("%w: %s", ErrDependencyNotFound, request.Name)
		}
		return Dependency{Data: image, Path: request.Name}, nil
	})
	if err != nil {
		t.Fatalf("LoadLibraryRecursive: %v", err)
	}
	t.Cleanup(module.Free)

	if got := len(module.recursive.dependencies); got != 3 {
		t.Fatalf("unexpected dependency count: got=%d want=3", got)
	}
	if reads[paths["a"]] != 0 {
		t.Fatalf("cycle reread the root image %d times", reads[paths["a"]])
	}
	if reads[paths["d"]] != 1 {
		t.Fatalf("diamond dependency read count: got=%d want=1", reads[paths["d"]])
	}
}

func TestDarwinRecursivePlanPreservesWeakMissingAndRejectsStrongMissing(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "root.dylib")
	missingPath := filepath.Join(filepath.Dir(rootPath), "missing.dylib")
	reader := func(request DependencyRequest) (Dependency, error) {
		return Dependency{}, fmt.Errorf("%w: %s", ErrDependencyNotFound, request.Name)
	}

	weakRoot := darwinTestMachO(t, rootPath, nil, []darwinTestLinkedDylib{
		{command: darwinLCLoadWeakDylib, name: missingPath},
	})
	weakModule, err := LoadLibraryRecursive(weakRoot, rootPath, reader)
	if err != nil {
		t.Fatalf("weak LoadLibraryRecursive: %v", err)
	}
	weakModule.Free()

	strongRoot := darwinTestMachO(t, rootPath, nil, []darwinTestLinkedDylib{
		{command: darwinLCLoadDylib, name: missingPath},
	})
	strongModule, err := LoadLibraryRecursive(strongRoot, rootPath, reader)
	if strongModule != nil {
		strongModule.Free()
		t.Fatal("strong missing dependency unexpectedly returned a module")
	}
	if !errors.Is(err, ErrDependencyNotFound) {
		t.Fatalf("strong missing dependency error: got=%v want ErrDependencyNotFound", err)
	}
}

func TestDarwinRecursivePlanKeepsSystemImportsOutOfReader(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "root.dylib")
	const systemLibrary = "/usr/lib/libSystem.B.dylib"
	const missingWeakSystemLibrary = "/usr/lib/libReflektorMissingWeak.dylib"
	root := darwinTestMachO(t, rootPath, nil, []darwinTestLinkedDylib{
		{command: darwinLCLoadDylib, name: systemLibrary},
		{command: darwinLCLoadWeakDylib, name: missingWeakSystemLibrary},
	})

	module, err := LoadLibraryRecursive(root, rootPath, func(request DependencyRequest) (Dependency, error) {
		t.Fatalf("system dependency unexpectedly reached reader: %#v", request)
		return Dependency{}, ErrDependencyNotFound
	})
	if err != nil {
		t.Fatalf("LoadLibraryRecursive: %v", err)
	}
	t.Cleanup(module.Free)
	want := []darwinSystemImport{
		{name: systemLibrary},
		{name: missingWeakSystemLibrary, weak: true},
	}
	if got := module.recursive.systemImports; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected system imports: got=%#v want=%#v", got, want)
	}
}

func TestDarwinRecursivePlanFallsBackToExpandedRPathSystemImport(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "root.dylib")
	const systemDirectory = "/usr/lib/swift"
	const linkedName = "@rpath/libReflektorSharedCacheOnly.dylib"
	root := darwinTestMachO(t, rootPath, []string{systemDirectory}, []darwinTestLinkedDylib{
		{command: darwinLCLoadDylib, name: linkedName},
	})

	readerCalls := 0
	module, err := LoadLibraryRecursive(root, rootPath, func(request DependencyRequest) (Dependency, error) {
		readerCalls++
		return Dependency{}, fmt.Errorf("%w: %s", ErrDependencyNotFound, request.Name)
	})
	if err != nil {
		t.Fatalf("LoadLibraryRecursive: %v", err)
	}
	t.Cleanup(module.Free)
	if readerCalls != 1 {
		t.Fatalf("expanded system import reader calls = %d, want 1", readerCalls)
	}
	want := []darwinSystemImport{{name: filepath.Join(systemDirectory, "libReflektorSharedCacheOnly.dylib")}}
	if got := module.recursive.systemImports; !reflect.DeepEqual(got, want) {
		t.Fatalf("expanded system imports: got=%#v want=%#v", got, want)
	}
}

func TestInspectDarwinDependenciesRecognizesLinkedCommandKinds(t *testing.T) {
	image := darwinTestMachO(t, "/tmp/libroot.dylib", []string{"/tmp/rpath"}, []darwinTestLinkedDylib{
		{command: darwinLCLoadDylib, name: "/tmp/regular.dylib"},
		{command: darwinLCLoadWeakDylib, name: "/tmp/weak.dylib"},
		{command: darwinLCReexportDylib, name: "/tmp/reexport.dylib"},
		{command: darwinLCLazyLoadDylib, name: "/tmp/lazy.dylib"},
		{command: darwinLCLoadUpwardDylib, name: "/tmp/upward.dylib"},
	})
	metadata, err := inspectDarwinDependencies(image)
	if err != nil {
		t.Fatalf("inspectDarwinDependencies: %v", err)
	}
	if metadata.installName != "/tmp/libroot.dylib" {
		t.Fatalf("unexpected install name: %q", metadata.installName)
	}
	if !reflect.DeepEqual(metadata.rpaths, []string{"/tmp/rpath"}) {
		t.Fatalf("unexpected rpaths: %q", metadata.rpaths)
	}
	if len(metadata.linked) != 5 || !metadata.linked[1].weak {
		t.Fatalf("unexpected linked dylibs: %#v", metadata.linked)
	}
	for index, linked := range metadata.linked {
		if index != 1 && linked.weak {
			t.Fatalf("dependency %d unexpectedly marked weak: %#v", index, linked)
		}
	}
}

func darwinTestMachO(t *testing.T, installName string, rpaths []string, linked []darwinTestLinkedDylib) []byte {
	t.Helper()

	commands := make([][]byte, 0, 1+len(rpaths)+len(linked))
	if installName != "" {
		commands = append(commands, darwinTestDylibCommand(darwinLCIDDylib, installName))
	}
	for _, rpath := range rpaths {
		commands = append(commands, darwinTestRPathCommand(rpath))
	}
	for _, dependency := range linked {
		commands = append(commands, darwinTestDylibCommand(dependency.command, dependency.name))
	}

	commandSize := 0
	for _, command := range commands {
		commandSize += len(command)
	}
	image := make([]byte, 32+commandSize)
	binary.LittleEndian.PutUint32(image[0:4], macho.Magic64)
	binary.LittleEndian.PutUint32(image[4:8], uint32(darwinTestCPU(t)))
	binary.LittleEndian.PutUint32(image[8:12], 0)
	binary.LittleEndian.PutUint32(image[12:16], uint32(macho.TypeDylib))
	binary.LittleEndian.PutUint32(image[16:20], uint32(len(commands)))
	binary.LittleEndian.PutUint32(image[20:24], uint32(commandSize))
	binary.LittleEndian.PutUint32(image[24:28], macho.FlagDyldLink|macho.FlagTwoLevel)

	offset := 32
	for _, command := range commands {
		copy(image[offset:], command)
		offset += len(command)
	}
	return image
}

func darwinTestDylibCommand(command uint32, name string) []byte {
	const fixedSize = 24
	commandSize := darwinTestAlignedSize(fixedSize + len(name) + 1)
	data := make([]byte, commandSize)
	binary.LittleEndian.PutUint32(data[0:4], command)
	binary.LittleEndian.PutUint32(data[4:8], uint32(commandSize))
	binary.LittleEndian.PutUint32(data[8:12], fixedSize)
	binary.LittleEndian.PutUint32(data[16:20], 0x10000)
	binary.LittleEndian.PutUint32(data[20:24], 0x10000)
	copy(data[fixedSize:], name)
	return data
}

func darwinTestRPathCommand(path string) []byte {
	const fixedSize = 12
	commandSize := darwinTestAlignedSize(fixedSize + len(path) + 1)
	data := make([]byte, commandSize)
	binary.LittleEndian.PutUint32(data[0:4], darwinLCRPath)
	binary.LittleEndian.PutUint32(data[4:8], uint32(commandSize))
	binary.LittleEndian.PutUint32(data[8:12], fixedSize)
	copy(data[fixedSize:], path)
	return data
}

func darwinTestAlignedSize(size int) int {
	return (size + 7) &^ 7
}

func darwinTestCPU(t *testing.T) macho.Cpu {
	t.Helper()
	switch runtime.GOARCH {
	case "amd64":
		return macho.CpuAmd64
	case "arm64":
		return macho.CpuArm64
	default:
		t.Fatalf("unsupported Darwin test architecture %s", runtime.GOARCH)
		return 0
	}
}
