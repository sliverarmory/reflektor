//go:build windows

package memmod

import (
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestParseExportForwarder(t *testing.T) {
	tests := []struct {
		forwarder  string
		moduleName string
		targetName string
		ordinal    uint16
		byOrdinal  bool
		wantErr    bool
	}{
		{forwarder: "KERNELBASE.CreateFileW", moduleName: "KERNELBASE.dll", targetName: "CreateFileW"},
		{forwarder: "example.dll.Run", moduleName: "example.dll", targetName: "Run"},
		{forwarder: "NTDLL.#27", moduleName: "NTDLL.dll", ordinal: 27, byOrdinal: true},
		{forwarder: "NTDLL.#0", moduleName: "NTDLL.dll", ordinal: 0, byOrdinal: true},
		{forwarder: "missing-separator", wantErr: true},
		{forwarder: "KERNEL32.#bad", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.forwarder, func(t *testing.T) {
			moduleName, targetName, ordinal, byOrdinal, err := parseExportForwarder(test.forwarder)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if moduleName != test.moduleName || targetName != test.targetName || ordinal != test.ordinal || byOrdinal != test.byOrdinal {
				t.Fatalf("parseExportForwarder(%q) = (%q, %q, %d, %v), want (%q, %q, %d, %v)",
					test.forwarder, moduleName, targetName, ordinal, byOrdinal,
					test.moduleName, test.targetName, test.ordinal, test.byOrdinal)
			}
		})
	}
}

func TestRecursiveSearchPaths(t *testing.T) {
	got := recursiveSearchPaths(
		`C:\fixture\plugins\middle.dll`,
		`C:\fixture\root.dll`,
	)
	want := []string{`C:\fixture\plugins`, `C:\fixture`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recursiveSearchPaths() = %#v, want %#v", got, want)
	}

	got = recursiveSearchPaths(`C:\fixture\middle.dll`, `C:\fixture\root.dll`)
	want = []string{`C:\fixture`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deduplicated recursiveSearchPaths() = %#v, want %#v", got, want)
	}
}

func TestCanonicalRecursivePath(t *testing.T) {
	path, key, err := canonicalRecursivePath(`C:\Fixture\ROOT.DLL`)
	if err != nil {
		t.Fatal(err)
	}
	if path != `C:\Fixture\ROOT.DLL` || key != `c:\fixture\root.dll` {
		t.Fatalf("canonicalRecursivePath() = (%q, %q)", path, key)
	}
	if _, _, err := canonicalRecursivePath(`relative\root.dll`); err == nil {
		t.Fatal("relative origin was accepted")
	}
}

func TestAPISetContractsRemainSystemResolved(t *testing.T) {
	for _, name := range []string{
		"api-ms-win-core-file-l1-1-0.dll",
		"ext-ms-win-ntuser-window-l1-1-0.dll",
	} {
		if !isAPISetContract(name) {
			t.Fatalf("isAPISetContract(%q) = false", name)
		}
	}
	for _, name := range []string{"ordinary-dependency.dll", "api-plugin.dll", "ext-plugin.dll"} {
		if isAPISetContract(name) {
			t.Fatalf("ordinary dependency %q was classified as an API-set contract", name)
		}
	}
}

func TestApplicationLocalRuntimeDependenciesRemainReaderResolvable(t *testing.T) {
	for _, name := range []string{"dbghelp.dll", "msvcp140.dll", "vcruntime140.dll", "vcruntime140_1.dll"} {
		if isWindowsSystemDependency(name) {
			t.Fatalf("application-local runtime %q was forced to System32", name)
		}
	}
}

func TestRecursiveResolverPrefersSystem32(t *testing.T) {
	readerCalled := false
	session := &recursiveLoadSession{
		rootPath: `C:\fixture\root.dll`,
		reader: func(DependencyRequest) (Dependency, error) {
			readerCalled = true
			return Dependency{}, ErrDependencyNotFound
		},
		records: make(map[string]*recursiveModuleRecord),
	}
	dependency, err := session.resolveImport(&Module{recursivePath: session.rootPath}, "kernel32.dll")
	if err != nil {
		t.Fatalf("resolve kernel32.dll: %v", err)
	}
	if readerCalled {
		t.Fatal("dependency reader was called for a System32 module")
	}
	if dependency.handle == 0 || dependency.module != nil {
		t.Fatalf("unexpected resolved dependency: %#v", dependency)
	}
	if err := windows.FreeLibrary(dependency.handle); err != nil {
		t.Fatalf("FreeLibrary(kernel32.dll): %v", err)
	}
}

func TestRecursiveResolverCallsReaderAfterSystemMiss(t *testing.T) {
	var gotRequest DependencyRequest
	session := &recursiveLoadSession{
		rootPath: `C:\fixture\root.dll`,
		reader: func(request DependencyRequest) (Dependency, error) {
			gotRequest = request
			return Dependency{
				Data: []byte{0},
				Path: `C:\fixture\reflektor-recursive-policy-test.dll`,
			}, nil
		},
		records: make(map[string]*recursiveModuleRecord),
	}
	_, err := session.resolveImport(
		&Module{recursivePath: `C:\fixture\plugins\middle.dll`},
		"reflektor-recursive-policy-test.dll",
	)
	if err == nil || !strings.Contains(err.Error(), "Incomplete IMAGE_DOS_HEADER") {
		t.Fatalf("resolve invalid reader image error = %v", err)
	}
	if gotRequest.Name != "reflektor-recursive-policy-test.dll" || gotRequest.ImporterPath != `C:\fixture\plugins\middle.dll` {
		t.Fatalf("unexpected dependency request: %#v", gotRequest)
	}
	wantPaths := []string{`C:\fixture\plugins`, `C:\fixture`}
	if !reflect.DeepEqual(gotRequest.SearchPaths, wantPaths) {
		t.Fatalf("dependency request search paths = %#v, want %#v", gotRequest.SearchPaths, wantPaths)
	}
}
