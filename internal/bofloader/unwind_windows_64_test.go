//go:build bof && windows && (amd64 || arm64)

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

func TestCollectUnwindTablesRejectsUnalignedX64UnwindRVA(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("x64 unwind encoding")
	}
	object, region, data := unwindTestFixture(unwindTestEntrySize())
	object.sections[0].size = uint64(unwindTestEntrySize())
	writeUnwindTestEntry(data, 0x200, 0x240, 0x301)

	if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "unaligned x64 unwind RVA") {
		t.Fatalf("collectUnwindTables() error = %v, want alignment rejection", err)
	}
}

func TestCollectUnwindTablesValidatesARM64XData(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("ARM64 xdata encoding")
	}
	entrySize := unwindTestEntrySize()

	t.Run("valid extended header", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		binary.LittleEndian.PutUint32(data[0:4], 0x200)
		binary.LittleEndian.PutUint32(data[4:8], 0x300)
		binary.LittleEndian.PutUint32(region.data[0x300:0x304], 0x10) // 0x40-byte function
		if _, err := collectUnwindTables(object, region); err != nil {
			t.Fatalf("collectUnwindTables() error = %v", err)
		}
	})

	t.Run("reserved packed flag", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		binary.LittleEndian.PutUint32(data[0:4], 0x200)
		binary.LittleEndian.PutUint32(data[4:8], 3|(0x10<<2))
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("collectUnwindTables() error = %v, want reserved-flag rejection", err)
		}
	})

	t.Run("zero packed length", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		binary.LittleEndian.PutUint32(data[0:4], 0x200)
		binary.LittleEndian.PutUint32(data[4:8], 1)
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "zero-length") {
			t.Fatalf("collectUnwindTables() error = %v, want zero-length rejection", err)
		}
	})

	t.Run("unsupported xdata version", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		binary.LittleEndian.PutUint32(data[0:4], 0x200)
		binary.LittleEndian.PutUint32(data[4:8], 0x300)
		binary.LittleEndian.PutUint32(region.data[0x300:0x304], 0x10|(1<<18))
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "version") {
			t.Fatalf("collectUnwindTables() error = %v, want version rejection", err)
		}
	})

	t.Run("xdata crosses section boundary", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		binary.LittleEndian.PutUint32(data[0:4], 0x200)
		binary.LittleEndian.PutUint32(data[4:8], 0x3fc)
		binary.LittleEndian.PutUint32(region.data[0x3fc:0x400], 0x10|(1<<22))
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "boundary") {
			t.Fatalf("collectUnwindTables() error = %v, want xdata-boundary rejection", err)
		}
	})

	t.Run("packed epilog index outside unwind codes", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		binary.LittleEndian.PutUint32(data[0:4], 0x200)
		binary.LittleEndian.PutUint32(data[4:8], 0x300)
		binary.LittleEndian.PutUint32(region.data[0x300:0x304], 0x10|(1<<21)|(4<<22)|(1<<27))
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "packed epilog index") {
			t.Fatalf("collectUnwindTables() error = %v, want epilog-index rejection", err)
		}
	})

	t.Run("scope reserved bits", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		binary.LittleEndian.PutUint32(data[0:4], 0x200)
		binary.LittleEndian.PutUint32(data[4:8], 0x300)
		binary.LittleEndian.PutUint32(region.data[0x300:0x304], 0x10|(1<<22)|(1<<27))
		binary.LittleEndian.PutUint32(region.data[0x304:0x308], 1<<18)
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "reserved bits") {
			t.Fatalf("collectUnwindTables() error = %v, want scope-reserved rejection", err)
		}
	})

	t.Run("unaligned exception handler", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		binary.LittleEndian.PutUint32(data[0:4], 0x200)
		binary.LittleEndian.PutUint32(data[4:8], 0x300)
		binary.LittleEndian.PutUint32(region.data[0x300:0x304], 0x10|(1<<20)|(1<<21)|(1<<27))
		binary.LittleEndian.PutUint32(region.data[0x308:0x30c], 0x201)
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "exception-handler") {
			t.Fatalf("collectUnwindTables() error = %v, want handler-alignment rejection", err)
		}
	})
}

func TestCollectUnwindTablesValidatesX64UnwindInfo(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("x64 unwind encoding")
	}
	entrySize := unwindTestEntrySize()

	t.Run("version", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		writeUnwindTestEntry(data, 0x200, 0x240, 0x300)
		region.data[0x300] = 0
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "unwind version") {
			t.Fatalf("collectUnwindTables() error = %v, want version rejection", err)
		}
	})

	t.Run("truncated code slots", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		object.sections[2].size = 4
		writeUnwindTestEntry(data, 0x200, 0x240, 0x300)
		region.data[0x300] = 1
		region.data[0x302] = 2
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "crosses a readable-section boundary") {
			t.Fatalf("collectUnwindTables() error = %v, want record-boundary rejection", err)
		}
	})

	t.Run("operation overruns count", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		writeUnwindTestEntry(data, 0x200, 0x240, 0x300)
		region.data[0x300] = 1
		region.data[0x301] = 1
		region.data[0x302] = 1
		region.data[0x304] = 1
		region.data[0x305] = 0x11 // UWOP_ALLOC_LARGE, info 1: requires three slots.
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "requires 3 slots") {
			t.Fatalf("collectUnwindTables() error = %v, want slot-count rejection", err)
		}
	})

	t.Run("truncated chained trailer", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		object.sections[2].size = 4
		writeUnwindTestEntry(data, 0x200, 0x240, 0x300)
		region.data[0x300] = 1 | 4<<3
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "crosses a readable-section boundary") {
			t.Fatalf("collectUnwindTables() error = %v, want chained-trailer rejection", err)
		}
	})

	t.Run("prolog exceeds function", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		writeUnwindTestEntry(data, 0x200, 0x240, 0x300)
		region.data[0x300] = 1
		region.data[0x301] = 0x41
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "prolog size") {
			t.Fatalf("collectUnwindTables() error = %v, want prolog-size rejection", err)
		}
	})

	t.Run("chained cycle", func(t *testing.T) {
		object, region, data := unwindTestFixture(entrySize)
		object.sections[0].size = uint64(entrySize)
		writeUnwindTestEntry(data, 0x200, 0x240, 0x300)
		region.data[0x300] = 1 | 4<<3
		binary.LittleEndian.PutUint32(region.data[0x304:0x308], 0x200)
		binary.LittleEndian.PutUint32(region.data[0x308:0x30c], 0x240)
		binary.LittleEndian.PutUint32(region.data[0x30c:0x310], 0x300)
		if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "cycle") {
			t.Fatalf("collectUnwindTables() error = %v, want chain-cycle rejection", err)
		}
	})
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
