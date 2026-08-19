//go:build windows

package memmod

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsExportCandidates(t *testing.T) {
	tests := []struct {
		name       string
		normalized string
		candidates []string
		wantErr    bool
	}{
		{name: " StartW ", normalized: "StartW", candidates: []string{"StartW", "_StartW"}},
		{name: "_StartW", normalized: "_StartW", candidates: []string{"_StartW", "StartW"}},
		{name: "  ", wantErr: true},
	}

	for _, test := range tests {
		normalized, candidates, err := windowsExportCandidates(test.name)
		if test.wantErr {
			if err == nil {
				t.Fatalf("windowsExportCandidates(%q) unexpectedly succeeded", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("windowsExportCandidates(%q): %v", test.name, err)
		}
		if normalized != test.normalized || !reflect.DeepEqual(candidates, test.candidates) {
			t.Fatalf("windowsExportCandidates(%q) = (%q, %#v), want (%q, %#v)", test.name, normalized, candidates, test.normalized, test.candidates)
		}
	}
}

func TestInvokeWindowsExportWithArgs(t *testing.T) {
	callback := windows.NewCallbackCDecl(func(first, second, third uintptr) uintptr {
		return first*100 + second*10 + third
	})

	if got, want := invokeWindowsExport(callback, 7, 8, 9), uintptr(789); got != want {
		t.Fatalf("invokeWindowsExport() = %d, want %d", got, want)
	}
}

func TestCallExportWithArgsRejectsGoSharedImages(t *testing.T) {
	module := &Module{goRuntime: true}
	for _, args := range [][]uintptr{nil, {1, 2, 3}} {
		result, err := module.CallExportWithArgs("StartW", args...)
		if result != 0 {
			t.Fatalf("CallExportWithArgs() result = %d, want 0", result)
		}
		if !errors.Is(err, ErrGoExportArgumentsUnsupported) {
			t.Fatalf("CallExportWithArgs() error = %v, want %v", err, ErrGoExportArgumentsUnsupported)
		}
	}
}

func TestCallExportWithArgsRejectsTooManyArgumentsBeforeResolution(t *testing.T) {
	module := &Module{}
	result, err := module.CallExportWithArgs("not-resolved", 1, 2, 3, 4)
	if result != 0 {
		t.Fatalf("CallExportWithArgs() result = %d, want 0", result)
	}
	if err == nil || !strings.Contains(err.Error(), "maximum is 3") {
		t.Fatalf("CallExportWithArgs() error = %v, want argument-limit error", err)
	}
}

func TestRecursiveForwarderPreservesGoExportOwner(t *testing.T) {
	const (
		exportDirectoryRVA = uint32(0x100)
		functionTableRVA   = uint32(0x140)
		forwarderRVA       = uint32(0x180)
		functionRVA        = uint32(0x300)
	)

	newSyntheticModule := func(exportName string, targetRVA uint32) *Module {
		t.Helper()
		base, err := windows.VirtualAlloc(0, 0x1000, windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
		if err != nil {
			t.Fatalf("VirtualAlloc synthetic export module: %v", err)
		}
		t.Cleanup(func() {
			if err := windows.VirtualFree(base, 0, windows.MEM_RELEASE); err != nil {
				t.Errorf("VirtualFree synthetic export module: %v", err)
			}
		})

		headers := &IMAGE_NT_HEADERS{}
		headers.OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_EXPORT] = IMAGE_DATA_DIRECTORY{
			VirtualAddress: exportDirectoryRVA,
			Size:           0x100,
		}
		exports := (*IMAGE_EXPORT_DIRECTORY)(a2p(base + uintptr(exportDirectoryRVA)))
		exports.NumberOfFunctions = 1
		exports.AddressOfFunctions = functionTableRVA
		*(*uint32)(a2p(base + uintptr(functionTableRVA))) = targetRVA

		return &Module{
			headers:     headers,
			codeBase:    base,
			nameExports: map[string]uint16{exportName: 0},
		}
	}

	dependency := newSyntheticModule("Target", functionRVA)
	dependency.goRuntime = true
	root := newSyntheticModule("Forwarded", forwarderRVA)
	forwarder := append([]byte("dependency.Target"), 0)
	copy(unsafe.Slice((*byte)(a2p(root.codeBase+uintptr(forwarderRVA))), len(forwarder)), forwarder)

	rootPath := `C:\fixture\root.dll`
	dependencyPath := `C:\fixture\dependency.dll`
	_, dependencyKey, err := canonicalRecursivePath(dependencyPath)
	if err != nil {
		t.Fatalf("canonical dependency path: %v", err)
	}
	session := &recursiveLoadSession{
		rootPath: rootPath,
		reader: func(request DependencyRequest) (Dependency, error) {
			if request.Name != "dependency.dll" {
				t.Fatalf("unexpected forwarded dependency request: %#v", request)
			}
			return Dependency{Data: []byte{1}, Path: dependencyPath}, nil
		},
		records: map[string]*recursiveModuleRecord{
			dependencyKey: {
				key:    dependencyKey,
				path:   dependencyPath,
				state:  recursiveLoadReady,
				module: dependency,
			},
		},
	}
	root.recursive = session
	root.recursivePath = rootPath
	dependency.recursive = session
	dependency.recursivePath = dependencyPath

	for attempt := 0; attempt < 2; attempt++ {
		resolved, err := root.resolveExportAddress("Forwarded")
		if err != nil {
			t.Fatalf("resolve forwarded Go export (attempt %d): %v", attempt+1, err)
		}
		if want := dependency.codeBase + uintptr(functionRVA); resolved.address != want {
			t.Fatalf("forwarded address (attempt %d) = %#x, want %#x", attempt+1, resolved.address, want)
		}
		if resolved.owner != dependency || !resolved.owner.goRuntime {
			t.Fatalf("forwarded owner (attempt %d) = %#v, want Go dependency %#v", attempt+1, resolved.owner, dependency)
		}
	}
}
