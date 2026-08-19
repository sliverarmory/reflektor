//go:build linux && (386 || amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/sliverarmory/reflektor/native"
	"golang.org/x/sys/unix"
)

func TestNativePackageLinuxDependencyGraphIsIsolated(t *testing.T) {
	const modulePath = "github.com/sliverarmory/reflektor"

	for _, cgoEnabled := range []string{"0", "1"} {
		cgoEnabled := cgoEnabled
		t.Run("cgo-"+cgoEnabled, func(t *testing.T) {
			cmd := exec.Command("go", "list", "-deps", "../native")
			cmd.Env = overrideEnv(os.Environ(), map[string]string{
				"GOOS":        "linux",
				"GOARCH":      runtime.GOARCH,
				"CGO_ENABLED": cgoEnabled,
			})
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("go list -deps ./native with CGO_ENABLED=%s: %v\n%s", cgoEnabled, err, output)
			}

			sawLinuxBackend := false
			for _, dependency := range strings.Fields(string(output)) {
				if dependency == modulePath || dependency == modulePath+"/memmod" || strings.HasPrefix(dependency, modulePath+"/memmod/") {
					t.Fatalf("native dependency graph includes generic loader package %q", dependency)
				}
				if dependency == modulePath+"/native/internal/linuxmem" {
					sawLinuxBackend = true
				}
			}
			if !sawLinuxBackend {
				t.Fatal("native dependency graph does not include native/internal/linuxmem")
			}

			assertNativeLinuxBridgeFiles(t, cgoEnabled, modulePath)
		})
	}
}

func assertNativeLinuxBridgeFiles(t *testing.T, cgoEnabled string, modulePath string) {
	t.Helper()

	cmd := exec.Command("go", "list", "-json", "../native", "../native/internal/linuxmem")
	cmd.Env = overrideEnv(os.Environ(), map[string]string{
		"GOOS":        "linux",
		"GOARCH":      runtime.GOARCH,
		"CGO_ENABLED": cgoEnabled,
	})
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -json native packages with CGO_ENABLED=%s: %v\n%s", cgoEnabled, err, output)
	}

	type listedPackage struct {
		ImportPath string
		GoFiles    []string
		CgoFiles   []string
		SFiles     []string
	}
	packages := make(map[string]listedPackage)
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var listed listedPackage
		if err := decoder.Decode(&listed); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode go list -json output: %v", err)
		}
		packages[listed.ImportPath] = listed
	}

	for _, importPath := range []string{modulePath + "/native", modulePath + "/native/internal/linuxmem"} {
		listed, ok := packages[importPath]
		if !ok {
			t.Fatalf("go list -json omitted %s", importPath)
		}
		if len(listed.CgoFiles) != 0 {
			t.Fatalf("%s CgoFiles = %v, want none", importPath, listed.CgoFiles)
		}
	}

	linuxBackend := packages[modulePath+"/native/internal/linuxmem"]
	if !slices.Contains(linuxBackend.GoFiles, "loader_linux.go") {
		t.Fatalf("linuxmem GoFiles = %v, want loader_linux.go", linuxBackend.GoFiles)
	}
	switch runtime.GOARCH {
	case "386":
		if !slices.Contains(linuxBackend.GoFiles, "call_386.go") || !slices.Contains(linuxBackend.SFiles, "call_386.s") {
			t.Fatalf("linux/386 bridge files: GoFiles=%v SFiles=%v", linuxBackend.GoFiles, linuxBackend.SFiles)
		}
		if slices.Contains(linuxBackend.GoFiles, "call_64.go") {
			t.Fatalf("linux/386 unexpectedly selected call_64.go: %v", linuxBackend.GoFiles)
		}
	case "amd64", "arm64":
		if !slices.Contains(linuxBackend.GoFiles, "call_64.go") {
			t.Fatalf("linux/%s GoFiles = %v, want purego call_64.go", runtime.GOARCH, linuxBackend.GoFiles)
		}
		if slices.Contains(linuxBackend.GoFiles, "call_386.go") || slices.Contains(linuxBackend.SFiles, "call_386.s") {
			t.Fatalf("linux/%s unexpectedly selected 386 dispatcher: GoFiles=%v SFiles=%v", runtime.GOARCH, linuxBackend.GoFiles, linuxBackend.SFiles)
		}
	default:
		t.Fatalf("unsupported Linux architecture %s", runtime.GOARCH)
	}
}

func TestNativePackageCallExportWithArgsLinux(t *testing.T) {
	requireCommand(t, "zig")

	fixturePath := buildArgumentSharedLib(t, t.TempDir(), runtime.GOOS, runtime.GOARCH)
	payload, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read native argument fixture: %v", err)
	}
	library, err := native.LoadLibrary(payload)
	if err != nil {
		t.Fatalf("native.LoadLibrary: %v", err)
	}
	t.Cleanup(func() { _ = library.Close() })

	if err := library.CallExport("ReflektorArgsInit"); err != nil {
		t.Fatalf("CallExport(ReflektorArgsInit): %v", err)
	}
	state, err := library.CallExportWithArgs("ReflektorArgsState")
	if err != nil {
		t.Fatalf("CallExportWithArgs(ReflektorArgsState) after init: %v", err)
	}
	if state != 40 {
		t.Fatalf("state after init = %d, want 40", state)
	}

	input := []byte("sliver-native-extension")
	const callbackReturn int32 = 0x5a17
	callbackCalls := 0
	callbackInputValid := true
	var callbackOutput []byte
	callback := purego.NewCallback(func(data uintptr, size int32) int32 {
		callbackCalls++
		var valid bool
		callbackOutput, valid = copyNativeCallbackPayload(data, size)
		if !valid {
			callbackInputValid = false
			return -1
		}
		return callbackReturn
	})

	result, err := library.CallExportWithArgs(
		"ReflektorArgsEcho",
		uintptr(unsafe.Pointer(unsafe.SliceData(input))),
		uintptr(len(input)),
		callback,
	)
	runtime.KeepAlive(input)
	if err != nil {
		t.Fatalf("CallExportWithArgs(ReflektorArgsEcho): %v", err)
	}
	if callbackCalls != 1 {
		t.Fatalf("callback calls = %d, want 1", callbackCalls)
	}
	if !callbackInputValid {
		t.Fatal("callback received a nil pointer or negative length")
	}
	if !bytes.Equal(callbackOutput, input) {
		t.Fatalf("callback output = %q, want %q", callbackOutput, input)
	}
	if result != uintptr(callbackReturn) {
		t.Fatalf("callback return = %#x, want %#x", result, uintptr(callbackReturn))
	}

	callbackAddress, err := library.CallExportWithArgs("ReflektorArgsCallbackAddress")
	if err != nil {
		t.Fatalf("CallExportWithArgs(ReflektorArgsCallbackAddress): %v", err)
	}
	result, err = library.CallExportWithArgs(
		"ReflektorArgsRun",
		uintptr(unsafe.Pointer(unsafe.SliceData(input))),
		uintptr(len(input)),
		callbackAddress,
	)
	runtime.KeepAlive(input)
	if err != nil {
		t.Fatalf("CallExportWithArgs(ReflektorArgsRun): %v", err)
	}
	var sum uintptr
	for _, value := range input {
		sum += uintptr(value)
	}
	if want := (sum << 16) | 41; result != want {
		t.Fatalf("stateful result = %#x, want %#x", result, want)
	}

	state, err = library.CallExportWithArgs("ReflektorArgsState")
	if err != nil {
		t.Fatalf("CallExportWithArgs(ReflektorArgsState) after run: %v", err)
	}
	if state != 41 {
		t.Fatalf("state after run = %d, want 41", state)
	}

	if err := library.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := library.CallExportWithArgs("ReflektorArgsState"); !errors.Is(err, native.ErrLibraryClosed) {
		t.Fatalf("call after Close error = %v, want ErrLibraryClosed", err)
	}
}

func TestNativePackageRustCallExportWithArgsLinux(t *testing.T) {
	fixturePath := buildNativeRustArgumentFixture(t)
	payload, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read native Rust argument fixture: %v", err)
	}
	library, err := native.LoadLibrary(payload)
	if err != nil {
		t.Fatalf("native.LoadLibrary(Rust): %v", err)
	}
	t.Cleanup(func() { _ = library.Close() })

	tests := []struct {
		name string
		args []uintptr
		want uintptr
	}{
		{name: "ReflektorRustArgs0", want: 0x1234},
		{name: "ReflektorRustArgs1", args: []uintptr{0x123}, want: 0x123 ^ 0x55},
		{name: "ReflektorRustArgs2", args: []uintptr{2, 7}, want: 2 + 7*3 + 7},
		{name: "ReflektorRustArgs3", args: []uintptr{2, 7, 11}, want: 2 + 7*3 + 11*5 + 11},
	}
	for _, test := range tests {
		got, err := library.CallExportWithArgs(test.name, test.args...)
		if err != nil {
			t.Fatalf("CallExportWithArgs(%s, %v): %v", test.name, test.args, err)
		}
		if got != test.want {
			t.Fatalf("CallExportWithArgs(%s, %v) = %#x, want %#x", test.name, test.args, got, test.want)
		}
	}

	if err := library.Close(); err != nil {
		t.Fatalf("Close Rust library: %v", err)
	}
}

func TestNativePackageELFLifecycleLinux(t *testing.T) {
	fixturePath := buildNativeLifecycleFixture(t)
	payload, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read native lifecycle fixture: %v", err)
	}

	wordSize := int(unsafe.Sizeof(uintptr(0)))
	record, err := unix.Mmap(-1, 0, 2*wordSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		t.Fatalf("mmap lifecycle result record: %v", err)
	}
	t.Cleanup(func() { _ = unix.Munmap(record) })

	library, err := native.LoadLibrary(payload)
	if err != nil {
		t.Fatalf("native.LoadLibrary(lifecycle): %v", err)
	}
	t.Cleanup(func() { _ = library.Close() })

	const constructorState = uintptr(0x13579bdf)
	const destructorSentinel = uintptr(0x5a17c0de)
	state, err := library.CallExportWithArgs("ReflektorLifecycleState")
	if err != nil {
		t.Fatalf("CallExportWithArgs(ReflektorLifecycleState): %v", err)
	}
	if state != constructorState {
		t.Fatalf("constructor state = %#x, want %#x", state, constructorState)
	}

	boundState, err := library.CallExportWithArgs(
		"ReflektorLifecycleBind",
		uintptr(unsafe.Pointer(unsafe.SliceData(record))),
	)
	runtime.KeepAlive(record)
	if err != nil {
		t.Fatalf("CallExportWithArgs(ReflektorLifecycleBind): %v", err)
	}
	if boundState != constructorState {
		t.Fatalf("bind result = %#x, want constructor state %#x", boundState, constructorState)
	}
	if got := nativeLifecycleRecordWord(record, 0); got != constructorState {
		t.Fatalf("bound lifecycle record = %#x, want %#x", got, constructorState)
	}
	if got := nativeLifecycleRecordWord(record, 1); got != 0 {
		t.Fatalf("pre-Close destructor count = %d, want 0", got)
	}

	if err := library.Close(); err != nil {
		t.Fatalf("Close lifecycle library: %v", err)
	}
	if got := nativeLifecycleRecordWord(record, 0); got != destructorSentinel {
		t.Fatalf("destructor sentinel = %#x, want %#x", got, destructorSentinel)
	}
	if got := nativeLifecycleRecordWord(record, 1); got != 1 {
		t.Fatalf("destructor count after Close = %d, want 1", got)
	}
	if err := library.Close(); err != nil {
		t.Fatalf("second Close lifecycle library: %v", err)
	}
	if got := nativeLifecycleRecordWord(record, 1); got != 1 {
		t.Fatalf("destructor count after second Close = %d, want 1", got)
	}
	if _, err := library.CallExportWithArgs("ReflektorLifecycleState"); !errors.Is(err, native.ErrLibraryClosed) {
		t.Fatalf("lifecycle call after Close error = %v, want ErrLibraryClosed", err)
	}
}

func buildNativeRustArgumentFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Skip("rustc not found in PATH")
	}

	output := filepath.Join(t.TempDir(), "libreflektor_native_args_rust.so")
	source := filepath.Join("..", "testdata", "rust", "native_args.rs")
	cmd := exec.Command("rustc",
		"--crate-name", "reflektor_native_args",
		"--crate-type", "cdylib",
		"--edition", "2021",
		"-C", "panic=abort",
		"-C", "opt-level=2",
		"-C", "strip=symbols",
		"-C", "link-arg=-Wl,-z,now",
		"-o", output,
		source,
	)
	if buildOutput, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build native Rust argument fixture: %v\n%s", err, buildOutput)
	}
	return output
}

func buildNativeLifecycleFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("zig"); err != nil {
		t.Skip("zig not found in PATH")
	}
	target, ok := zigTargetFor("linux", runtime.GOARCH)
	if !ok {
		t.Fatalf("unsupported Linux lifecycle target %s", runtime.GOARCH)
	}

	output := filepath.Join(t.TempDir(), "libreflektor_native_lifecycle.so")
	cmd := exec.Command("zig", "cc",
		"-target", target,
		"-shared", "-fPIC", "-nostdlib",
		"-Wl,-z,now", "-Wl,-z,defs",
		"-O2", "-g0",
		"-o", output,
		filepath.Join("..", "testdata", "c", "native_lifecycle.c"),
	)
	cmd.Env = append(
		os.Environ(),
		"ZIG_GLOBAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-global-cache"),
		"ZIG_LOCAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-local-cache"),
	)
	if buildOutput, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build native lifecycle fixture: %v\n%s", err, buildOutput)
	}
	return output
}

func nativeLifecycleRecordWord(record []byte, index int) uintptr {
	wordSize := int(unsafe.Sizeof(uintptr(0)))
	offset := index * wordSize
	if wordSize == 4 {
		return uintptr(binary.NativeEndian.Uint32(record[offset : offset+wordSize]))
	}
	return uintptr(binary.NativeEndian.Uint64(record[offset : offset+wordSize]))
}

//go:nocheckptr
func copyNativeCallbackPayload(data uintptr, size int32) ([]byte, bool) {
	if data == 0 || size < 0 {
		return nil, false
	}
	payload := unsafe.Slice((*byte)(unsafe.Pointer(data)), int(size))
	return append([]byte(nil), payload...), true
}
