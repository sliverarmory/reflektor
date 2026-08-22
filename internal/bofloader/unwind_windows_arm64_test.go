//go:build windows && arm64

package bofloader

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestCollectUnwindTablesValidatesARM64XData(t *testing.T) {
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
