//go:build linux && cgo

package cgobootstrap

/*
#include <stdlib.h>
*/
import "C"

// Force a cgo-linked object into linux builds so libc/libdl are present for
// runtime symbol and dependency resolution in the in-memory loader.
var _ = C.int(0)
