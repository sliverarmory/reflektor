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

const asyncBOFWait = 45 * time.Second

type asyncBOFRun struct {
	marker   byte
	gate     uint32
	object   *bof.Object
	done     chan error
	started  bool
	finished bool
}

type asyncBOFEvent struct {
	marker byte
	output bof.Output
}

// TestConcurrentBOFStreaming proves that separate native BOFs actually run at
// the same time. Both must emit two records before either host gate opens; a
// process-wide execution lock would prevent the second BOF from reaching its
// gate and make this test time out.
func TestConcurrentBOFStreaming(t *testing.T) {
	target, ok := nativeBOFTarget()
	if !ok {
		t.Fatalf("missing BOF fixture target for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	path := buildBOFSource(t, t.TempDir(), target, "async_fixture", "async_fixture.c")
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	runs := []*asyncBOFRun{
		{marker: 'A', done: make(chan error, 1)},
		{marker: 'B', done: make(chan error, 1)},
	}
	defer func() {
		// Release every native worker even if an assertion fails while it is
		// waiting. Close only after ExecuteWithOutput has returned, so a failed
		// test cannot deadlock while trying to unmap a running image.
		for _, run := range runs {
			atomic.StoreUint32(&run.gate, 1)
		}
		for _, run := range runs {
			if run.started && !run.finished {
				select {
				case executeErr := <-run.done:
					run.finished = true
					if executeErr != nil {
						t.Errorf("BOF %c cleanup execution: %v", run.marker, executeErr)
					}
				case <-time.After(asyncBOFWait):
					t.Errorf("BOF %c did not exit after its gate opened", run.marker)
				}
			}
			if run.object != nil && (!run.started || run.finished) {
				if closeErr := run.object.Close(); closeErr != nil {
					t.Errorf("BOF %c Close(): %v", run.marker, closeErr)
				}
			}
		}
	}()

	for _, run := range runs {
		run := run
		resolvedGate := false
		loaded, loadErr := bof.LoadWithOptions(image, bof.LoadOptions{
			ResolveSymbol: func(imported bof.Import) (uintptr, bool, error) {
				if strings.Contains(imported.Name, "HostRelease") {
					resolvedGate = true
					return uintptr(unsafe.Pointer(&run.gate)), true, nil
				}
				return 0, false, nil
			},
		})
		if loadErr != nil {
			t.Fatalf("load BOF %c: %v", run.marker, loadErr)
		}
		run.object = loaded
		if !resolvedGate {
			t.Fatalf("BOF %c did not import HostRelease", run.marker)
		}
	}

	events := make(chan asyncBOFEvent, 6)
	for _, run := range runs {
		run := run
		run.started = true
		go func() {
			run.done <- run.object.ExecuteWithOutput([]byte{run.marker}, func(output bof.Output) {
				events <- asyncBOFEvent{marker: run.marker, output: output}
			})
		}()
	}

	phase := map[byte]int{'A': 0, 'B': 0}
	starts := make(map[byte]bof.Output, len(runs))
	ready := 0
	startTimeout := time.NewTimer(asyncBOFWait)
	defer startTimeout.Stop()
	for ready != len(runs) {
		select {
		case event := <-events:
			switch phase[event.marker] {
			case 0:
				starts[event.marker] = event.output
				phase[event.marker] = 1
			case 1:
				if event.output.Type != bof.OutputDefault || len(event.output.Data) != 0 {
					t.Fatalf("BOF %c ready output = %#v, want empty default record", event.marker, event.output)
				}
				phase[event.marker] = 2
				ready++
			default:
				t.Fatalf("BOF %c emitted an unexpected record before release: %#v", event.marker, event.output)
			}
		case executeErr := <-runs[0].done:
			runs[0].finished = true
			t.Fatalf("BOF A exited before both gates opened: %v", executeErr)
		case executeErr := <-runs[1].done:
			runs[1].finished = true
			t.Fatalf("BOF B exited before both gates opened: %v", executeErr)
		case <-startTimeout.C:
			t.Fatalf("both BOFs did not emit before release; phases: A=%d B=%d", phase['A'], phase['B'])
		}
	}

	for _, run := range runs {
		assertAsyncBOFStart(t, run.marker, starts[run.marker])
		select {
		case executeErr := <-run.done:
			run.finished = true
			t.Fatalf("BOF %c returned before its gate opened: %v", run.marker, executeErr)
		default:
		}
	}

	atomic.StoreUint32(&runs[0].gate, 1)
	waitForAsyncBOFFinish(t, runs[0], runs[1], events)
	select {
	case executeErr := <-runs[1].done:
		runs[1].finished = true
		t.Fatalf("BOF B returned while its gate remained closed: %v", executeErr)
	default:
	}

	atomic.StoreUint32(&runs[1].gate, 1)
	waitForAsyncBOFFinish(t, runs[1], nil, events)
	select {
	case event := <-events:
		t.Fatalf("unexpected extra BOF output: %#v", event)
	default:
	}

	for _, run := range runs {
		if closeErr := run.object.Close(); closeErr != nil {
			t.Fatalf("BOF %c Close(): %v", run.marker, closeErr)
		}
		run.object = nil
		// Streamed records remain owned and valid after the source stack buffer
		// is overwritten, native execution ends, and the image is unmapped.
		assertAsyncBOFStart(t, run.marker, starts[run.marker])
	}
	runtime.KeepAlive(runs)
}

// TestSameImageBOFExecutionIsSerialized checks that a second invocation of
// one mutable image does not enter native code while the first is gated.
func TestSameImageBOFExecutionIsSerialized(t *testing.T) {
	target, ok := nativeBOFTarget()
	if !ok {
		t.Fatalf("missing BOF fixture target for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	path := buildBOFSource(t, t.TempDir(), target, "async_fixture", "async_fixture.c")
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var gate uint32
	resolvedGate := false
	object, err := bof.LoadWithOptions(image, bof.LoadOptions{
		ResolveSymbol: func(imported bof.Import) (uintptr, bool, error) {
			if strings.Contains(imported.Name, "HostRelease") {
				resolvedGate = true
				return uintptr(unsafe.Pointer(&gate)), true, nil
			}
			return 0, false, nil
		},
	})
	if err != nil {
		t.Fatalf("load shared BOF image: %v", err)
	}
	if !resolvedGate {
		object.Close()
		t.Fatal("BOF did not import HostRelease")
	}

	type invocation struct {
		marker   byte
		done     chan error
		started  bool
		finished bool
	}
	runs := [2]invocation{
		{marker: 'A', done: make(chan error, 1)},
		{marker: 'B', done: make(chan error, 1)},
	}
	events := make(chan asyncBOFEvent, 6)
	defer func() {
		atomic.StoreUint32(&gate, 1)
		allFinished := true
		for index := range runs {
			run := &runs[index]
			if run.started && !run.finished {
				select {
				case executeErr := <-run.done:
					run.finished = true
					if executeErr != nil {
						t.Errorf("BOF %c cleanup execution: %v", run.marker, executeErr)
					}
				case <-time.After(asyncBOFWait):
					t.Errorf("BOF %c did not exit after its gate opened", run.marker)
				}
			}
			allFinished = allFinished && (!run.started || run.finished)
		}
		// Never unmap an image while either invocation may still be running.
		if allFinished {
			if closeErr := object.Close(); closeErr != nil {
				t.Errorf("shared BOF Close(): %v", closeErr)
			}
		}
		runtime.KeepAlive(&gate)
	}()

	start := func(index int, entered chan<- struct{}) {
		run := &runs[index]
		run.started = true
		go func() {
			if entered != nil {
				close(entered)
			}
			run.done <- object.ExecuteWithOutput([]byte{run.marker}, func(output bof.Output) {
				events <- asyncBOFEvent{marker: run.marker, output: output}
			})
		}()
	}

	start(0, nil)
	gotEvents := make([]asyncBOFEvent, 0, 6)
	firstTimer := time.NewTimer(asyncBOFWait)
	defer firstTimer.Stop()
	for len(gotEvents) < 2 {
		select {
		case event := <-events:
			if event.marker != 'A' {
				t.Fatalf("unexpected output before BOF B starts: %#v", event)
			}
			gotEvents = append(gotEvents, event)
		case executeErr := <-runs[0].done:
			runs[0].finished = true
			t.Fatalf("BOF A exited before its gate opened: %v", executeErr)
		case <-firstTimer.C:
			t.Fatalf("BOF A did not emit both pre-release records; got %d", len(gotEvents))
		}
	}
	assertAsyncBOFStart(t, 'A', gotEvents[0].output)
	if gotEvents[1].output.Type != bof.OutputDefault || len(gotEvents[1].output.Data) != 0 {
		t.Fatalf("BOF A ready output = %#v, want empty default record", gotEvents[1].output)
	}

	entered := make(chan struct{})
	start(1, entered)
	<-entered
	runtime.Gosched()
	// The handshake ensures B reached the call site. Give every native runner,
	// including QEMU guests, time to reveal an accidental concurrent entry.
	quietTimer := time.NewTimer(time.Second)
	defer quietTimer.Stop()
	select {
	case event := <-events:
		t.Fatalf("BOF B entered the same image while A was gated: %#v", event)
	case executeErr := <-runs[0].done:
		runs[0].finished = true
		t.Fatalf("BOF A exited before its gate opened: %v", executeErr)
	case executeErr := <-runs[1].done:
		runs[1].finished = true
		t.Fatalf("BOF B returned while A was gated: %v", executeErr)
	case <-quietTimer.C:
	}

	atomic.StoreUint32(&gate, 1)
	remaining := time.NewTimer(asyncBOFWait)
	defer remaining.Stop()
	aDone := (<-chan error)(runs[0].done)
	bDone := (<-chan error)(runs[1].done)
	for len(gotEvents) < 6 || aDone != nil || bDone != nil {
		select {
		case event := <-events:
			gotEvents = append(gotEvents, event)
			if len(gotEvents) > 6 {
				t.Fatalf("unexpected extra BOF output: %#v", event)
			}
		case executeErr := <-aDone:
			runs[0].finished = true
			aDone = nil
			if executeErr != nil {
				t.Fatalf("BOF A ExecuteWithOutput(): %v", executeErr)
			}
		case executeErr := <-bDone:
			runs[1].finished = true
			bDone = nil
			if executeErr != nil {
				t.Fatalf("BOF B ExecuteWithOutput(): %v", executeErr)
			}
		case <-remaining.C:
			t.Fatalf("same-image BOFs did not finish; outputs=%d A=%v B=%v", len(gotEvents), runs[0].finished, runs[1].finished)
		}
	}

	for index, marker := range []byte{'A', 'B'} {
		start := gotEvents[index*3]
		ready := gotEvents[index*3+1]
		finish := gotEvents[index*3+2]
		if start.marker != marker || ready.marker != marker || finish.marker != marker {
			t.Fatalf("same-image output order = %#v, want all A records before B", gotEvents)
		}
		assertAsyncBOFStart(t, marker, start.output)
		if ready.output.Type != bof.OutputDefault || len(ready.output.Data) != 0 {
			t.Fatalf("BOF %c ready output = %#v, want empty default record", marker, ready.output)
		}
		wantFinish := []byte{'F', marker, 0}
		if finish.output.Type != bof.OutputUTF8 || !bytes.Equal(finish.output.Data, wantFinish) {
			t.Fatalf("BOF %c final output = %#v, want type=%d data=%x", marker, finish.output, bof.OutputUTF8, wantFinish)
		}
	}
	select {
	case event := <-events:
		t.Fatalf("unexpected extra BOF output: %#v", event)
	default:
	}
}

func assertAsyncBOFStart(t *testing.T, marker byte, output bof.Output) {
	t.Helper()
	want := []byte{'S', marker, 0xff}
	if output.Type != bof.OutputOEM || !bytes.Equal(output.Data, want) {
		t.Fatalf("BOF %c start output = %#v, want type=%d data=%x", marker, output, bof.OutputOEM, want)
	}
}

func waitForAsyncBOFFinish(t *testing.T, run, other *asyncBOFRun, events <-chan asyncBOFEvent) {
	t.Helper()
	timer := time.NewTimer(asyncBOFWait)
	defer timer.Stop()
	gotOutput := false
	gotDone := false
	for !gotOutput || !gotDone {
		select {
		case event := <-events:
			if event.marker != run.marker || gotOutput {
				t.Fatalf("unexpected BOF output while waiting for %c: %#v", run.marker, event)
			}
			want := []byte{'F', run.marker, 0}
			if event.output.Type != bof.OutputUTF8 || !bytes.Equal(event.output.Data, want) {
				t.Fatalf("BOF %c final output = %#v, want type=%d data=%x", run.marker, event.output, bof.OutputUTF8, want)
			}
			gotOutput = true
		case executeErr := <-run.done:
			run.finished = true
			if executeErr != nil {
				t.Fatalf("BOF %c ExecuteWithOutput(): %v", run.marker, executeErr)
			}
			gotDone = true
		case <-timer.C:
			t.Fatalf("BOF %c did not finish after its gate opened; output=%v done=%v", run.marker, gotOutput, gotDone)
		}
		if other != nil {
			select {
			case executeErr := <-other.done:
				other.finished = true
				t.Fatalf("BOF %c returned before its gate opened: %v", other.marker, executeErr)
			default:
			}
		}
	}
}
