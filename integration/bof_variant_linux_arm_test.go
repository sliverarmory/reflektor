//go:build linux && !android && arm && arm.7

package reflektor_test

import (
	"os"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/sliverarmory/reflektor/bof"
)

func TestLinuxARMRuntimeVariant(t *testing.T) {
	target, ok := nativeBOFTarget()
	if !ok {
		t.Fatal("missing native linux/arm BOF target")
	}
	image, err := os.ReadFile(buildBOFFixture(t, t.TempDir(), target))
	if err != nil {
		t.Fatal(err)
	}

	goarm := executableGOARM(t)
	loaded, err := bof.Load(image)
	if strings.Contains(goarm, "softfloat") {
		if err == nil {
			_ = loaded.Close()
			t.Fatal("bof.Load() accepted a soft-float linux/arm executable")
		}
		if !strings.Contains(err.Error(), "require GOARM=7 hard-float") {
			t.Fatalf("bof.Load() error = %v, want hard-float requirement", err)
		}
		return
	}
	if goarm != "7" && goarm != "7,hardfloat" {
		t.Fatalf("unexpected GOARM build setting %q", goarm)
	}
	if err != nil {
		t.Fatalf("bof.Load() error = %v", err)
	}
	if err := loaded.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func executableGOARM(t *testing.T) string {
	t.Helper()
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("executable build settings are unavailable")
	}
	for _, setting := range info.Settings {
		if setting.Key == "GOARM" {
			return setting.Value
		}
	}
	t.Fatal("executable does not record GOARM")
	return ""
}
