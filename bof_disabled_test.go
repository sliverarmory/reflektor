//go:build !bof

package reflektor

import (
	"errors"
	"testing"
)

func TestBOFSupportDisabledWithoutBuildTag(t *testing.T) {
	if BOFEnabled {
		t.Fatal("BOFEnabled = true without bof build tag")
	}
	for _, image := range [][]byte{nil, {1}} {
		if _, err := LoadBOF(image); !errors.Is(err, ErrBOFDisabled) {
			t.Fatalf("LoadBOF(%v) error = %v, want ErrBOFDisabled", image, err)
		}
		if _, err := LoadBOFWithOptions(image, BOFLoadOptions{}); !errors.Is(err, ErrBOFDisabled) {
			t.Fatalf("LoadBOFWithOptions(%v) error = %v, want ErrBOFDisabled", image, err)
		}
	}
	if _, err := LoadBOFFile("does-not-need-to-exist.o"); !errors.Is(err, ErrBOFDisabled) {
		t.Fatalf("LoadBOFFile() error = %v, want ErrBOFDisabled", err)
	}
	if _, err := LoadBOFFileWithOptions("does-not-need-to-exist.o", BOFLoadOptions{}); !errors.Is(err, ErrBOFDisabled) {
		t.Fatalf("LoadBOFFileWithOptions() error = %v, want ErrBOFDisabled", err)
	}
}
