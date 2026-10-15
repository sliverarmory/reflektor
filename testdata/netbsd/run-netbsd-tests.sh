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

: "${REFLEKTOR_BOF_CORPUS_DIR:?REFLEKTOR_BOF_CORPUS_DIR must point to the pinned BOF corpus checkout}"
corpus_dir="${REFLEKTOR_BOF_CORPUS_DIR}/dist/netbsd/amd64"
corpus_count="$(find "${corpus_dir}" -maxdepth 1 -type f -name '*.o' | wc -l | tr -d '[:space:]')"
if [[ "${corpus_count}" != 25 ]]; then
	echo "NetBSD/amd64 BOF corpus contains ${corpus_count} objects, want exactly 25" >&2
	exit 1
fi

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
	TestLoadAndExecuteGeneratedBOF
	TestBOFLoadWithOptions
	TestConcurrentBOFStreaming
	TestSameImageBOFExecutionIsSerialized
	TestCooperativeBOFStop
	TestBOFPackageDependencyGraphIsIsolated
	TestPlatformSupportManifest
)
bof_pattern="^($(IFS='|'; echo "${bof_tests[*]}"))$"
CGO_ENABLED=0 go test "${purego_gcflags[@]}" ./integration -run "${bof_pattern}" -timeout 45m -count=1 -v | tee netbsd-bof-test.log
assert_passes netbsd-bof-test.log "${bof_tests[@]}"

CGO_ENABLED=0 go test "${purego_gcflags[@]}" ./integration -run '^TestSituationalAwarenessBOFCorpus$' -timeout 45m -count=1 -v | tee netbsd-bof-corpus-test.log
assert_passes netbsd-bof-corpus-test.log TestSituationalAwarenessBOFCorpus

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
		CGO_ENABLED=0 go test "${purego_gcflags[@]}" . ./cli ./memmod ./native/... -timeout 45m -count=1 -v | tee "netbsd-packages-cgo-${cgo_enabled}-test.log"
		CGO_ENABLED=0 go test "${purego_gcflags[@]}" ./integration -run "${shared_pattern}" -timeout 45m -count=1 -v | tee "netbsd-shared-cgo-${cgo_enabled}-test.log"
	else
		CGO_ENABLED=1 go test . ./cli ./memmod ./native/... -timeout 45m -count=1 -v | tee "netbsd-packages-cgo-${cgo_enabled}-test.log"
		CGO_ENABLED=1 go test ./integration -run "${shared_pattern}" -timeout 45m -count=1 -v | tee "netbsd-shared-cgo-${cgo_enabled}-test.log"
	fi
	if grep -Eq '^[[:space:]]*--- SKIP:' "netbsd-packages-cgo-${cgo_enabled}-test.log"; then
		echo "NetBSD/amd64 package tests were skipped with CGO_ENABLED=${cgo_enabled}" >&2
		exit 1
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

cli_dir="$(mktemp -d /tmp/reflektor-netbsd-cli-e2e.XXXXXX)"
trap 'rm -rf -- "${cli_dir}"' EXIT
cli_c_fixture="${cli_dir}/basic-c.so"
cli_rust_target_dir="${cli_dir}/cargo-target"
cli_rust_fixture="${cli_rust_target_dir}/x86_64-unknown-netbsd/release/libreflektor_http_fixture.so"

zig cc -target x86_64-netbsd -shared -fPIC -O2 -g0 \
	-o "${cli_c_fixture}" testdata/c/basic.c
cargo build \
	--manifest-path testdata/rust/Cargo.toml \
	--locked \
	--release \
	--target x86_64-unknown-netbsd \
	--target-dir "${cli_rust_target_dir}"

run_cli_fixture() {
	local cgo_enabled="$1"
	local fixture_name="$2"
	local fixture_path="$3"
	local expected_marker="$4"
	local marker_path="${cli_dir}/marker-${fixture_name}-cgo-${cgo_enabled}"
	local output=""

	rm -f "${marker_path}"
	output="$(REFLEKTOR_MARKER="${marker_path}" "${cli_path}" "${fixture_path}" --call-export StartW)"
	if [[ "${output}" != "ok" ]]; then
		echo "NetBSD/amd64 ${fixture_name} CLI with CGO_ENABLED=${cgo_enabled} output=${output@Q}, want ok" >&2
		exit 1
	fi
	if [[ ! -s "${marker_path}" || "$(cat "${marker_path}")" != "${expected_marker}" ]]; then
		echo "NetBSD/amd64 ${fixture_name} CLI with CGO_ENABLED=${cgo_enabled} marker mismatch" >&2
		exit 1
	fi
}

for cgo_enabled in 0 1; do
	cli_path="${cli_dir}/reflektor-cgo-${cgo_enabled}"
	if [[ "${cgo_enabled}" == "0" ]]; then
		CGO_ENABLED=0 go build "${purego_gcflags[@]}" -buildvcs=false -trimpath -o "${cli_path}" ./cli
	else
		CGO_ENABLED=1 go build -buildvcs=false -trimpath -o "${cli_path}" ./cli
	fi
	run_cli_fixture "${cgo_enabled}" c "${cli_c_fixture}" ok
	run_cli_fixture "${cgo_enabled}" rust "${cli_rust_fixture}" ok:200
done

echo "NetBSD/amd64 BOF, C/Rust root, native, recursive, and CLI runtime tests passed"
