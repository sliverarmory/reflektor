//go:build windows && (386 || amd64 || arm64)

package reflektor_test

import (
	"context"
	"fmt"
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
	windowsGraphRootEnv   = "REFLEKTOR_GRAPH_ROOT"
	windowsGraphKindEnv   = "REFLEKTOR_GRAPH_KIND"
	windowsGraphHiddenEnv = "REFLEKTOR_GRAPH_HIDDEN"
)

func TestWindowsRecursiveCyclesAndDiamonds(t *testing.T) {
	requireCommand(t, "zig")
	for _, kind := range []string{"cycle", "diamond"} {
		t.Run(kind, func(t *testing.T) {
			stateDir := t.TempDir()
			graphDir := filepath.Join(stateDir, "graph")
			if err := os.Mkdir(graphDir, 0o755); err != nil {
				t.Fatal(err)
			}
			var graph windowsRecursiveGraph
			if kind == "cycle" {
				graph = buildWindowsCycleGraph(t, graphDir, runtime.GOARCH)
			} else {
				graph = buildWindowsDiamondGraph(t, graphDir, runtime.GOARCH)
			}
			hiddenDir := filepath.Join(stateDir, "graph-hidden")
			logPath := filepath.Join(stateDir, "graph-events.txt")
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsRecursiveGraphChild$", "-test.count=1", "-test.v")
			cmd.Env = overrideEnv(os.Environ(), map[string]string{
				windowsGraphRootEnv:   graph.root,
				windowsGraphKindEnv:   kind,
				windowsGraphHiddenEnv: hiddenDir,
				"REFLEKTOR_GRAPH_LOG": logPath,
			})
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("recursive %s graph child timed out: %v\n%s", kind, ctx.Err(), output)
			}
			if err != nil {
				t.Fatalf("recursive %s graph child failed: %v\n%s", kind, err, output)
			}
		})
	}
}

func TestWindowsRecursiveGraphChild(t *testing.T) {
	kind := os.Getenv(windowsGraphKindEnv)
	if kind == "" {
		return
	}
	root := os.Getenv(windowsGraphRootEnv)
	hidden := os.Getenv(windowsGraphHiddenEnv)
	logPath := os.Getenv("REFLEKTOR_GRAPH_LOG")
	if root == "" || hidden == "" || logPath == "" {
		t.Fatal("recursive graph child is missing paths")
	}
	var export string
	var want uintptr
	var moduleNames []string
	switch kind {
	case "cycle":
		export, want = "CycleRoundTrip", 43
		moduleNames = []string{"reflektor_cycle_a.dll", "reflektor_cycle_b.dll"}
	case "diamond":
		export, want = "DiamondResult", 42
		moduleNames = []string{"reflektor_diamond_root.dll", "reflektor_diamond_left.dll", "reflektor_diamond_right.dll", "reflektor_diamond_leaf.dll"}
	default:
		t.Fatalf("unknown recursive graph %q", kind)
	}

	library, err := reflektor.LoadLibraryFileRecursive(root)
	if err != nil {
		t.Fatalf("load %s graph: %v", kind, err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = library.Close()
		}
	}()
	assertWindowsGraphLog(t, kind, logPath, false)
	assertWindowsGraphNotNativeLoaded(t, moduleNames)
	if err := os.Rename(filepath.Dir(root), hidden); err != nil {
		t.Fatalf("hide %s graph DLL files: %v", kind, err)
	}
	for i := 0; i < 2; i++ {
		got, err := library.CallExportWithArgs(export)
		if err != nil || got != want {
			t.Fatalf("%s graph call %d = %d, %v; want %d", kind, i+1, got, err, want)
		}
		if kind == "cycle" {
			forwarded, forwardErr := library.CallExportWithArgs("CycleForwardedResult")
			if forwardErr != nil || forwarded != 42 {
				t.Fatalf("cycle graph forwarded call %d = %d, %v; want 42", i+1, forwarded, forwardErr)
			}
		}
	}
	if err := library.Close(); err != nil {
		t.Fatalf("close %s graph: %v", kind, err)
	}
	closed = true
	assertWindowsGraphLog(t, kind, logPath, true)
}

func assertWindowsGraphNotNativeLoaded(t *testing.T, names []string) {
	t.Helper()
	getModuleHandle := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	for _, name := range names {
		wide, err := windows.UTF16PtrFromString(name)
		if err != nil {
			t.Fatal(err)
		}
		handle, _, _ := getModuleHandle.Call(uintptr(unsafe.Pointer(wide)))
		if handle != 0 {
			t.Fatalf("recursive DLL %s was registered with the Windows loader", name)
		}
	}
}

func assertWindowsGraphLog(t *testing.T, kind string, path string, detached bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s graph lifecycle log: %v", kind, err)
	}
	got := string(data)
	if kind == "cycle" {
		want := "BA"
		if detached {
			want = "BAab"
		}
		if got != want {
			t.Fatalf("cycle lifecycle log = %q, want %q", got, want)
		}
		return
	}
	if len(got) < 4 || got[0] != 'L' || got[3] != 'R' || !strings.Contains(got[1:3], "X") || !strings.Contains(got[1:3], "Y") {
		t.Fatalf("diamond attach order = %q, want leaf, both branches, root", got)
	}
	if !detached {
		if len(got) != 4 {
			t.Fatalf("diamond attach log = %q, want four events", got)
		}
		return
	}
	want := fmt.Sprintf("%sr%c%cl", got[:4], got[2]+('a'-'A'), got[1]+('a'-'A'))
	if got != want {
		t.Fatalf("diamond lifecycle log = %q, want reverse detach order %q", got, want)
	}
}
