//go:build windows && (amd64 || arm64)

package bofloader

import (
	"encoding/binary"
	"runtime"
	"strings"
	"testing"
)

func TestCollectUnwindTablesRejectsMalformedPdata(t *testing.T) {
	entrySize := 12
	if runtime.GOARCH == "arm64" {
		entrySize = 8
	}
	object, region, _ := unwindTestFixture(entrySize)
	object.sections[0].size = uint64(entrySize - 1)

	_, err := collectUnwindTables(object, region)
	if err == nil || !strings.Contains(err.Error(), "not a multiple") {
		t.Fatalf("collectUnwindTables() error = %v, want size error", err)
	}
}

func TestCollectUnwindTablesTrimsTrailingPadding(t *testing.T) {
	entrySize := 12
	if runtime.GOARCH == "arm64" {
		entrySize = 8
	}
	object, region, data := unwindTestFixture(entrySize)
	binary.LittleEndian.PutUint32(data[0:4], 0x200)
	if runtime.GOARCH == "arm64" {
		binary.LittleEndian.PutUint32(data[4:8], 1|(0x10<<2)) // 0x40-byte packed function
	} else {
		binary.LittleEndian.PutUint32(data[4:8], 0x240)
		binary.LittleEndian.PutUint32(data[8:12], 0x300)
	}
	object.sections[0].name = ".pdata$fixture"

	tables, err := collectUnwindTables(object, region)
	if err != nil {
		t.Fatalf("collectUnwindTables() error = %v", err)
	}
	if len(tables) != 1 || tables[0].entries != 1 {
		t.Fatalf("collectUnwindTables() = %#v, want one table with one entry", tables)
	}
}

func TestCollectUnwindTablesRejectsInvalidMappedRanges(t *testing.T) {
	entrySize := unwindTestEntrySize()

	t.Run("section address mismatch", func(t *testing.T) {
		object, region, _ := unwindTestFixture(entrySize)
		object.sections[1].address++
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "address does not match") {
			t.Fatalf("collectUnwindTables() error = %v, want address mismatch", err)
		}
	})

	t.Run("function outside executable section", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		writeUnwindTestEntry(data, 0x500, 0x540, 0x300)
		object.sections[0].size = uint64(entrySize)
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "executable range") {
			t.Fatalf("collectUnwindTables() error = %v, want executable-range rejection", err)
		}
	})

	t.Run("unwind data outside readable section", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		writeUnwindTestEntry(data, 0x200, 0x240, 0x500)
		object.sections[0].size = uint64(entrySize)
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "readable") {
			t.Fatalf("collectUnwindTables() error = %v, want readable-range rejection", err)
		}
	})
}

func TestCollectUnwindTablesRejectsOverlappingFunctionsAcrossTables(t *testing.T) {
	entrySize := unwindTestEntrySize()
	object, region, first := unwindTestFixture(entrySize)
	object.sections[0].size = uint64(entrySize)
	writeUnwindTestEntry(first, 0x200, 0x240, 0x300)

	secondOffset := uint64(0x180)
	second := region.data[secondOffset : secondOffset+uint64(entrySize)]
	writeUnwindTestEntry(second, 0x220, 0x260, 0x300)
	object.sections = append(object.sections, objectSection{
		name:       ".pdata$second",
		mapped:     true,
		offset:     secondOffset,
		address:    region.address + uintptr(secondOffset),
		size:       uint64(entrySize),
		protection: protRead,
	})

	if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("collectUnwindTables() error = %v, want overlap rejection", err)
	}
}

func unwindTestEntrySize() int {
	if runtime.GOARCH == "arm64" {
		return 8
	}
	return 12
}

func unwindTestFixture(entrySize int) (*objectFile, *memoryRegion, []byte) {
	region := &memoryRegion{address: 0x100000, data: make([]byte, 0x1000)}
	object := &objectFile{
		imageBase: region.address,
		sections: []objectSection{
			{
				name:       ".pdata",
				mapped:     true,
				offset:     0x100,
				address:    region.address + 0x100,
				size:       uint64(2 * entrySize),
				protection: protRead,
			},
			{
				name:       ".text",
				mapped:     true,
				offset:     0x200,
				address:    region.address + 0x200,
				size:       0x100,
				protection: protRead | protExec,
			},
			{
				name:       ".xdata",
				mapped:     true,
				offset:     0x300,
				address:    region.address + 0x300,
				size:       0x100,
				protection: protRead,
			},
		},
	}
	if entrySize == 12 {
		region.data[0x300] = 1 // x64 UNWIND_INFO version 1, no codes or flags.
	}
	return object, region, region.data[0x100 : 0x100+2*entrySize]
}

func writeUnwindTestEntry(data []byte, begin, end, unwind uint32) {
	binary.LittleEndian.PutUint32(data[0:4], begin)
	if runtime.GOARCH == "arm64" {
		length := (end - begin) / 4
		binary.LittleEndian.PutUint32(data[4:8], 1|(length<<2))
		if unwind&3 == 0 && unwind != 0x300 {
			binary.LittleEndian.PutUint32(data[4:8], unwind)
		}
		return
	}
	binary.LittleEndian.PutUint32(data[4:8], end)
	binary.LittleEndian.PutUint32(data[8:12], unwind)
}
