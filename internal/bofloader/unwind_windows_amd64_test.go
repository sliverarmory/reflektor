//go:build windows && amd64

package bofloader

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestCollectUnwindTablesRejectsUnalignedX64UnwindRVA(t *testing.T) {
	object, region, data := unwindTestFixture(unwindTestEntrySize())
	object.sections[0].size = uint64(unwindTestEntrySize())
	writeUnwindTestEntry(data, 0x200, 0x240, 0x301)

	if _, err := collectUnwindTables(object, region); err == nil || !strings.Contains(err.Error(), "unaligned x64 unwind RVA") {
		t.Fatalf("collectUnwindTables() error = %v, want alignment rejection", err)
	}
}

func TestCollectUnwindTablesValidatesX64UnwindInfo(t *testing.T) {
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
