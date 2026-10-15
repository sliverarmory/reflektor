# Reflektor roadmap

Reflektor loads native BOFs and shared libraries from bytes. This roadmap prioritizes broader runtime support, library compatibility, and safer host integration. Milestones are ordered by current priority and risk, without date commitments; the platform tracks can proceed in parallel after shared foundation work. The [capability manifest](platform-support.json) records implemented support; proposed targets stay `unsupported` until runtime tests pass.

## Current baseline

At `e36aba4`, BOF execution and the root, `native`, and recursive shared-library loaders have runtime coverage on 13 of 45 non-Wasm Go targets: Windows `386`, `amd64`, and `arm64`; Darwin `amd64` and `arm64`; FreeBSD `amd64` and `arm64`; and Linux `386`, `amd64`, ARMv7 hard-float, `arm64`, `ppc64le`, and `riscv64`. Five targets run through emulated CI. The other 32 targets remain unsupported on all four surfaces.

Go `c-shared` is a separate capability. A target can support BOFs and C/Rust libraries while the pinned Go toolchain marks `c-shared` as `n-a`. The manifest tracks this distinction per target.

## Support gate for a new platform

Before claiming both BOF and shared-library support for a new platform:

- [ ] Build the BOF and C/Rust fixtures for the exact OS, architecture, and ABI variant.
- [ ] Execute BOF load, imports and callbacks, repeated calls, concurrency, close behavior, and the applicable pinned corpus on the target. Record the expected corpus count and investigate every omission.
- [ ] Execute C and Rust libraries through root and `native`; test transitive dependencies through the root recursive API; and run the CLI's current file-load and export-call path. Cover unload behavior in each applicable API.
- [ ] Reject malformed, wrong-architecture, and unsupported-feature objects and libraries explicitly. Verify executable-memory protection transitions and cleanup on the target.
- [ ] Test both CGO modes where the surface claims them. Test Go `c-shared` separately when the pinned Go toolchain provides that build mode; otherwise retain `n-a`.
- [ ] Run named target tests without unexpected skips on a native or emulated runner. A cross-build alone does not establish runtime support.
- [ ] Update the fixture target lists, [manifest](platform-support.json), [README](README.md), and CI together. Keep the manifest consistency test passing.

Record partial progress by surface in the manifest; do not claim platform parity until every intended surface passes. The [platform support test](integration/platform_support_test.go) checks the manifest against Go's non-Wasm target list, the fixture matrices, build-mode availability, and runner status. The [FreeBSD runtime harness](testdata/freebsd/run-freebsd-tests.sh) is a model for BOF, library, and CLI acceptance. Sliver end-to-end coverage is a separate integration gate; the [Sliver overlay](test/sliver-e2e/README.md) documents the FreeBSD blocker at its pinned upstream revision.

## Milestone 0: library lifecycle and dependency bytes

- [x] Make root `Library.Close` mark the handle closed and release its lock before calling native unload code. Document when concurrent or reentrant `Close` calls return relative to the first unload; test destructor reentry, concurrent call/close, and repeated close.
- [x] Add an opt-in public resolver for recursive dependency bytes, using the existing internal dependency request and response model. Preserve the current file APIs and system-library delegation.
- [x] Prove a root → middle → leaf graph can load from caller-supplied bytes without dependency files. Cover origin-relative names, missing dependencies, cycles and diamonds, size limits, and deterministic resolution. The public graph tests exercise Linux `amd64`; the existing backend tests cover Darwin cycle and diamond planning and Windows cycle rejection.

This milestone changes no platform status. It gives new ports a stable dependency boundary and removes a lifecycle risk before expanding native unload paths.

## Milestone 1: NetBSD amd64

NetBSD `amd64` is the first new OS target. Existing ELF/amd64 relocations and the pinned PureGo dependency provide reusable call and callback pieces, but Reflektor has no NetBSD loader implementation or runtime proof. Go `c-shared` is `n-a` for this target, so the library scope is C and Rust.

- [ ] Prove the pinned toolchains can build the target fixtures and identify an executable NetBSD `amd64` CI runner.
- [ ] Add NetBSD BOF executable-memory, thread identity, system-symbol, callback, and invocation paths.
- [ ] Add NetBSD ELF root and `native` loaders, runtime symbol resolution, system-library boundaries, and recursive dependency search.
- [ ] Run the full support gate in a NetBSD `amd64` guest or native runner before changing the manifest.

## Milestone 2: Linux loong64

Linux `loong64` extends the existing Linux family to a new instruction set. The pinned PureGo dependency has LoongArch foreign-call support, but Reflektor needs architecture-specific BOF and shared-library code. Unlike NetBSD, the manifest records Go `c-shared` as toolchain-available but currently unsupported by Reflektor.

- [ ] Prove the pinned toolchains can build target BOF and C/Rust/Go fixtures and identify an executable `loong64` CI runner.
- [ ] Implement ELF machine and ABI validation, BOF and shared-library relocations, import thunks, invocation, and instruction-cache handling.
- [ ] Run BOF and C/Rust library coverage under an exact-architecture runner, then prove Go `c-shared` as a separate runtime gate.
- [ ] Extend the corpus, fixtures, manifest, and CI without weakening existing platform checks.

## Milestone 3: FreeBSD 386 feasibility, then implementation

FreeBSD `386` can reuse ELF/x86 object handling, but it is not a build-tag-only port. PureGo v0.11.0's 386 foreign-call and callback assembly is Linux-only, and its FreeBSD CGO-free startup covers `amd64` and `arm64`. Reflektor's 386 call bridges are also Linux-only. Go `c-shared` is `n-a` here.

- [ ] Prove a FreeBSD 32-bit foreign call, Go callback, thread startup, and CGO-free invocation in a small guest fixture. Resolve the PureGo dependency gap upstream or with a maintained local bridge.
- [ ] If the bridge passes, port BOF memory, thread, symbol, and invocation paths and the FreeBSD ELF root, `native`, and recursive loaders.
- [ ] Apply the full support gate in a FreeBSD `386` runner before recording C/Rust library and BOF support.

If the bridge cannot be maintained safely, leave this target unsupported and move to the next platform candidate.

## Milestone 4: Android arm64 feasibility

Android is deliberately excluded from the Linux loader build paths. Before committing to a full port, prove executable mappings, native callbacks, Bionic symbol resolution, and recursive system dependencies inside an Android app on an emulator and a device. Android linker namespaces and executable-code policy must be part of that proof. See the [Android linker documentation](https://source.android.com/docs/core/architecture/vndk/linker-namespace) and [Android 10 behavior changes](https://developer.android.com/about/versions/10/behavior-changes-10).

- [ ] Establish the permitted app packaging, API level, code provenance, and runtime environment.
- [ ] Run a minimal BOF plus C/Rust library and callback fixture within that environment.
- [ ] If feasible, implement the full BOF, root, `native`, recursive, and applicable Go `c-shared` gates before changing the manifest.

## Library compatibility

These projects can proceed alongside platform milestones. For each feature, define support or explicit rejection across root single-image, `native`, and recursive paths, then verify that behavior with real C/Rust fixtures on every affected target. A recursive-only fix does not establish single-image support.

1. **ELF packed RELR:** implement and test packed relocations in custom recursive dependencies.
2. **ELF general TLS:** define storage, thread lifetime, initialization, and unload semantics before accepting `PT_TLS` or TLS symbols.
3. **ELF IFUNC and IRELATIVE:** define safe resolver timing and reentry behavior, then test constructors, dependencies, and unload.
4. **Windows recursive imports:** add delay-load imports and dependency cycles as separate changes, with fixtures for binding, forwarders, initialization order, and unload.

System runtime libraries remain delegated to the OS loader. Successful support for one feature does not imply support for the others.

## BOF host integration and isolation

- [ ] Add a typed, opt-in host adapter over `ValidateImports` and `ResolveSymbol` for Beacon APIs requiring host services, such as token and temporary-process operations. Unprovided privileged imports must keep failing explicitly.
- [ ] Design an optional subprocess BOF runner with deadlines, crash reporting, ordered typed output, and worker cleanup. Define how host callbacks cross the process boundary before accepting process-local resolver pointers in worker mode.
- [ ] Add native worker-thread callback routing only after its lifetime and output-order contract is defined. Keep the existing entry-thread behavior as the default.
- [ ] Address the Darwin `arm64` Mach-O variadic Beacon print limitation only with an ABI-correct bridge and native test objects; `BeaconOutput` remains available.

The current cooperative Stop API remains useful for BOFs that poll it. The subprocess runner would provide a separate hard timeout and crash boundary.

## Deployment and tooling

- [ ] Investigate Linux musl and static-host portability with an Alpine runtime fixture. `CGO_ENABLED=0` currently still links the root and `native` packages to glibc. First prove BOF, C/Rust root and `native`, recursive dependencies, and callbacks without glibc; evaluate Go `c-shared` separately.
- [ ] Add CLI recursive mode and native export arguments with return-value reporting. Add a BOF command if it serves a concrete operator workflow.
- [ ] Extend Darwin CI across OS versions where native runners are available, retaining native Intel and ARM coverage and the existing dyld stress tests.
- [ ] Consider file and recursive APIs in `native` only after establishing a consumer need and preserving its C/Rust-only, smaller-footprint contract.

## Later platform candidates

NetBSD `arm64`, OpenBSD `amd64` then `arm64`, DragonFly `amd64`, and illumos/Solaris `amd64` can reuse parts of the ELF work but each needs OS-specific memory, dynamic-loader, callback, and runtime-runner proof. OpenBSD additionally needs validation under its executable-memory and `pledge` policy. Big-endian Linux targets need parser and relocation redesign; iOS needs code-signing and executable-memory feasibility work; AIX needs XCOFF support; and Plan 9 lacks a defined BOF format in the manifest. The [manifest](platform-support.json) remains the complete inventory of unsupported targets.
