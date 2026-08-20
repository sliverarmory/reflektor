//go:build bof

package reflektor

import (
	"errors"
	"fmt"

	"github.com/sliverarmory/reflektor/internal/bofloader"
)

// BOFEnabled reports whether this Reflektor build includes BOF loading.
const BOFEnabled = true

type enabledBOFHandle struct {
	loader *bofloader.Loader
}

func loadBOF(data []byte) (bofHandle, error) {
	loader, err := bofloader.Load(data)
	if err != nil {
		return nil, fmt.Errorf("reflektor: load BOF: %w", err)
	}
	return &enabledBOFHandle{loader: loader}, nil
}

func (handle *enabledBOFHandle) execute(args []byte) ([]BOFOutput, error) {
	outputs, err := handle.loader.Execute(args)
	converted := make([]BOFOutput, len(outputs))
	for index := range outputs {
		converted[index] = BOFOutput{Type: outputs[index].Type, Data: outputs[index].Data}
	}
	if errors.Is(err, bofloader.ErrClosed) {
		err = ErrBOFClosed
	}
	return converted, err
}

func (handle *enabledBOFHandle) close() error {
	return handle.loader.Close()
}
