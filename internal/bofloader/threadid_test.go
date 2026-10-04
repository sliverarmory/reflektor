//go:build (darwin && !ios && (amd64 || arm64)) || (freebsd && (amd64 || arm64)) || (linux && !android && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64)) || (windows && (386 || amd64 || arm64))

package bofloader

import (
	"runtime"
	"sync"
	"testing"
)

func TestCurrentExecutionThreadID(t *testing.T) {
	type result struct {
		first  uint64
		second uint64
		err    error
	}
	results := make(chan result, 2)
	release := make(chan struct{})
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()

			first, err := currentExecutionThreadID()
			if err != nil {
				results <- result{err: err}
			} else {
				second, err := currentExecutionThreadID()
				results <- result{first: first, second: second, err: err}
			}
			<-release
		}()
	}
	first, second := <-results, <-results
	close(release)
	workers.Wait()
	for _, got := range []result{first, second} {
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.first == 0 || got.first != got.second {
			t.Fatalf("thread ID changed while pinned: %d then %d", got.first, got.second)
		}
	}
	if first.first == second.first {
		t.Fatalf("concurrent pinned workers share thread ID %d", first.first)
	}
}
