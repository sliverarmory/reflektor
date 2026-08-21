//go:build bof

package reflektor_test

import (
	"debug/macho"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	reflektor "github.com/sliverarmory/reflektor"
)

var bofOptionsHostData = uintptr(0x43)

func TestLoadBOFWithOptions(t *testing.T) {
	requireCommand(t, "zig")
	target, ok := nativeBOFTarget()
	if !ok {
		t.Fatalf("missing BOF fixture target for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	extraArguments := []string(nil)
	if target.goos == "darwin" {
		extraArguments = append(extraArguments, "-DBOF_DARWIN")
	}
	path := buildBOFSource(t, t.TempDir(), target, "options_fixture", "options_fixture.c", extraArguments...)
	if target.format == "macho" {
		validateMachOOptionsGOTRelocations(t, path, target.goarch)
	}
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entryPoint := "custom_entry"
	if target.format == "macho" {
		entryPoint = "_custom_entry"
	}

	denied := errors.New("test import policy denied object")
	resolverCalled := false
	if loaded, loadErr := reflektor.LoadBOFWithOptions(image, reflektor.BOFLoadOptions{
		EntryPoint:      entryPoint,
		ValidateImports: func([]reflektor.BOFImport) error { return denied },
		ResolveSymbol: func(reflektor.BOFImport) (uintptr, bool, error) {
			resolverCalled = true
			return 0, false, nil
		},
	}); !errors.Is(loadErr, denied) || loaded != nil || resolverCalled {
		if loaded != nil {
			_ = loaded.Close()
		}
		t.Fatalf("policy rejection = loaded %#v, error %v, resolverCalled=%v", loaded, loadErr, resolverCalled)
	}

	callback := newBOFOptionsTestCallback()
	if callback == 0 {
		t.Fatal("custom callback address is zero")
	}
	validated := false
	resolvedHostValue := false
	resolvedHostData := false
	resolvedPrivileged := false
	loaded, err := reflektor.LoadBOFWithOptions(image, reflektor.BOFLoadOptions{
		EntryPoint: entryPoint,
		ValidateImports: func(imports []reflektor.BOFImport) error {
			validated = true
			assertBOFOptionsImports(t, imports)
			return nil
		},
		ResolveSymbol: func(imported reflektor.BOFImport) (uintptr, bool, error) {
			if !validated {
				t.Fatal("ResolveSymbol ran before ValidateImports")
			}
			if imported.Builtin {
				t.Fatalf("custom resolver received built-in import %#v", imported)
			}
			switch {
			case strings.Contains(imported.Name, "HostResolvedValue"):
				resolvedHostValue = true
				return callback, true, nil
			case strings.Contains(imported.Name, "HostResolvedData"):
				resolvedHostData = true
				return uintptr(unsafe.Pointer(&bofOptionsHostData)), true, nil
			case strings.Contains(imported.Name, "BeaconInjectProcess"):
				if !imported.RequiresHost {
					t.Fatalf("privileged callback is not host-required: %#v", imported)
				}
				resolvedPrivileged = true
				return callback, true, nil
			default:
				return 0, false, nil
			}
		},
	})
	if err != nil {
		t.Fatalf("LoadBOFWithOptions() error = %v", err)
	}
	defer loaded.Close()
	if !validated || !resolvedHostValue || runtime.GOOS == "darwin" && !resolvedHostData || !resolvedPrivileged {
		t.Fatalf("options callbacks: validated=%v hostValue=%v hostData=%v privileged=%v", validated, resolvedHostValue, resolvedHostData, resolvedPrivileged)
	}

	var arguments reflektor.BOFArguments
	if err := arguments.AddString(""); err != nil {
		t.Fatal(err)
	}
	if err := arguments.AddString("options"); err != nil {
		t.Fatal(err)
	}
	outputs, err := loaded.Execute(arguments.Bytes())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(outputs) != 1 || outputs[0].Type != reflektor.BOFOutputDefault || string(outputs[0].Data) != "bof-options-ok" {
		t.Fatalf("Execute() outputs = %#v", outputs)
	}
	if err := loaded.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	resolveFixtureImport := func(imported reflektor.BOFImport) (uintptr, bool, error) {
		if strings.Contains(imported.Name, "HostResolvedValue") || strings.Contains(imported.Name, "BeaconInjectProcess") {
			return callback, true, nil
		}
		if strings.Contains(imported.Name, "HostResolvedData") {
			return uintptr(unsafe.Pointer(&bofOptionsHostData)), true, nil
		}
		return 0, false, nil
	}

	for _, test := range []struct {
		name       string
		entryPoint string
		wantError  string
	}{
		{name: "missing", entryPoint: "missing_entry", wantError: "not found"},
		{name: "non-executable", entryPoint: "bof_options_global", wantError: "not in a mapped executable section"},
	} {
		t.Run(test.name+" entry", func(t *testing.T) {
			selected := test.entryPoint
			if target.format == "macho" {
				selected = "_" + selected
			}
			invalidResolverCalled := false
			invalid, loadErr := reflektor.LoadBOFWithOptions(image, reflektor.BOFLoadOptions{
				EntryPoint: selected,
				ResolveSymbol: func(imported reflektor.BOFImport) (uintptr, bool, error) {
					invalidResolverCalled = true
					return resolveFixtureImport(imported)
				},
			})
			if invalid != nil {
				_ = invalid.Close()
			}
			if loadErr == nil || !strings.Contains(loadErr.Error(), test.wantError) {
				t.Fatalf("LoadBOFWithOptions(entry=%q) = %#v, %v", selected, invalid, loadErr)
			}
			if invalidResolverCalled {
				t.Fatal("ResolveSymbol ran for an invalid entry point")
			}
		})
	}

	_, err = reflektor.LoadBOFWithOptions(image, reflektor.BOFLoadOptions{
		EntryPoint: entryPoint,
		ResolveSymbol: func(imported reflektor.BOFImport) (uintptr, bool, error) {
			if strings.Contains(imported.Name, "HostResolvedValue") {
				return callback, true, nil
			}
			if strings.Contains(imported.Name, "HostResolvedData") {
				return uintptr(unsafe.Pointer(&bofOptionsHostData)), true, nil
			}
			return 0, false, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "requires a host-provided resolver") {
		t.Fatalf("missing privileged callback error = %v", err)
	}
}

func validateMachOOptionsGOTRelocations(t *testing.T, path, arch string) {
	t.Helper()
	file, err := macho.Open(path)
	if err != nil {
		t.Fatalf("open Mach-O options fixture: %v", err)
	}
	defer file.Close()
	types := make(map[uint8]bool)
	for _, section := range file.Sections {
		for _, relocation := range section.Relocs {
			types[relocation.Type] = true
		}
	}
	switch arch {
	case "amd64":
		if !types[3] && !types[4] { // X86_64_RELOC_GOT_LOAD / X86_64_RELOC_GOT
			t.Fatalf("Mach-O amd64 options fixture has no GOT relocation: %v", types)
		}
	case "arm64":
		if !types[5] || !types[6] { // ARM64_RELOC_GOT_LOAD_PAGE21 / PAGEOFF12
			t.Fatalf("Mach-O arm64 options fixture lacks GOT page pair: %v", types)
		}
	default:
		t.Fatalf("unexpected Mach-O options fixture architecture %q", arch)
	}
}

func assertBOFOptionsImports(t *testing.T, imports []reflektor.BOFImport) {
	t.Helper()
	want := map[string]bool{
		"BeaconDataParse":         false,
		"BeaconDataExtractOrNull": false,
		"BeaconOutput":            false,
		"toWideChar":              false,
		"HostResolvedValue":       false,
		"BeaconInjectProcess":     false,
	}
	if runtime.GOOS == "darwin" {
		want["HostResolvedData"] = false
	}
	for _, imported := range imports {
		for name := range want {
			if !strings.Contains(imported.Name, name) {
				continue
			}
			want[name] = true
			switch name {
			case "HostResolvedValue", "HostResolvedData":
				if imported.Builtin || imported.RequiresHost {
					t.Fatalf("custom import classification = %#v", imported)
				}
			case "BeaconInjectProcess":
				if imported.Builtin || !imported.RequiresHost {
					t.Fatalf("privileged import classification = %#v", imported)
				}
			default:
				if !imported.Builtin || imported.RequiresHost {
					t.Fatalf("built-in import classification = %#v", imported)
				}
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("import policy did not observe %s: %#v", name, imports)
		}
	}
}
