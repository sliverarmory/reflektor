//go:build windows && (386 || amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"debug/pe"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/sliverarmory/reflektor"
	"golang.org/x/sys/windows"
)

func TestWindowsRecursiveDelayImports(t *testing.T) {
	requireCommand(t, "zig")
	requireCommand(t, "lld-link")

	for _, forwarder := range []bool{false, true} {
		name := "direct"
		if forwarder {
			name = "forwarder"
		}
		t.Run(name, func(t *testing.T) {
			graphDir := filepath.Join(t.TempDir(), "graph")
			if err := os.Mkdir(graphDir, 0o755); err != nil {
				t.Fatal(err)
			}
			rootPath, delayName := buildWindowsDelayFixtures(t, graphDir, forwarder)
			assertWindowsDelayDirectory(t, rootPath, delayName)

			library, err := reflektor.LoadLibraryFileRecursive(rootPath)
			if err != nil {
				t.Fatalf("load delay-import fixture: %v", err)
			}
			closed := false
			defer func() {
				if !closed {
					_ = library.Close()
				}
			}()

			assertWindowsModulesNotNativeLoaded(t, "libreflektor_delay_leaf.dll", "libreflektor_delay_forwarder.dll")
			hiddenDir := filepath.Join(filepath.Dir(graphDir), "graph-hidden")
			if err := os.Rename(graphDir, hiddenDir); err != nil {
				t.Fatalf("hide delay-import graph after load: %v", err)
			}
			for call := 0; call < 2; call++ {
				value, err := library.CallExportWithArgs("DelayValue")
				if err != nil || value != 42 {
					t.Fatalf("DelayValue call %d = (%d, %v), want (42, nil)", call, value, err)
				}
			}

			observer, err := windows.VirtualAlloc(0, 4096, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
			if err != nil {
				t.Fatalf("allocate detach observer: %v", err)
			}
			defer windows.VirtualFree(observer, 0, windows.MEM_RELEASE)
			if _, err := library.CallExportWithArgs("DelaySetObserver", observer); err != nil {
				t.Fatalf("register detach observer through delay import: %v", err)
			}
			if err := library.Close(); err != nil {
				t.Fatalf("close delay-import graph: %v", err)
			}
			closed = true
			order := (*[3]int32)(unsafe.Pointer(observer))
			if *order != [3]int32{1, 2, 2} {
				t.Fatalf("root and leaf detach order = %v, want [1 2 2]", *order)
			}
		})
	}
}

func buildWindowsDelayFixtures(t *testing.T, dir string, forwarder bool) (rootPath string, delayName string) {
	t.Helper()
	target := map[string]string{
		"386":   "x86-windows-msvc",
		"amd64": "x86_64-windows-msvc",
		"arm64": "aarch64-windows-msvc",
	}[runtime.GOARCH]
	machine := map[string]string{
		"386":   "x86",
		"amd64": "x64",
		"arm64": "arm64",
	}[runtime.GOARCH]
	if target == "" {
		t.Fatalf("unsupported Windows delay fixture target %s", runtime.GOARCH)
	}

	for _, name := range []string{"leaf", "forwarder", "root"} {
		source := filepath.Join("..", "testdata", "c", "windows_delay_"+name+".c")
		object := filepath.Join(dir, name+".obj")
		runWindowsDelayTool(t, "zig", "cc", "-target", target, "-O2", "-g0", "-c", "-o", object, source)
	}
	leafPath := filepath.Join(dir, "libreflektor_delay_leaf.dll")
	leafImport := filepath.Join(dir, "libreflektor_delay_leaf.lib")
	runWindowsDelayTool(t, "lld-link", "/dll", "/nodefaultlib", "/machine:"+machine,
		"/entry:DllMain", "/out:"+leafPath, "/implib:"+leafImport, filepath.Join(dir, "leaf.obj"))
	if forwarder {
		delayName = "libreflektor_delay_forwarder.dll"
		forwarderPath := filepath.Join(dir, delayName)
		forwarderImport := filepath.Join(dir, "libreflektor_delay_forwarder.lib")
		runWindowsDelayTool(t, "lld-link", "/dll", "/nodefaultlib", "/machine:"+machine,
			"/entry:DllMain", "/def:"+filepath.Join("..", "testdata", "c", "windows_delay_forwarder.def"),
			"/out:"+forwarderPath, "/implib:"+forwarderImport, filepath.Join(dir, "forwarder.obj"))
		normalizeWindowsDelayForwarderFixture(t, forwarderPath)
		leafImport = forwarderImport
	} else {
		delayName = "libreflektor_delay_leaf.dll"
	}
	rootPath = filepath.Join(dir, "reflektor_delay_root.dll")
	runWindowsDelayTool(t, "lld-link", "/dll", "/nodefaultlib", "/machine:"+machine,
		"/entry:DllMain", "/delayload:"+delayName,
		"/out:"+rootPath, filepath.Join(dir, "root.obj"), leafImport)
	return rootPath, delayName
}

// The Windows/386 Actions linker decorates the module portion of .def
// forwarders with a leading underscore. Keep the fixture's forwarder naming
// identical across architectures; dependency resolution remains literal.
func normalizeWindowsDelayForwarderFixture(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	for _, symbol := range []string{"ReflektorDelayValue", "ReflektorDelaySetObserver"} {
		canonical := []byte("libreflektor_delay_leaf." + symbol + "\x00")
		decorated := []byte("_libreflektor_delay_leaf." + symbol + "\x00")
		if bytes.Contains(data, decorated) {
			replacement := append(append([]byte{}, canonical...), 0)
			data = bytes.Replace(data, decorated, replacement, 1)
			changed = true
		} else if !bytes.Contains(data, canonical) {
			t.Fatalf("forwarder DLL lacks %s target", symbol)
		}
	}
	if changed {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("normalize x86 PE forwarders: %v", err)
		}
	}
}

func runWindowsDelayTool(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(),
		"ZIG_GLOBAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-global-cache"),
		"ZIG_LOCAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-local-cache"),
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
}

func assertWindowsDelayDirectory(t *testing.T, rootPath string, delayName string) {
	t.Helper()
	image, err := pe.Open(rootPath)
	if err != nil {
		t.Fatalf("parse delay-import fixture: %v", err)
	}
	defer image.Close()
	var directories []pe.DataDirectory
	switch header := image.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		directories = header.DataDirectory[:]
	case *pe.OptionalHeader64:
		directories = header.DataDirectory[:]
	default:
		t.Fatalf("unsupported PE optional header %T", image.OptionalHeader)
	}
	if directories[13].VirtualAddress == 0 || directories[13].Size < 64 {
		t.Fatalf("missing delay import descriptor for %s: %#v", delayName, directories[13])
	}
	if directories[1].Size != 0 {
		t.Fatalf("unexpected ordinary import directory in delay fixture: %#v", directories[1])
	}
	data, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), delayName) {
		t.Fatalf("delay-import DLL name %q not present in PE image", delayName)
	}
}

func assertWindowsModulesNotNativeLoaded(t *testing.T, names ...string) {
	t.Helper()
	getModuleHandle := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	for _, name := range names {
		wideName, err := windows.UTF16PtrFromString(name)
		if err != nil {
			t.Fatal(err)
		}
		handle, _, callErr := getModuleHandle.Call(uintptr(unsafe.Pointer(wideName)))
		if handle != 0 {
			t.Fatalf("delay dependency %s was registered with the Windows OS loader: handle=%#x, error=%v", name, handle, callErr)
		}
	}
}

func TestWindowsRecursiveDelayImportRejectsLegacyDescriptor(t *testing.T) {
	requireCommand(t, "zig")
	requireCommand(t, "lld-link")
	graphDir := t.TempDir()
	rootPath, _ := buildWindowsDelayFixtures(t, graphDir, false)
	image, err := pe.Open(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	var delayRVA uint32
	switch header := image.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		delayRVA = header.DataDirectory[13].VirtualAddress
	case *pe.OptionalHeader64:
		delayRVA = header.DataDirectory[13].VirtualAddress
	}
	var delayOffset uint32
	for _, section := range image.Sections {
		if delayRVA >= section.VirtualAddress && delayRVA-section.VirtualAddress+4 <= section.Size {
			delayOffset = section.Offset + delayRVA - section.VirtualAddress
			break
		}
	}
	_ = image.Close()
	if delayOffset == 0 {
		t.Fatal("cannot locate delay descriptor in PE file")
	}
	data, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	data[delayOffset] = 0 // the old VA-based descriptor form is intentionally unsupported
	_, err = reflektor.LoadLibraryRecursive(data)
	if err == nil || !strings.Contains(err.Error(), "unsupported delay-load descriptor attributes") {
		t.Fatalf("legacy delay descriptor error = %v", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("delay descriptor was resolved before validation: %v", err)
	}
}
