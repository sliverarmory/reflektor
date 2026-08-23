#!/usr/bin/env bash

set -euo pipefail

cd /workspace

target="$(go env GOOS)/$(go env GOARCH)"
case "${target}" in
  linux/arm)
    if [[ "$(go env GOARM)" != "7" ]]; then
      echo "shared-library tests require linux/arm GOARM=7" >&2
      exit 1
    fi
    zig_target="arm-linux-gnueabihf"
    rust_target="armv7-unknown-linux-gnueabihf"
    ;;
  linux/riscv64)
    zig_target="riscv64-linux-gnu"
    rust_target="riscv64gc-unknown-linux-gnu"
    ;;
  linux/ppc64le)
    zig_target="powerpc64le-linux-gnu"
    rust_target="powerpc64le-unknown-linux-gnu"
    ;;
  *)
    echo "unsupported emulated shared-library target: ${target}" >&2
    exit 1
    ;;
esac

for command in go gcc zig rustc cargo file nm objdump; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    echo "required shared-library test tool is missing: ${command}" >&2
    exit 1
  fi
done

echo "shared-library target: ${target}"
echo "zig: $(zig version)"
echo "rustc: $(rustc --version)"
echo "cargo: $(cargo --version)"

export GOCACHE=/tmp/go-build-cache
export GOMODCACHE=/opt/go-mod-cache
export GOPROXY=off
export GOTOOLCHAIN=local
export ZIG_GLOBAL_CACHE_DIR=/tmp/zig-global-cache
export ZIG_LOCAL_CACHE_DIR=/tmp/zig-local-cache

intentional_skip_pattern='TestBuild(CSharedLibraryMatrix|RecursiveCSharedLibraryMatrix|BOFMatrix)|TestLinuxARMRuntimeVariant|TestLoadAndExecuteGeneratedBOF|TestLoadAndExecuteLegacyDarwinELFBOF|TestBOFLoadWithOptions|TestBOFPackageDependencyGraphIsIsolated|TestSituationalAwarenessBOFCorpus(Child)?'

shared_packages=(
  .
  ./cli
  ./memmod
  ./native/...
  ./integration
)

required_shared_tests=(
  TestLoadGeneratedCLinuxSOAndCallStartW
  TestLoadGeneratedRustHTTPSharedLibrary
  TestLoadGeneratedCSharedLibraryRecursiveMode
  TestLoadGeneratedRustSharedLibraryRecursiveMode
  TestLoadLibraryRecursiveDependencies
  TestCallExportWithArgs
  TestNativePackageCallExport
  TestNativePackageRejectsGoCSharedImage
  TestNativePackageLinuxDependencyGraphIsIsolated
  TestNativePackageCallExportWithArgsLinux
  TestNativePackageRustCallExportWithArgsLinux
  TestNativePackageELFLifecycleLinux
  TestLoadLibraryAndCallExport_Linux
)

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

assert_no_unexpected_skips() {
  local log_path="$1"
  local unexpected_skips=""
  unexpected_skips="$(grep -E '^[[:space:]]*--- SKIP:' "${log_path}" | grep -Ev "${intentional_skip_pattern}" || true)"
  if [[ -n "${unexpected_skips}" ]]; then
    echo "${target} tests were skipped; refusing to pass CI." >&2
    echo "${unexpected_skips}" >&2
    exit 1
  fi
}

export CGO_ENABLED=1
export CC=gcc
export CXX=g++
go test "${shared_packages[@]}" -skip "${intentional_skip_pattern}" -timeout 45m -count=1 -v | tee linux-shared-cgo-test.log
assert_required_tests linux-shared-cgo-test.log \
  "${required_shared_tests[@]}" \
  TestLoadGeneratedGoLinuxSOAndCallStartW \
  TestLoadGeneratedGoSharedLibraryRecursiveMode \
  TestNativePackageCSharedConsumerLinux
assert_no_unexpected_skips linux-shared-cgo-test.log

export CGO_ENABLED=0
unset CC CXX
nocgo_skip_pattern="${intentional_skip_pattern}|TestLoadGeneratedGo(LinuxSOAndCallStartW|SharedLibraryRecursiveMode)"
go test "${shared_packages[@]}" -skip "${nocgo_skip_pattern}" -timeout 45m -count=1 -v | tee linux-shared-nocgo-test.log
assert_required_tests linux-shared-nocgo-test.log "${required_shared_tests[@]}" TestCallExportWithPuregoCallback
assert_no_unexpected_skips linux-shared-nocgo-test.log

cli_dir="$(mktemp -d /tmp/reflektor-cli-e2e.XXXXXX)"
cli_c_fixture="${cli_dir}/basic-c.so"
cli_go_fixture="${cli_dir}/basic-go.so"
cli_rust_target_dir="${cli_dir}/cargo-target"
cli_rust_fixture="${cli_rust_target_dir}/${rust_target}/release/libreflektor_http_fixture.so"
zig cc -target "${zig_target}" -shared -fPIC -O2 -g0 \
  -o "${cli_c_fixture}" testdata/c/basic.c
CGO_ENABLED=1 CC=gcc CXX=g++ \
  go build -buildmode=c-shared -buildvcs=false -trimpath \
    -o "${cli_go_fixture}" ./testdata/go/basic
cargo build \
  --manifest-path testdata/rust/Cargo.toml \
  --locked \
  --release \
  --target "${rust_target}" \
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
    echo "${target} ${fixture_name} CLI lifecycle with CGO_ENABLED=${cgo_enabled} output = ${output@Q}, want ok" >&2
    exit 1
  fi
  if [[ ! -s "${marker_path}" || "$(cat "${marker_path}")" != "${expected_marker}" ]]; then
    echo "${target} ${fixture_name} CLI lifecycle with CGO_ENABLED=${cgo_enabled} marker mismatch" >&2
    exit 1
  fi
}

for cgo_enabled in 0 1; do
  cli_path="${cli_dir}/reflektor-cgo-${cgo_enabled}"
  CGO_ENABLED="${cgo_enabled}" CC=gcc CXX=g++ \
    go build -buildvcs=false -trimpath -o "${cli_path}" ./cli
  run_cli_fixture "${cgo_enabled}" c "${cli_c_fixture}" ok
  run_cli_fixture "${cgo_enabled}" rust "${cli_rust_fixture}" ok:200
  if [[ "${cgo_enabled}" == "1" ]]; then
    run_cli_fixture "${cgo_enabled}" go-c-shared "${cli_go_fixture}" ok
  fi
done

echo "all root, native, recursive, C, Rust, Go c-shared, and CLI tests passed for ${target} with no unexpected skips"
