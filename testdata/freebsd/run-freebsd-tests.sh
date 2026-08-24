#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${repo_root}"

target="$(go env GOOS)/$(go env GOARCH)"
case "${target}" in
	freebsd/amd64)
		zig_target="x86_64-freebsd"
		rust_target="x86_64-unknown-freebsd"
		;;
	freebsd/arm64)
		zig_target="aarch64-freebsd"
		rust_target="aarch64-unknown-freebsd"
		;;
	*)
		echo "FreeBSD end-to-end tests require freebsd/amd64 or freebsd/arm64, got ${target}" >&2
		exit 1
		;;
esac

if [[ "$(uname -s)" != "FreeBSD" ]]; then
	echo "FreeBSD end-to-end tests must execute in a real FreeBSD guest" >&2
	exit 1
fi

for command in bash cargo cc file go nm objdump procstat rustc zig; do
	if ! command -v "${command}" >/dev/null 2>&1; then
		echo "required FreeBSD test tool is missing: ${command}" >&2
		exit 1
	fi
done

: "${REFLEKTOR_BOF_CORPUS_DIR:?REFLEKTOR_BOF_CORPUS_DIR must point to the pinned BOF corpus checkout}"
if [[ ! -s "${REFLEKTOR_BOF_CORPUS_DIR}/testdata/e2e-manifest.json" ]]; then
	echo "FreeBSD BOF corpus manifest is absent" >&2
	exit 1
fi
corpus_dir="${REFLEKTOR_BOF_CORPUS_DIR}/dist/freebsd/$(go env GOARCH)"
corpus_count="$(find "${corpus_dir}" -maxdepth 1 -type f -name '*.o' | wc -l | tr -d '[:space:]')"
if [[ "${corpus_count}" != "25" ]]; then
	echo "FreeBSD BOF corpus contains ${corpus_count} objects, want exactly 25" >&2
	exit 1
fi

export GOCACHE="${GOCACHE:-/tmp/reflektor-freebsd-go-build-cache}"
export GOMODCACHE="${GOMODCACHE:-/tmp/reflektor-freebsd-go-mod-cache}"
export GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"
export GOTOOLCHAIN=local
export ZIG_GLOBAL_CACHE_DIR="${ZIG_GLOBAL_CACHE_DIR:-/tmp/reflektor-freebsd-zig-global-cache}"
export ZIG_LOCAL_CACHE_DIR="${ZIG_LOCAL_CACHE_DIR:-/tmp/reflektor-freebsd-zig-local-cache}"
export CARGO_TARGET_DIR="${CARGO_TARGET_DIR:-/tmp/reflektor-freebsd-cargo-target}"
export CPATH="/usr/local/include${CPATH:+:${CPATH}}"
export LIBRARY_PATH="/usr/local/lib${LIBRARY_PATH:+:${LIBRARY_PATH}}"
export PKG_CONFIG_PATH="/usr/local/libdata/pkgconfig${PKG_CONFIG_PATH:+:${PKG_CONFIG_PATH}}"

purego_gcflags=(-gcflags=github.com/ebitengine/purego/internal/fakecgo=-std)

echo "runtime: $(uname -a)"
echo "freebsd: $(freebsd-version)"
echo "go: $(go version)"
echo "zig: $(zig version)"
echo "rustc: $(rustc --version)"
echo "cargo: $(cargo --version)"
echo "target: ${target}"

assert_required_tests() {
	local log_path="$1"
	shift
	local test_name=""
	for test_name in "$@"; do
		if ! grep -Fq -- "--- PASS: ${test_name} " "${log_path}"; then
			echo "required ${target} test did not pass: ${test_name}" >&2
			exit 1
		fi
	done
}

assert_no_skips() {
	local log_path="$1"
	if grep -Eq '^[[:space:]]*--- SKIP:' "${log_path}"; then
		echo "${target} tests were skipped; refusing to pass CI" >&2
		grep -E '^[[:space:]]*--- SKIP:' "${log_path}" >&2
		exit 1
	fi
}

bof_unit_log="freebsd-bof-unit-test.log"
CGO_ENABLED=0 go test "${purego_gcflags[@]}" ./bof ./internal/bofloader -count=1 -v | tee "${bof_unit_log}"
assert_no_skips "${bof_unit_log}"

bof_log="freebsd-bof-test.log"
CGO_ENABLED=0 go test "${purego_gcflags[@]}" ./integration \
	-run '^(TestPlatformSupportManifest|TestLoadAndExecuteGeneratedBOF|TestBOFLoadWithOptions|TestBOFPackageDependencyGraphIsIsolated)$' \
	-count=1 -v | tee "${bof_log}"
assert_required_tests "${bof_log}" \
	TestPlatformSupportManifest \
	TestLoadAndExecuteGeneratedBOF \
	TestBOFLoadWithOptions \
	TestBOFPackageDependencyGraphIsIsolated
assert_no_skips "${bof_log}"

corpus_log="freebsd-bof-corpus-test.log"
CGO_ENABLED=0 go test "${purego_gcflags[@]}" ./integration \
	-run '^TestSituationalAwarenessBOFCorpus$' -timeout 45m -count=1 -v | tee "${corpus_log}"
assert_required_tests "${corpus_log}" TestSituationalAwarenessBOFCorpus
assert_no_skips "${corpus_log}"

common_shared_tests=(
	TestLoadGeneratedCFreeBSDSOAndCallStartW
	TestLoadGeneratedRustHTTPSharedLibrary
	TestLoadGeneratedCSharedLibraryRecursiveMode
	TestLoadGeneratedRustSharedLibraryRecursiveMode
	TestLoadLibraryRecursiveDependencies
	TestLoadLibraryRecursiveFreeBSDSystemLibraryDelegatesToRTLD
	TestLoadLibraryRecursiveFreeBSDLDLibraryPathDependency
	TestCallExportWithArgs
	TestNativePackageCallExport
	TestNativePackageFreeBSDDependencyGraphIsIsolated
	TestNativePackageCallExportWithArgsFreeBSD
	TestNativePackageRustCallExportWithArgsFreeBSD
	TestNativePackageELFLifecycleFreeBSD
)
for cgo_enabled in 0 1; do
	shared_log="freebsd-shared-cgo-${cgo_enabled}-test.log"
	required_tests=("${common_shared_tests[@]}")
	if [[ "${cgo_enabled}" == "0" ]]; then
		required_tests+=(TestCallExportWithPuregoCallback)
	fi
	if [[ "${cgo_enabled}" == "1" && "${target}" == "freebsd/amd64" ]]; then
		required_tests+=(
			TestLoadGeneratedGoFreeBSDSOAndCallStartW
			TestLoadGeneratedGoSharedLibraryRecursiveMode
			TestNativePackageRejectsGoCSharedImage
			TestNativePackageCSharedConsumerFreeBSD
		)
	fi
	test_pattern="^($(IFS='|'; echo "${required_tests[*]}"))$"

	if [[ "${cgo_enabled}" == "0" ]]; then
		CGO_ENABLED=0 go test "${purego_gcflags[@]}" . ./cli ./memmod ./native/... -count=1 -v | tee "freebsd-packages-cgo-${cgo_enabled}-test.log"
		CGO_ENABLED=0 go test "${purego_gcflags[@]}" ./integration -run "${test_pattern}" -timeout 45m -count=1 -v | tee "${shared_log}"
	else
		CGO_ENABLED=1 go test . ./cli ./memmod ./native/... -count=1 -v | tee "freebsd-packages-cgo-${cgo_enabled}-test.log"
		CGO_ENABLED=1 go test ./integration -run "${test_pattern}" -timeout 45m -count=1 -v | tee "${shared_log}"
	fi
	assert_no_skips "freebsd-packages-cgo-${cgo_enabled}-test.log"
	assert_required_tests "${shared_log}" "${required_tests[@]}"
	assert_no_skips "${shared_log}"
done

if [[ "${target}" == "freebsd/arm64" ]]; then
	unsupported_log="freebsd-arm64-go-c-shared-unsupported.log"
	if CGO_ENABLED=1 go build -buildmode=c-shared -o /tmp/reflektor-freebsd-arm64-unsupported.so ./testdata/go/basic >"${unsupported_log}" 2>&1; then
		echo "Go unexpectedly built a freebsd/arm64 c-shared image" >&2
		exit 1
	fi
	if ! grep -Fq -- '-buildmode=c-shared not supported on freebsd/arm64' "${unsupported_log}"; then
		echo "Go freebsd/arm64 c-shared failure changed unexpectedly" >&2
		cat "${unsupported_log}" >&2
		exit 1
	fi
fi

cli_dir="$(mktemp -d /tmp/reflektor-freebsd-cli-e2e.XXXXXX)"
trap 'rm -rf -- "${cli_dir}"' EXIT
cli_c_fixture="${cli_dir}/basic-c.so"
cli_go_fixture="${cli_dir}/basic-go.so"
cli_rust_target_dir="${cli_dir}/cargo-target"
cli_rust_fixture="${cli_rust_target_dir}/${rust_target}/release/libreflektor_http_fixture.so"

zig cc -target "${zig_target}" -shared -fPIC -O2 -g0 \
	-o "${cli_c_fixture}" testdata/c/basic.c
cargo build \
	--manifest-path testdata/rust/Cargo.toml \
	--locked \
	--release \
	--target "${rust_target}" \
	--target-dir "${cli_rust_target_dir}"
if [[ "${target}" == "freebsd/amd64" ]]; then
	CGO_ENABLED=1 go build -buildmode=c-shared -buildvcs=false -trimpath \
		-o "${cli_go_fixture}" ./testdata/go/basic
fi

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
		echo "${target} ${fixture_name} CLI with CGO_ENABLED=${cgo_enabled} output=${output@Q}, want ok" >&2
		exit 1
	fi
	if [[ ! -s "${marker_path}" || "$(cat "${marker_path}")" != "${expected_marker}" ]]; then
		echo "${target} ${fixture_name} CLI with CGO_ENABLED=${cgo_enabled} marker mismatch" >&2
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
	if [[ "${cgo_enabled}" == "1" && "${target}" == "freebsd/amd64" ]]; then
		run_cli_fixture "${cgo_enabled}" go-c-shared "${cli_go_fixture}" ok
	fi
done

echo "all BOF, root, native, recursive, C, Rust, supported Go c-shared, and CLI tests passed for ${target} with no skips"
