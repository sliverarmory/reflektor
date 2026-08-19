package reflektor_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type recursiveSharedLibraries struct {
	root   string
	middle string
	leaf   string
}

func TestBuildRecursiveCSharedLibraryMatrix(t *testing.T) {
	requireCommand(t, "zig")
	requireCommand(t, "file")
	requireCommand(t, "nm")
	requireCommand(t, "objdump")

	outDir := t.TempDir()
	for _, target := range sharedLibTargets {
		target := target
		t.Run(fmt.Sprintf("%s-%s", target.goos, target.goarch), func(t *testing.T) {
			graphDir := filepath.Join(outDir, target.goos+"-"+target.goarch)
			if err := os.MkdirAll(graphDir, 0o755); err != nil {
				t.Fatalf("create recursive fixture directory: %v", err)
			}
			libraries := buildRecursiveSharedLibs(t, graphDir, target.goos, target.goarch)
			for _, path := range []string{libraries.leaf, libraries.middle, libraries.root} {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatalf("stat %s: %v", path, err)
				}
				if info.Size() == 0 {
					t.Fatalf("empty recursive fixture: %s", path)
				}
				if fileOut := runCmd(t, "file", path); !strings.Contains(fileOut, target.fileProbe) {
					t.Fatalf("unexpected architecture probe for %s: want substring %q, got %q", path, target.fileProbe, fileOut)
				}
			}

			switch target.goos {
			case "windows":
				if out := runCmd(t, "objdump", "-p", libraries.root); !strings.Contains(out, "StartW") || !strings.Contains(strings.ToLower(out), "libreflektor_middle.dll") {
					t.Fatalf("recursive Windows root is missing StartW or middle import:\n%s", out)
				}
			case "darwin":
				if out := runCmd(t, "nm", libraries.root); !strings.Contains(out, "_StartW") {
					t.Fatalf("recursive Darwin root is missing _StartW:\n%s", out)
				}
			default:
				if out := runCmd(t, "objdump", "-p", libraries.root); !strings.Contains(out, "libreflektor_middle.so") {
					t.Fatalf("recursive Linux root is missing middle DT_NEEDED entry:\n%s", out)
				}
			}
		})
	}
}

func buildRecursiveSharedLibs(t *testing.T, outDir string, goos string, goarch string) recursiveSharedLibraries {
	t.Helper()

	target, ok := zigTargetFor(goos, goarch)
	if !ok {
		t.Fatalf("unsupported recursive shared-library target %s/%s", goos, goarch)
	}

	ext, err := sharedLibExt(goos)
	if err != nil {
		t.Fatalf("recursive shared-library extension: %v", err)
	}
	leaf := filepath.Join(outDir, "libreflektor_leaf."+ext)
	middle := filepath.Join(outDir, "libreflektor_middle."+ext)
	root := filepath.Join(outDir, "reflektor_recursive_root."+ext)

	common := []string{"cc", "-target", target, "-O2", "-g0"}
	switch goos {
	case "darwin":
		buildRecursiveFixture(t, common, []string{
			"-dynamiclib", "-fPIC",
			"-Wl,-install_name,@rpath/libreflektor_leaf.dylib",
			"-o", leaf, filepath.Join("..", "testdata", "c", "recursive_leaf.c"),
		})
		buildRecursiveFixture(t, common, []string{
			"-dynamiclib", "-fPIC",
			"-Wl,-install_name,@rpath/libreflektor_middle.dylib",
			"-Wl,-rpath,@loader_path",
			"-o", middle, filepath.Join("..", "testdata", "c", "recursive_middle.c"),
			"-L" + outDir, "-lreflektor_leaf",
		})
		buildRecursiveFixture(t, common, []string{
			"-dynamiclib", "-fPIC",
			"-Wl,-rpath,@loader_path",
			"-o", root, filepath.Join("..", "testdata", "c", "recursive_root.c"),
			"-L" + outDir, "-lreflektor_middle",
		})
	case "linux":
		buildRecursiveFixture(t, common, []string{
			"-shared", "-fPIC", "-Wl,-z,now", "-Wl,-z,defs",
			"-Wl,-soname,libreflektor_leaf.so",
			"-o", leaf, filepath.Join("..", "testdata", "c", "recursive_leaf.c"),
		})
		buildRecursiveFixture(t, common, []string{
			"-shared", "-fPIC", "-Wl,-z,now", "-Wl,-z,defs",
			"-Wl,-soname,libreflektor_middle.so",
			"-Wl,--enable-new-dtags", "-Wl,-rpath,$ORIGIN",
			"-o", middle, filepath.Join("..", "testdata", "c", "recursive_middle.c"),
			"-L" + outDir, "-Wl,--no-as-needed", "-lreflektor_leaf",
		})
		buildRecursiveFixture(t, common, []string{
			"-shared", "-fPIC", "-Wl,-z,now", "-Wl,-z,defs",
			"-Wl,-soname,reflektor_recursive_root.so",
			"-Wl,--enable-new-dtags", "-Wl,-rpath,$ORIGIN",
			"-o", root, filepath.Join("..", "testdata", "c", "recursive_root.c"),
			"-L" + outDir, "-Wl,--no-as-needed", "-lreflektor_middle",
		})
	case "windows":
		leafImport := filepath.Join(outDir, "libreflektor_leaf.lib")
		middleImport := filepath.Join(outDir, "libreflektor_middle.lib")
		buildRecursiveFixture(t, common, []string{
			"-shared", "-Wl,--out-implib," + leafImport,
			"-o", leaf, filepath.Join("..", "testdata", "c", "recursive_leaf.c"),
		})
		buildRecursiveFixture(t, common, []string{
			"-shared", "-Wl,--out-implib," + middleImport,
			"-o", middle, filepath.Join("..", "testdata", "c", "recursive_middle.c"), leafImport,
		})
		buildRecursiveFixture(t, common, []string{
			"-shared", "-o", root, filepath.Join("..", "testdata", "c", "recursive_root.c"), middleImport,
		})
	default:
		t.Fatalf("unsupported recursive shared-library OS %s", goos)
	}

	return recursiveSharedLibraries{root: root, middle: middle, leaf: leaf}
}

func buildRecursiveFixture(t *testing.T, common []string, args []string) {
	t.Helper()
	cmdArgs := append(append([]string{}, common...), args...)
	cmd := exec.Command("zig", cmdArgs...)
	cmd.Env = append(
		os.Environ(),
		"ZIG_GLOBAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-global-cache"),
		"ZIG_LOCAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-local-cache"),
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build recursive fixture with zig %s: %v\n%s", strings.Join(cmdArgs, " "), err, output)
	}
}
