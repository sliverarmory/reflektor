package bofloader

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestConcurrentOutputDeliveryMatchesCaptureOrder(t *testing.T) {
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	delivered := make(chan string, 2)
	context := newExecutionContext(func(output Output) {
		if string(output.Data) == "first" {
			close(firstEntered)
			<-releaseFirst
		}
		delivered <- string(output.Data)
	}, true)
	firstDone := make(chan struct{})
	go func() {
		context.appendOutput(0, []byte("first"))
		close(firstDone)
	}()
	<-firstEntered
	secondDone := make(chan struct{})
	go func() {
		context.appendOutput(0, []byte("second"))
		close(secondDone)
	}()
	select {
	case <-secondDone:
		t.Fatal("second callback overtook the first output delivery")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirst)
	<-firstDone
	<-secondDone
	outputs, err := context.result()
	if err != nil || len(outputs) != 2 || string(outputs[0].Data) != "first" || string(outputs[1].Data) != "second" {
		t.Fatalf("captured output = %#v, %v", outputs, err)
	}
	if first, second := <-delivered, <-delivered; first != "first" || second != "second" {
		t.Fatalf("delivery order = %q, %q", first, second)
	}
}

func TestManagedWorkerCallbackAfterContextRemovalIsIgnored(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	context := newExecutionContext(nil, true)
	context.workersEnabled = true
	context.workerRanges = []executableRange{{address: 1, size: 1}}
	unregister, err := registerExecutionContext(context)
	if err != nil {
		t.Fatal(err)
	}
	unregister()
	if got := reflektorStartWorker(1, 0, 0); got != 0 {
		t.Fatalf("late ReflektorStartWorker = %d, want 0", got)
	}
	if got := beaconOutput(0, 0, 0); got != 0 {
		t.Fatalf("late BeaconOutput = %d, want 0", got)
	}
	outputs, err := context.result()
	if err != nil || len(outputs) != 0 || context.workers != 0 {
		t.Fatalf("retired context = %#v, %v, workers %d", outputs, err, context.workers)
	}
}

func TestManagedWorkerRejectsAddressOutsideExecutableSection(t *testing.T) {
	context := newExecutionContext(nil, false)
	context.workersEnabled = true
	context.workerRanges = []executableRange{{address: 0x1000, size: 0x100}}
	for _, address := range []uintptr{0, 0xfff, 0x1100} {
		if got := context.startWorker(address, 0, 0); got != 0 {
			t.Fatalf("startWorker(%#x) = %d, want 0", address, got)
		}
	}
	if context.workers != 0 || context.workerBytes != 0 {
		t.Fatalf("invalid starts reserved %d workers and %d bytes", context.workers, context.workerBytes)
	}
	_, err := context.result()
	if err == nil || !strings.Contains(err.Error(), "not in a mapped executable BOF section") {
		t.Fatalf("invalid worker addresses error = %v", err)
	}
}

func TestStreamingExecutionContextDoesNotRetainOutput(t *testing.T) {
	var received []Output
	context := newExecutionContext(func(output Output) {
		received = append(received, output)
	}, false)
	data := []byte{0x41, 0x00, 0xff}
	context.appendOutput(-7, data)
	context.appendOutput(0, nil)
	data[0] = 0

	outputs, err := context.result()
	if err != nil || len(outputs) != 0 {
		t.Fatalf("streaming result = %#v, %v; want no retained outputs", outputs, err)
	}
	if len(received) != 2 || received[0].Type != -7 || !bytes.Equal(received[0].Data, []byte{0x41, 0x00, 0xff}) || received[1].Type != 0 || len(received[1].Data) != 0 {
		t.Fatalf("delivered outputs = %#v", received)
	}
}

func TestStreamingOutputCallbackPanicBecomesExecutionError(t *testing.T) {
	calls := 0
	context := newExecutionContext(func(Output) {
		calls++
		panic("sink failed")
	}, false)
	context.appendOutput(0, []byte("first"))
	context.appendOutput(0, []byte("second"))
	outputs, err := context.result()
	if calls != 1 || len(outputs) != 0 || err == nil || !strings.Contains(err.Error(), "sink failed") {
		t.Fatalf("result = %#v, %v; callback calls = %d", outputs, err, calls)
	}
}

func TestReflektorShouldStopUsesCurrentExecutionContext(t *testing.T) {
	if got := reflektorShouldStop(); got != 0 {
		t.Fatalf("poll without an execution = %d, want 0", got)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	stop := make(chan struct{})
	context := newExecutionContext(nil, false)
	context.stop = stop
	unregister, err := registerExecutionContext(context)
	if err != nil {
		t.Fatal(err)
	}
	defer unregister()
	if got := reflektorShouldStop(); got != 0 {
		t.Fatalf("poll before stop = %d, want 0", got)
	}
	close(stop)
	if got := reflektorShouldStop(); got != 1 {
		t.Fatalf("poll after stop = %d, want 1", got)
	}
}

func TestStopRequestedLatchesOneShotSignal(t *testing.T) {
	stop := make(chan struct{}, 1)
	context := newExecutionContext(nil, false)
	context.stop = stop
	if context.stopRequested() {
		t.Fatal("stop requested before any signal")
	}
	stop <- struct{}{}
	if !context.stopRequested() || !context.stopRequested() {
		t.Fatal("stop request was not retained after one-shot signal")
	}
}
