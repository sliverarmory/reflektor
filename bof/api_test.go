package bof

import (
	"encoding/binary"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/sliverarmory/reflektor/internal/bofloader"
)

type retryCloseLoader struct {
	closeCalls int
}

func (*retryCloseLoader) Execute([]byte) ([]bofloader.Output, error) {
	return nil, nil
}

func (loader *retryCloseLoader) Close() error {
	loader.closeCalls++
	if loader.closeCalls == 1 {
		return errors.New("retry cleanup")
	}
	return nil
}

func TestObjectCloseIsIdempotent(t *testing.T) {
	object := &Object{loader: &bofloader.Loader{}}
	if err := object.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := object.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if _, err := object.Execute(nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("Execute() error = %v, want ErrClosed", err)
	}
}

func TestObjectCloseRetriesCleanupWithoutReenablingExecution(t *testing.T) {
	loader := &retryCloseLoader{}
	object := &Object{loader: loader}
	if err := object.Close(); err == nil {
		t.Fatal("first Close() succeeded, want retryable cleanup error")
	}
	if _, err := object.Execute(nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("Execute() after failed Close error = %v, want ErrClosed", err)
	}
	if err := object.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if loader.closeCalls != 2 {
		t.Fatalf("close calls = %d, want 2", loader.closeCalls)
	}
	if err := object.Close(); err != nil {
		t.Fatalf("third Close() error = %v", err)
	}
}

func TestObjectTranslatesLoaderClosedError(t *testing.T) {
	object := &Object{loader: &bofloader.Loader{}}
	if _, err := object.Execute(nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("Execute() error = %v, want ErrClosed", err)
	}
}

func TestLoadRejectsEmptyImage(t *testing.T) {
	if _, err := Load(nil); err == nil {
		t.Fatal("Load accepted an empty image")
	}
}

func TestArguments(t *testing.T) {
	var arguments Arguments
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

func TestArgumentsRejectEmbeddedNUL(t *testing.T) {
	var arguments Arguments
	if err := arguments.AddString("a\x00b"); err == nil {
		t.Fatal("AddString accepted embedded NUL")
	}
	if err := arguments.AddUTF16String("a\x00b"); err == nil {
		t.Fatal("AddUTF16String accepted embedded NUL")
	}
}

func TestArgumentsEnforceCallbackLimit(t *testing.T) {
	arguments := Arguments{payload: make([]byte, maxArgumentPayloadSize)}
	if err := arguments.AddInt16(1); err == nil {
		t.Fatal("AddInt16 accepted an argument beyond the callback buffer limit")
	}
	arguments.payload = make([]byte, maxArgumentPayloadSize-4)
	if err := arguments.AddBytes(nil); err != nil {
		t.Fatalf("AddBytes at limit: %v", err)
	}
	if got := len(arguments.Bytes()); got != maxArgumentBufferSize {
		t.Fatalf("packed length = %d, want %d", got, maxArgumentBufferSize)
	}
}

func TestObjectExecuteRejectsOversizedArguments(t *testing.T) {
	object := &Object{loader: &bofloader.Loader{}}
	if _, err := object.Execute(make([]byte, maxArgumentBufferSize+1)); err == nil {
		t.Fatal("Execute accepted an argument buffer beyond the callback limit")
	}
}

func TestLoadFileRejectsOversizedRegularFile(t *testing.T) {
	path := t.TempDir() + "/oversized.o"
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxObjectSize + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatal("LoadFile accepted an oversized file")
	}
}
