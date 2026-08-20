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
	region := &memoryRegion{address: 0x100000, data: make([]byte, 0x1000)}
	object := &objectFile{sections: []objectSection{{
		name:    ".pdata",
		mapped:  true,
		offset:  0x100,
		address: region.address + 0x100,
		size:    uint64(entrySize - 1),
	}}}

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
	region := &memoryRegion{address: 0x100000, data: make([]byte, 0x1000)}
	data := region.data[0x100 : 0x100+2*entrySize]
	binary.LittleEndian.PutUint32(data[0:4], 0x200)
	if runtime.GOARCH == "arm64" {
		binary.LittleEndian.PutUint32(data[4:8], 1) // packed unwind record
	} else {
		binary.LittleEndian.PutUint32(data[4:8], 0x240)
		binary.LittleEndian.PutUint32(data[8:12], 0x300)
	}
	object := &objectFile{sections: []objectSection{{
		name:    ".pdata$fixture",
		mapped:  true,
		offset:  0x100,
		address: region.address + 0x100,
		size:    uint64(len(data)),
	}}}

	tables, err := collectUnwindTables(object, region)
	if err != nil {
		t.Fatalf("collectUnwindTables() error = %v", err)
	}
	if len(tables) != 1 || tables[0].entries != 1 {
		t.Fatalf("collectUnwindTables() = %#v, want one table with one entry", tables)
	}
}
