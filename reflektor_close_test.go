package reflektor

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type closeTestModule struct {
	call  func()
	free  func()
	frees atomic.Int32
}

func (module *closeTestModule) CallExport(string) error {
	if module.call != nil {
		module.call()
	}
	return nil
}

func (module *closeTestModule) CallExportWithArgs(string, ...uintptr) (uintptr, error) {
	if module.call != nil {
		module.call()
	}
	return 0, nil
}

func (module *closeTestModule) Free() {
	module.frees.Add(1)
	if module.free != nil {
		module.free()
	}
}

func awaitCloseTest(t *testing.T, done <-chan error, operation string) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s timed out", operation)
	}
}

func TestLibraryCloseAllowsDestructorReentry(t *testing.T) {
	module := &closeTestModule{}
	library := &Library{module: module}
	module.free = func() {
		if err := library.Close(); err != nil {
			t.Errorf("reentrant Close: %v", err)
		}
		if err := library.CallExport("after-close"); !errors.Is(err, ErrLibraryClosed) {
			t.Errorf("reentrant CallExport error = %v, want ErrLibraryClosed", err)
		}
		if _, err := library.CallExportWithArgs("after-close"); !errors.Is(err, ErrLibraryClosed) {
			t.Errorf("reentrant CallExportWithArgs error = %v, want ErrLibraryClosed", err)
		}
	}

	done := make(chan error, 1)
	go func() { done <- library.Close() }()
	awaitCloseTest(t, done, "Close during destructor reentry")
	if err := library.Close(); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
	if got := module.frees.Load(); got != 1 {
		t.Fatalf("Free calls = %d, want 1", got)
	}
}

func TestLibraryCloseWaitsForCallAndAllowsConcurrentCloseDuringUnload(t *testing.T) {
	callStarted := make(chan struct{})
	allowCallReturn := make(chan struct{})
	freeStarted := make(chan struct{})
	allowFreeReturn := make(chan struct{})
	module := &closeTestModule{
		call: func() {
			close(callStarted)
			<-allowCallReturn
		},
		free: func() {
			close(freeStarted)
			<-allowFreeReturn
		},
	}
	library := &Library{module: module}
	callDone := make(chan error, 1)
	go func() { callDone <- library.CallExport("running") }()
	<-callStarted

	closeDone := make(chan error, 1)
	go func() { closeDone <- library.Close() }()
	select {
	case <-freeStarted:
		t.Fatal("unload began while an export was still running")
	default:
	}
	close(allowCallReturn)
	awaitCloseTest(t, callDone, "in-flight CallExport")
	select {
	case <-freeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("unload did not begin after the export returned")
	}

	secondCloseDone := make(chan error, 1)
	go func() { secondCloseDone <- library.Close() }()
	awaitCloseTest(t, secondCloseDone, "concurrent Close during unload")
	if err := library.CallExport("after-close"); !errors.Is(err, ErrLibraryClosed) {
		t.Errorf("CallExport during unload error = %v, want ErrLibraryClosed", err)
	}
	close(allowFreeReturn)
	awaitCloseTest(t, closeDone, "first Close")
	if got := module.frees.Load(); got != 1 {
		t.Fatalf("Free calls = %d, want 1", got)
	}
}
