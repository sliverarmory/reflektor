//go:build bof

package reflektor

import (
	"os"
	"testing"
)

func TestBOFSupportEnabledWithBuildTag(t *testing.T) {
	if !BOFEnabled {
		t.Fatal("BOFEnabled = false with bof build tag")
	}
}

func TestLoadBOFFileRejectsOversizedRegularFile(t *testing.T) {
	path := t.TempDir() + "/oversized.o"
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxBOFObjectSize + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBOFFile(path); err == nil {
		t.Fatal("LoadBOFFile accepted an oversized file")
	}
}
