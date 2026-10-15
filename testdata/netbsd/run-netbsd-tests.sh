#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${repo_root}"

if [[ "$(uname -s)" != NetBSD || "$(go env GOOS)/$(go env GOARCH)" != netbsd/amd64 ]]; then
	echo "NetBSD/amd64 runtime tests require a NetBSD/amd64 guest" >&2
	exit 1
fi
for command in bash cargo cc file go pmap rustc zig; do
	command -v "${command}" >/dev/null || { echo "missing NetBSD test tool: ${command}" >&2; exit 1; }
done

export GOCACHE="${GOCACHE:-/tmp/reflektor-netbsd-go-build-cache}"
export GOMODCACHE="${GOMODCACHE:-/tmp/reflektor-netbsd-go-mod-cache}"
export GOTOOLCHAIN=local
export GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"
export ZIG_GLOBAL_CACHE_DIR="${ZIG_GLOBAL_CACHE_DIR:-/tmp/reflektor-netbsd-zig-global-cache}"
export ZIG_LOCAL_CACHE_DIR="${ZIG_LOCAL_CACHE_DIR:-/tmp/reflektor-netbsd-zig-local-cache}"
export CARGO_TARGET_DIR="${CARGO_TARGET_DIR:-/tmp/reflektor-netbsd-cargo-target}"
export CPATH="/usr/pkg/include${CPATH:+:${CPATH}}"
export LIBRARY_PATH="/usr/pkg/lib${LIBRARY_PATH:+:${LIBRARY_PATH}}"
export LD_LIBRARY_PATH="/usr/pkg/lib${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
export PKG_CONFIG_PATH="/usr/pkg/lib/pkgconfig${PKG_CONFIG_PATH:+:${PKG_CONFIG_PATH}}"

purego_gcflags=(-gcflags=github.com/ebitengine/purego/internal/fakecgo=-std)

assert_passes() {
	local log_path="$1"
	shift
	local name
	for name in "$@"; do
		if ! grep -Fq -- "--- PASS: ${name} " "${log_path}"; then
			echo "required NetBSD/amd64 test did not pass: ${name}" >&2
			exit 1
		fi
	done
	if grep -Eq '^[[:space:]]*--- SKIP:' "${log_path}"; then
		echo "NetBSD/amd64 tests were skipped:" >&2
		grep -E '^[[:space:]]*--- SKIP:' "${log_path}" >&2
		exit 1
	fi
}

CGO_ENABLED=0 go test "${purego_gcflags[@]}" ./bof ./internal/bofloader -count=1

bof_tests=(
	TestNetBSDGeneratedBOF
	TestPlatformSupportManifest
)
bof_pattern="^($(IFS='|'; echo "${bof_tests[*]}"))$"
CGO_ENABLED=0 go test "${purego_gcflags[@]}" ./integration -run "${bof_pattern}" -count=1 -v | tee netbsd-bof-test.log
assert_passes netbsd-bof-test.log "${bof_tests[@]}"

common_shared_tests=(
	TestLoadGeneratedCNetBSDSOAndCallStartW
	TestLoadGeneratedRustNetBSDSharedLibrary
	TestLoadGeneratedCSharedLibraryRecursiveMode
	TestLoadGeneratedRustSharedLibraryRecursiveMode
	TestLoadLibraryRecursiveDependencies
	TestCallExportWithArgs
	TestNativePackageCallExport
	TestNativeNetBSDCImageAndClose
	TestNativeNetBSDRustImageAndClose
)
for cgo_enabled in 0 1; do
	required_tests=("${common_shared_tests[@]}")
	if [[ "${cgo_enabled}" == 0 ]]; then
		required_tests+=(TestCallExportWithPuregoCallback)
	fi
	shared_pattern="^($(IFS='|'; echo "${required_tests[*]}"))$"
	if [[ "${cgo_enabled}" == 0 ]]; then
		CGO_ENABLED=0 go test "${purego_gcflags[@]}" ./integration -run "${shared_pattern}" -count=1 -v | tee "netbsd-shared-cgo-${cgo_enabled}-test.log"
	else
		CGO_ENABLED=1 go test ./integration -run "${shared_pattern}" -count=1 -v | tee "netbsd-shared-cgo-${cgo_enabled}-test.log"
	fi
	assert_passes "netbsd-shared-cgo-${cgo_enabled}-test.log" "${required_tests[@]}"
done

if CGO_ENABLED=1 go build -buildmode=c-shared -o /tmp/reflektor-netbsd-unsupported.so ./testdata/go/basic >netbsd-go-c-shared-unsupported.log 2>&1; then
	echo "Go unexpectedly built a netbsd/amd64 c-shared image" >&2
	exit 1
fi
if ! grep -Fq -- '-buildmode=c-shared not supported on netbsd/amd64' netbsd-go-c-shared-unsupported.log; then
	echo "Go netbsd/amd64 c-shared failure changed unexpectedly" >&2
	cat netbsd-go-c-shared-unsupported.log >&2
	exit 1
fi

echo "NetBSD/amd64 BOF, C/Rust root, native, and recursive runtime smoke tests passed"
