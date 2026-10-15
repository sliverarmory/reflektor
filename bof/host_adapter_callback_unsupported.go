//go:build android || ios || (darwin && !ios && !(amd64 || arm64)) || (freebsd && !(amd64 || arm64)) || (linux && !android && !(386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64)) || (windows && !(386 || amd64 || arm64)) || (!darwin && !freebsd && !linux && !windows)

package bof

import "errors"

func registerHostCallback(*HostAdapter, string) (uintptr, error) {
	return 0, errors.New("native BOF host callbacks are unsupported on this platform")
}
