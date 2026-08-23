# Reflektor

<img align="right" src=".github/images/reflektor.png" alt="Reflektor" width="300">

Reflektor is a Go library and CLI for loading shared libraries from bytes and
invoking exported functions. Its `bof` subpackage loads and executes native
Beacon Object Files.

It exposes a stable root package (`reflektor`) so other projects can import it directly, while platform-specific loading is handled behind `memmod`.

<br clear="right">

## Platform Support

The table below describes the root `reflektor` shared-library loader, the
`native` package, and recursive shared-library loading. BOF support is tracked
separately because it can cover a host without implying shared-library parity.

| OS | Architectures | Shared Library Format | Status | Loader Notes |
| --- | --- | --- | --- | --- |
| Windows | `386`, `amd64`, `arm64` | PE (`.dll`) | Supported | In-memory PE loader |
| Darwin | `amd64`, `arm64` | Mach-O (`.dylib`, bundle) | Supported | Dyld4 root-image loader with system dependencies registered through public dyld; supports no-cgo builds and avoids temp-file legacy NS APIs. |
| Linux | `386`, `amd64`, `arm64`, `ppc64le`, `riscv64`; ARMv7 hard-float (`GOARM=7`) | ELF (`.so`) | Supported | Pure Go in-memory ELF loader (maps PT_LOAD segments, applies relocations, resolves externals from runtime modules/`dlsym`); no `memfd`, no `/dev/shm`, no temp-file disk writes. ARMv7, ppc64le, and riscv64 receive QEMU runtime proof. |
| Other | - | - | Unsupported | Returns an explicit unsupported-platform error. |

The machine-readable [platform capability manifest](platform-support.json)
lists every non-Wasm `go tool dist list` target and records BOF format,
endianness, CGO requirements, shared-library surfaces, Go `c-shared`
availability, architecture variants where required, and the runtime proof for
each target. `runtime` means CI executes
the surface, `runtime-emulated` means CI executes it through emulation,
`unsupported` means Reflektor has not implemented that surface, and `n-a`
means the pinned Go toolchain does not provide the applicable build mode.

## Public API

Import path:

```go
import "github.com/sliverarmory/reflektor"
```

Example:

```go
payload := []byte{}

lib, err := reflektor.LoadLibrary(payload)
if err != nil {
    return err
}
defer lib.Close()

if err := lib.CallExport("StartW"); err != nil {
    return err
}
```

Native C and Rust exports can also receive up to three machine-word arguments
and return a machine-word value:

```go
result, err := lib.CallExportWithArgs(
    "Run",
    uintptr(unsafe.Pointer(unsafe.SliceData(input))),
    uintptr(uint32(len(input))),
    callbackPointer,
)
runtime.KeepAlive(input)
```

This matches extension entry points such as
`Run(char *buffer, uint32_t size, callback_fn callback)`. Convert a Go pointer
to `uintptr` directly in the method call, as above, and keep the pointed-to
object alive until the export returns. CGO-free Darwin and Linux callers can
create C-callable Go callbacks with `purego.NewCallback`; Windows callers can
use `syscall.NewCallback`.

You can also load from a path:

```go
lib, err := reflektor.LoadLibraryFile("./payload.dylib")
```

To read and map a library's non-system dependencies through Reflektor as well,
use recursive mode:

```go
lib, err := reflektor.LoadLibraryFileRecursive("./payload.dylib")
```

The byte-oriented equivalent resolves relative dependency names from the
current working directory:

```go
lib, err := reflektor.LoadLibraryRecursive(payload)
```

Both recursive APIs read the complete custom dependency graph before the root
export is invoked. `LoadLibraryFileRecursive` is preferred for libraries that
use origin-relative names such as `$ORIGIN`, `@loader_path`, or `@rpath`.

## Beacon Object Files

BOF loading is provided by a separate, tag-free package:

```go
import "github.com/sliverarmory/reflektor/bof"
```

No CGO or custom build tags are required. The package boundary keeps the BOF
parser, relocator, native callback bridge, and Beacon compatibility layer out
of shared-library-only consumers. BOF-only consumers can likewise import this
subpackage without pulling in the root package's `memmod` shared-library
backend.

`bof.Load` accepts the native relocatable-object convention used by each host.
Linux/ARMv7 hard-float (`GOARM=7`), Linux/ppc64le, and Linux/riscv64 also have
full root, `native`, recursive, C, Rust, and Go `c-shared` test coverage. ARMv5,
ARMv6, soft-float, and Thumb-compiled BOFs are not included in the ARM support
claim. PowerPC64 objects must use the little-endian ELFv2 ABI; ELFv1,
big-endian, NOTOC/P9NOTOC calls, and external tail branches are rejected.

| Host | BOF object format | Entry ABI |
| --- | --- | --- |
| Windows `386`, `amd64`, `arm64` | COFF (`.o`) | `go(char *, int32)` using the Windows ABI |
| Linux `386`, `amd64`, `arm64`, `ppc64le`, `riscv64`; ARMv7 hard-float (`GOARM=7`, ARM-state objects) | ELF `ET_REL` (`.o`) | `go(char *, int32)` using the host ABI |
| Darwin `amd64`, `arm64` | Mach-O `MH_OBJECT` (`.o`); legacy ELF `ET_REL` accepted for compatibility | `go(char *, int32)` using the host ABI |

Compile native Darwin objects with Zig's `x86_64-macos-none` or
`aarch64-macos-none` targets while using only host-compatible types and
imported Beacon/system functions. The native arm64 target follows Apple's x18
platform-register reservation automatically. The earlier constrained ELF
Darwin interchange format remains accepted for backwards compatibility; those
Linux-targeted arm64 objects must explicitly reserve x18.
Windows COFF machine code is not portable to Linux or Darwin.
The ARMv7 hard-float, ppc64le, and riscv64 rows run BOFs without CGO under QEMU,
including generated fixtures, load options, repeated and concurrent execution,
close behavior, and the portable external BOF corpus. The same target images
exercise C and Rust shared libraries through root, `native`, recursive, and CLI
lifecycles with both CGO modes. Go `c-shared` exercises the cgo-enabled root,
recursive, and CLI lifecycles, while `native` proves its intentional Go-image
rejection. ARM uses a `linux/arm/v7` image; ppc64le and riscv64 use native Debian
images with checksummed target Go toolchains under user-mode emulation.

```go
var arguments bof.Arguments
_ = arguments.AddString("example")

loaded, err := bof.Load(objectBytes)
if err != nil {
    return err
}
defer loaded.Close()

records, err := loaded.Execute(arguments.Bytes())
```

Use `bof.LoadWithOptions` (or `bof.LoadFileWithOptions`) when the host needs an
exact entry symbol or an import boundary. `ValidateImports` receives a sorted,
owned snapshot before image allocation, callback registration, or dynamic
library lookup. `ResolveSymbol` can then supply stable native function or data
addresses using the platform ABI for custom imports. Reflektor's built-in
callbacks always take precedence and
cannot be replaced through the custom resolver.

External data must use the target's native indirection convention when the
object format requires it. Windows data declarations should use
`__declspec(dllimport)` so COFF emits an import-pointer reference rather than a
range-limited direct reference.

Unsupported Beacon integration APIs—including token, temporary-process, and
process-injection callbacks—are marked `RequiresHost` and fail explicitly
unless `ResolveSymbol` supplies them; they are never silent stubs or system
symbol lookups. The built-in compatibility set also includes the bounded
`BeaconDataExtractOrNull` helper and `toWideChar`, whose destination is always
UTF-16LE and whose maximum length is measured in bytes.

The loader supplies the Beacon data, format, and output callbacks, preserves
each output record's channel, uses page-level W^X protections, and serializes
execution around the process-wide native callback bridge. A BOF is still
arbitrary native code in the current process: malformed code can corrupt or
terminate the host, so untrusted objects need a subprocess boundary.
Object images are capped at 64 MiB and packed argument buffers at 16 MiB.
Callback capture is synchronous: a BOF that starts native worker threads must
join them before its entry point returns.

`BeaconPrintf` and `BeaconFormatPrintf` accept at most ten machine-word
arguments and implement bounded string, character, integer, and pointer
conversions. Floating-point conversions are rejected on 64-bit Unix because
its variadic ABI does not expose those values to the fixed callback bridge.
Native Mach-O Darwin/arm64 objects must use `BeaconOutput` instead: imports of
`BeaconPrintf` and `BeaconFormatPrintf` are rejected because Apple's variadic
ABI is incompatible with the fixed callback bridge. Legacy Darwin ELF objects
retain the existing callback behavior.

## Native-only Package

Hosts that load only native C or Rust extensions can use the tag-free
`github.com/sliverarmory/reflektor/native` package:

```go
import "github.com/sliverarmory/reflektor/native"

lib, err := native.LoadLibrary(payload)
```

It exposes the same `CallExport`, `CallExportWithArgs`, and `Close` lifecycle
for byte-backed native images. On Linux, its import graph deliberately excludes
the root loader's Go c-shared TLS reservation. Linux `amd64`, `arm64`,
`ppc64le`, and `riscv64` use the PureGo call bridge in both CGO modes; ARMv7
uses its hard-float bridge, while Linux `386` uses Reflektor's integer-only
`runtime.cgocall` dispatcher. Valid Go c-shared payloads are rejected before
mapping with `native.ErrGoSharedLibraryUnsupported`; use the cgo-enabled root
`reflektor` package when Go c-shared loading is required. File and recursive
loading remain root-package features.

## CLI

The CLI is in `reflektor/cli` and uses Cobra.

Build:

```bash
go build -o reflektor ./cli
```

Usage:

```bash
./reflektor <shared-library-path> [--call-export StartW]
```

`--call-export` defaults to `StartW`.

## Behavior Notes

- `CallExport` preserves the original zero-argument API. `CallExportWithArgs`
  accepts zero through three `uintptr` arguments and returns the platform's
  primary machine-word return value.
- `CallExportWithArgs` supports native C and Rust images. Go c-shared images
  return `ErrGoExportArgumentsUnsupported`; their zero-argument exports remain
  available through `CallExport`.
- On Linux, the runtime-aware foreign-call bridges keep C-to-Go callbacks safe.
  Consequently, `CGO_ENABLED=0` hosts importing either Reflektor package are
  dynamically linked against the platform's glibc loader; this is not a fully
  static or musl-portable build mode.
- Reflektor normalizes common symbol naming differences where possible (for example underscore-prefixed forms).
- The root `reflektor.Library` API remains intentionally small:
  `CallExport()`, `CallExportWithArgs()`, and `Close()`.
- Recursive mode maps file-backed application dependencies from their bytes and
  resolves imports within the in-memory graph. Platform runtime libraries remain
  delegated to the native loader: Darwin shared-cache libraries, Windows
  System32/API-set libraries, and Linux libraries in trusted system roots. Those
  libraries require OS-managed TLS, symbol versioning, loader registration, and
  other facilities that cannot be reproduced by simply reading a file—and some
  Darwin shared-cache images do not exist as standalone readable files.
- Linux custom images reject general ELF TLS, IFUNC/IRELATIVE, and RELR with
  explicit errors; those features remain available through the system-library
  carveout. Windows custom dependency cycles and delay-load import tables are
  also rejected explicitly. Darwin and Linux graph cycles are deduplicated.
- After the first export call, Darwin recursive mappings remain process-resident
  because dyld retains their loader records. Reusing a Darwin install-name in a
  later load follows dyld's first-loaded identity semantics. Calls made through
  the same `Library` reuse one mapped root, so an initializer and later
  argument-bearing exports share module state.
- `LoadLibrary` and `LoadLibraryFile` retain their original behavior and API.

## Test Data And Validation

C test shared libraries are generated from:

- `reflektor/testdata/c/args.c`
- `reflektor/testdata/c/basic.c`
- `reflektor/testdata/c/native_lifecycle.c`
- `reflektor/testdata/c/recursive_leaf.c`
- `reflektor/testdata/c/recursive_middle.c`
- `reflektor/testdata/c/recursive_root.c`

The recursive C fixture is a transitive root -> middle -> leaf graph. Its test
checks that the graph is absent from Linux `/proc/self/maps` or the Windows
loader module registry, then renames the dependency directory before calling
`StartW`. On Darwin the rename happens before the lazy dyld transaction. These
checks prove the custom dependencies came from bytes captured by Reflektor.

The Rust fixtures are built from `reflektor/testdata/rust` and
`reflektor/testdata/rust/native_args.rs`. The HTTPS fixture exports `StartW`,
performs a bounded `GET https://example.com/` through libcurl on Darwin/Linux or
WinHTTP on Windows, and records `ok:200` after receiving a non-empty successful
response. Both fixtures are dependency-free Rust (`no_std`) so they do not
require unsupported thread-local runtime state from the in-memory loaders.

The generated Linux Go `c-shared` fixture also resolves the current user
through libc NSS before starting its scheduler work. This exercises native
loader selection for system GNU IFUNC symbols (including RISC-V `memcpy`) in
both the legacy and recursive load paths.

Build test shared libraries for the full matrix:

```bash
./testdata/build_c_shared_libs.sh
```

Run tests:

```bash
go test ./...
```

The Rust fixture test requires Cargo with Rust 1.94.0 and outbound HTTPS access. Linux also requires the libcurl development package so the fixture can link against the system TLS client.

Linux cross-architecture Docker/QEMU harnesses:

- Shared-library matrix: `testdata/docker/linux-memmod.Dockerfile` and
  `testdata/docker/run-linux-memmod-matrix.sh`
- ARMv7 BOFs and shared libraries: `testdata/docker/linux-bof.Dockerfile` and
  `testdata/docker/run-linux-bof-tests.sh`
- RISC-V BOFs and shared libraries: `testdata/docker/linux-riscv64-bof.Dockerfile` and
  `testdata/docker/run-linux-riscv64-bof-tests.sh`
- PPC64LE BOFs and shared libraries: `testdata/docker/linux-ppc64le-bof.Dockerfile` and
  `testdata/docker/run-linux-ppc64le-bof-tests.sh`

The emulated runners reject every unexpected skip and require named passes for
the C, Rust, Go `c-shared`, legacy byte, file, recursive dependency, native,
CLI, BOF fixture, and 25-object BOF corpus lifecycles. The Sliver E2E workflow
also builds and runs a real shared implant and native extension for each target.

## Repository Layout

- `reflektor/reflektor.go`: root importable package (`reflektor`).
- `reflektor/bof`: public, tag-free BOF API and argument encoder.
- `reflektor/internal/bofloader`: internal COFF/ELF/Mach-O loader and Beacon bridge.
- `reflektor/native`: tag-free native C/Rust-only package.
- `reflektor/memmod`: OS-specific loader backends.
- `reflektor/cli`: CLI entrypoint.
- `reflektor/integration`: external-package loader and native API tests.
- `reflektor/testdata`: portable shared-library fixtures and build/test harnesses.
