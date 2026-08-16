# Reflektor

Reflektor is a Go library and CLI for loading shared libraries from bytes and invoking exported functions.

It exposes a stable root package (`reflektor`) so other projects can import it directly, while platform-specific loading is handled behind `memmod`.

## Platform Support

| OS | Architectures | Shared Library Format | Status | Loader Notes |
| --- | --- | --- | --- | --- |
| Windows | `386`, `amd64`, `arm64` | PE (`.dll`) | Supported | In-memory PE loader |
| Darwin | `amd64`, `arm64` | Mach-O (`.dylib`, bundle) | Supported | Dyld4 root-image loader with system dependencies registered through public dyld; supports no-cgo builds and avoids temp-file legacy NS APIs. |
| Linux | `386`, `amd64`, `arm64` | ELF (`.so`) | Supported | Pure Go in-memory ELF loader (maps PT_LOAD segments, applies relocations, resolves externals from runtime modules/`dlsym`); no `memfd`, no `/dev/shm`, no temp-file disk writes. |
| Other | - | - | Unsupported | Returns an explicit unsupported-platform error. |

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

You can also load from a path:

```go
lib, err := reflektor.LoadLibraryFile("./payload.dylib")
```

## CLI

The CLI is in `/Users/moloch/git/reflektor/cli` and uses Cobra.

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

- `CallExport` is designed for zero-argument exports.
- Reflektor normalizes common symbol naming differences where possible (for example underscore-prefixed forms).
- The root `reflektor.Library` interface is intentionally small: `CallExport()` and `Close()`.

## Test Data And Validation

C test shared libraries are generated from:

- `/Users/moloch/git/reflektor/testdata/c/basic.c`

The Rust HTTPS fixture is built from `/Users/moloch/git/reflektor/testdata/rust`. It exports `StartW`, performs a bounded `GET https://example.com/` through libcurl on Darwin/Linux or WinHTTP on Windows, and records `ok:200` after receiving a non-empty successful response. The fixture is dependency-free Rust (`no_std`) so it does not require unsupported thread-local runtime state from the in-memory loaders.

Build test shared libraries for the full matrix:

```bash
./testdata/build_c_shared_libs.sh
```

Run tests:

```bash
go test ./...
```

The Rust fixture test requires Cargo with Rust 1.94.0 and outbound HTTPS access. Linux also requires the libcurl development package so the fixture can link against the system TLS client.

Linux cross-arch Docker harness:

- `/Users/moloch/git/reflektor/testdata/docker/linux-memmod.Dockerfile`
- `/Users/moloch/git/reflektor/testdata/docker/run-linux-memmod-matrix.sh`

## Repository Layout

- `/Users/moloch/git/reflektor/reflektor.go`: root importable package (`reflektor`).
- `/Users/moloch/git/reflektor/memmod`: OS-specific loader backends.
- `/Users/moloch/git/reflektor/cli`: CLI entrypoint.
- `/Users/moloch/git/reflektor/testdata`: portable shared-library fixtures and build/test harnesses.
