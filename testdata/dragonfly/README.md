# DragonFly amd64 feasibility probe

DragonFly/amd64 remains `unsupported` in `platform-support.json`. This probe
records target-native prerequisites and the current blockers. It is not a BOF
or shared-library acceptance test.

## Reproduce the guest

The run on 2026-10-15 used the official DragonFly 6.4.2 live ISO and QEMU
11.1.2 with x86_64 TCG on an arm64 Mac. The [release mirror](https://avalon.dragonflybsd.org/iso-images/)
publishes `dfly-x86_64-6.4.2_REL.iso.bz2` with MD5
`906095312c4a4ac0577fb91d5eb87033`; its decompressed ISO has MD5
`fdab09ac37cf427b8c01de5d54bfd1d7`. The image boots to a root login
without a password. This QEMU command uses the same virtual machine settings
as the probe run, with a visible console for manual login:

```sh
qemu-system-x86_64 -machine q35,accel=tcg -cpu qemu64 -smp 2 -m 2048 \
  -cdrom dfly-x86_64-6.4.2_REL.iso -boot d -display cocoa \
  -netdev user,id=net0 -device e1000,netdev=net0
```

Log in as `root`, run `dhclient em0`, then transfer a checkout into the live
guest. For example, `git archive HEAD | gzip -1 > /tmp/reflektor.tar.gz` on
the host, serve `/tmp` with `python3 -m http.server 8000 --bind 127.0.0.1`,
and run this in the guest:

```sh
fetch -o /tmp/reflektor.tar.gz http://10.0.2.2:8000/reflektor.tar.gz
mkdir -p /tmp/reflektor
tar -C /tmp/reflektor -xzf /tmp/reflektor.tar.gz
cd /tmp/reflektor
sh testdata/dragonfly/run-probe.sh
```

The native probe builds a DragonFly C shared library and calls it through
`dlopen`/`dlsym`/`dlclose`, changes an anonymous mapping from RW to RX and
executes it, then checks distinct `lwp_gettid` values on two native threads.
The 6.4.2 guest's `cc 8.3 [DragonFly]` produced:

```text
dragonfly probe: executable memory=42 shared export=77 entry thread=1 worker thread=2
```

## Go and Reflektor boundary

The [official Go 1.26.6 DragonFly archive](https://dl.google.com/go/go1.26.6.dragonfly-amd64.tar.gz)
has SHA256 `7ed0537a740803fb3c6979fe45ad757b48d8899aa50589345eafcca41c294e37`
in Go's download metadata. It ran natively as `go version go1.26.6
dragonfly/amd64` in this guest. The live image mounts `/` read-only and has
separate 487 MiB tmpfs mounts at `/root` and `/tmp`; extract the Go archive
under `/root`, keep source and caches under `/tmp`, and set `GOPATH=/tmp/gopath`
to avoid Go's default `/root/go` path colliding with `GOROOT`.

With the repo at `/tmp/reflektor` and Go at `/root/go`, the following commands
reproduce the API and dependency checks:

```sh
cd /tmp/reflektor
export GOPATH=/tmp/gopath GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache
export GOTOOLCHAIN=local CGO_ENABLED=0
cc -c -O1 -fPIC -fno-stack-protector -o /tmp/dragonfly_fixture.o testdata/bof/fixture.c
/root/go/bin/go run testdata/dragonfly/api_probe.go \
  /tmp/dragonfly_fixture.o /tmp/reflektor-dragonfly-probe/libprobe.so
/root/go/bin/go build -o /tmp/purego-probe testdata/dragonfly/purego_compile_probe.go
CGO_ENABLED=1 /root/go/bin/go build -buildmode=c-shared \
  -o /tmp/dragonfly-basic.so ./testdata/go/basic
```

The BOF object built as `ELF 64-bit LSB relocatable, x86-64`, and the C
library built as a DragonFly ELF shared object. The API probe reported:

```text
bof.Load: reflektor: load BOF: bofloader: ELF BOFs are unsupported on dragonfly
reflektor.LoadLibrary: reflektor: load library: memmod shared-library loader is unsupported on dragonfly/amd64
native.LoadLibrary: reflektor/native: load library: unsupported platform dragonfly/amd64
```

The pinned `github.com/ebitengine/purego v0.11.0` does not supply a DragonFly
foreign-call and callback bridge. Building `purego_compile_probe.go` failed
inside PureGo with undefined `maxRegAllocStructSize`, `syscallArgs`, and other
ABI symbols. The official Go toolchain also rejected
`go build -buildmode=c-shared` with `-buildmode=c-shared not supported on
dragonfly/amd64`, matching the manifest's `n-a` entry. `go test ./bof` passed
with the unsupported loader; `go test ./internal/bofloader` failed where tests
assumed thread identity, callbacks, and executable memory were available.
The integration test package cannot currently compile on DragonFly because
`newBOFOptionsTestCallback` has no implementation for this target.

Before enabling any DragonFly support claim, supply and validate a PureGo
DragonFly amd64 bridge (including no-CGO callbacks), add BOF and all shared
loader platform paths, then run the full target-native BOF, C/Rust library,
recursive, CLI, and corpus gates described in the support roadmap. The
read-only live image has no Rust compiler; a writable guest or prepared Rust
toolchain is needed for that gate.
