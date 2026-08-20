//go:build bof && ((darwin && (amd64 || arm64)) || (linux && (386 || amd64 || arm64)))

package bofloader

import "github.com/ebitengine/purego"

func platformCallbacks() map[string]uintptr {
	return map[string]uintptr{
		"BeaconDataParse":      purego.NewCallback(beaconDataParse),
		"BeaconDataInt":        purego.NewCallback(beaconDataInt),
		"BeaconDataShort":      purego.NewCallback(beaconDataShort),
		"BeaconDataLength":     purego.NewCallback(beaconDataLength),
		"BeaconDataExtract":    purego.NewCallback(beaconDataExtract),
		"BeaconFormatAlloc":    purego.NewCallback(beaconFormatAlloc),
		"BeaconFormatReset":    purego.NewCallback(beaconFormatReset),
		"BeaconFormatFree":     purego.NewCallback(beaconFormatFree),
		"BeaconFormatAppend":   purego.NewCallback(beaconFormatAppend),
		"BeaconFormatPrintf":   purego.NewCallback(beaconFormatPrintf),
		"BeaconFormatToString": purego.NewCallback(beaconFormatToString),
		"BeaconFormatInt":      purego.NewCallback(beaconFormatInt),
		"BeaconPrintf":         purego.NewCallback(beaconPrintf),
		"BeaconOutput":         purego.NewCallback(beaconOutput),
	}
}
