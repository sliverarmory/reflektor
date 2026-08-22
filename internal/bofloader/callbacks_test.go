package bofloader

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

func TestBeaconDataCallbacks(t *testing.T) {
	executionLock.Lock()
	defer executionLock.Unlock()
	context := newExecutionContext()
	activeExecution.Store(context)
	defer activeExecution.Store(nil)

	argument := make([]byte, 4+4+2+4+5)
	binary.LittleEndian.PutUint32(argument[0:4], uint32(len(argument)-4))
	binary.LittleEndian.PutUint32(argument[4:8], 0xf1234567)
	binary.LittleEndian.PutUint16(argument[8:10], 0x8123)
	binary.LittleEndian.PutUint32(argument[10:14], 5)
	copy(argument[14:], "hello")
	var parser beaconDataParser
	beaconDataParse(uintptr(unsafe.Pointer(&parser)), byteSliceAddress(argument), uintptr(len(argument)))
	if got := uint32(beaconDataInt(uintptr(unsafe.Pointer(&parser)))); got != 0xf1234567 {
		t.Fatalf("BeaconDataInt = %#x", got)
	}
	if got := uint16(beaconDataShort(uintptr(unsafe.Pointer(&parser)))); got != 0x8123 {
		t.Fatalf("BeaconDataShort = %#x", got)
	}
	var extractedSize int32
	extracted := beaconDataExtract(uintptr(unsafe.Pointer(&parser)), uintptr(unsafe.Pointer(&extractedSize)))
	if extractedSize != 5 || string(pointerBytes(extracted, int(extractedSize))) != "hello" {
		t.Fatalf("BeaconDataExtract = %q, %d", pointerBytes(extracted, int(extractedSize)), extractedSize)
	}
	if got := beaconDataLength(uintptr(unsafe.Pointer(&parser))); got != 0 {
		t.Fatalf("BeaconDataLength = %d", got)
	}
	if _, err := context.result(); err != nil {
		t.Fatalf("callback error: %v", err)
	}
}

func TestBeaconDataExtractOrNull(t *testing.T) {
	executionLock.Lock()
	defer executionLock.Unlock()
	context := newExecutionContext()
	activeExecution.Store(context)
	defer activeExecution.Store(nil)

	argument := make([]byte, 4+4+1+4+4)
	binary.LittleEndian.PutUint32(argument[0:4], uint32(len(argument)-4))
	binary.LittleEndian.PutUint32(argument[4:8], 1)
	argument[8] = 0
	binary.LittleEndian.PutUint32(argument[9:13], 4)
	copy(argument[13:], "text")

	var parser beaconDataParser
	beaconDataParse(uintptr(unsafe.Pointer(&parser)), byteSliceAddress(argument), uintptr(len(argument)))
	var extractedSize int32
	if got := beaconDataExtractOrNull(uintptr(unsafe.Pointer(&parser)), uintptr(unsafe.Pointer(&extractedSize))); got != 0 || extractedSize != 1 {
		t.Fatalf("empty BeaconDataExtractOrNull = %#x, %d", got, extractedSize)
	}
	got := beaconDataExtractOrNull(uintptr(unsafe.Pointer(&parser)), uintptr(unsafe.Pointer(&extractedSize)))
	if got == 0 || extractedSize != 4 || string(pointerBytes(got, int(extractedSize))) != "text" {
		t.Fatalf("non-empty BeaconDataExtractOrNull = %#x, %d", got, extractedSize)
	}
	if _, err := context.result(); err != nil {
		t.Fatalf("callback error: %v", err)
	}
}

func TestBeaconFormatAndOutputCallbacks(t *testing.T) {
	executionLock.Lock()
	defer executionLock.Unlock()
	context := newExecutionContext()
	activeExecution.Store(context)
	defer activeExecution.Store(nil)

	var format beaconFormat
	beaconFormatAlloc(uintptr(unsafe.Pointer(&format)), 128)
	prefix := []byte("prefix:")
	beaconFormatAppend(uintptr(unsafe.Pointer(&format)), byteSliceAddress(prefix), uintptr(len(prefix)))
	formatString := append([]byte(" %08x %s"), 0)
	word := append([]byte("done"), 0)
	beaconFormatPrintf(
		uintptr(unsafe.Pointer(&format)),
		byteSliceAddress(formatString),
		0x2a,
		byteSliceAddress(word),
		0, 0, 0, 0, 0, 0, 0, 0,
	)
	beaconFormatInt(uintptr(unsafe.Pointer(&format)), 0x01020304)
	var outputSize int32
	outputAddress := beaconFormatToString(uintptr(unsafe.Pointer(&format)), uintptr(unsafe.Pointer(&outputSize)))
	want := append([]byte("prefix: 0000002a done"), 1, 2, 3, 4)
	if got := pointerBytes(outputAddress, int(outputSize)); !bytes.Equal(got, want) {
		t.Fatalf("formatted data = %q, want %q", got, want)
	}
	beaconOutput(13, outputAddress, uintptr(outputSize))
	beaconFormatFree(uintptr(unsafe.Pointer(&format)))

	outputs, err := context.result()
	if err != nil {
		t.Fatalf("callback error: %v", err)
	}
	if len(outputs) != 1 || outputs[0].Type != 13 || !bytes.Equal(outputs[0].Data, want) {
		t.Fatalf("outputs = %#v", outputs)
	}
}

func TestBeaconPrintfFormatting(t *testing.T) {
	executionLock.Lock()
	defer executionLock.Unlock()
	context := newExecutionContext()
	activeExecution.Store(context)
	defer activeExecution.Store(nil)

	format := append([]byte("pid=%d hex=%#x name=%s %%"), 0)
	name := append([]byte("reflektor"), 0)
	beaconPrintf(0, byteSliceAddress(format), 42, 42, byteSliceAddress(name), 0, 0, 0, 0, 0, 0, 0)
	outputs, err := context.result()
	if err != nil {
		t.Fatalf("callback error: %v", err)
	}
	if len(outputs) != 1 || string(outputs[0].Data) != "pid=42 hex=0x2a name=reflektor %" {
		t.Fatalf("outputs = %#v", outputs)
	}
}

func TestBeaconPrintfWidePrecisionDoesNotSplitUTF8(t *testing.T) {
	format := append([]byte("%.1ls|%.2ls|%.3ls"), 0)
	var wide16 = [...]uint16{'é', 'x', 0}
	var wide32 = [...]uint32{'é', 'x', 0}
	wideAddress := uintptr(unsafe.Pointer(&wide32[0]))
	if runtime.GOOS == "windows" {
		wideAddress = uintptr(unsafe.Pointer(&wide16[0]))
	}
	formatted, err := formatPrintf(byteSliceAddress(format), [maxPrintfArgument]uintptr{
		wideAddress, wideAddress, wideAddress,
	})
	runtime.KeepAlive(wide16)
	runtime.KeepAlive(wide32)
	if err != nil {
		t.Fatalf("formatPrintf() error = %v", err)
	}
	if formatted != "|é|éx" {
		t.Fatalf("formatPrintf() = %q, want %q", formatted, "|é|éx")
	}
}

func TestBeaconCallbackResolution(t *testing.T) {
	for _, name := range []string{
		"BeaconOutput", "_BeaconOutput", "__imp_BeaconOutput", "__imp__BeaconOutput@12",
		"BeaconDataExtractOrNull", "_BeaconDataExtractOrNull", "toWideChar", "_toWideChar", "_toWideChar@12", "__imp_toWideChar", "__imp__toWideChar@12",
	} {
		address, ok, err := resolveBeaconCallback(name)
		if err != nil {
			t.Fatalf("resolveBeaconCallback(%q): %v", name, err)
		}
		if !ok || address == 0 {
			t.Fatalf("resolveBeaconCallback(%q) = %#x, %v", name, address, ok)
		}
	}
	if address, ok, err := resolveBeaconCallback("BeaconInjectProcess"); err != nil || ok || address != 0 {
		t.Fatalf("unsupported privileged callback resolved: address=%#x ok=%v err=%v", address, ok, err)
	}
}

func TestMalformedCallbackIsReported(t *testing.T) {
	executionLock.Lock()
	defer executionLock.Unlock()
	context := newExecutionContext()
	activeExecution.Store(context)
	defer activeExecution.Store(nil)

	var parser beaconDataParser
	invalid := []byte{1, 2}
	beaconDataParse(uintptr(unsafe.Pointer(&parser)), byteSliceAddress(invalid), uintptr(len(invalid)))
	_, err := context.result()
	if err == nil || !strings.Contains(err.Error(), "BeaconDataParse") {
		t.Fatalf("error = %v", err)
	}
}

func TestWriteThunk(t *testing.T) {
	buffer := make([]byte, 16)
	target := uintptr(0x12345678)
	if pointerSize() == 8 {
		wideTarget := uint64(0x1234567812345678)
		target = uintptr(wideTarget)
	}
	if err := writeThunk(buffer, target); err != nil {
		t.Fatal(err)
	}
	switch runtime.GOARCH {
	case "386":
		if buffer[0] != 0xb8 || buffer[5] != 0xff || buffer[6] != 0xe0 || binary.LittleEndian.Uint32(buffer[1:5]) != uint32(target) {
			t.Fatalf("386 thunk = %x", buffer)
		}
	case "amd64":
		if !bytes.Equal(buffer[:6], []byte{0xff, 0x25, 0, 0, 0, 0}) || binary.LittleEndian.Uint64(buffer[6:14]) != uint64(target) {
			t.Fatalf("amd64 thunk = %x", buffer)
		}
	case "arm64":
		if binary.LittleEndian.Uint32(buffer[:4]) != 0x58000050 || binary.LittleEndian.Uint32(buffer[4:8]) != 0xd61f0200 || binary.LittleEndian.Uint64(buffer[8:]) != uint64(target) {
			t.Fatalf("arm64 thunk = %x", buffer)
		}
	}
}
