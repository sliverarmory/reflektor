package bofworker

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

func TestProtocolPreservesBinaryTypedRecords(t *testing.T) {
	wantRequest := Request{Image: []byte{1, 0, 2}, Args: []byte{0xff}, EntryPoint: "_go"}
	var requestWire bytes.Buffer
	if err := WriteRequest(&requestWire, wantRequest); err != nil {
		t.Fatal(err)
	}
	gotRequest, err := ReadRequest(&requestWire)
	if err != nil || !reflect.DeepEqual(gotRequest, wantRequest) {
		t.Fatalf("request = %#v, %v; want %#v", gotRequest, err, wantRequest)
	}

	var responseWire bytes.Buffer
	if err := WriteOutput(&responseWire, -7, []byte{0xff, 0, 0x80}); err != nil {
		t.Fatal(err)
	}
	if err := WriteOutput(&responseWire, 0, nil); err != nil {
		t.Fatal(err)
	}
	if err := WriteResult(&responseWire, "native failure"); err != nil {
		t.Fatal(err)
	}
	type record struct {
		kind int
		data []byte
	}
	var records []record
	message, err := ReadResponse(&responseWire, func(kind int, data []byte) error {
		records = append(records, record{kind: kind, data: data})
		return nil
	})
	if err != nil || message != "native failure" || !reflect.DeepEqual(records, []record{{-7, []byte{0xff, 0, 0x80}}, {0, []byte{}}}) {
		t.Fatalf("response = %#v, %q, %v", records, message, err)
	}
}

func TestReadRequestRejectsOversizedLengthsBeforeAllocation(t *testing.T) {
	for _, offset := range []int{8, 12, 16} {
		var header [20]byte
		copy(header[:8], magic[:])
		binary.LittleEndian.PutUint32(header[8:12], 1)
		binary.LittleEndian.PutUint32(header[offset:offset+4], ^uint32(0))
		if _, err := ReadRequest(bytes.NewReader(header[:])); err == nil {
			t.Fatalf("oversized length at offset %d was accepted", offset)
		}
	}
}

func TestReadResponseRejectsOversizedRecord(t *testing.T) {
	var header [9]byte
	header[0] = frameOutput
	binary.LittleEndian.PutUint32(header[5:9], MaxRecordSize+1)
	if _, err := ReadResponse(bytes.NewReader(header[:]), func(int, []byte) error { return nil }); err == nil {
		t.Fatal("oversized output record was accepted")
	}
}
