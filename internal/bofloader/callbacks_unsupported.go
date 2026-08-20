//go:build bof && ((darwin && !(amd64 || arm64)) || (linux && !(386 || amd64 || arm64)) || (windows && !(386 || amd64 || arm64)) || (!darwin && !linux && !windows))

package bofloader

func platformCallbacks() map[string]uintptr {
	return nil
}
