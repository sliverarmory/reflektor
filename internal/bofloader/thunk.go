package bofloader

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
)

func writePointer(destination []byte, value uintptr) {
	switch pointerSize() {
	case 4:
		binary.LittleEndian.PutUint32(destination, uint32(value))
	case 8:
		binary.LittleEndian.PutUint64(destination, uint64(value))
	default:
		panic("bofloader: unsupported pointer size")
	}
}

func writeThunk(destination []byte, target uintptr) error {
	if len(destination) < 16 {
		return errors.New("import thunk requires a 16-byte destination")
	}
	clear(destination[:16])
	switch runtime.GOARCH {
	case "386":
		// MOV EAX, imm32; JMP EAX.
		destination[0] = 0xb8
		binary.LittleEndian.PutUint32(destination[1:5], uint32(target))
		destination[5] = 0xff
		destination[6] = 0xe0
	case "amd64":
		// JMP QWORD PTR [RIP+0], followed by the absolute destination.
		copy(destination[:6], []byte{0xff, 0x25, 0x00, 0x00, 0x00, 0x00})
		binary.LittleEndian.PutUint64(destination[6:14], uint64(target))
	case "arm64":
		// LDR X16, #8; BR X16; followed by the absolute destination.
		binary.LittleEndian.PutUint32(destination[0:4], 0x58000050)
		binary.LittleEndian.PutUint32(destination[4:8], 0xd61f0200)
		binary.LittleEndian.PutUint64(destination[8:16], uint64(target))
	default:
		return fmt.Errorf("import thunks are unsupported on %s", runtime.GOARCH)
	}
	return nil
}
