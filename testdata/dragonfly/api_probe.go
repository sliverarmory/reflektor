// Run this on DragonFly with a BOF object and C shared library to record the
// current public API behavior without changing the support manifest.
package main

import (
	"fmt"
	"os"

	"github.com/sliverarmory/reflektor"
	"github.com/sliverarmory/reflektor/bof"
	"github.com/sliverarmory/reflektor/native"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: api_probe.go <BOF.o> <library.so>")
		os.Exit(2)
	}
	object, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	library, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	loadedBOF, bofErr := bof.Load(object)
	if loadedBOF != nil {
		defer loadedBOF.Close()
	}
	rootLibrary, rootErr := reflektor.LoadLibrary(library)
	if rootLibrary != nil {
		defer rootLibrary.Close()
	}
	nativeLibrary, nativeErr := native.LoadLibrary(library)
	if nativeLibrary != nil {
		defer nativeLibrary.Close()
	}
	fmt.Printf("bof.Load: %v\n", bofErr)
	fmt.Printf("reflektor.LoadLibrary: %v\n", rootErr)
	fmt.Printf("native.LoadLibrary: %v\n", nativeErr)
}
