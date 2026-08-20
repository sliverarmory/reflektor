//go:build bof && windows && (amd64 || arm64)

package bofloader

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type unwindRegistration struct {
	tables []uintptr
}

type unwindTable struct {
	address uintptr
	entries uint32
}

//go:nocheckptr
func registerUnwindInfo(object *objectFile, region *memoryRegion) (*unwindRegistration, error) {
	tables, err := collectUnwindTables(object, region)
	if err != nil {
		return nil, fmt.Errorf("bofloader: validate Windows unwind information: %w", err)
	}
	if len(tables) == 0 {
		return nil, nil
	}

	registration := &unwindRegistration{tables: make([]uintptr, 0, len(tables))}
	for _, table := range tables {
		functionTable := (*windows.RUNTIME_FUNCTION)(unsafe.Pointer(table.address))
		if !windows.RtlAddFunctionTable(functionTable, table.entries, object.imageBase) {
			rollbackErr := registration.close()
			registrationError := errors.Join(
				fmt.Errorf("bofloader: RtlAddFunctionTable rejected %d entries at %#x", table.entries, table.address),
				rollbackErr,
			)
			if rollbackErr != nil {
				return registration, registrationError
			}
			return nil, registrationError
		}
		registration.tables = append(registration.tables, table.address)
	}
	return registration, nil
}

//go:nocheckptr
func (registration *unwindRegistration) close() error {
	if registration == nil || len(registration.tables) == 0 {
		return nil
	}
	for index := len(registration.tables) - 1; index >= 0; index-- {
		address := registration.tables[index]
		functionTable := (*windows.RUNTIME_FUNCTION)(unsafe.Pointer(address))
		if !windows.RtlDeleteFunctionTable(functionTable) {
			// Keep this and all earlier table addresses so a later Close call can
			// retry without unmapping memory still referenced by the OS unwinder.
			registration.tables = registration.tables[:index+1]
			return fmt.Errorf("RtlDeleteFunctionTable rejected table at %#x", address)
		}
	}
	registration.tables = nil
	return nil
}

func collectUnwindTables(object *objectFile, region *memoryRegion) ([]unwindTable, error) {
	if object == nil || region == nil || region.address == 0 {
		return nil, errors.New("nil or closed mapped object")
	}
	entrySize := uint64(12)
	if runtime.GOARCH == "arm64" {
		entrySize = 8
	}

	imageSize := uint64(len(region.data))
	tables := make([]unwindTable, 0, 1)
	for sectionIndex := range object.sections {
		section := &object.sections[sectionIndex]
		if section.name != ".pdata" && !strings.HasPrefix(section.name, ".pdata$") {
			continue
		}
		if !section.mapped || section.size == 0 {
			continue
		}
		if section.address == 0 || section.offset > imageSize || section.size > imageSize-section.offset {
			return nil, fmt.Errorf("section %q is outside the mapped image", section.name)
		}
		if section.size%entrySize != 0 {
			return nil, fmt.Errorf("section %q size %d is not a multiple of %d", section.name, section.size, entrySize)
		}

		data := region.data[section.offset : section.offset+section.size]
		entries := len(data) / int(entrySize)
		for entries != 0 && allZero(data[(entries-1)*int(entrySize):entries*int(entrySize)]) {
			entries--
		}
		if entries == 0 {
			continue
		}
		if err := validateUnwindEntries(section.name, data[:entries*int(entrySize)], int(entrySize), imageSize); err != nil {
			return nil, err
		}
		tables = append(tables, unwindTable{address: section.address, entries: uint32(entries)})
	}
	return tables, nil
}

func validateUnwindEntries(sectionName string, data []byte, entrySize int, imageSize uint64) error {
	var previousBegin uint32
	for offset := 0; offset < len(data); offset += entrySize {
		entry := data[offset : offset+entrySize]
		if allZero(entry) {
			return fmt.Errorf("section %q has an empty unwind entry at index %d", sectionName, offset/entrySize)
		}
		begin := binary.LittleEndian.Uint32(entry[0:4])
		if uint64(begin) >= imageSize {
			return fmt.Errorf("section %q entry %d begin RVA %#x exceeds image size %#x", sectionName, offset/entrySize, begin, imageSize)
		}
		if offset != 0 && begin < previousBegin {
			return fmt.Errorf("section %q unwind entries are not sorted by begin RVA", sectionName)
		}
		previousBegin = begin

		if entrySize == 12 {
			end := binary.LittleEndian.Uint32(entry[4:8])
			if end <= begin || uint64(end) > imageSize {
				return fmt.Errorf("section %q entry %d has invalid range [%#x, %#x)", sectionName, offset/entrySize, begin, end)
			}
			unwindRVA := binary.LittleEndian.Uint32(entry[8:12]) &^ uint32(3)
			if unwindRVA == 0 || uint64(unwindRVA) >= imageSize {
				return fmt.Errorf("section %q entry %d has invalid unwind RVA %#x", sectionName, offset/entrySize, unwindRVA)
			}
			continue
		}

		// ARM64 uses either packed unwind data (low bits non-zero) or an RVA
		// to an xdata record (low bits zero).
		unwindData := binary.LittleEndian.Uint32(entry[4:8])
		if unwindData&3 == 0 && (unwindData == 0 || uint64(unwindData) >= imageSize) {
			return fmt.Errorf("section %q entry %d has invalid xdata RVA %#x", sectionName, offset/entrySize, unwindData)
		}
	}
	return nil
}

func allZero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}
