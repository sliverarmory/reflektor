package reflektor

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode/utf16"
)

const (
	maxBOFObjectSize          = 64 << 20
	maxBOFArgumentBufferSize  = 16 << 20
	maxBOFArgumentPayloadSize = maxBOFArgumentBufferSize - 4
)

var (
	// ErrBOFDisabled is returned when Reflektor was built without the `bof`
	// build tag. Rebuild the importing program with `-tags bof` to enable BOFs.
	ErrBOFDisabled = errors.New("reflektor: BOF support is disabled; rebuild with -tags bof")
	// ErrBOFClosed is returned when Execute is called after Close.
	ErrBOFClosed = errors.New("reflektor: BOF is closed")
)

// BOFOutput is one typed record emitted through BeaconOutput or BeaconPrintf.
// Type is the Beacon output channel supplied by the object and Data is an
// owned copy of that record's bytes.
type BOFOutput struct {
	Type int
	Data []byte
}

// BOFImport describes one external symbol referenced by a BOF image. Name is
// the exact object-file symbol spelling. Builtin marks callbacks implemented by
// Reflektor. RequiresHost marks Beacon APIs that Reflektor deliberately does
// not implement and will not search for in system libraries.
type BOFImport struct {
	Name         string
	Weak         bool
	Builtin      bool
	RequiresHost bool
}

// BOFLoadOptions controls entry-point selection and external-symbol policy.
// Its zero value preserves LoadBOF's default behavior.
type BOFLoadOptions struct {
	// EntryPoint selects an exact defined executable symbol. When empty,
	// Reflektor searches go, _go, coffee, and _coffee in that order.
	EntryPoint string

	// ValidateImports runs after parsing and host validation, but before image
	// allocation, callback registration, or dynamic-library lookup. The slice
	// is an owned, deterministic snapshot and may be retained by the callback.
	ValidateImports func([]BOFImport) error

	// ResolveSymbol may provide a native address for an import that is not a
	// built-in Reflektor callback. Function addresses must follow the object's
	// platform ABI. Returning handled=false falls back to the normal system
	// resolver, except for RequiresHost imports, which fail explicitly. Any
	// returned error aborts loading. A handled address must be nonzero and
	// remain valid until the loaded BOF is closed. The resolver is called at
	// most once for each exact imported name during each load.
	ResolveSymbol func(BOFImport) (address uintptr, handled bool, err error)
}

// Beacon output channel values used by Cobalt-compatible BOFs.
const (
	BOFOutputDefault = 0x00
	BOFOutputError   = 0x0d
	BOFOutputOEM     = 0x1e
	BOFOutputUTF8    = 0x20
)

// BOFArguments builds the length-prefixed argument format consumed by the
// BeaconData* callbacks. Its zero value is ready for use.
type BOFArguments struct {
	payload []byte
}

// AddInt32 appends a little-endian Beacon "integer" argument.
func (arguments *BOFArguments) AddInt32(value int32) error {
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], uint32(value))
	return arguments.append(encoded[:])
}

// AddInt16 appends a little-endian Beacon "short" argument.
func (arguments *BOFArguments) AddInt16(value int16) error {
	var encoded [2]byte
	binary.LittleEndian.PutUint16(encoded[:], uint16(value))
	return arguments.append(encoded[:])
}

// AddBytes appends a four-byte length followed by an arbitrary byte string.
func (arguments *BOFArguments) AddBytes(value []byte) error {
	if uint64(len(arguments.payload))+4+uint64(len(value)) > maxBOFArgumentPayloadSize {
		return fmt.Errorf("reflektor: BOF argument payload exceeds %d bytes", maxBOFArgumentPayloadSize)
	}
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(len(value)))
	arguments.payload = append(arguments.payload, length[:]...)
	arguments.payload = append(arguments.payload, value...)
	return nil
}

// AddString appends a NUL-terminated UTF-8 string argument.
func (arguments *BOFArguments) AddString(value string) error {
	if strings.IndexByte(value, 0) >= 0 {
		return errors.New("reflektor: BOF string argument contains NUL")
	}
	encoded := make([]byte, len(value)+1)
	copy(encoded, value)
	return arguments.AddBytes(encoded)
}

// AddUTF16String appends a NUL-terminated UTF-16LE "wstring" argument.
func (arguments *BOFArguments) AddUTF16String(value string) error {
	if strings.IndexByte(value, 0) >= 0 {
		return errors.New("reflektor: BOF UTF-16 argument contains NUL")
	}
	codeUnits := utf16.Encode([]rune(value))
	encoded := make([]byte, (len(codeUnits)+1)*2)
	for index, codeUnit := range codeUnits {
		binary.LittleEndian.PutUint16(encoded[index*2:], codeUnit)
	}
	return arguments.AddBytes(encoded)
}

// Bytes returns an owned argument buffer with its four-byte payload-length
// prefix. Calling Bytes does not consume or alias the builder.
func (arguments *BOFArguments) Bytes() []byte {
	packed := make([]byte, 4+len(arguments.payload))
	binary.LittleEndian.PutUint32(packed, uint32(len(arguments.payload)))
	copy(packed[4:], arguments.payload)
	return packed
}

// Reset discards all currently packed arguments while retaining capacity.
func (arguments *BOFArguments) Reset() {
	arguments.payload = arguments.payload[:0]
}

func (arguments *BOFArguments) append(value []byte) error {
	if uint64(len(arguments.payload))+uint64(len(value)) > maxBOFArgumentPayloadSize {
		return fmt.Errorf("reflektor: BOF argument payload exceeds %d bytes", maxBOFArgumentPayloadSize)
	}
	arguments.payload = append(arguments.payload, value...)
	return nil
}

type bofHandle interface {
	execute([]byte) ([]BOFOutput, error)
	close() error
}

// BOF is an in-memory native relocatable Beacon Object File. A loaded BOF may
// be executed more than once. Close releases its mapped image.
type BOF struct {
	mu     sync.RWMutex
	handle bofHandle
	closed bool
}

// LoadBOF loads a native relocatable BOF image from memory using default load
// options. BOF support is opt-in and requires the `bof` build tag.
func LoadBOF(data []byte) (*BOF, error) {
	return LoadBOFWithOptions(data, BOFLoadOptions{})
}

// LoadBOFWithOptions loads a native relocatable BOF image from memory. Windows
// accepts COFF, Linux accepts ELF, and Darwin accepts native Mach-O plus legacy
// ELF relocatable objects.
func LoadBOFWithOptions(data []byte, options BOFLoadOptions) (*BOF, error) {
	if !BOFEnabled {
		return nil, ErrBOFDisabled
	}
	if len(data) == 0 {
		return nil, errors.New("reflektor: empty BOF image")
	}
	handle, err := loadBOF(data, options)
	if err != nil {
		return nil, err
	}
	return &BOF{handle: handle}, nil
}

// LoadBOFFile reads and loads a native relocatable BOF image from disk.
func LoadBOFFile(path string) (*BOF, error) {
	return LoadBOFFileWithOptions(path, BOFLoadOptions{})
}

// LoadBOFFileWithOptions reads and loads a native relocatable BOF image from
// disk using the supplied load options.
func LoadBOFFileWithOptions(path string, options BOFLoadOptions) (*BOF, error) {
	if !BOFEnabled {
		return nil, ErrBOFDisabled
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reflektor: read BOF file: %w", err)
	}
	defer file.Close()

	if info, statErr := file.Stat(); statErr == nil && info.Mode().IsRegular() && info.Size() > maxBOFObjectSize {
		return nil, fmt.Errorf("reflektor: BOF file is %d bytes; maximum is %d", info.Size(), maxBOFObjectSize)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBOFObjectSize+1))
	if err != nil {
		return nil, fmt.Errorf("reflektor: read BOF file: %w", err)
	}
	if len(data) > maxBOFObjectSize {
		return nil, fmt.Errorf("reflektor: BOF file exceeds %d bytes", maxBOFObjectSize)
	}
	return LoadBOFWithOptions(data, options)
}

// Execute invokes the object's go (or coffee) entry point with an encoded
// Beacon argument buffer and returns the records emitted by Beacon callbacks.
func (bof *BOF) Execute(args []byte) ([]BOFOutput, error) {
	if bof == nil {
		return nil, ErrBOFClosed
	}
	bof.mu.RLock()
	defer bof.mu.RUnlock()
	if bof.closed || bof.handle == nil {
		return nil, ErrBOFClosed
	}
	if len(args) > maxBOFArgumentBufferSize {
		return nil, fmt.Errorf("reflektor: BOF argument buffer is %d bytes; maximum is %d", len(args), maxBOFArgumentBufferSize)
	}
	return bof.handle.execute(args)
}

// Close releases the BOF's mapped image. It is safe to call more than once.
func (bof *BOF) Close() error {
	if bof == nil {
		return nil
	}
	bof.mu.Lock()
	defer bof.mu.Unlock()
	if bof.closed && bof.handle == nil {
		return nil
	}
	bof.closed = true
	if bof.handle == nil {
		return nil
	}
	if err := bof.handle.close(); err != nil {
		// Keep the handle only so a later Close can retry cleanup. Execute is
		// permanently disabled as soon as the first Close begins.
		return err
	}
	bof.handle = nil
	return nil
}
