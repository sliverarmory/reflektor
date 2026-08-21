package bofloader

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestObjectImportsAreDeterministicAndClassified(t *testing.T) {
	object := &objectFile{symbols: map[uint32]objectSymbol{
		1: {index: 1, name: "BeaconInjectProcess", section: sectionUndefined, weak: true},
		2: {index: 2, name: "__imp_BeaconOutput", section: sectionUndefined},
		3: {index: 3, name: "HOST$Callback", section: sectionUndefined},
		4: {index: 4, name: "BeaconInjectProcess", section: sectionUndefined},
		5: {index: 5, name: "defined", section: 0},
	}}
	got := objectImports(object, []uint32{3, 1, 5, 2, 4})
	want := []Import{
		{Name: "BeaconInjectProcess", RequiresHost: true},
		{Name: "HOST$Callback"},
		{Name: "__imp_BeaconOutput", Builtin: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("objectImports() = %#v, want %#v", got, want)
	}
}

func TestObjectImportsTreatsGlobalOffsetTableAsELFPseudoSymbolOnly(t *testing.T) {
	for _, format := range []string{"elf", "coff", "macho"} {
		object := &objectFile{format: format, symbols: map[uint32]objectSymbol{
			1: {index: 1, name: "_GLOBAL_OFFSET_TABLE_", section: sectionUndefined},
		}}
		imports := objectImports(object, []uint32{1})
		if format == "elf" && len(imports) != 0 {
			t.Fatalf("ELF imports = %#v, want pseudo-symbol omitted", imports)
		}
		if format != "elf" && (len(imports) != 1 || imports[0].Name != "_GLOBAL_OFFSET_TABLE_") {
			t.Fatalf("%s imports = %#v, want real import", format, imports)
		}
	}
}

func TestResolveImportedSymbolOrderingAndHostRequirement(t *testing.T) {
	customCalled := false
	address, err := resolveImportedSymbol(objectSymbol{name: "BeaconOutput", section: sectionUndefined}, LoadOptions{
		ResolveSymbol: func(Import) (uintptr, bool, error) {
			customCalled = true
			return 1, true, nil
		},
	})
	if err != nil || address == 0 || customCalled {
		t.Fatalf("built-in resolution = %#x, %v; customCalled=%v", address, err, customCalled)
	}

	const hostAddress = uintptr(0x12345678)
	address, err = resolveImportedSymbol(objectSymbol{name: "BeaconInjectProcess", section: sectionUndefined}, LoadOptions{
		ResolveSymbol: func(imported Import) (uintptr, bool, error) {
			if !imported.RequiresHost || imported.Builtin {
				t.Fatalf("host import classification = %#v", imported)
			}
			return hostAddress, true, nil
		},
	})
	if err != nil || address != hostAddress {
		t.Fatalf("host resolution = %#x, %v", address, err)
	}

	_, err = resolveImportedSymbol(objectSymbol{name: "BeaconInjectProcess", section: sectionUndefined}, LoadOptions{})
	if err == nil || !strings.Contains(err.Error(), "requires a host-provided resolver") {
		t.Fatalf("missing host callback error = %v", err)
	}

	wantErr := errors.New("denied")
	_, err = resolveImportedSymbol(objectSymbol{name: "HOST$Callback", section: sectionUndefined, weak: true}, LoadOptions{
		ResolveSymbol: func(Import) (uintptr, bool, error) { return 0, false, wantErr },
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("weak custom resolver error = %v, want %v", err, wantErr)
	}

	_, err = resolveImportedSymbol(objectSymbol{name: "HOST$Callback", section: sectionUndefined}, LoadOptions{
		ResolveSymbol: func(Import) (uintptr, bool, error) { return 0, true, nil },
	})
	if err == nil || !strings.Contains(err.Error(), "zero address") {
		t.Fatalf("zero handled address error = %v", err)
	}
}

func TestPrivilegedBeaconCallbacksRequireHostResolver(t *testing.T) {
	for _, name := range []string{
		"BeaconUseToken",
		"BeaconRevertToken",
		"BeaconIsAdmin",
		"BeaconGetSpawnTo",
		"BeaconSpawnTemporaryProcess",
		"BeaconInjectProcess",
		"BeaconInjectTemporaryProcess",
		"BeaconCleanupProcess",
	} {
		t.Run(name, func(t *testing.T) {
			imported := classifyImport(objectSymbol{name: name, section: sectionUndefined})
			if imported.Builtin || !imported.RequiresHost {
				t.Fatalf("classifyImport(%q) = %#v", name, imported)
			}
			if _, err := resolveImportedSymbol(objectSymbol{name: name, section: sectionUndefined}, LoadOptions{}); err == nil || !strings.Contains(err.Error(), "requires a host-provided resolver") {
				t.Fatalf("resolveImportedSymbol(%q) error = %v", name, err)
			}
		})
	}
}

func TestResolveExternalSymbolsCachesExactImportNames(t *testing.T) {
	region, err := allocateMemory(2 * systemPageSize())
	if err != nil {
		t.Fatal(err)
	}
	defer region.close()
	object := &objectFile{symbols: map[uint32]objectSymbol{
		1: {index: 1, name: "HOST$Same", section: sectionUndefined, weak: true},
		2: {index: 2, name: "HOST$Same", section: sectionUndefined},
	}}
	referenced := []uint32{1, 2}
	imports := objectImports(object, referenced)
	resolverCalls := 0
	_, err = resolveExternalSymbols(
		object,
		referenced,
		region,
		uint64(systemPageSize()),
		uint64(systemPageSize()),
		imports,
		LoadOptions{ResolveSymbol: func(imported Import) (uintptr, bool, error) {
			resolverCalls++
			if imported.Weak {
				t.Fatalf("duplicate import remained weak: %#v", imported)
			}
			return 0x12345678, true, nil
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolverCalls != 1 {
		t.Fatalf("ResolveSymbol calls = %d, want 1", resolverCalls)
	}
}
