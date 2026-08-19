# Sliver end-to-end overlay

This harness tests the current Reflektor checkout inside a real Sliver build.
`overlay.sh prepare` temporarily replaces Reflektor in Sliver's embedded implant
module, runs Sliver's official `go generate ./implant`, restores `implant/go-mod`
without a local replacement, and verifies the vendored `native` and `memmod`
source byte-for-byte. The generated Sliver server therefore embeds the current
Reflektor source rather than the version previously committed to Sliver's
vendor directory.

The GitHub workflow accepts only an immutable 40-hex Sliver commit. Automatic
push and pull request runs use the signed native-package bootstrap commit
`ada79c8ecdad89b495b2d9e840551abd01b80ca0`; manual runs may supply another
immutable commit. Once BishopFox/sliver#2336 is merged, the fallback should be
replaced with its immutable merge commit. The workflow never depends on a
feature branch name.

Each native runner builds the current root-package CLI and passes it to Sliver's
integration driver with `-reflektor`. Darwin and Linux build that CLI with cgo
enabled because it loads Sliver's Go c-shared implant. Windows remains cgo-free.
Linux/386 performs the same flow under Docker/QEMU and builds the CLI with the
container's native i386 GCC toolchain.
