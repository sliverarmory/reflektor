//go:build darwin && arm64

package reflektor_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sliverarmory/reflektor/bof"
)

func TestDarwinARM64NativeVariadicBOF(t *testing.T) {
	requireCommand(t, "clang")
	path := filepath.Join(t.TempDir(), "darwin_variadic.o")
	command := exec.Command("clang", "-arch", "arm64", "-mmacosx-version-min=11.0", "-c", "-O1", "-g0",
		"-fno-stack-protector", "-fno-asynchronous-unwind-tables", "-fno-unwind-tables",
		"-o", path, filepath.Join("..", "testdata", "bof", "darwin_variadic.c"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build native Apple arm64 variadic BOF: %v\n%s", err, output)
	}
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := bof.Load(image)
	if err != nil {
		t.Fatalf("load native Apple arm64 variadic BOF: %v", err)
	}
	defer loaded.Close()
	want := []string{
		"stack=7:ok:1122334455667788:4:5:6:7:8:9:10",
		"indirect=-3:ok",
		"no-args",
		"float=1.5",
		"format=1:ok:3:4:5:6:7:8:9:10:tail",
	}
	for run := range 2 {
		outputs, err := loaded.Execute(nil)
		if err != nil {
			t.Fatalf("execute native Apple arm64 variadic BOF run %d: %v", run, err)
		}
		if len(outputs) != len(want) {
			t.Fatalf("run %d outputs = %#v, want %q", run, outputs, want)
		}
		for i := range want {
			if outputs[i].Type != bof.OutputDefault || string(outputs[i].Data) != want[i] {
				t.Fatalf("run %d output %d = %#v, want %q", run, i, outputs[i], want[i])
			}
		}
	}
}
