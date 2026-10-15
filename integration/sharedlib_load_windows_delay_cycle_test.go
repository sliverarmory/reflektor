//go:build windows && (386 || amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"context"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/sliverarmory/reflektor"
	"golang.org/x/sys/windows"
)

const (
	windowsDelayCycleRootEnv   = "REFLEKTOR_DELAY_CYCLE_ROOT"
	windowsDelayCycleHiddenEnv = "REFLEKTOR_DELAY_CYCLE_HIDDEN"
)

type windowsDelayCycleGraph struct {
	root      string
	peer      string
	forwarder string
}

// The root imports the peer normally, the peer delay-imports the forwarder,
// and the forwarder points back to an export in the root. A delayed edge must
// participate in mapping, initialization, and detach order just like a normal
// import edge.
func TestWindowsDelayCycleComposition(t *testing.T) {
	requireCommand(t, "zig")
	requireCommand(t, "lld-link")

	stateDir := t.TempDir()
	graphDir := filepath.Join(stateDir, "graph")
	if err := os.Mkdir(graphDir, 0o755); err != nil {
		t.Fatal(err)
	}
	graph := buildWindowsDelayCycleGraph(t, graphDir)
	assertWindowsDelayCyclePE(t, graph)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsDelayCycleCompositionChild$", "-test.count=1", "-test.v")
	cmd.Env = overrideEnv(os.Environ(), map[string]string{
		windowsDelayCycleRootEnv:   graph.root,
		windowsDelayCycleHiddenEnv: filepath.Join(stateDir, "graph-hidden"),
	})
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("delay-cycle child timed out: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("delay-cycle child failed: %v\n%s", err, output)
	}
}

func TestWindowsDelayCycleCompositionChild(t *testing.T) {
	root := os.Getenv(windowsDelayCycleRootEnv)
	if root == "" {
		return
	}
	hidden := os.Getenv(windowsDelayCycleHiddenEnv)
	if hidden == "" {
		t.Fatal("missing hidden graph directory")
	}

	library, err := reflektor.LoadLibraryFileRecursive(root)
	if err != nil {
		t.Fatalf("load delay-cycle graph: %v", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = library.Close()
		}
	}()
	assertWindowsModulesNotNativeLoaded(t,
		"reflektor_delay_cycle_root.dll",
		"reflektor_delay_cycle_peer.dll",
		"reflektor_delay_cycle_forwarder.dll",
	)
	if err := os.Rename(filepath.Dir(root), hidden); err != nil {
		t.Fatalf("hide delay-cycle graph after load: %v", err)
	}
	for call := 0; call < 2; call++ {
		value, err := library.CallExportWithArgs("DelayCycleResult")
		if err != nil || value != 42 {
			t.Fatalf("DelayCycleResult call %d = (%d, %v), want (42, nil)", call, value, err)
		}
	}

	observer, err := windows.VirtualAlloc(0, 4096, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		t.Fatalf("allocate detach observer: %v", err)
	}
	defer windows.VirtualFree(observer, 0, windows.MEM_RELEASE)
	if _, err := library.CallExportWithArgs("DelayCycleSetObserver", observer); err != nil {
		t.Fatalf("register detach observer: %v", err)
	}
	if err := library.Close(); err != nil {
		t.Fatalf("close delay-cycle graph: %v", err)
	}
	closed = true
	record := (*[5]int32)(unsafe.Pointer(observer))
	if want := [5]int32{1, 2, 3, 3, 0}; *record != want {
		t.Fatalf("delay-cycle detach record = %v, want %v", *record, want)
	}
}

func buildWindowsDelayCycleGraph(t *testing.T, dir string) windowsDelayCycleGraph {
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
		t.Fatalf("unsupported Windows delay-cycle target %s", runtime.GOARCH)
	}
	source := filepath.Join("..", "testdata", "c", "windows_delay_cycle.c")
	for _, variant := range []struct {
		name   string
		define string
	}{
		{"root", "REFLEKTOR_DELAY_CYCLE_ROOT"},
		{"peer", "REFLEKTOR_DELAY_CYCLE_PEER"},
		{"forwarder", "REFLEKTOR_DELAY_CYCLE_FORWARDER"},
	} {
		runWindowsDelayTool(t, "zig", "cc", "-target", target, "-O2", "-g0",
			"-D"+variant.define, "-c", "-o", filepath.Join(dir, variant.name+".obj"), source)
	}

	graph := windowsDelayCycleGraph{
		root:      filepath.Join(dir, "reflektor_delay_cycle_root.dll"),
		peer:      filepath.Join(dir, "reflektor_delay_cycle_peer.dll"),
		forwarder: filepath.Join(dir, "reflektor_delay_cycle_forwarder.dll"),
	}
	forwarderImport := filepath.Join(dir, "reflektor_delay_cycle_forwarder.lib")
	peerImport := filepath.Join(dir, "reflektor_delay_cycle_peer.lib")
	common := []string{"/dll", "/nodefaultlib", "/machine:" + machine, "/entry:DllMain"}
	runWindowsDelayTool(t, "lld-link", append(append([]string{}, common...),
		"/def:"+filepath.Join("..", "testdata", "c", "windows_delay_cycle_forwarder.def"),
		"/out:"+graph.forwarder, "/implib:"+forwarderImport, filepath.Join(dir, "forwarder.obj"))...)
	normalizeWindowsDelayCycleForwarderFixture(t, graph.forwarder)
	runWindowsDelayTool(t, "lld-link", append(append([]string{}, common...),
		"/delayload:"+filepath.Base(graph.forwarder), "/out:"+graph.peer,
		"/implib:"+peerImport, filepath.Join(dir, "peer.obj"), forwarderImport)...)
	runWindowsDelayTool(t, "lld-link", append(append([]string{}, common...),
		"/out:"+graph.root, filepath.Join(dir, "root.obj"), peerImport)...)
	return graph
}

// The Windows/386 Actions linker decorates the module portion of a .def
// forwarder. The fixture names the root DLL literally on every architecture.
func normalizeWindowsDelayCycleForwarderFixture(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	canonical := []byte("reflektor_delay_cycle_root.DelayCycleRootValue\x00")
	decorated := []byte("_reflektor_delay_cycle_root.DelayCycleRootValue\x00")
	if bytes.Contains(data, decorated) {
		replacement := append(append([]byte{}, canonical...), 0)
		data = bytes.Replace(data, decorated, replacement, 1)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("normalize x86 PE delay-cycle forwarder: %v", err)
		}
	} else if !bytes.Contains(data, canonical) {
		t.Fatal("delay-cycle forwarder DLL lacks redirect back to root")
	}
}

func assertWindowsDelayCyclePE(t *testing.T, graph windowsDelayCycleGraph) {
	t.Helper()
	assertWindowsDelayDirectory(t, graph.peer, filepath.Base(graph.forwarder))
	root, err := pe.Open(graph.root)
	if err != nil {
		t.Fatalf("parse delay-cycle root: %v", err)
	}
	defer root.Close()
	imports, err := root.ImportedLibraries()
	if err != nil {
		t.Fatalf("read delay-cycle root imports: %v", err)
	}
	if len(imports) != 1 || !strings.EqualFold(imports[0], filepath.Base(graph.peer)) {
		t.Fatalf("root ordinary imports = %v, want peer DLL", imports)
	}
	image, err := os.ReadFile(graph.forwarder)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(image, []byte("reflektor_delay_cycle_root.DelayCycleRootValue\x00")) {
		t.Fatal("forwarder DLL lacks export redirect back into the root")
	}
}
