package reflektor_test

import (
	"bytes"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/sliverarmory/reflektor/bof"
)

// TestCooperativeBOFStop verifies that the native entry observes a stop
// request, delivers its final output, and exits before Close releases it.
func TestCooperativeBOFStop(t *testing.T) {
	target, ok := nativeBOFTarget()
	if !ok {
		t.Fatalf("missing BOF fixture target for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	path := buildBOFSource(t, t.TempDir(), target, "stop_fixture", "stop_fixture.c")
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	object, err := bof.Load(image)
	if err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	stopClosed := false
	outputs := make(chan bof.Output, 2)
	executionDone := make(chan error, 1)
	closeDone := make(chan error, 1)
	started := false
	finished := false
	closing := false
	closed := false
	defer func() {
		if !stopClosed {
			close(stop)
		}
		if started && !finished {
			select {
			case <-executionDone:
			case <-time.After(asyncBOFWait):
				t.Error("BOF did not exit after stop request")
			}
		}
		if closing && !closed {
			select {
			case <-closeDone:
			case <-time.After(asyncBOFWait):
				t.Error("Close did not return after BOF exit")
			}
		} else if !closing {
			if err := object.Close(); err != nil {
				t.Errorf("BOF cleanup Close(): %v", err)
			}
		}
	}()

	started = true
	go func() {
		executionDone <- object.ExecuteWithOptions(nil, bof.ExecuteOptions{
			OnOutput: func(output bof.Output) { outputs <- output },
			Stop:     stop,
		})
	}()

	select {
	case output := <-outputs:
		if output.Type != bof.OutputDefault || !bytes.Equal(output.Data, []byte("ready")) {
			t.Fatalf("first output = %#v, want ready", output)
		}
	case err := <-executionDone:
		finished = true
		t.Fatalf("BOF exited before stop request: %v", err)
	case <-time.After(asyncBOFWait):
		t.Fatal("BOF did not emit its ready output")
	}

	closing = true
	closeStarted := make(chan struct{})
	go func() {
		close(closeStarted)
		closeDone <- object.Close()
	}()
	<-closeStarted
	select {
	case err := <-executionDone:
		finished = true
		t.Fatalf("BOF exited before stop request: %v", err)
	case err := <-closeDone:
		closed = true
		t.Fatalf("Close returned while native entry was active: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(stop)
	stopClosed = true
	var finalOutput bof.Output
	select {
	case finalOutput = <-outputs:
	case err := <-executionDone:
		finished = true
		if err != nil {
			t.Fatalf("ExecuteWithOptions(): %v", err)
		}
		// Both channels may be ready when select runs. The output callback
		// must have completed before ExecuteWithOptions can return, so the
		// final record must already be buffered even if completion wins.
		select {
		case finalOutput = <-outputs:
		default:
			t.Fatal("BOF returned without delivering its final output")
		}
	case <-time.After(asyncBOFWait):
		t.Fatal("BOF did not emit its stopped output")
	}
	if finalOutput.Type != bof.OutputDefault || !bytes.Equal(finalOutput.Data, []byte("stopped")) {
		t.Fatalf("final output = %#v, want stopped", finalOutput)
	}
	if !finished {
		select {
		case err := <-executionDone:
			finished = true
			if err != nil {
				t.Fatalf("ExecuteWithOptions(): %v", err)
			}
		case <-time.After(asyncBOFWait):
			t.Fatal("BOF did not return after stop request")
		}
	}
	select {
	case err := <-closeDone:
		closed = true
		if err != nil {
			t.Fatalf("Close(): %v", err)
		}
	case <-time.After(asyncBOFWait):
		t.Fatal("Close did not return after BOF exit")
	}
}
