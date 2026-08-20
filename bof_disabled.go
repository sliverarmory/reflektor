//go:build !bof

package reflektor

// BOFEnabled reports whether this Reflektor build includes BOF loading.
const BOFEnabled = false

func loadBOF([]byte) (bofHandle, error) {
	return nil, ErrBOFDisabled
}
