package reflektor

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
)

type retryCloseBOFHandle struct {
	closeCalls int
}

func (*retryCloseBOFHandle) execute([]byte) ([]BOFOutput, error) {
	return nil, nil
}

func (handle *retryCloseBOFHandle) close() error {
	handle.closeCalls++
	if handle.closeCalls == 1 {
		return errors.New("retry cleanup")
	}
	return nil
}

func TestBOFCloseIsIdempotent(t *testing.T) {
	bof := &BOF{}
	if err := bof.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := bof.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if _, err := bof.Execute(nil); !errors.Is(err, ErrBOFClosed) {
		t.Fatalf("Execute() error = %v, want ErrBOFClosed", err)
	}
}

func TestBOFCloseRetriesCleanupWithoutReenablingExecution(t *testing.T) {
	handle := &retryCloseBOFHandle{}
	bof := &BOF{handle: handle}
	if err := bof.Close(); err == nil {
		t.Fatal("first Close() succeeded, want retryable cleanup error")
	}
	if _, err := bof.Execute(nil); !errors.Is(err, ErrBOFClosed) {
		t.Fatalf("Execute() after failed Close error = %v, want ErrBOFClosed", err)
	}
	if err := bof.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if handle.closeCalls != 2 {
		t.Fatalf("close calls = %d, want 2", handle.closeCalls)
	}
	if err := bof.Close(); err != nil {
		t.Fatalf("third Close() error = %v", err)
	}
}

func TestBOFArguments(t *testing.T) {
	var arguments BOFArguments
	if err := arguments.AddInt32(-2); err != nil {
		t.Fatal(err)
	}
	if err := arguments.AddInt16(0x1234); err != nil {
		t.Fatal(err)
	}
	if err := arguments.AddString("hi"); err != nil {
		t.Fatal(err)
	}
	if err := arguments.AddUTF16String("A"); err != nil {
		t.Fatal(err)
	}
	if err := arguments.AddBytes([]byte{0xaa, 0xbb}); err != nil {
		t.Fatal(err)
	}

	packed := arguments.Bytes()
	wantPayload := []byte{
		0xfe, 0xff, 0xff, 0xff,
		0x34, 0x12,
		0x03, 0x00, 0x00, 0x00, 'h', 'i', 0,
		0x04, 0x00, 0x00, 0x00, 'A', 0, 0, 0,
		0x02, 0x00, 0x00, 0x00, 0xaa, 0xbb,
	}
	if got := binary.LittleEndian.Uint32(packed[:4]); got != uint32(len(wantPayload)) {
		t.Fatalf("payload length = %d, want %d", got, len(wantPayload))
	}
	if !reflect.DeepEqual(packed[4:], wantPayload) {
		t.Fatalf("payload = %x, want %x", packed[4:], wantPayload)
	}

	packed[4] = 0
	if arguments.Bytes()[4] != 0xfe {
		t.Fatal("Bytes returned an alias of the builder")
	}
	arguments.Reset()
	if got := arguments.Bytes(); !reflect.DeepEqual(got, []byte{0, 0, 0, 0}) {
		t.Fatalf("Bytes after Reset = %x", got)
	}
}

func TestBOFArgumentsRejectEmbeddedNUL(t *testing.T) {
	var arguments BOFArguments
	if err := arguments.AddString("a\x00b"); err == nil {
		t.Fatal("AddString accepted embedded NUL")
	}
	if err := arguments.AddUTF16String("a\x00b"); err == nil {
		t.Fatal("AddUTF16String accepted embedded NUL")
	}
}

func TestBOFArgumentsEnforceCallbackLimit(t *testing.T) {
	arguments := BOFArguments{payload: make([]byte, maxBOFArgumentPayloadSize)}
	if err := arguments.AddInt16(1); err == nil {
		t.Fatal("AddInt16 accepted an argument beyond the callback buffer limit")
	}
	arguments.payload = make([]byte, maxBOFArgumentPayloadSize-4)
	if err := arguments.AddBytes(nil); err != nil {
		t.Fatalf("AddBytes at limit: %v", err)
	}
	if got := len(arguments.Bytes()); got != maxBOFArgumentBufferSize {
		t.Fatalf("packed length = %d, want %d", got, maxBOFArgumentBufferSize)
	}
}

func TestBOFExecuteRejectsOversizedArguments(t *testing.T) {
	bof := &BOF{handle: &retryCloseBOFHandle{}}
	if _, err := bof.Execute(make([]byte, maxBOFArgumentBufferSize+1)); err == nil {
		t.Fatal("Execute accepted an argument buffer beyond the callback limit")
	}
}
