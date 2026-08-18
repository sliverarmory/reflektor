//go:build windows && (386 || amd64 || arm64)

package reflektor_test

import (
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func assertRecursiveDependenciesNotNativeLoaded(t *testing.T, graphDir string) {
	t.Helper()
	getModuleHandle := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	for _, name := range []string{"libreflektor_leaf.dll", "libreflektor_middle.dll"} {
		wideName, err := windows.UTF16PtrFromString(name)
		if err != nil {
			t.Fatalf("encode recursive module name %s: %v", name, err)
		}
		handle, _, _ := getModuleHandle.Call(uintptr(unsafe.Pointer(wideName)))
		if handle != 0 {
			t.Fatalf("recursive dependency %s was registered with the Windows OS loader (graph %s)", name, filepath.Clean(graphDir))
		}
	}
}
