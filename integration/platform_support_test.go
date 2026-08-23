package reflektor_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
)

const unsupportedPlatformStatus = "unsupported"

type platformSupportManifest struct {
	Version         int                     `json:"version"`
	StatusValues    []string                `json:"status_values"`
	EndianValues    []string                `json:"endian_values"`
	BOFFormatValues []string                `json:"bof_format_values"`
	RunnerValues    []string                `json:"runner_values"`
	ExcludedTargets []string                `json:"excluded_targets"`
	Targets         []platformSupportTarget `json:"targets"`
}

type platformSupportTarget struct {
	GOOS            string `json:"goos"`
	GOARCH          string `json:"goarch"`
	GOARM           string `json:"goarm,omitempty"`
	Endian          string `json:"endian"`
	BOFFormat       string `json:"bof_format"`
	BOF             string `json:"bof"`
	BOFCGOFree      bool   `json:"bof_cgo_free"`
	RootShared      string `json:"root_shared"`
	NativeShared    string `json:"native_shared"`
	RecursiveShared string `json:"recursive_shared"`
	GoCShared       string `json:"go_c_shared"`
	SharedCGOFree   bool   `json:"shared_cgo_free"`
	Runner          string `json:"runner"`
}

func TestPlatformSupportManifest(t *testing.T) {
	data, err := os.ReadFile("../platform-support.json")
	if err != nil {
		t.Fatalf("read platform support manifest: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest platformSupportManifest
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode platform support manifest: %v", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		t.Fatalf("decode platform support manifest: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("platform support manifest version = %d, want 1", manifest.Version)
	}
	wantStatuses := []string{"runtime", "runtime-emulated", unsupportedPlatformStatus, "n-a"}
	if !slices.Equal(manifest.StatusValues, wantStatuses) {
		t.Fatalf("status_values = %v, want %v", manifest.StatusValues, wantStatuses)
	}
	wantExcluded := []string{"js/wasm", "wasip1/wasm"}
	if !slices.Equal(manifest.ExcludedTargets, wantExcluded) {
		t.Fatalf("excluded_targets = %v, want %v", manifest.ExcludedTargets, wantExcluded)
	}
	if !slices.Equal(manifest.EndianValues, []string{"little", "big"}) {
		t.Fatalf("endian_values = %v, want little and big", manifest.EndianValues)
	}
	if !slices.Equal(manifest.BOFFormatValues, []string{"elf", "macho", "coff", "xcoff", "undefined"}) {
		t.Fatalf("bof_format_values = %v, want the declared native object formats", manifest.BOFFormatValues)
	}
	if !slices.Equal(manifest.RunnerValues, []string{"github-native", "github-container", "github-qemu-arm-v7", "github-qemu-ppc64le", "github-qemu-riscv64", "none"}) {
		t.Fatalf("runner_values = %v, want the checked-in CI proof types", manifest.RunnerValues)
	}

	distOutput, err := exec.Command("go", "tool", "dist", "list").CombinedOutput()
	if err != nil {
		t.Fatalf("go tool dist list: %v\n%s", err, distOutput)
	}
	excluded := make(map[string]struct{}, len(manifest.ExcludedTargets))
	for _, target := range manifest.ExcludedTargets {
		excluded[target] = struct{}{}
	}
	var wantTargets []string
	for _, target := range strings.Fields(string(distOutput)) {
		if _, skip := excluded[target]; !skip {
			wantTargets = append(wantTargets, target)
		}
	}

	allowedStatuses := make(map[string]struct{}, len(manifest.StatusValues))
	for _, status := range manifest.StatusValues {
		allowedStatuses[status] = struct{}{}
	}
	allowedEndian := stringSet(manifest.EndianValues)
	allowedBOFFormats := stringSet(manifest.BOFFormatValues)
	allowedRunners := stringSet(manifest.RunnerValues)
	cSharedTargets := pinnedGoCSharedTargets(t, excluded)
	gotTargets := make([]string, 0, len(manifest.Targets))
	for index, target := range manifest.Targets {
		name := target.GOOS + "/" + target.GOARCH
		gotTargets = append(gotTargets, name)
		for surface, status := range map[string]string{
			"bof":              target.BOF,
			"root_shared":      target.RootShared,
			"native_shared":    target.NativeShared,
			"recursive_shared": target.RecursiveShared,
			"go_c_shared":      target.GoCShared,
		} {
			if _, ok := allowedStatuses[status]; !ok {
				t.Fatalf("targets[%d] %s status %q is not declared", index, surface, status)
			}
		}
		if _, ok := allowedEndian[target.Endian]; !ok {
			t.Fatalf("targets[%d] endian %q is not declared", index, target.Endian)
		}
		if _, ok := allowedBOFFormats[target.BOFFormat]; !ok {
			t.Fatalf("targets[%d] BOF format %q is not declared", index, target.BOFFormat)
		}
		if _, ok := allowedRunners[target.Runner]; !ok {
			t.Fatalf("targets[%d] runner %q is not declared", index, target.Runner)
		}
		if target.Endian != expectedEndian(target.GOARCH) {
			t.Fatalf("targets[%d] %s endian = %s, want %s", index, name, target.Endian, expectedEndian(target.GOARCH))
		}
		if target.BOFFormat != expectedBOFFormat(target.GOOS) {
			t.Fatalf("targets[%d] %s BOF format = %s, want %s", index, name, target.BOFFormat, expectedBOFFormat(target.GOOS))
		}
		if name == "linux/arm" {
			if target.GOARM != "7,hardfloat" {
				t.Fatalf("targets[%d] %s GOARM = %q, want 7,hardfloat", index, name, target.GOARM)
			}
		} else if target.GOARM != "" {
			t.Fatalf("targets[%d] %s unexpectedly declares GOARM = %q", index, name, target.GOARM)
		}
		if target.BOFCGOFree && target.BOF == unsupportedPlatformStatus {
			t.Fatalf("targets[%d] %s marks unsupported BOF execution as CGO-free", index, name)
		}
		if target.SharedCGOFree && target.RootShared == unsupportedPlatformStatus {
			t.Fatalf("targets[%d] %s marks unsupported root shared loading as CGO-free", index, name)
		}
		if target.BOF == "n-a" || target.RootShared == "n-a" || target.NativeShared == "n-a" || target.RecursiveShared == "n-a" {
			t.Fatalf("targets[%d] %s uses n-a outside go_c_shared", index, name)
		}
		if target.GoCShared != "runtime" && target.GoCShared != "runtime-emulated" && target.GoCShared != unsupportedPlatformStatus && target.GoCShared != "n-a" {
			t.Fatalf("targets[%d] %s go_c_shared = %s, want runtime, runtime-emulated, unsupported, or n-a", index, name, target.GoCShared)
		}
		_, goSupportsCShared := cSharedTargets[name]
		if (target.GoCShared != "n-a") != goSupportsCShared {
			t.Fatalf("targets[%d] %s go_c_shared = %s, pinned Go toolchain support = %v", index, name, target.GoCShared, goSupportsCShared)
		}
		hasRuntimeProof := target.BOF == "runtime" || target.BOF == "runtime-emulated" ||
			target.RootShared == "runtime" || target.RootShared == "runtime-emulated" ||
			target.NativeShared == "runtime" || target.NativeShared == "runtime-emulated" ||
			target.RecursiveShared == "runtime" || target.RecursiveShared == "runtime-emulated" ||
			target.GoCShared == "runtime" || target.GoCShared == "runtime-emulated"
		if hasRuntimeProof != (target.Runner != "none") {
			t.Fatalf("targets[%d] %s runtime status/runner disagree: runner=%s", index, name, target.Runner)
		}
	}
	if !slices.Equal(gotTargets, wantTargets) {
		t.Fatalf("manifest targets do not exactly match non-Wasm go tool dist list\ngot:  %v\nwant: %v", gotTargets, wantTargets)
	}

	assertManifestSurfaceTargets(t, manifest.Targets, "bof", func(target platformSupportTarget) string { return target.BOF }, bofTargetNames())
	sharedTargets := sharedLibraryTargetNames()
	assertManifestSurfaceTargets(t, manifest.Targets, "root_shared", func(target platformSupportTarget) string { return target.RootShared }, sharedTargets)
	assertManifestSurfaceTargets(t, manifest.Targets, "native_shared", func(target platformSupportTarget) string { return target.NativeShared }, sharedTargets)
	assertManifestSurfaceTargets(t, manifest.Targets, "recursive_shared", func(target platformSupportTarget) string { return target.RecursiveShared }, sharedTargets)
	assertManifestSurfaceTargets(t, manifest.Targets, "go_c_shared_runtime", func(target platformSupportTarget) string {
		if target.GoCShared == "runtime" || target.GoCShared == "runtime-emulated" {
			return "runtime"
		}
		return unsupportedPlatformStatus
	}, sharedTargets)

	sawLinuxARM := false
	sawLinuxPPC64LE := false
	sawLinuxRISCV64 := false
	for _, target := range manifest.Targets {
		if target.GOOS == "linux" && target.GOARCH == "arm" {
			if target.GOARM != "7,hardfloat" || target.BOF != "runtime-emulated" || !target.BOFCGOFree ||
				target.RootShared != "runtime-emulated" || target.NativeShared != "runtime-emulated" ||
				target.RecursiveShared != "runtime-emulated" || target.GoCShared != "runtime-emulated" ||
				!target.SharedCGOFree || target.Runner != "github-qemu-arm-v7" {
				t.Fatalf("linux/arm capabilities = %#v, want full QEMU runtime support with CGO-free C/Rust loading", target)
			}
			sawLinuxARM = true
		}
		if target.GOOS == "linux" && target.GOARCH == "riscv64" {
			if target.BOF != "runtime-emulated" || !target.BOFCGOFree ||
				target.RootShared != "runtime-emulated" || target.NativeShared != "runtime-emulated" ||
				target.RecursiveShared != "runtime-emulated" || target.GoCShared != "runtime-emulated" ||
				!target.SharedCGOFree || target.Runner != "github-qemu-riscv64" {
				t.Fatalf("linux/riscv64 capabilities = %#v, want full QEMU runtime support with CGO-free C/Rust loading", target)
			}
			sawLinuxRISCV64 = true
		}
		if target.GOOS == "linux" && target.GOARCH == "ppc64le" {
			if target.BOF != "runtime-emulated" || !target.BOFCGOFree ||
				target.RootShared != "runtime-emulated" || target.NativeShared != "runtime-emulated" ||
				target.RecursiveShared != "runtime-emulated" || target.GoCShared != "runtime-emulated" ||
				!target.SharedCGOFree || target.Runner != "github-qemu-ppc64le" {
				t.Fatalf("linux/ppc64le capabilities = %#v, want full QEMU runtime support with CGO-free C/Rust loading", target)
			}
			sawLinuxPPC64LE = true
		}
	}
	if !sawLinuxARM || !sawLinuxPPC64LE || !sawLinuxRISCV64 {
		t.Fatalf("platform support manifest emulated Linux targets: arm=%v ppc64le=%v riscv64=%v, want all three", sawLinuxARM, sawLinuxPPC64LE, sawLinuxRISCV64)
	}
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func expectedEndian(goarch string) string {
	switch goarch {
	case "mips", "mips64", "ppc64", "s390x":
		return "big"
	default:
		return "little"
	}
}

func expectedBOFFormat(goos string) string {
	switch goos {
	case "aix":
		return "xcoff"
	case "darwin", "ios":
		return "macho"
	case "plan9":
		return "undefined"
	case "windows":
		return "coff"
	default:
		return "elf"
	}
}

func pinnedGoCSharedTargets(t *testing.T, excluded map[string]struct{}) map[string]struct{} {
	t.Helper()
	supportedPath := filepath.Join(runtime.GOROOT(), "src", "internal", "platform", "supported.go")
	data, err := os.ReadFile(supportedPath)
	if err != nil {
		t.Fatalf("read pinned Go build-mode support from %s: %v", supportedPath, err)
	}
	start := bytes.Index(data, []byte(`case "c-shared":`))
	if start < 0 {
		t.Fatalf("%s has no c-shared build-mode case", supportedPath)
	}
	endOffset := bytes.Index(data[start:], []byte(`case "default":`))
	if endOffset < 0 {
		t.Fatalf("%s has no build-mode case after c-shared", supportedPath)
	}
	targetPattern := regexp.MustCompile(`"([a-z0-9]+/[a-z0-9]+)"`)
	result := make(map[string]struct{})
	for _, match := range targetPattern.FindAllSubmatch(data[start:start+endOffset], -1) {
		target := string(match[1])
		if _, skip := excluded[target]; !skip {
			result[target] = struct{}{}
		}
	}
	if len(result) == 0 {
		t.Fatalf("%s c-shared build-mode case contains no non-Wasm targets", supportedPath)
	}
	return result
}

func assertManifestSurfaceTargets(t *testing.T, targets []platformSupportTarget, surface string, status func(platformSupportTarget) string, want []string) {
	t.Helper()
	var got []string
	for _, target := range targets {
		if status(target) != unsupportedPlatformStatus {
			got = append(got, target.GOOS+"/"+target.GOARCH)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%s runtime targets = %v, want %v", surface, got, want)
	}
}

func bofTargetNames() []string {
	targets := make([]string, 0, len(bofTargets))
	for _, target := range bofTargets {
		targets = append(targets, fmt.Sprintf("%s/%s", target.goos, target.goarch))
	}
	sort.Strings(targets)
	return targets
}

func sharedLibraryTargetNames() []string {
	targets := make([]string, 0, len(sharedLibTargets))
	for _, target := range sharedLibTargets {
		targets = append(targets, fmt.Sprintf("%s/%s", target.goos, target.goarch))
	}
	sort.Strings(targets)
	return targets
}
