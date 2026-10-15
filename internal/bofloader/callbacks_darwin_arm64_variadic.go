//go:build darwin && !ios && arm64

package bofloader

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
)

var darwinARM64VariadicCallbacks struct {
	once   sync.Once
	values map[string]uintptr
	err    error
}

func resolveMachOARM64VariadicCallback(symbol string) (uintptr, error) {
	darwinARM64VariadicCallbacks.once.Do(func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				darwinARM64VariadicCallbacks.err = fmt.Errorf("register Darwin arm64 variadic Beacon callbacks: %v", recovered)
			}
		}()
		darwinARM64VariadicCallbacks.values = map[string]uintptr{
			"BeaconPrintf":       purego.NewCallback(beaconPrintfDarwinARM64),
			"BeaconFormatPrintf": purego.NewCallback(beaconFormatPrintfDarwinARM64),
		}
	})
	if darwinARM64VariadicCallbacks.err != nil {
		return 0, darwinARM64VariadicCallbacks.err
	}
	address := darwinARM64VariadicCallbacks.values[normalizeImportedSymbol(symbol)]
	if address == 0 {
		return 0, fmt.Errorf("unknown Darwin arm64 variadic callback %q", symbol)
	}
	return address, nil
}

func formatPrintfDarwinARM64Stack(address, stackAddress uintptr) (string, error) {
	if stackAddress == 0 || stackAddress&15 != 0 {
		return "", fmt.Errorf("invalid Darwin arm64 variadic stack address %#x", stackAddress)
	}
	return formatPrintfArguments(address, printfArguments{
		stackAddress: stackAddress,
		wordSize:     8,
	})
}

func beaconPrintfDarwinARM64(typeValue, formatAddress, stackAddress uintptr) (result uintptr) {
	defer callbackPanic("BeaconPrintf")
	formatted, err := formatPrintfDarwinARM64Stack(formatAddress, stackAddress)
	if err != nil {
		callbackError("BeaconPrintf", "%v", err)
	}
	if context := activeExecutionContext(); context != nil {
		context.appendOutput(int(int32(typeValue)), []byte(formatted))
	}
	return 0
}

func beaconFormatPrintfDarwinARM64(formatAddress, formatStringAddress, stackAddress uintptr) (result uintptr) {
	defer callbackPanic("BeaconFormatPrintf")
	format, data, ok := checkedFormat("BeaconFormatPrintf", formatAddress)
	if !ok {
		return 0
	}
	formatted, err := formatPrintfDarwinARM64Stack(formatStringAddress, stackAddress)
	if err != nil {
		callbackError("BeaconFormatPrintf", "%v", err)
	}
	if len(formatted) > len(data)-int(format.length) {
		callbackError("BeaconFormatPrintf", "formatted data of %d bytes exceeds remaining capacity %d", len(formatted), len(data)-int(format.length))
		return 0
	}
	copy(data[int(format.length):], formatted)
	format.length += int32(len(formatted))
	format.buffer = format.original + uintptr(format.length)
	return 0
}
