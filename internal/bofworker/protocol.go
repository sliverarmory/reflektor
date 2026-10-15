// Package bofworker defines the one-shot protocol between a BOF host and its
// same-version worker executable. It is deliberately not a public wire format.
package bofworker

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	MaxImageSize  = 64 << 20
	MaxArgsSize   = 16 << 20
	MaxEntrySize  = 4096
	MaxRecordSize = 16 << 20
	MaxErrorSize  = 64 << 10

	frameOutput = 1
	frameResult = 2
)

var magic = [8]byte{'R', 'B', 'O', 'F', 'W', '0', '1', 0}

// Request contains only data that is meaningful in the worker's address space.
type Request struct {
	Image      []byte
	Args       []byte
	EntryPoint string
}

func WriteRequest(w io.Writer, request Request) error {
	if err := validateRequestLengths(len(request.Image), len(request.Args), len(request.EntryPoint)); err != nil {
		return err
	}
	var header [20]byte
	copy(header[:8], magic[:])
	binary.LittleEndian.PutUint32(header[8:12], uint32(len(request.Image)))
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(request.Args)))
	binary.LittleEndian.PutUint32(header[16:20], uint32(len(request.EntryPoint)))
	for _, part := range [][]byte{header[:], request.Image, request.Args, []byte(request.EntryPoint)} {
		if err := writeAll(w, part); err != nil {
			return err
		}
	}
	return nil
}

func ReadRequest(r io.Reader) (Request, error) {
	var header [20]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Request{}, err
	}
	if [8]byte(header[:8]) != magic {
		return Request{}, errors.New("reflektor: incompatible BOF worker protocol")
	}
	imageLength := binary.LittleEndian.Uint32(header[8:12])
	argsLength := binary.LittleEndian.Uint32(header[12:16])
	entryLength := binary.LittleEndian.Uint32(header[16:20])
	if imageLength == 0 || imageLength > MaxImageSize || argsLength > MaxArgsSize || entryLength > MaxEntrySize {
		return Request{}, errors.New("reflektor: invalid BOF worker request lengths")
	}
	imageSize := int(imageLength)
	argsSize := int(argsLength)
	entrySize := int(entryLength)
	if err := validateRequestLengths(imageSize, argsSize, entrySize); err != nil {
		return Request{}, err
	}
	request := Request{
		Image: make([]byte, imageSize),
		Args:  make([]byte, argsSize),
	}
	entry := make([]byte, entrySize)
	for _, part := range [][]byte{request.Image, request.Args, entry} {
		if _, err := io.ReadFull(r, part); err != nil {
			return Request{}, err
		}
	}
	request.EntryPoint = string(entry)
	return request, nil
}

func validateRequestLengths(imageSize, argsSize, entrySize int) error {
	if imageSize <= 0 || imageSize > MaxImageSize {
		return fmt.Errorf("reflektor: BOF worker image length must be 1..%d bytes", MaxImageSize)
	}
	if argsSize < 0 || argsSize > MaxArgsSize {
		return fmt.Errorf("reflektor: BOF worker argument length exceeds %d bytes", MaxArgsSize)
	}
	if entrySize < 0 || entrySize > MaxEntrySize {
		return fmt.Errorf("reflektor: BOF worker entry point exceeds %d bytes", MaxEntrySize)
	}
	return nil
}

func WriteOutput(w io.Writer, outputType int, data []byte) error {
	if len(data) > MaxRecordSize {
		return fmt.Errorf("reflektor: BOF worker output record exceeds %d bytes", MaxRecordSize)
	}
	var header [9]byte
	header[0] = frameOutput
	binary.LittleEndian.PutUint32(header[1:5], uint32(int32(outputType)))
	binary.LittleEndian.PutUint32(header[5:9], uint32(len(data)))
	if err := writeAll(w, header[:]); err != nil {
		return err
	}
	return writeAll(w, data)
}

func WriteResult(w io.Writer, message string) error {
	if len(message) > MaxErrorSize {
		message = message[:MaxErrorSize]
	}
	var header [5]byte
	header[0] = frameResult
	binary.LittleEndian.PutUint32(header[1:5], uint32(len(message)))
	if err := writeAll(w, header[:]); err != nil {
		return err
	}
	return writeAll(w, []byte(message))
}

// ReadResponse delivers output records in wire order and returns the worker's
// terminal error message. Empty message means successful execution.
func ReadResponse(r io.Reader, emit func(int, []byte) error) (string, error) {
	for {
		var kind [1]byte
		if _, err := io.ReadFull(r, kind[:]); err != nil {
			return "", err
		}
		switch kind[0] {
		case frameOutput:
			var header [8]byte
			if _, err := io.ReadFull(r, header[:]); err != nil {
				return "", err
			}
			length := binary.LittleEndian.Uint32(header[4:8])
			if length > MaxRecordSize {
				return "", fmt.Errorf("reflektor: BOF worker output record exceeds %d bytes", MaxRecordSize)
			}
			data := make([]byte, length)
			if _, err := io.ReadFull(r, data); err != nil {
				return "", err
			}
			if err := emit(int(int32(binary.LittleEndian.Uint32(header[:4]))), data); err != nil {
				return "", err
			}
		case frameResult:
			var length [4]byte
			if _, err := io.ReadFull(r, length[:]); err != nil {
				return "", err
			}
			if binary.LittleEndian.Uint32(length[:]) > MaxErrorSize {
				return "", fmt.Errorf("reflektor: BOF worker error exceeds %d bytes", MaxErrorSize)
			}
			message := make([]byte, binary.LittleEndian.Uint32(length[:]))
			if _, err := io.ReadFull(r, message); err != nil {
				return "", err
			}
			return string(message), nil
		default:
			return "", fmt.Errorf("reflektor: invalid BOF worker frame type %d", kind[0])
		}
	}
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) != 0 {
		count, err := w.Write(data)
		if err != nil {
			return err
		}
		if count <= 0 || count > len(data) {
			return io.ErrShortWrite
		}
		data = data[count:]
	}
	return nil
}
