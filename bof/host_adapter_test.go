package bof

import (
	"errors"
	"strings"
	"testing"
)

func TestHostAdapterRequiresExplicitServices(t *testing.T) {
	adapter := NewHostAdapter(HostServices{
		UseToken:    func(TokenHandle) (bool, error) { return true, nil },
		RevertToken: func() error { return nil },
	})
	options := adapter.Apply(LoadOptions{})
	if err := options.ValidateImports([]Import{{Name: "__imp__BeaconUseToken", RequiresHost: true}}); err != nil {
		t.Fatalf("registered token service denied: %v", err)
	}
	for _, name := range []string{"BeaconSpawnTemporaryProcess", "BeaconInjectProcess", "BeaconUseToken@0"} {
		if err := options.ValidateImports([]Import{{Name: name, RequiresHost: true}}); err == nil || !strings.Contains(err.Error(), "no service") {
			t.Errorf("ValidateImports(%q) = %v, want explicit rejection", name, err)
		}
		if address, handled, err := options.ResolveSymbol(Import{Name: name, RequiresHost: true}); address != 0 || handled || err != nil {
			t.Errorf("ResolveSymbol(%q) = %#x, %v, %v", name, address, handled, err)
		}
	}
	if err := NewHostAdapter(HostServices{UseToken: func(TokenHandle) (bool, error) { return true, nil }}).
		Apply(LoadOptions{}).ValidateImports([]Import{{Name: "BeaconUseToken", RequiresHost: true}}); err == nil {
		t.Fatal("UseToken without RevertToken was accepted")
	}
}

func TestHostAdapterComposesCustomImportOptions(t *testing.T) {
	denied := errors.New("denied by host")
	validated := false
	resolved := false
	base := LoadOptions{
		EntryPoint: "custom",
		ValidateImports: func(imports []Import) error {
			validated = true
			if len(imports) != 1 || imports[0].Name != "CustomValue" {
				t.Fatalf("imports = %#v", imports)
			}
			return denied
		},
		ResolveSymbol: func(imported Import) (uintptr, bool, error) {
			resolved = true
			if imported.Name != "CustomValue" {
				t.Fatalf("import = %#v", imported)
			}
			return 0x1234, true, nil
		},
	}
	options := NewHostAdapter(HostServices{}).Apply(base)
	if options.EntryPoint != "custom" {
		t.Fatalf("entry point = %q", options.EntryPoint)
	}
	if err := options.ValidateImports([]Import{{Name: "CustomValue"}}); !errors.Is(err, denied) || !validated {
		t.Fatalf("validation = %v, called=%v", err, validated)
	}
	if address, handled, err := options.ResolveSymbol(Import{Name: "CustomValue"}); address != 0x1234 || !handled || err != nil || !resolved {
		t.Fatalf("resolution = %#x, %v, %v, called=%v", address, handled, err, resolved)
	}
}

func TestHostAdapterServiceRequiresActiveEntryThread(t *testing.T) {
	calls := 0
	adapter := NewHostAdapter(HostServices{UseToken: func(TokenHandle) (bool, error) {
		calls++
		return true, nil
	}})
	if got := adapter.useToken(0x345); got != 0 || calls != 0 {
		t.Fatalf("callback outside execution = %#x, calls=%d", got, calls)
	}
}
