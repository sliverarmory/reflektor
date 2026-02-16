package main

/*
#include <stdint.h>
*/
import "C"

import (
	"os"
	"runtime"
)

func markerPath() string {
	if env := os.Getenv("REFLEKTOR_MARKER"); env != "" {
		return env
	}
	if runtime.GOOS == "windows" {
		return `C:\Windows\Temp\reflektor_marker.txt`
	}
	return "/tmp/reflektor_marker.txt"
}

//export StartW
func StartW() {
	_ = os.WriteFile(markerPath(), []byte("ok"), 0o600)
}

//export StartWStatus
func StartWStatus() C.int {
	StartW()
	return 1337
}

func main() {}
