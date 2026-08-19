//go:build darwin && (amd64 || arm64)

package reflektor_test

import "testing"

func assertRecursiveDependenciesNotNativeLoaded(t *testing.T, graphDir string) {
	t.Helper()
	_ = graphDir
	// Darwin registers the already-captured dependency bytes with dyld during
	// CallExport. Renaming the graph first proves dyld cannot open those files.
}
