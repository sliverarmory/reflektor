package main

/*
#include <stdint.h>
*/
import "C"

import (
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func markerPath() string {
	if path := os.Getenv("REFLEKTOR_MARKER"); path != "" {
		return path
	}
	if runtime.GOOS == "windows" {
		return `C:\Windows\Temp\reflektor_marker.txt`
	}
	return "/tmp/reflektor_marker.txt"
}

func runRuntimeWork() {
	readyPath := os.Getenv("REFLEKTOR_READY")
	releasePath := os.Getenv("REFLEKTOR_RELEASE")
	done := make(chan struct{})

	go func() {
		defer close(done)
		// Exercise guest scheduler and timer state before announcing readiness.
		// This catches runtime initialization and host/guest TLS collisions that a
		// C-only export or an immediately returning Go wrapper would miss.
		<-time.NewTimer(100 * time.Millisecond).C
		if readyPath != "" {
			_ = os.MkdirAll(filepath.Dir(readyPath), 0o700)
			_ = os.WriteFile(readyPath, []byte("ready"), 0o600)
		}
		for releasePath != "" {
			if _, err := os.Stat(releasePath); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		_ = os.WriteFile(markerPath(), []byte("ok"), 0o600)
	}()

	<-done

	closedPath := os.Getenv("REFLEKTOR_CLOSED")
	afterClosePath := os.Getenv("REFLEKTOR_AFTER_CLOSE")
	if closedPath != "" && afterClosePath != "" {
		started := make(chan struct{})
		go func() {
			close(started)
			for {
				if _, err := os.Stat(closedPath); err == nil {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			_ = os.WriteFile(afterClosePath, []byte("alive"), 0o600)
		}()
		<-started
	}
}

//export StartW
func StartW() {
	runRuntimeWork()
}

//export StartWStatus
func StartWStatus() C.int {
	runRuntimeWork()
	return 1337
}

func main() {}
