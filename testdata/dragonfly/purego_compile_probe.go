// Build this file only. It checks whether the pinned PureGo release provides
// its foreign-call bridge for the current target without executing a call.
package main

import "github.com/ebitengine/purego"

var _ = purego.SyscallN

func main() {}
