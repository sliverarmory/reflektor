package reflektor

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sliverarmory/reflektor/memmod"
)

func TestDependencyCandidatesKeepAbsoluteImportsExact(t *testing.T) {
	absolute := filepath.Join(t.TempDir(), "libexact.so")
	got := dependencyCandidates(memmod.DependencyRequest{
		Name:         absolute,
		ImporterPath: filepath.Join(t.TempDir(), "root.so"),
		SearchPaths:  []string{t.TempDir()},
	})
	if want := []string{absolute}; !reflect.DeepEqual(got, want) {
		t.Fatalf("absolute dependency candidates = %#v, want %#v", got, want)
	}
}

func TestReadRegularDependencyRejectsDirectoriesAndOversizedFiles(t *testing.T) {
	directory := t.TempDir()
	if _, err := readRegularDependency(directory); err == nil {
		t.Fatal("directory was accepted as a recursive dependency")
	}

	oversized := filepath.Join(directory, "oversized.so")
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxRecursiveDependencyFileSize + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readRegularDependency(oversized); err == nil {
		t.Fatal("oversized file was accepted as a recursive dependency")
	}

	for _, path := range []string{directory, oversized} {
		_, err := readDependencyFile(memmod.DependencyRequest{Name: path, ImporterPath: filepath.Join(directory, "root.so")})
		if err == nil {
			t.Fatalf("unsafe dependency %q was accepted", path)
		}
		if errors.Is(err, memmod.ErrDependencyNotFound) {
			t.Fatalf("terminal dependency error for %q was misclassified as not found: %v", path, err)
		}
	}
}

func TestDependencyCandidatesExpandOriginSearchPath(t *testing.T) {
	directory := t.TempDir()
	importer := filepath.Join(directory, "root.so")
	name := "libchild.so"
	got := dependencyCandidates(memmod.DependencyRequest{
		Name:         name,
		ImporterPath: importer,
		SearchPaths:  []string{"$ORIGIN/deps"},
	})
	wantPrefix := []string{filepath.Join(directory, "deps", name)}
	if len(got) < len(wantPrefix) || !reflect.DeepEqual(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("origin dependency candidates = %#v, want prefix %#v", got, wantPrefix)
	}
}

func TestDependencyCandidatesDoNotFallBackToProcessWorkingDirectory(t *testing.T) {
	name := "libreflektor-cwd-only-test.so"
	got := dependencyCandidates(memmod.DependencyRequest{
		Name:         name,
		ImporterPath: filepath.Join(t.TempDir(), "root.so"),
	})
	for _, candidate := range got {
		if candidate == name || candidate == filepath.Join(".", name) {
			t.Fatalf("dependency candidates unexpectedly contain process-CWD path %q: %#v", candidate, got)
		}
	}
}

func TestMemoryLibraryOriginIsUnique(t *testing.T) {
	first, err := memoryLibraryOrigin([]byte("same image"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := memoryLibraryOrigin([]byte("same image"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("memory library origins collided: %q", first)
	}
}

func TestRecursiveResolverInputValidation(t *testing.T) {
	resolver := func(DependencyRequest) (Dependency, error) { return Dependency{}, ErrDependencyNotFound }
	for _, test := range []struct {
		name     string
		data     []byte
		origin   string
		resolver DependencyResolver
		message  string
	}{
		{"empty image", nil, filepath.Join(t.TempDir(), "root.so"), resolver, "empty library image"},
		{"relative origin", []byte{1}, "root.so", resolver, "absolute path"},
		{"nil resolver", []byte{1}, filepath.Join(t.TempDir(), "root.so"), nil, "resolver is nil"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadLibraryRecursiveWithResolver(test.data, test.origin, test.resolver)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestCheckedResolvedDependencyCopiesBytesAndLimitsSize(t *testing.T) {
	request := memmod.DependencyRequest{Name: "child.so", ImporterPath: filepath.Join(t.TempDir(), "root.so")}
	path := filepath.Join(t.TempDir(), "child.so")
	input := []byte{1, 2, 3}
	result, err := checkedResolvedDependency(Dependency{Data: input, Path: path}, request, 3)
	if err != nil {
		t.Fatalf("checkedResolvedDependency: %v", err)
	}
	input[0] = 9
	if result.Data[0] != 1 || result.Path != path {
		t.Fatalf("resolved dependency = %#v, want copied bytes and stable absolute path", result)
	}

	for _, test := range []struct {
		name       string
		dependency Dependency
	}{
		{"relative path", Dependency{Data: []byte{1}, Path: "child.so"}},
		{"oversized bytes", Dependency{Data: []byte{1, 2, 3, 4}, Path: path}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := checkedResolvedDependency(test.dependency, request, 3)
			if err == nil || errors.Is(err, ErrDependencyNotFound) {
				t.Fatalf("invalid resolved dependency error = %v, want terminal error", err)
			}
		})
	}
}
