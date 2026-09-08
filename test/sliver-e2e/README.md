# Sliver end-to-end overlay

This harness tests the current Reflektor checkout inside a real Sliver build.
`overlay.sh prepare` temporarily replaces Reflektor in Sliver's embedded implant
module and runs Sliver's official `go generate ./implant`. It keeps the resolved
module graph and checksums produced by Sliver's vendor generator, removes only
the temporary local replacement, verifies the vendor metadata, and compares the
vendored `native` and `memmod` source byte-for-byte. The generated Sliver server
therefore embeds the current Reflektor source and its selected dependencies
rather than the versions previously committed to Sliver's vendor directory.

The GitHub workflow accepts only an immutable 40-hex Sliver commit. Automatic
push and pull request runs use the signed merge commit for
BishopFox/sliver#2336,
`b428274515a9274aa47d940035d24087df1d564a`; manual runs may supply another
immutable commit. The workflow never depends on a feature branch name.

The pinned Sliver commit predates compiler-matrix entries for Linux ARMv7,
ppc64le, and riscv64. During `prepare`, `overlay.sh` verifies and applies the
checked-in `sliver-new-linux-targets.patch`. That narrow patch adds the three
server and client generator/compiler targets, their Zig triples, the integration
driver whitelist, and the production client native-extension console filters.
It also selects the existing Unix and Linux native-extension implementations
while excluding the unsupported Linux implementation on those architectures,
and supplies the generated RISC-V C integer aliases omitted from Sliver's
vendored PTY package so its shared implant can compile.
Preparation fails if the patch no longer applies exactly; verification checks
every compiler map, client filter, driver entry, and mutually exclusive source
constraint. This is test-only source adaptation and does not claim those targets
are supported by the unpatched pinned Sliver revision.

Each native runner builds the current root-package CLI and passes it to Sliver's
integration driver with `-reflektor`. Darwin and Linux build that CLI with cgo
enabled because it loads Sliver's Go c-shared implant. Windows remains cgo-free.
Linux/386 performs the same flow under Docker/QEMU and builds the CLI with the
container's native i386 GCC toolchain.
Linux ARMv7 hard-float, ppc64le, and riscv64 use
`Dockerfile.linux-emulated`. The driver and cgo-enabled Reflektor CLI are target
binaries executed under QEMU, while the static Sliver server remains
linux/amd64 so its embedded Go and Zig build assets remain self-contained and
native to the Actions host. The ARM runtime explicitly exports
`GOARM=7,hardfloat` so Sliver's inherited implant build environment cannot drift
to a different ARM ABI. The generated target shared implant is loaded by
the target CLI, connects a real session, and then loads, initializes, calls,
and lists a target-native C extension.

FreeBSD is intentionally absent from this Sliver harness. At the pinned Sliver
commit, the exact server build used here fails for both `freebsd/amd64` and
`freebsd/arm64`: `implant/sliver/transports/wireguard/wireguard_generic.go`
references undefined `net`, `device`, and `errors` names, and the server has no
FreeBSD `assetsFs` binding. The integration driver independently lacks its
FreeBSD process helpers. Reflektor's standalone FreeBSD QEMU jobs still execute
the complete BOF and shared-library lifecycle; the Sliver layer can be added
after upstream Sliver itself compiles for those targets.
