//go:build netbsd && amd64

package reflektor_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func assertRecursiveDependenciesNotNativeLoaded(t *testing.T, graphDir string) {
	t.Helper()
	absDir, err := filepath.Abs(graphDir)
	if err != nil {
		t.Fatalf("resolve recursive graph directory: %v", err)
	}
	if evaluated, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = evaluated
	}
	output, err := exec.Command("pmap", "-v", "-p", strconv.Itoa(os.Getpid())).CombinedOutput()
	if err != nil {
		t.Fatalf("pmap current process: %v\n%s", err, output)
	}
	if bytes.Contains(output, []byte(absDir)) {
		t.Fatalf("recursive dependency graph appears as an OS file mapping: %s\n%s", absDir, output)
	}
}
