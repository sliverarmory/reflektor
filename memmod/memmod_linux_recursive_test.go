//go:build linux && (386 || amd64 || arm64)

package memmod

import (
	"debug/elf"
	"errors"
	"reflect"
	"testing"
)

func TestLinuxRecursivePostOrderHandlesCyclesAndDeduplicates(t *testing.T) {
	root := &linuxRecursiveNode{path: "root"}
	left := &linuxRecursiveNode{path: "left"}
	right := &linuxRecursiveNode{path: "right"}
	leaf := &linuxRecursiveNode{path: "leaf"}

	root.dependencies = []linuxRecursiveDependency{{custom: left}, {custom: right}}
	left.dependencies = []linuxRecursiveDependency{{custom: leaf}}
	right.dependencies = []linuxRecursiveDependency{{custom: leaf}, {custom: left}}
	leaf.dependencies = []linuxRecursiveDependency{{custom: right}}

	order := linuxRecursivePostOrder(root)
	got := make([]string, 0, len(order))
	seen := make(map[*linuxRecursiveNode]struct{})
	for _, node := range order {
		if _, exists := seen[node]; exists {
			t.Fatalf("node %q appeared more than once in recursive init order", node.path)
		}
		seen[node] = struct{}{}
		got = append(got, node.path)
	}
	want := []string{"right", "leaf", "left", "root"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected recursive init order: got=%v want=%v", got, want)
	}
}

func TestLinuxRecursiveScopeIsRootFirstBreadthFirst(t *testing.T) {
	root := &linuxRecursiveNode{path: "root"}
	left := &linuxRecursiveNode{path: "left"}
	right := &linuxRecursiveNode{path: "right"}
	leaf := &linuxRecursiveNode{path: "leaf"}
	system := &linuxRecursiveSystem{path: "/usr/lib/libsystem.so"}

	root.dependencies = []linuxRecursiveDependency{{custom: left}, {system: system}, {custom: right}}
	left.dependencies = []linuxRecursiveDependency{{custom: leaf}}
	right.dependencies = []linuxRecursiveDependency{{custom: leaf}}

	scope := linuxRecursiveScope(root)
	got := make([]string, 0, len(scope))
	for _, entry := range scope {
		if entry.custom != nil {
			got = append(got, entry.custom.path)
		} else {
			got = append(got, entry.system.path)
		}
	}
	want := []string{"root", "left", "/usr/lib/libsystem.so", "right", "leaf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected recursive symbol scope: got=%v want=%v", got, want)
	}
}

func TestRecursiveSymbolVersionMatches(t *testing.T) {
	unversionedImport := elf.Symbol{Name: "value"}
	versionedImport := elf.Symbol{Name: "value", HasVersion: true, Version: "LIB_2.0"}
	defaultExport := elf.Symbol{Name: "value", HasVersion: true, Version: "LIB_2.0", VersionIndex: elf.VersionIndex(2)}
	hiddenExport := elf.Symbol{Name: "value", HasVersion: true, Version: "LIB_2.0", VersionIndex: elf.VersionIndex(0x8002)}
	wrongExport := elf.Symbol{Name: "value", HasVersion: true, Version: "LIB_1.0", VersionIndex: elf.VersionIndex(2)}

	if !recursiveSymbolVersionMatches(versionedImport, defaultExport) {
		t.Fatal("matching explicit symbol versions did not bind")
	}
	if !recursiveSymbolVersionMatches(versionedImport, hiddenExport) {
		t.Fatal("an explicit version should be allowed to bind a hidden version definition")
	}
	if recursiveSymbolVersionMatches(versionedImport, wrongExport) {
		t.Fatal("different explicit symbol versions unexpectedly matched")
	}
	if recursiveSymbolVersionMatches(unversionedImport, hiddenExport) {
		t.Fatal("unversioned import unexpectedly matched a hidden version definition")
	}
	if !recursiveSymbolVersionMatches(unversionedImport, defaultExport) {
		t.Fatal("unversioned import did not match the default version definition")
	}
}

func TestLinuxPathWithinRootUsesDirectoryBoundary(t *testing.T) {
	if !linuxPathWithinRoot("/usr/lib/x86_64-linux-gnu/libc.so.6", "/usr/lib") {
		t.Fatal("expected multiarch system library to be within /usr/lib")
	}
	if linuxPathWithinRoot("/usr/lib-malicious/libc.so.6", "/usr/lib") {
		t.Fatal("path-prefix collision was classified as a system library")
	}
	if linuxPathWithinRoot("/usr/local/lib/libcustom.so", "/usr/lib") {
		t.Fatal("custom /usr/local library was classified under /usr/lib")
	}
}

func TestLinuxNativeDependencyNames(t *testing.T) {
	for _, name := range []string{"libc.so.6", "libcurl.so.4", "ld-linux-x86-64.so.2", "ld-musl-aarch64.so.1"} {
		if !isLinuxNativeDependencyName(name) {
			t.Fatalf("expected %q to use native system fallback", name)
		}
	}
	if isLinuxNativeDependencyName("libreflektor_custom.so") {
		t.Fatal("custom dependency was classified for native fallback")
	}
}

func TestLinuxRecursiveDependencySearchPathOrderAndRPathInheritance(t *testing.T) {
	t.Setenv("LD_LIBRARY_PATH", "/env/one:/env/two")
	node := &linuxRecursiveNode{rPaths: []string{"/node/rpath"}}
	search, inherited := linuxRecursiveDependencySearchPaths(node, []string{"/parent/rpath"})
	want := []string{"/node/rpath", "/parent/rpath", "/env/one", "/env/two"}
	if !reflect.DeepEqual(search, want) {
		t.Fatalf("RPATH search order = %#v, want %#v", search, want)
	}
	if !reflect.DeepEqual(inherited, []string{"/node/rpath", "/parent/rpath"}) {
		t.Fatalf("inherited RPATHs = %#v", inherited)
	}

	node = &linuxRecursiveNode{runPaths: []string{"/node/runpath"}, rPaths: []string{"/ignored/rpath"}}
	search, inherited = linuxRecursiveDependencySearchPaths(node, []string{"/parent/rpath"})
	want = []string{"/parent/rpath", "/env/one", "/env/two", "/node/runpath"}
	if !reflect.DeepEqual(search, want) {
		t.Fatalf("RUNPATH search order = %#v, want %#v", search, want)
	}
	if !reflect.DeepEqual(inherited, []string{"/parent/rpath"}) {
		t.Fatalf("RUNPATH unexpectedly became inherited: %#v", inherited)
	}
}

func TestLinuxRecursiveDiscoveryUsesRegisteredSONAMEsForCycles(t *testing.T) {
	root := &linuxRecursiveNode{path: "/graph/root.so", soname: "root.so", aliases: make(map[string]struct{}), needed: []string{"child.so"}}
	child := &linuxRecursiveNode{path: "/graph/child.so", soname: "child.so", aliases: make(map[string]struct{}), needed: []string{"root.so"}}
	readerCalls := 0
	loader := &linuxRecursiveLoader{
		reader: func(DependencyRequest) (Dependency, error) {
			readerCalls++
			return Dependency{}, errors.New("registered SONAME should not be read again")
		},
		group:    &linuxRecursiveGroup{},
		byPath:   make(map[string]*linuxRecursiveNode),
		bySONAME: make(map[string]*linuxRecursiveNode),
	}
	loader.registerCustomNode(root)
	loader.registerCustomNode(child)
	if err := loader.discover(root, nil); err != nil {
		t.Fatalf("discover registered cycle: %v", err)
	}
	if readerCalls != 0 {
		t.Fatalf("registered cycle reread %d dependencies", readerCalls)
	}
	if len(root.dependencies) != 1 || root.dependencies[0].custom != child {
		t.Fatalf("root dependency was not linked to registered child: %#v", root.dependencies)
	}
	if len(child.dependencies) != 1 || child.dependencies[0].custom != root {
		t.Fatalf("child dependency was not linked back to registered root: %#v", child.dependencies)
	}
}
