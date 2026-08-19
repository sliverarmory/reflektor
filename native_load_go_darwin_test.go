//go:build darwin && (amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/sliverarmory/reflektor/native"
)

func TestNativePackageRejectsGoCSharedInLaterFatSlice(t *testing.T) {
	requireCommand(t, "zig")

	otherArch := "amd64"
	if runtime.GOARCH == "amd64" {
		otherArch = "arm64"
	}
	nativePath := buildArgumentSharedLib(t, t.TempDir(), "darwin", otherArch)
	goPath := buildOneGoSharedLib(t, t.TempDir(), "darwin", runtime.GOARCH)
	mixed := buildMixedDarwinFat(t, nativePath, goPath)

	library, err := native.LoadLibrary(mixed)
	if library != nil {
		_ = library.Close()
		t.Fatal("native.LoadLibrary returned a library for a fat image containing a later Go slice")
	}
	if !errors.Is(err, native.ErrGoSharedLibraryUnsupported) {
		t.Fatalf("native.LoadLibrary(mixed fat) error = %v, want ErrGoSharedLibraryUnsupported", err)
	}
}

func buildMixedDarwinFat(t *testing.T, nativePath string, goPath string) []byte {
	t.Helper()
	type fatSlice struct {
		cpu    macho.Cpu
		subCPU uint32
		data   []byte
	}
	readSlice := func(path string) fatSlice {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read Mach-O slice %q: %v", path, err)
		}
		file, err := macho.NewFile(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("parse Mach-O slice %q: %v", path, err)
		}
		defer file.Close()
		return fatSlice{cpu: file.Cpu, subCPU: file.SubCpu, data: data}
	}

	slices := []fatSlice{readSlice(nativePath), readSlice(goPath)}
	if slices[0].cpu == slices[1].cpu {
		t.Fatalf("mixed-fat slices have duplicate CPU type %v", slices[0].cpu)
	}
	const (
		fatHeaderSize  = 8
		fatArchSize    = 20
		fatSliceAlign  = uint32(14)
		fatSliceStride = 1 << fatSliceAlign
	)
	align := func(value int) int {
		return (value + fatSliceStride - 1) &^ (fatSliceStride - 1)
	}

	offsets := make([]int, len(slices))
	cursor := align(fatHeaderSize + len(slices)*fatArchSize)
	for index := range slices {
		offsets[index] = cursor
		cursor = align(cursor + len(slices[index].data))
	}
	if uint64(cursor) > uint64(^uint32(0)) {
		t.Fatalf("mixed-fat fixture is too large: %d bytes", cursor)
	}

	image := make([]byte, cursor)
	binary.BigEndian.PutUint32(image[0:4], macho.MagicFat)
	binary.BigEndian.PutUint32(image[4:8], uint32(len(slices)))
	for index := range slices {
		record := fatHeaderSize + index*fatArchSize
		binary.BigEndian.PutUint32(image[record:record+4], uint32(slices[index].cpu))
		binary.BigEndian.PutUint32(image[record+4:record+8], slices[index].subCPU)
		binary.BigEndian.PutUint32(image[record+8:record+12], uint32(offsets[index]))
		binary.BigEndian.PutUint32(image[record+12:record+16], uint32(len(slices[index].data)))
		binary.BigEndian.PutUint32(image[record+16:record+20], fatSliceAlign)
		copy(image[offsets[index]:], slices[index].data)
	}

	fat, err := macho.NewFatFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("parse constructed mixed-fat fixture: %v", err)
	}
	defer fat.Close()
	if len(fat.Arches) != 2 {
		t.Fatalf("mixed-fat architecture count = %d, want 2", len(fat.Arches))
	}
	if fat.Arches[0].File.Section("__go_buildinfo") != nil {
		t.Fatal("first mixed-fat slice unexpectedly contains Go build info")
	}
	if fat.Arches[1].File.Section("__go_buildinfo") == nil {
		t.Fatal("second mixed-fat slice does not contain Go build info")
	}
	if fat.Arches[0].Cpu != slices[0].cpu || fat.Arches[1].Cpu != slices[1].cpu {
		t.Fatalf("mixed-fat slice order changed: got [%v %v], want [%v %v]", fat.Arches[0].Cpu, fat.Arches[1].Cpu, slices[0].cpu, slices[1].cpu)
	}
	return image
}
