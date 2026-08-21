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

func loadBOF(data []byte, options BOFLoadOptions) (bofHandle, error) {
	loaderOptions := bofloader.LoadOptions{EntryPoint: options.EntryPoint}
	if options.ValidateImports != nil {
		loaderOptions.ValidateImports = func(imports []bofloader.Import) error {
			converted := make([]BOFImport, len(imports))
			for index, imported := range imports {
				converted[index] = BOFImport{
					Name:         imported.Name,
					Weak:         imported.Weak,
					Builtin:      imported.Builtin,
					RequiresHost: imported.RequiresHost,
				}
			}
			return options.ValidateImports(converted)
		}
	}
	if options.ResolveSymbol != nil {
		loaderOptions.ResolveSymbol = func(imported bofloader.Import) (uintptr, bool, error) {
			return options.ResolveSymbol(BOFImport{
				Name:         imported.Name,
				Weak:         imported.Weak,
				Builtin:      imported.Builtin,
				RequiresHost: imported.RequiresHost,
			})
		}
	}
	loader, err := bofloader.LoadWithOptions(data, loaderOptions)
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
