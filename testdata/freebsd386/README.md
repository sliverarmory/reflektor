# FreeBSD/i386 feasibility probe

FreeBSD/i386 remains **unsupported** in `platform-support.json`. This probe
tests one prerequisite for a CGO-free port: PureGo v0.11.0 foreign calls,
callbacks, and native-thread startup with Go 1.26.6. It does not run the BOF
loader or any of the root, native, or recursive shared-library loaders.

## Build the diagnostic binary

Use the exact Go version in `go.mod` and a writable Go build cache. `GO` names
the Go 1.26.6 executable in the commands below.

```sh
GO=/path/to/go1.26.6/bin/go
export GOCACHE="${TMPDIR:-/tmp}/reflektor-freebsd386-go-cache"

# The unmodified PureGo v0.11.0 module does not compile for this target.
GOOS=freebsd GOARCH=386 CGO_ENABLED=0 GOTOOLCHAIN=local \
  "$GO" build \
  -gcflags=github.com/ebitengine/purego/internal/fakecgo=-std \
  -o /tmp/probe-unmodified ./testdata/freebsd386

# Cross-build with four build-tag changes in a disposable PureGo copy.
GO="$GO" sh testdata/freebsd386/build-probe.sh /tmp/freebsd386-probe-build
```

The unmodified build failed with:

```text
# github.com/ebitengine/purego/internal/fakecgo
.../callbacks.go:88:28: undefined: threadentry
.../callbacks.go:89:28: undefined: x_cgo_init
.../go_util.go:37:2: undefined: _cgo_sys_thread_start
```

`build-probe.sh` widens the build tags on PureGo's `sys_386.s`,
`sys_unix_386.s`, `zcallback_386.s`, and `internal/fakecgo/go_freebsd.go` in
that disposable copy. It uses a temporary module replacement and does not
change this repository's `go.mod` or support manifest. Those tag changes make
the probe compile; they are not a validated FreeBSD/i386 PureGo port.

## Run in a FreeBSD 14.4 i386 guest

The observed guest used the [official FreeBSD 14.4 i386 UFS QCOW2
image](https://download.freebsd.org/releases/VM-IMAGES/14.4-RELEASE/i386/Latest/)
with SHA-256
`fc58258a8f49689a59b9e3f954e7fbcc0391ba797bfc25aebb7704581ad7e638`.
The corresponding checksum is in the official `CHECKSUM.SHA256` file at that
URL. The guest ran a binary cross-built with Go 1.26.6; no Go toolchain was
installed in the guest.

One reproducible QEMU setup (from a host with `qemu-system-i386`,
`qemu-img`, and `xz`) is:

```sh
mkdir -p /tmp/freebsd386-guest/share
cd /tmp/freebsd386-guest
curl -fL -O https://download.freebsd.org/releases/VM-IMAGES/14.4-RELEASE/i386/Latest/FreeBSD-14.4-RELEASE-i386-ufs.qcow2.xz
shasum -a 256 FreeBSD-14.4-RELEASE-i386-ufs.qcow2.xz
xz -dk FreeBSD-14.4-RELEASE-i386-ufs.qcow2.xz
qemu-img create -f qcow2 -b "$PWD/FreeBSD-14.4-RELEASE-i386-ufs.qcow2" \
  -F qcow2 overlay.qcow2
cp /tmp/freebsd386-probe-build/probe-patched share/
qemu-system-i386 -machine pc,accel=tcg -cpu max -smp 2 -m 2048 \
  -display curses -monitor none -serial null \
  -drive file="$PWD/overlay.qcow2",format=qcow2,if=ide,index=0 \
  -drive file="fat:rw:$PWD/share",format=raw,if=ide,index=1,media=disk \
  -boot order=c -nic user,model=e1000 -no-reboot
```

At the VGA console, log in as `root` (no password in this release image),
then run:

```sh
freebsd-version
uname -m
mount_msdosfs /dev/ada1s1 /mnt
ldd /mnt/probe-patched
/mnt/probe-patched
env LD_PRELOAD=/lib/libthr.so.3 /mnt/probe-patched
echo $?
```

The 2026-10-15 run showed `14.4-RELEASE` and `i386`. `file` on the host
identified `probe-patched` as an ELF32 FreeBSD executable with
`/usr/libexec/ld-elf.so.1` interpreter. `objdump -p probe-patched` showed a
dynamic section with no `NEEDED` entry; `ldd /mnt/probe-patched` listed only
`/mnt/probe-patched:`. The guest has `/lib/libthr.so.3`, but direct execution
stopped before `main`:

```text
ld-elf.so.1: /mnt/probe-patched: Undefined symbol "pthread_attr_init"
```

Preloading `libthr` allowed the probe to start. Its observed output was:

```text
target=freebsd/386 go=go1.26.6 cgo-free probe
foreign-call=getpid:939
same-thread-callback=42
Segmentation fault (core dumped)
$ echo $?
139
```

The PID varies by run. The `pthread_create`/`pthread_join` native-thread
callback did not complete. The crash location has not been isolated, and the
binary's missing `libthr` dependency also needs a proper fix. Until both
issues are resolved and the BOF and all shared-library runtime gates pass,
FreeBSD/i386 must remain unsupported, including `bof_cgo_free` and
`shared_cgo_free`.
