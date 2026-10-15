package reflektor_test

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/sliverarmory/reflektor/bof"
)

// TestManagedBOFWorkers uses a native BOF function on a host-managed OS thread.
// It emits after the entry function has returned and proves that Execute and
// Close retain the image until that function and its callback have completed.
func TestManagedBOFWorkers(t *testing.T) {
	target, ok := nativeBOFTarget()
	if !ok {
		t.Fatalf("missing BOF fixture target for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	path := buildBOFSource(t, t.TempDir(), target, "managed_worker_fixture", "managed_worker_fixture.c")
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var release uint32
	resolvedGate := false
	object, err := bof.LoadWithOptions(image, bof.LoadOptions{
		ResolveSymbol: func(imported bof.Import) (uintptr, bool, error) {
			if strings.Contains(imported.Name, "HostRelease") {
				resolvedGate = true
				return uintptr(unsafe.Pointer(&release)), true, nil
			}
			return 0, false, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resolvedGate {
		t.Fatal("fixture did not import HostRelease")
	}
	defer func() { _ = object.Close() }()
	defer atomic.StoreUint32(&release, 1)

	var disabled []bof.Output
	err = object.ExecuteWithOptions(nil, bof.ExecuteOptions{OnOutput: func(output bof.Output) {
		disabled = append(disabled, output)
	}})
	if err == nil || !strings.Contains(err.Error(), "managed workers are disabled") {
		t.Fatalf("default execution error = %v, want disabled-worker error", err)
	}
	if len(disabled) != 1 || !bytes.Equal(disabled[0].Data, []byte("worker-disabled")) {
		t.Fatalf("default execution output = %#v", disabled)
	}

	outputs := make(chan bof.Output, 8)
	executeDone := make(chan error, 1)
	go func() {
		executeDone <- object.ExecuteWithOptions(nil, bof.ExecuteOptions{
			ManagedWorkers: true,
			OnOutput:       func(output bof.Output) { outputs <- output },
		})
	}()

	seen := map[string]int{}
	for len(seen) < 2 {
		select {
		case output := <-outputs:
			seen[string(output.Data)]++
		case err := <-executeDone:
			t.Fatalf("Execute returned before managed worker was released: %v", err)
		case <-time.After(asyncBOFWait):
			t.Fatal("managed worker did not emit ready and entry records")
		}
	}
	if seen["worker-ready"] != 1 || seen["entry-returned"] != 1 {
		t.Fatalf("initial output records = %#v", seen)
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- object.Close() }()
	select {
	case err := <-executeDone:
		t.Fatalf("Execute returned while worker was running: %v", err)
	case err := <-closeDone:
		t.Fatalf("Close unmapped an active worker: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	atomic.StoreUint32(&release, 1)
	select {
	case err := <-executeDone:
		if err != nil {
			t.Fatalf("managed execution: %v", err)
		}
	case <-time.After(asyncBOFWait):
		t.Fatal("Execute did not join the managed worker")
	}
	for len(outputs) != 0 {
		output := <-outputs
		seen[string(output.Data)]++
	}
	if seen["A"] != 1 || seen["B"] != 1 || seen["worker-ready"] != 2 || seen["worker-done"] != 2 || len(seen) != 5 {
		t.Fatalf("joined output records = %#v; want copied A and B from both workers", seen)
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close after worker join: %v", err)
		}
	case <-time.After(asyncBOFWait):
		t.Fatal("Close did not return after worker join")
	}
}
