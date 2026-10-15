# OpenBSD amd64 feasibility

OpenBSD `amd64` is currently unsupported for BOFs, root shared libraries,
`native` shared libraries, and recursive shared libraries. The support
manifest intentionally remains unchanged. Go `c-shared` is `n-a` on this
target in the pinned Go toolchain, so future library acceptance covers C and
Rust images.

The executable-memory probe ran in the cross-platform-actions OpenBSD 7.9
`x86-64` QEMU image (asset SHA-256
`ad05bb9cdf8fb8edf0514ff4c30b124bf0067d20be6d14c49ebf89bbbc55a9c0`).
It verified that:

- `dlopen(NULL)` and `dlsym(..., "puts")` work;
- a private anonymous mapping can be written, changed from RW to RX, and
  executed without a simultaneous writable/executable page;
- an RWX mapping is denied; and
- a pledged process must retain OpenBSD's `prot_exec` promise to make an RX
  mapping. The Go host must arrange this before it drops that promise.

The first implementation blocker is the pinned
`github.com/ebitengine/purego v0.11.0` dependency. On Go 1.26.6, the minimal
[`Dlopen`/`SyscallN`/`NewCallback` probe](ffi_probe/main.go) fails to
cross-build for `GOOS=openbsd GOARCH=amd64 CGO_ENABLED=0` inside PureGo's
`struct_amd64.go` with `undefined: maxRegAllocStructSize`, `undefined:
syscallArgs`, and related missing ABI definitions. Upstream tracks OpenBSD
support in [issue #454](https://github.com/ebitengine/purego/issues/454).
Reflektor's BOF callback and invocation paths and its shared-library call and
dynamic-symbol paths all use this dependency. Adding OpenBSD build tags alone
would leave those paths unbuildable.

To repeat the two probes:

```sh
# On a host with the pinned Go toolchain:
bash testdata/openbsd/check-purego-bridge.sh

# Inside an OpenBSD/amd64 guest with the checkout synchronized:
sh testdata/openbsd/run-feasibility.sh
```

The CI feasibility job repeats both probes. Its PureGo step accepts only a
successful build or the known missing-ABI failure. An unrelated build error
fails the job.

## Full support gate

After an OpenBSD foreign-call/callback bridge is available and maintained,
port the BOF executable-memory, thread identity, system-symbol, callback,
and invocation paths. Then port ELF root and `native` dynamic loading,
runtime module discovery, and recursive dependency search with OpenBSD rtld
behavior. Preserve W^X and document the `prot_exec` requirement for hosts
that use `pledge`.

Only change the manifest after a real OpenBSD guest or native runner passes
generated BOFs, all 25 pinned corpus objects, C and Rust root/`native`/recursive
fixtures in both CGO modes, callbacks, lifecycle, dependency search, the CLI,
and the platform-support test without skips. OpenBSD `amd64` needs its own CI
runtime job as part of that gate.
