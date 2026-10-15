package reflektor_test

import (
	"path/filepath"
	"testing"
)

type windowsRecursiveGraph struct {
	root    string
	modules []string
	export  string
	want    uintptr
	imports map[string][]string
}

func buildWindowsCycleGraph(t *testing.T, outDir string, goarch string) windowsRecursiveGraph {
	t.Helper()
	target, ok := zigTargetFor("windows", goarch)
	if !ok {
		t.Fatalf("unsupported Windows cycle target %s", goarch)
	}
	common := []string{"cc", "-target", target, "-O2", "-g0"}
	source := filepath.Join("..", "testdata", "c", "windows_recursive_graph.c")
	a := filepath.Join(outDir, "reflektor_cycle_a.dll")
	b := filepath.Join(outDir, "reflektor_cycle_b.dll")
	aImport := filepath.Join(outDir, "reflektor_cycle_a.lib")
	bImport := filepath.Join(outDir, "reflektor_cycle_b.lib")

	// The bootstrap image supplies A's import library while B is linked.
	// Replace it with the real A after B's import library exists.
	buildRecursiveFixture(t, common, []string{
		"-shared", "-DREFLEKTOR_CYCLE_A", "-DREFLEKTOR_GRAPH_BOOTSTRAP",
		"-Wl,--out-implib," + aImport, "-o", a, source,
	})
	buildRecursiveFixture(t, common, []string{
		"-shared", "-DREFLEKTOR_CYCLE_B",
		"-Wl,--out-implib," + bImport, "-o", b, source, aImport,
	})
	buildRecursiveFixture(t, common, []string{
		"-shared", "-DREFLEKTOR_CYCLE_A",
		"-Wl,--out-implib," + aImport, "-o", a, source, bImport,
	})
	return windowsRecursiveGraph{
		root: a, modules: []string{a, b}, export: "CycleRoundTrip", want: 43,
		imports: map[string][]string{
			a: {filepath.Base(b)},
			b: {filepath.Base(a)},
		},
	}
}

func buildWindowsDiamondGraph(t *testing.T, outDir string, goarch string) windowsRecursiveGraph {
	t.Helper()
	target, ok := zigTargetFor("windows", goarch)
	if !ok {
		t.Fatalf("unsupported Windows diamond target %s", goarch)
	}
	common := []string{"cc", "-target", target, "-O2", "-g0"}
	source := filepath.Join("..", "testdata", "c", "windows_recursive_graph.c")
	leaf := filepath.Join(outDir, "reflektor_diamond_leaf.dll")
	left := filepath.Join(outDir, "reflektor_diamond_left.dll")
	right := filepath.Join(outDir, "reflektor_diamond_right.dll")
	root := filepath.Join(outDir, "reflektor_diamond_root.dll")
	leafImport := filepath.Join(outDir, "reflektor_diamond_leaf.lib")
	leftImport := filepath.Join(outDir, "reflektor_diamond_left.lib")
	rightImport := filepath.Join(outDir, "reflektor_diamond_right.lib")
	buildRecursiveFixture(t, common, []string{
		"-shared", "-DREFLEKTOR_DIAMOND_LEAF",
		"-Wl,--out-implib," + leafImport, "-o", leaf, source,
	})
	buildRecursiveFixture(t, common, []string{
		"-shared", "-DREFLEKTOR_DIAMOND_LEFT",
		"-Wl,--out-implib," + leftImport, "-o", left, source, leafImport,
	})
	buildRecursiveFixture(t, common, []string{
		"-shared", "-DREFLEKTOR_DIAMOND_RIGHT",
		"-Wl,--out-implib," + rightImport, "-o", right, source, leafImport,
	})
	buildRecursiveFixture(t, common, []string{
		"-shared", "-DREFLEKTOR_DIAMOND_ROOT",
		"-o", root, source, leftImport, rightImport,
	})
	return windowsRecursiveGraph{
		root: root, modules: []string{root, left, right, leaf}, export: "DiamondResult", want: 42,
		imports: map[string][]string{
			root:  {filepath.Base(left), filepath.Base(right)},
			left:  {filepath.Base(leaf)},
			right: {filepath.Base(leaf)},
		},
	}
}
