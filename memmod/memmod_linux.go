//go:build linux && (386 || amd64 || arm64)

package memmod

import (
	"bytes"
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	rtldNow   = 2
	rtldLocal = 0
)

type linuxDynAPI struct {
	dlopen  uintptr
	dlsym   uintptr
	dlclose uintptr
	dlerror uintptr
}

var (
	linuxAPIOnce sync.Once
	linuxAPI     linuxDynAPI
	linuxAPIErr  error
)

type Module struct {
	mu     sync.RWMutex
	fd     int
	handle uintptr
	path   string
	closed bool
}

func LoadLibrary(data []byte) (*Module, error) {
	if len(data) == 0 {
		return nil, errors.New("empty ELF image")
	}
	if err := validateELFForCurrentArch(data); err != nil {
		return nil, err
	}

	api, err := getLinuxDynAPI()
	if err != nil {
		return nil, err
	}

	fd, err := createAnonymousLibraryFD()
	if err != nil {
		return nil, fmt.Errorf("create anonymous shared object fd: %w", err)
	}
	written := 0
	for written < len(data) {
		n, err := unix.Write(fd, data[written:])
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			_ = unix.Close(fd)
			return nil, fmt.Errorf("write anonymous shared object: %w", err)
		}
		if n <= 0 {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("write anonymous shared object: short write (%d/%d)", written, len(data))
		}
		written += n
	}

	module := &Module{
		fd:   fd,
		path: fmt.Sprintf("/proc/self/fd/%d", fd),
	}

	cPath, err := cStringBytes(module.path)
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}

	// clear stale dlerror
	_ = cCall0(api.dlerror)
	handle := cCall2(api.dlopen, cStringPtr(cPath), uintptr(rtldNow|rtldLocal))
	runtime.KeepAlive(cPath)
	if handle == 0 {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("dlopen(%s): %w", module.path, lastDLErrorWithFallback(api, "unknown dlopen error"))
	}

	module.handle = handle
	return module, nil
}

func (module *Module) Free() {
	module.mu.Lock()
	defer module.mu.Unlock()

	if module.closed {
		return
	}
	module.closed = true

	if module.handle != 0 {
		if api, err := getLinuxDynAPI(); err == nil {
			_ = cCall1(api.dlclose, module.handle)
		}
		module.handle = 0
	}
	if module.fd >= 0 {
		_ = unix.Close(module.fd)
		module.fd = -1
	}
}

func (module *Module) CallExport(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("export name cannot be empty")
	}

	candidates := []string{name}
	if strings.HasPrefix(name, "_") {
		candidates = append(candidates, strings.TrimPrefix(name, "_"))
	} else {
		candidates = append(candidates, "_"+name)
	}

	var (
		addr uintptr
		err  error
	)
	for _, candidate := range candidates {
		addr, err = module.ProcAddressByName(candidate)
		if err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("resolve export %q: %w", name, err)
	}

	_ = cCall0(addr)
	return nil
}

func (module *Module) ProcAddressByName(name string) (uintptr, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("export name cannot be empty")
	}

	module.mu.RLock()
	if module.closed {
		module.mu.RUnlock()
		return 0, errors.New("library is closed")
	}
	handle := module.handle
	module.mu.RUnlock()
	if handle == 0 {
		return 0, errors.New("library handle is nil")
	}

	api, err := getLinuxDynAPI()
	if err != nil {
		return 0, err
	}

	cName, err := cStringBytes(name)
	if err != nil {
		return 0, err
	}

	// clear stale dlerror
	_ = cCall0(api.dlerror)
	sym := cCall2(api.dlsym, handle, cStringPtr(cName))
	runtime.KeepAlive(cName)
	if err := lastDLError(api); err != nil {
		return 0, fmt.Errorf("dlsym(%s): %w", name, err)
	}
	if sym == 0 {
		return 0, errors.New("symbol address is nil")
	}
	return sym, nil
}

func (module *Module) ProcAddressByOrdinal(ordinal uint16) (uintptr, error) {
	_ = ordinal
	return 0, errors.New("ProcAddressByOrdinal is not supported on linux; use ProcAddressByName")
}

func createAnonymousLibraryFD() (int, error) {
	// Prefer O_TMPFILE on tmpfs so there is never a directory entry.
	fd, err := unix.Open("/dev/shm", unix.O_RDWR|unix.O_CLOEXEC|unix.O_TMPFILE, 0o600)
	if err == nil {
		return fd, nil
	}

	// Fallback: create under /dev/shm then unlink immediately. The open fd
	// remains usable via /proc/self/fd/<n> while avoiding persistent files.
	f, tmpErr := os.CreateTemp("/dev/shm", "reflektor-memmod-*")
	if tmpErr != nil {
		return -1, errors.Join(err, tmpErr)
	}
	name := f.Name()
	if rmErr := os.Remove(name); rmErr != nil {
		_ = f.Close()
		return -1, fmt.Errorf("unlink temp shared object %s: %w", name, rmErr)
	}
	dupFD, dupErr := unix.Dup(int(f.Fd()))
	if closeErr := f.Close(); closeErr != nil && dupErr == nil {
		return -1, fmt.Errorf("close temp shared object file %s: %w", name, closeErr)
	}
	if dupErr != nil {
		return -1, fmt.Errorf("dup temp shared object fd: %w", dupErr)
	}
	return dupFD, nil
}

func cStringBytes(s string) ([]byte, error) {
	if strings.ContainsRune(s, '\x00') {
		return nil, errors.New("string contains NUL")
	}
	b := make([]byte, len(s)+1)
	copy(b, s)
	return b, nil
}

func cStringPtr(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b[0]))
}

func cStringFromPtr(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	const maxLen = 1 << 20
	buf := make([]byte, 0, 64)
	for i := 0; i < maxLen; i++ {
		ch := *(*byte)(unsafe.Pointer(ptr + uintptr(i)))
		if ch == 0 {
			return string(buf)
		}
		buf = append(buf, ch)
	}
	return string(buf)
}

func lastDLError(api *linuxDynAPI) error {
	msg := cStringFromPtr(cCall0(api.dlerror))
	if msg == "" {
		return nil
	}
	return errors.New(msg)
}

func lastDLErrorWithFallback(api *linuxDynAPI, fallback string) error {
	if err := lastDLError(api); err != nil {
		return err
	}
	return errors.New(fallback)
}

func getLinuxDynAPI() (*linuxDynAPI, error) {
	linuxAPIOnce.Do(func() {
		linuxAPIErr = initLinuxDynAPI()
	})
	if linuxAPIErr != nil {
		return nil, linuxAPIErr
	}
	return &linuxAPI, nil
}

func initLinuxDynAPI() error {
	libcPath, baseAddr, err := findRuntimeLibc()
	if err != nil {
		return err
	}

	dlopenOff, err := findELFSymbolOffset(libcPath, "dlopen")
	if err != nil {
		return fmt.Errorf("resolve libc symbol dlopen: %w", err)
	}
	dlsymOff, err := findELFSymbolOffset(libcPath, "dlsym")
	if err != nil {
		return fmt.Errorf("resolve libc symbol dlsym: %w", err)
	}
	dlcloseOff, err := findELFSymbolOffset(libcPath, "dlclose")
	if err != nil {
		return fmt.Errorf("resolve libc symbol dlclose: %w", err)
	}
	dlerrorOff, err := findELFSymbolOffset(libcPath, "dlerror")
	if err != nil {
		return fmt.Errorf("resolve libc symbol dlerror: %w", err)
	}

	linuxAPI = linuxDynAPI{
		dlopen:  baseAddr + dlopenOff,
		dlsym:   baseAddr + dlsymOff,
		dlclose: baseAddr + dlcloseOff,
		dlerror: baseAddr + dlerrorOff,
	}
	return nil
}

type procMapEntry struct {
	start  uintptr
	offset uintptr
	perms  string
	path   string
}

func findRuntimeLibc() (string, uintptr, error) {
	entries, err := readProcMaps()
	if err != nil {
		return "", 0, err
	}

	bestScore := -1
	var best procMapEntry
	for _, entry := range entries {
		score := libcPathScore(entry.path)
		if score > bestScore {
			bestScore = score
			best = entry
		}
	}
	if bestScore < 0 || best.path == "" {
		return "", 0, errors.New("failed to locate runtime libc mapping")
	}
	if best.start < best.offset {
		return "", 0, fmt.Errorf("invalid libc mapping base for %s", best.path)
	}
	return best.path, best.start - best.offset, nil
}

func libcPathScore(path string) int {
	p := strings.ToLower(path)
	switch {
	case strings.Contains(p, "libc.so"):
		return 100
	case strings.Contains(p, "libc-"):
		return 95
	case strings.Contains(p, "ld-musl"):
		return 90
	case strings.Contains(p, "musl"):
		return 85
	case strings.Contains(p, "ld-linux"):
		return 80
	default:
		return -1
	}
}

func readProcMaps() ([]procMapEntry, error) {
	raw, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		return nil, fmt.Errorf("read /proc/self/maps: %w", err)
	}

	lines := strings.Split(string(raw), "\n")
	entries := make([]procMapEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if !strings.Contains(fields[1], "x") {
			continue
		}

		rangeParts := strings.SplitN(fields[0], "-", 2)
		if len(rangeParts) != 2 {
			continue
		}
		start, startErr := parseHexUintptr(rangeParts[0])
		offset, offsetErr := parseHexUintptr(fields[2])
		if startErr != nil || offsetErr != nil {
			continue
		}

		path := ""
		if len(fields) >= 6 {
			path = strings.Join(fields[5:], " ")
			path = strings.TrimSuffix(path, " (deleted)")
		}
		if path == "" || !strings.HasPrefix(path, "/") {
			continue
		}

		entries = append(entries, procMapEntry{
			start:  start,
			offset: offset,
			perms:  fields[1],
			path:   path,
		})
	}
	return entries, nil
}

func parseHexUintptr(s string) (uintptr, error) {
	var out uintptr
	for _, r := range s {
		out <<= 4
		switch {
		case r >= '0' && r <= '9':
			out += uintptr(r - '0')
		case r >= 'a' && r <= 'f':
			out += uintptr(r-'a') + 10
		case r >= 'A' && r <= 'F':
			out += uintptr(r-'A') + 10
		default:
			return 0, fmt.Errorf("invalid hex string %q", s)
		}
	}
	return out, nil
}

func findELFSymbolOffset(path string, symbol string) (uintptr, error) {
	f, err := elf.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open elf %s: %w", path, err)
	}
	defer f.Close()

	if syms, err := f.DynamicSymbols(); err == nil {
		if off, ok := matchSymbolOffset(syms, symbol); ok {
			return off, nil
		}
	}
	if syms, err := f.Symbols(); err == nil {
		if off, ok := matchSymbolOffset(syms, symbol); ok {
			return off, nil
		}
	}
	return 0, fmt.Errorf("symbol %s not found in %s", symbol, path)
}

func matchSymbolOffset(symbols []elf.Symbol, want string) (uintptr, bool) {
	for _, s := range symbols {
		if s.Value == 0 {
			continue
		}
		if s.Name == want || strings.HasPrefix(s.Name, want+"@") {
			return uintptr(s.Value), true
		}
	}
	return 0, false
}

func validateELFForCurrentArch(data []byte) error {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid ELF image: %w", err)
	}
	defer f.Close()

	machine, err := currentELFMachine()
	if err != nil {
		return err
	}
	if f.Machine != machine {
		return fmt.Errorf("foreign platform (provided: %s, expected: %s)", f.Machine, machine)
	}
	if f.Type != elf.ET_DYN {
		return fmt.Errorf("unsupported ELF file type: %s", f.Type)
	}
	return nil
}

func currentELFMachine() (elf.Machine, error) {
	switch runtime.GOARCH {
	case "386":
		return elf.EM_386, nil
	case "amd64":
		return elf.EM_X86_64, nil
	case "arm64":
		return elf.EM_AARCH64, nil
	default:
		return 0, fmt.Errorf("unsupported linux architecture: %s", runtime.GOARCH)
	}
}
