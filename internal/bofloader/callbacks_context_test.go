package bofloader

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

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
