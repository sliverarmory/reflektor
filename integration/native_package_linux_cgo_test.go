//go:build linux && cgo && (386 || amd64 || arm64)

package reflektor_test

import (
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sliverarmory/reflektor/native"
)

func TestNativePackageCSharedConsumerLinux(t *testing.T) {
	consumerPath := buildNativeConsumerSharedLibrary(t)

	t.Run("no-reflektor-tls-arena", func(t *testing.T) {
		consumer, err := elf.Open(consumerPath)
		if err != nil {
			t.Fatalf("open native-package c-shared consumer: %v", err)
		}
		defer consumer.Close()

		wordSize := uint64(4)
		if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
			wordSize = 8
		}
		reflektorPoolSize := uint64(64 * 2 * wordSize)
		maxGoRuntimeTLS := 2 * wordSize

		tlsSegments := 0
		for _, program := range consumer.Progs {
			if program.Type != elf.PT_TLS {
				continue
			}
			tlsSegments++
			if program.Memsz >= reflektorPoolSize {
				t.Fatalf("PT_TLS memsz %#x contains a Reflektor-sized TLS pool (pool=%#x)", program.Memsz, reflektorPoolSize)
			}
			if program.Memsz > maxGoRuntimeTLS {
				t.Fatalf("PT_TLS memsz %#x exceeds the Go runtime allowance %#x", program.Memsz, maxGoRuntimeTLS)
			}
		}
		if tlsSegments != 1 {
			t.Fatalf("PT_TLS segment count = %d, want 1 Go runtime segment", tlsSegments)
		}
	})

	t.Run("native-rejects-go-image", func(t *testing.T) {
		payload, err := os.ReadFile(consumerPath)
		if err != nil {
			t.Fatalf("read native-package c-shared consumer: %v", err)
		}
		if _, err := native.LoadLibrary(payload); !errors.Is(err, native.ErrGoSharedLibraryUnsupported) {
			t.Fatalf("native.LoadLibrary(Go c-shared) error = %v, want ErrGoSharedLibraryUnsupported", err)
		}
	})
}

func buildNativeConsumerSharedLibrary(t *testing.T) string {
	t.Helper()

	outDir := t.TempDir()
	outputPath := filepath.Join(outDir, fmt.Sprintf("native_consumer_linux-%s.so", runtime.GOARCH))
	cmd := exec.Command("go",
		"build",
		"-buildmode=c-shared",
		"-trimpath",
		"-o", outputPath,
		"../testdata/go/native_consumer",
	)
	cmd.Env = withoutEnv(overrideEnv(os.Environ(), map[string]string{
		"GOOS":        "linux",
		"GOARCH":      runtime.GOARCH,
		"CGO_ENABLED": "1",
		"GOCACHE":     filepath.Join(os.TempDir(), "reflektor-native-consumer-go-cache"),
	}), "CC", "CXX")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build native-package c-shared consumer: %v\n%s", err, output)
	}
	cleanupGoSharedSidecars(outputPath, "so")
	return outputPath
}
